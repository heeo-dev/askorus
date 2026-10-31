package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/proc"
)

// The test binary doubles as a fake codex. The judge tests use the shared
// fake (internal/fakecli). The ask tests keep their own: with
// ASKSEMBLE_FAKE_CODEX=1 it records {args, cwd, stdin} to
// ASKSEMBLE_FAKE_ARGS and prints what `codex exec --json` prints.
// ASKSEMBLE_FAKE_MODE: "badmodel", "noresume", "nothing" (exit 0, no turn).
func TestMain(m *testing.M) {
	if fakecli.Active() {
		os.Exit(fakecli.Main(os.Args[1:]))
	}
	if os.Getenv("ASKSEMBLE_FAKE_CODEX") == "1" {
		os.Exit(fakeCodex(os.Args[1:]))
	}
	os.Exit(m.Run())
}

type fakeCall struct {
	Args  []string `json:"args"`
	Cwd   string   `json:"cwd"`
	Stdin string   `json:"stdin"`
}

func fakeCodex(args []string) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("codex-cli 0.164.0")
		return 0
	}
	in, _ := io.ReadAll(os.Stdin)
	cwd, _ := os.Getwd()
	b, _ := json.Marshal(fakeCall{Args: args, Cwd: cwd, Stdin: string(in)})
	if f, err := os.OpenFile(os.Getenv("ASKSEMBLE_FAKE_ARGS"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		f.Write(append(b, '\n'))
		f.Close()
	}
	thread := "01a188b9-9dfe-7b92-b37c-d12a1eca2234"
	if slices.Contains(args, "resume") {
		thread = args[len(args)-2]
	}
	switch os.Getenv("ASKSEMBLE_FAKE_MODE") {
	case "noresume":
		fmt.Fprintf(os.Stderr, "Error: thread/resume: thread/resume failed: no rollout found for thread id %s (code -32600)\n", thread)
		return 1
	case "badmodel":
		inner := `{"type":"error","status":400,"error":{"type":"invalid_request_error","message":"The 'x' model is not supported when using Codex with a ChatGPT account.","code":"model_not_found"}}`
		q, _ := json.Marshal(inner)
		fmt.Printf("{\"type\":\"thread.started\",\"thread_id\":%q}\n", thread)
		fmt.Println(`{"type":"item.completed","item":{"id":"item_0","type":"error","message":"Model metadata for x not found."}}`)
		fmt.Printf("{\"type\":\"error\",\"message\":%s}\n{\"type\":\"turn.failed\",\"error\":{\"message\":%s}}\n", q, q)
		return 1
	case "nothing":
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(in)), "\n")
	fmt.Printf("{\"type\":\"thread.started\",\"thread_id\":%q}\r\n", thread)
	fmt.Println(`{"type":"turn.started"}`)
	fmt.Println(`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"확인하겠습니다."}}`)
	fmt.Println(`{"type":"item.completed","item":{"id":"item_1","type":"web_search","id":"exec-1","query":"q"}}`)
	a, _ := json.Marshal("fake: " + lines[len(lines)-1])
	fmt.Printf("{\"type\":\"item.completed\",\"item\":{\"id\":\"item_2\",\"type\":\"agent_message\",\"text\":%s}}\n", a)
	fmt.Println(`{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":80,"output_tokens":9}}`)
	return 0
}

func fakeAdapter(t *testing.T, tiers map[string]core.AskModel) (*Adapter, string, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(t.TempDir(), "args.jsonl")
	t.Setenv("ASKSEMBLE_FAKE_CODEX", "1")
	t.Setenv("ASKSEMBLE_FAKE_ARGS", argsFile)
	t.Setenv("ASKSEMBLE_FAKE_MODE", "")
	return New(Options{Path: exe, Tiers: tiers}), argsFile, t.TempDir()
}

