package compares

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
	"github.com/asksemble/asksemble/agent/internal/logx"
	"github.com/asksemble/asksemble/agent/internal/runs"
)

// ---- fakes ----------------------------------------------------------------

// writerAI is a fakeAI that also asks and writes combined answers, as the
// three adapters do: by default the combined answer of fakecli's ok mode,
// fakecli.SynthLead.
type writerAI struct {
	*fakeAI
	display string
	tiers   map[string]core.AskModel
	leaves  bool // Synthesize returns a cleanup ref

	smu    sync.Mutex
	synth  func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error)
	synths []core.SynthRequest
	panicT bool // AskTiers panics
	panicN bool // DisplayName panics
}

func newWriter(name, family, display string) *writerAI {
	return &writerAI{fakeAI: newFake(name, family), display: display, tiers: map[string]core.AskModel{
		core.TierQuick:    {Model: name + "-quick", Effort: "low"},
		core.TierStandard: {Model: name + "-standard", Effort: "medium"},
		core.TierDeep:     {Model: name + "-deep"},
	}}
}

func (w *writerAI) Ask(context.Context, core.AskRequest) (core.AskResult, error) {
	return core.AskResult{}, nil
}

func (w *writerAI) AskTiers() map[string]core.AskModel {
	w.smu.Lock()
	defer w.smu.Unlock()
	if w.panicT {
		panic("tiers bug")
	}
	return w.tiers
}

func (w *writerAI) DisplayName() string {
	w.smu.Lock()
	defer w.smu.Unlock()
	if w.panicN {
		panic("name bug")
	}
	return w.display
}

func (w *writerAI) setSynth(f func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error)) {
	w.smu.Lock()
	w.synth = f
	w.smu.Unlock()
}

func (w *writerAI) Synthesize(ctx context.Context, req core.SynthRequest) (core.SynthResult, error) {
	w.smu.Lock()
	w.synths = append(w.synths, req)
	n, f, leaves := len(w.synths), w.synth, w.leaves
	w.smu.Unlock()
	if f != nil {
		return f(ctx, req, n)
	}
	res := okSynth(req)
	if leaves {
		res.Cleanup = fmt.Sprintf("%s-synth-%d", w.name, n)
	}
	return res, nil
}

// okSynth is fakecli's ok combined answer with the model asked for.
func okSynth(req core.SynthRequest) core.SynthResult {
	b, _ := json.Marshal(map[string]string{"answer": fakecli.SynthLead})
	return core.SynthResult{Output: b, Model: req.Model, InputTokens: 30, OutputTokens: 8}
}

func (w *writerAI) synthCalls() []core.SynthRequest {
	w.smu.Lock()
	defer w.smu.Unlock()
	return slices.Clone(w.synths)
}

// cleanerWriter is a writerAI whose writing leaves something behind, as
// OpenCode's does.
type cleanerWriter struct{ *writerAI }

func (c cleanerWriter) CleanupJudge(ctx context.Context, folder string, refs []string) error {
	return cleanerAI{c.fakeAI}.CleanupJudge(ctx, folder, refs)
}

// threeWriters are the reference PC's three AIs, which write combined
// answers; opencode leaves conversations behind.
func threeWriters() (*writerAI, *writerAI, *writerAI, []core.Adapter) {
	cl, cx, oc := newWriter("claude", "anthropic", "Claude"), newWriter("codex", "openai", "Codex"), newWriter("opencode", "deepseek", "OpenCode")
	oc.leaves = true
	return cl, cx, oc, []core.Adapter{cl, cx, cleanerWriter{oc}}
}

func writerOf(a core.Adapter) *writerAI {
	switch v := a.(type) {
	case *writerAI:
		return v
	case cleanerWriter:
		return v.writerAI
	}
	return nil
}

func synthTotal(ais []core.Adapter) int {
	n := 0
	for _, a := range ais {
		if w := writerOf(a); w != nil {
			n += len(w.synthCalls())
		}
	}
	return n
}

func withSynth(s DirectSpec, sy *SynthSpec) DirectSpec {
	s.Synth = sy
	return s
}

func synthOf(t *testing.T, c Comparison) Synthesis {
	t.Helper()
	if c.Synthesis == nil {
		t.Fatalf("no synthesis in %+v", c)
	}
	return *c.Synthesis
}

// ---- tests ----------------------------------------------------------------

func TestSynthDirect(t *testing.T) {
	cl, cx, oc, ais := threeWriters()
	m, _ := newTestManager(t, ais, nil)
	var atCall []string // the comparison's status when the writer was called
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		m.locked(func() {
			for _, c := range m.byID {
				atCall = append(atCall, c.status)
			}
		})
		return okSynth(req), nil
	})
	c := startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"}))
	if s := synthOf(t, c); s.Status != SynthWaiting || s.Agent != "claude" {
		t.Errorf("at start: %+v", s)
	}
	c = waitIdle(t, m, "phone", c.ID)
	s := synthOf(t, c)
	if c.Status != Done || s.Status != SynthDone || s.Agent != "claude" || s.Model != "claude-standard" || s.Attempt != 1 ||
		s.Judged || s.Error != nil || s.InputTokens != 30 || s.OutputTokens != 8 || s.Text != fakecli.SynthLead {
		t.Fatalf("synthesis = %+v", s)
	}
	calls := cl.synthCalls()
	if len(calls) != 1 || len(cx.synthCalls())+len(oc.synthCalls()) != 0 {
		t.Fatalf("synth calls %d", synthTotal(ais))
	}
	// Right after the comparison took its answers, and once.
	if !slices.Equal(atCall, []string{Done}) {
		t.Errorf("comparison status at the call: %v", atCall)
	}
	req := calls[0]
	if req.Model != "claude-standard" || req.Effort != "medium" || req.Folder != m.o.Gate.Folder || req.Refine {
		t.Errorf("request = %+v", req)
	}
	// synth-v5: the question and every answer with its AI's name, nothing
	// of the judging.
	nonce := nonceOf(req.Prompt)
	if !strings.HasPrefix(req.Prompt, "[Asksemble synthesis v5] ") {
		t.Errorf("prompt head:\n%s", req.Prompt)
	}
	for _, want := range []string{
		"\nQUESTION: \"" + testQuestion + "\"\n", `ANSWER: {"ai":"Claude","text":"The number is NUM 1."}`,
		`ANSWER: {"ai":"Codex","text":"The number is NUM 1."}`, `ANSWER: {"ai":"OpenCode","text":"The number is NUM 2."}`,
	} {
		if !strings.Contains(req.Prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, req.Prompt)
		}
	}
	for _, gone := range []string{"DIFFER", "CHECK", "COMPARED", "EARLIER QUESTIONS"} {
		if strings.Contains(req.Prompt, gone) {
			t.Errorf("prompt holds %q:\n%s", gone, req.Prompt)
		}
	}
	if len(nonce) != 16 || strings.Trim(nonce, "0123456789abcdef") != "" {
		t.Errorf("nonce %q", nonce)
	}
	// The comparison's own status was done before the synthesis ended,
	// and an idle one has no busy synthesis.
	if again, _ := m.Synthesize("phone", c.ID, "codex"); synthOf(t, again).Text != s.Text || synthTotal(ais) != 1 {
		t.Errorf("a done synthesis written again: %+v", synthOf(t, again))
	}
}

