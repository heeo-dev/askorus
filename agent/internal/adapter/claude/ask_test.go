package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
)

func newAskAdapter(t *testing.T, tiers map[string]core.AskModel) (*Adapter, string, string) {
	t.Helper()
	r, argsFile := fakeRunner(t)
	root := t.TempDir()
	t.Setenv("ASKSEMBLE_FAKE_PROJECTS", filepath.Join(root, "projects"))
	t.Setenv("ASKSEMBLE_FAKE_ASK", "")
	folder := filepath.Join(root, "ask")
	os.MkdirAll(folder, 0o700)
	return New(Options{ClaudeDir: root, ClaudePath: r.Path(), AskTiers: tiers}), argsFile, folder
}

func TestAskSendsThePromptOnStdin(t *testing.T) {
	a, argsFile, folder := newAskAdapter(t, nil)
	prompt := core.AskPrompt("대한민국의 수도는?\n한 문장으로 답하세요. 100% \"정확히\" & 짧게")
	res, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: prompt, Tier: core.TierDeep})
	if err != nil {
		t.Fatal(err)
	}
	calls := recordedCalls(t, argsFile)
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	want := []string{"-p", "--output-format", "json", "--model", "opus", "--effort", "medium",
		"--tools", "WebSearch,WebFetch", "--allowedTools", "WebSearch,WebFetch", "--strict-mcp-config"}
	if !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("args = %q", calls[0].Args)
	}
	if calls[0].Stdin != prompt {
		t.Errorf("stdin = %q", calls[0].Stdin)
	}
	if !sameFolder(calls[0].Cwd, folder) {
		t.Errorf("cwd = %q, want %q", calls[0].Cwd, folder)
	}
	if res.Answer != `fake answer: 한 문장으로 답하세요. 100% "정확히" & 짧게` || !ValidUUID(res.SessionID) {
		t.Errorf("result = %+v", res)
	}
	if res.Model != "claude-opus-5-5" || res.InputTokens != 12 || res.OutputTokens != 7 {
		t.Errorf("model and tokens = %+v", res)
	}

	// A follow-up carries the same conversation on, with the request's model.
	res2, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "더", Tier: core.TierStandard,
		Model: "haiku", Effort: "low", Resume: res.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if res2.SessionID != res.SessionID {
		t.Errorf("follow-up session = %s", res2.SessionID)
	}
	got := recordedCalls(t, argsFile)[1].Args
	want = []string{"-p", "--output-format", "json", "--model", "haiku", "--effort", "low",
		"--tools", "WebSearch,WebFetch", "--allowedTools", "WebSearch,WebFetch", "--strict-mcp-config", "--resume", res.SessionID}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("follow-up args = %q", got)
	}
}

// askCalls returns the `claude -p` calls the fake received.
func askCalls(t *testing.T, file string) []fakeCall {
	t.Helper()
	var out []fakeCall
	for _, c := range recordedCalls(t, file) {
		if len(c.Args) > 0 && c.Args[0] == "-p" {
			out = append(out, c)
		}
	}
	return out
}

func sameFolder(a, b string) bool {
	ea, _ := filepath.EvalSymlinks(a)
	eb, _ := filepath.EvalSymlinks(b)
	return core.InFolder(ea, eb) && core.InFolder(eb, ea)
}

func TestAskTiersAndOverrides(t *testing.T) {
	a, argsFile, folder := newAskAdapter(t, map[string]core.AskModel{core.TierQuick: {Model: "claude-haiku-5-5"}})
	tiers := a.AskTiers()
	if tiers[core.TierQuick] != (core.AskModel{Model: "claude-haiku-5-5", Effort: "medium"}) || tiers[core.TierStandard].Model != "sonnet" || len(tiers) != 3 {
		t.Fatalf("tiers = %+v", tiers)
	}
	if _, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierQuick}); err != nil {
		t.Fatal(err)
	}
	if args := recordedCalls(t, argsFile)[0].Args; args[4] != "claude-haiku-5-5" {
		t.Errorf("args = %q", args)
	}
	if !slices.Contains(a.capabilities(), core.CapAsk) {
		t.Errorf("capabilities = %v", a.capabilities())
	}
	// Every model, the overridden one too, takes Claude Code's five efforts (11-06).
	all := []string{"low", "medium", "high", "xhigh", "max"}
	e := a.AskEfforts()
	if len(e) != 3 || !slices.Equal(e["claude-haiku-5-5"], all) || !slices.Equal(e["sonnet"], all) || !slices.Equal(e["opus"], all) {
		t.Errorf("AskEfforts = %v", e)
	}
}

func TestAskErrors(t *testing.T) {
	a, argsFile, folder := newAskAdapter(t, nil)
	ask := func(req core.AskRequest) error {
		req.Folder, req.Prompt = folder, "q"
		if req.Tier == "" {
			req.Tier = core.TierStandard
		}
		_, err := a.Ask(context.Background(), req)
		return err
	}
	t.Setenv("ASKSEMBLE_FAKE_ASK", "badmodel")
	if err := ask(core.AskRequest{Model: "nope"}); core.ErrorCode(err) != core.ErrCodeBadModel {
		t.Errorf("bad model: %v", err)
	}
	t.Setenv("ASKSEMBLE_FAKE_ASK", "noresume")
	if err := ask(core.AskRequest{Resume: "12345678-1234-4234-8234-123456789abc"}); core.ErrorCode(err) != core.ErrCodeNotFound {
		t.Errorf("unknown session: %v", err)
	}
	t.Setenv("ASKSEMBLE_FAKE_ASK", "garbage")
	if err := ask(core.AskRequest{}); core.ErrorCode(err) != core.ErrCodeAskFailed {
		t.Errorf("no JSON: %v", err)
	}
	t.Setenv("ASKSEMBLE_FAKE_ASK", "")
	n := len(recordedCalls(t, argsFile))
	// Refused before claude runs.
	for name, req := range map[string]core.AskRequest{
		"resume not a uuid": {Resume: "../x"},
		"model with slash":  {Model: "a/b"},
		"model as a flag":   {Model: "--dangerously-skip-permissions"},
		"unknown effort":    {Effort: "ultra"},
	} {
		if err := ask(req); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if got := len(recordedCalls(t, argsFile)); got != n {
		t.Errorf("refused asks ran claude %d times", got-n)
	}
	missing := New(Options{ClaudeDir: t.TempDir(), ClaudePath: filepath.Join(t.TempDir(), "claude.exe")})
	if _, err := missing.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q"}); core.ErrorCode(err) != core.ErrCodeAgentUnavailable {
		t.Errorf("missing claude: %v", err)
	}
}

