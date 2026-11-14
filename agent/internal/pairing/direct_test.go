package pairing

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

var phone = Origin{Login: "owner@example.com", RemoteIP: "100.64.0.9"}

// A pairing window that is open on the PC is the person's consent: a request
// without a code is taken at once, the window is used up, and the pair
// command reads who came in, with the two digits the phone showed (#58).
func TestDirectIntoAnOpenWindow(t *testing.T) {
	p, dir := newPairer(t)
	asked := atomic.Int32{}
	p.Confirm = func(context.Context, Question) (bool, bool) { asked.Add(1); return false, true }
	_, w, err := Open(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !p.WindowIsOpen() {
		t.Fatal("the window is not seen as open")
	}
	id, err := p.StartDirect(" Galaxy S23 ", "47", phone)
	if err != nil {
		t.Fatal(err)
	}
	st, tok, d := waitFinal(t, p, id)
	if st != StateApproved || tok == "" || d.Name != "Galaxy S23" {
		t.Fatalf("state %s, token %q, device %+v", st, tok, d)
	}
	if asked.Load() != 0 {
		t.Error("the screen was asked although a window was open")
	}
	res, ok := ResultFor(dir, w.ID)
	if !ok || !res.OK || res.DeviceName != "Galaxy S23" || res.Match != "47" || res.DeviceID != d.ID {
		t.Errorf("result for the pair command = %+v %v", res, ok)
	}
	if p.WindowIsOpen() {
		t.Error("the window stayed open after one device came in")
	}
	// The used window lets nobody else in: the next request is asked about.
	if st, _, _ := waitFinal(t, p, mustStart(t, p, "Other", "11")); st != StateDenied {
		t.Errorf("a request after the window was used: %s", st)
	}
	if asked.Load() != 1 {
		t.Errorf("the screen was asked %d times for the second request", asked.Load())
	}
}

func mustStart(t *testing.T, p *Pairer, name, match string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		id, err := p.StartDirect(name, match, phone)
		if err == nil {
			return id
		}
		if !errors.Is(err, ErrBusy) || time.Now().After(deadline) {
			t.Fatalf("StartDirect: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// No window: the PC asks on its screen, with the name, the two digits and
// where the request came from. Yes pairs, no refuses.
func TestDirectAskedOnTheScreen(t *testing.T) {
	for _, allow := range []bool{true, false} {
		p, _ := newPairer(t)
		var got Question
		p.Confirm = func(_ context.Context, q Question) (bool, bool) { got = q; return allow, true }
		id, err := p.StartDirect("Galaxy S23", "47", phone)
		if err != nil {
			t.Fatal(err)
		}
		st, tok, _ := waitFinal(t, p, id)
		if allow && (st != StateApproved || tok == "") || !allow && (st != StateDenied || tok != "") {
			t.Errorf("allow=%v: state %s, token %q", allow, st, tok)
		}
		if got.DeviceName != "Galaxy S23" || got.Match != "47" || got.Login != phone.Login || got.RemoteIP != phone.RemoteIP || got.ID == "" || got.ID == id {
			t.Errorf("question = %+v (request id %s)", got, id)
		}
		if list, _ := p.Devices.List(); len(list) != map[bool]int{true: 1, false: 0}[allow] {
			t.Errorf("allow=%v: %d devices", allow, len(list))
		}
	}
}

// Nobody at the PC: a phone that is already paired sees the request and
// answers it. The question on the screen is taken away then.
func TestDirectAnsweredByAPairedPhone(t *testing.T) {
	p, _ := newPairer(t)
	if _, _, err := p.Devices.Add("Old phone"); err != nil {
		t.Fatal(err)
	}
	gone := make(chan struct{})
	p.Confirm = func(ctx context.Context, _ Question) (bool, bool) {
		<-ctx.Done() // the person never touches the PC
		close(gone)
		return false, true
	}
	id, err := p.StartDirect("New phone", "08", phone)
	if err != nil {
		t.Fatal(err)
	}
	var q Question
	deadline := time.Now().Add(2 * time.Second)
	for {
		if list := p.Pending(); len(list) == 1 {
			q = list[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the request never showed up for the paired phone")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if q.DeviceName != "New phone" || q.Match != "08" || q.ID == id {
		t.Errorf("pending = %+v", q)
	}
	if err := p.Answer("not-the-id", true); !errors.Is(err, ErrUnknownRequest) {
		t.Errorf("an answer to another id = %v", err)
	}
	if err := p.Answer(q.ID, true); err != nil {
		t.Fatal(err)
	}
	st, tok, d := waitFinal(t, p, id)
	if st != StateApproved || tok == "" || d.Name != "New phone" {
		t.Fatalf("state %s, token %q, device %+v", st, tok, d)
	}
	select {
	case <-gone:
	case <-time.After(2 * time.Second):
		t.Error("the question stayed on the screen after the phone answered")
	}
	if len(p.Pending()) != 0 {
		t.Error("the request is still listed")
	}
	if err := p.Answer(q.ID, false); !errors.Is(err, ErrUnknownRequest) {
		t.Errorf("a second answer = %v", err)
	}
}

// A window the person opens while the phone waits lets it in: "run
// askorus-agent pair on the PC" is what the phone says when nobody answers.
func TestDirectLetInByAWindowOpenedLater(t *testing.T) {
	p, dir := newPairer(t)
	if _, _, err := p.Devices.Add("Old phone"); err != nil {
		t.Fatal(err)
	}
	id, err := p.StartDirect("New phone", "31", phone)
	if err != nil {
		t.Fatal(err)
	}
	if st, _, _, _ := p.Status(id); st != StatePending {
		t.Fatalf("state before the window = %s", st)
	}
	_, w, err := Open(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if st, _, _ := waitFinal(t, p, id); st != StateApproved {
		t.Fatalf("state = %s", st)
	}
	if res, ok := ResultFor(dir, w.ID); !ok || !res.OK || res.Match != "31" {
		t.Errorf("result = %+v %v", res, ok)
	}
}

// With no window, no screen to ask on and no paired phone, nobody could
// answer: the phone is told at once instead of waiting two minutes.
func TestDirectWithNobodyToAnswer(t *testing.T) {
	p, _ := newPairer(t)
	if _, err := p.StartDirect("Galaxy S23", "47", phone); !errors.Is(err, ErrNoApprover) {
		t.Errorf("no dialog, no devices: %v", err)
	}
	// A dialog that turns out not to show (no display): the same, a moment later.
	p.Confirm = func(context.Context, Question) (bool, bool) { return false, false }
	id, err := p.StartDirect("Galaxy S23", "47", phone)
	if err != nil {
		t.Fatal(err)
	}
	if st, tok, _ := waitFinal(t, p, id); st != StateUnattended || tok != "" {
		t.Errorf("state %s, token %q", st, tok)
	}
	// Free again for the next request.
	p.Confirm = func(context.Context, Question) (bool, bool) { return true, true }
	if st, _, _ := waitFinal(t, p, mustStart(t, p, "Galaxy S23", "47")); st != StateApproved {
		t.Errorf("after an unattended request: %s", st)
	}
}

// Nobody answers in time: no device, and a dialog that ends because the
// time is up is not a refusal.
func TestDirectTimesOut(t *testing.T) {
	p, _ := newPairer(t)
	p.Timeout = 60 * time.Millisecond
	p.Confirm = func(ctx context.Context, _ Question) (bool, bool) { <-ctx.Done(); return false, true }
	id, err := p.StartDirect("Galaxy S23", "47", phone)
	if err != nil {
		t.Fatal(err)
	}
	if st, tok, _ := waitFinal(t, p, id); st != StateTimeout || tok != "" {
		t.Errorf("state %s, token %q", st, tok)
	}
	if list, _ := p.Devices.List(); len(list) != 0 {
		t.Errorf("%d devices after a timeout", len(list))
	}
}

func TestDirectRefusesBadInput(t *testing.T) {
	p, _ := newPairer(t)
	p.Confirm = func(context.Context, Question) (bool, bool) { return true, true }
	for _, match := range []string{"", "4", "474", "4a", "٤٧", " 7"} {
		if _, err := p.StartDirect("Galaxy S23", match, phone); !errors.Is(err, ErrBadMatch) {
			t.Errorf("match %q: %v", match, err)
		}
	}
	if _, err := p.StartDirect("  ", "47", phone); !errors.Is(err, ErrBadName) {
		t.Errorf("empty name: %v", err)
	}
	// One at a time: a second request while one waits is told to come back.
	p.Confirm = func(ctx context.Context, _ Question) (bool, bool) { <-ctx.Done(); return false, true }
	if _, err := p.StartDirect("First", "10", phone); err != nil {
		t.Fatal(err)
	}
	if _, err := p.StartDirect("Second", "11", phone); !errors.Is(err, ErrBusy) {
		t.Errorf("second request: %v", err)
	}
}
