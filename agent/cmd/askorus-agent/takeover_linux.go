//go:build linux

package main

// What update-finish needs on Linux to take over (#71, Phase 1 plan part
// 27). When the agent runs as its systemd user unit, systemd ends
// everything left in the unit's control group as soon as the agent ends,
// and a new session (Setsid) does not leave that group. The new program
// therefore leaves it before it says that it takes over. It is the new
// program that does this, so agents already installed can update to it.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/update"
)

// scopedEnv marks an update-finish that systemd-run has moved. It is taken
// away again at once, so the agent it starts does not carry it.
const scopedEnv = "ASKORUS_UPDATE_SCOPED"

// serviceStopWait bounds systemctl, which only queues the stop.
const serviceStopWait = 10 * time.Second

func procFile(pid int, name string) string {
	return "/proc/" + strconv.Itoa(pid) + "/" + name
}

// cgroupInUnit reports whether a /proc/<pid>/cgroup text puts the process
// in unit, or in a group below it.
func cgroupInUnit(text, unit string) bool {
	for line := range strings.Lines(text) {
		// "<hierarchy>:<controllers>:<path>"
		f := strings.SplitN(strings.TrimSpace(line), ":", 3)
		if len(f) == 3 && slices.Contains(strings.Split(f[2], "/"), unit) {
			return true
		}
	}
	return false
}

// inService reports whether pid runs inside the agent's systemd unit.
func inService(pid int) bool {
	if pid <= 0 {
		return false
	}
	b, err := os.ReadFile(procFile(pid, "cgroup"))
	return err == nil && cgroupInUnit(string(b), unitName)
}

// leaveService moves this update-finish into a scope of its own when it
// runs inside the unit. It execs, so the pid stays the one the update lock
// is handed to. An error means it must not take over: the old agent then
// keeps running and puts its program back (R-135).
func leaveService() error {
	moved := os.Getenv(scopedEnv) != ""
	os.Unsetenv(scopedEnv)
	if !inService(os.Getpid()) {
		return nil
	}
	if moved {
		return fail("the update could not leave the agent's systemd service", "업데이트가 에이전트의 systemd 서비스에서 나오지 못했습니다")
	}
	run, err := exec.LookPath("systemd-run")
	if err != nil {
		return fail("systemd-run was not found, so the agent cannot update itself under systemd; run the install line in a terminal",
			"systemd-run이 없어 systemd에서 스스로 업데이트하지 못합니다. 터미널에서 설치 한 줄을 실행하세요")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := append([]string{"systemd-run", "--user", "--scope", "--quiet", "--collect", "--", exe}, os.Args[1:]...)
	return syscall.Exec(run, args, append(os.Environ(), scopedEnv+"=1"))
}

// stopService stops the unit the old agent ran in, so systemd does not
// start a second agent when the old one ended with an error or was ended.
func stopService() {
	ctx, cancel := context.WithTimeout(context.Background(), serviceStopWait)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "--user", "stop", "--no-block", unitName).CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "systemctl --user stop %s: %s\n", unitName, unitErrLine(string(out), err))
	}
}

// installedServe reports whether pid is a serve of the program installed
// at exe: that very file, or the one it became in this update's swap.
func installedServe(exe string, pid int) bool {
	runs, err := os.Readlink(procFile(pid, "exe"))
	if err != nil {
		return false
	}
	runs = strings.TrimSuffix(runs, " (deleted)")
	if runs != exe && runs != update.Sibling(exe, "linux", "prev") {
		return false
	}
	b, err := os.ReadFile(procFile(pid, "cmdline"))
	if err != nil {
		return false
	}
	args := strings.Split(string(b), "\x00")
	return len(args) > 1 && args[1] == "serve"
}

func isZombie(pid int) bool {
	b, err := os.ReadFile(procFile(pid, "stat"))
	if err != nil {
		return false
	}
	// "<pid> (<name>) <state> ...": the name may hold brackets and spaces.
	i := strings.LastIndexByte(string(b), ')')
	return i >= 0 && i+2 < len(b) && b[i+2] == 'Z'
}
