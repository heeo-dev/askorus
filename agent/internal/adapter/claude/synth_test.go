package claude

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// synthTask has two answers and one 다른 말 item: Claude against Codex.
func synthTask() judge.SynthTask {
	return judge.SynthTask{
		Question: "첫 배는 몇 시인가요? \"따옴표\" & 100%",
		Answers: []judge.SynthAnswer{
			{AI: "Claude", Text: "첫 배는 7시입니다."},
			{AI: "Codex", Text: "첫 배는 8시입니다. --dangerously-skip-permissions"},
		},
		Differ: []judge.SynthDiffer{{Sides: []judge.SynthSide{
			{AIs: []string{"Claude"}, Says: []string{"첫 배는 7시입니다."}},
			{AIs: []string{"Codex"}, Says: []string{"첫 배는 8시입니다."}},
		}}},
		Compared: true,
	}
}

func synthRequest(t *testing.T, folder string) (core.SynthRequest, string) {
	t.Helper()
	tk := synthTask()
	nonce := judge.SynthNonce(tk)
	p := judge.SynthPrompt(tk, nonce)
	if p == "" {
		t.Fatal("judge.SynthPrompt refused the task")
	}
	return core.SynthRequest{Folder: folder, Prompt: p, Model: "sonnet", Effort: "medium"}, nonce
}

func wantSynthArgs(model, effort string) []string {
	return []string{"-p", "--output-format", "json", "--model", model, "--effort", effort,
		"--tools", "", "--strict-mcp-config", "--no-session-persistence", "--safe-mode",
		"--json-schema", core.SynthSchemaV1}
}

