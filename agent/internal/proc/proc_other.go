//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
)

// prepare gives the AI a process group of its own (macOS and Linux, #52),
// so everything it starts can be ended together, and makes the end of the
// context end the whole group, not only the first process.
func prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// The group's id is the first process's id (Setpgid with no Pgid).
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// contain returns what ends the processes the command left running: the
// same promise as the job object on Windows. A process that moved itself
// to another group or session is out of reach; none of the AIs does.
func contain(cmd *exec.Cmd) func() {
	pgid := cmd.Process.Pid
	return func() {
		// No such group any more is the usual case and not an error.
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
}
