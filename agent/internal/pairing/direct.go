package pairing

// Pairing without a code (#58). With Tailscale built in, a phone signed in to
// the person's own account finds this PC in the account's device list and
// asks to pair without having seen a QR code. What stays is the person's
// consent, given in one of three ways:
//
//   - a pairing window is open on the PC (`pair` or `setup` is waiting in a
//     terminal the person is looking at): the first request is taken at once.
//     The window counts only while that command runs (it says so every
//     second), and for one request;
//   - no window: the PC shows a question on its screen (Confirm);
//   - nobody at the PC: a phone that is already paired answers (Pending,
//     Answer).
//
// The phone shows two digits and sends them along; every place that asks or
// reports shows the same two digits, so a request from another device that
// only carries the phone's name can be told apart.
//
// Whatever answers first settles the request, once: a refusal that came
// first is not undone by a window opened a moment later, and an answer to a
// request that is already settled is an error.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
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
	q        Question
	done     chan struct{} // closed when the request is settled
	deadline time.Time     // an answer counts until then

	mu       sync.Mutex
	state    State  // StatePending until settled
	windowID string // the window that let it in, if one did
}

// settle records the outcome if there is none yet, and reports whether it
// did: the first answer counts, whoever gave it. An allowance or a refusal
// that comes when the time is up is too late, even when it gets here before
// the timeout does: the request is then settled as timed out.
func (d *direct) settle(st State, windowID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state != StatePending {
		return false
	}
	late := (st == StateApproved || st == StateDenied) && !d.deadline.IsZero() && !time.Now().Before(d.deadline)
	if late {
		st, windowID = StateTimeout, ""
	}
	d.state, d.windowID = st, windowID
	close(d.done)
	return !late
}

func (d *direct) outcome() (State, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state, d.windowID
}

func answerState(allow bool) State {
	if allow {
		return StateApproved
	}
	return StateDenied
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

// openWindow returns the pairing window, if one is open now: not expired,
// not used for a request yet, and held by a pair command that still runs.
func (p *Pairer) openWindow() (Window, bool) {
	var w Window
	now := p.now()
	if readJSON(filepath.Join(p.Dir, windowFile), &w) != nil || w.ID == "" || !now.Before(w.ExpiresAt) {
		return Window{}, false
	}
	p.mu.Lock()
	used := p.isUsedLocked(w.ID)
	p.mu.Unlock()
	if used {
		return Window{}, false
	}
	fi, err := os.Stat(filepath.Join(p.Dir, aliveFile))
	if err != nil {
		return Window{}, false
	}
	sign := fi.ModTime()
	if age := now.Sub(sign); age >= AliveFor || age <= -AliveFor {
		return Window{}, false
	}
	// The PC's clock may have been set back onto an old sign of life. A sign
	// that has not moved for AliveFor of this process's own time is old.
	p.mu.Lock()
	if !sign.Equal(p.aliveSeen) {
		p.aliveSeen, p.aliveSeenAt = sign, time.Now()
	}
	old := sinceSeen(p.aliveSeenAt) >= AliveFor
	p.mu.Unlock()
	if old {
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
// of the PC owner's own Tailscale account. ended, if not nil, is called once
// with the outcome when the request is settled, whether or not the phone
// ever asks for it.
func (p *Pairer) StartDirect(deviceName, match string, o Origin, ended func(State)) (string, error) {
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
	timeout := p.Timeout
	if timeout == 0 {
		timeout = DecisionTimeout
	}
	d := &direct{
		q:        Question{ID: qid, DeviceName: name, Match: match, Login: o.Login, RemoteIP: o.RemoteIP, At: p.now().UTC()},
		done:     make(chan struct{}),
		deadline: time.Now().Add(timeout),
		state:    StatePending,
	}
	p.mu.Lock()
	if p.reqs == nil {
		p.reqs = map[string]*pending{}
	}
	p.reqs[id] = &pending{state: StatePending}
	p.direct = d
	p.mu.Unlock()
	go p.awaitDirect(id, d, ended)
	return id, nil
}

// awaitDirect waits for the first answer: an open window (now, or one the
// person opens while the phone waits), the question on the PC's screen, or
// a paired phone. Then it records the outcome for Status.
func (p *Pairer) awaitDirect(id string, d *direct, ended func(State)) {
	poll := p.Poll
	if poll == 0 {
		poll = 200 * time.Millisecond
	}
	ctx, cancel := context.WithDeadline(context.Background(), d.deadline)
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
				d.settle(answerState(allow), "")
			}
			shown <- ok
		}()
	}

	t := time.NewTicker(poll)
	defer t.Stop()
	for waiting := true; waiting; {
		if w, ok := p.openWindow(); ok {
			// The person has a pairing window open: that is the consent. The
			// window is used up from this moment, before any token exists.
			if d.settle(StateApproved, w.ID) {
				p.markUsed(w.ID, w.ExpiresAt)
			}
			break
		}
		ask()
		select {
		case <-d.done:
			waiting = false
		case ok := <-shown:
			if !ok {
				// No screen to ask on. Only a paired phone can still answer.
				if list, err := p.Devices.List(); err != nil || len(list) == 0 {
					d.settle(StateUnattended, "")
				}
			}
		case <-ctx.Done():
			d.settle(StateTimeout, "")
		case <-t.C:
		}
	}
	cancel()

	state, windowID := d.outcome()
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
		// The pair command reads who came in.
		p.finish(res)
	}

	// Told before the place in the line is free again: whoever counts the
	// outcome has counted it when the next request comes.
	if ended != nil {
		ended(e.state)
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
	if st, _ := p.direct.outcome(); st != StatePending {
		return []Question{} // settled a moment ago: nothing left to answer
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
	if !d.settle(answerState(allow), "") {
		// Settled already: the screen, a window, another phone or the time.
		return ErrUnknownRequest
	}
	return nil
}