func TestSynthArgsAndStdin(t *testing.T) {
	a, folder := newJudgeAdapter(t)
	req, nonce := synthRequest(t, folder)
	res, err := a.Synthesize(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	calls := fakecli.Calls(t)
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	c := calls[0]
	if !slices.Equal(c.Args, wantSynthArgs("sonnet", "medium")) {
		t.Errorf("args = %q", c.Args)
	}
	if len(c.Args) != 14 || c.Args[8] != "" || c.Args[13] != core.SynthSchemaV1 {
		t.Errorf("--tools or schema = %q", c.Args)
	}
	if c.Stdin != req.Prompt {
		t.Errorf("stdin = %q", c.Stdin)
	}
	if !sameFolder(c.Dir, folder) {
		t.Errorf("dir = %q, want %q", c.Dir, folder)
	}
	for _, arg := range c.Args {
		for _, s := range []string{"첫 배", "따옴표", "Codex", nonce, "dangerously"} {
			if strings.Contains(arg, s) {
				t.Errorf("argument %q holds %q", arg, s)
			}
		}
	}
	if res.Model != "claude-sonnet-fake" || res.InputTokens != 15 || res.OutputTokens != 7 || res.Cleanup != "" {
		t.Errorf("result = %+v", res)
	}
	text, err := judge.ValidateSynth(res.Output)
	if err != nil {
		t.Fatalf("ValidateSynth: %v (output %s)", err, res.Output)
	}
	if !strings.HasPrefix(text, fakecli.SynthLead) || judge.UnnamedSides(synthTask(), text) != 0 {
		t.Errorf("text = %q", text)
	}
	// An empty effort becomes medium: the command needs one.
	req.Effort = ""
	if _, err := a.Synthesize(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if calls := fakecli.Calls(t); !slices.Equal(calls[1].Args, wantSynthArgs("sonnet", "medium")) {
		t.Errorf("empty effort args = %q", calls[1].Args)
	}
	req.Model, req.Effort = "claude-opus-5-5[1m]", "high"
	if _, err := a.Synthesize(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if calls := fakecli.Calls(t); !slices.Equal(calls[2].Args, wantSynthArgs("claude-opus-5-5[1m]", "high")) {
		t.Errorf("model args = %q", calls[2].Args)
	}
}

func TestCheckSynthArgsRefuses(t *testing.T) {
	good := synthArgs("sonnet", "medium")
	if err := checkSynthArgs(good); err != nil {
		t.Fatalf("own args refused: %v", err)
	}
	set := func(i int, v string) []string { s := slices.Clone(good); s[i] = v; return s }
	add := func(extra ...string) []string { return append(slices.Clone(good), extra...) }
	drop := func(i int) []string { return slices.Delete(slices.Clone(good), i, i+1) }
	bad := map[string][]string{
		"empty":                 nil,
		"permission bypass":     add("--dangerously-skip-permissions"),
		"bypass in place":       set(11, "--dangerously-skip-permissions"),
		"permission mode":       add("--permission-mode", "bypassPermissions"),
		"allowedTools":          add("--allowedTools", "WebSearch"),
		"tools default":         set(8, "default"),
		"web tools":             set(8, askWebTools),
		"no safe mode":          drop(11),
		"no strict mcp":         set(9, "--bare"),
		"resume":                add("--resume", "12345678-1234-4234-8234-123456789abc"),
		"mcp config":            add("--mcp-config", "x.json"),
		"judge schema":          set(13, core.JudgeSchemaV1),
		"schema one byte":       set(13, strings.Replace(core.SynthSchemaV1, "answer", "answeR", 1)),
		"schema trailing space": set(13, core.SynthSchemaV1+" "),
		"prompt argument":       add("첫 배는 몇 시인가요?"),
		"model as a flag":       synthArgs("--dangerously-skip-permissions", "medium"),
		"unknown effort":        synthArgs("sonnet", "ultra"),
		"empty effort":          synthArgs("sonnet", ""),
		"text output":           set(2, "text"),
		"judge args":            judgeArgs("sonnet", "medium"),
		"ask args":              askArgs("sonnet", "medium", ""),
	}
	for name, args := range bad {
		if err := checkSynthArgs(args); !errors.Is(err, ErrArgRejected) {
			t.Errorf("%s: %q accepted (%v)", name, args, err)
		}
	}
	// The judge's, the ask's and the Runner's checkers refuse this shape.
	if err := checkJudgeArgs(good); !errors.Is(err, ErrArgRejected) {
		t.Errorf("checkJudgeArgs accepted the synth args (%v)", err)
	}
	if err := checkAskArgs(good); !errors.Is(err, ErrArgRejected) {
		t.Errorf("checkAskArgs accepted the synth args (%v)", err)
	}
	if err := checkArgs(good); !errors.Is(err, ErrArgRejected) {
		t.Errorf("Runner's checkArgs accepted the synth args (%v)", err)
	}
}

func TestSynthErrors(t *testing.T) {
	a, folder := newJudgeAdapter(t)
	req, _ := synthRequest(t, folder)
	for mode, want := range map[string]string{
		fakecli.SynthUsageLimit:       core.ErrCodeJudgeFailed,
		fakecli.SynthProse:            core.ErrCodeJudgeBadOutput,
		fakecli.SynthBadJSON:          core.ErrCodeJudgeBadOutput,
		fakecli.SynthToolUse:          core.ErrCodeJudgeUsedTool,
		fakecli.SynthBadModel:         core.ErrCodeBadModel,
		fakecli.SynthFailedWithOutput: core.ErrCodeJudgeFailed,
	} {
		t.Setenv(fakecli.EnvSynth, mode)
		res, err := a.Synthesize(context.Background(), req)
		if core.ErrorCode(err) != want {
			t.Errorf("%s: err = %v, want %s", mode, err, want)
		}
		if len(res.Output) != 0 || res.Cleanup != "" || res.Model == "" {
			t.Errorf("%s: result %+v", mode, res)
		}
		if d := judgeDetail(err); strings.Contains(d, fakecli.SynthLead) || strings.Contains(d, fakecli.SynthProseText) {
			t.Errorf("%s: detail %q", mode, d)
		}
	}
	t.Setenv(fakecli.EnvSynth, fakecli.SynthUsageLimit)
	if _, err := a.Synthesize(context.Background(), req); judgeDetail(err) != fakecli.UsageLimitClaude {
		t.Errorf("usage limit detail = %q", judgeDetail(err))
	}
	// The writer's output that is not a combined answer is the validator's
	// to refuse, not the adapter's.
	for _, mode := range []string{fakecli.SynthEmpty, fakecli.SynthMissingNames} {
		t.Setenv(fakecli.EnvSynth, mode)
		res, err := a.Synthesize(context.Background(), req)
		if err != nil || len(res.Output) == 0 {
			t.Errorf("%s: %v %s", mode, err, res.Output)
		}
	}
	t.Setenv(fakecli.EnvSynth, "")

	n := len(fakecli.Calls(t))
	for name, c := range map[string]struct {
		req  func(core.SynthRequest) core.SynthRequest
		code string
	}{
		"empty prompt":     {func(r core.SynthRequest) core.SynthRequest { r.Prompt = ""; return r }, core.ErrCodeInternal},
		"blank prompt":     {func(r core.SynthRequest) core.SynthRequest { r.Prompt = " \n"; return r }, core.ErrCodeInternal},
		"no folder":        {func(r core.SynthRequest) core.SynthRequest { r.Folder = ""; return r }, core.ErrCodeInternal},
		"relative folder":  {func(r core.SynthRequest) core.SynthRequest { r.Folder = "judge"; return r }, core.ErrCodeInternal},
		"missing folder":   {func(r core.SynthRequest) core.SynthRequest { r.Folder = filepath.Join(folder, "gone"); return r }, core.ErrCodeInternal},
		"no model":         {func(r core.SynthRequest) core.SynthRequest { r.Model = ""; return r }, core.ErrCodeBadModel},
		"model as a flag":  {func(r core.SynthRequest) core.SynthRequest { r.Model = "--dangerously-skip-permissions"; return r }, core.ErrCodeBadModel},
		"model with space": {func(r core.SynthRequest) core.SynthRequest { r.Model = "sonnet --safe-mode"; return r }, core.ErrCodeBadModel},
		"unknown effort":   {func(r core.SynthRequest) core.SynthRequest { r.Effort = "ultra"; return r }, core.ErrCodeBadModel},
	} {
		res, err := a.Synthesize(context.Background(), c.req(req))
		if core.ErrorCode(err) != c.code || len(res.Output) != 0 {
			t.Errorf("%s: err = %v, want %s", name, err, c.code)
		}
	}
	if got := len(fakecli.Calls(t)); got != n {
		t.Errorf("refused requests ran claude %d times", got-n)
	}
	missing := New(Options{ClaudeDir: t.TempDir(), ClaudePath: filepath.Join(t.TempDir(), "claude.exe")})
	if _, err := missing.Synthesize(context.Background(), req); core.ErrorCode(err) != core.ErrCodeAgentUnavailable {
		t.Errorf("missing claude: %v", err)
	}
	script := New(Options{ClaudeDir: t.TempDir(), ClaudePath: "claude.cmd"})
	if _, err := script.Synthesize(context.Background(), req); core.ErrorCode(err) != core.ErrCodeAgentUnavailable {
		t.Errorf("claude.cmd: %v", err)
	}
}

// Synthesize sets its own time limit, apart from the judge's; a caller's
// shorter deadline still wins, and a context that already ended runs
// nothing.
func TestSynthTimeout(t *testing.T) {
	if synthTimeout != core.SynthTimeout {
		t.Fatalf("synthTimeout = %v, want core.SynthTimeout", synthTimeout)
	}
	a, folder := newJudgeAdapter(t)
	req, _ := synthRequest(t, folder)
	t.Setenv(fakecli.EnvSynth, fakecli.SynthSlow)
	t.Setenv(fakecli.EnvSleep, "20s")
	old := synthTimeout
	synthTimeout = 300 * time.Millisecond
	t.Cleanup(func() { synthTimeout = old })
	start := time.Now()
	res, err := a.Synthesize(context.Background(), req)
	if core.ErrorCode(err) != core.ErrCodeJudgeTimeout || time.Since(start) > 10*time.Second || len(res.Output) != 0 {
		t.Errorf("own limit: err = %v after %s", err, time.Since(start))
	}
	synthTimeout = old
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start = time.Now()
	if _, err := a.Synthesize(ctx, req); core.ErrorCode(err) != core.ErrCodeJudgeTimeout || time.Since(start) > 10*time.Second {
		t.Errorf("caller's deadline: err = %v after %s", err, time.Since(start))
	}
	ended, cancelEnded := context.WithCancel(context.Background())
	cancelEnded()
	n := len(fakecli.Calls(t))
	if _, err := a.Synthesize(ended, req); core.ErrorCode(err) != core.ErrCodeJudgeTimeout {
		t.Errorf("ended context: %v", err)
	}
	if got := len(fakecli.Calls(t)); got != n {
		t.Errorf("an ended context ran claude")
	}
}

func TestDisplayName(t *testing.T) {
	var n core.Named = New(Options{ClaudeDir: t.TempDir(), ClaudePath: "claude.cmd"})
	if n.DisplayName() != "Claude" {
		t.Errorf("DisplayName = %q", n.DisplayName())
	}
}
