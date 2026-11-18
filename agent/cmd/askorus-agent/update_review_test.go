package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/api"
	"github.com/heeo-dev/askorus/agent/internal/update"
)

// gatedWork waits in WaitIdle until the test lets it go.
type gatedWork struct {
	fakeWork
	waiting chan struct{}
	gate    chan struct{}
}

func (w *gatedWork) WaitIdle(ctx context.Context) error {
	close(w.waiting)
	<-w.gate
	return nil
}

func (p *updatePC) gitHubHits() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hits
}

func waitHolds(t *testing.T, w *fakeWork, n int) []bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if h := w.held(); len(h) >= n {
			return h
		}
		if time.Now().After(deadline) {
			t.Fatalf("holds: %v", w.held())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// R-132 (#35 review): what Start checked is checked again after the wait:
// a phone turned off (or disconnected, or updates turned off) meanwhile
// takes nothing.
func TestPhoneUpdateChecksAgainAfterTheWait(t *testing.T) {
	for _, change := range []func(e phoneEnv){
		func(e phoneEnv) { e.store.SetUpdate(e.phone, false) },
		func(e phoneEnv) { e.store.Revoke(e.phone) },
		func(e phoneEnv) { e.run("off") },
	} {
		e := newPhoneEnv(t)
		w := &gatedWork{waiting: make(chan struct{}), gate: make(chan struct{})}
		if err := e.u.Start(e.phone, "0.5.1", w); err != nil {
			t.Fatal(err)
		}
		<-w.waiting
		change(e)
		close(w.gate)
		h := waitHolds(t, &w.fakeWork, 2)
		if !h[0] || h[1] {
			t.Fatalf("holds %v", h)
		}
		if pr, _ := update.ReadProgress(e.dir); pr.State != update.ProgressNotTaken || !strings.Contains(pr.Reason, "while the agent waited") {
			t.Fatalf("progress: %+v", pr)
		}
		if e.program() != "agent 0.5.0" || len(e.launches) != 0 {
			t.Fatalf("something changed: %q %q", e.program(), e.launches)
		}
	}
}

// R-133 (#35 review): a version the remembered check does not name is
// refused without asking GitHub, and a version that did not come up an
// hour ago is not tried again yet.
func TestPhoneUpdateDoesNotAskOrRestartOverAndOver(t *testing.T) {
	e := newPhoneEnv(t)
	e.u.Status(context.Background(), e.phone) // the phone saw 0.5.1
	hits := e.gitHubHits()
	for range 3 {
		if err := e.u.Start(e.phone, "9.9.9", &fakeWork{}); !errors.Is(err, api.ErrUpdateChanged) {
			t.Fatalf("a version never seen: %v", err)
		}
	}
	if e.gitHubHits() != hits {
		t.Fatalf("refusing an unseen version asked GitHub %d times", e.gitHubHits()-hits)
	}

	update.WriteProgress(e.dir, update.Progress{From: "0.5.0", To: "0.5.1", State: update.ProgressRolledBack, Reason: "r", At: time.Now().Add(-10 * time.Minute)})
	if err := e.u.Start(e.phone, "0.5.1", &fakeWork{}); !errors.Is(err, api.ErrUpdateFailedRecently) {
		t.Fatalf("a version that did not come up 10 minutes ago: %v", err)
	}
	update.WriteProgress(e.dir, update.Progress{From: "0.5.0", To: "0.5.1", State: update.ProgressRolledBack, Reason: "r", At: time.Now().Add(-2 * time.Hour)})
	if err := e.u.Start(e.phone, "0.5.1", &fakeWork{}); err != nil {
		t.Fatalf("two hours later: %v", err)
	}
	<-e.ended
	<-e.done
}

// R-134 (#35 review): an agent that started but never listens on its
// Tailscale node is not taken for up: the old one goes back.
func TestUpdateNowNeedsTheNewAgentToListen(t *testing.T) {
	p := newUpdatePC(t)
	p.running()
	p.neverReady = true
	out, err := p.run("now")
	<-p.done
	if err == nil {
		t.Fatalf("now passed with an agent that never listened:\n%s", out)
	}
	if p.program() != "agent 0.5.0" {
		t.Fatalf("the program is %q", p.program())
	}
	if pr, _ := update.ReadProgress(p.dir); pr.State != update.ProgressFailed && pr.State != update.ProgressRolledBack {
		t.Fatalf("progress: %+v", pr)
	}
}

// R-135 (#35 review): when the new program never says it takes over, the
// old agent is not stopped and the old program goes back.
func TestUpdateNowKeepsTheOldAgentWhenTheNewProgramIsGone(t *testing.T) {
	p := newUpdatePC(t)
	p.running()
	p.helperDies = true
	_, err := p.run("now")
	if err == nil || !strings.Contains(err.Error(), "did not take over") {
		t.Fatalf("now: %v", err)
	}
	if !p.alive[111] || !p.up {
		t.Fatal("the old agent was stopped although nothing took over")
	}
	if p.program() != "agent 0.5.0" {
		t.Fatalf("the program is %q", p.program())
	}
	if len(p.killed) != 1 {
		t.Fatalf("the new program was not ended: %v", p.killed)
	}
	if pr, _ := update.ReadProgress(p.dir); pr.State != update.ProgressNotTaken {
		t.Fatalf("progress: %+v", pr)
	}
	if _, err := os.Stat(filepath.Join(p.dir, "update.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the lock was left behind")
	}
}

// R-136 (#35 review): an update that another live process runs is not
// joined.
func TestUpdateNowWaitsForAnotherUpdate(t *testing.T) {
	p := newUpdatePC(t)
	p.alive[4242] = true
	os.WriteFile(filepath.Join(p.dir, "update.lock"), []byte("4242\n"), 0o600)
	if _, err := p.run("now"); err == nil || !strings.Contains(err.Error(), "another update") {
		t.Fatalf("now with another update under way: %v", err)
	}
	if p.program() != "agent 0.5.0" {
		t.Fatalf("the program is %q", p.program())
	}
	if _, err := os.Stat(update.Sibling(p.exe, runtime.GOOS, "new")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("it downloaded while another update ran")
	}
}
