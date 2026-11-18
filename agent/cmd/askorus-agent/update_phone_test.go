package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/api"
	"github.com/heeo-dev/askorus/agent/internal/devices"
	"github.com/heeo-dev/askorus/agent/internal/update"
)

// fakeWork is the API server as an update sees it.
type fakeWork struct {
	mu    sync.Mutex
	holds []bool
	waits int
}

func (w *fakeWork) Busy() api.UpdateBusy { return api.UpdateBusy{} }
func (w *fakeWork) Hold(on bool)         { w.mu.Lock(); w.holds = append(w.holds, on); w.mu.Unlock() }
func (w *fakeWork) WaitIdle(context.Context) error {
	w.mu.Lock()
	w.waits++
	w.mu.Unlock()
	return nil
}

func (w *fakeWork) held() []bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]bool(nil), w.holds...)
}

type phoneEnv struct {
	*updatePC
	u     *phoneUpdater
	store *devices.Store
	phone string
	ended chan struct{}
}

func newPhoneEnv(t *testing.T) phoneEnv {
	p := newUpdatePC(t)
	store, err := devices.Open(p.dir)
	if err != nil {
		t.Fatal(err)
	}
	_, d, _ := store.Add("Galaxy S23")
	ended := make(chan struct{}, 1)
	u := newPhoneUpdater(p.dir, store, func() { ended <- struct{}{} })
	return phoneEnv{updatePC: p, u: u, store: store, phone: d.ID, ended: ended}
}

func TestPhoneUpdaterInfo(t *testing.T) {
	e := newPhoneEnv(t)
	if i := e.u.Info(e.phone); !i.On || !i.Can || i.Why != "" || !i.Allowed {
		t.Fatalf("a paired phone: %+v", i)
	}
	if i := e.u.Info("someone else"); i.Allowed {
		t.Fatal("a device that is not paired may update")
	}
	e.store.SetUpdate(e.phone, false)
	if i := e.u.Info(e.phone); i.Allowed {
		t.Fatal("a phone turned off may update")
	}
	e.store.SetUpdate(e.phone, true)

	whys := []struct {
		name   string
		change func()
		why    string
	}{
		{"a branch build", func() { version = "0.5.0-dev+abc" }, "not_release"},
		{"another folder", func() { updateExe = func() (string, error) { return filepath.Join(t.TempDir(), "x.exe"), nil } }, "not_installed"},
		{"no root key built in", func() { updateRoots = func() ([]update.PublicKey, error) { return nil, nil } }, "no_key"},
		{"updates off", func() { e.run("off") }, "off"},
	}
	for _, c := range whys {
		savedV, savedExe, savedRoots := version, updateExe, updateRoots
		c.change()
		if i := e.u.Info(e.phone); i.Can || i.Why != c.why {
			t.Errorf("%s: %+v", c.name, i)
		}
		version, updateExe, updateRoots = savedV, savedExe, savedRoots
		e.run("on")
	}
}