// A follow-up never resumes a session another process runs or another
// action holds: claude would fork a copy of the conversation.
func TestAskFollowUpWaitsForNobody(t *testing.T) {
	a, argsFile, folder := newAskAdapter(t, nil)
	const id = "12345678-1234-4234-8234-123456789abc"
	req := core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierStandard, Resume: id}
	unlock, err := a.lock(id) // an open or stop from the phone in progress
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Ask(context.Background(), req); core.ErrorCode(err) != core.ErrCodeBusy {
		t.Errorf("while locked: %v", err)
	}
	unlock()
	a.live = func() map[string]liveSession {
		return map[string]liveSession{id: {PID: 4242, SessionID: id, Kind: "bg"}}
	}
	if _, err := a.Ask(context.Background(), req); core.ErrorCode(err) != core.ErrCodeBusy {
		t.Errorf("while it runs as a remote session: %v", err)
	}
	if n := len(askCalls(t, argsFile)); n != 0 {
		t.Errorf("claude -p ran %d times", n)
	}
	a.live = func() map[string]liveSession { return nil }
	if _, err := a.Ask(context.Background(), req); err != nil {
		t.Fatalf("free again: %v", err)
	}
	if _, err := a.lock(id); err != nil {
		t.Errorf("the ask kept the session locked: %v", err)
	}
}

// A slow claude is ended at the time limit.
func TestAskTimeout(t *testing.T) {
	a, _, folder := newAskAdapter(t, nil)
	t.Setenv("ASKSEMBLE_FAKE_SLEEP", "20s")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := a.Ask(ctx, core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierStandard})
	if core.ErrorCode(err) != core.ErrCodeAskTimeout || time.Since(start) > 10*time.Second {
		t.Errorf("err = %v after %s", err, time.Since(start))
	}
}

func TestCheckAskArgs(t *testing.T) {
	good := askArgs("sonnet", "medium", "", false)
	if err := checkAskArgs(good); err != nil {
		t.Fatalf("own args refused: %v", err)
	}
	if err := checkAskArgs(askArgs("sonnet", "", "12345678-1234-4234-8234-123456789abc", false)); err != nil {
		t.Fatalf("resume args refused: %v", err)
	}
	// 11-06: a one-off ask (a debate turn) keeps no session, and never resumes.
	oneOff := askArgs("sonnet", "medium", "", true)
	if err := checkAskArgs(oneOff); err != nil || oneOff[len(oneOff)-1] != "--no-session-persistence" {
		t.Fatalf("one-off args %q: %v", oneOff, err)
	}
	if err := checkAskArgs(append(slices.Clone(oneOff), "--resume", "12345678-1234-4234-8234-123456789abc")); !errors.Is(err, ErrArgRejected) {
		t.Errorf("one-off with resume: %v", err)
	}
	bad := map[string][]string{
		"no -p":             good[1:],
		"permission bypass": append(slices.Clone(good), "--dangerously-skip-permissions"),
		"permission mode":   append(slices.Clone(good), "--permission-mode", "bypassPermissions"),
		"mcp config":        append(slices.Clone(good), "--mcp-config", "x.json"),
		"settings":          append(slices.Clone(good), "--settings", "{}"),
		"prompt argument":   append(slices.Clone(good), "hello"),
		"more tools":        {"-p", "--output-format", "json", "--model", "sonnet", "--tools", "WebSearch,WebFetch,Bash", "--allowedTools", "WebSearch,WebFetch", "--strict-mcp-config"},
		"allow bash":        {"-p", "--output-format", "json", "--model", "sonnet", "--tools", "WebSearch,WebFetch", "--allowedTools", "Bash", "--strict-mcp-config"},
		"no strict mcp":     {"-p", "--output-format", "json", "--model", "sonnet", "--tools", "WebSearch,WebFetch", "--allowedTools", "WebSearch,WebFetch"},
		"no tools limit":    {"-p", "--output-format", "json", "--model", "sonnet", "--allowedTools", "WebSearch,WebFetch", "--strict-mcp-config"},
		"twice":             append(slices.Clone(good), "--model", "opus"),
		"stream output":     {"-p", "--output-format", "stream-json", "--model", "sonnet", "--tools", "WebSearch,WebFetch", "--allowedTools", "WebSearch,WebFetch", "--strict-mcp-config"},
		"resume not uuid":   append(slices.Clone(good), "--resume", "--fork-session"),
	}
	for name, args := range bad {
		if err := checkAskArgs(args); !errors.Is(err, ErrArgRejected) {
			t.Errorf("%s: %q accepted (%v)", name, args, err)
		}
	}
}
