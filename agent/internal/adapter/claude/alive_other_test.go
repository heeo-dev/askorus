//go:build !windows

package claude

import (
	"os"
	"os/exec"
	"testing"
)

// #52, found by the live session test on Linux: outside Windows no process
// ever counted as running, so Claude Code's live session files were all
// ignored and a session that had just been woken showed as stopped.
func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid(), "") {
		t.Error("this test's own process does not count as running")
	}
	if processAlive(0, "") || processAlive(-1, "") {
		t.Error("pid 0 or -1 counts as running")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// This test binary with a filter that matches nothing: it starts, runs
	// no test and ends.
	cmd := exec.Command(exe, "-test.run", "^$")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	cmd.Wait()
	if processAlive(pid, "") {
		t.Errorf("pid %d counts as running after it ended", pid)
	}
}
