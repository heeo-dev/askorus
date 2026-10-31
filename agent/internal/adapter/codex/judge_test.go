package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
	"github.com/asksemble/asksemble/agent/internal/proc"
)

// judgeTask has an opposite (NUM 3 against NUM 4) and a unit without a
// number, for the fake's rule.
func judgeTask() core.JudgeTask {
	return core.JudgeTask{
		Question: "사과는 몇 개인가요? \"정확히\" & 100%",
		Units: []core.JudgeUnit{
			{ID: "a1", Kind: judge.UnitSentence, Text: "사과는 NUM 3개입니다."},
			{ID: "a2", Kind: judge.UnitSentence, Text: "좋은 하루 되세요."},
		},
		Other:  []core.JudgeUnit{{ID: "b1", Kind: judge.UnitSentence, Text: "사과는 NUM 4개입니다."}},
		Shape:  judge.ShapeDirected,
		Labels: judge.LabelsV1,
	}
}

func judgePrompt(t *testing.T, tk core.JudgeTask) string {
	t.Helper()
	p := judge.Prompt(tk, judge.Nonce(tk))
	if p == "" {
		t.Fatal("judge.Prompt refused the task")
	}
	return p
}

// judgeAdapter is an adapter on the fake codex, with a schema folder that
// does not exist yet and a judge folder.
func judgeAdapter(t *testing.T) (a *Adapter, schemaDir, folder string) {
	t.Helper()
	exe := fakecli.Use(t, fakecli.RoleCodex)
	schemaDir = filepath.Join(t.TempDir(), "Asksemble")
	return New(Options{Path: exe, SchemaDir: schemaDir}), schemaDir, t.TempDir()
}

