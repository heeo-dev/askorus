package claude

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

func testAdapter(t *testing.T) *Adapter {
	t.Helper()
	r, _ := fakeRunner(t)
	agents, _ := filepath.Abs(filepath.Join("testdata", "agents.json"))
	t.Setenv("ASKORUS_FAKE_AGENTS", agents)
	claudeDir := filepath.Dir(copyProjects(t))
	a := New(Options{ClaudeDir: claudeDir, ClaudePath: r.Path()})
	a.folderExists = func(string) bool { return true } // testdata folders are from another PC
	return a
}

func sessionsByID(ss []core.Session) map[string]core.Session {
	m := map[string]core.Session{}
	for _, s := range ss {
		m[s.ID] = s
	}
	return m
}

func TestListSessions(t *testing.T) {
	a := testAdapter(t)
	res, err := a.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v", res.Warnings)
	}
	m := sessionsByID(res.Sessions)
	if len(m) != 5 {
		t.Fatalf("got %d sessions, want 5 (4 transcripts + 1 new bg session; bad id dropped)", len(m))
	}
	check := func(id string, state core.State, attach string) core.Session {
		t.Helper()
		s, ok := m["claude:"+id]
		if !ok {
			t.Fatalf("missing %s", id)
		}
		if s.Agent != "claude" || s.State != state || s.AttachCommand != attach {
			t.Errorf("%s: agent=%q state=%q attach=%q, want %q %q", id, s.Agent, s.State, s.AttachCommand, state, attach)
		}
		if !slices.Equal(s.OpenModes, []string{core.OpenModeOfficial}) {
			t.Errorf("%s: openModes = %v", id, s.OpenModes)
		}
		return s
	}
	s1 := check(id1, core.StateRunning, `claude attach "login job"`)
	if s1.Activity != "working" || s1.Title != "Login fix" || s1.Name != "login job" {
		t.Errorf("running bg session: %+v", s1)
	}
	check(id2, core.StateStopped, "claude attach named-bg")
	s5 := check(id5, core.StateTerminal, "")
	if !slices.Contains(s5.Badges, "desktop") {
		t.Errorf("desktop badge missing: %v", s5.Badges)
	}
	check(id6, core.StateHistory, "claude --resume "+id6)
	s7 := check(id7, core.StateRunning, "claude attach \"brand new\"")
	if s7.Title != "brand new" || s7.Folder != `C:\work\new` || s7.LastActive.IsZero() || s7.Activity != "blocked" {
		t.Errorf("bg session without transcript: %+v", s7)
	}
}

func TestListSessionsWithoutAgents(t *testing.T) {
	a := testAdapter(t)
	t.Setenv("ASKORUS_FAKE_EXIT", "1")
	res, err := a.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(res.Warnings, WarnAgentsUnavailable) {
		t.Errorf("warnings = %v", res.Warnings)
	}
	for _, s := range res.Sessions {
		if s.State != core.StateUnknown || s.AttachCommand != "" {
			t.Errorf("%s: state %q attach %q, want unknown and no command", s.ID, s.State, s.AttachCommand)
		}
	}
}

func TestInfo(t *testing.T) {
	a := testAdapter(t)
	t.Setenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "")
	t.Setenv("DISABLE_GROWTHBOOK", "")
	// A phone request that went away first is not cached as a broken claude.
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	a.Info(gone)
	info := a.Info(context.Background())
	wantCaps := []core.Capability{core.CapList, core.CapPreview, core.CapStart, core.CapResume, core.CapStop, core.CapRemove, core.CapOpenOfficial, core.CapAsk}
	if ptySupported() {
		wantCaps = append(wantCaps, core.CapServe)
	}
	if !info.Available || info.Version != "2.1.310" || !slices.Equal(info.Capabilities, wantCaps) || len(info.Notices) != 0 {
		t.Errorf("info = %+v", info)
	}
	t.Setenv("DISABLE_GROWTHBOOK", "1")
	if info := a.Info(context.Background()); !slices.Contains(info.Notices, NoticeRCDisabledEnv) {
		t.Errorf("notices = %v", info.Notices)
	}

	missing := New(Options{ClaudeDir: t.TempDir(), ClaudePath: filepath.Join(t.TempDir(), "claude.cmd")})
	if info := missing.Info(context.Background()); info.Available || !slices.Contains(info.Notices, NoticeNotFound) || !slices.Equal(info.Capabilities, []core.Capability{core.CapList, core.CapPreview}) {
		t.Errorf("missing claude info = %+v", info)
	}
	if res, err := missing.ListSessions(context.Background()); err != nil || !slices.Contains(res.Warnings, WarnAgentsUnavailable) {
		t.Errorf("missing claude list = %+v, %v", res, err)
	}
}

