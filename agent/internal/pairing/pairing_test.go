package pairing

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/devices"
)

func newPairer(t *testing.T) (*Pairer, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := devices.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &Pairer{Dir: dir, Devices: store, Poll: 10 * time.Millisecond, Timeout: 2 * time.Second}, dir
}

// answer plays the pair command: it waits for the request and answers.
func answer(t *testing.T, dir, id string, allow bool) <-chan Request {
	t.Helper()
	ch := make(chan Request, 1)
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if r, ok := PendingRequest(dir, id); ok {
				Decide(dir, id, allow)
				ch <- r
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		close(ch)
	}()
	return ch
}

// waitFinal plays the phone: it asks until the request is no longer pending.
func waitFinal(t *testing.T, p *Pairer, id string) (State, string, devices.Device) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, tok, d, err := p.Status(id)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if st != StatePending {
			return st, tok, d
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("still pending")
	return "", "", devices.Device{}
}

func TestPairAllowed(t *testing.T) {
	p, dir := newPairer(t)
	code, w, err := Open(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 26 {
		t.Errorf("code length %d", len(code))
	}
	b, _ := os.ReadFile(filepath.Join(dir, windowFile))
	if strings.Contains(string(b), code) {
		t.Fatal("window file contains the raw code")
	}
	seen := answer(t, dir, w.ID, true)
	id, err := p.Start(strings.ToLower(code), " Galaxy S23 ", Origin{Login: "owner@example.com", RemoteIP: "100.64.0.9"})
	if err != nil {
		t.Fatal(err)
	}
	if st, _, _, _ := p.Status(id); st != StatePending && st != StateApproved {
		t.Errorf("first status = %q", st)
	}
	st, tok, d := waitFinal(t, p, id)
	r := <-seen
	if r.DeviceName != "Galaxy S23" || r.Login != "owner@example.com" || r.RemoteIP != "100.64.0.9" {
		t.Errorf("request shown on PC = %+v", r)
	}
	if st != StateApproved {
		t.Fatalf("state = %q", st)
	}
	if got, ok := p.Devices.Verify(tok); !ok || got.ID != d.ID {
		t.Error("issued token does not verify")
	}
	if _, _, _, err := p.Status(id); !errors.Is(err, ErrUnknownRequest) {
		t.Errorf("token handed out twice: %v", err)
	}
	res, ok := ResultFor(dir, w.ID)
	if !ok || !res.OK || res.DeviceID != d.ID {
		t.Errorf("result = %+v %v", res, ok)
	}
	if WindowOpen(dir, w.ID) {
		t.Error("window still open after use")
	}
	if _, err := p.Start(code, "again", Origin{}); !errors.Is(err, ErrNoWindow) {
		t.Errorf("code reused: %v", err)
	}
}

func TestPairDenied(t *testing.T) {
	p, dir := newPairer(t)
	code, w, _ := Open(dir, time.Now())
	answer(t, dir, w.ID, false)
	id, err := p.Start(code, "phone", Origin{})
	if err != nil {
		t.Fatal(err)
	}
	if st, tok, _ := waitFinal(t, p, id); st != StateDenied || tok != "" {
		t.Fatalf("state = %q", st)
	}
	if res, _ := ResultFor(dir, w.ID); res.OK || res.Reason != "denied" {
		t.Errorf("result = %+v", res)
	}
	if list, _ := p.Devices.List(); len(list) != 0 {
		t.Error("device added after refusal")
	}
	if _, err := p.Start(code, "phone", Origin{}); !errors.Is(err, ErrNoWindow) {
		t.Errorf("refused code reusable: %v", err)
	}
}

func TestPairTimeout(t *testing.T) {
	p, dir := newPairer(t)
	p.Timeout = 50 * time.Millisecond
	code, w, _ := Open(dir, time.Now())
	id, _ := p.Start(code, "phone", Origin{})
	if st, _, _ := waitFinal(t, p, id); st != StateTimeout {
		t.Fatalf("state = %q", st)
	}
	if WindowOpen(dir, w.ID) {
		t.Error("window still open after timeout")
	}
}

// A token the phone never fetched does not leave a device behind.
func TestUncollectedTokenIsRevoked(t *testing.T) {
	p, dir := newPairer(t)
	p.CollectTTL = 50 * time.Millisecond
	code, w, _ := Open(dir, time.Now())
	answer(t, dir, w.ID, true)
	id, _ := p.Start(code, "phone", Origin{})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if res, ok := ResultFor(dir, w.ID); ok && res.OK {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if list, _ := p.Devices.List(); len(list) != 1 {
		t.Fatalf("device not added: %d", len(list))
	}
	for time.Now().Before(deadline) {
		if list, _ := p.Devices.List(); len(list) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if list, _ := p.Devices.List(); len(list) != 0 {
		t.Error("uncollected device still there")
	}
	if _, _, _, err := p.Status(id); !errors.Is(err, ErrUnknownRequest) {
		t.Errorf("dropped request still answers: %v", err)
	}
}

func TestBadCodesCloseWindow(t *testing.T) {
	p, dir := newPairer(t)
	code, w, _ := Open(dir, time.Now())
	for i := 0; i < MaxBadCodes; i++ {
		if _, err := p.Start("AAAA", "phone", Origin{}); !errors.Is(err, ErrBadCode) {
			t.Fatalf("try %d: %v", i, err)
		}
	}
	if WindowOpen(dir, w.ID) {
		t.Fatal("window open after too many wrong codes")
	}
	if _, err := p.Start(code, "phone", Origin{}); !errors.Is(err, ErrNoWindow) {
		t.Errorf("right code after lockout: %v", err)
	}
}

func TestExpired(t *testing.T) {
	p, dir := newPairer(t)
	start := time.Now()
	code, _, _ := Open(dir, start)
	p.Now = func() time.Time { return start.Add(WindowTTL) }
	if _, err := p.Start(code, "phone", Origin{}); !errors.Is(err, ErrNoWindow) {
		t.Errorf("expired code: %v", err)
	}
}

func TestNoWindowBadNameUnknownID(t *testing.T) {
	p, dir := newPairer(t)
	if _, err := p.Start("X", "phone", Origin{}); !errors.Is(err, ErrNoWindow) {
		t.Errorf("no window: %v", err)
	}
	code, _, _ := Open(dir, time.Now())
	for _, name := range []string{"", "   ", strings.Repeat("a", 65), "bad\nname", "bell\x07"} {
		if _, err := p.Start(code, name, Origin{}); !errors.Is(err, ErrBadName) {
			t.Errorf("name %q: %v", name, err)
		}
	}
	for _, id := range []string{"", "nope", strings.Repeat("0", 32)} {
		if _, _, _, err := p.Status(id); !errors.Is(err, ErrUnknownRequest) {
			t.Errorf("Status(%q) = %v", id, err)
		}
	}
}

func TestBusy(t *testing.T) {
	p, dir := newPairer(t)
	code, w, _ := Open(dir, time.Now())
	id, err := p.Start(code, "first", Origin{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Start(code, "second", Origin{}); !errors.Is(err, ErrBusy) {
		t.Errorf("second request: %v", err)
	}
	Decide(dir, w.ID, true)
	if st, _, _ := waitFinal(t, p, id); st != StateApproved {
		t.Errorf("first request: %q", st)
	}
}
