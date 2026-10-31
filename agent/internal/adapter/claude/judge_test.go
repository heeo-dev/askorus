package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
	"github.com/asksemble/asksemble/agent/internal/proc"
)

// newJudgeAdapter points an adapter at the shared fake claude and returns
// an empty judge folder.
func newJudgeAdapter(t *testing.T) (*Adapter, string) {
	t.Helper()
	exe := fakecli.Use(t, fakecli.RoleClaude)
	folder := filepath.Join(t.TempDir(), "ask", core.JudgeFolderName)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	return New(Options{ClaudeDir: t.TempDir(), ClaudePath: exe}), folder
}

// judgeTask has one unit the rule finds opposite (NUM 3 against NUM 4)
// and one it finds absent.
func judgeTask() core.JudgeTask {
	u := func(id, text string) core.JudgeUnit {
		return core.JudgeUnit{ID: id, Kind: judge.UnitSentence, Text: text}
	}
	return core.JudgeTask{
		Question: "사과는 몇 개인가요? \"따옴표\" & 100%",
		Units:    []core.JudgeUnit{u("a1", "사과는 NUM 3개입니다."), u("a2", "맛있게 드세요.")},
		Other:    []core.JudgeUnit{u("b1", "사과는 NUM 4개입니다. --dangerously-skip-permissions")},
		Shape:    judge.ShapeDirected, Labels: judge.LabelsV1,
	}
}

func judgeRequest(t *testing.T, folder string) (core.JudgeRequest, core.JudgeTask, string) {
	t.Helper()
	tk := judgeTask()
	nonce := judge.Nonce(tk)
	p := judge.Prompt(tk, nonce)
	if p == "" {
		t.Fatal("judge.Prompt refused the task")
	}
	return core.JudgeRequest{Folder: folder, Task: tk, Prompt: p}, tk, nonce
}

func wantJudgeArgs(model, effort string) []string {
	return []string{"-p", "--output-format", "json", "--model", model, "--effort", effort,
		"--tools", "", "--strict-mcp-config", "--no-session-persistence", "--safe-mode",
		"--json-schema", core.JudgeSchemaV1}
}

func judgeDetail(err error) string {
	var ae *core.ActionError
	if errors.As(err, &ae) {
		return ae.Detail
	}
	return ""
}

