//go:build linux || darwin

package main

import (
	"os"
	"time"
)

// stuckKillWait is how long a killed agent may take to be gone. It is
// shorter than the five seconds after which systemd would restart it.
const stuckKillWait = 3 * time.Second

// endStuck ends the old agent that was asked to stop for an update and did
// not (#70, #71; the maintainer's decision of 2026-11-22, Phase 1 plan part
// 27). Only a serve of the installed program is ended. It reports whether
// the old agent is gone; when it is not, the update goes back as before.
func endStuck(exe string, pid int) bool {
	if pid <= 1 || pid == os.Getpid() {
		return false
	}
	// Taken before the check: on Linux this is a pidfd, so a pid that went
	// to another process in between is never signalled.
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	defer p.Release()
	if !installedServe(exe, pid) {
		return !processRunning(pid)
	}
	if err := p.Kill(); err != nil && processRunning(pid) {
		return false
	}
	return waitFor(func() bool { return !processRunning(pid) }, stuckKillWait)
}
