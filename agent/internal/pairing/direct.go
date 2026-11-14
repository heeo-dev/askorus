package pairing

// Pairing without a code (#58). With Tailscale built in, a phone signed in to
// the person's own account finds this PC in the account's device list and
// asks to pair without having seen a QR code. What stays is the person's
// consent, given in one of three ways:
//
//   - a pairing window is open on the PC (`pair` or `setup` is waiting in a
//     terminal the person is looking at): the first request is taken at once;
//   - no window: the PC shows a question on its screen (Confirm);
//   - nobody at the PC: a phone that is already paired answers (Pending,
//     Answer).
//
// The phone shows two digits and sends them along; every place that asks or
// reports shows the same two digits, so a request from another device that
// only carries the phone's name can be told apart.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sync"
	"time"
)

var (
	// ErrNoApprover: no window is open, no question can be shown on this
	// PC and no phone is paired, so nobody could answer.
	ErrNoApprover = errors.New("nobody can allow it: no pairing window, no screen to ask on, no paired phone")
	ErrBadMatch   = errors.New("match must be two digits")
)

// StateUnattended: the request found nobody who could answer it.
const StateUnattended State = "unattended"

// Question is a code-less request as the person sees it.
type Question struct {
	// ID names the request for a paired phone's answer. It is not the id
	// the asking phone holds: that one fetches the token.
	ID         string    `json:"id"`
	DeviceName string    `json:"deviceName"`
	Match      string    `json:"match"`
	Login      string    `json:"login,omitempty"`
	RemoteIP   string    `json:"remoteIp,omitempty"`
	At         time.Time `json:"at"`
}

// direct is the code-less request that waits for an answer; at most one.
type direct struct {
	q      Question
	answer chan bool
	once   sync.Once
}

func (d *direct) decide(allow bool) {
	d.once.Do(func() { d.answer <- allow })
}

func matchOK(s string) bool {
	return len(s) == 2 && s[0] >= '0' && s[0] <= '9' && s[1] >= '0' && s[1] <= '9'
}

func randomHex(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// openWindow returns the pairing window, if one is open now.
func (p *Pairer) openWindow() (Window, bool) {
	var w Window
	if readJSON(filepath.Join(p.Dir, windowFile), &w) != nil || w.ID == "" || !p.now().Before(w.ExpiresAt) {
		return Window{}, false
	}
	return w, true
}

// WindowIsOpen reports whether a pairing window is open on the PC: a
// code-less request is then taken without a question.
func (p *Pairer) WindowIsOpen() bool {
	_, ok := p.openWindow()
	return ok
}

// StartDirect takes a request without a code and returns a request id for
// Status, as Start does. The caller has already been checked to be a device
// of the PC owner's own Tailscale account.
func (p *Pairer) StartDirect(deviceName, match string, o Origin) (string, error) {
	name, ok := cleanName(deviceName)
	if !ok {
		return "", ErrBadName
	}
	if !matchOK(match) {
		return "", ErrBadMatch
	}
	p.mu.Lock()
	if p.busy {
		p.mu.Unlock()
		return "", ErrBusy
	}
	p.busy = true
	p.mu.Unlock()

	id, err := randomHex(16)
	qid, err2 := randomHex(6)
	if err != nil || err2 != nil {
		p.release()
		return "", errors.Join(err, err2)
	}
	_, windowOpen := p.openWindow()
	if !windowOpen && p.Confirm == nil {
		// Nobody is at a window and nothing can be shown on the screen: only
		// a paired phone could answer. Without one, say so at once.
		if list, err := p.Devices.List(); err != nil || len(list) == 0 {
			p.release()
			return "", ErrNoApprover
		}
	}
	d := &direct{
		q:      Question{ID: qid, DeviceName: name, Match: match, Login: o.Login, RemoteIP: o.RemoteIP, At: p.now().UTC()},
		answer: make(chan bool, 1),
	}
	p.mu.Lock()
	if p.reqs == nil {
		p.reqs = map[string]*pending{}
	}
	p.reqs[id] = &pending{state: StatePending}
	p.direct = d
	p.mu.Unlock()
	go p.awaitDirect(id, d)
	return id, nil
}

// awaitDirect waits for the first answer: an open window (now, or one the
// person opens while the phone waits), the question on the PC's screen, or
// a paired phone. Then it records the outcome for Status.
func (p *Pairer) awaitDirect(id string, d *direct) {
	timeout := p.Timeout
	if timeout == 0 {
		timeout = DecisionTimeout
	}
	poll := p.Poll
	if poll == 0 {
		poll = 200 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// The question on the screen. It is taken away (ctx) as soon as the
	// answer came from elsewhere.
	shown := make(chan bool, 1)
	asked := false
	ask := func() {
		if asked || p.Confirm == nil {
			return
		}
		asked = true
		go func() {
			allow, ok := p.Confirm(ctx, d.q)
			// An answer counts only while the question was still open: a
			// dialog that ends because the time is up is no refusal.
			if ok && ctx.Err() == nil {
				d.decide(allow)
			}
			shown <- ok
		}()
	}

	windowID := ""
	allow, state := false, StatePending
	t := time.NewTicker(poll)
	defer t.Stop()
	for state == StatePending {
		if w, ok := p.openWindow(); ok {
			// The person has a pairing window open: that is the consent.
			windowID, allow, state = w.ID, true, StateApproved
			break
		}
		ask()
		select {
		case allow = <-d.answer:
			state = StateDenied
			if allow {
				state = StateApproved
			}
		case ok := <-shown:
			if !ok {
				// No screen to ask on. Only a paired phone can still answer.
				if list, err := p.Devices.List(); err != nil || len(list) == 0 {
					state = StateUnattended
				}
			}
		case <-ctx.Done():
			state = StateTimeout
		case <-t.C:
		}
	}
	cancel()

	e := &pending{state: state}
	res := Result{WindowID: windowID, DeviceName: d.q.DeviceName, Match: d.q.Match}
	switch state {
	case StateApproved:
		token, dev, err := p.Devices.Add(d.q.DeviceName)
		if err != nil {
			e.state, res.Reason = StateFailed, "store"
		} else {
			e.token, e.device = token, dev
			res.OK, res.DeviceID, res.DeviceName = true, dev.ID, dev.Name
		}
	case StateDenied:
		res.Reason = "denied"
	default:
		res.Reason = "timeout"
	}
	if windowID != "" {
		// The window is used up, and the pair command reads who came in.
		p.finish(res)
	}

	ttl := p.CollectTTL
	if ttl == 0 {
		ttl = CollectTimeout
	}
	p.mu.Lock()
	p.reqs[id] = e
	p.busy = false
	p.direct = nil
	e.timer = time.AfterFunc(ttl, func() { p.drop(id) })
	p.mu.Unlock()
}

// Pending is the code-less request that waits for an answer, for a paired
// phone to show. There is at most one.
func (p *Pairer) Pending() []Question {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.direct == nil {
		return []Question{}
	}
	return []Question{p.direct.q}
}

// Answer is a paired phone's answer to the waiting request.
func (p *Pairer) Answer(questionID string, allow bool) error {
	p.mu.Lock()
	d := p.direct
	p.mu.Unlock()
	if d == nil || questionID == "" || d.q.ID != questionID {
		return ErrUnknownRequest
	}
	d.decide(allow)
	return nil
}
