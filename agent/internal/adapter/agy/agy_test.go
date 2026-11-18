package agy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

// The test binary doubles as a fake agy (internal/fakecli).
func TestMain(m *testing.M) {
	if fakecli.Active() {
		os.Exit(fakecli.Main(os.Args[1:]))
	}
	os.Exit(m.Run())
}

const wantAskSettings = `{"agentMode":"","toolPermission":"request-review","allowNonWorkspaceAccess":false,"disableSlashCommands":true,` +
	`"permissions":{"allow":[],"deny":["command(*)","unsandboxed(*)","read_file(*)","write_file(*)","execute_url(*)","download_url(*)","mcp(*)"]}}`

// TestLockSettings pins the deny rules: what the 2026-11-18 probes ran
// with. Every run denies commands, writes, the browser, downloads and MCP;
// /usage keeps slash commands on.
func TestLockSettings(t *testing.T) {
	if askSettings != wantAskSettings {
		t.Fatalf("askSettings = %s", askSettings)
	}
	usage := lockSettings(true, false)
	if usage != wantAskSettings {
		t.Errorf("lockSettings(true, false) = %s", usage)
	}
	slash := lockSettings(true, true)
	if !strings.Contains(slash, `"disableSlashCommands":false`) || !strings.Contains(slash, `"read_file(*)"`) {
		t.Errorf("lockSettings(true, true) = %s", slash)
	}
	reads := lockSettings(false, false)
	if strings.Contains(reads, "read_file") || !strings.Contains(reads, `"command(*)"`) || !strings.Contains(reads, `"write_file(*)"`) {
		t.Errorf("lockSettings(false, false) = %s", reads)
	}
	for _, s := range []string{askSettings, slash, reads} {
		var v struct {
			ToolPermission string `json:"toolPermission"`
			Permissions    struct {
				Allow []string `json:"allow"`
			} `json:"permissions"`
		}
		if err := json.Unmarshal([]byte(s), &v); err != nil || v.ToolPermission != "request-review" || v.Permissions.Allow == nil || len(v.Permissions.Allow) != 0 {
			t.Errorf("%s: %+v %v", s, v, err)
		}
	}
}

func TestArgs(t *testing.T) {
	got := askArgs("gemini-3.8-flash-low", "")
	want := []string{"--agent", "askorus-ask", "--model", "gemini-3.8-flash-low", "--input-format", "stream-json",
		"--output-format", "stream-json", "--disable-slash-commands", "--settings", wantAskSettings}
	if !slices.Equal(got, want) {
		t.Fatalf("askArgs = %q", got)
	}
	conv := "0f3b5391-6d93-4cda-96c7-c453a27fc063"
	resumed := askArgs("gemini-3.8-flash-high", conv)
	if !slices.Equal(resumed[4:6], []string{"--conversation", conv}) {
		t.Errorf("resumed = %q", resumed)
	}
	for _, ok := range [][]string{got, resumed} {
		if err := checkArgs(ok); err != nil {
			t.Errorf("checkArgs(%q) = %v", ok, err)
		}
	}
	bad := map[string][]string{
		"bypass":         append(slices.Clone(got), "--dangerously-skip-permissions"),
		"mode":           append(slices.Clone(got), "--mode", "auto"),
		"other agent":    slices.Concat([]string{"--agent", "default"}, got[2:]),
		"claude model":   askArgs("claude-sonnet-4-6", ""),
		"flag model":     askArgs("-p", ""),
		"upper model":    askArgs("Gemini-3.8-Flash", ""),
		"bad conv":       askArgs("gemini-3.8-flash-low", "../x"),
		"other settings": slices.Concat(got[:len(got)-1], []string{lockSettings(false, false)}),
		"prompt":         append(slices.Clone(got), "-p", "hello"),
		"swapped":        slices.Concat(got[2:4], got[0:2], got[4:]),
		"empty":          nil,
	}
	for name, args := range bad {
		if err := checkArgs(args); err == nil {
			t.Errorf("%s: checkArgs(%q) accepted", name, args)
		}
	}
}