// Who writes: the AI picked when it was asked and may write, else the
// first asked AI in turn order that may; an AI that was not asked never
// writes.
func TestSynthWriterPick(t *testing.T) {
	type tc struct {
		name   string
		asked  []string
		agent  string
		mutate func(cl, cx, oc, gm *writerAI)
		want   string // "" for no_judge
	}
	for _, c := range []tc{
		{name: "picked", asked: []string{"claude", "codex", "opencode"}, agent: "codex", want: "codex"},
		{name: "none picked", asked: []string{"opencode", "claude", "codex"}, want: "opencode"},
		{name: "not asked", asked: []string{"claude", "codex", "opencode"}, agent: "gemini", want: "claude"},
		{name: "not asked, turn order", asked: []string{"codex", "opencode", "claude"}, agent: "gemini", want: "codex"},
		{name: "picked is unavailable", asked: []string{"claude", "codex", "opencode"}, agent: "claude",
			mutate: func(cl, cx, oc, gm *writerAI) { cl.setAvailable(false) }, want: "codex"},
		{name: "picked tier model of an unknown company", asked: []string{"claude", "codex", "opencode"}, agent: "claude",
			mutate: func(cl, cx, oc, gm *writerAI) { cl.fams = map[string]string{"claude-standard": ""} }, want: "codex"},
		{name: "picked has no tier model", asked: []string{"claude", "codex", "opencode"}, agent: "claude",
			mutate: func(cl, cx, oc, gm *writerAI) { cl.tiers = map[string]core.AskModel{} }, want: "codex"},
		{name: "tiers panic", asked: []string{"claude", "codex", "opencode"}, agent: "claude",
			mutate: func(cl, cx, oc, gm *writerAI) { cl.panicT = true }, want: "codex"},
		{name: "only gemini writes", asked: []string{"claude", "codex", "opencode"}, agent: "gemini",
			mutate: func(cl, cx, oc, gm *writerAI) {
				for _, w := range []*writerAI{cl, cx, oc} {
					w.tiers = map[string]core.AskModel{}
				}
			}, want: ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			cl, cx, oc, ais := threeWriters()
			gm := newWriter("gemini", "google", "Gemini")
			ais = append(ais, gm)
			if c.mutate != nil {
				c.mutate(cl, cx, oc, gm)
			}
			fr := &fakeRuns{}
			m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
			var parts []runs.Part
			for _, a := range c.asked {
				parts = append(parts, map[string]runs.Part{
					"claude": part("claude", "The number is NUM 1.", "claude-m"), "codex": part("codex", "The number is NUM 1.", "codex-m"),
					"opencode": part("opencode", "The number is NUM 2.", "oc-m"),
				}[a])
			}
			fr.set(endedRun("run-1", parts...))
			rs := runSpec("phone", "run-1")
			rs.Asked, rs.Judges = c.asked, c.asked
			rs.Synth = &SynthSpec{Agent: c.agent}
			id, err := m.Attach(rs)
			if err != nil {
				t.Fatal(err)
			}
			s := synthOf(t, waitIdle(t, m, "phone", id))
			if len(gm.synthCalls()) != 0 {
				t.Errorf("gemini, not asked, wrote")
			}
			if c.want == "" {
				if s.Status != SynthFailed || s.Error == nil || s.Error.Code != core.ErrCodeNoJudge || synthTotal(ais) != 0 || s.Agent != c.agent {
					t.Errorf("synthesis = %+v", s)
				}
				return
			}
			w := writerOf(map[string]core.Adapter{"claude": cl, "codex": cx, "opencode": oc}[c.want])
			if s.Status != SynthDone || s.Agent != c.want || len(w.synthCalls()) != 1 || synthTotal(ais) != 1 {
				t.Errorf("synthesis = %+v, want %s", s, c.want)
			}
		})
	}
}

// In a direct comparison an AI that wrote a PC answer or is among the
// judges may write; with no judges named, any AI that writes.
func TestSynthWriterDirect(t *testing.T) {
	for _, c := range []struct {
		name, agent string
		judges      []string
		want        string
	}{
		{"judge may write", "codex", []string{"codex"}, "codex"},
		{"not asked falls back to the first pc answer", "gemini", []string{"codex"}, "claude"},
		{"no judges: any AI", "gemini", nil, "gemini"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cl, cx, oc, ais := threeWriters()
			gm := newWriter("gemini", "google", "Gemini")
			ais = append(ais, gm)
			m, _ := newTestManager(t, ais, nil)
			s := threeAnswers("phone", "")
			s.Answers = []DirectAnswer{s.Answers[0], s.Answers[2]} // claude and opencode
			s.Judges = c.judges
			s.Synth = &SynthSpec{Agent: c.agent, Tier: core.TierQuick}
			got := synthOf(t, waitIdle(t, m, "phone", startDirect(t, m, s).ID))
			w := map[string]*writerAI{"claude": cl, "codex": cx, "opencode": oc, "gemini": gm}[c.want]
			if got.Status != SynthDone || got.Agent != c.want || len(w.synthCalls()) != 1 || got.Model != c.want+"-quick" {
				t.Errorf("synthesis = %+v, want %s", got, c.want)
			}
			if req := w.synthCalls()[0]; req.Effort != "low" {
				t.Errorf("effort %q", req.Effort)
			}
		})
	}
}

