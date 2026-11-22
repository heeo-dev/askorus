//go:build linux || darwin

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// pretendAgentEnv makes a copy of this test binary wait like an agent that
// does not stop.
const pretendAgentEnv = "ASKORUS_TEST_PRETEND_AGENT"

func init() {
	if os.Getenv(pretendAgentEnv) == "1" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}

// copyOfTests puts this test binary at path, as a program to run.
func copyOfTests(t *testing.T, path string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	from, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer from.Close()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	to, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	defer to.Close()
	if _, err := io.Copy(to, from); err != nil {
		t.Fatal(err)
	}
}

// R-178 (#71): only a serve of the installed program is ended. Nothing
// else that happens to have the pid, the name or the folder.
func TestEndStuckEndsOnlyTheInstalledServe(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "in stall") // a space, as a home folder may have
	exe := filepath.Join(dir, "askorus-agent")

	for _, c := range []struct {
		name  string
		runs  string
		args  []string
		swap  bool // renamed to .prev while it runs, as an update does
		ended bool
	}{
		{"the installed serve", exe, []string{"serve"}, false, true},
		{"the installed serve with a port", exe, []string{"serve", "-port", "28790"}, false, true},
		{"the installed serve after the swap", exe, []string{"serve"}, true, true},
		{"a serve of the kept previous program", exe + ".prev", []string{"serve"}, false, true},
		{"another command of the installed program", exe, []string{"pair"}, false, false},
		{"serve as a later argument", exe, []string{"folders", "serve"}, false, false},
		{"a serve of the same name elsewhere", filepath.Join(root, "other", "askorus-agent"), []string{"serve"}, false, false},
		{"another program in the folder", filepath.Join(dir, "other"), []string{"serve"}, false, false},
		{"a program whose name only begins so", exe + "-tool", []string{"serve"}, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			copyOfTests(t, c.runs)
			cmd := exec.Command(c.runs, c.args...)
			cmd.Env = append(os.Environ(), pretendAgentEnv+"=1")
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer cmd.Wait()
			defer cmd.Process.Kill()
			if c.swap {
				if err := os.Rename(exe, exe+".prev"); err != nil {
					t.Fatal(err)
				}
				copyOfTests(t, exe) // the new program is at the path now
				defer os.Remove(exe + ".prev")
			}
			time.Sleep(200 * time.Millisecond)
			if got := endStuck(exe, cmd.Process.Pid); got != c.ended {
				t.Errorf("endStuck = %v, want %v", got, c.ended)
			}
			if running := processRunning(cmd.Process.Pid); running == c.ended {
				t.Errorf("running afterwards = %v", running)
			}
		})
	}

	for _, pid := range []int{-1, 0, 1, os.Getpid()} {
		if endStuck(exe, pid) {
			t.Errorf("endStuck(%d) = true", pid)
		}
	}
}
