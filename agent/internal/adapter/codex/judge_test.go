package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/proc"
)

// judgeAdapter is an adapter on the fake codex, with a schema folder that
// does not exist yet and a judge folder, for the one-off calls.
func judgeAdapter(t *testing.T) (a *Adapter, schemaDir, folder string) {
	t.Helper()
	exe := fakecli.Use(t, fakecli.RoleCodex)
	schemaDir = filepath.Join(t.TempDir(), "Asksemble")
	return New(Options{Path: exe, SchemaDir: schemaDir}), schemaDir, t.TempDir()
}

// wantJudgeArgs is the one-off command's exact arguments (judgeArgs, a
// combined answer's or a question spec's), with the features written out
// in wantOff (codex_test.go), the same list an ask turns off.
func wantJudgeArgs(model, effort, schema string) []string {
	return slices.Concat([]string{"exec", "-s", "read-only", "--ignore-user-config", "--skip-git-repo-check", "--ephemeral", "--json",
		"-m", model, "-c", `model_reasoning_effort="` + effort + `"`, "-c", `web_search="disabled"`, "--output-schema", schema},
		wantDisables(), []string{"-"})
}

func sameDir(a, b string) bool {
	ia, err1 := os.Stat(a)
	ib, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(ia, ib)
}

func actionError(t *testing.T, err error) *core.ActionError {
	t.Helper()
	var ae *core.ActionError
	if !errors.As(err, &ae) {
		t.Fatalf("error %v is not an ActionError", err)
	}
	return ae
}

