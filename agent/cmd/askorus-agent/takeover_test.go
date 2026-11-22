package main

import (
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/update"
)

// R-178 (#71, decided 2026-11-22): an old agent that does not stop is
// ended and the update goes on. When it was the service's, the service is
// stopped before the new agent starts.
func TestUpdateEndsAnOldAgentThatDoesNotStop(t *testing.T) {
	p := newUpdatePC(t)
	p.running()
	updateStop = func(int) error { return nil } // it does not stop
	var order []string
	updateInService = func(pid int) bool {
		if !updatePID(pid) {
			t.Error("the service was looked at after the old agent had ended")
		}
		return pid == 111
	}
	updateEndStuck = func(exe string, pid int, service bool) bool {
		order = append(order, "end")
		if exe != p.exe || pid != 111 || !service {
			t.Errorf("endStuck(%q, %d, %v)", exe, pid, service)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.alive[pid], p.up = false, false
		return true
	}
	updateStopService = func() error {
		order = append(order, "service")
		p.mu.Lock()
		defer p.mu.Unlock()
		if len(p.launches) != 1 {
			t.Errorf("the service was stopped after the new agent started: %q", p.launches)
		}
		return nil
	}
	out, err := p.run("now")
	if err != nil {
		t.Fatalf("now: %v\n%s", err, out)
	}
	<-p.done
	if p.program() != "agent 0.5.1" || !slices.Equal(order, []string{"end", "service"}) {
		t.Fatalf("program %q, order %q", p.program(), order)
	}
	if pr, _ := update.ReadProgress(p.dir); pr.State != update.ProgressDone {
		t.Fatalf("progress: %+v", pr)
	}
}

// One that cannot be ended (not the installed serve) stays, and the update
// goes back as before. Its service is left alone.
func TestUpdateGoesBackWhenTheOldAgentCannotBeEnded(t *testing.T) {
	p := newUpdatePC(t)
	p.running()
	updateStop = func(int) error { return nil }
	updateInService = func(int) bool { return true }
	updateEndStuck = func(string, int, bool) bool { return false }
	updateStopService = func() error { t.Error("the service of an agent that goes on was stopped"); return nil }
	if _, err := p.run("now"); err == nil {
		t.Fatal("now took the update")
	}
	<-p.done
	pr, _ := update.ReadProgress(p.dir)
	if p.program() != "agent 0.5.0" || pr.State != update.ProgressRolledBack || !strings.Contains(pr.Reason, "did not stop") {
		t.Fatalf("program %q, progress %+v", p.program(), pr)
	}
	if !p.alive[111] {
		t.Fatal("the old agent was ended")
	}
}

// R-177 (#71): an update-finish that cannot leave the service says nothing,
// so the old agent is not stopped and puts its program back.
func TestUpdateThatCannotLeaveTheServiceChangesNothing(t *testing.T) {
	p := newUpdatePC(t)
	p.running()
	updateLeave = func() error { return errors.New("still in the service") }
	if _, err := p.run("now"); err == nil || !strings.Contains(err.Error(), "did not take over") {
		t.Fatalf("now: %v", err)
	}
	<-p.done
	pr, _ := update.ReadProgress(p.dir)
	if p.program() != "agent 0.5.0" || pr.State != update.ProgressNotTaken {
		t.Fatalf("program %q, progress %+v", p.program(), pr)
	}
	if !p.alive[111] || !p.up || len(p.launches) != 1 {
		t.Fatalf("the old agent was stopped or a serve was started: %q", p.launches)
	}
}

// An agent that stops by itself is never ended, and without a service
// there is none to stop.
func TestUpdateLeavesAnAgentThatStopsAlone(t *testing.T) {
	p := newUpdatePC(t)
	p.running()
	updateEndStuck = func(string, int, bool) bool { t.Error("an agent that stops was ended"); return false }
	updateStopService = func() error { t.Error("a service was stopped where there is none"); return nil }
	if out, err := p.run("now"); err != nil {
		t.Fatalf("now: %v\n%s", err, out)
	}
	<-p.done
}

// R-181: the unit's stop is only queued, so the new agent waits until the
// port's lock is free. Started before that it would say "already runs" and
// the update would go back.
func TestUpdateWaitsUntilTheServiceLetsTheLockGo(t *testing.T) {
	p := newUpdatePC(t)
	p.running()
	updateWaitFree = 2 * time.Second
	updateStop = func(int) error {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.alive[111] = false // ended; the unit's restarted serve holds the lock
		return nil
	}
	updateInService = func(int) bool { return true }
	var freed atomic.Bool
	updateStopService = func() error {
		time.AfterFunc(200*time.Millisecond, func() {
			p.mu.Lock()
			p.up = false
			p.mu.Unlock()
			freed.Store(true)
		})
		return nil
	}
	launch := updateLaunch
	updateLaunch = func(exe string, args []string, dir, log string) (int, error) {
		if args[0] == "serve" && !freed.Load() {
			t.Error("the new agent was started while the lock was held")
		}
		return launch(exe, args, dir, log)
	}
	if out, err := p.run("now"); err != nil {
		t.Fatalf("now: %v\n%s", err, out)
	}
	<-p.done
}

// R-182 (2ho's re-check): while something still holds the port's lock the
// new agent is not started, and the program is not put back under what
// runs: that is the unit's restarted serve, of the new program.
func TestUpdateDoesNotGoBackUnderAnAgentTheServiceRestarted(t *testing.T) {
	p := newUpdatePC(t)
	p.running()
	updateStop = func(int) error {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.alive[111] = false // ended, and the restarted serve holds the lock
		return nil
	}
	updateInService = func(int) bool { return true }
	updateStopService = func() error { return errors.New("no bus") }
	launch := updateLaunch
	updateLaunch = func(exe string, args []string, dir, log string) (int, error) {
		if args[0] == "serve" {
			t.Error("a second agent was started on a held lock")
		}
		return launch(exe, args, dir, log)
	}
	if _, err := p.run("now"); err == nil {
		t.Fatal("now said it worked")
	}
	<-p.done
	pr, _ := update.ReadProgress(p.dir)
	if p.program() != "agent 0.5.1" || pr.State != update.ProgressFailed || !strings.Contains(pr.Reason, "still holds") {
		t.Fatalf("program %q, progress %+v", p.program(), pr)
	}
}