func TestModels(t *testing.T) {
	if got := cliModel("gemini-3.8-flash", "low"); got != "gemini-3.8-flash-low" {
		t.Errorf("cliModel = %q", got)
	}
	if got := cliModel("gemini-3.8-flash-high", ""); got != "gemini-3.8-flash-high" {
		t.Errorf("cliModel without level = %q", got)
	}
	a := &Adapter{}
	tiers := a.AskTiers()
	for tier, want := range map[string]string{core.TierQuick: "low", core.TierStandard: "medium", core.TierDeep: "high"} {
		if m := tiers[tier]; m.Model != "gemini-3.8-flash" || m.Effort != want {
			t.Errorf("%s = %+v", tier, m)
		}
	}
	if e := a.AskEfforts()["gemini-3.8-flash"]; !slices.Equal(e, []string{"low", "medium", "high"}) {
		t.Errorf("efforts = %q", e)
	}
	// A model of the settings' own brings its own level, or none.
	a = &Adapter{tiers: map[string]core.AskModel{core.TierDeep: {Model: "gemini-4-hydrogen-xhigh"}, core.TierQuick: {Effort: "medium"}}}
	tiers = a.AskTiers()
	if m := tiers[core.TierDeep]; m.Model != "gemini-4-hydrogen-xhigh" || m.Effort != "" {
		t.Errorf("deep = %+v", m)
	}
	if m := tiers[core.TierQuick]; m.Model != "gemini-3.8-flash" || m.Effort != "medium" {
		t.Errorf("quick = %+v", m)
	}
	if got := (&Adapter{}).DisplayName(); got != "Antigravity" {
		t.Errorf("DisplayName = %q", got)
	}
}