// A schema file that could not be written is tried again by the next
// combined answer.
func TestSchemaWriteRetried(t *testing.T) {
	exe := fakecli.Use(t, fakecli.RoleCodex)
	folder := t.TempDir()
	// A file where the agent folder should be: the first write fails.
	schemaDir := filepath.Join(t.TempDir(), "Asksemble")
	if err := os.WriteFile(schemaDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New(Options{Path: exe, SchemaDir: schemaDir})
	req := core.SynthRequest{Folder: folder, Prompt: synthPrompt(t), Model: "gpt-6-luna"}
	_, err := a.Synthesize(context.Background(), req)
	if ae := actionError(t, err); ae.Code != core.ErrCodeInternal || strings.Contains(ae.Detail, schemaDir) {
		t.Errorf("failed write: %v", err)
	}
	if n := len(fakecli.Calls(t)); n != 0 {
		t.Errorf("codex ran %d times", n)
	}
	// The next combined answer writes it.
	if err := os.Remove(schemaDir); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Synthesize(context.Background(), req); err != nil {
		t.Fatalf("after the folder came back: %v", err)
	}
	if n := synthWrites(a); n != 1 {
		t.Errorf("schema written %d times", n)
	}
}

// TestParseJudgeOutput covers codex event shapes the fake does not print.
func TestParseJudgeOutput(t *testing.T) {
	const (
		started   = `{"type":"thread.started","thread_id":"01a188b9-9dfe-7b92-b37c-d12a1eca2234"}`
		turn      = `{"type":"turn.started"}`
		reasoning = `{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"**Comparing**"}}`
		answer    = `{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"{\"judgments\":[]}"}}`
		done      = `{"type":"turn.completed","usage":{"input_tokens":50,"cached_input_tokens":10,"output_tokens":4}}`
	)
	lines := func(ls ...string) proc.Result { return proc.Result{Stdout: []byte(strings.Join(ls, "\r\n") + "\r\n")} }
	ok := func(name string, res proc.Result, out string) {
		t.Helper()
		r, err := parseJudgeOutput(res, "gpt-6-luna")
		if err != nil || string(r.Output) != out || r.Model != "gpt-6-luna" {
			t.Errorf("%s: %+v %v", name, r, err)
		}
	}
	fail := func(name string, res proc.Result, code string) *core.ActionError {
		t.Helper()
		r, err := parseJudgeOutput(res, "gpt-6-luna")
		var ae *core.ActionError
		if !errors.As(err, &ae) || ae.Code != code || r.Output != nil || r.Model != "gpt-6-luna" {
			t.Errorf("%s: %+v %v, want %s", name, r, err, code)
			return &core.ActionError{}
		}
		return ae
	}

	r, err := parseJudgeOutput(lines(started, turn, reasoning, answer, done), "gpt-6-luna")
	if err != nil || string(r.Output) != `{"judgments":[]}` || r.InputTokens != 50 || r.OutputTokens != 4 {
		t.Errorf("plain: %+v %v", r, err)
	}
	ok("last message wins", lines(started,
		`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"비교하겠습니다."}}`, answer, done), `{"judgments":[]}`)
	ok("allowed items", lines(started,
		`{"type":"item.started","item":{"id":"item_0","type":"todo_list","items":[]}}`,
		`{"type":"item.updated","item":{"id":"item_0","type":"todo_list","items":[]}}`,
		`{"type":"item.completed","item":{"id":"item_2","type":"error","message":"Model metadata not found."}}`,
		"not json", answer, done), `{"judgments":[]}`)

	for name, item := range map[string]string{
		"command started":   `{"type":"item.started","item":{"id":"item_5","type":"command_execution","command":"curl x","status":"in_progress"}}`,
		"command completed": `{"type":"item.completed","item":{"id":"item_5","type":"command_execution","command":"dir","status":"completed"}}`,
		"web search":        `{"type":"item.completed","item":{"id":"item_5","type":"web_search","query":"q"}}`,
		"file change":       `{"type":"item.updated","item":{"id":"item_5","type":"file_change","changes":[]}}`,
		"mcp tool":          `{"type":"item.started","item":{"id":"item_5","type":"mcp_tool_call","server":"s","tool":"t"}}`,
		"unknown item":      `{"type":"item.completed","item":{"id":"item_5","type":"collab_tool_call"}}`,
		"no item type":      `{"type":"item.completed","item":{"id":"item_5"}}`,
	} {
		fail(name, lines(started, turn, item, answer, done), core.ErrCodeJudgeUsedTool)
	}
	// A tool counts before a failure.
	fail("tool then failure", lines(started,
		`{"type":"item.started","item":{"id":"item_5","type":"command_execution"}}`,
		`{"type":"turn.failed","error":{"message":"boom"}}`), core.ErrCodeJudgeUsedTool)

	// Failures.
	inner := `{"type":"error","status":400,"error":{"type":"invalid_request_error","code":"model_not_found"}}`
	q := strings.ReplaceAll(inner, `"`, `\"`)
	ae := fail("model not found without message", lines(started, `{"type":"turn.failed","error":{"message":"`+q+`"}}`), core.ErrCodeBadModel)
	if ae.Detail != inner {
		t.Errorf("model not found detail = %q", ae.Detail)
	}
	ae = fail("error event only", proc.Result{Stdout: []byte(`{"type":"error","message":"Reconnecting... 5/5"}` + "\n"), ExitCode: 1}, core.ErrCodeJudgeFailed)
	if ae.Detail != "Reconnecting... 5/5" {
		t.Errorf("error event detail = %q", ae.Detail)
	}
	stderr := "2026-11-01T00:00:00.000000Z ERROR codex_core: boom\nError: something broke\n"
	ae = fail("stderr", proc.Result{Stderr: []byte(stderr), ExitCode: 1}, core.ErrCodeJudgeFailed)
	if ae.Detail != "Error: something broke" {
		t.Errorf("stderr detail = %q", ae.Detail)
	}
	ae = fail("nothing at all", proc.Result{ExitCode: 3}, core.ErrCodeJudgeFailed)
	if !strings.Contains(ae.Detail, "exit code 3") {
		t.Errorf("no message detail = %q", ae.Detail)
	}
	res := lines(started, answer, done)
	res.ExitCode = 1
	ae = fail("exit code after the answer", res, core.ErrCodeJudgeFailed)
	if strings.Contains(ae.Detail, "judgments") {
		t.Errorf("exit code detail = %q", ae.Detail)
	}
	ae = fail("judge JSON as the message", lines(started, `{"type":"turn.failed","error":{"message":"{\"judgments\":[{\"id\":\"a1\"}]}"}}`), core.ErrCodeJudgeFailed)
	if strings.Contains(ae.Detail, "judgments") {
		t.Errorf("judge JSON detail = %q", ae.Detail)
	}
	long := strings.Repeat("한도 ", 400)
	ae = fail("long message", lines(started, `{"type":"turn.failed","error":{"message":"`+long+`"}}`), core.ErrCodeJudgeFailed)
	if n := utf8.RuneCountInString(ae.Detail); n > core.JudgeMessageChars || n < core.JudgeMessageChars-1 {
		t.Errorf("long message detail has %d characters", n)
	}
	// Tokens come back with an error.
	if r, _ := parseJudgeOutput(lines(started, done, `{"type":"turn.failed","error":{"message":"x"}}`), "gpt-6-luna"); r.InputTokens != 50 {
		t.Errorf("tokens with a failure = %+v", r)
	}

	// No answer.
	fail("no agent message", lines(started, turn, reasoning, done), core.ErrCodeJudgeBadOutput)
	fail("empty agent message", lines(started, `{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"  "}}`, done), core.ErrCodeJudgeBadOutput)
	fail("no output", proc.Result{}, core.ErrCodeJudgeBadOutput)
	fail("message only started", lines(started, `{"type":"item.started","item":{"id":"item_1","type":"agent_message","text":"{}"}}`, done), core.ErrCodeJudgeBadOutput)
}

func TestFamily(t *testing.T) {
	a := &Adapter{}
	for model, want := range map[string]string{
		"gpt-6-luna":        "openai",
		"gpt-6.1-terra":     "openai",
		"gpt-6.1-sol":       "openai",
		"GPT-5":             "openai",
		"o3":                "openai",
		"o4-mini":           "openai",
		"codex-mini-latest": "openai",
		"":                  "",
		"o":                 "",
		"oss-20b":           "",
		"openai/gpt-5":      "",
		"gpt":               "",
		"claude-sonnet-5-5": "",
		"deepseek-v4.1":     "",
		"codex":             "",
		"unknown":           "",
	} {
		if got := a.Family(model); got != want {
			t.Errorf("Family(%q) = %q, want %q", model, got, want)
		}
	}
}

// JudgeMinVersion is the one-off command's tested version, the fake's
// default.
func TestJudgeMinVersion(t *testing.T) {
	if !core.VersionAtLeast(JudgeMinVersion, "0.164.0") || !core.VersionAtLeast("0.164.0", JudgeMinVersion) {
		t.Errorf("JudgeMinVersion = %s", JudgeMinVersion)
	}
	if fakecli.Versions[fakecli.RoleCodex] != JudgeMinVersion {
		t.Errorf("the fake plays codex %s", fakecli.Versions[fakecli.RoleCodex])
	}
}