func calls(t *testing.T, file string) []fakeCall {
	t.Helper()
	b, _ := os.ReadFile(file)
	var out []fakeCall
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l == "" {
			continue
		}
		var c fakeCall
		if err := json.Unmarshal([]byte(l), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func TestInfo(t *testing.T) {
	a, _, _ := fakeAdapter(t, nil)
	// A phone request that went away first must not leave codex out of
	// asks for the next five minutes.
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	a.Info(gone)
	info := a.Info(context.Background())
	if !info.Available || info.Version != "0.164.0" || !slices.Equal(info.Capabilities, []core.Capability{core.CapAsk}) || len(info.Notices) != 0 {
		t.Errorf("info = %+v", info)
	}
	missing := New(Options{Path: filepath.Join(t.TempDir(), "codex.exe")})
	if info := missing.Info(context.Background()); info.Available || len(info.Capabilities) != 0 || !slices.Contains(info.Notices, NoticeNotFound) {
		t.Errorf("missing info = %+v", info)
	}
	if res, err := a.ListSessions(context.Background()); err != nil || res.Sessions == nil || len(res.Sessions) != 0 {
		t.Errorf("sessions = %+v %v", res, err)
	}
}

func TestAskAndFollowUp(t *testing.T) {
	a, argsFile, folder := fakeAdapter(t, nil)
	prompt := core.AskPrompt("대한민국의 수도는?\n한 문장으로. 100% \"정확히\" & 짧게")
	res, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: prompt, Tier: core.TierStandard})
	if err != nil {
		t.Fatal(err)
	}
	c := calls(t, argsFile)[0]
	want := []string{"--search", "exec", "-s", "read-only", "--ignore-user-config", "--skip-git-repo-check", "--json",
		"-m", "gpt-6.1-terra", "-c", `model_reasoning_effort="medium"`, "-"}
	if !reflect.DeepEqual(c.Args, want) {
		t.Errorf("args = %q", c.Args)
	}
	if c.Stdin != prompt {
		t.Errorf("stdin = %q", c.Stdin)
	}
	if ec, _ := filepath.EvalSymlinks(c.Cwd); !strings.EqualFold(ec, mustEval(folder)) {
		t.Errorf("cwd = %q", c.Cwd)
	}
	// The last agent message is the answer; the lead-in is not.
	if res.Answer != `fake: 한 문장으로. 100% "정확히" & 짧게` || res.SessionID != "01a188b9-9dfe-7b92-b37c-d12a1eca2234" ||
		res.Model != "gpt-6.1-terra" || res.InputTokens != 100 || res.OutputTokens != 9 {
		t.Errorf("result = %+v", res)
	}
	res2, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "더", Tier: core.TierDeep, Effort: "high", Resume: res.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"--search", "exec", "-s", "read-only", "resume", "--ignore-user-config", "--skip-git-repo-check", "--json",
		"-m", "gpt-6.1-sol", "-c", `model_reasoning_effort="high"`, "-c", `sandbox_mode="read-only"`, res.SessionID, "-"}
	if got := calls(t, argsFile)[1].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("follow-up args = %q", got)
	}
	if res2.SessionID != res.SessionID {
		t.Errorf("follow-up thread = %s", res2.SessionID)
	}
}

func mustEval(p string) string {
	e, _ := filepath.EvalSymlinks(p)
	return e
}

