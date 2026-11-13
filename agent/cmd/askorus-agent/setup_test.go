package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/adapter/claude"
	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/tsnode"
)

// fakeSetup is a PC for setup (#37): what it has, and what setup did to it.
type fakeSetup struct {
	elevated, onPath, running bool
	dir, auto                 string
	claude                    core.AdapterInfo
	others                    []namedInfo // Codex, OpenCode
	node                      nodeInfo    // the running agent's Tailscale node
	signsIn                   bool        // the person signs in when login shows the link
	loginErr                  error
	paired                    int
	pairIn                    string // what pair read
	calls                     []string
}

func signedInNode() nodeInfo {
	return nodeInfo{Port: 8790, Status: tsnode.Status{State: tsnode.StateRunning, Name: "askorus-pc.example.ts.net", Owner: "me@example.com", IPs: []string{"100.64.0.1"}}}
}

func needsLoginNode() nodeInfo {
	return nodeInfo{Port: 8790, Status: tsnode.Status{State: tsnode.StateNeedsLogin, LoginURL: "https://login.tailscale.com/a/abc123"}}
}

func (f *fakeSetup) env() setupEnv {
	return setupEnv{
		elevated: func() bool { return f.elevated },
		exe:      func() (string, error) { return filepath.Join(f.dir, "askorus-agent.exe"), nil },
		exeDir:   func() (string, error) { return f.dir, nil },
		pathHas:  func(string) bool { return f.onPath },
		pathAdd: func(d string) (bool, error) {
			f.calls = append(f.calls, "path on "+d)
			return true, nil
		},
		claude:  func(context.Context) core.AdapterInfo { return f.claude },
		others:  func(context.Context) []namedInfo { return f.others },
		running: func(int) bool { return f.running },
		start: func(io.Writer) error {
			f.calls = append(f.calls, "start")
			f.running = true
			return nil
		},
		autoGet: func() string { return f.auto },
		autoSet: func(exe string) error {
			f.calls = append(f.calls, "autostart on "+filepath.Base(exe))
			return nil
		},
		node: func() (nodeInfo, bool) { return f.node, f.running },
		login: func(context.Context, io.Writer) error {
			f.calls = append(f.calls, "login")
			if f.signsIn {
				f.node = signedInNode()
			}
			return f.loginErr
		},
		paired: func() int { return f.paired },
		pair: func(in io.Reader, _ io.Writer) error {
			b, _ := io.ReadAll(in)
			f.pairIn = string(b)
			f.calls = append(f.calls, "pair")
			return nil
		},
	}
}

// newPC is a PC with Claude Code and Codex (no OpenCode) whose agent, once
// started, is already signed in to Tailscale. Its exe folder is only a name
// (setup refuses a temporary folder such as t.TempDir); the fake writes
// nothing there.
func newPC(t *testing.T) *fakeSetup {
	t.Helper()
	dir := filepath.Join(string(filepath.Separator)+"Users", "someone", "AppData", "Local", "Programs", "Askorus")
	return &fakeSetup{dir: dir, claude: core.AdapterInfo{Available: true, Version: "2.1.319"},
		others: []namedInfo{{name: "Codex", info: core.AdapterInfo{Available: true, Version: "0.163.0"}}, {name: "OpenCode"}},
		node:   signedInNode()}
}

func runFake(f *fakeSetup, input string) (string, error) {
	var out bytes.Buffer
	err := runSetup(context.Background(), config.Config{Port: 8790}, f.env(), strings.NewReader(input), &out)
	return out.String(), err
}

// A new PC (#57): setup puts the folder on PATH, starts the agent, sets the
// logon entry, has the person sign in to Tailscale and pairs, in that
// order, and ends with the once-per-PC tips. Nothing of Tailscale is
// installed or set on the PC.
func TestSetupNewPC(t *testing.T) {
	f := newPC(t)
	f.node, f.signsIn = needsLoginNode(), true
	out, err := runFake(f, "y\n")
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	want := []string{"path on " + f.dir, "start", "autostart on askorus-agent.exe", "login", "pair"}
	if !slices.Equal(f.calls, want) {
		t.Errorf("calls = %q, want %q", f.calls, want)
	}
	if f.pairIn != "y\n" {
		t.Errorf("pair read %q; it must get the input setup did not read", f.pairIn)
	}
	for _, s := range []string{"[1/5] This PC / 이 PC", "[2/5] AI programs / AI 프로그램", "Claude Code 2.1.319 found", "Codex 0.163.0 found (asks)",
		"OpenCode not found (optional", "[3/5] Run the agent / 에이전트 켜기", "[4/5] Tailscale sign-in", "에이전트에 들어 있어서 로그인만 하면 됩니다",
		"[5/5] Pair the phone / 폰과 짝 맺기", "폰에 따로 설치할 것은 없습니다", "claude --rc", "askorus-agent check", "설정을 마쳤습니다"} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
	for _, s := range []string{"only one AI program", "tailscale serve", "tailscale.com/download", "HTTPS"} {
		if strings.Contains(out, s) {
			t.Errorf("output has %q:\n%s", s, out)
		}
	}
}