// N10: the adapter reads only the projects folder. Login files placed next
// to it must not affect anything, and no API output may mention them.
func TestIgnoresFilesOutsideProjects(t *testing.T) {
	a := testAdapter(t)
	claudeDir := filepath.Dir(a.transcripts.Root)
	os.WriteFile(filepath.Join(claudeDir, ".credentials.json"), []byte(`{"secret":"x"}`), 0o600)
	os.WriteFile(filepath.Join(claudeDir, "stray.jsonl"), []byte(`{"type":"ai-title","aiTitle":"stray"}`), 0o600)
	res, _ := a.ListSessions(context.Background())
	for _, s := range res.Sessions {
		if s.Title == "stray" {
			t.Error("read a file outside projects")
		}
	}
}

func TestAgentsCache(t *testing.T) {
	r, argsFile := fakeRunner(t)
	agents, _ := filepath.Abs(filepath.Join("testdata", "agents.json"))
	t.Setenv("ASKORUS_FAKE_AGENTS", agents)
	a := New(Options{ClaudeDir: filepath.Dir(copyProjects(t)), ClaudePath: r.Path()})
	ctx := context.Background()
	runs := func() int { return len(recordedArgs(t, argsFile)) }

	a.ListSessions(ctx)
	a.ListSessions(ctx)
	if n := runs(); n != 1 {
		t.Errorf("fresh cache: claude ran %d times, want 1", n)
	}

	a.Invalidate()
	a.ListSessions(ctx)
	if n := runs(); n != 2 {
		t.Errorf("after Invalidate: claude ran %d times, want 2", n)
	}

	// An older result is served at once and refreshed in the background.
	a.amu.Lock()
	a.agentsAt = a.agentsAt.Add(-10 * time.Second)
	a.amu.Unlock()
	if res, _ := a.ListSessions(ctx); len(res.Warnings) != 0 {
		t.Errorf("stale result not used: %v", res.Warnings)
	}
	deadline := time.Now().Add(10 * time.Second)
	for runs() < 3 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := runs(); n != 3 {
		t.Errorf("background refresh: claude ran %d times, want 3", n)
	}
}

// R-86 (security review 11-08): a session name is pasted into cmd,
// PowerShell or sh, so a name any of them would run or expand gives way to
// the short id.
func TestAttachCommand(t *testing.T) {
	const short = "cf6efa4e"
	cases := map[string]string{
		"job":                       "claude attach job",
		"named-bg":                  "claude attach named-bg",
		"v1.2_fix":                  "claude attach v1.2_fix",
		"my job":                    `claude attach "my job"`,
		"새 작업":                      `claude attach "새 작업"`,
		"로그인":                       "claude attach 로그인",
		"a&b":                       `claude attach "a&b"`,
		"fix (login), part 2: #12":  `claude attach "fix (login), part 2: #12"`,
		"$(Start-Process calc.exe)": "claude attach " + short,
		"`calc`":                    "claude attach " + short,
		"a\"b":                      "claude attach " + short,
		"100% done":                 "claude attach " + short,
		"%PATH%":                    "claude attach " + short,
		"hi!":                       "claude attach " + short,
		`C:\work`:                   "claude attach " + short,
		"a;b":                       "claude attach " + short,
		"a|b":                       "claude attach " + short,
		"a<b":                       "claude attach " + short,
		"x\ny":                      "claude attach " + short,
		"“smart”":                   "claude attach " + short,
		"‘smart’":                   "claude attach " + short,
		"＂wide＂":                    "claude attach " + short,
		// Best-fit conversion (Codex cross-review 11-08): modifier
		// letters, combining marks and click letters become " ` | ! in
		// a program that reads its command line through ANSI functions.
		"xʺ & calc & ʺ": "claude attach " + short,
		"aˋb":           "claude attach " + short,
		"àb":           "claude attach " + short,
		"a̎b":           "claude attach " + short,
		"aǀb":           "claude attach " + short,
		"aǃb":           "claude attach " + short,
		"café crème":    `claude attach "café crème"`,
		"日本語の作業":        "claude attach 日本語の作業",
		"Задача":        "claude attach Задача",
		"-x":            "claude attach " + short,
		"--help":        "claude attach " + short,
		"":              "claude attach " + short,
	}
	for name, want := range cases {
		if got := attachCommand(name, short, "55555555-5555-4555-8555-555555555555"); got != want {
			t.Errorf("attachCommand(%q) = %q, want %q", name, got, want)
		}
	}
	// Without a plain short id the session id is next, and then nothing.
	if got := attachCommand("$(x)", "", "55555555-5555-4555-8555-555555555555"); got != "claude attach 55555555-5555-4555-8555-555555555555" {
		t.Errorf("no short id: %q", got)
	}
	if got := attachCommand("$(x)", "$(y)", "-z"); got != "" {
		t.Errorf("nothing plain: %q, want no command", got)
	}
}

func TestPlainID(t *testing.T) {
	for _, ok := range []string{"cf6efa4e", "55555555-5555-4555-8555-555555555555", "a_b"} {
		if !plainID(ok) {
			t.Errorf("plainID(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "-x", "a b", "a$b", "a\"b", "가"} {
		if plainID(bad) {
			t.Errorf("plainID(%q) = true", bad)
		}
	}
}