func TestAskErrors(t *testing.T) {
	a, argsFile, folder := fakeAdapter(t, map[string]core.AskModel{core.TierQuick: {Model: "gpt-6-luna-mini"}})
	if m := a.AskTiers()[core.TierQuick]; m.Model != "gpt-6-luna-mini" || m.Effort != "medium" {
		t.Errorf("tier override = %+v", m)
	}
	ask := func(req core.AskRequest) error {
		req.Folder, req.Prompt, req.Tier = folder, "q", core.TierStandard
		_, err := a.Ask(context.Background(), req)
		return err
	}
	t.Setenv("ASKSEMBLE_FAKE_MODE", "badmodel")
	err := ask(core.AskRequest{Model: "x"})
	var ae *core.ActionError
	if core.ErrorCode(err) != core.ErrCodeBadModel || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("bad model: %v", err)
	}
	_ = ae
	t.Setenv("ASKSEMBLE_FAKE_MODE", "noresume")
	if err := ask(core.AskRequest{Resume: "01a188b9-9dfe-7b92-b37c-d12a1eca2235"}); core.ErrorCode(err) != core.ErrCodeNotFound {
		t.Errorf("unknown thread: %v", err)
	}
	t.Setenv("ASKSEMBLE_FAKE_MODE", "nothing")
	if err := ask(core.AskRequest{}); core.ErrorCode(err) != core.ErrCodeAskFailed {
		t.Errorf("no turn: %v", err)
	}
	t.Setenv("ASKSEMBLE_FAKE_MODE", "")
	n := len(calls(t, argsFile))
	for name, req := range map[string]core.AskRequest{
		"thread not a uuid": {Resume: "../../x"},
		"model as a flag":   {Model: "--dangerously-bypass-approvals-and-sandbox"},
		"model with quote":  {Model: `x"`},
		"unknown effort":    {Effort: "max"},
	} {
		if err := ask(req); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if got := len(calls(t, argsFile)); got != n {
		t.Errorf("refused asks ran codex %d times", got-n)
	}
	missing := New(Options{Path: filepath.Join(t.TempDir(), "codex.exe")})
	if _, err := missing.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q"}); core.ErrorCode(err) != core.ErrCodeAgentUnavailable {
		t.Errorf("missing codex: %v", err)
	}
}

func TestCheckArgs(t *testing.T) {
	good := askArgs("gpt-6.1-terra", "medium", "")
	if err := checkArgs(good); err != nil {
		t.Fatal(err)
	}
	if err := checkArgs(askArgs("gpt-6.1-terra", "", "01a188b9-9dfe-7b92-b37c-d12a1eca2234")); err != nil {
		t.Fatal(err)
	}
	ins := func(at int, extra ...string) []string { return slices.Insert(slices.Clone(good), at, extra...) }
	for name, args := range map[string][]string{
		"bypass":          ins(2, "--dangerously-bypass-approvals-and-sandbox"),
		"full auto":       ins(2, "--full-auto"),
		"workspace write": {"--search", "exec", "-s", "workspace-write", "--ignore-user-config", "--skip-git-repo-check", "--json", "-m", "gpt-6.1-terra", "-"},
		"approval":        ins(4, "-a", "never"),
		"profile":         ins(4, "--profile", "work"),
		"config key":      ins(len(good)-1, "-c", `approval_policy="never"`),
		"user config":     {"--search", "exec", "-s", "read-only", "--skip-git-repo-check", "--json", "-m", "gpt-6.1-terra", "-"},
		"prompt argument": append(slices.Clone(good[:len(good)-1]), "hello"),
		"add dir":         ins(4, "--add-dir", `C:\`),
	} {
		if err := checkArgs(args); err == nil {
			t.Errorf("%s: %q accepted", name, args)
		}
	}
}

func TestFindExePrefersTheRealExe(t *testing.T) {
	if _, err := findExe(filepath.Join(t.TempDir(), "codex.cmd")); err == nil {
		t.Error("a .cmd setting was accepted")
	}
	// An npm install: only codex.cmd on PATH, the exe under node_modules.
	npm := t.TempDir()
	os.WriteFile(filepath.Join(npm, "codex.cmd"), []byte("@echo off\r\n"), 0o600)
	var exe string
	for _, target := range []string{"aarch64-pc-windows-msvc", "x86_64-pc-windows-msvc"} { // both targets: take this PC's
		vendor := filepath.Join(npm, "node_modules", "@openai", "codex", "vendor", target, "codex")
		os.MkdirAll(vendor, 0o700)
		os.WriteFile(filepath.Join(vendor, "codex.exe"), []byte("MZ"), 0o700)
		if strings.HasPrefix(target, vendorArch()) {
			exe = filepath.Join(vendor, "codex.exe")
		}
	}
	t.Setenv("PATH", npm)
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	got, err := findExe("")
	if proc.CheckExe(exe) == nil && (err != nil || got != exe) {
		t.Errorf("findExe = %q, %v; want %q", got, err, exe)
	}
}