func wantJudgeArgs(model, effort, schema string) []string {
	return []string{"exec", "-s", "read-only", "--ignore-user-config", "--skip-git-repo-check", "--ephemeral", "--json",
		"-m", model, "-c", `model_reasoning_effort="` + effort + `"`, "-c", `web_search="disabled"`, "--output-schema", schema,
		"--disable", "shell_tool", "--disable", "unified_exec", "--disable", "hooks", "--disable", "apps",
		"--disable", "plugins", "--disable", "remote_plugin", "--disable", "multi_agent", "--disable", "browser_use",
		"--disable", "browser_use_external", "--disable", "in_app_browser", "--disable", "computer_use",
		"--disable", "image_generation", "--disable", "view_image", "--disable", "goals", "--disable", "sleep_tool",
		"--disable", "tool_suggest", "--disable", "skill_search", "--disable", "skill_mcp_dependency_install", "-"}
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

func writes(a *Adapter) int {
	a.schemaMu.Lock()
	defer a.schemaMu.Unlock()
	return a.schemaWrites
}

func TestJudgeArgsAndStdin(t *testing.T) {
	a, schemaDir, folder := judgeAdapter(t)
	tk := judgeTask()
	p := judgePrompt(t, tk)
	res, err := a.Judge(context.Background(), core.JudgeRequest{Folder: folder, Task: tk, Prompt: p})
	if err != nil {
		t.Fatal(err)
	}
	schema := filepath.Join(schemaDir, schemaName)
	calls := fakecli.Calls(t)
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	c := calls[0]
	if want := wantJudgeArgs("gpt-6-luna", "medium", schema); !slices.Equal(c.Args, want) {
		t.Errorf("args = %q\nwant %q", c.Args, want)
	}
	if c.Role != fakecli.RoleCodex || c.Stdin != p || !sameDir(c.Dir, folder) || c.Schema != core.JudgeSchemaV1 {
		t.Errorf("call = role %s, dir %s, stdin %d bytes, schema %q", c.Role, c.Dir, len(c.Stdin), c.Schema)
	}
	for _, arg := range c.Args {
		if strings.Contains(arg, "\n") || strings.Contains(arg, "NUM") || strings.Contains(arg, "사과") || strings.Contains(arg, "data-") {
			t.Errorf("prompt text in argument %q", arg)
		}
	}
	if b, err := os.ReadFile(schema); err != nil || string(b) != core.JudgeSchemaV1 {
		t.Errorf("schema file = %q, %v", b, err)
	}
	if res.Model != "gpt-6-luna" || res.InputTokens != 100 || res.OutputTokens != 9 || res.Cleanup != "" {
		t.Errorf("result = %+v", res)
	}
	v, err := judge.Validate(tk, res.Output)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(v.Verdicts) != 2 || len(v.Missing) != 0 ||
		v.Verdicts[0].Label != judge.LabelOpposite || !slices.Equal(v.Verdicts[0].Evidence, []string{"b1"}) ||
		v.Verdicts[1].Label != judge.LabelAbsent {
		t.Errorf("validated = %+v", v)
	}

	// The settings' model and effort; the schema file is not written again.
	res, err = a.Judge(context.Background(), core.JudgeRequest{Folder: folder, Prompt: p, Model: "gpt-6.1-terra", Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if got := fakecli.Calls(t)[1].Args; !slices.Equal(got, wantJudgeArgs("gpt-6.1-terra", "high", schema)) {
		t.Errorf("override args = %q", got)
	}
	if res.Model != "gpt-6.1-terra" {
		t.Errorf("override model = %s", res.Model)
	}
	if n := writes(a); n != 1 {
		t.Errorf("schema written %d times", n)
	}
	// The ask and judge checks each refuse the other's shape.
	if err := checkArgs(c.Args); err == nil {
		t.Error("the ask check accepted judge arguments")
	}
	if err := a.checkJudgeArgs(c.Args); err != nil {
		t.Errorf("the judge check refused its own arguments: %v", err)
	}
}

func TestSchemaFirstCallConcurrent(t *testing.T) {
	a, schemaDir, folder := judgeAdapter(t)
	p := judgePrompt(t, judgeTask())
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = a.Judge(context.Background(), core.JudgeRequest{Folder: folder, Prompt: p})
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("judge %d: %v", i, err)
		}
	}
	if n := writes(a); n != 1 {
		t.Errorf("schema written %d times", n)
	}
	if b, err := os.ReadFile(filepath.Join(schemaDir, schemaName)); err != nil || string(b) != core.JudgeSchemaV1 {
		t.Errorf("schema file = %q, %v", b, err)
	}
	calls := fakecli.Calls(t)
	if len(calls) != 8 {
		t.Fatalf("%d calls", len(calls))
	}
	for _, c := range calls {
		if c.Schema != core.JudgeSchemaV1 {
			t.Errorf("codex read schema %q", c.Schema)
		}
	}
	// Only the schema file is left in the folder (no temporary files).
	if entries, _ := os.ReadDir(schemaDir); len(entries) != 1 {
		t.Errorf("schema folder holds %d entries", len(entries))
	}
}

func TestSchemaTampered(t *testing.T) {
	a, schemaDir, folder := judgeAdapter(t)
	p := judgePrompt(t, judgeTask())
	schema := filepath.Join(schemaDir, schemaName)
	if _, err := a.Judge(context.Background(), core.JudgeRequest{Folder: folder, Prompt: p}); err != nil {
		t.Fatal(err)
	}
	// Changed after this process wrote it: refused, never repaired.
	tampered := strings.Replace(core.JudgeSchemaV1, `"additionalProperties":false`, `"additionalProperties":true`, 1)
	if err := os.WriteFile(schema, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := a.Judge(context.Background(), core.JudgeRequest{Folder: folder, Prompt: p})
	if ae := actionError(t, err); ae.Code != core.ErrCodeInternal || !strings.Contains(ae.Detail, "restart") {
		t.Errorf("tampered: %v", err)
	}
	if b, _ := os.ReadFile(schema); string(b) != tampered {
		t.Error("the tampered file was rewritten")
	}
	os.Remove(schema)
	if _, err := a.Judge(context.Background(), core.JudgeRequest{Folder: folder, Prompt: p}); core.ErrorCode(err) != core.ErrCodeInternal {
		t.Errorf("removed: %v", err)
	}
	if n := len(fakecli.Calls(t)); n != 1 {
		t.Errorf("codex ran %d times", n)
	}
	if n := writes(a); n != 1 {
		t.Errorf("schema written %d times", n)
	}

	// A new process writes over a file of another version once.
	if err := os.WriteFile(schema, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	b := New(Options{Path: a.exe, SchemaDir: schemaDir})
	if _, err := b.Judge(context.Background(), core.JudgeRequest{Folder: folder, Prompt: p}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(schema); string(got) != core.JudgeSchemaV1 || writes(b) != 1 {
		t.Errorf("new process: writes %d, file %q", writes(b), got)
	}
	// A new process that finds the right bytes does not write at all.
	c := New(Options{Path: a.exe, SchemaDir: schemaDir})
	if _, err := c.Judge(context.Background(), core.JudgeRequest{Folder: folder, Prompt: p}); err != nil {
		t.Fatal(err)
	}
	if n := writes(c); n != 0 {
		t.Errorf("right file written %d times", n)
	}

	// No agent folder: internal, nothing runs.
	n := len(fakecli.Calls(t))
	d := New(Options{Path: a.exe, SchemaDir: schemaDir})
	d.schemaDir = ""
	if _, err := d.Judge(context.Background(), core.JudgeRequest{Folder: folder, Prompt: p}); core.ErrorCode(err) != core.ErrCodeInternal {
		t.Errorf("no schema folder: %v", err)
	}
	if got := len(fakecli.Calls(t)); got != n {
		t.Errorf("no schema folder ran codex %d times", got-n)
	}
}

func TestSchemaWriteRetried(t *testing.T) {
	exe := fakecli.Use(t, fakecli.RoleCodex)
	folder := t.TempDir()
	// A file where the agent folder should be: the first write fails.
	schemaDir := filepath.Join(t.TempDir(), "Asksemble")
	if err := os.WriteFile(schemaDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New(Options{Path: exe, SchemaDir: schemaDir})
	p := judgePrompt(t, judgeTask())
	_, err := a.Judge(context.Background(), core.JudgeRequest{Folder: folder, Prompt: p})
	if ae := actionError(t, err); ae.Code != core.ErrCodeInternal || strings.Contains(ae.Detail, schemaDir) {
		t.Errorf("failed write: %v", err)
	}
	if n := len(fakecli.Calls(t)); n != 0 {
		t.Errorf("codex ran %d times", n)
	}
	// The next judge writes it.
	if err := os.Remove(schemaDir); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Judge(context.Background(), core.JudgeRequest{Folder: folder, Prompt: p}); err != nil {
		t.Fatalf("after the folder came back: %v", err)
	}
	if n := writes(a); n != 1 {
		t.Errorf("schema written %d times", n)
	}
}

func TestCheckJudgeArgsRefuses(t *testing.T) {
	a := New(Options{Path: filepath.Join(t.TempDir(), "codex.exe"), SchemaDir: t.TempDir()})
	schema := a.schemaPath()
	good := judgeArgs("gpt-6-luna", "medium", schema)
	if err := a.checkJudgeArgs(good); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(good, wantJudgeArgs("gpt-6-luna", "medium", schema)) {
		t.Fatalf("judgeArgs = %q", good)
	}
	ins := func(at int, extra ...string) []string { return slices.Insert(slices.Clone(good), at, extra...) }
	set := func(at int, v string) []string { s := slices.Clone(good); s[at] = v; return s }
	del := func(at int) []string { return slices.Delete(slices.Clone(good), at, at+1) }
	last := len(good) - 1
	// apps is the feature that brings the person's Gmail and the like.
	apps := slices.Index(good, "apps")
	if apps < 1 || good[apps-1] != "--disable" {
		t.Fatalf("apps is not turned off: %q", good)
	}
	leftOn := slices.Delete(slices.Clone(good), apps-1, apps+1)
	for name, args := range map[string][]string{
		"search":               ins(0, "--search"),
		"bypass":               ins(1, "--dangerously-bypass-approvals-and-sandbox"),
		"bypass hook trust":    ins(1, "--dangerously-bypass-hook-trust"),
		"full auto":            ins(1, "--full-auto"),
		"workspace write":      set(2, "workspace-write"),
		"danger full access":   set(2, "danger-full-access"),
		"no ignore user cfg":   del(3),
		"no ephemeral":         del(5),
		"oss":                  set(5, "--oss"),
		"no json":              del(6),
		"other schema":         set(14, filepath.Join(t.TempDir(), schemaName)),
		"relative schema":      set(14, schemaName),
		"no schema":            append(slices.Clone(good[:13]), "-"),
		"extra -c":             ins(last, "-c", `approval_policy="never"`),
		"web search on":        set(12, `web_search="live"`),
		"effort as other key":  set(10, `model_reasoning_summary="medium"`),
		"effort not codex's":   set(10, `model_reasoning_effort="max"`),
		"effort unquoted":      set(10, "model_reasoning_effort=medium"),
		"effort injected":      set(10, `model_reasoning_effort="medium" -c sandbox_mode="danger-full-access"`),
		"model as a flag":      set(8, "--dangerously-bypass-approvals-and-sandbox"),
		"model with quote":     set(8, `x"`),
		"resume":               ins(1, "resume"),
		"resume last":          ins(last, "resume", "--last"),
		"prompt argument":      set(last, "hello"),
		"prompt and stdin":     append(slices.Clone(good), "hello"),
		"add dir":              ins(1, "--add-dir", `C:\`),
		"profile":              ins(1, "--profile", "work"),
		"approval":             ins(1, "-a", "never"),
		"cd":                   ins(1, "-C", `C:\`),
		"config key swapped":   set(12, `sandbox_mode="danger-full-access"`),
		"ask":                  askArgs("gpt-6-luna", "medium", ""),
		"ask follow-up":        askArgs("gpt-6-luna", "medium", "01a188b9-9dfe-7b92-b37c-d12a1eca2234"),
		"empty":                nil,
		"options before exec":  append([]string{"-c", `web_search="disabled"`}, good[:len(good)-2]...),
		"same length reorder":  append(append(slices.Clone(good[:10]), "-c", `web_search="disabled"`, "-c", `model_reasoning_effort="medium"`), good[13:]...),
		"ephemeral and resume": set(5, "resume"),
		"apps left on":         leftOn,
		"apps left on, padded": append(slices.Clone(leftOn[:last-2]), "--disable", "hooks", "-"),
		"apps enabled":         set(apps-1, "--enable"),
		"feature swapped":      set(apps, "memories"),
		"enable added":         ins(last, "--enable", "apps"),
		"no features off":      append(slices.Clone(good[:15]), "-"),
	} {
		if err := a.checkJudgeArgs(args); err == nil {
			t.Errorf("%s: %q accepted", name, args)
		}
	}
	// The ask check refuses every judge shape.
	if err := checkArgs(good); err == nil {
		t.Error("ask check accepted judge arguments")
	}
	if err := checkArgs(ins(0, "--search")); err == nil {
		t.Error("ask check accepted judge arguments after --search")
	}
	// Without an agent folder no schema path passes.
	noDir := &Adapter{}
	if err := noDir.checkJudgeArgs(judgeArgs("gpt-6-luna", "medium", "")); err == nil {
		t.Error("empty schema path accepted")
	}
}

func TestJudgeModes(t *testing.T) {
	a, _, folder := judgeAdapter(t)
	tk := judgeTask()
	p := judgePrompt(t, tk)
	run := func(mode string) (core.JudgeResult, error) {
		t.Helper()
		t.Setenv(fakecli.EnvJudge, mode)
		return a.Judge(context.Background(), core.JudgeRequest{Folder: folder, Task: tk, Prompt: p})
	}

	res, err := run(fakecli.ModeMissingID)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := judge.Validate(tk, res.Output); err != nil || !slices.Equal(v.Missing, []string{"a1"}) {
		t.Errorf("missing_id: %+v %v", v, err)
	}
	res, err = run(fakecli.ModeUnsure)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := judge.Validate(tk, res.Output); err != nil || len(v.Verdicts) != 2 || !v.Verdicts[0].Unsure {
		t.Errorf("unsure: %+v %v", v, err)
	}

	// Prose and cut-off JSON come back as they are; Validate refuses them.
	for mode, text := range map[string]string{fakecli.ModeProse: fakecli.ProseText, fakecli.ModeBadJSON: fakecli.BadJSONText} {
		res, err := run(mode)
		if err != nil || string(res.Output) != text {
			t.Errorf("%s: %q %v", mode, res.Output, err)
			continue
		}
		var je *judge.Error
		if _, err := judge.Validate(tk, res.Output); !errors.As(err, &je) || je.Code != core.ErrCodeJudgeBadOutput {
			t.Errorf("%s: Validate gave %v", mode, err)
		}
	}

	res, err = run(fakecli.ModeUsageLimit)
	if ae := actionError(t, err); ae.Code != core.ErrCodeJudgeFailed || ae.Detail != fakecli.UsageLimitCodex {
		t.Errorf("usage_limit: %v", err)
	}
	if res.Output != nil || res.Model != "gpt-6-luna" {
		t.Errorf("usage_limit result = %+v", res)
	}

	res, err = run(fakecli.ModeBadModel)
	if ae := actionError(t, err); ae.Code != core.ErrCodeBadModel || !strings.Contains(ae.Detail, "not supported") || strings.Contains(ae.Detail, "{") {
		t.Errorf("bad_model: %v", err)
	}
	if res.Output != nil {
		t.Errorf("bad_model output = %s", res.Output)
	}

	// A tool call throws the judgment away, even though JSON followed.
	res, err = run(fakecli.ModeToolUse)
	if code := core.ErrorCode(err); code != core.ErrCodeJudgeUsedTool {
		t.Errorf("tool_use: %v", err)
	}
	if res.Output != nil || res.Model != "gpt-6-luna" || res.InputTokens != 100 {
		t.Errorf("tool_use result = %+v", res)
	}

	// A failure after the JSON: codex's words, never the judge's JSON.
	res, err = run(fakecli.ModeFailedWithOutput)
	ae := actionError(t, err)
	if ae.Code != core.ErrCodeJudgeFailed || ae.Detail != "stream disconnected before completion" || strings.Contains(err.Error(), "judgments") {
		t.Errorf("failed_with_output: %v", err)
	}
	if res.Output != nil {
		t.Errorf("failed_with_output output = %s", res.Output)
	}

	if n := len(fakecli.Calls(t)); n != 8 {
		t.Errorf("%d calls", n)
	}
}

func TestJudgeTimeout(t *testing.T) {
	a, _, folder := judgeAdapter(t)
	t.Setenv(fakecli.EnvJudge, fakecli.ModeSlow)
	t.Setenv(fakecli.EnvSleep, "30s")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	res, err := a.Judge(ctx, core.JudgeRequest{Folder: folder, Prompt: judgePrompt(t, judgeTask())})
	if core.ErrorCode(err) != core.ErrCodeJudgeTimeout {
		t.Errorf("slow: %v", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("timeout took %v", d)
	}
	if res.Output != nil || res.Model != "gpt-6-luna" {
		t.Errorf("slow result = %+v", res)
	}
	// A context that ended before the call: proc.Run gives the context's
	// own error, which is still a timeout.
	ended, cancelEnded := context.WithCancel(context.Background())
	cancelEnded()
	if _, err := a.Judge(ended, core.JudgeRequest{Folder: folder, Prompt: judgePrompt(t, judgeTask())}); core.ErrorCode(err) != core.ErrCodeJudgeTimeout {
		t.Errorf("ended context: %v", err)
	}
}

// Judge sets its own time limit: a caller with no deadline still gets
// judge_timeout once judgeTimeout is up.
func TestJudgeOwnTimeout(t *testing.T) {
	if judgeTimeout != core.JudgeTimeout {
		t.Fatalf("judgeTimeout = %v, want core.JudgeTimeout", judgeTimeout)
	}
	a, _, folder := judgeAdapter(t)
	t.Setenv(fakecli.EnvJudge, fakecli.ModeSlow)
	t.Setenv(fakecli.EnvSleep, "20s")
	old := judgeTimeout
	judgeTimeout = 300 * time.Millisecond
	t.Cleanup(func() { judgeTimeout = old })
	start := time.Now()
	res, err := a.Judge(context.Background(), core.JudgeRequest{Folder: folder, Prompt: judgePrompt(t, judgeTask())})
	if core.ErrorCode(err) != core.ErrCodeJudgeTimeout || time.Since(start) > 10*time.Second {
		t.Errorf("err = %v after %s", err, time.Since(start))
	}
	if res.Output != nil {
		t.Errorf("output %s", res.Output)
	}
}

func TestJudgeGuards(t *testing.T) {
	a, schemaDir, folder := judgeAdapter(t)
	p := judgePrompt(t, judgeTask())

	missing := New(Options{Path: filepath.Join(t.TempDir(), "codex.exe"), SchemaDir: schemaDir})
	if _, err := missing.Judge(context.Background(), core.JudgeRequest{Folder: folder, Prompt: p}); core.ErrorCode(err) != core.ErrCodeAgentUnavailable {
		t.Errorf("missing codex: %v", err)
	}
	if _, err := os.Stat(schemaDir); err == nil {
		t.Error("a missing codex wrote the schema file")
	}
	if _, err := a.Judge(context.Background(), core.JudgeRequest{Folder: folder}); core.ErrorCode(err) != core.ErrCodeInternal {
		t.Errorf("empty prompt: %v", err)
	}
	for name, req := range map[string]core.JudgeRequest{
		"model as a flag":  {Model: "--dangerously-bypass-approvals-and-sandbox"},
		"model with quote": {Model: `x"`},
		"model with space": {Model: "gpt-6 luna"},
		"effort max":       {Effort: "max"},
		"effort injected":  {Effort: `medium" -c sandbox_mode="danger-full-access`},
	} {
		req.Folder, req.Prompt = folder, p
		_, err := a.Judge(context.Background(), req)
		if ae := actionError(t, err); ae.Code != core.ErrCodeBadModel || strings.Contains(ae.Detail, "dangerously") || strings.Contains(ae.Detail, "sandbox") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if n := len(fakecli.Calls(t)); n != 0 {
		t.Errorf("refused judges ran codex %d times", n)
	}

	// codex went away after New: agent_unavailable.
	gone := New(Options{Path: a.exe, SchemaDir: schemaDir})
	gone.exe = filepath.Join(t.TempDir(), "codex.exe")
	if _, err := gone.Judge(context.Background(), core.JudgeRequest{Folder: folder, Prompt: p}); core.ErrorCode(err) != core.ErrCodeAgentUnavailable {
		t.Errorf("codex gone: %v", err)
	}
	// A judge folder that is missing, relative or empty is the agent's
	// fault (internal, as with claude and opencode), and runs nothing.
	for _, f := range []string{filepath.Join(folder, "none"), "judge", ""} {
		_, err := a.Judge(context.Background(), core.JudgeRequest{Folder: f, Prompt: p})
		if ae := actionError(t, err); ae.Code != core.ErrCodeInternal || strings.Contains(ae.Detail, folder) {
			t.Errorf("folder %q: %v", f, err)
		}
	}
	if n := len(fakecli.Calls(t)); n != 0 {
		t.Errorf("codex ran %d times", n)
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

func TestJudgeDefault(t *testing.T) {
	a := &Adapter{}
	d := a.JudgeDefault()
	if d != (core.AskModel{Model: "gpt-6-luna", Effort: "medium"}) || !efforts[d.Effort] || a.Family(d.Model) != "openai" {
		t.Errorf("JudgeDefault = %+v", d)
	}
	if !core.VersionAtLeast(JudgeMinVersion, "0.164.0") || !core.VersionAtLeast("0.164.0", JudgeMinVersion) {
		t.Errorf("JudgeMinVersion = %s", JudgeMinVersion)
	}
	if fakecli.Versions[fakecli.RoleCodex] != JudgeMinVersion {
		t.Errorf("the fake plays codex %s", fakecli.Versions[fakecli.RoleCodex])
	}
}