func TestPhoneUpdaterStatus(t *testing.T) {
	e := newPhoneEnv(t)
	st := e.u.Status(context.Background(), e.phone)
	if st.Current != "0.5.0" || st.Latest != "0.5.1" || !st.Newer || st.CheckError != "" || st.Checked.IsZero() {
		t.Fatalf("status: %+v", st)
	}
	update.WriteProgress(e.dir, update.Progress{From: "0.4.0", To: "0.5.0", State: update.ProgressDone, At: time.Now()})
	if st := e.u.Status(context.Background(), e.phone); st.Last == nil || st.Last.State != "done" || st.Last.To != "0.5.0" {
		t.Fatalf("last: %+v", st.Last)
	}
	// Off: GitHub is asked nothing, and nothing new is known.
	e.run("off")
	os.Remove(filepath.Join(e.dir, "update.json"))
	if st := e.u.Status(context.Background(), e.phone); st.On || st.Latest != "" || st.CheckError != "" {
		t.Fatalf("off: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "update.json")); err == nil {
		t.Fatal("a status while off asked GitHub")
	}
	e.run("on")
	// A release without signed information says so (a new agent: this one
	// remembers its last ask for a minute, R-133).
	delete(e.files, update.ReleaseFile)
	os.Remove(filepath.Join(e.dir, "update.json"))
	e.u.checker = updateChecker(e.dir)
	if st := e.u.Status(context.Background(), e.phone); st.CheckError != "no_release_info" {
		t.Fatalf("no release.json: %+v", st)
	}
}

func TestPhoneUpdaterStartRefuses(t *testing.T) {
	e := newPhoneEnv(t)
	w := &fakeWork{}
	if err := e.u.Start(e.phone, "0.5.2", w); !errors.Is(err, api.ErrUpdateChanged) {
		t.Errorf("another version than the latest: %v", err)
	}
	if err := e.u.Start("someone else", "0.5.1", w); !errors.Is(err, api.ErrUpdateNotAllowed) {
		t.Errorf("a device that is not paired: %v", err)
	}
	e.store.SetUpdate(e.phone, false)
	if err := e.u.Start(e.phone, "0.5.1", w); !errors.Is(err, api.ErrUpdateNotAllowed) {
		t.Errorf("a phone turned off: %v", err)
	}
	e.store.SetUpdate(e.phone, true)
	e.run("off")
	if err := e.u.Start(e.phone, "0.5.1", w); !errors.Is(err, api.ErrUpdateOff) {
		t.Errorf("updates off: %v", err)
	}
	e.run("on")
	if len(w.held()) != 0 || len(e.launches) != 0 || e.program() != "agent 0.5.0" {
		t.Fatalf("a refused start changed something: %v %q %q", w.held(), e.launches, e.program())
	}
}

// The phone's go: new work is held, the work that runs is waited for, the
// new program is started to take over from this agent, and this agent ends.
func TestPhoneUpdaterHandsOver(t *testing.T) {
	e := newPhoneEnv(t)
	w := &fakeWork{}
	if err := e.u.Start(e.phone, "0.5.1", w); err != nil {
		t.Fatal(err)
	}
	if err := e.u.Start(e.phone, "0.5.1", w); !errors.Is(err, api.ErrUpdateRunning) {
		t.Fatalf("a second start: %v", err)
	}
	select {
	case <-e.ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the agent was not told to end")
	}
	<-e.done
	if h := w.held(); len(h) != 1 || !h[0] || w.waits != 1 {
		t.Fatalf("holds %v, waits %d", h, w.waits)
	}
	if !strings.HasPrefix(e.launches[0], updateFinishArg+" ") || !strings.HasSuffix(e.launches[0], " 0.5.0 0.5.1 1") {
		t.Fatalf("launches: %q", e.launches)
	}
	if e.program() != "agent 0.5.1" {
		t.Fatalf("the program is %q", e.program())
	}
	if pr, _ := update.ReadProgress(e.dir); pr.State != update.ProgressDone {
		t.Fatalf("progress: %+v", pr)
	}
}

// When the new program cannot be taken, nothing changes and the agent
// takes work again.
func TestPhoneUpdaterGoesOnWhenNothingWasTaken(t *testing.T) {
	e := newPhoneEnv(t)
	update.Says = func(string) (string, error) { return "askorus-agent 0.4.9", nil }
	w := &fakeWork{}
	if err := e.u.Start(e.phone, "0.5.1", w); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if h := w.held(); len(h) == 2 {
			if !h[0] || h[1] {
				t.Fatalf("holds %v", h)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the hold was never let go")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if pr, _ := update.ReadProgress(e.dir); pr.State != update.ProgressNotTaken || !strings.Contains(pr.Reason, "says") {
		t.Fatalf("progress: %+v", pr)
	}
	select {
	case <-e.ended:
		t.Fatal("the agent ended although nothing was taken")
	default:
	}
	if e.program() != "agent 0.5.0" || len(e.launches) != 0 {
		t.Fatalf("something changed: %q %q", e.program(), e.launches)
	}
	// It can be tried again.
	update.Says = func(path string) (string, error) {
		b, _ := os.ReadFile(path)
		return "askorus-agent " + strings.TrimPrefix(string(b), "agent "), nil
	}
	if err := e.u.Start(e.phone, "0.5.1", w); err != nil {
		t.Fatalf("again: %v", err)
	}
	<-e.ended
	<-e.done
}

// An update whose end was never written (the PC went off) is read from the
// version that runs when the agent starts again.
func TestSettleProgress(t *testing.T) {
	e := newPhoneEnv(t)
	old := time.Now().Add(-10 * time.Minute)
	for _, c := range []struct{ running, want string }{{"0.5.1", update.ProgressDone}, {"0.5.0", update.ProgressRolledBack}, {"0.4.0", update.ProgressSwapped}} {
		update.WriteProgress(e.dir, update.Progress{From: "0.5.0", To: "0.5.1", State: update.ProgressSwapped, At: old})
		version = c.running
		settleProgress(e.dir)
		if pr, _ := update.ReadProgress(e.dir); pr.State != c.want {
			t.Errorf("running %s: %+v", c.running, pr)
		}
	}
	// One that is under way now is left alone.
	update.WriteProgress(e.dir, update.Progress{From: "0.5.0", To: "0.5.1", State: update.ProgressSwapped, At: time.Now()})
	version = "0.5.1"
	settleProgress(e.dir)
	if pr, _ := update.ReadProgress(e.dir); pr.State != update.ProgressSwapped {
		t.Errorf("under way: %+v", pr)
	}
}

func TestDevicesUpdateCommand(t *testing.T) {
	e := newPhoneEnv(t)
	var out bytes.Buffer
	if err := devicesCmd(e.dir, []string{"update", "Galaxy S23", "off"}, &out); err != nil || !strings.Contains(out.String(), "can no longer update") {
		t.Fatalf("off: %q, %v", out.String(), err)
	}
	out.Reset()
	devicesCmd(e.dir, []string{"list"}, &out)
	if !strings.Contains(out.String(), "UPDATE") || !strings.Contains(out.String(), "off") {
		t.Fatalf("list: %q", out.String())
	}
	out.Reset()
	if err := devicesCmd(e.dir, []string{"update", e.phone, "on"}, &out); err != nil || !strings.Contains(out.String(), "can update") {
		t.Fatalf("on: %q, %v", out.String(), err)
	}
	for _, args := range [][]string{{"update", "nobody", "on"}, {"update", e.phone}, {"update", e.phone, "maybe"}} {
		if err := devicesCmd(e.dir, args, &out); err == nil {
			t.Errorf("%q ran", args)
		}
	}
}