// The writer's model is its ask tier model: the run's tier, the settings'
// override, an empty effort passed on as it is.
func TestSynthTierModel(t *testing.T) {
	cl, _, _, ais := threeWriters()
	cl.tiers[core.TierDeep] = core.AskModel{Model: "claude-override"}
	fr := &fakeRuns{}
	m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
	run := endedRun("run-d", threeParts()...)
	run.Tier = core.TierDeep
	fr.set(run)
	rs := runSpec("phone", "run-d")
	rs.Synth = &SynthSpec{Agent: "claude"} // the run's tier, from the run
	id, _ := m.Attach(rs)
	s := synthOf(t, waitIdle(t, m, "phone", id))
	if s.Status != SynthDone || s.Model != "claude-override" {
		t.Fatalf("synthesis = %+v", s)
	}
	if req := cl.synthCalls()[0]; req.Model != "claude-override" || req.Effort != "" {
		t.Errorf("request %+v", req)
	}
	// A direct one without a tier writes with standard; a bad tier is
	// refused.
	c := waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID)
	if synthOf(t, c).Model != "claude-standard" {
		t.Errorf("direct: %+v", synthOf(t, c))
	}
	_, err := m.Start(withSynth(threeAnswers("phone", ""), &SynthSpec{Tier: "fast"}))
	var se *SpecError
	if !errors.As(err, &se) || se.Code != CodeBadRequest || se.Message != specSynth {
		t.Errorf("bad tier: %v", err)
	}
	_, err = m.Start(withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "gemini"}))
	if !errors.As(err, &se) || se.Message != specSynth {
		t.Errorf("unknown AI: %v", err)
	}
	// Off with an AI named is off.
	c = waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Off: true, Agent: "codex"})).ID)
	if s := synthOf(t, c); s.Status != SynthOff || s.Agent != "codex" || s.Judged {
		t.Errorf("off: %+v", s)
	}
}

// Fewer than two answers: need_two_answers and no call. A synthesis that
// is off makes no call either, until it is asked for.
func TestSynthNeedTwoAndOff(t *testing.T) {
	_, _, _, ais := threeWriters()
	fr := &fakeRuns{}
	m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
	failedPart := runs.Part{Agent: "codex", Status: runs.Failed, Error: &runs.Error{Code: core.ErrCodeAskFailed}}
	fr.set(endedRun("run-1", part("claude", "The number is NUM 1.", "claude-m"), failedPart))
	rs := runSpec("phone", "run-1")
	rs.Asked, rs.Judges, rs.Synth = []string{"claude", "codex"}, []string{"claude", "codex"}, &SynthSpec{Agent: "claude"}
	id, _ := m.Attach(rs)
	c := waitIdle(t, m, "phone", id)
	if s := synthOf(t, c); c.Status != Failed || s.Status != SynthFailed || s.Error.Code != core.ErrCodeNeedTwoAnswers || synthTotal(ais) != 0 {
		t.Errorf("one answer: %s %+v", c.Status, s)
	}
	// Writing it later cannot help: still need_two_answers.
	if again, err := m.Synthesize("phone", id, ""); err != nil || synthOf(t, again).Error.Code != core.ErrCodeNeedTwoAnswers {
		t.Errorf("Synthesize: %v %+v", err, again.Synthesis)
	}

	c = waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
	if s := synthOf(t, c); s.Status != SynthOff || s.Agent != "" || synthTotal(ais) != 0 {
		t.Fatalf("absent synthesis: %+v", s)
	}
	b, _ := json.Marshal(c)
	if !strings.Contains(string(b), `"synthesis":{"status":"off","judged":false}`) {
		t.Errorf("off JSON: %s", b)
	}
	got, err := m.Synthesize("phone", c.ID, "codex")
	if err != nil || synthOf(t, got).Status != SynthRunning || synthOf(t, got).Agent != "codex" {
		t.Fatalf("Synthesize: %v %+v", err, got.Synthesis)
	}
	c = waitIdle(t, m, "phone", c.ID)
	if s := synthOf(t, c); s.Status != SynthDone || s.Agent != "codex" || s.Judged || len(writerOf(ais[1]).synthCalls()) != 1 {
		t.Errorf("written later: %+v", s)
	}
}

// Bad output is asked for once more with a new nonce; a second bad one
// fails with judge_bad_output.
func TestSynthBadOutputRetry(t *testing.T) {
	for _, c := range []struct {
		name    string
		replies []func(req core.SynthRequest) (core.SynthResult, error)
		status  string
		code    string
		calls   int
	}{
		{"prose then ok", []func(core.SynthRequest) (core.SynthResult, error){
			func(req core.SynthRequest) (core.SynthResult, error) {
				return core.SynthResult{Output: []byte(fakecli.SynthProseText), Model: req.Model}, nil
			},
			func(req core.SynthRequest) (core.SynthResult, error) { return okSynth(req), nil },
		}, SynthDone, "", 2},
		{"adapter bad output then ok", []func(core.SynthRequest) (core.SynthResult, error){
			func(req core.SynthRequest) (core.SynthResult, error) {
				return core.SynthResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeBadOutput, "")
			},
			func(req core.SynthRequest) (core.SynthResult, error) { return okSynth(req), nil },
		}, SynthDone, "", 2},
		{"empty twice", []func(core.SynthRequest) (core.SynthResult, error){
			func(req core.SynthRequest) (core.SynthResult, error) {
				return core.SynthResult{Output: []byte(fakecli.SynthEmptyText), Model: req.Model}, nil
			},
			func(req core.SynthRequest) (core.SynthResult, error) {
				return core.SynthResult{Output: []byte(fakecli.SynthBadJSONText), Model: req.Model}, nil
			},
		}, SynthFailed, core.ErrCodeJudgeBadOutput, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			cl, _, _, ais := threeWriters()
			cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
				return c.replies[n-1](req)
			})
			m, _ := newTestManager(t, ais, nil)
			s := synthOf(t, waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID))
			code := ""
			if s.Error != nil {
				code = s.Error.Code
			}
			calls := cl.synthCalls()
			if s.Status != c.status || code != c.code || len(calls) != c.calls || s.Attempt != c.calls {
				t.Fatalf("synthesis %+v, %d calls", s, len(calls))
			}
			if n1, n2 := nonceOf(calls[0].Prompt), nonceOf(calls[1].Prompt); n1 == "" || n1 == n2 {
				t.Errorf("nonces %q %q", n1, n2)
			}
			if s.InputTokens != 30*boolInt(c.status == SynthDone) || (c.status == SynthFailed && s.Text != "") {
				t.Errorf("tokens or text: %+v", s)
			}
		})
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Every other error ends the synthesis at once, with the writer's words
// only for judge_failed and never with the synth JSON.
func TestSynthErrors(t *testing.T) {
	long := strings.Repeat("한도 ", 400)
	for _, c := range []struct {
		name string
		err  error
		code string
		msg  string
	}{
		{"usage limit", core.Fail(core.ErrCodeJudgeFailed, fakecli.UsageLimitClaude), core.ErrCodeJudgeFailed, fakecli.UsageLimitClaude},
		{"words with the synth JSON", core.Fail(core.ErrCodeJudgeFailed, `oops {"answer":"SECRET-TEXT"}`), core.ErrCodeJudgeFailed, synthMessages[core.ErrCodeJudgeFailed]},
		{"long words", core.Fail(core.ErrCodeJudgeFailed, long), core.ErrCodeJudgeFailed, ""},
		{"tool", core.Fail(core.ErrCodeJudgeUsedTool, ""), core.ErrCodeJudgeUsedTool, synthMessages[core.ErrCodeJudgeUsedTool]},
		{"bad model", core.Fail(core.ErrCodeBadModel, `C:\Users\x\model`), core.ErrCodeBadModel, synthMessages[core.ErrCodeBadModel]},
		{"gone", core.Fail(core.ErrCodeAgentUnavailable, "claude was not found"), core.ErrCodeAgentUnavailable, synthMessages[core.ErrCodeAgentUnavailable]},
		{"timeout", core.Fail(core.ErrCodeJudgeTimeout, ""), core.ErrCodeJudgeTimeout, synthMessages[core.ErrCodeJudgeTimeout]},
		{"internal", core.Fail(core.ErrCodeInternal, "schema changed at C:\\x"), core.ErrCodeInternal, synthMessages[core.ErrCodeInternal]},
		{"not an ActionError", errors.New("boom"), core.ErrCodeInternal, synthMessages[core.ErrCodeInternal]},
	} {
		t.Run(c.name, func(t *testing.T) {
			cl, _, _, ais := threeWriters()
			cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
				return core.SynthResult{Model: req.Model, Output: []byte(`{"answer":"x"}`)}, c.err
			})
			m, _ := newTestManager(t, ais, nil)
			s := synthOf(t, waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID))
			if s.Status != SynthFailed || s.Error == nil || s.Error.Code != c.code || len(cl.synthCalls()) != 1 || s.Text != "" {
				t.Fatalf("synthesis %+v, %d calls", s, len(cl.synthCalls()))
			}
			if c.msg != "" && s.Error.Message != c.msg {
				t.Errorf("message %q, want %q", s.Error.Message, c.msg)
			}
			if c.name == "long words" && len([]rune(s.Error.Message)) != core.JudgeMessageChars {
				t.Errorf("long message has %d characters", len([]rune(s.Error.Message)))
			}
		})
	}
}

