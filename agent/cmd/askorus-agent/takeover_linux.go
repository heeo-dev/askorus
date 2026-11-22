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
	"errors"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

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
	return syscall.Exec(run, scopeArgs(exe, os.Args[1:]), append(os.Environ(), scopedEnv+"=1"))
}

// scopeArgs are systemd-run's arguments. Newer systemd-run (258) replaces
// ${NAME} in a scope's command, so with a "$" in it that is turned off
// (R-181). The option came with 254: an older one refuses it, and then
// nothing is taken over.
func scopeArgs(exe string, args []string) []string {
	out := []string{"systemd-run", "--user", "--scope", "--quiet", "--collect"}
	command := append([]string{exe}, args...)
	if slices.ContainsFunc(command, func(s string) bool { return strings.Contains(s, "$") }) {
		out = append(out, "--expand-environment=no")
	}
	return append(append(out, "--"), command...)
}

// stopService stops the unit the old agent ran in, so systemd does not
// start a second agent when the old one ended with an error or was ended.
func stopService() error {
	ctx, cancel := context.WithTimeout(context.Background(), serviceStopWait)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "--user", "stop", "--no-block", unitName).CombinedOutput()
	if err != nil {
		return fail("systemctl --user stop %s did not work (%s)", "systemctl --user stop %s이 되지 않았습니다 (%s)", unitName, unitErrLine(string(out), err))
	}
	return nil
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

// procStat is the state letter and the start time (field 22) of a process.
func procStat(pid int) (state byte, started string, ok bool) {
	b, err := os.ReadFile(procFile(pid, "stat"))
	if err != nil {
		return 0, "", false
	}
	// "<pid> (<name>) <state> ...": the name may hold brackets and spaces.
	i := strings.LastIndexByte(string(b), ')')
	if i < 0 {
		return 0, "", false
	}
	f := strings.Fields(string(b[i+1:]))
	if len(f) < 20 {
		return 0, "", false
	}
	return f[0][0], f[19], true
}

func isZombie(pid int) bool {
	state, _, ok := procStat(pid)
	return ok && state == 'Z'
}

// heldProcessID is a process held for a signal: by a pidfd, or by its
// start time where the kernel has no pidfd (before 5.3).
type heldProcessID struct {
	pid, fd int
	started string
}

func holdProcess(pid int) (*heldProcessID, error) {
	fd, err := unix.PidfdOpen(pid, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil, err
	}
	if err != nil {
		fd = -1
	}
	_, started, ok := procStat(pid)
	if !ok {
		if fd >= 0 {
			unix.Close(fd)
		}
		return nil, unix.ESRCH
	}
	return &heldProcessID{pid: pid, fd: fd, started: started}, nil
}

func (h *heldProcessID) kill() error {
	if h.fd >= 0 {
		err := unix.PidfdSendSignal(h.fd, unix.SIGKILL, nil, 0)
		if !errors.Is(err, unix.ENOSYS) {
			return err
		}
	}
	if _, started, ok := procStat(h.pid); !ok || started != h.started {
		return unix.ESRCH // it ended, and the pid may be someone else's
	}
	return unix.Kill(h.pid, unix.SIGKILL)
}

func (h *heldProcessID) release() {
	if h.fd >= 0 {
		unix.Close(h.fd)
	}
}