func TestStdinMessage(t *testing.T) {
	q := "줄 1: \"따옴표\" \\ & | < > ^ % !\n-p --dangerously-skip-permissions"
	line := stdinMessage(q)
	if strings.Count(line, "\n") != 1 || !strings.HasSuffix(line, "\n") {
		t.Fatalf("not one line: %q", line)
	}
	var m struct {
		Event   string `json:"event"`
		Message struct {
			Role    string `json:"role"`
			Content []struct{ Type, Text string }
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &m); err != nil || m.Event != "user" || m.Message.Role != "user" ||
		len(m.Message.Content) != 1 || m.Message.Content[0].Type != "text" || m.Message.Content[0].Text != q {
		t.Errorf("message = %+v %v", m, err)
	}
}

func TestStripFileLinks(t *testing.T) {
	for in, want := range map[string]string{
		"See [photo-1.jpg](file:///C:/Users/someone/AppData/photo-1.jpg) here.": "See photo-1.jpg here.",
		"Path file:///C:/Users/someone/AppData/photo-2.jpg ends":                "Path photo-2.jpg ends",
		"Angle <file:///home/someone/x/photo-3.png>.":                           "Angle photo-3.png.",
		"Back [a](file:///C:\\Users\\someone\\b.jpg)":                           "Back a",
		"Web [go](https://go.dev/doc) stays":                                    "Web [go](https://go.dev/doc) stays",
		"No link at all":                                                        "No link at all",
	} {
		if got := stripFileLinks(in); got != want {
			t.Errorf("stripFileLinks(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAgentFile(t *testing.T) {
	a := New(Options{Path: filepath.Join(t.TempDir(), "none.exe")})
	ask := t.TempDir()
	dir, err := a.askFolder(ask)
	if err != nil || dir != filepath.Join(ask, FolderName) {
		t.Fatalf("askFolder = %q %v", dir, err)
	}
	p := filepath.Join(dir, ".agents", "agents", askAgent+".md")
	b, err := os.ReadFile(p)
	if err != nil || string(b) != askAgentFile {
		t.Fatalf("agent file = %q %v", b, err)
	}
	if !strings.Contains(askAgentFile, "\ntools: [search_web]\n") || !strings.Contains(askAgentFile, "\nsubagent: false\n") {
		t.Errorf("agent file content = %q", askAgentFile)
	}
	if _, err := a.askFolder(ask); err != nil || a.writes != 1 {
		t.Errorf("second askFolder = %v, writes %d", err, a.writes)
	}
	// A file changed under a running agent is never run.
	if err := os.WriteFile(p, []byte(strings.Replace(askAgentFile, "[search_web]", "[search_web, run_command]", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.askFolder(ask); core.ErrorCode(err) != core.ErrCodeInternal {
		t.Errorf("changed file = %v", err)
	}
	// A new agent writes it right again.
	b2 := New(Options{Path: filepath.Join(t.TempDir(), "none.exe")})
	if _, err := b2.askFolder(ask); err != nil {
		t.Errorf("new agent = %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != askAgentFile {
		t.Errorf("rewritten = %q", b)
	}
	if _, err := a.askFolder(filepath.Join(ask, "missing")); core.ErrorCode(err) != core.ErrCodeInternal {
		t.Errorf("missing folder = %v", err)
	}
}

func TestFindExe(t *testing.T) {
	if _, err := findExe(filepath.Join(t.TempDir(), "agy.cmd")); err == nil {
		t.Error("a .cmd was accepted")
	}
	if runtime.GOOS != "windows" {
		return
	}
	base := t.TempDir()
	p := filepath.Join(base, "agy", "bin", "agy.exe")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("MZ"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("LOCALAPPDATA", base)
	if got, err := findExe(""); err != nil || got != p {
		t.Errorf("findExe = %q %v", got, err)
	}
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if _, err := findExe(""); err == nil {
		t.Error("found an agy that is not there")
	}
}

// fakeAgy makes an adapter around the fake agy, with an ask folder.
func fakeAgy(t *testing.T) (*Adapter, string) {
	t.Helper()
	exe := fakecli.Use(t, fakecli.RoleAgy)
	return New(Options{Path: exe}), t.TempDir()
}

func ask(t *testing.T, a *Adapter, req core.AskRequest) (core.AskResult, error) {
	t.Helper()
	if req.Prompt == "" {
		req.Prompt = "preamble\n\n세 줄 질문"
	}
	if req.Tier == "" {
		req.Tier = core.TierStandard
	}
	return a.Ask(context.Background(), req)
}

func TestAsk(t *testing.T) {
	a, folder := fakeAgy(t)
	prompt := "preamble\n\n-p --dangerously-skip-permissions & \"질문\""
	r, err := ask(t, a, core.AskRequest{Folder: folder, Prompt: prompt})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.Answer, "fake agy: ") || !conversationRe.MatchString(r.SessionID) || r.Model != "gemini-3.8-flash-medium" || r.InputTokens != 2100 || r.OutputTokens != 40 {
		t.Errorf("result = %+v", r)
	}
	calls := fakecli.Calls(t)
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	c := calls[0]
	if c.Dir != filepath.Join(folder, FolderName) {
		t.Errorf("dir = %q", c.Dir)
	}
	if !slices.Equal(c.Args, askArgs("gemini-3.8-flash-medium", "")) {
		t.Errorf("args = %q", c.Args)
	}
	for _, arg := range c.Args {
		if strings.Contains(arg, "질문") || strings.Contains(arg, "preamble") {
			t.Errorf("the question is in an argument: %q", arg)
		}
	}
	if c.Stdin != stdinMessage(prompt) {
		t.Errorf("stdin = %q", c.Stdin)
	}

	// A follow-up carries the conversation on; quick and deep pick their models.
	r2, err := ask(t, a, core.AskRequest{Folder: folder, Resume: r.SessionID, Tier: core.TierQuick})
	if err != nil || r2.SessionID != r.SessionID || r2.Model != "gemini-3.8-flash-low" {
		t.Errorf("follow-up = %+v %v", r2, err)
	}
	if c := fakecli.Calls(t); !slices.Equal(c[1].Args, askArgs("gemini-3.8-flash-low", r.SessionID)) {
		t.Errorf("follow-up args = %q", c[1].Args)
	}
	// The app's own pick of model and level.
	r3, err := ask(t, a, core.AskRequest{Folder: folder, Model: "gemini-3.8-flash", Effort: "high"})
	if err != nil || r3.Model != "gemini-3.8-flash-high" {
		t.Errorf("own pick = %+v %v", r3, err)
	}
	// A one-off ask (a debate turn) carries nothing on.
	r4, err := ask(t, a, core.AskRequest{Folder: folder, OneOff: true})
	if err != nil || r4.SessionID != "" || r4.Answer == "" {
		t.Errorf("one-off = %+v %v", r4, err)
	}
}

func TestAskRefusedBeforeRunning(t *testing.T) {
	a, folder := fakeAgy(t)
	im := core.Image{Path: filepath.Join(folder, "up_0123456789abcdef0123456789abcdef.jpg"), Type: core.ImageJPEG}
	for name, c := range map[string]struct {
		req  core.AskRequest
		code string
	}{
		"claude model": {core.AskRequest{Model: "claude-sonnet-4-6"}, core.ErrCodeBadModel},
		"flag model":   {core.AskRequest{Model: "--mode=auto"}, core.ErrCodeBadModel},
		"bad effort":   {core.AskRequest{Effort: "ultra"}, core.ErrCodeBadModel},
		"bad resume":   {core.AskRequest{Resume: "ses_1"}, core.ErrCodeNotFound},
		"one-off on":   {core.AskRequest{Resume: "0f3b5391-6d93-4cda-96c7-c453a27fc063", OneOff: true}, core.ErrCodeInternal},
		"photo":        {core.AskRequest{Images: []core.Image{im}}, core.ErrCodeNoImages},
		"no folder":    {core.AskRequest{Folder: filepath.Join(folder, "missing")}, core.ErrCodeInternal},
	} {
		if c.req.Folder == "" {
			c.req.Folder = folder
		}
		if _, err := ask(t, a, c.req); core.ErrorCode(err) != c.code {
			t.Errorf("%s: %v, want %s", name, err, c.code)
		}
	}
	if n := len(fakecli.Calls(t)); n != 0 {
		t.Errorf("%d runs", n)
	}
	missing := New(Options{Path: filepath.Join(folder, "none.exe")})
	if _, err := ask(t, missing, core.AskRequest{Folder: folder}); core.ErrorCode(err) != core.ErrCodeAgentUnavailable {
		t.Errorf("missing agy = %v", err)
	}
}

// TestAskModes: what agy prints in each case, and what the ask makes of it.
func TestAskModes(t *testing.T) {
	for _, c := range []struct {
		agy, ask string
		code     string
		detail   string
		answer   string
	}{
		{agy: fakecli.AgyRetry, answer: "fake agy: "},
		{agy: fakecli.AgyLinks, answer: "See photo-1.jpg and photo-1.jpg. fake agy: "},
		{agy: fakecli.AgyCapacity, code: core.ErrCodeAskFailed, detail: "No capacity available"},
		{agy: fakecli.AgyBadTool, code: core.ErrCodeAskFailed, detail: "run_command"},
		{agy: fakecli.AgyWrongAgent, code: core.ErrCodeInternal},
		{agy: fakecli.AgyOpenMode, code: core.ErrCodeInternal},
		{agy: fakecli.AgyNoInit, code: core.ErrCodeInternal},
		{agy: fakecli.AgyDenied, code: core.ErrCodeAskFailed, detail: "may not read"},
		{ask: fakecli.AskEmpty, code: core.ErrCodeAskEmpty},
		{ask: fakecli.AskSignedOut, code: core.ErrCodeSignedOut, detail: LoginCommand},
		{ask: fakecli.AskFail, code: core.ErrCodeUsageLimit, detail: fakecli.AgyResetTime},
	} {
		t.Run(c.agy+c.ask, func(t *testing.T) {
			a, folder := fakeAgy(t)
			t.Setenv(fakecli.EnvAgy, c.agy)
			t.Setenv(fakecli.EnvAsk, c.ask)
			r, err := ask(t, a, core.AskRequest{Folder: folder})
			if c.code == "" {
				if err != nil || !strings.HasPrefix(r.Answer, c.answer) || r.SessionID == "" {
					t.Errorf("= %+v %v", r, err)
				}
				if strings.Contains(r.Answer, "file:") || strings.Contains(r.Answer, "someone") {
					t.Errorf("a path reached the answer: %q", r.Answer)
				}
				return
			}
			var ae *core.ActionError
			if !errors.As(err, &ae) || ae.Code != c.code || !strings.Contains(ae.Detail, c.detail) {
				t.Errorf("= %+v %v, want %s with %q", r, err, c.code, c.detail)
			}
			if r.Answer != "" {
				t.Errorf("an answer came with the error: %q", r.Answer)
			}
		})
	}
}

func TestAskNoConversation(t *testing.T) {
	a, folder := fakeAgy(t)
	t.Setenv(fakecli.EnvAgy, fakecli.AgyNoResume)
	if _, err := ask(t, a, core.AskRequest{Folder: folder, Resume: "0f3b5391-6d93-4cda-96c7-c453a27fc063"}); core.ErrorCode(err) != core.ErrCodeNotFound {
		t.Errorf("unknown conversation = %v", err)
	}
}

func TestInfoAndUsage(t *testing.T) {
	a, _ := fakeAgy(t)
	info := a.Info(context.Background())
	if !info.Available || info.Version != "1.5.10" || !slices.Equal(info.Capabilities, []core.Capability{core.CapAsk}) || len(info.Notices) != 0 {
		t.Errorf("info = %+v", info)
	}
	u := a.Usage(context.Background())
	if len(u) != 2 || u[0].Name != "Gemini Models" || u[0].Left != 0.94 || u[0].ResetAt != fakecli.AgyResetTime || u[1].Left != 1 {
		t.Errorf("usage = %+v", u)
	}
	if n := len(fakecli.Calls(t)); n != 0 {
		t.Errorf("the checks were recorded as %d runs", n)
	}

	out, _ := fakeAgy(t)
	t.Setenv(fakecli.EnvSignIn, fakecli.SignedOut)
	info = out.Info(context.Background())
	if !info.Available || !slices.Contains(info.Notices, NoticeSignedOut) {
		t.Errorf("signed out info = %+v", info)
	}
	if u := out.Usage(context.Background()); u != nil {
		t.Errorf("signed out usage = %+v", u)
	}

	missing := New(Options{Path: filepath.Join(t.TempDir(), "none.exe")})
	if info := missing.Info(context.Background()); info.Available || !slices.Equal(info.Notices, []string{NoticeNotFound}) {
		t.Errorf("missing info = %+v", info)
	}
}

func TestReadUsageGuards(t *testing.T) {
	// A /usage that started a turn, or answered another command, says nothing.
	for _, out := range []string{
		`{"conversation_id":"0f3b5391-6d93-4cda-96c7-c453a27fc063","status":"SUCCESS","response":"Gemini 94%"}`,
		`{"conversation_id":"","status":"SUCCESS","command":{"name":"help","data":{}}}`,
		`not json`,
	} {
		if st := readUsage(procResult(out, "")); st.known {
			t.Errorf("readUsage(%s) = %+v", out, st)
		}
	}
	if st := readUsage(procResult("", `AGY_ERROR: {"short_error":"x","status":"UNAUTHENTICATED"}`)); !st.known || st.signedIn {
		t.Errorf("unauthenticated = %+v", st)
	}
}

func TestFailure(t *testing.T) {
	for _, c := range []struct{ msg, status, code string }{
		{"invalid model selection (--model \"x\")", "", core.ErrCodeBadModel},
		{"API error: UNAUTHENTICATED (code 401)", "", core.ErrCodeSignedOut},
		{"anything", "UNAUTHENTICATED", core.ErrCodeSignedOut},
		{"You have reached your weekly limit", "", core.ErrCodeUsageLimit},
		{"x", "RESOURCE_EXHAUSTED", core.ErrCodeUsageLimit},
		{fakecli.CapacityAgy, "UNAVAILABLE", core.ErrCodeAskFailed},
		{"", "", core.ErrCodeAskFailed},
	} {
		if got := core.ErrorCode(failure(c.msg, c.status, 1)); got != c.code {
			t.Errorf("failure(%q, %q) = %s, want %s", c.msg, c.status, got, c.code)
		}
	}
}

func procResult(stdout, stderr string) proc.Result {
	return proc.Result{Stdout: []byte(stdout), Stderr: []byte(stderr)}
}