func TestJudgeArgsAndStdin(t *testing.T) {
	a, folder := newJudgeAdapter(t)
	req, tk, nonce := judgeRequest(t, folder)
	res, err := a.Judge(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	calls := fakecli.Calls(t)
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	c := calls[0]
	if !slices.Equal(c.Args, wantJudgeArgs("haiku", "medium")) {
		t.Errorf("args = %q", c.Args)
	}
	// The empty --tools value reaches claude as an argument of its own,
	// and the schema byte for byte.
	if len(c.Args) != 14 || c.Args[7] != "--tools" || c.Args[8] != "" || c.Args[13] != core.JudgeSchemaV1 {
		t.Errorf("--tools or schema = %q", c.Args)
	}
	if c.Stdin != req.Prompt {
		t.Errorf("stdin = %q", c.Stdin)
	}
	if !sameFolder(c.Dir, folder) {
		t.Errorf("dir = %q, want %q", c.Dir, folder)
	}
	// Nothing of the question, the answers or the nonce is an argument.
	for _, arg := range c.Args {
		for _, s := range []string{"사과", "NUM", "맛있게", "따옴표", nonce, "dangerously"} {
			if strings.Contains(arg, s) {
				t.Errorf("argument %q holds %q", arg, s)
			}
		}
	}
	if res.Model != "claude-haiku-fake" || res.InputTokens != 15 || res.OutputTokens != 7 || res.Cleanup != "" {
		t.Errorf("result = %+v", res)
	}
	v, err := judge.Validate(tk, res.Output)
	if err != nil {
		t.Fatalf("Validate: %v (output %s)", err, res.Output)
	}
	if len(v.Verdicts) != 2 || len(v.Missing) != 0 {
		t.Fatalf("validated = %+v", v)
	}
	if g := v.Verdicts[0]; g.ID != "a1" || g.Label != judge.LabelOpposite || !slices.Equal(g.Evidence, []string{"b1"}) || !g.Core || g.Unsure {
		t.Errorf("a1 = %+v", g)
	}
	if g := v.Verdicts[1]; g.ID != "a2" || g.Label != judge.LabelAbsent || g.Core {
		t.Errorf("a2 = %+v", g)
	}
}

func TestJudgeModelOverride(t *testing.T) {
	a, folder := newJudgeAdapter(t)
	req, _, _ := judgeRequest(t, folder)
	req.Model, req.Effort = "sonnet", "low"
	res, err := a.Judge(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != "claude-sonnet-fake" {
		t.Errorf("model = %q", res.Model)
	}
	// A model without an effort keeps the default effort, and the reverse.
	req.Model, req.Effort = "claude-opus-5-5", ""
	if _, err := a.Judge(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	req.Model, req.Effort = "", "high"
	if _, err := a.Judge(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	calls := fakecli.Calls(t)
	want := [][]string{wantJudgeArgs("sonnet", "low"), wantJudgeArgs("claude-opus-5-5", "medium"), wantJudgeArgs("haiku", "high")}
	if len(calls) != len(want) {
		t.Fatalf("calls = %+v", calls)
	}
	for i, w := range want {
		if !slices.Equal(calls[i].Args, w) {
			t.Errorf("call %d args = %q", i, calls[i].Args)
		}
	}
}

func TestCheckJudgeArgsRefuses(t *testing.T) {
	good := judgeArgs("haiku", "medium")
	if err := checkJudgeArgs(good); err != nil {
		t.Fatalf("own args refused: %v", err)
	}
	if err := checkJudgeArgs(judgeArgs("claude-sonnet-5-5[1m]", "max")); err != nil {
		t.Fatalf("full model name refused: %v", err)
	}
	set := func(i int, v string) []string { s := slices.Clone(good); s[i] = v; return s }
	add := func(extra ...string) []string { return append(slices.Clone(good), extra...) }
	drop := func(i int) []string { return slices.Delete(slices.Clone(good), i, i+1) }
	bad := map[string][]string{
		"empty":                    nil,
		"permission bypass":        add("--dangerously-skip-permissions"),
		"bypass in place":          set(11, "--dangerously-skip-permissions"),
		"allow dangerously":        set(10, "--allow-dangerously-skip-permissions"),
		"permission mode":          add("--permission-mode", "bypassPermissions"),
		"permission mode in place": append(drop(12)[:12], "--permission-mode", "bypassPermissions"),
		"allowedTools":             add("--allowedTools", "Bash"),
		"allowedTools for tools":   set(7, "--allowedTools"),
		"tools Bash":               set(8, "Bash"),
		"tools default":            set(8, "default"),
		"web tools":                set(8, askWebTools),
		"no safe mode":             drop(11),
		"safe mode swapped":        set(11, "--verbose"),
		"no session persistence":   set(10, "--debug"),
		"no strict mcp":            set(9, "--bare"),
		"resume":                   add("--resume", "12345678-1234-4234-8234-123456789abc"),
		"continue":                 set(10, "--continue"),
		"mcp config":               add("--mcp-config", "x.json"),
		"settings":                 add("--settings", "{}"),
		"add dir":                  add("--add-dir", "C:\\"),
		"schema one byte":          set(13, strings.Replace(core.JudgeSchemaV1, "absent", "absenT", 1)),
		"schema trailing space":    set(13, core.JudgeSchemaV1+" "),
		"other schema":             set(13, `{"type":"object"}`),
		"prompt argument":          add("사과는 몇 개인가요?"),
		"prompt in place of -p":    set(0, "사과는 몇 개인가요?"),
		"twice":                    add("--model", "opus"),
		"model as a flag":          judgeArgs("--dangerously-skip-permissions", "medium"),
		"model with slash":         judgeArgs("a/b", "medium"),
		"unknown effort":           judgeArgs("haiku", "ultra"),
		"effort as a flag":         judgeArgs("haiku", "--permission-mode"),
		"stream json":              set(2, "stream-json"),
		"text output":              set(2, "text"),
		"model and effort swapped": {"-p", "--output-format", "json", "--effort", "medium", "--model", "haiku", "--tools", "", "--strict-mcp-config", "--no-session-persistence", "--safe-mode", "--json-schema", core.JudgeSchemaV1},
		"ask args":                 askArgs("haiku", "medium", ""),
		"ask resume args":          askArgs("haiku", "medium", "12345678-1234-4234-8234-123456789abc"),
	}
	for name, args := range bad {
		if err := checkJudgeArgs(args); !errors.Is(err, ErrArgRejected) {
			t.Errorf("%s: %q accepted (%v)", name, args, err)
		}
	}
	// Each checker refuses the other's shape, and the Runner's refuses
	// a judge outright.
	if err := checkAskArgs(good); !errors.Is(err, ErrArgRejected) {
		t.Errorf("checkAskArgs accepted the judge args (%v)", err)
	}
	if err := checkArgs(good); !errors.Is(err, ErrArgRejected) {
		t.Errorf("Runner's checkArgs accepted the judge args (%v)", err)
	}
}

func TestJudgeOutputs(t *testing.T) {
	a, folder := newJudgeAdapter(t)
	req, tk, _ := judgeRequest(t, folder)
	for mode, check := range map[string]func(judge.Validated) bool{
		fakecli.ModeRule: func(v judge.Validated) bool { return len(v.Verdicts) == 2 && len(v.Missing) == 0 },
		fakecli.ModeUnsure: func(v judge.Validated) bool {
			return len(v.Verdicts) == 2 && v.Verdicts[0].Unsure && v.Verdicts[1].Unsure
		},
		// A missing id is the validator's to find, not the adapter's.
		fakecli.ModeMissingID: func(v judge.Validated) bool { return len(v.Missing) == 1 && v.Missing[0] == "a1" },
	} {
		t.Setenv(fakecli.EnvJudge, mode)
		res, err := a.Judge(context.Background(), req)
		if err != nil {
			t.Errorf("%s: %v", mode, err)
			continue
		}
		v, err := judge.Validate(tk, res.Output)
		if err != nil || !check(v) {
			t.Errorf("%s: %+v %v (output %s)", mode, v, err, res.Output)
		}
	}
}

func TestJudgeErrors(t *testing.T) {
	a, folder := newJudgeAdapter(t)
	req, _, _ := judgeRequest(t, folder)
	for mode, want := range map[string]string{
		fakecli.ModeUsageLimit: core.ErrCodeJudgeFailed,
		fakecli.ModeProse:      core.ErrCodeJudgeBadOutput,
		fakecli.ModeBadJSON:    core.ErrCodeJudgeBadOutput,
		fakecli.ModeToolUse:    core.ErrCodeJudgeUsedTool,
		fakecli.ModeBadModel:   core.ErrCodeBadModel,
	} {
		t.Setenv(fakecli.EnvJudge, mode)
		res, err := a.Judge(context.Background(), req)
		if core.ErrorCode(err) != want {
			t.Errorf("%s: err = %v, want %s", mode, err, want)
		}
		if len(res.Output) != 0 || res.Cleanup != "" {
			t.Errorf("%s: output %s cleanup %q", mode, res.Output, res.Cleanup)
		}
		if res.Model == "" {
			t.Errorf("%s: no model in the result", mode)
		}
		if d := judgeDetail(err); strings.Contains(d, "judgments") || strings.Contains(d, fakecli.ProseText) {
			t.Errorf("%s: detail %q", mode, d)
		}
	}
	t.Setenv(fakecli.EnvJudge, fakecli.ModeUsageLimit)
	if _, err := a.Judge(context.Background(), req); judgeDetail(err) != fakecli.UsageLimitClaude {
		t.Errorf("usage limit detail = %q", judgeDetail(err))
	}
	// A tool the judge reached for still reports what it cost.
	t.Setenv(fakecli.EnvJudge, fakecli.ModeToolUse)
	if res, _ := a.Judge(context.Background(), req); res.InputTokens != 15 || res.OutputTokens != 7 || res.Model != "claude-haiku-fake" {
		t.Errorf("tool use result = %+v", res)
	}
	t.Setenv(fakecli.EnvJudge, "")

	// Refused before claude runs.
	n := len(fakecli.Calls(t))
	for name, c := range map[string]struct {
		req  func(core.JudgeRequest) core.JudgeRequest
		code string
	}{
		"empty prompt":     {func(r core.JudgeRequest) core.JudgeRequest { r.Prompt = ""; return r }, core.ErrCodeInternal},
		"blank prompt":     {func(r core.JudgeRequest) core.JudgeRequest { r.Prompt = " \n\t"; return r }, core.ErrCodeInternal},
		"no folder":        {func(r core.JudgeRequest) core.JudgeRequest { r.Folder = ""; return r }, core.ErrCodeInternal},
		"relative folder":  {func(r core.JudgeRequest) core.JudgeRequest { r.Folder = "judge"; return r }, core.ErrCodeInternal},
		"missing folder":   {func(r core.JudgeRequest) core.JudgeRequest { r.Folder = filepath.Join(folder, "gone"); return r }, core.ErrCodeInternal},
		"model with slash": {func(r core.JudgeRequest) core.JudgeRequest { r.Model = "a/b"; return r }, core.ErrCodeBadModel},
		"model as a flag":  {func(r core.JudgeRequest) core.JudgeRequest { r.Model = "--dangerously-skip-permissions"; return r }, core.ErrCodeBadModel},
		"model with space": {func(r core.JudgeRequest) core.JudgeRequest { r.Model = "haiku --safe-mode"; return r }, core.ErrCodeBadModel},
		"unknown effort":   {func(r core.JudgeRequest) core.JudgeRequest { r.Effort = "ultra"; return r }, core.ErrCodeBadModel},
		"effort as a flag": {func(r core.JudgeRequest) core.JudgeRequest { r.Effort = "--verbose"; return r }, core.ErrCodeBadModel},
	} {
		res, err := a.Judge(context.Background(), c.req(req))
		if core.ErrorCode(err) != c.code {
			t.Errorf("%s: err = %v, want %s", name, err, c.code)
		}
		if len(res.Output) != 0 {
			t.Errorf("%s: output %s", name, res.Output)
		}
	}
	if got := len(fakecli.Calls(t)); got != n {
		t.Errorf("refused judges ran claude %d times", got-n)
	}

	// No claude, or claude only as a script: nothing runs.
	missing := New(Options{ClaudeDir: t.TempDir(), ClaudePath: filepath.Join(t.TempDir(), "claude.exe")})
	if _, err := missing.Judge(context.Background(), req); core.ErrorCode(err) != core.ErrCodeAgentUnavailable {
		t.Errorf("missing claude: %v", err)
	}
	script := New(Options{ClaudeDir: t.TempDir(), ClaudePath: "claude.cmd"})
	if _, err := script.Judge(context.Background(), req); core.ErrorCode(err) != core.ErrCodeAgentUnavailable {
		t.Errorf("claude.cmd: %v", err)
	}
}

// A judge that does not answer in time is ended; a shorter deadline of
// the caller wins over JudgeTimeout.
func TestJudgeTimeout(t *testing.T) {
	a, folder := newJudgeAdapter(t)
	req, _, _ := judgeRequest(t, folder)
	t.Setenv(fakecli.EnvJudge, fakecli.ModeSlow)
	t.Setenv(fakecli.EnvSleep, "20s")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	res, err := a.Judge(ctx, req)
	if core.ErrorCode(err) != core.ErrCodeJudgeTimeout || time.Since(start) > 10*time.Second {
		t.Errorf("err = %v after %s", err, time.Since(start))
	}
	if len(res.Output) != 0 {
		t.Errorf("output %s", res.Output)
	}
	// A context that ended before the call: proc.Run gives the context's
	// own error, which is still a timeout and runs nothing.
	ended, cancelEnded := context.WithCancel(context.Background())
	cancelEnded()
	if _, err := a.Judge(ended, req); core.ErrorCode(err) != core.ErrCodeJudgeTimeout {
		t.Errorf("ended context: %v", err)
	}
}

// Judge sets its own time limit: a caller with no deadline still gets
// judge_timeout once judgeTimeout is up.
func TestJudgeOwnTimeout(t *testing.T) {
	if judgeTimeout != core.JudgeTimeout {
		t.Fatalf("judgeTimeout = %v, want core.JudgeTimeout", judgeTimeout)
	}
	a, folder := newJudgeAdapter(t)
	req, _, _ := judgeRequest(t, folder)
	t.Setenv(fakecli.EnvJudge, fakecli.ModeSlow)
	t.Setenv(fakecli.EnvSleep, "20s")
	old := judgeTimeout
	judgeTimeout = 300 * time.Millisecond
	t.Cleanup(func() { judgeTimeout = old })
	start := time.Now()
	res, err := a.Judge(context.Background(), req)
	if core.ErrorCode(err) != core.ErrCodeJudgeTimeout || time.Since(start) > 10*time.Second {
		t.Errorf("err = %v after %s", err, time.Since(start))
	}
	if len(res.Output) != 0 {
		t.Errorf("output %s", res.Output)
	}
}

// When claude fails after it made the judge JSON, the message comes from
// stderr: the judge JSON in result never becomes a message.
func TestJudgeErrorHasNoJudgeJSON(t *testing.T) {
	a, folder := newJudgeAdapter(t)
	req, _, _ := judgeRequest(t, folder)
	t.Setenv(fakecli.EnvJudge, fakecli.ModeFailedWithOutput)
	res, err := a.Judge(context.Background(), req)
	if core.ErrorCode(err) != core.ErrCodeJudgeFailed {
		t.Fatalf("err = %v", err)
	}
	if d := judgeDetail(err); d != fakecli.OverloadedStderr || strings.Contains(err.Error(), "judgments") {
		t.Errorf("detail = %q, error %q", d, err)
	}
	if len(res.Output) != 0 {
		t.Errorf("output %s", res.Output)
	}
}

// parseJudgeOutput on outputs the fake does not make.
func TestParseJudgeOutput(t *testing.T) {
	const obj = `{"judgments":[]}`
	long := strings.Repeat("요금 한도에 걸렸습니다. ", 100)
	cases := []struct {
		name, stdout, stderr string
		exit                 int
		code, detail, output string
	}{
		{name: "object", stdout: `{"type":"result","subtype":"success","is_error":false,"result":"x","structured_output":` + obj + `}`, output: obj},
		{name: "JSON string", stdout: `{"type":"result","is_error":false,"structured_output":"{\"judgments\":[]}"}`, output: obj},
		{name: "last result line wins", stdout: `{"type":"result","is_error":true,"result":"old"}` + "\r\n" + `{"type":"system"}` + "\n" + `{"type":"result","is_error":false,"structured_output":` + obj + "}\r\n", output: obj},
		{name: "null", stdout: `{"type":"result","is_error":false,"result":"done","structured_output":null}`, code: core.ErrCodeJudgeBadOutput},
		{name: "empty string", stdout: `{"type":"result","is_error":false,"structured_output":"  "}`, code: core.ErrCodeJudgeBadOutput},
		{name: "no result line", stdout: "not json\n", exit: 1, code: core.ErrCodeJudgeFailed, detail: "claude ended with exit code 1 and no result"},
		{name: "no result, stderr", stderr: "Error: something broke\n  at x", exit: 1, code: core.ErrCodeJudgeFailed, detail: "Error: something broke at x"},
		{name: "no result, unknown model", stderr: `[claude-code:unrecognized_model] {"model":"x"}`, exit: 1, code: core.ErrCodeBadModel, detail: "haiku"},
		{name: "no result, judge JSON on stderr", stderr: `{"judgments":[{"id":"a1"}]}`, exit: 1, code: core.ErrCodeJudgeFailed, detail: "claude ended with exit code 1 and no result"},
		{name: "exit code without is_error", stdout: `{"type":"result","is_error":false,"result":` + `"{\"judgments\":[]}","structured_output":` + obj + `}`, stderr: "killed", exit: 3, code: core.ErrCodeJudgeFailed, detail: "killed"},
		{name: "is_error, judge JSON in result, no stderr", stdout: `{"type":"result","is_error":true,"result":"{\"judgments\":[]}"}`, exit: 1, code: core.ErrCodeJudgeFailed, detail: "claude reported an error (exit code 1)"},
		{name: "is_error with output and result text", stdout: `{"type":"result","is_error":true,"result":"some words","structured_output":` + obj + `}`, exit: 1, code: core.ErrCodeJudgeFailed, detail: "claude reported an error (exit code 1)"},
		{name: "is_error, result preferred to stderr", stdout: `{"type":"result","is_error":true,"result":"API Error: 500"}`, stderr: "trace", exit: 1, code: core.ErrCodeJudgeFailed, detail: "API Error: 500"},
		{name: "api 404", stdout: `{"type":"result","is_error":true,"api_error_status":404,"result":"model not found"}`, exit: 1, code: core.ErrCodeBadModel, detail: "haiku"},
		{name: "api 404 on success is no error", stdout: `{"type":"result","is_error":false,"api_error_status":404,"structured_output":` + obj + `}`, output: obj},
		{name: "schema retries", stdout: `{"type":"result","subtype":"error_max_structured_output_retries","is_error":true}`, exit: 1, code: core.ErrCodeJudgeBadOutput},
		{name: "denials first", stdout: `{"type":"result","is_error":true,"subtype":"error_max_structured_output_retries","permission_denials":[{"tool_name":"WebFetch"}],"structured_output":` + obj + `}`, exit: 1, code: core.ErrCodeJudgeUsedTool},
		{name: "denials not a list", stdout: `{"type":"result","is_error":false,"permission_denials":{"tool_name":"Bash"},"structured_output":` + obj + `}`, code: core.ErrCodeJudgeFailed},
		{name: "long message", stdout: `{"type":"result","is_error":true,"result":"` + long + `"}`, exit: 1, code: core.ErrCodeJudgeFailed},
	}
	for _, c := range cases {
		res, err := parseJudgeOutput(proc.Result{Stdout: []byte(c.stdout), Stderr: []byte(c.stderr), ExitCode: c.exit}, "haiku")
		if core.ErrorCode(err) != c.code {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.code)
			continue
		}
		if c.detail != "" && judgeDetail(err) != c.detail {
			t.Errorf("%s: detail = %q, want %q", c.name, judgeDetail(err), c.detail)
		}
		if string(res.Output) != c.output {
			t.Errorf("%s: output = %s, want %s", c.name, res.Output, c.output)
		}
		if d := judgeDetail(err); utf8.RuneCountInString(d) > core.JudgeMessageChars || strings.Contains(d, "judgments") {
			t.Errorf("%s: detail %q", c.name, d)
		}
	}
	if _, err := parseJudgeOutput(proc.Result{Stdout: []byte(`{"type":"result","is_error":true,"result":"` + long + `"}`), ExitCode: 1}, "haiku"); utf8.RuneCountInString(judgeDetail(err)) != core.JudgeMessageChars {
		t.Errorf("long detail has %d characters", utf8.RuneCountInString(judgeDetail(err)))
	}
}

func TestFamily(t *testing.T) {
	a := New(Options{ClaudeDir: t.TempDir(), ClaudePath: "claude.cmd"}) // Family needs no claude
	for model, want := range map[string]string{
		"haiku": "anthropic", "sonnet": "anthropic", "opus": "anthropic", "default": "anthropic",
		"best": "anthropic", "opusplan": "anthropic", "Sonnet": "anthropic", "OPUS": "anthropic",
		"sonnet[1m]": "anthropic", "opus[1M]": "anthropic", "claude-sonnet-5-5": "anthropic",
		"claude-sonnet-5-5[1m]": "anthropic", "Claude-Haiku-4-5-20251001": "anthropic", "claude-opus-4-7": "anthropic",
		"": "", "[1m]": "", "claude": "", "claude-": "", "claude-[1m]": "", "haiku-x": "", "sonnet[2m]": "",
		"gpt-6-luna": "", "deepseek-v4.1-flash": "", "opencode-go/deepseek-v4.1-flash#max": "",
		"anthropic/claude-sonnet-5-5": "", "us.anthropic.claude-sonnet-5-5": "", "gemini-3-pro": "", "unknown": "",
	} {
		if got := a.Family(model); got != want {
			t.Errorf("Family(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestJudgeDefault(t *testing.T) {
	a := New(Options{ClaudeDir: t.TempDir(), ClaudePath: "claude.cmd"})
	def := a.JudgeDefault()
	if def != (core.AskModel{Model: "haiku", Effort: "medium"}) {
		t.Errorf("JudgeDefault = %+v", def)
	}
	if !config.ValidModel(def.Model) || !config.ValidEffort(def.Effort) || a.Family(def.Model) != "anthropic" {
		t.Errorf("default %+v is not a valid Anthropic model", def)
	}
	if err := checkJudgeArgs(judgeArgs(def.Model, def.Effort)); err != nil {
		t.Errorf("default args refused: %v", err)
	}
	if !core.VersionAtLeast(JudgeMinVersion, "2.1.314") || JudgeMinVersion != fakecli.Versions[fakecli.RoleClaude] {
		t.Errorf("JudgeMinVersion = %q", JudgeMinVersion)
	}
	var j core.Judger = a
	if _, ok := j.(core.JudgeCleaner); ok {
		t.Error("claude keeps no judge conversation and needs no cleaner")
	}
}
