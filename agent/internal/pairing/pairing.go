// Package pairing joins a phone to this PC (N4, N7).
//
// The `pair` command and the running server are separate processes. They
// talk through small files in the agent directory:
//
//	pairing.json           pair command: window id, code hash, expiry
//	pairing-request.json   server: a phone sent the right code
//	pairing-decision.json  pair command: the person at the PC allowed or refused
//	pairing-result.json    server: the device that was added, or why not
//
// The code is 128 random bits, valid for 5 minutes, and used at most once:
// the first request with the right code consumes the window whether or not
// the person allows it. Five wrong codes close the window. The phone does
// not hold a request open while the PC decides; it asks again (Status).
package pairing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/devices"
)

const (
	windowFile   = "pairing.json"
	requestFile  = "pairing-request.json"
	decisionFile = "pairing-decision.json"
	resultFile   = "pairing-result.json"

	// WindowTTL is how long a pairing code works.
	WindowTTL = 5 * time.Minute
	// MaxBadCodes closes the window after this many wrong codes.
	MaxBadCodes = 5
	// DecisionTimeout is how long the phone waits for the person at the PC.
	DecisionTimeout = 2 * time.Minute
)

var (
	ErrNoWindow = errors.New("no pairing window is open")
	ErrBadCode  = errors.New("wrong pairing code")
	ErrBusy     = errors.New("another device is pairing")
	ErrTimeout  = errors.New("nobody answered on the PC")
	ErrBadName  = errors.New("device name must be 1-64 printable characters")
)

