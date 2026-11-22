//go:build linux || darwin

package main

// Ending an agent that was asked to stop and did not (#70, #71; the
// maintainer's decision of 2026-11-22, Phase 1 plan part 27). It is only
// ever one process: the serve of the installed program that update-finish
// waited for, or the one that holds the lock of these settings' port when
// install.sh asks (R-179: an agent of other settings that runs from the
// same program is not it).

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/config"
)

// stuckKillWait is how long a killed agent may take to be gone. It is
// shorter than the five seconds after which systemd would restart it.
const stuckKillWait = 3 * time.Second

// endStuck ends pid when it is a serve of the program installed at exe.
// service says it runs in the agent's systemd unit: the unit is stopped
// first, and when that fails nothing is ended, or systemd would start it
// again. It reports whether the agent is gone; when it is not, the update
// goes back as before.
func endStuck(exe string, pid int, service bool) bool {
	if pid <= 1 || pid == os.Getpid() {
		return false
	}
	// Held before the check, so that a pid which went to another process in
	// between is never signalled (holdProcess).
	h, err := holdProcess(pid)
	if err != nil {
		return !processRunning(pid)
	}
	defer h.release()
	if !installedServe(exe, pid) {
		return !processRunning(pid)
	}
	if service {
		if err := updateStopService(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return false
		}
	}
	if err := h.kill(); err != nil && processRunning(pid) {
		return false
	}
	return waitFor(func() bool { return !processRunning(pid) }, stuckKillWait)
}

// endStuckCmd is `end-stuck <installed program>`, which install.sh runs
// with the new program after the old one's shutdown failed. It is not a
// command for people. The agent to end is the one of these settings: the
// holder of their port's lock.
func endStuckCmd(dir string, args []string, out io.Writer) error {
	if len(args) != 1 || !filepath.IsAbs(args[0]) {
		return fail("usage: askorus-agent end-stuck <installed program>", "사용법: askorus-agent end-stuck <설치된 프로그램>")
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	if !agentRunning(cfg.Port) {
		return nil // it stopped after all, or shutdown took a zombie for it
	}
	exe := args[0]
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	pid := lockedPID(cfg.Port)
	if !installedServe(exe, pid) {
		return fail("the agent that runs with port %d is not serve of %s, so it is left alone",
			"포트 %d로 도는 에이전트가 %s의 serve가 아니어서 그대로 둡니다", cfg.Port, exe)
	}
	say(out, m("the agent did not stop: ending it (pid %d)", "에이전트가 멈추지 않아 끝냅니다 (pid %d)", pid))
	if !endStuck(exe, pid, inService(pid)) || !waitFor(func() bool { return !agentRunning(cfg.Port) }, stuckKillWait) {
		return fail("the agent could not be ended", "에이전트를 끝내지 못했습니다")
	}
	return nil
}