// S2 decision 3 for writers: a model of another company than planned is
// bad_model, its output thrown away and nothing asked again.
func TestSynthCompany(t *testing.T) {
	for _, model := range []string{"deepseek-x", ""} {
		cl, _, _, ais := threeWriters()
		cl.fams = map[string]string{"deepseek-x": "deepseek"}
		cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
			res := okSynth(req)
			res.Model = model
			return res, nil
		})
		m, _ := newTestManager(t, ais, nil)
		s := synthOf(t, waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID))
		if s.Status != SynthFailed || s.Error.Code != core.ErrCodeBadModel || s.Error.Message != msgSynthCompany || len(cl.synthCalls()) != 1 || s.Text != "" {
			t.Errorf("model %q: %+v", model, s)
		}
	}
}

// A writer that panics, or whose name panics, ends internal; one that
// hangs ends judge_timeout after the limit and abandonAfter.
func TestSynthPanicAndLimit(t *testing.T) {
	cl, _, _, ais := threeWriters()
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) { panic("writer bug") })
	m, _ := newTestManager(t, ais, nil)
	s := synthOf(t, waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID))
	if s.Status != SynthFailed || s.Error.Code != core.ErrCodeInternal {
		t.Errorf("panic: %+v", s)
	}

	cl, _, _, ais = threeWriters()
	cl.panicN = true
	m, _ = newTestManager(t, ais, nil)
	s = synthOf(t, waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "codex"})).ID))
	if s.Status != SynthFailed || s.Error.Code != core.ErrCodeInternal || synthTotal(ais) != 0 {
		t.Errorf("name panic: %+v", s)
	}

	old := abandonAfter
	abandonAfter = 50 * time.Millisecond
	t.Cleanup(func() { abandonAfter = old })
	cl, _, _, ais = threeWriters()
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		<-stuck // ignores its context
		return core.SynthResult{}, nil
	})
	m, _ = newTestManager(t, ais, func(o *Options) { o.SynthLimit = 100 * time.Millisecond })
	start := time.Now()
	s = synthOf(t, waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID))
	if s.Status != SynthFailed || s.Error.Code != core.ErrCodeJudgeTimeout || time.Since(start) > 5*time.Second {
		t.Errorf("stuck: %+v after %s", s, time.Since(start))
	}
	// One that ends with its context is judge_timeout too.
	cl, _, _, ais = threeWriters()
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		<-ctx.Done()
		return core.SynthResult{}, ctx.Err()
	})
	m, _ = newTestManager(t, ais, func(o *Options) { o.SynthLimit = 100 * time.Millisecond })
	s = synthOf(t, waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID))
	if s.Status != SynthFailed || s.Error.Code != core.ErrCodeJudgeTimeout || len(cl.synthCalls()) != 1 {
		t.Errorf("limit: %+v", s)
	}
}

// A writer waits for a process slot of its AI, at most the queue limit.
func TestSynthSlots(t *testing.T) {
	cl, _, _, ais := threeWriters()
	hold := make(chan struct{})
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		if n == 1 {
			<-hold
		}
		return okSynth(req), nil
	})
	m, _ := newTestManager(t, ais, func(o *Options) { o.Gate.Settings.PerAI = 1; o.QueueLimit = 300 * time.Millisecond })
	// Comparison A: claude's one slot is held by a writer that hangs.
	a := startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"}))
	waitFor(t, "claude to write A", func() bool { return len(cl.synthCalls()) == 1 })
	// B: claude writes too, and waits for its slot past the limit.
	b := waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID)
	sy := synthOf(t, b)
	if b.Status != Done || sy.Status != SynthFailed || sy.Error.Code != core.ErrCodeJudgeTimeout || sy.Error.Message != msgSynthQueue || sy.Attempt != 0 || len(cl.synthCalls()) != 1 {
		t.Errorf("queued past the limit: %+v", sy)
	}
	close(hold)
	if s := synthOf(t, waitIdle(t, m, "phone", a.ID)); s.Status != SynthDone {
		t.Errorf("A: %+v", s)
	}
}

