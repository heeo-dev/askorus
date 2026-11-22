package main

// askorus-agent update (#35, Phase 1 plan part 27): look for a newer signed
// release and take it. The steps:
//
//  1. check: fetch the signed release information (internal/update) and
//     find a newer version with an agent for this system
//  2. stage: download it next to this program, check its size and
//     SHA-256, and that it says its version
//  3. swap: put it in place, keeping this one as askorus-agent.prev
//  4. hand over: start the new program as `update-finish`, then stop the
//     running agent. The new program first leaves the agent's systemd
//     service (#71). It waits for the old one to end, and ends one that
//     does not within a minute (macOS, Linux). It starts the agent and
//     checks it comes up within a minute; when it does not, it puts the old
//     program back and starts that instead.
//
// Only a release build at the install path updates itself, and only while
// updates are on (on by default; askorus-agent update off). The phone's
// button (S4) runs the same steps from the running agent.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/update"
)

// updateFinishArg is the hidden command the new program runs to take over
// (step 4). Not a command for people, so it is not in the usage.
const updateFinishArg = "update-finish"

// Seams for tests.
var (
	updateSource = func() update.Source { return update.NewSource(version) }
	updateRoots  = update.Roots
	updateNow    = time.Now
	updateExe    = os.Executable
	updateHome   = os.UserHomeDir
	updateLaunch = startDetached
	updatePID    = processRunning
	updateUp     = func(port int) bool { return agentRunning(port) }
	updateStop   = signalStop

	updateLeave       = leaveService // out of the agent's service (R-177)
	updateInService   = inService
	updateStopService = stopService
	updateEndStuck    = endStuck // an old agent that does not stop (R-178)
	updateWaitFree    = 10 * time.Second
	// updateWaitTakeover bounds what update-finish may add for both.
	updateWaitTakeover = 25 * time.Second
	// updateReady: the agent started as pid is listening on its Tailscale
	// node; serve writes its pid file only then (R-134). The stop event or
	// lock that updateUp sees comes before Tailscale starts.
	updateReady = func(dir string, pid int) bool { return readPID(dir) == pid }

	updateWaitOld   = 60 * time.Second // for the old agent to end
	updateWaitNew   = 60 * time.Second // for the new one to come up (part 27)
	updateSettle    = 5 * time.Second  // and stay up
	updateHandshake = 15 * time.Second // for the new program to say it takes over (R-135)
	updatePoll      = 200 * time.Millisecond
)

