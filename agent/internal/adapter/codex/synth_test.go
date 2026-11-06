package codex

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

func synthTask() judge.SynthTask {
	return judge.SynthTask{
		Question: "첫 배는 몇 시인가요? \"정확히\" & 100%",
		Answers:  []judge.SynthAnswer{{AI: "Claude", Text: "7시입니다."}, {AI: "Codex", Text: "8시입니다."}},
	}
}

func synthPrompt(t *testing.T) string {
	t.Helper()
	tk := synthTask()
	p := judge.SynthPrompt(tk, judge.SynthNonce(tk))
	if p == "" {
		t.Fatal("judge.SynthPrompt refused the task")
	}
	return p
}

func synthWrites(a *Adapter) int {
	a.synthMu.Lock()
	defer a.synthMu.Unlock()
	return a.synthWrites
}

func TestSynthArgsAndStdin(t *testing.T) {
	a, schemaDir, folder := judgeAdapter(t)
	p := synthPrompt(t)
	res, err := a.Synthesize(context.Background(), core.SynthRequest{Folder: folder, Prompt: p, Model: "gpt-6.1-terra", Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	schema := filepath.Join(schemaDir, core.SynthSchemaFile)
	calls := fakecli.Calls(t)
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	c := calls[0]
	if want := wantJudgeArgs("gpt-6.1-terra", "high", schema); !slices.Equal(c.Args, want) {
		t.Errorf("args = %q\nwant %q", c.Args, want)
	}
	if c.Stdin != p || !sameDir(c.Dir, folder) || c.Schema != core.SynthSchemaV1 {
		t.Errorf("call = dir %s, stdin %d bytes, schema %q", c.Dir, len(c.Stdin), c.Schema)
	}
	for _, arg := range c.Args {
		if strings.Contains(arg, "\n") || strings.Contains(arg, "첫 배") || strings.Contains(arg, "data-") {
			t.Errorf("prompt text in argument %q", arg)
		}
	}
	if res.Model != "gpt-6.1-terra" || res.InputTokens != 300 || res.OutputTokens != 40 || res.Cleanup != "" {
		t.Errorf("result = %+v", res)
	}
	text, err := judge.ValidateSynth(res.Output)
	if err != nil || !strings.HasPrefix(text, fakecli.SynthLead) {
		t.Fatalf("ValidateSynth: %q %v", text, err)
	}
	// An empty effort becomes medium.
	if _, err := a.Synthesize(context.Background(), core.SynthRequest{Folder: folder, Prompt: p, Model: "gpt-6-luna"}); err != nil {
		t.Fatal(err)
	}
	if got := fakecli.Calls(t)[1].Args; !slices.Equal(got, wantJudgeArgs("gpt-6-luna", "medium", schema)) {
		t.Errorf("empty effort args = %q", got)
	}
	// The ask and synth checks each refuse the other's shape.
	if err := checkArgs(c.Args); err == nil {
		t.Error("the ask check accepted synth arguments")
	}
	if err := a.checkSynthArgs(c.Args); err != nil {
		t.Errorf("the synth check refused its own arguments: %v", err)
	}
	if err := a.checkSynthArgs(askArgs("gpt-6-luna", "medium", "", false)); err == nil {
		t.Error("the synth check accepted ask arguments")
	}
}

// The synth schema file is written once, also when the first calls come
// at once, and every call reads it; a changed file is refused, never
// repaired.
func TestSynthSchemaFile(t *testing.T) {
	a, schemaDir, folder := judgeAdapter(t)
	sp := synthPrompt(t)
	var wg sync.WaitGroup
	errs := make([]error, 6)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = a.Synthesize(context.Background(), core.SynthRequest{Folder: folder, Prompt: sp, Model: "gpt-6-luna"})
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("synth %d: %v", i, err)
		}
	}
	if synthWrites(a) != 1 {
		t.Errorf("synth schema written %d times", synthWrites(a))
	}
	if b, err := os.ReadFile(filepath.Join(schemaDir, core.SynthSchemaFile)); err != nil || string(b) != core.SynthSchemaV1 {
		t.Errorf("synth schema file = %q, %v", b, err)
	}
	for _, c := range fakecli.Calls(t) {
		if filepath.Base(c.Args[14]) != core.SynthSchemaFile || c.Schema != core.SynthSchemaV1 {
			t.Errorf("call with %s read %q", c.Args[14], c.Schema)
		}
	}
	// Only the schema file is left in the folder (no temporary files).
	if entries, _ := os.ReadDir(schemaDir); len(entries) != 1 {
		t.Errorf("schema folder holds %d entries", len(entries))
	}

	// Changed after this process wrote it: refused, never repaired.
	synthFile := filepath.Join(schemaDir, core.SynthSchemaFile)
	tampered := strings.Replace(core.SynthSchemaV1, `"additionalProperties":false`, `"additionalProperties":true`, 1)
	if err := os.WriteFile(synthFile, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	n := len(fakecli.Calls(t))
	_, err := a.Synthesize(context.Background(), core.SynthRequest{Folder: folder, Prompt: sp, Model: "gpt-6-luna"})
	if ae := actionError(t, err); ae.Code != core.ErrCodeInternal || !strings.Contains(ae.Detail, "restart") {
		t.Errorf("tampered: %v", err)
	}
	if b, _ := os.ReadFile(synthFile); string(b) != tampered || len(fakecli.Calls(t)) != n {
		t.Error("the tampered file was rewritten or codex ran")
	}
	// A new process writes over it once.
	b := New(Options{Path: a.exe, SchemaDir: schemaDir})
	if _, err := b.Synthesize(context.Background(), core.SynthRequest{Folder: folder, Prompt: sp, Model: "gpt-6-luna"}); err != nil || synthWrites(b) != 1 {
		t.Errorf("new process: %v, writes %d", err, synthWrites(b))
	}
	// A new process that finds the right bytes does not write at all.
	c := New(Options{Path: a.exe, SchemaDir: schemaDir})
	if _, err := c.Synthesize(context.Background(), core.SynthRequest{Folder: folder, Prompt: sp, Model: "gpt-6-luna"}); err != nil || synthWrites(c) != 0 {
		t.Errorf("right file: %v, writes %d", err, synthWrites(c))
	}
	// No agent folder: internal, nothing runs.
	d := New(Options{Path: a.exe, SchemaDir: schemaDir})
	d.schemaDir = ""
	n = len(fakecli.Calls(t))
	if _, err := d.Synthesize(context.Background(), core.SynthRequest{Folder: folder, Prompt: sp, Model: "gpt-6-luna"}); core.ErrorCode(err) != core.ErrCodeInternal || len(fakecli.Calls(t)) != n {
		t.Errorf("no schema folder: %v", err)
	}
}

func TestCheckSynthArgsRefuses(t *testing.T) {
	a := New(Options{Path: filepath.Join(t.TempDir(), "codex.exe"), SchemaDir: t.TempDir()})
	schema := a.synthSchemaPath()
	good := synthArgs("gpt-6-luna", "medium", schema)
	if err := a.checkSynthArgs(good); err != nil {
		t.Fatalf("own args refused: %v", err)
	}
	set := func(i int, v string) []string { s := slices.Clone(good); s[i] = v; return s }
	bad := map[string][]string{
		"old judge schema": synthArgs("gpt-6-luna", "medium", filepath.Join(a.schemaDir, "judge-schema-v1.json")),
		"other schema":     synthArgs("gpt-6-luna", "medium", filepath.Join(t.TempDir(), core.SynthSchemaFile)),
		"relative schema":  synthArgs("gpt-6-luna", "medium", core.SynthSchemaFile),
		"search":           append([]string{"--search"}, good...),
		"web search on":    set(12, `web_search="live"`),
		"no ephemeral":     set(5, "--json"),
		"workspace write":  set(2, "workspace-write"),
		"bypass":           append(slices.Clone(good[:len(good)-1]), "--dangerously-bypass-approvals-and-sandbox", "-"),
		"feature on":       set(16, "--enable"),
		"model as a flag":  synthArgs("--full-auto", "medium", schema),
		"unknown effort":   synthArgs("gpt-6-luna", "turbo", schema),
		"prompt argument":  set(len(good)-1, "첫 배는 몇 시인가요?"),
		"short":            good[:len(good)-1],
		"ask args":         askArgs("gpt-6-luna", "medium", "", false),
		"ask resume args":  askArgs("gpt-6-luna", "medium", "0b6f2a8e-1111-4222-8333-444455556666", false),
		"profile":          set(3, "--profile"),
		"config overrides": set(11, `model_provider="x"`),
	}
	for name, args := range bad {
		if err := a.checkSynthArgs(args); err == nil {
			t.Errorf("%s: %q accepted", name, args)
		}
	}
	none := New(Options{Path: filepath.Join(t.TempDir(), "codex.exe"), SchemaDir: t.TempDir()})
	none.schemaDir = ""
	if err := none.checkSynthArgs(synthArgs("gpt-6-luna", "medium", "")); err == nil {
		t.Error("an empty schema path accepted")
	}
}

func TestSynthModes(t *testing.T) {
	a, _, folder := judgeAdapter(t)
	p := synthPrompt(t)
	req := core.SynthRequest{Folder: folder, Prompt: p, Model: "gpt-6-luna"}
	for mode, want := range map[string]string{
		fakecli.SynthUsageLimit:       core.ErrCodeJudgeFailed,
		fakecli.SynthToolUse:          core.ErrCodeJudgeUsedTool,
		fakecli.SynthBadModel:         core.ErrCodeBadModel,
		fakecli.SynthFailedWithOutput: core.ErrCodeJudgeFailed,
	} {
		t.Setenv(fakecli.EnvSynth, mode)
		res, err := a.Synthesize(context.Background(), req)
		if core.ErrorCode(err) != want || len(res.Output) != 0 || res.Model != "gpt-6-luna" {
			t.Errorf("%s: %+v %v, want %s", mode, res, err, want)
		}
		if err != nil && strings.Contains(err.Error(), fakecli.SynthLead) {
			t.Errorf("%s: the answer in the error %q", mode, err)
		}
	}
	t.Setenv(fakecli.EnvSynth, fakecli.SynthUsageLimit)
	if _, err := a.Synthesize(context.Background(), req); actionError(t, err).Detail != fakecli.UsageLimitCodex {
		t.Errorf("usage limit detail = %q", actionError(t, err).Detail)
	}
	// Output that is not a combined answer comes back for the validator.
	for mode, check := range map[string]func(string, error) bool{
		fakecli.SynthProse:        func(_ string, err error) bool { return err != nil },
		fakecli.SynthBadJSON:      func(_ string, err error) bool { return err != nil },
		fakecli.SynthEmpty:        func(_ string, err error) bool { return err != nil },
		fakecli.SynthMissingNames: func(text string, err error) bool { return err == nil && text == fakecli.SynthMissingNamesText },
	} {
		t.Setenv(fakecli.EnvSynth, mode)
		res, err := a.Synthesize(context.Background(), req)
		if err != nil {
			t.Errorf("%s: %v", mode, err)
			continue
		}
		if text, verr := judge.ValidateSynth(res.Output); !check(text, verr) {
			t.Errorf("%s: %q %v", mode, text, verr)
		}
	}
}

func TestSynthTimeoutAndGuards(t *testing.T) {
	if synthTimeout != core.SynthTimeout {
		t.Fatalf("synthTimeout = %v", synthTimeout)
	}
	a, _, folder := judgeAdapter(t)
	p := synthPrompt(t)
	t.Setenv(fakecli.EnvSynth, fakecli.SynthSlow)
	t.Setenv(fakecli.EnvSleep, "20s")
	old := synthTimeout
	synthTimeout = 300 * time.Millisecond
	t.Cleanup(func() { synthTimeout = old })
	start := time.Now()
	res, err := a.Synthesize(context.Background(), core.SynthRequest{Folder: folder, Prompt: p, Model: "gpt-6-luna"})
	if core.ErrorCode(err) != core.ErrCodeJudgeTimeout || time.Since(start) > 10*time.Second || len(res.Output) != 0 {
		t.Errorf("own limit: %v after %s", err, time.Since(start))
	}
	t.Setenv(fakecli.EnvSynth, "")
	n := len(fakecli.Calls(t))
	for name, c := range map[string]struct {
		req  core.SynthRequest
		code string
	}{
		"empty prompt":    {core.SynthRequest{Folder: folder, Model: "gpt-6-luna"}, core.ErrCodeInternal},
		"relative folder": {core.SynthRequest{Folder: "judge", Prompt: p, Model: "gpt-6-luna"}, core.ErrCodeInternal},
		"missing folder":  {core.SynthRequest{Folder: filepath.Join(folder, "gone"), Prompt: p, Model: "gpt-6-luna"}, core.ErrCodeInternal},
		"no model":        {core.SynthRequest{Folder: folder, Prompt: p}, core.ErrCodeBadModel},
		"model as a flag": {core.SynthRequest{Folder: folder, Prompt: p, Model: "--full-auto"}, core.ErrCodeBadModel},
		"unknown effort":  {core.SynthRequest{Folder: folder, Prompt: p, Model: "gpt-6-luna", Effort: "turbo"}, core.ErrCodeBadModel},
	} {
		if _, err := a.Synthesize(context.Background(), c.req); core.ErrorCode(err) != c.code {
			t.Errorf("%s: %v, want %s", name, err, c.code)
		}
	}
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Synthesize(ended, core.SynthRequest{Folder: folder, Prompt: p, Model: "gpt-6-luna"}); core.ErrorCode(err) != core.ErrCodeJudgeTimeout {
		t.Errorf("ended context: %v", err)
	}
	if got := len(fakecli.Calls(t)); got != n {
		t.Errorf("refused requests ran codex %d times", got-n)
	}
	missing := New(Options{Path: filepath.Join(t.TempDir(), "codex.exe"), SchemaDir: t.TempDir()})
	if _, err := missing.Synthesize(context.Background(), core.SynthRequest{Folder: folder, Prompt: p, Model: "gpt-6-luna"}); core.ErrorCode(err) != core.ErrCodeAgentUnavailable {
		t.Errorf("missing codex: %v", err)
	}
	var named core.Named = a
	if named.DisplayName() != "Codex" {
		t.Errorf("DisplayName = %q", named.DisplayName())
	}
}