// The writers of every AI together run at most MaxProcesses at once, and
// each AI at most judge.perAI.
func TestSynthSemaphores(t *testing.T) {
	for _, perAI := range []int{2, 1} {
		t.Run(fmt.Sprint("perAI ", perAI), func(t *testing.T) {
			var mu sync.Mutex
			running, peak := map[string]int{}, map[string]int{}
			all, allPeak := 0, 0
			want := min(MaxProcesses, 4*perAI)
			var ais []core.Adapter
			var ws []*writerAI
			for i, fam := range []string{"anthropic", "openai", "deepseek", "google"} {
				w := newWriter(fmt.Sprintf("ai%d", i+1), fam, fmt.Sprintf("AI %d", i+1))
				w.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
					mu.Lock()
					running[w.name]++
					all++
					peak[w.name] = max(peak[w.name], running[w.name])
					allPeak = max(allPeak, all)
					mu.Unlock()
					// Hold the slot until every slot that can be taken is.
					deadline := time.Now().Add(300 * time.Millisecond)
					for time.Now().Before(deadline) {
						mu.Lock()
						full := all >= want
						mu.Unlock()
						if full {
							break
						}
						time.Sleep(time.Millisecond)
					}
					time.Sleep(5 * time.Millisecond)
					mu.Lock()
					running[w.name]--
					all--
					mu.Unlock()
					return okSynth(req), nil
				})
				ais, ws = append(ais, w), append(ws, w)
			}
			m, _ := newTestManager(t, ais, func(o *Options) { o.Gate.Settings.PerAI = perAI })
			var ids []string
			for k := range 2 {
				for _, w := range ws {
					s := DirectSpec{Owner: "phone", Question: testQuestion, Synth: &SynthSpec{Agent: w.name}}
					for i, a := range ais {
						s.Answers = append(s.Answers, DirectAnswer{ID: a.Agent(), Agent: a.Agent(), Via: judge.ViaPC, Model: "m", Text: fmt.Sprintf("Value NUM %d.", i+k)})
					}
					ids = append(ids, startDirect(t, m, s).ID)
				}
			}
			for _, id := range ids {
				if s := synthOf(t, waitIdle(t, m, "phone", id)); s.Status != SynthDone {
					t.Fatalf("synthesis %+v", s)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			for _, w := range ws {
				if p := peak[w.name]; p > perAI || p < 1 {
					t.Errorf("%s wrote %d at once, want at most %d", w.name, p, perAI)
				}
				if n := len(w.synthCalls()); n != 2 {
					t.Errorf("%s wrote %d times, want 2", w.name, n)
				}
			}
			if allPeak != want {
				t.Errorf("%d writers ran at once, want %d", allPeak, want)
			}
		})
	}
}

// Cancel: a waiting synthesis fails at once, a queued one leaves the
// queue, a running one finishes without a second attempt; an ended
// comparison's busy synthesis is cancelled too.
func TestSynthCancel(t *testing.T) {
	t.Run("waiting", func(t *testing.T) {
		// While the round takes the answers.
		_, cx, oc, _ := threeWriters()
		pb := &planBlock{fakeAI: newFake("claude", "anthropic"), entered: make(chan struct{}), release: make(chan struct{})}
		ais := []core.Adapter{pb, cx, cleanerWriter{oc}}
		m, _ := newTestManager(t, ais, nil)
		c := startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "codex"}))
		select {
		case <-pb.entered:
		case <-time.After(10 * time.Second):
			close(pb.release)
			t.Fatal("the round did not take the answers")
		}
		got, err := m.Cancel("phone", c.ID)
		if s := synthOf(t, got); err != nil || s.Status != SynthFailed || s.Error.Code != core.ErrCodeCancelled {
			t.Fatalf("cancel: %v %+v", err, s)
		}
		close(pb.release)
		c = waitIdle(t, m, "phone", c.ID)
		if s := synthOf(t, c); s.Status != SynthFailed || s.Error.Code != core.ErrCodeCancelled || synthTotal(ais) != 0 {
			t.Errorf("after the round: %+v", s)
		}
	})
	t.Run("waiting for its run", func(t *testing.T) {
		_, _, _, ais := threeWriters()
		fr := &fakeRuns{}
		m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
		fr.set(runs.Run{ID: "run-w", Status: runs.Running})
		rs := runSpec("phone", "run-w")
		rs.Synth = &SynthSpec{}
		id, _ := m.Attach(rs)
		got, _ := m.Cancel("phone", id)
		if s := synthOf(t, got); got.Status != Failed || s.Status != SynthFailed || s.Error.Code != core.ErrCodeCancelled {
			t.Errorf("cancel: %s %+v", got.Status, s)
		}
	})
	t.Run("queued", func(t *testing.T) {
		cl, _, _, ais := threeWriters()
		hold := make(chan struct{})
		cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
			if n == 1 {
				<-hold
			}
			return okSynth(req), nil
		})
		m, _ := newTestManager(t, ais, func(o *Options) { o.Gate.Settings.PerAI = 1 })
		a := startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"}))
		waitFor(t, "claude to write A", func() bool { return len(cl.synthCalls()) == 1 })
		b := startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"}))
		waitFor(t, "B's synthesis to queue", func() bool {
			got, _ := m.Get("phone", b.ID)
			return got.Status == Done && synthOf(t, got).Status == SynthRunning
		})
		time.Sleep(20 * time.Millisecond)
		got, err := m.Cancel("phone", b.ID)
		if err != nil || got.Status != Done {
			t.Fatalf("cancel: %v %s", err, got.Status)
		}
		got = waitIdle(t, m, "phone", b.ID)
		if sy := synthOf(t, got); sy.Status != SynthFailed || sy.Error.Code != core.ErrCodeCancelled || len(cl.synthCalls()) != 1 {
			t.Errorf("queued: %+v", sy)
		}
		close(hold)
		waitIdle(t, m, "phone", a.ID)
	})
	t.Run("running", func(t *testing.T) {
		cl, _, _, ais := threeWriters()
		entered, release := make(chan struct{}), make(chan struct{})
		cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
			close(entered)
			<-release
			return core.SynthResult{Output: []byte(fakecli.SynthProseText), Model: req.Model}, nil
		})
		m, _ := newTestManager(t, ais, nil)
		c := startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"}))
		<-entered
		got, err := m.Cancel("phone", c.ID)
		if err != nil || got.Status != Done || synthOf(t, got).Status != SynthRunning {
			t.Fatalf("cancel: %v %s %+v", err, got.Status, got.Synthesis)
		}
		close(release)
		s := synthOf(t, waitIdle(t, m, "phone", c.ID))
		if s.Status != SynthFailed || s.Error.Code != core.ErrCodeJudgeBadOutput || s.Attempt != 1 || len(cl.synthCalls()) != 1 {
			t.Errorf("running: %+v", s)
		}
		// Cancel of an idle comparison changes nothing.
		again, _ := m.Cancel("phone", c.ID)
		if synthOf(t, again).Error.Code != core.ErrCodeJudgeBadOutput {
			t.Errorf("cancel again: %+v", again.Synthesis)
		}
		// A later explicit write is not stopped by that cancel.
		if _, err := m.Synthesize("phone", c.ID, ""); err != nil {
			t.Fatal(err)
		}
		cl.setSynth(nil)
		if s := synthOf(t, waitIdle(t, m, "phone", c.ID)); s.Status != SynthDone {
			t.Errorf("written after the cancel: %+v", s)
		}
	})
}