func updateCmd(dir string, args []string, out io.Writer) error {
	sub := "status"
	if len(args) == 1 {
		sub = args[0]
	}
	if len(args) > 1 || (sub != "status" && sub != "check" && sub != "now" && sub != "on" && sub != "off") {
		return fail("usage: askorus-agent update status | check | now | on | off", "사용법: askorus-agent update status | check | now | on | off")
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	switch sub {
	case "on", "off":
		on := sub == "on"
		cfg.Updates = &on
		if err := config.Save(dir, cfg); err != nil {
			return err
		}
		if on {
			say(out, m("updates are on: when a phone asks, the agent asks GitHub for a newer signed release", "업데이트를 켰습니다. 폰이 물으면 에이전트가 GitHub에 서명된 새 버전을 묻습니다"))
		} else {
			say(out, m("updates are off: the agent asks GitHub for nothing, and phones cannot update it", "업데이트를 껐습니다. 에이전트는 GitHub에 아무것도 묻지 않고, 폰에서 업데이트할 수 없습니다"))
		}
		return nil
	case "status":
		return updateStatus(dir, cfg, out)
	case "check":
		if !cfg.UpdatesOn() {
			return fail("updates are off (askorus-agent update on)", "업데이트가 꺼져 있습니다 (askorus-agent update on)")
		}
		f, err := updateChecker(dir).Check(context.Background(), true)
		if err != nil {
			return updateFailed(err)
		}
		sayFound(out, f)
		return nil
	}
	return updateNowCmd(dir, cfg, out)
}

func updateChecker(dir string) *update.Checker {
	return &update.Checker{Dir: dir, Current: version, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Source: updateSource(), Roots: updateRoots, Now: updateNow}
}

// selfUpdatable says why this agent cannot update itself; nil when it can.
func selfUpdatable(cfg config.Config) (string, error) {
	exe, err := updateExe()
	if err != nil {
		return "", err
	}
	if !cfg.UpdatesOn() {
		return exe, fail("updates are off (askorus-agent update on)", "업데이트가 꺼져 있습니다 (askorus-agent update on)")
	}
	if !update.IsRelease(version) {
		return exe, fail("this agent is not a release build (%s), so it does not update itself", "이 에이전트는 릴리스 버전이 아니라(%s) 스스로 업데이트하지 않습니다", version)
	}
	home, _ := updateHome()
	want := update.InstallPath(runtime.GOOS, os.Getenv, home)
	if !update.SamePath(runtime.GOOS, exe, want) {
		return exe, fail("this agent is not where the install script puts it (%s), so it does not update itself; run the install line again",
			"이 에이전트는 설치 스크립트가 두는 곳(%s)에 있지 않아 스스로 업데이트하지 않습니다. 설치 한 줄을 다시 실행하세요", want)
	}
	return exe, nil
}

func updateStatus(dir string, cfg config.Config, out io.Writer) error {
	if cfg.UpdatesOn() {
		say(out, m("updates: on (the agent asks GitHub only when a phone asks; askorus-agent update off)", "업데이트: 켜짐 (폰이 물을 때만 에이전트가 GitHub에 묻습니다. 끄려면 askorus-agent update off)"))
	} else {
		say(out, m("updates: off (askorus-agent update on)", "업데이트: 꺼짐 (askorus-agent update on)"))
	}
	say(out, m("this agent: %s", "이 에이전트: %s", version))
	var why biError
	if _, err := selfUpdatable(cfg); cfg.UpdatesOn() && errors.As(err, &why) {
		say(out, why.msg)
	}
	if st, err := update.LoadState(dir); err != nil {
		say(out, msg{en: err.Error(), ko: "업데이트 상태 파일(update.json)을 읽을 수 없습니다. 지우면 처음부터 다시 확인합니다"})
	} else if st.Latest != "" {
		say(out, m("latest release seen: %s (%s)", "본 최신 버전: %s (%s)", st.Latest, st.Checked.Local().Format("2006-01-02 15:04")))
	}
	if p, ok := update.ReadProgress(dir); ok {
		sayProgress(out, p)
	}
	return nil
}

func sayFound(out io.Writer, f update.Found) {
	if f.Newer {
		say(out, m("a newer release is there: %s (this agent: %s); take it with: askorus-agent update now", "새 버전이 있습니다: %s (이 에이전트: %s). 업데이트하려면: askorus-agent update now", f.Latest, f.Current))
		return
	}
	say(out, m("this agent (%s) is up to date; the latest release is %s", "이 에이전트(%s)가 최신입니다. 최신 버전은 %s입니다", f.Current, f.Latest))
}

func sayProgress(out io.Writer, p update.Progress) {
	at := p.At.Local().Format("2006-01-02 15:04")
	switch p.State {
	case update.ProgressDone:
		say(out, m("last update: %s → %s, done (%s)", "마지막 업데이트: %s → %s, 끝남 (%s)", p.From, p.To, at))
	case update.ProgressRolledBack:
		say(out, m("last update: %s → %s did not come up and %s is back (%s): %s", "마지막 업데이트: %s → %s가 켜지지 않아 %s로 되돌렸습니다 (%s): %s", p.From, p.To, p.From, at, p.Reason))
	case update.ProgressFailed:
		say(out, m("last update: %s → %s failed, and so did going back (%s): %s; run the install line again", "마지막 업데이트: %s → %s가 실패했고 되돌리기도 실패했습니다 (%s): %s. 설치 한 줄을 다시 실행하세요", p.From, p.To, at, p.Reason))
	case update.ProgressNotTaken:
		say(out, m("last update: %s → %s stopped before anything changed (%s): %s", "마지막 업데이트: %s → %s는 아무것도 바꾸기 전에 멈췄습니다 (%s): %s", p.From, p.To, at, p.Reason))
	default:
		say(out, m("last update: %s → %s, %s (%s)", "마지막 업데이트: %s → %s, %s (%s)", p.From, p.To, p.State, at))
	}
}

// updateFailed words the errors a person can act on.
func updateFailed(err error) error {
	switch {
	case errors.Is(err, update.ErrNoRoot):
		return fail("this agent was built without an update key, so it cannot check releases", "이 에이전트는 업데이트 키 없이 만들어져 릴리스를 확인할 수 없습니다")
	case errors.Is(err, update.ErrNoReleaseInfo):
		return fail("the latest release has no signed update information; run the install line to update", "최신 릴리스에 서명된 업데이트 정보가 없습니다. 설치 한 줄로 업데이트하세요")
	case errors.Is(err, update.ErrExpired):
		return fail("the release information has expired (is the PC's clock right?): %v", "릴리스 정보가 만료됐습니다(PC 시계가 맞나요?): %v", err)
	case errors.Is(err, update.ErrBadSignature), errors.Is(err, update.ErrOldKey), errors.Is(err, update.ErrBadFiles), errors.Is(err, update.ErrWrongFile):
		return fail("the release does not check out, so nothing was changed: %v", "릴리스가 확인되지 않아 아무것도 바꾸지 않았습니다: %v", err)
	}
	return err
}

// updateNowCmd takes a newer release from the terminal.
func updateNowCmd(dir string, cfg config.Config, out io.Writer) error {
	exe, err := selfUpdatable(cfg)
	if err != nil {
		return err
	}
	f, err := updateChecker(dir).Check(context.Background(), true)
	if err != nil {
		return updateFailed(err)
	}
	if !f.Newer {
		sayFound(out, f)
		return nil
	}
	say(out, m("downloading %s and checking its signature…", "%s를 받고 서명을 확인합니다…", f.Latest))
	running := updateUp(cfg.Port)
	oldPID := 0
	if running {
		oldPID = readPID(dir)
	}
	started := updateNow()
	if err := handOver(dir, cfg, exe, f, oldPID, running); err != nil {
		return err
	}
	if running {
		say(out, m("stopping the agent; the new one takes over (phones lose the PC for a moment)…", "에이전트를 멈추고 새 에이전트가 이어받습니다(폰이 잠깐 끊깁니다)…"))
		// An agent that ended by itself meanwhile is fine: the new program
		// sees it gone and takes over.
		_ = updateStop(cfg.Port)
	}
	p, ok := waitProgress(dir, f.Latest, started, updateWaitOld+updateWaitTakeover+updateWaitNew+updateSettle+15*time.Second)
	if !ok {
		return fail("the update did not finish in time; see %s and run askorus-agent update status", "업데이트가 제때 끝나지 않았습니다. %s를 보고 askorus-agent update status를 실행하세요", filepath.Join(dir, "logs", "update.out"))
	}
	sayProgress(out, p)
	if p.State != update.ProgressDone {
		return fail("the update did not take", "업데이트가 되지 않았습니다")
	}
	return nil
}

// handOver stages f, swaps it in and starts the new program as
// update-finish. The caller then stops the old agent (the terminal by
// signalStop, the agent itself by its own shutdown). It holds the update
// lock (R-136) and hands it to the new program, and returns only once the
// new program says it is there to take over (R-135): otherwise the old
// program is put back and the old agent goes on.
func handOver(dir string, cfg config.Config, exe string, f update.Found, oldPID int, start bool) error {
	unlock, err := update.Lock(dir, os.Getpid(), updatePID)
	if errors.Is(err, update.ErrBusy) {
		return fail("another update of this agent is under way", "이 에이전트의 다른 업데이트가 진행 중입니다")
	}
	if err != nil {
		return err
	}
	handed := false
	defer func() {
		if !handed {
			unlock()
		}
	}()
	staged, err := update.Stage(context.Background(), updateSource(), f, runtime.GOOS, exe)
	if err != nil {
		return updateFailed(err)
	}
	prev, err := update.Swap(exe, staged, runtime.GOOS)
	if err != nil {
		os.Remove(staged)
		return err
	}
	started := updateNow()
	p := update.Progress{From: version, To: f.Latest, State: update.ProgressSwapped, At: started}
	if err := update.WriteProgress(dir, p); err != nil {
		_ = update.Restore(exe, prev, runtime.GOOS)
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o700); err != nil {
		_ = update.Restore(exe, prev, runtime.GOOS)
		return err
	}
	startArg := "0"
	if start {
		startArg = "1"
	}
	putBack := func(why string) error {
		if rerr := update.Restore(exe, prev, runtime.GOOS); rerr != nil {
			_ = update.WriteProgress(dir, update.Progress{From: version, To: f.Latest, State: update.ProgressFailed, Reason: why + "; putting the old agent back failed: " + rerr.Error(), At: updateNow()})
			return fmt.Errorf("%s; and putting the old agent back failed: %v", why, rerr)
		}
		_ = update.WriteProgress(dir, update.Progress{From: version, To: f.Latest, State: update.ProgressNotTaken, Reason: why, At: updateNow()})
		return errors.New(why)
	}
	helper, err := updateLaunch(exe, []string{updateFinishArg, strconv.Itoa(oldPID), version, f.Latest, startArg}, dir, filepath.Join(dir, "logs", "update.out"))
	if err != nil {
		return putBack("the new agent did not start: " + err.Error())
	}
	took := waitFor(func() bool {
		p, ok := update.ReadProgress(dir)
		return ok && p.To == f.Latest && p.State != update.ProgressSwapped && !p.At.Before(started)
	}, updateHandshake)
	if !took {
		killProcess(helper)
		return putBack("the new agent did not take over")
	}
	// The new program has taken over: the lock is its own now. When it was
	// quick enough to end already there is nothing left to hand over.
	_ = update.TransferLock(dir, os.Getpid(), helper)
	handed = true
	return nil
}

// updateLines are the lines check and setup show about updates: whether
// they are on and what goes outside the PC for them, why this agent cannot
// take them, and an update that did not take.
func updateLines(dir string, cfg config.Config) []checkLine {
	var lines []checkLine
	add := func(level string, x msg) { lines = append(lines, checkLine{level, x}) }
	if !cfg.UpdatesOn() {
		add("info", m("updates are off: phones cannot update this agent (askorus-agent update on)", "업데이트가 꺼져 있습니다. 폰에서 이 에이전트를 업데이트할 수 없습니다 (askorus-agent update on)"))
		return lines
	}
	add("ok", m("updates are on: when a phone asks, the agent asks GitHub (%s) for a newer signed release, sending only the address and its version; every paired phone may update it (off: askorus-agent update off, or devices update ID off)",
		"업데이트가 켜져 있습니다. 폰이 물으면 에이전트가 GitHub(%s)에 서명된 새 버전을 묻고, 주소와 에이전트 버전만 보냅니다. 짝 맺은 폰은 모두 업데이트할 수 있습니다 (끄기: askorus-agent update off, 폰별로는 devices update ID off)", update.DefaultBase))
	var why biError
	if _, err := selfUpdatable(cfg); errors.As(err, &why) {
		add("info", why.msg)
	} else if roots, err := updateRoots(); err != nil || len(roots) == 0 {
		add("info", m("this agent was built without an update key, so phones cannot update it; run the install line to update", "이 에이전트는 업데이트 키 없이 만들어져 폰에서 업데이트할 수 없습니다. 설치 한 줄로 업데이트하세요"))
	}
	if p, ok := update.ReadProgress(dir); ok && (p.State == update.ProgressRolledBack || p.State == update.ProgressFailed) {
		var b strings.Builder
		sayProgress(&b, p)
		if two := strings.SplitN(strings.TrimSpace(b.String()), "\n", 2); len(two) == 2 {
			add("warn", msg{en: two[0], ko: two[1]})
		}
	}
	return lines
}

// waitProgress waits for the update to version to finish after since.
func waitProgress(dir, to string, since time.Time, d time.Duration) (update.Progress, bool) {
	deadline := time.Now().Add(d)
	for {
		if p, ok := update.ReadProgress(dir); ok && p.To == to && !p.At.Before(since) && p.State != update.ProgressSwapped && p.State != update.ProgressFinishing {
			return p, true
		}
		if time.Now().After(deadline) {
			return update.Progress{}, false
		}
		time.Sleep(updatePoll)
	}
}

// updateFinishCmd is step 4, run by the new program: update-finish <old
// pid> <from> <to> <start 1|0>.
func updateFinishCmd(dir string, args []string) error {
	if len(args) != 4 {
		return errors.New("usage: askorus-agent update-finish <old pid> <from> <to> <start>")
	}
	oldPID, _ := strconv.Atoi(args[0])
	from, to, start := args[1], args[2], args[3] == "1"
	exe, err := updateExe()
	if err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	prev := update.Sibling(exe, runtime.GOOS, "prev")
	// The lock is this program's once the old agent hands it over; it goes
	// when the update has ended, however it ended.
	defer update.ReleaseLock(dir, os.Getpid())
	record := func(state, reason string) error {
		return update.WriteProgress(dir, update.Progress{From: from, To: to, State: state, Reason: reason, At: updateNow()})
	}
	// Out of the old agent's service first, or systemd ends this program
	// with it (R-177). Without that nothing is taken over: the old agent
	// goes on and puts its program back.
	if err := updateLeave(); err != nil {
		return err
	}
	// Looked at while the old agent still runs.
	service := oldPID > 0 && updateInService(oldPID)
	// Say at once that this program runs: only then does the old agent end (R-135).
	if err := record(update.ProgressFinishing, ""); err != nil {
		return err
	}
	// The old agent ends first: two would fight for the PC's Tailscale node.
	if oldPID > 0 && !waitFor(func() bool { return !updatePID(oldPID) }, updateWaitOld) && !updateEndStuck(exe, oldPID, service) {
		if err := update.Restore(exe, prev, runtime.GOOS); err != nil {
			return record(update.ProgressFailed, "the old agent did not stop, and putting it back failed: "+err.Error())
		}
		return record(update.ProgressRolledBack, "the old agent did not stop")
	}
	if service {
		// Or systemd may start a second agent next to the new one, when the
		// old one ended with an error. It only queues the stop, so the new
		// agent waits until nothing holds the port's lock (R-181).
		if err := updateStopService(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		waitFor(func() bool { return !updateUp(cfg.Port) }, updateWaitFree)
	}
	if !start {
		return record(update.ProgressDone, "")
	}
	reason, ok := startAndWatch(dir, cfg.Port, exe)
	if ok {
		return record(update.ProgressDone, "")
	}
	// The new agent did not come up: the old program goes back and runs.
	if err := update.Restore(exe, prev, runtime.GOOS); err != nil {
		return record(update.ProgressFailed, reason+"; putting the old agent back failed: "+err.Error())
	}
	if why, ok := startAndWatch(dir, cfg.Port, exe); !ok {
		return record(update.ProgressFailed, reason+"; the old agent did not come up either: "+why)
	}
	return record(update.ProgressRolledBack, reason)
}

// startAndWatch starts the agent from exe and checks it comes up within
// updateWaitNew and stays up for updateSettle. A start that fails is
// stopped again.
func startAndWatch(dir string, port int, exe string) (string, bool) {
	pid, err := updateLaunch(exe, []string{"serve"}, dir, filepath.Join(dir, "logs", "background.out"))
	if err != nil {
		return "it did not start: " + err.Error(), false
	}
	// Up means this very agent listens on its Tailscale node (R-134).
	if !waitFor(func() bool { return updateUp(port) && updateReady(dir, pid) }, updateWaitNew) {
		stopStarted(port, pid)
		return "it did not come up within a minute", false
	}
	if waitFor(func() bool { return !updatePID(pid) }, updateSettle) {
		return "it stopped right after it came up", false
	}
	return "", true
}

// stopStarted asks an agent that did not come up to stop, and waits a
// little for it to end.
func stopStarted(port, pid int) {
	_ = updateStop(port)
	waitFor(func() bool { return !updatePID(pid) }, 15*time.Second)
	if updatePID(pid) {
		killProcess(pid)
	}
}

// killProcess ends a program this update started; tests replace it.
var killProcess = func(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}

// waitFor polls ok until it is true or d has passed.
func waitFor(ok func() bool, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if ok() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(updatePoll)
	}
}
