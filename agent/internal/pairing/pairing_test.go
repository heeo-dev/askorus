package pairing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/devices"
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
	tok, d, err := p.Pair(context.Background(), strings.ToLower(code), " Galaxy S23 ", Origin{Login: "owner@example.com", RemoteIP: "100.64.0.9"})
	if err != nil {
		t.Fatal(err)
	}
	r := <-seen
	if r.DeviceName != "Galaxy S23" || r.Login != "owner@example.com" || r.RemoteIP != "100.64.0.9" {
		t.Errorf("request shown on PC = %+v", r)
	}
	if got, ok := p.Devices.Verify(tok); !ok || got.ID != d.ID {
		t.Error("issued token does not verify")
	}
	res, ok := ResultFor(dir, w.ID)
	if !ok || !res.OK || res.DeviceID != d.ID {
		t.Errorf("result = %+v %v", res, ok)
	}
	if WindowOpen(dir, w.ID) {
		t.Error("window still open after use")
	}
	if _, _, err := p.Pair(context.Background(), code, "again", Origin{}); !errors.Is(err, ErrNoWindow) {
		t.Errorf("code reused: %v", err)
	}
}

func TestPairDenied(t *testing.T) {
	p, dir := newPairer(t)
	code, w, _ := Open(dir, time.Now())
	answer(t, dir, w.ID, false)
	if _, _, err := p.Pair(context.Background(), code, "phone", Origin{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("err = %v", err)
	}
	if res, _ := ResultFor(dir, w.ID); res.OK || res.Reason != "denied" {
		t.Errorf("result = %+v", res)
	}
	if list, _ := p.Devices.List(); len(list) != 0 {
		t.Error("device added after refusal")
	}
	if _, _, err := p.Pair(context.Background(), code, "phone", Origin{}); !errors.Is(err, ErrNoWindow) {
		t.Errorf("refused code reusable: %v", err)
	}
}

func TestPairTimeout(t *testing.T) {
	p, dir := newPairer(t)
	p.Timeout = 50 * time.Millisecond
	code, w, _ := Open(dir, time.Now())
	if _, _, err := p.Pair(context.Background(), code, "phone", Origin{}); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
	if WindowOpen(dir, w.ID) {
		t.Error("window still open after timeout")
	}
}

func TestBadCodesCloseWindow(t *testing.T) {
	p, dir := newPairer(t)
	code, w, _ := Open(dir, time.Now())
	for i := 0; i < MaxBadCodes; i++ {
		if _, _, err := p.Pair(context.Background(), "AAAA", "phone", Origin{}); !errors.Is(err, ErrBadCode) {
			t.Fatalf("try %d: %v", i, err)
		}
	}
	if WindowOpen(dir, w.ID) {
		t.Fatal("window open after too many wrong codes")
	}
	if _, _, err := p.Pair(context.Background(), code, "phone", Origin{}); !errors.Is(err, ErrNoWindow) {
		t.Errorf("right code after lockout: %v", err)
	}
}

func TestExpired(t *testing.T) {
	p, dir := newPairer(t)
	start := time.Now()
	code, _, _ := Open(dir, start)
	p.Now = func() time.Time { return start.Add(WindowTTL) }
	if _, _, err := p.Pair(context.Background(), code, "phone", Origin{}); !errors.Is(err, ErrNoWindow) {
		t.Errorf("expired code: %v", err)
	}
}

func TestNoWindowAndBadName(t *testing.T) {
	p, dir := newPairer(t)
	if _, _, err := p.Pair(context.Background(), "X", "phone", Origin{}); !errors.Is(err, ErrNoWindow) {
		t.Errorf("no window: %v", err)
	}
	code, _, _ := Open(dir, time.Now())
	for _, name := range []string{"", "   ", strings.Repeat("a", 65), "bad\nname", "bell\x07"} {
		if _, _, err := p.Pair(context.Background(), code, name, Origin{}); !errors.Is(err, ErrBadName) {
			t.Errorf("name %q: %v", name, err)
		}
	}
}

func TestBusy(t *testing.T) {
	p, dir := newPairer(t)
	code, w, _ := Open(dir, time.Now())
	done := make(chan error, 1)
	go func() {
		_, _, err := p.Pair(context.Background(), code, "first", Origin{})
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := PendingRequest(dir, w.ID); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first request never arrived")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, _, err := p.Pair(context.Background(), code, "second", Origin{}); !errors.Is(err, ErrBusy) {
		t.Errorf("second request: %v", err)
	}
	Decide(dir, w.ID, true)
	if err := <-done; err != nil {
		t.Errorf("first request: %v", err)
	}
}