// Close: a running writer is ended and the synthesis is cancelled; its
// conversation is still cleaned up (R-30), with a context Close has not
// ended.
func TestSynthClose(t *testing.T) {
	_, _, oc, ais := threeWriters()
	entered := make(chan struct{})
	oc.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		close(entered)
		<-ctx.Done()
		return core.SynthResult{Model: req.Model, Cleanup: "opencode-synth-1"}, core.Fail(core.ErrCodeJudgeTimeout, "")
	})
	var live atomic.Int32 // cleanups whose context still ran
	oc.cleanup = func(ctx context.Context, folder string, refs []string) error {
		if ctx.Err() == nil {
			live.Add(1)
		}
		return nil
	}
	m, _ := newTestManager(t, ais, nil)
	c := startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "opencode"}))
	<-entered
	m.Close()
	got, _ := m.Get("phone", c.ID)
	if s := synthOf(t, got); s.Status != SynthFailed || s.Error.Code != core.ErrCodeCancelled || s.Error.Message != msgSynthClosed {
		t.Errorf("after Close: %+v", s)
	}
	waitFor(t, "the writer's cleanup", func() bool { return live.Load() == 1 })
	if calls := (cleanerAI{oc.fakeAI}).cleanupCalls(); len(calls) != 1 || !slices.Equal(calls[0], []string{"opencode-synth-1"}) {
		t.Errorf("cleanups = %v", calls)
	}
	// After Close nothing starts.
	after, err := m.Start(withSynth(threeAnswers("phone", ""), &SynthSpec{}))
	if err != nil || synthOf(t, after).Status != SynthFailed || synthOf(t, after).Error.Code != core.ErrCodeCancelled {
		t.Errorf("Start after Close: %v %+v", err, after.Synthesis)
	}
}

// The writer's conversation is cleaned up after the synthesis, in the
// judge folder and within (2+refs)×core.JudgeCleanupTimeout, and WaitIdle
// waits for it; a failure is logged with its code only.
func TestSynthCleanup(t *testing.T) {
	_, _, oc, ais := threeWriters()
	gate := make(chan struct{})
	var deadline time.Duration
	var folder string
	oc.cleanup = func(ctx context.Context, f string, refs []string) error {
		<-gate
		d, _ := ctx.Deadline()
		deadline, folder = time.Until(d), f
		return core.Fail(core.ErrCodeJudgeFailed, "opencode could not delete opencode-synth-1")
	}
	m, buf := newTestManager(t, ais, nil)
	c := startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "opencode"}))
	waitFor(t, "the synthesis", func() bool {
		got, _ := m.Get("phone", c.ID)
		return synthOf(t, got).Status == SynthDone
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, ok := m.WaitIdle(ctx, "phone", c.ID); !ok || ctx.Err() == nil {
		t.Error("idle while the cleanup runs")
	}
	close(gate)
	waitIdle(t, m, "phone", c.ID)
	calls := (cleanerAI{oc.fakeAI}).cleanupCalls()
	if len(calls) != 1 || !slices.Equal(calls[0], []string{"opencode-synth-1"}) {
		t.Errorf("cleanups = %v", calls)
	}
	if folder != m.o.Gate.Folder || deadline <= 2*core.JudgeCleanupTimeout || deadline > 3*core.JudgeCleanupTimeout {
		t.Errorf("cleanup in %q with %v left, want (2+1)×%v", folder, deadline, core.JudgeCleanupTimeout)
	}
	logs := buf.String()
	if !strings.Contains(logs, `"msg":"writer cleanup failed","writer":"opencode","code":"judge_failed"`) || strings.Contains(logs, "opencode-synth-1") {
		t.Errorf("logs: %s", logs)
	}
	// A writer that leaves nothing gets no cleanup.
	_, _, oc2, ais2 := threeWriters()
	m2, _ := newTestManager(t, ais2, nil)
	waitIdle(t, m2, "phone", startDirect(t, m2, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID)
	if n := len((cleanerAI{oc2.fakeAI}).cleanupCalls()); n != 0 {
		t.Errorf("cleanups with claude writing: %d", n)
	}
}

// Synthesize: 409 while the comparison waits or its synthesis runs, a
// done one unchanged, a failed one written again with the AI asked for.
func TestSynthesizeCall(t *testing.T) {
	cl, cx, _, ais := threeWriters()
	fr := &fakeRuns{}
	m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
	fr.set(runs.Run{ID: "run-s", Status: runs.Running})
	rs := runSpec("phone", "run-s")
	rs.Synth = &SynthSpec{Agent: "claude"}
	id, _ := m.Attach(rs)
	if _, err := m.Synthesize("phone", id, ""); !errors.Is(err, ErrRunning) {
		t.Errorf("while waiting for the run: %v", err)
	}
	entered, done := make(chan struct{}), make(chan struct{})
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		if n == 1 {
			close(entered)
			<-done
			return core.SynthResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeFailed, fakecli.UsageLimitClaude)
		}
		return okSynth(req), nil
	})
	run := endedRun("run-s", threeParts()...)
	fr.set(run)
	m.RunEnded(run)
	<-entered
	if _, err := m.Synthesize("phone", id, ""); !errors.Is(err, ErrRunning) {
		t.Errorf("while writing: %v", err)
	}
	if _, err := m.Retry("phone", id); !errors.Is(err, ErrRunning) {
		t.Errorf("Retry while writing: %v", err)
	}
	close(done)
	c := waitIdle(t, m, "phone", id)
	if s := synthOf(t, c); c.Status != Done || s.Status != SynthFailed || s.Error.Code != core.ErrCodeJudgeFailed || s.Error.Message != fakecli.UsageLimitClaude {
		t.Fatalf("usage limit: %s %+v", c.Status, s)
	}
	if _, err := m.Synthesize("tablet", id, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("another device: %v", err)
	}
	if _, err := m.Synthesize("phone", "nope", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	got, err := m.Synthesize("phone", id, "codex")
	if s := synthOf(t, got); err != nil || s.Status != SynthRunning || s.Error != nil || s.Attempt != 0 || s.Ms != 0 || s.Agent != "codex" {
		t.Fatalf("Synthesize: %v %+v", err, s)
	}
	c = waitIdle(t, m, "phone", id)
	if s := synthOf(t, c); s.Status != SynthDone || s.Agent != "codex" || len(cx.synthCalls()) != 1 || len(cl.synthCalls()) != 1 {
		t.Errorf("written again: %+v", s)
	}
	// Without an AI it keeps the one asked for before.
	cl2, _, _, ais2 := threeWriters()
	cl2.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		if n == 1 {
			return core.SynthResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeTimeout, "")
		}
		return okSynth(req), nil
	})
	m2, _ := newTestManager(t, ais2, nil)
	c2 := waitIdle(t, m2, "phone", startDirect(t, m2, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID)
	if _, err := m2.Synthesize("phone", c2.ID, ""); err != nil {
		t.Fatal(err)
	}
	if s := synthOf(t, waitIdle(t, m2, "phone", c2.ID)); s.Status != SynthDone || s.Agent != "claude" || len(cl2.synthCalls()) != 2 {
		t.Errorf("same AI again: %+v", s)
	}
}

