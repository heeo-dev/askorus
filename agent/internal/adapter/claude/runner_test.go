package claude

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// R-18: exec copies output with io.Copy, which used an embedded
// bytes.Buffer's ReadFrom and skipped the cap.
func TestCapBufferHoldsThroughCopy(t *testing.T) {
	b := &capBuffer{max: 10}
	n, err := io.Copy(b, strings.NewReader(strings.Repeat("x", 25)))
	if err != nil || n != 25 || len(b.Bytes()) != 10 {
		t.Errorf("copied %d (%v), kept %d bytes", n, err, len(b.Bytes()))
	}
}

func TestRunnerVersionAndAgents(t *testing.T) {
	r, argsFile := fakeRunner(t)
	t.Setenv("ASKORUS_FAKE_AGENTS", filepath.Join("testdata", "agents.json"))
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
	t.Setenv("ASKORUS_FAKE_EXIT", "3")
	if _, err := r.Run(context.Background(), "", "--version"); err == nil || !strings.Contains(err.Error(), "exit code 3") {
		t.Errorf("err = %v", err)
	}
	t.Setenv("ASKORUS_FAKE_EXIT", "")
	t.Setenv("ASKORUS_FAKE_SLEEP", "5s")
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
		{"remote-control", "--name"},
		{"remote-control", "--spawn", "-x"},
		{"remote-control", "extra"},
		{"remote-control", "--name", "a", "--permission-mode", "bypassPermissions"},
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
		{"remote-control", "--name", "my app", "--spawn", "same-dir", "--capacity", "4"},
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

// R-102 (#44): an error about a claude command names the subcommand and the
// options, never a value. A session name can be words from a conversation
// and the error text reaches agent.log.
func TestRunnerErrorHasNoArgumentValues(t *testing.T) {
	if got := argNames([]string{"--bg", "--remote-control", "my secret plan", "-n", "my secret plan", "--model=opus"}); got != "--bg --remote-control -n --model" {
		t.Errorf("argNames = %q", got)
	}
	if got := argNames([]string{"stop", "0a1b2c3d"}); got != "stop" {
		t.Errorf("argNames = %q", got)
	}
	r, _ := fakeRunner(t)
	// The fake claude knows no such session: it ends with a code.
	_, err := r.Run(context.Background(), "", "stop", "5ec2e7aa")
	if err == nil || !strings.Contains(err.Error(), "claude stop: exit code") || strings.Contains(err.Error(), "5ec2e7aa") {
		t.Errorf("err = %v", err)
	}
	t.Setenv("ASKORUS_FAKE_SLEEP", "5s")
	r.timeout = 300 * time.Millisecond
	_, err = r.Run(context.Background(), "", "--bg", "--remote-control", "my secret plan", "-n", "my secret plan")
	if err == nil || !strings.Contains(err.Error(), "timed out") || strings.Contains(err.Error(), "secret") {
		t.Errorf("err = %v", err)
	}
}

// R-102 (#44): the log line of a folder server that ended keeps the reason
// and leaves out the last lines of its screen, which the phone still gets.
func TestServeLogReasonLeavesTheScreenOut(t *testing.T) {
	for _, c := range [][2]string{
		{serverEnded + " (exit status 1): Session: my secret plan | Connected", serverEnded + " (exit status 1)"},
		{serverEnded + ": Session: my secret plan", serverEnded},
		{serverEnded, serverEnded},
	} {
		if got := ServeLogReason(core.ServeStatus{State: core.ServeFailed, Error: c[0]}); got != c[1] {
			t.Errorf("%q -> %q, want %q", c[0], got, c[1])
		}
	}
	// What is not a screen stays: the help for the consent, the folder, the pid.
	if got := ServeLogReason(core.ServeStatus{State: core.ServeFailed, Error: consentHelp}); got != consentHelp {
		t.Errorf("consent = %q", got)
	}
	if got := ServeLogReason(core.ServeStatus{State: core.ServeExternal, Error: "pid 12"}); got != "pid 12" {
		t.Errorf("external = %q", got)
	}
}
