//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// #52: the systemd user unit. The agent's path and the terminal's PATH go
// into the file as values, never as lines of their own.
func TestUnitText(t *testing.T) {
	got := unitText(`/home/a b/100%/"x"/askorus-agent`, `/usr/bin:/mnt/c/Program Files/x:/home/a/.local/bin`, "/home/a/my agent")
	for _, want := range []string{
		"# command: \"/home/a b/100%/\\\"x\\\"/askorus-agent\" serve\n",
		"ExecStart=\"/home/a b/100%%/\\\"x\\\"/askorus-agent\" serve\n",
		"Environment=\"ASKORUS_DETACHED=1\"\n",
		"Environment=\"PATH=/usr/bin:/mnt/c/Program Files/x:/home/a/.local/bin\"\n",
		"Environment=\"ASKORUS_AGENT_DIR=/home/a/my agent\"\n",
		"[Install]\nWantedBy=default.target\n",
		"Restart=on-failure\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the unit lacks %q:\n%s", want, got)
		}
	}
	// A PATH with a line break would add a line of its own: it is left out.
	if bad := unitText("/x/askorus-agent", "/usr/bin\nExecStartPre=/bin/evil", "/a\nExecStartPre=/bin/evil"); strings.Contains(bad, "evil") || strings.Contains(bad, "PATH=") || strings.Contains(bad, agentDirEnv) {
		t.Errorf("a line break got into the unit:\n%s", bad)
	}
}

func TestAutostartOnLinux(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("PATH", "/usr/bin:/home/me/.local/bin")
	var calls []string
	var fail error
	old := systemctlUser
	systemctlUser = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return fail
	}
	t.Cleanup(func() { systemctlUser = old })
	unit := filepath.Join(cfg, "systemd", "user", unitName)

	if autostartGet() != "" {
		t.Fatal("on before it was turned on")
	}
	// Without this user's systemd nothing is written.
	fail = errors.New("Failed to connect to bus")
	if err := autostartSet("/opt/askorus-agent"); err == nil || !strings.Contains(err.Error(), "systemd does not answer") {
		t.Fatalf("no systemd: %v", err)
	}
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatal("a unit was written without systemd")
	}

	// systemd answers but does not take the unit: no file is left that says it is on.
	systemctlUser = func(args ...string) error {
		if args[0] == "enable" {
			return errors.New("Unit file does not exist")
		}
		return nil
	}
	if err := autostartSet("/opt/askorus-agent"); err == nil || autostartGet() != "" {
		t.Fatalf("enable failed, but autostart reads %q (%v)", autostartGet(), err)
	}
	systemctlUser = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return fail
	}

	fail, calls = nil, nil
	if err := autostartSet("/opt/askorus-agent"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"show-environment", "daemon-reload", "enable " + unitName}; !slices.Equal(calls, want) {
		t.Errorf("systemctl calls = %q, want %q", calls, want)
	}
	b, err := os.ReadFile(unit)
	if err != nil || !strings.Contains(string(b), "Environment=\"PATH=/usr/bin:/home/me/.local/bin\"") {
		t.Fatalf("unit = %q, %v", b, err)
	}
	if got := autostartGet(); got != `"/opt/askorus-agent" serve` || got != autostartCommand("/opt/askorus-agent") {
		t.Errorf("autostartGet = %q", got)
	}

	calls = nil
	if err := autostartRemove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unit); !os.IsNotExist(err) || autostartGet() != "" {
		t.Error("the unit stayed after autostart off")
	}
	if want := []string{"disable " + unitName, "daemon-reload"}; !slices.Equal(calls, want) {
		t.Errorf("systemctl calls = %q, want %q", calls, want)
	}
	// Off twice is fine and asks systemd nothing.
	calls = nil
	if err := autostartRemove(); err != nil || len(calls) != 0 {
		t.Errorf("off again: %v, %q", err, calls)
	}
}
