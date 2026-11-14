package pairing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	w := openHeld(t, dir)
	if !p.WindowIsOpen() {
		t.Fatal("the window is not seen as open")
	}
	id, err := p.StartDirect(" Galaxy S23 ", "47", phone, nil)
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

// openHeld opens a pairing window as a pair command that runs does.
func openHeld(t *testing.T, dir string) Window {
	t.Helper()
	_, w, err := Open(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := Hold(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	return w
}

func mustStart(t *testing.T, p *Pairer, name, match string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		id, err := p.StartDirect(name, match, phone, nil)
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
		id, err := p.StartDirect("Galaxy S23", "47", phone, nil)
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
	id, err := p.StartDirect("New phone", "08", phone, nil)
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
	id, err := p.StartDirect("New phone", "31", phone, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st, _, _, _ := p.Status(id); st != StatePending {
		t.Fatalf("state before the window = %s", st)
	}
	w := openHeld(t, dir)
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
	if _, err := p.StartDirect("Galaxy S23", "47", phone, nil); !errors.Is(err, ErrNoApprover) {
		t.Errorf("no dialog, no devices: %v", err)
	}
	// A dialog that turns out not to show (no display): the same, a moment later.
	p.Confirm = func(context.Context, Question) (bool, bool) { return false, false }
	id, err := p.StartDirect("Galaxy S23", "47", phone, nil)
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
	id, err := p.StartDirect("Galaxy S23", "47", phone, nil)
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
		if _, err := p.StartDirect("Galaxy S23", match, phone, nil); !errors.Is(err, ErrBadMatch) {
			t.Errorf("match %q: %v", match, err)
		}
	}
	if _, err := p.StartDirect("  ", "47", phone, nil); !errors.Is(err, ErrBadName) {
		t.Errorf("empty name: %v", err)
	}
	// One at a time: a second request while one waits is told to come back.
	p.Confirm = func(ctx context.Context, _ Question) (bool, bool) { <-ctx.Done(); return false, true }
	if _, err := p.StartDirect("First", "10", phone, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := p.StartDirect("Second", "11", phone, nil); !errors.Is(err, ErrBusy) {
		t.Errorf("second request: %v", err)
	}
}

// R-109. A window takes a phone without a question only while the pair
// command that opened it runs. A command that was killed, or whose terminal
// was closed, leaves its file behind: that file alone lets nobody in.
func TestDirectNeedsALivePairCommand(t *testing.T) {
	p, dir := newPairer(t)
	asked := atomic.Int32{}
	p.Confirm = func(context.Context, Question) (bool, bool) { asked.Add(1); return false, true }

	// The window file with nothing that says the command runs.
	if _, _, err := Open(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if p.WindowIsOpen() {
		t.Fatal("a window nobody holds counts as open")
	}
	if st, tok, _ := waitFinal(t, p, mustStart(t, p, "Galaxy S23", "47")); st != StateDenied || tok != "" {
		t.Errorf("into a window nobody holds: state %s, token %q", st, tok)
	}
	if asked.Load() != 1 {
		t.Errorf("the screen was asked %d times", asked.Load())
	}

	// The command said so a while ago and then went away.
	if err := Hold(dir, time.Now().Add(-AliveFor-time.Second)); err != nil {
		t.Fatal(err)
	}
	if p.WindowIsOpen() {
		t.Error("a window whose command stopped saying that it runs counts as open")
	}
	// It runs: the window counts.
	if err := Hold(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !p.WindowIsOpen() {
		t.Error("a held window does not count as open")
	}
	if list, _ := p.Devices.List(); len(list) != 0 {
		t.Errorf("%d devices", len(list))
	}
}

// The pair command keeps saying that it runs until it stops, and what it
// leaves behind is gone once the files are closed.
func TestKeepAlive(t *testing.T) {
	p, dir := newPairer(t)
	if _, _, err := Open(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	stop, err := KeepAlive(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !p.WindowIsOpen() {
		t.Error("the window of a running command is not open")
	}
	// Seen from later, with no new sign of life, the window does not count.
	p.Now = func() time.Time { return time.Now().Add(AliveFor + time.Second) }
	if p.WindowIsOpen() {
		t.Error("the window counts long after the last sign of life")
	}
	p.Now = nil
	stop()
	stop() // twice is fine
	Close(dir)
	if _, err := os.Stat(filepath.Join(dir, aliveFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the sign of life stayed after Close: %v", err)
	}
}

// A pair command that ends takes away its own window, not the one another
// command opened in the meantime.
func TestCloseWindowLeavesAnotherWindow(t *testing.T) {
	p, dir := newPairer(t)
	first := openHeld(t, dir)
	second := openHeld(t, dir)
	CloseWindow(dir, first.ID)
	if !WindowOpen(dir, second.ID) || !p.WindowIsOpen() {
		t.Fatal("ending the first command closed the second one's window")
	}
	CloseWindow(dir, second.ID)
	if WindowOpen(dir, second.ID) || p.WindowIsOpen() {
		t.Error("the window stayed after its own command ended")
	}
}

// R-110. A window is good for one request even when its file could not be
// deleted (on Windows a file that is being read at that moment stays): the
// agent remembers that it was used.
func TestAUsedWindowLetsNobodyElseIn(t *testing.T) {
	p, dir := newPairer(t)
	asked := atomic.Int32{}
	p.Confirm = func(context.Context, Question) (bool, bool) { asked.Add(1); return false, true }
	code, w, err := Open(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := Hold(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := waitFinal(t, p, mustStart(t, p, "Galaxy S23", "47")); st != StateApproved {
		t.Fatalf("first request: %s", st)
	}
	// The file is still there, as if deleting it had failed.
	if err := writeJSON(filepath.Join(dir, windowFile), w); err != nil {
		t.Fatal(err)
	}
	if err := Hold(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if p.WindowIsOpen() {
		t.Error("the used window counts as open again")
	}
	if st, tok, _ := waitFinal(t, p, mustStart(t, p, "Other", "11")); st != StateDenied || tok != "" {
		t.Errorf("second request into the same window: state %s, token %q", st, tok)
	}
	if asked.Load() != 1 {
		t.Errorf("the screen was asked %d times for the second request", asked.Load())
	}
	// Nor does its code work any more.
	if _, err := p.Start(code, "Third", phone); !errors.Is(err, ErrNoWindow) {
		t.Errorf("the code of the used window: %v", err)
	}
	if list, _ := p.Devices.List(); len(list) != 1 {
		t.Errorf("%d devices", len(list))
	}
}

// The same for a window used with its code: once the person answered, a
// window file that stayed lets no second device in, with or without a code.
func TestAWindowUsedWithItsCodeIsUsedUp(t *testing.T) {
	p, dir := newPairer(t)
	code, w, err := Open(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	id, err := p.Start(code, "Galaxy S23", phone)
	if err != nil {
		t.Fatal(err)
	}
	<-answer(t, dir, w.ID, true)
	if st, _, _ := waitFinal(t, p, id); st != StateApproved {
		t.Fatalf("state %s", st)
	}
	if err := writeJSON(filepath.Join(dir, windowFile), w); err != nil {
		t.Fatal(err)
	}
	if err := Hold(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if p.WindowIsOpen() {
		t.Error("the used window counts as open")
	}
	if _, err := p.Start(code, "Other", phone); !errors.Is(err, ErrNoWindow) {
		t.Errorf("the code a second time: %v", err)
	}
}

// R-112. The first answer settles the request: a refusal is not undone by a
// window that opens a moment later, the window stays for the phone it was
// opened for, and a later answer is an error.
func TestTheFirstAnswerSettlesIt(t *testing.T) {
	p, dir := newPairer(t)
	p.Poll = time.Hour // nothing looks for a window by itself during this test
	if _, _, err := p.Devices.Add("Old phone"); err != nil {
		t.Fatal(err)
	}
	ended := make(chan State, 2)
	id, err := p.StartDirect("New phone", "31", phone, func(st State) { ended <- st })
	if err != nil {
		t.Fatal(err)
	}
	q := p.Pending()[0]
	if err := p.Answer(q.ID, false); err != nil {
		t.Fatal(err)
	}
	openHeld(t, dir) // too late
	if err := p.Answer(q.ID, true); !errors.Is(err, ErrUnknownRequest) {
		t.Errorf("an answer after the first one = %v", err)
	}
	if st, tok, _ := waitFinal(t, p, id); st != StateDenied || tok != "" {
		t.Errorf("state %s, token %q", st, tok)
	}
	if st := <-ended; st != StateDenied {
		t.Errorf("ended with %s", st)
	}
	if !p.WindowIsOpen() {
		t.Error("the refused request used up the window")
	}
	if list, _ := p.Devices.List(); len(list) != 1 {
		t.Errorf("%d devices", len(list))
	}

	// settle itself: once.
	d := &direct{done: make(chan struct{}), state: StatePending}
	if !d.settle(StateDenied, "") || d.settle(StateApproved, "w1") || d.settle(StateTimeout, "") {
		t.Error("a request was settled more than once")
	}
	if st, w := d.outcome(); st != StateDenied || w != "" {
		t.Errorf("outcome %s %q", st, w)
	}

	// An answer that comes when the time is up is too late, even when it
	// gets here before the timeout does: no device, no window used.
	for _, st := range []State{StateApproved, StateDenied} {
		late := &direct{done: make(chan struct{}), state: StatePending, deadline: time.Now().Add(-time.Millisecond)}
		if late.settle(st, "w1") {
			t.Errorf("a late %s was taken", st)
		}
		if got, w := late.outcome(); got != StateTimeout || w != "" {
			t.Errorf("a late %s: outcome %s %q", st, got, w)
		}
	}
	early := &direct{done: make(chan struct{}), state: StatePending, deadline: time.Now().Add(time.Minute)}
	if !early.settle(StateApproved, "w1") {
		t.Error("an answer in time was not taken")
	}
}

// A used window stays used when the agent is started again: the record is
// on disk before any token exists (R-110).
func TestAUsedWindowStaysUsedAfterARestart(t *testing.T) {
	p, dir := newPairer(t)
	p.Confirm = func(context.Context, Question) (bool, bool) { return false, true }
	code, w, err := Open(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := Hold(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := waitFinal(t, p, mustStart(t, p, "Galaxy S23", "47")); st != StateApproved {
		t.Fatalf("first request: %s", st)
	}
	// The window file stayed, its pair command still runs, and the agent starts again.
	if err := writeJSON(filepath.Join(dir, windowFile), w); err != nil {
		t.Fatal(err)
	}
	if err := Hold(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	again := &Pairer{Dir: dir, Devices: p.Devices, Poll: 10 * time.Millisecond, Timeout: 2 * time.Second}
	again.Confirm = func(context.Context, Question) (bool, bool) { return false, true }
	if again.WindowIsOpen() {
		t.Error("after a restart the used window counts as open")
	}
	if st, tok, _ := waitFinal(t, again, mustStart(t, again, "Other", "11")); st != StateDenied || tok != "" {
		t.Errorf("after a restart, into the used window: state %s, token %q", st, tok)
	}
	if _, err := again.Start(code, "Third", phone); !errors.Is(err, ErrNoWindow) {
		t.Errorf("after a restart, the code of the used window: %v", err)
	}
	// A pair command that ends removes its files, not the record.
	Close(dir)
	if _, err := os.Stat(filepath.Join(dir, usedFile)); err != nil {
		t.Errorf("the record of used windows went with Close: %v", err)
	}
	// A new window is a new window.
	openHeld(t, dir)
	if !again.WindowIsOpen() {
		t.Error("a new window does not count as open")
	}
}

// A sign of life that does not move is an old one, whatever the PC's clock
// says: a clock set back onto the time of a killed pair command's last sign
// must not open its window again (R-109).
func TestAnOldSignOfLifeStaysOldWhenTheClockIsSetBack(t *testing.T) {
	p, dir := newPairer(t)
	openHeld(t, dir)
	if !p.WindowIsOpen() {
		t.Fatal("the held window is not open")
	}
	// The pair command is killed. Later the PC's clock is set back, so by
	// the clock the last sign looks fresh. By the agent's own time it is old.
	passed := time.Duration(0)
	old := sinceSeen
	sinceSeen = func(t time.Time) time.Duration { return time.Since(t) + passed }
	defer func() { sinceSeen = old }()
	passed = AliveFor + time.Second
	if p.WindowIsOpen() {
		t.Error("an old sign of life opened the window because the clock agrees with it")
	}
	// A pair command that runs moves its sign: the window counts again.
	passed = 0
	if err := Hold(dir, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if !p.WindowIsOpen() {
		t.Error("a sign of life that moved does not count")
	}
}

// The outcome is told when the request ends, also for a phone that never
// asks how it went (R-111 counts on this).
func TestEndedIsCalledWithoutAStatusCall(t *testing.T) {
	p, _ := newPairer(t)
	p.Timeout = 60 * time.Millisecond
	p.Confirm = func(ctx context.Context, _ Question) (bool, bool) { <-ctx.Done(); return false, true }
	ended := make(chan State, 2)
	if _, err := p.StartDirect("Galaxy S23", "47", phone, func(st State) { ended <- st }); err != nil {
		t.Fatal(err)
	}
	select {
	case st := <-ended:
		if st != StateTimeout {
			t.Errorf("ended with %s", st)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the end was never told")
	}
	select {
	case st := <-ended:
		t.Errorf("told twice: %s", st)
	case <-time.After(50 * time.Millisecond):
	}
}
