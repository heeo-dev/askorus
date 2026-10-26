package claude

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunnerVersionAndAgents(t *testing.T) {
	r, argsFile := fakeRunner(t)
	t.Setenv("ASKSEMBLE_FAKE_AGENTS", filepath.Join("testdata", "agents.json"))
	ctx := context.Background()

	v, err := r.Version(ctx)
	if err != nil || v != "2.1.310" {
		t.Fatalf("Version = %q, %v", v, err)
	}
	entries, err := r.Agents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 6 || entries[0].PID != 100 || entries[0].Kind != kindBackground {
		t.Errorf("entries = %+v", entries)
	}
	got := recordedArgs(t, argsFile)
	want := [][]string{{"--version"}, {"agents", "--json", "--all"}}
	if len(got) != len(want) || strings.Join(got[0], " ") != "--version" || strings.Join(got[1], " ") != "agents --json --all" {
		t.Errorf("claude was run with %v, want %v", got, want)
	}
}

func TestRunnerExitCodeAndTimeout(t *testing.T) {
	r, _ := fakeRunner(t)
	t.Setenv("ASKSEMBLE_FAKE_EXIT", "3")
	if _, err := r.Run(context.Background(), "", "--version"); err == nil || !strings.Contains(err.Error(), "exit code 3") {
		t.Errorf("err = %v", err)
	}
	t.Setenv("ASKSEMBLE_FAKE_EXIT", "")
	t.Setenv("ASKSEMBLE_FAKE_SLEEP", "5s")
	r.timeout = 300 * time.Millisecond
	start := time.Now()
	if _, err := r.Run(context.Background(), "", "--version"); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %v", err)
	}
	if time.Since(start) > 4*time.Second {
		t.Errorf("timeout took %s", time.Since(start))
	}
}

// Principle 7: no permission-relaxing flag reaches claude, and nothing that
// is not on the allow list either. Rejected arguments never start a process.
func TestRunnerRejectsArguments(t *testing.T) {
	r, argsFile := fakeRunner(t)
	bad := [][]string{
		{"--dangerously-skip-permissions"},
		{"--allow-dangerously-skip-permissions"},
		{"--permission-mode", "bypassPermissions"},
		{"--permission-mode=bypassPermissions"},
		{"--PERMISSION-MODE=acceptEdits"},
		{"--allowedTools", "Bash"},
		{"--add-dir", "C:\\"},
		{"agents", "--json", "--all", "--dangerously-skip-permissions"},
		{"-p", "hi"},
		{"stop"},
		{"stop", "a", "b"},
		{"rm", "--all"},
		{"daemon", "stop", "--any"},
		{"agents", "extra"},
		{"--bg", "--resume"},
		{"--bg", "--resume", "-x"},
		{"--bg", "-n", "--dangerously-skip-permissions"},
		{"--bg", "--remote-control", "name", "extra"},
		{"--bg", "--resume", "id", "--allow-dangerously-skip-permissions"},
		{"attach", "x"},
		{"--version\n--dangerously-skip-permissions"},
		{"--json\x00"},
	}
	for _, args := range bad {
		if _, err := r.Run(context.Background(), "", args...); !errors.Is(err, ErrArgRejected) {
			t.Errorf("Run(%q) err = %v, want ErrArgRejected", args, err)
		}
	}
	if got := recordedArgs(t, argsFile); len(got) != 0 {
		t.Errorf("claude was started for rejected args: %v", got)
	}
}

// The exact argument lists the launch rules build are accepted.
func TestLaunchArgumentsAccepted(t *testing.T) {
	good := [][]string{
		{"--bg", "--resume", "0b6f2a8e-1111-4222-8333-444455556666"},
		{"--bg", "--resume", "0b6f2a8e-1111-4222-8333-444455556666", "--remote-control", "-n", "My title", "--model", "opus", "--effort", "high"},
		{"--bg", "--remote-control", "my job", "-n", "my job"},
		{"stop", "634f64ab"},
		{"rm", "634f64ab"},
		{"logs", "634f64ab"},
	}
	for _, args := range good {
		if err := checkArgs(args); err != nil {
			t.Errorf("checkArgs(%q) = %v", args, err)
		}
	}
}

func TestDeniedFlagsWinOverAllowList(t *testing.T) {
	allowedFlags["--dangerously-skip-permissions"] = true
	defer delete(allowedFlags, "--dangerously-skip-permissions")
	if err := checkArgs([]string{"--dangerously-skip-permissions"}); !errors.Is(err, ErrArgRejected) {
		t.Errorf("err = %v", err)
	}
}

func TestNewRunnerRefusesScripts(t *testing.T) {
	for _, p := range []string{`C:\npm\claude.cmd`, `C:\npm\claude.BAT`, `/usr/local/bin/claude.sh`, `C:\x\claude.ps1`} {
		if _, err := NewRunner(p); !errors.Is(err, ErrScript) {
			t.Errorf("NewRunner(%q) err = %v", p, err)
		}
	}
}

func TestParseAgentsShapes(t *testing.T) {
	for _, in := range []string{"", "  \n", "[]", `{"agents":[]}`} {
		if _, err := parseAgents([]byte(in)); err != nil {
			t.Errorf("parseAgents(%q) = %v", in, err)
		}
	}
	if list, err := parseAgents([]byte(`{"agents":[{"pid":1,"kind":"interactive"}]}`)); err != nil || len(list) != 1 {
		t.Errorf("wrapped = %v %v", list, err)
	}
	if _, err := parseAgents([]byte("Error: not logged in")); err == nil {
		t.Error("text output accepted")
	}
}