// Retry of a comparison that took its answers leaves its combined answer
// as it is, done, failed or off, and calls no writer: Synthesize writes it
// again. (A comparison that starts over writes it anew: TestSynthRestart.)
func TestSynthRetryKeepsIt(t *testing.T) {
	cl, _, _, ais := threeWriters()
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		if n == 2 {
			return core.SynthResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeFailed, fakecli.UsageLimitClaude)
		}
		return okSynth(req), nil
	})
	m, _ := newTestManager(t, ais, nil)
	done := waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID)
	failed := waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID)
	off := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
	want := []string{SynthDone, SynthFailed, SynthOff}
	for i, c := range []Comparison{done, failed, off} {
		if s := synthOf(t, c); c.Status != Done || s.Status != want[i] {
			t.Fatalf("before: %s %+v, want %s", c.Status, s, want[i])
		}
	}
	for i, c := range []Comparison{done, failed, off} {
		r, err := m.Retry("phone", c.ID)
		if err != nil || r.Status != Done || synthOf(t, r).Status != want[i] {
			t.Errorf("Retry: %v %s %+v, want %s", err, r.Status, r.Synthesis, want[i])
		}
		if s := synthOf(t, waitIdle(t, m, "phone", c.ID)); s.Status != want[i] {
			t.Errorf("after the retry: %+v, want %s", s, want[i])
		}
	}
	if n := len(cl.synthCalls()); n != 2 {
		t.Errorf("%d writer calls, want 2: a Retry wrote", n)
	}
	if s := synthOf(t, failed); s.Error == nil || s.Error.Message != fakecli.UsageLimitClaude || s.Text != "" {
		t.Errorf("failed: %+v", s)
	}
}

// A comparison that starts over (cancelled while it waited for its run)
// writes the combined answer when its round ends.
func TestSynthRestart(t *testing.T) {
	cl, _, _, ais := threeWriters()
	fr := &fakeRuns{}
	m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
	fr.set(runs.Run{ID: "run-r", Status: runs.Running})
	rs := runSpec("phone", "run-r")
	rs.Synth = &SynthSpec{Agent: "claude"}
	id, _ := m.Attach(rs)
	if got, _ := m.Cancel("phone", id); synthOf(t, got).Status != SynthFailed {
		t.Fatalf("cancel: %+v", got.Synthesis)
	}
	fr.set(endedRun("run-r", threeParts()...))
	if _, err := m.Retry("phone", id); err != nil {
		t.Fatal(err)
	}
	if s := synthOf(t, waitIdle(t, m, "phone", id)); s.Status != SynthDone || len(cl.synthCalls()) != 1 {
		t.Errorf("after the restart: %+v", s)
	}
}

// A comparison whose answers could not be taken (an adapter's Family
// panicked) fails internal and its combined answer need_two_answers,
// without a call; Retry takes the answers again and writes it.
func TestSynthBroken(t *testing.T) {
	_, cx, oc, _ := threeWriters()
	pf := &panicFamily{fakeAI: newFake("claude", "anthropic")}
	ais := []core.Adapter{pf, cx, cleanerWriter{oc}}
	m, _ := newTestManager(t, ais, nil)
	c := waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "codex"})).ID)
	if s := synthOf(t, c); c.Status != Failed || c.Error.Code != core.ErrCodeInternal || s.Status != SynthFailed || s.Error.Code != core.ErrCodeNeedTwoAnswers || synthTotal(ais) != 0 {
		t.Fatalf("broken: %s %+v, synthesis %+v", c.Status, c.Error, s)
	}
	pf.fixed.Store(true)
	if _, err := m.Retry("phone", c.ID); err != nil {
		t.Fatal(err)
	}
	c = waitIdle(t, m, "phone", c.ID)
	if s := synthOf(t, c); c.Status != Done || c.Error != nil || len(c.Answers) != 3 || s.Status != SynthDone || len(cx.synthCalls()) != 1 {
		t.Errorf("after the retry: %s %+v, synthesis %+v", c.Status, c.Error, s)
	}
}

// Names: a PC answer by its adapter's DisplayName, a pasted one by its
// app's name.
func TestSynthNames(t *testing.T) {
	cl, _, _, ais := threeWriters()
	m, _ := newTestManager(t, ais, nil)
	s := threeAnswers("phone", "")
	s.Answers[2] = DirectAnswer{ID: "pasted", Agent: "gemini", Via: judge.ViaHandoff, Text: "The number is NUM 2."}
	s.Synth = &SynthSpec{Agent: "claude"}
	waitIdle(t, m, "phone", startDirect(t, m, s).ID)
	p := cl.synthCalls()[0].Prompt
	if !strings.Contains(p, `ANSWER: {"ai":"Claude","text":"The number is NUM 1."}`) || !strings.Contains(p, `ANSWER: {"ai":"Gemini","text":"The number is NUM 2."}`) {
		t.Errorf("prompt:\n%s", p)
	}
}

// The snapshot carries no prompt or nonce, and the text only once done;
// the logs carry codes and counts only.
func TestSynthSnapshotAndLogs(t *testing.T) {
	cl, _, _, ais := threeWriters()
	secret := "SECRET-ANSWER-7781"
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		if n == 1 {
			return core.SynthResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeFailed, "AI-MESSAGE-4410")
		}
		b, _ := json.Marshal(map[string]string{"answer": "TEXT-MARKER-1234 " + fakecli.SynthLead})
		return core.SynthResult{Output: b, Model: req.Model}, nil
	})
	m, buf := newTestManager(t, ais, nil)
	s := threeAnswers("phone", "")
	s.Question = "QUESTION-MARKER-5512 what?"
	s.Answers[0].Text = "It is NUM 1, " + secret + "."
	s.Synth = &SynthSpec{Agent: "claude"}
	c := waitIdle(t, m, "phone", startDirect(t, m, s).ID)
	if _, err := m.Synthesize("phone", c.ID, ""); err != nil {
		t.Fatal(err)
	}
	c = waitIdle(t, m, "phone", c.ID)
	b, _ := json.Marshal(c)
	js := string(b)
	if !strings.Contains(js, `"synthesis":{"status":"done","agent":"claude","model":"claude-standard","attempt":1,`) ||
		!strings.Contains(js, `"text":"TEXT-MARKER-1234 `) || !strings.Contains(js, `"judged":false`) || strings.Contains(js, "unnamedSides") {
		t.Errorf("snapshot JSON: %s", js)
	}
	for _, call := range cl.synthCalls() {
		if strings.Contains(js, nonceOf(call.Prompt)) || strings.Contains(js, "Asksemble synthesis") {
			t.Error("the snapshot holds the prompt or its nonce")
		}
	}
	logs := buf.String()
	leaks := []string{secret, "AI-MESSAGE-4410", "QUESTION-MARKER-5512", "TEXT-MARKER-1234", "NUM 1", logx.Redacted}
	for _, call := range cl.synthCalls() {
		leaks = append(leaks, nonceOf(call.Prompt))
	}
	for _, leak := range leaks {
		if strings.Contains(logs, leak) {
			t.Errorf("logs hold %q:\n%s", leak, logs)
		}
	}
	if strings.Count(logs, `"msg":"synthesis started"`) != 2 || strings.Count(logs, `"msg":"synthesis ended"`) != 2 {
		t.Errorf("logs:\n%s", logs)
	}
	for _, want := range []string{`"writer":"claude"`, `"rules":"synth-v5"`, `"ais":3`, `"code":"judge_failed"`} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %s:\n%s", want, logs)
		}
	}
}