// Window is an open pairing window.
type Window struct {
	ID        string    `json:"id"`
	CodeHash  string    `json:"codeHash"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Request is a phone that sent the right code and waits for the PC.
type Request struct {
	WindowID   string    `json:"windowId"`
	DeviceName string    `json:"deviceName"`
	Login      string    `json:"login"`
	RemoteIP   string    `json:"remoteIp"`
	At         time.Time `json:"at"`
}

// Decision is the answer from the person at the PC.
type Decision struct {
	WindowID string `json:"windowId"`
	Allow    bool   `json:"allow"`
}

// Result is what happened to the request.
type Result struct {
	WindowID   string `json:"windowId"`
	OK         bool   `json:"ok"`
	DeviceID   string `json:"deviceId,omitempty"`
	DeviceName string `json:"deviceName,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

var codeEnc = base32.StdEncoding.WithPadding(base32.NoPadding)

// Open starts a pairing window and returns the code to show in the QR. Any
// earlier window is replaced.
func Open(dir string, now time.Time) (code string, w Window, err error) {
	raw := make([]byte, 16) // 128 bits
	if _, err := rand.Read(raw); err != nil {
		return "", Window{}, err
	}
	id := make([]byte, 6)
	if _, err := rand.Read(id); err != nil {
		return "", Window{}, err
	}
	code = codeEnc.EncodeToString(raw) // 26 characters, A-Z and 2-7
	w = Window{ID: hex.EncodeToString(id), CodeHash: hashCode(code), CreatedAt: now.UTC(), ExpiresAt: now.Add(WindowTTL).UTC()}
	Close(dir)
	if err := writeJSON(filepath.Join(dir, windowFile), w); err != nil {
		return "", Window{}, err
	}
	return code, w, nil
}

// Close removes every pairing file.
func Close(dir string) {
	for _, f := range []string{windowFile, requestFile, decisionFile, resultFile} {
		os.Remove(filepath.Join(dir, f))
	}
}

// PendingRequest returns the request for window id, if a phone is waiting.
func PendingRequest(dir, id string) (Request, bool) {
	var r Request
	if readJSON(filepath.Join(dir, requestFile), &r) != nil || r.WindowID != id {
		return Request{}, false
	}
	return r, true
}

// Decide records the PC-side answer.
func Decide(dir, id string, allow bool) error {
	return writeJSON(filepath.Join(dir, decisionFile), Decision{WindowID: id, Allow: allow})
}

// ResultFor returns the outcome for window id, once there is one.
func ResultFor(dir, id string) (Result, bool) {
	var r Result
	if readJSON(filepath.Join(dir, resultFile), &r) != nil || r.WindowID != id {
		return Result{}, false
	}
	return r, true
}

// WindowOpen reports whether window id is still open.
func WindowOpen(dir, id string) bool {
	var w Window
	return readJSON(filepath.Join(dir, windowFile), &w) == nil && w.ID == id
}

// Pairer is the server side. A phone first calls Start, which returns at
// once with a request id, then asks Status until the person at the PC has
// answered. A phone that leaves the browser or loses the connection for a
// while can simply ask again.
type Pairer struct {
	Dir        string
	Devices    *devices.Store
	Now        func() time.Time
	Poll       time.Duration // how often to look for the decision
	Timeout    time.Duration // DecisionTimeout by default
	CollectTTL time.Duration // CollectTimeout by default

	mu     sync.Mutex
	busy   bool                // a request waits for the PC
	misses map[string]int      // wrong codes per window id
	reqs   map[string]*pending // by request id
}

// CollectTimeout is how long an issued token waits for the phone to fetch
// it. A device whose token was never fetched is removed again.
const CollectTimeout = 5 * time.Minute

// State is where a pairing request is.
type State string

const (
	StatePending  State = "pending"
	StateApproved State = "approved"
	StateDenied   State = "denied"
	StateTimeout  State = "timeout"
	StateFailed   State = "failed"
)

// ErrUnknownRequest is returned for a request id that does not exist, or
// whose outcome was already fetched or dropped.
var ErrUnknownRequest = errors.New("unknown pairing request")

type pending struct {
	state  State
	token  string
	device devices.Device
	timer  *time.Timer
}

// Origin describes where a pairing request came from, for the PC prompt.
type Origin struct {
	Login    string
	RemoteIP string
}

// Start checks the code, shows the request on the PC and returns a request
// id for Status. The id is a secret: whoever holds it gets the token.
func (p *Pairer) Start(code, deviceName string, o Origin) (string, error) {
	name, ok := cleanName(deviceName)
	if !ok {
		return "", ErrBadName
	}
	w, err := p.claim(code)
	if err != nil {
		return "", err
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		p.release()
		return "", err
	}
	id := hex.EncodeToString(raw)
	req := Request{WindowID: w.ID, DeviceName: name, Login: o.Login, RemoteIP: o.RemoteIP, At: p.now().UTC()}
	if err := writeJSON(filepath.Join(p.Dir, requestFile), req); err != nil {
		p.release()
		return "", err
	}
	p.mu.Lock()
	if p.reqs == nil {
		p.reqs = map[string]*pending{}
	}
	p.reqs[id] = &pending{state: StatePending}
	p.mu.Unlock()
	go p.await(id, w.ID, name)
	return id, nil
}

// await waits for the PC, then records the outcome for Status.
func (p *Pairer) await(id, windowID, name string) {
	allow, err := p.waitDecision(context.Background(), windowID)
	res := Result{WindowID: windowID}
	e := &pending{}
	switch {
	case err != nil:
		e.state, res.Reason = StateTimeout, "timeout"
	case !allow:
		e.state, res.Reason = StateDenied, "denied"
	default:
		token, d, err := p.Devices.Add(name)
		if err != nil {
			e.state, res.Reason = StateFailed, "store"
		} else {
			e.state, e.token, e.device = StateApproved, token, d
			res.OK, res.DeviceID, res.DeviceName = true, d.ID, d.Name
		}
	}
	p.finish(res)

	ttl := p.CollectTTL
	if ttl == 0 {
		ttl = CollectTimeout
	}
	p.mu.Lock()
	p.reqs[id] = e
	p.busy = false
	e.timer = time.AfterFunc(ttl, func() { p.drop(id) })
	p.mu.Unlock()
}

// drop forgets an outcome nobody fetched. An approved device whose token
// never reached the phone is removed, so no unusable device stays behind.
func (p *Pairer) drop(id string) {
	p.mu.Lock()
	e, ok := p.reqs[id]
	if ok {
		delete(p.reqs, id)
	}
	p.mu.Unlock()
	if ok && e.state == StateApproved {
		_, _ = p.Devices.Revoke(e.device.ID)
	}
}

// Status reports a request. A final outcome is handed out once: an
// approved token is returned and forgotten.
func (p *Pairer) Status(id string) (State, string, devices.Device, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.reqs[id]
	if !ok || len(id) != 32 {
		return "", "", devices.Device{}, ErrUnknownRequest
	}
	if e.state == StatePending {
		return StatePending, "", devices.Device{}, nil
	}
	delete(p.reqs, id)
	if e.timer != nil {
		e.timer.Stop()
	}
	return e.state, e.token, e.device, nil
}

func (p *Pairer) release() {
	p.mu.Lock()
	p.busy = false
	p.mu.Unlock()
}

// claim checks the code and takes the window for this request.
func (p *Pairer) claim(code string) (Window, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.busy {
		return Window{}, ErrBusy
	}
	var w Window
	path := filepath.Join(p.Dir, windowFile)
	if readJSON(path, &w) != nil || w.ID == "" {
		return Window{}, ErrNoWindow
	}
	if !p.now().Before(w.ExpiresAt) {
		os.Remove(path)
		return Window{}, ErrNoWindow
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) > 64 || subtle.ConstantTimeCompare([]byte(hashCode(code)), []byte(w.CodeHash)) != 1 {
		if p.misses == nil {
			p.misses = map[string]int{}
		}
		p.misses[w.ID]++
		if p.misses[w.ID] >= MaxBadCodes {
			os.Remove(path)
		}
		return Window{}, ErrBadCode
	}
	p.busy = true
	return w, nil
}

func (p *Pairer) waitDecision(ctx context.Context, id string) (bool, error) {
	timeout := p.Timeout
	if timeout == 0 {
		timeout = DecisionTimeout
	}
	poll := p.Poll
	if poll == 0 {
		poll = 200 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		var d Decision
		if readJSON(filepath.Join(p.Dir, decisionFile), &d) == nil && d.WindowID == id {
			return d.Allow, nil
		}
		select {
		case <-ctx.Done():
			return false, ErrTimeout
		case <-t.C:
		}
	}
}

// finish consumes the window and leaves only the result for the pair
// command to read.
func (p *Pairer) finish(res Result) {
	for _, f := range []string{windowFile, requestFile, decisionFile} {
		os.Remove(filepath.Join(p.Dir, f))
	}
	_ = writeJSON(filepath.Join(p.Dir, resultFile), res)
}

func (p *Pairer) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func cleanName(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > 64 {
		return "", false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return "", false
		}
	}
	return s, true
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte("asksemble-pair:" + code))
	return hex.EncodeToString(sum[:])
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(path, append(b, '\n'))
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
