package main

// The phone's update button (#35, S4): the running agent's api.Updater.
// It runs the same steps as `askorus-agent update now`, from inside the
// agent: it holds new work, waits up to ten minutes for questions still
// being answered, swaps the program in and starts it as update-finish, then
// ends itself so the new agent can take over.

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/api"
	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/devices"
	"github.com/heeo-dev/askorus/agent/internal/update"
)

// updateWorkWait is how long the work that runs is waited for (part 27).
var updateWorkWait = 10 * time.Minute

type phoneUpdater struct {
	dir     string
	devices *devices.Store
	checker *update.Checker
	// end ends this agent once the new program is started.
	end func()

	mu      sync.Mutex
	running bool
}

func newPhoneUpdater(dir string, store *devices.Store, end func()) *phoneUpdater {
	return &phoneUpdater{dir: dir, devices: store, checker: updateChecker(dir), end: end}
}

// why is the code for why this agent cannot update itself ("" when it can).
func updateWhy(cfg config.Config) string {
	if !cfg.UpdatesOn() {
		return "off"
	}
	if !update.IsRelease(version) {
		return "not_release"
	}
	if _, err := selfUpdatable(cfg); err != nil {
		return "not_installed"
	}
	if roots, err := updateRoots(); err != nil || len(roots) == 0 {
		return "no_key"
	}
	return ""
}

func (u *phoneUpdater) Info(device string) api.UpdateInfo {
	cfg, err := config.Load(u.dir)
	if err != nil {
		return api.UpdateInfo{Why: "off"}
	}
	i := api.UpdateInfo{On: cfg.UpdatesOn(), Why: updateWhy(cfg)}
	i.Can = i.Why == ""
	d, ok := u.devices.Get(device)
	i.Allowed = ok && !d.NoUpdate
	return i
}

func (u *phoneUpdater) Status(ctx context.Context, device string) api.UpdateStatus {
	st := api.UpdateStatus{UpdateInfo: u.Info(device), Current: version}
	u.mu.Lock()
	st.Running = u.running
	u.mu.Unlock()
	if p, ok := update.ReadProgress(u.dir); ok {
		st.Last = &api.UpdateLast{From: p.From, To: p.To, State: p.State, Reason: p.Reason, At: p.At}
	}
	if !st.On {
		return st // off: GitHub is asked nothing
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	f, err := u.checker.Check(ctx, false)
	if err != nil {
		st.CheckError = checkErrorCode(err)
		return st
	}
	st.Latest, st.Newer, st.Checked = f.Latest, f.Newer, f.Checked
	return st
}

func checkErrorCode(err error) string {
	switch {
	case errors.Is(err, update.ErrNoRoot):
		return "no_key"
	case errors.Is(err, update.ErrNoReleaseInfo):
		return "no_release_info"
	case errors.Is(err, update.ErrExpired):
		return "expired"
	case errors.Is(err, update.ErrBadSignature), errors.Is(err, update.ErrOldKey), errors.Is(err, update.ErrBadFiles):
		return "bad_release"
	}
	return "unreachable"
}

func (u *phoneUpdater) Start(device, want string, w api.Work) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.running {
		return api.ErrUpdateRunning
	}
	info := u.Info(device)
	if !info.Can {
		return api.ErrUpdateOff
	}
	if !info.Allowed {
		return api.ErrUpdateNotAllowed
	}
	cfg, err := config.Load(u.dir)
	if err != nil {
		return err
	}
	exe, err := selfUpdatable(cfg)
	if err != nil {
		return api.ErrUpdateOff
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	f, err := u.checker.Check(ctx, true)
	if err != nil {
		return err
	}
	if !f.Newer || f.Latest != want {
		return api.ErrUpdateChanged
	}
	u.running = true
	go u.run(cfg, exe, f, w)
	return nil
}

// run is the update after the phone's go: hold, wait, hand over, end.
func (u *phoneUpdater) run(cfg config.Config, exe string, f update.Found, w api.Work) {
	w.Hold(true)
	ctx, cancel := context.WithTimeout(context.Background(), updateWorkWait)
	_ = w.WaitIdle(ctx) // after ten minutes what still runs is cut off, as the phone said
	cancel()
	if err := handOver(u.dir, cfg, exe, f, os.Getpid(), true); err != nil {
		// Nothing was changed (or it was put back): work goes on.
		if p, ok := update.ReadProgress(u.dir); !ok || p.To != f.Latest || p.State == update.ProgressSwapped {
			_ = update.WriteProgress(u.dir, update.Progress{From: version, To: f.Latest, State: update.ProgressNotTaken, Reason: err.Error(), At: updateNow()})
		}
		w.Hold(false)
		u.mu.Lock()
		u.running = false
		u.mu.Unlock()
		return
	}
	u.end() // the new program waits for this agent to end
}

// settleProgress tidies the last update's record when an agent starts: an
// update-finish that never wrote its end (the PC went off) is read from
// which version is running now.
func settleProgress(dir string) {
	p, ok := update.ReadProgress(dir)
	if !ok || p.State != update.ProgressSwapped || updateNow().Sub(p.At) < updateWaitOld+updateWaitNew+updateSettle {
		return
	}
	switch version {
	case p.To:
		p.State, p.Reason = update.ProgressDone, "seen when the agent started again"
	case p.From:
		p.State, p.Reason = update.ProgressRolledBack, "the old agent started again"
	default:
		return
	}
	p.At = updateNow()
	_ = update.WriteProgress(dir, p)
}
