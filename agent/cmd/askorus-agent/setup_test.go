package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/adapter/claude"
	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/tailnet"
)

// fakeSetup is a PC for setup (#37): what it has, and what setup did to it.
type fakeSetup struct {
	elevated, onPath, listening bool
	other                       bool // another program, not an agent, has the port
	dir, auto                   string
	claude                      core.AdapterInfo
	others                      []namedInfo   // Codex, OpenCode
	nets                        []tailnetInfo // one per look; the last repeats
	paired                      int
	pairIn                      string // what pair read
	serveErr                    error  // what serveOn returns
	calls                       []string
	looks                       int
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
		claude: func(context.Context) core.AdapterInfo { return f.claude },
		others: func(context.Context) []namedInfo { return f.others },
		tailnet: func(context.Context, int) tailnetInfo {
			ti := f.nets[min(f.looks, len(f.nets)-1)]
			f.looks++
			return ti
		},
		servePort: func(_ context.Context, p int) int { return p },
		serveOn: func(_ context.Context, h, l int) error {
			f.calls = append(f.calls, fmt.Sprintf("serve %d->%d", h, l))
			return f.serveErr
		},
		listening: func(int) bool { return f.listening },
		ours:      func(int) bool { return f.listening && !f.other },
		settings:  `C:\Users\someone\AppData\Roaming\Askorus\agent.json`,
		start: func(io.Writer) error {
			f.calls = append(f.calls, "start")
			return nil
		},
		autoGet: func() string { return f.auto },
		autoSet: func(exe string) error {
			f.calls = append(f.calls, "autostart on "+filepath.Base(exe))
			return nil
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

func servedNet() tailnetInfo {
	return tailnetInfo{status: tailnet.Status{Running: true, DNSName: "pc.example.ts.net", OwnerLogin: "me@example.com", HTTPS: true},
		serve: tailnet.Serve{Host: "pc.example.ts.net", HTTPSPort: 8790}, served: true}
}

// newPC is a PC with Claude Code, Codex and Tailscale ready (no OpenCode).
// Its exe folder is only a name (setup refuses a temporary folder such as
// t.TempDir); the fake writes nothing there.
func newPC(t *testing.T) *fakeSetup {
	t.Helper()
	dir := filepath.Join(string(filepath.Separator)+"Users", "someone", "AppData", "Local", "Programs", "Askorus")
	return &fakeSetup{dir: dir, claude: core.AdapterInfo{Available: true, Version: "2.1.319"},
		others: []namedInfo{{name: "Codex", info: core.AdapterInfo{Available: true, Version: "0.163.0"}}, {name: "OpenCode"}},
		nets:   []tailnetInfo{servedNet()}}
}

func runFake(f *fakeSetup, input string) (string, error) {
	var out bytes.Buffer
	err := runSetup(context.Background(), config.Config{Port: 8790}, f.env(), strings.NewReader(input), &out)
	return out.String(), err
}

// A new PC: setup puts the folder on PATH, starts the agent, sets the logon
// entry and pairs, in that order, and ends with the once-per-PC tips.
func TestSetupNewPC(t *testing.T) {
	f := newPC(t)
	out, err := runFake(f, "y\n")
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	want := []string{"path on " + f.dir, "start", "autostart on askorus-agent.exe", "pair"}
	if !slices.Equal(f.calls, want) {
		t.Errorf("calls = %q, want %q", f.calls, want)
	}
	if f.pairIn != "y\n" {
		t.Errorf("pair read %q; it must get the input setup did not read", f.pairIn)
	}
	for _, s := range []string{"[1/5] This PC / 이 PC", "[2/5] AI programs / AI 프로그램", "Claude Code 2.1.319 found", "Codex 0.163.0 found (asks)",
		"OpenCode not found (optional", "[5/5] Pair the phone / 폰과 짝 맺기", "claude --rc", "askorus-agent check", "설정을 마쳤습니다"} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
	if strings.Contains(out, "only one AI program") {
		t.Errorf("two AIs, yet it warns of one:\n%s", out)
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

// Run again on a PC that is set up: it changes nothing and does not pair
// again.
func TestSetupAgainChangesNothing(t *testing.T) {
	f := newPC(t)
	f.onPath, f.listening, f.paired = true, true, 1
	f.auto = autostartCommand(filepath.Join(f.dir, "askorus-agent.exe"), false)
	out, err := runFake(f, "")
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	if len(f.calls) != 0 {
		t.Errorf("calls = %q, want none", f.calls)
	}
	if !strings.Contains(out, "askorus-agent pair") {
		t.Errorf("no word on adding a phone:\n%s", out)
	}
}

// A logon entry left by a copy in another folder is pointed at this exe
// (2호's review, 11-10): it would start that copy, or nothing.
func TestSetupFixesAnOldLogonEntry(t *testing.T) {
	f := newPC(t)
	f.onPath, f.listening, f.paired = true, true, 1
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
	noNet := tailnetInfo{problem: fail("tailscale is not connected", "Tailscale이 연결되어 있지 않습니다")}
	noHTTPS := servedNet()
	noHTTPS.status.HTTPS = false
	funnel := servedNet()
	funnel.serve.Funnel = true
	for name, c := range map[string]struct {
		set   func(*fakeSetup)
		says  string
		calls []string
	}{
		"administrator": {func(f *fakeSetup) { f.elevated = true }, "관리자 권한", nil},
		"temporary":     {func(f *fakeSetup) { f.dir = filepath.Join(os.TempDir(), "x") }, "looks temporary", nil},
		"no AI at all": {func(f *fakeSetup) {
			f.claude.Available, f.others = false, []namedInfo{{name: "Codex"}, {name: "OpenCode"}}
		}, "Claude Code, Codex or OpenCode", []string{"path on"}},
		"elevated svc":   {func(f *fakeSetup) { f.claude.Notices = []string{claude.NoticeServiceElevated} }, "claude daemon stop --any", []string{"path on"}},
		"no tailscale":   {func(f *fakeSetup) { f.nets = []tailnetInfo{noNet} }, "tailscale.com/download", []string{"path on"}},
		"no https":       {func(f *fakeSetup) { f.nets = []tailnetInfo{noHTTPS} }, "HTTPS Certificates", []string{"path on"}},
		"funnel":         {func(f *fakeSetup) { f.nets = []tailnetInfo{funnel} }, "funnel --https=8790 off", []string{"path on"}},
		"port taken":     {func(f *fakeSetup) { f.listening, f.other = true, true }, "another program already uses 127.0.0.1:8790", []string{"path on"}},
		"serve not done": {func(f *fakeSetup) { f.nets = []tailnetInfo{{status: servedNet().status}} }, "tailscale serve --bg --https=8790 http://127.0.0.1:8790", []string{"path on"}},
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
				got = append(got, strings.Fields(call)[0]+" "+strings.Fields(call)[1])
			}
			if len(c.calls) == 0 && len(got) != 0 || len(c.calls) > 0 && !slices.Equal(got, c.calls) {
				t.Errorf("calls = %q, want %q", f.calls, c.calls)
			}
		})
	}
}

// Without tailscale serve, setup shows the setting and asks (2026-11-09
// decision): y turns it on and looks again; Just Enter, or y when tailscale
// fails, has the person run it and press Enter. It turns serve on once at
// most, and pair gets the input after.
func TestSetupServe(t *testing.T) {
	notServed := tailnetInfo{status: servedNet().status}
	for name, c := range map[string]struct {
		input    string
		nets     []tailnetInfo
		serveErr error
		calls    []string // serve calls only
		looks    int
		says     string
	}{
		"y turns it on":    {"y\ny\n", []tailnetInfo{notServed, servedNet()}, nil, []string{"serve 8790->8790"}, 2, "켰습니다. 다시 확인합니다"},
		"Enter: yourself":  {"\n\ny\n", []tailnetInfo{notServed, servedNet()}, nil, nil, 2, "다른 PowerShell 창에서 실행하세요"},
		"no: yourself":     {"n\n\ny\n", []tailnetInfo{notServed, servedNet()}, nil, nil, 2, "다른 PowerShell 창에서 실행하세요"},
		"y but it fails":   {"y\n\ny\n", []tailnetInfo{notServed, servedNet()}, errors.New("tailscale serve: exit status 1: Serve is not enabled on your tailnet. To enable, visit: https://login.tailscale.com/f/serve"), []string{"serve 8790->8790"}, 2, "login.tailscale.com/f/serve"},
		"on but not there": {"y\n\ny\n", []tailnetInfo{notServed, notServed, servedNet()}, nil, []string{"serve 8790->8790"}, 3, "다른 PowerShell 창에서 실행하세요"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newPC(t)
			f.nets, f.serveErr = c.nets, c.serveErr
			out, err := runFake(f, c.input)
			if err != nil {
				t.Fatalf("setup: %v\n%s", err, out)
			}
			var serves []string
			for _, call := range f.calls {
				if strings.HasPrefix(call, "serve ") {
					serves = append(serves, call)
				}
			}
			if !slices.Equal(serves, c.calls) || f.looks != c.looks {
				t.Errorf("serve calls = %q, looks = %d; want %q, %d", serves, f.looks, c.calls, c.looks)
			}
			for _, s := range []string{"tailscale serve --bg --https=8790 http://127.0.0.1:8790", "not the internet", "[y/N]: ", c.says} {
				if !strings.Contains(out, s) {
					t.Errorf("output lacks %q:\n%s", s, out)
				}
			}
			if f.pairIn != "y\n" {
				t.Errorf("pair read %q, want the line after setup's", f.pairIn)
			}
		})
	}
}