// Without a writer the synthesis fails with no_judge, also when no AI
// writes at all (adapters that pass the gate but cannot write).
func TestSynthNoWriter(t *testing.T) {
	ais := threeAIs()
	m, _ := newTestManager(t, ais, nil)
	c := waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{})).ID)
	if s := synthOf(t, c); s.Status != SynthFailed || s.Error.Code != core.ErrCodeNoJudge || s.Error.Message != synthMessages[core.ErrCodeNoJudge] {
		t.Errorf("no writer: %+v", s)
	}
}

// ---- S5b review (R-35) ------------------------------------------------------

// In a run's comparison the earlier questions reach the writer when it is
// among the AIs the phone listed with them, or, with none listed, among
// the AIs asked (part 20, R-35); no other writer gets them.
func TestSynthEarlierOnlyToAListedWriter(t *testing.T) {
	const earlier = "EARLIER-ONLY-THE-LISTED-GOT"
	for _, c := range []struct {
		writer string
		listed []string
		want   bool
	}{{"claude", []string{"codex"}, false}, {"codex", []string{"codex"}, true}, {"claude", nil, true}} {
		cl, cx, _, ais := threeWriters()
		fr := &fakeRuns{}
		m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
		fr.set(endedRun("run-e", threeParts()...))
		rs := runSpec("phone", "run-e")
		rs.Judges, rs.Earlier, rs.Synth = c.listed, []string{earlier}, &SynthSpec{Agent: c.writer}
		id, _ := m.Attach(rs)
		got := synthOf(t, waitIdle(t, m, "phone", id))
		if got.Status != SynthDone || got.Agent != c.writer {
			t.Fatalf("%s %v: synthesis %+v", c.writer, c.listed, got)
		}
		w := cl
		if c.writer == "codex" {
			w = cx
		}
		p := w.synthCalls()[0].Prompt
		if has := strings.Contains(p, earlier); has != c.want || strings.Contains(p, "EARLIER QUESTIONS") != c.want {
			t.Errorf("%s %v: the prompt holds the earlier question: %v, want %v", c.writer, c.listed, has, c.want)
		}
	}
}

// Contract B: fewer than two answers is need_two_answers, which the phone
// hides, also when the comparison failed internal: a run comparison whose
// run is gone has no answers, and writing again cannot help.
func TestSynthRunGoneIsNeedTwo(t *testing.T) {
	_, _, _, ais := threeWriters()
	fr := &fakeRuns{}
	m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
	fr.set(runs.Run{ID: "other", Status: runs.Running}) // the run the comparison asks for is gone
	rs := runSpec("phone", "gone")
	rs.Synth = &SynthSpec{Agent: "claude"}
	id, err := m.Attach(rs)
	if err != nil {
		t.Fatal(err)
	}
	c := waitIdle(t, m, "phone", id)
	if s := synthOf(t, c); c.Status != Failed || c.Error == nil || c.Error.Code != core.ErrCodeInternal ||
		s.Status != SynthFailed || s.Error == nil || s.Error.Code != core.ErrCodeNeedTwoAnswers || synthTotal(ais) != 0 {
		t.Errorf("comparison %s %+v; synthesis %+v", c.Status, c.Error, s)
	}
	again, err := m.Synthesize("phone", id, "")
	if s := synthOf(t, again); err != nil || s.Error == nil || s.Error.Code != core.ErrCodeNeedTwoAnswers || synthTotal(ais) != 0 {
		t.Errorf("POST …/synthesis again: %v, %+v, want need_two_answers", err, s)
	}
}

// S5b decision 18: the comparison lives KeepFor from the end of its
// combined answer, not from its own end.
func TestSynthMovesFinishedAt(t *testing.T) {
	cl, _, _, ais := threeWriters()
	t0 := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	now := atomic.Int64{}
	now.Store(t0.UnixNano())
	m, _ := newTestManager(t, ais, func(o *Options) { o.Now = func() time.Time { return time.Unix(0, now.Load()) } })
	entered, release := make(chan struct{}), make(chan struct{})
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		close(entered)
		<-release
		return okSynth(req), nil
	})
	c := startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"}))
	<-entered // the comparison ended at t0
	now.Store(t0.Add(29 * time.Minute).UnixNano())
	close(release)
	waitIdle(t, m, "phone", c.ID)
	now.Store(t0.Add(31 * time.Minute).UnixNano())
	if got, ok := m.Get("phone", c.ID); !ok || synthOf(t, got).Status != SynthDone {
		t.Errorf("pruned 2 minutes after its combined answer was written: %v %+v", ok, got.Synthesis)
	}
}

// A direct comparison without judges: when no AI of a PC answer can write,
// any AI that writes does (pickWriter's anyAI fallback).
func TestSynthDirectAnyWriterFallback(t *testing.T) {
	cl, cx, oc, ais := threeWriters()
	cl.tiers = map[string]core.AskModel{} // claude and opencode answered but cannot write
	oc.tiers = map[string]core.AskModel{}
	m, _ := newTestManager(t, ais, nil)
	s := threeAnswers("phone", "")
	s.Answers = []DirectAnswer{s.Answers[0], s.Answers[2]}
	s.Synth = &SynthSpec{}
	got := synthOf(t, waitIdle(t, m, "phone", startDirect(t, m, s).ID))
	if got.Status != SynthDone || got.Agent != "codex" || len(cx.synthCalls()) != 1 {
		t.Errorf("synthesis = %+v", got)
	}
}