// Claude Code is not needed (2026-11-10 decision): without it setup goes on
// with the other AIs, says that My PCs needs it, and leaves out the Claude
// Code tips. With only one AI it says combined answers and debates need
// two.
func TestSetupWithoutClaudeCode(t *testing.T) {
	for name, c := range map[string]struct {
		others []namedInfo
		one    bool
	}{
		"codex and opencode": {[]namedInfo{{name: "Codex", info: core.AdapterInfo{Available: true, Version: "0.163.0"}}, {name: "OpenCode", info: core.AdapterInfo{Available: true, Version: "2.1.3"}}}, false},
		"codex only":         {[]namedInfo{{name: "Codex", info: core.AdapterInfo{Available: true, Version: "0.163.0"}}, {name: "OpenCode"}}, true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newPC(t)
			f.claude = core.AdapterInfo{}
			f.others = c.others
			out, err := runFake(f, "y\n")
			if err != nil {
				t.Fatalf("setup: %v\n%s", err, out)
			}
			if want := []string{"path on " + f.dir, "start", "autostart on askorus-agent.exe", "pair"}; !slices.Equal(f.calls, want) {
				t.Errorf("calls = %q, want %q", f.calls, want)
			}
			for _, s := range []string{"Claude Code was not found", "My PCs", "'내 PC'", "Codex 0.163.0 found (asks)", "설정을 마쳤습니다"} {
				if !strings.Contains(out, s) {
					t.Errorf("output lacks %q:\n%s", s, out)
				}
			}
			if strings.Contains(out, "claude --rc") || strings.Contains(out, "[X]") {
				t.Errorf("Claude Code tips or a problem mark without Claude Code:\n%s", out)
			}
			if got := strings.Contains(out, "only one AI program"); got != c.one {
				t.Errorf("one-AI warning shown = %v, want %v:\n%s", got, c.one, out)
			}
		})
	}
}

// Run again on a PC that is set up: it changes nothing, asks for no login
// and does not pair again.
func TestSetupAgainChangesNothing(t *testing.T) {
	f := newPC(t)
	f.onPath, f.running, f.paired = true, true, 1
	f.auto = autostartCommand(filepath.Join(f.dir, "askorus-agent.exe"))
	out, err := runFake(f, "")
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	if len(f.calls) != 0 {
		t.Errorf("calls = %q, want none", f.calls)
	}
	for _, s := range []string{"askorus-agent pair", "me@example.com", "askorus-pc.example.ts.net"} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
}

// A logon entry left by a copy in another folder is pointed at this exe
// (2호's review, 11-10): it would start that copy, or nothing.
func TestSetupFixesAnOldLogonEntry(t *testing.T) {
	f := newPC(t)
	f.onPath, f.running, f.paired = true, true, 1
	f.auto = `"C:\old\askorus-agent.exe" start`
	out, err := runFake(f, "")
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	if !slices.Equal(f.calls, []string{"autostart on askorus-agent.exe"}) || !strings.Contains(out, `C:\old\askorus-agent.exe`) {
		t.Errorf("calls = %q\n%s", f.calls, out)
	}
}

// Each step setup cannot fix stops it there with how to fix it, before it
// changes anything after that step.
func TestSetupStops(t *testing.T) {
	for name, c := range map[string]struct {
		set   func(*fakeSetup)
		says  string
		calls []string // the first word or two of each call
	}{
		"administrator": {func(f *fakeSetup) { f.elevated = true }, "관리자 권한", nil},
		"temporary":     {func(f *fakeSetup) { f.dir = filepath.Join(os.TempDir(), "x") }, "looks temporary", nil},
		"no AI at all": {func(f *fakeSetup) {
			f.claude.Available, f.others = false, []namedInfo{{name: "Codex"}, {name: "OpenCode"}}
		}, "Claude Code, Codex or OpenCode", []string{"path on"}},
		"elevated svc": {func(f *fakeSetup) { f.claude.Notices = []string{claude.NoticeServiceElevated} }, "claude daemon stop --any", []string{"path on"}},
		// The person did not sign in (closed the window, or the wait ran
		// out): setup stops before pairing, which could not work yet.
		"not signed in": {func(f *fakeSetup) { f.node = needsLoginNode() }, "아직 Tailscale에 로그인되지 않았습니다", []string{"path on", "start", "autostart on", "login"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newPC(t)
			c.set(f)
			out, err := runFake(f, "")
			var be biError
			if !errors.As(err, &be) || !strings.Contains(be.ko, "setup을 다시") {
				t.Fatalf("err = %v, want the stop error\n%s", err, out)
			}
			if !strings.Contains(out, c.says) || !strings.Contains(out, "[X]") {
				t.Errorf("output lacks %q:\n%s", c.says, out)
			}
			var got []string
			for _, call := range f.calls {
				w := strings.Fields(call)
				if len(w) > 1 && w[1] == "on" {
					got = append(got, w[0]+" "+w[1])
				} else {
					got = append(got, w[0])
				}
			}
			if !slices.Equal(got, c.calls) {
				t.Errorf("calls = %q, want %q", f.calls, c.calls)
			}
		})
	}
}

// The login step (#57): an error from it (the agent stopped while waiting)
// ends setup with that error and nothing is paired.
func TestSetupLoginFails(t *testing.T) {
	f := newPC(t)
	f.node = needsLoginNode()
	f.loginErr = fail("the agent is not running", "에이전트가 돌고 있지 않습니다")
	out, err := runFake(f, "y\n")
	if err == nil || !strings.Contains(err.Error(), "the agent is not running") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if slices.Contains(f.calls, "pair") {
		t.Errorf("paired after a failed login: %q", f.calls)
	}
}
