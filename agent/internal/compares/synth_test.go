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
// which names every DIFFER side of the prompt.
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
	b, _ := json.Marshal(map[string]string{"answer": fakecli.SynthText(req.Prompt)})
	return core.SynthResult{Output: b, Model: req.Model, InputTokens: 30, OutputTokens: 8}
}

func (w *writerAI) synthCalls() []core.SynthRequest {
	w.smu.Lock()
	defer w.smu.Unlock()
	return slices.Clone(w.synths)
}

// cleanerWriter is a writerAI whose judging and writing leave something
// behind, as OpenCode's does.
type cleanerWriter struct{ *writerAI }

func (c cleanerWriter) CleanupJudge(ctx context.Context, folder string, refs []string) error {
	return cleanerAI{c.fakeAI}.CleanupJudge(ctx, folder, refs)
}

// threeWriters are the reference PC's three AIs, writing as well as
// judging; opencode leaves conversations behind.
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

func judgeTotal(ais []core.Adapter) int {
	n := 0
	for _, a := range ais {
		if w := writerOf(a); w != nil {
			n += len(w.judgeCalls())
		} else if _, ok := a.(*plainAI); !ok {
			n += len(fakeOf(a).judgeCalls())
		}
	}
	return n
}

// ---- tests ----------------------------------------------------------------

func TestSynthDirect(t *testing.T) {
	cl, cx, oc, ais := threeWriters()
	m, _ := newTestManager(t, ais, nil)
	var atCall []string // the request statuses when the writer was called
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		m.locked(func() {
			for _, c := range m.byID {
				for _, r := range c.requests {
					atCall = append(atCall, r.Status)
				}
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
		!s.Judged || s.UnnamedSides != 0 || s.Error != nil || s.InputTokens != 30 || s.OutputTokens != 8 {
		t.Fatalf("synthesis = %+v", s)
	}
	if !strings.HasPrefix(s.Text, fakecli.SynthLead) || !strings.Contains(s.Text, "Claude, Codex vs OpenCode") {
		t.Errorf("text = %q", s.Text)
	}
	calls := cl.synthCalls()
	if len(calls) != 1 || len(cx.synthCalls())+len(oc.synthCalls()) != 0 || judgeTotal(ais) != 6 {
		t.Fatalf("synth calls %d, judge calls %d", synthTotal(ais), judgeTotal(ais))
	}
	// After every judge request ended, and once.
	if len(atCall) != 6 || slices.ContainsFunc(atCall, func(st string) bool { return st != judge.RequestDone }) {
		t.Errorf("request statuses at the call: %v", atCall)
	}
	req := calls[0]
	if req.Model != "claude-standard" || req.Effort != "medium" || req.Folder != m.o.Gate.Folder {
		t.Errorf("request = %+v", req)
	}
	nonce := nonceOf(req.Prompt)
	for _, want := range []string{
		"[Asksemble synthesis v4]", `ANSWER: {"ai":"Claude","text":"The number is NUM 1."}`, `ANSWER: {"ai":"OpenCode","text":"The number is NUM 2."}`,
		`DIFFER: {"sides":[{"ais":["Claude","Codex"]`, `{"ais":["OpenCode"],"says":["The number is NUM 2."]}`, "\nCOMPARED.", "QUESTION: \"" + testQuestion + "\"",
	} {
		if !strings.Contains(req.Prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, req.Prompt)
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

// The synthesis waits while the judges run and starts once, right when the
// round ends, without waiting for the judges' cleanup.
func TestSynthWaitsForTheRound(t *testing.T) {
	cl, _, oc, ais := threeWriters()
	release := make(chan struct{})
	cl.setJudge(func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		<-release
		return ruleJudge(req), nil
	})
	cleanupGate := make(chan struct{})
	oc.cleanup = func(ctx context.Context, folder string, refs []string) error {
		<-cleanupGate
		return nil
	}
	m, _ := newTestManager(t, ais, nil)
	c := startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "codex"}))
	waitFor(t, "claude to judge", func() bool { return len(cl.judgeCalls()) == 2 })
	time.Sleep(20 * time.Millisecond)
	if got, _ := m.Get("phone", c.ID); synthOf(t, got).Status != SynthWaiting || synthTotal(ais) != 0 {
		t.Fatalf("while judging: %+v", synthOf(t, got))
	}
	close(release)
	cx := writerOf(ais[1])
	waitFor(t, "the synthesis", func() bool {
		got, _ := m.Get("phone", c.ID)
		return synthOf(t, got).Status == SynthDone
	})
	// Opencode's judge cleanup has not ended yet.
	if n := len(cleanerAI{oc.fakeAI}.cleanupCalls()); n != 1 || len(cx.synthCalls()) != 1 {
		t.Errorf("cleanups %d, synth calls %d", n, len(cx.synthCalls()))
	}
	got, _ := m.Get("phone", c.ID)
	if got.Status != Done {
		t.Errorf("status %s", got.Status)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, ok := m.WaitIdle(ctx, "phone", c.ID); !ok || ctx.Err() == nil {
		t.Error("idle while the cleanup runs")
	}
	close(cleanupGate)
	waitIdle(t, m, "phone", c.ID)
	if synthTotal(ais) != 1 {
		t.Errorf("synth calls %d", synthTotal(ais))
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

// When every judge request failed the combined answer is still written,
// from the answers only.
func TestSynthNotJudged(t *testing.T) {
	_, _, _, ais := threeWriters()
	for _, a := range ais {
		writerOf(a).setJudge(func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
			return core.JudgeResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeFailed, "usage limit")
		})
	}
	m, _ := newTestManager(t, ais, nil)
	c := waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "opencode"})).ID)
	s := synthOf(t, c)
	if c.Status != Failed || s.Status != SynthDone || s.Judged || s.UnnamedSides != 0 || s.Text != fakecli.SynthLead {
		t.Fatalf("status %s, synthesis %+v", c.Status, s)
	}
	p := writerOf(ais[2]).synthCalls()[0].Prompt
	if !strings.Contains(p, "\nNOT COMPARED.") || strings.Contains(p, "DIFFER: ") {
		t.Errorf("prompt:\n%s", p)
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
	if s := synthOf(t, c); s.Status != SynthDone || s.Agent != "codex" || !s.Judged || len(writerOf(ais[1]).synthCalls()) != 1 {
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

// The writer takes the judges' process slots and waits at most the queue
// limit for them.
func TestSynthSlots(t *testing.T) {
	cl, _, _, ais := threeWriters()
	hold := make(chan struct{})
	cl.setJudge(func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		<-hold
		return ruleJudge(req), nil
	})
	m, _ := newTestManager(t, ais, func(o *Options) { o.Gate.Settings.PerAI = 1; o.QueueLimit = 300 * time.Millisecond })
	// Comparison A: claude's one slot is held by a judge that hangs.
	a := startDirect(t, m, threeAnswers("phone", ""))
	waitFor(t, "claude to judge A", func() bool { return len(cl.judgeCalls()) == 1 })
	// B: claude and codex answered and opencode judges; claude writes.
	s := threeAnswers("phone", "")
	s.Answers, s.Judges = s.Answers[:2], []string{"opencode"}
	s.Synth = &SynthSpec{Agent: "claude"}
	b := waitIdle(t, m, "phone", startDirect(t, m, s).ID)
	sy := synthOf(t, b)
	if sy.Status != SynthFailed || sy.Error.Code != core.ErrCodeJudgeTimeout || sy.Error.Message != msgSynthQueue || len(cl.synthCalls()) != 0 {
		t.Errorf("queued past the limit: %+v", sy)
	}
	close(hold)
	waitIdle(t, m, "phone", a.ID)
}

// Cancel: a waiting synthesis fails at once, a queued one leaves the
// queue, a running one finishes without a second attempt; an ended
// comparison's busy synthesis is cancelled too.
func TestSynthCancel(t *testing.T) {
	t.Run("waiting", func(t *testing.T) {
		cl, _, _, ais := threeWriters()
		release := make(chan struct{})
		cl.setJudge(func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
			<-release
			return ruleJudge(req), nil
		})
		m, _ := newTestManager(t, ais, nil)
		c := startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "codex"}))
		waitFor(t, "claude to judge", func() bool { return len(cl.judgeCalls()) == 2 })
		got, err := m.Cancel("phone", c.ID)
		if s := synthOf(t, got); err != nil || s.Status != SynthFailed || s.Error.Code != core.ErrCodeCancelled {
			t.Fatalf("cancel: %v %+v", err, s)
		}
		close(release)
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
		cl.setJudge(func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
			<-hold
			return ruleJudge(req), nil
		})
		m, _ := newTestManager(t, ais, func(o *Options) { o.Gate.Settings.PerAI = 1 })
		a := startDirect(t, m, threeAnswers("phone", ""))
		waitFor(t, "claude to judge A", func() bool { return len(cl.judgeCalls()) == 1 })
		s := threeAnswers("phone", "")
		s.Answers, s.Judges = s.Answers[:2], []string{"opencode"}
		s.Synth = &SynthSpec{Agent: "claude"}
		b := startDirect(t, m, s)
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
		if sy := synthOf(t, got); sy.Status != SynthFailed || sy.Error.Code != core.ErrCodeCancelled || len(cl.synthCalls()) != 0 {
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
// conversation is still cleaned up (R-30).
func TestSynthClose(t *testing.T) {
	_, _, oc, ais := threeWriters()
	entered := make(chan struct{})
	oc.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		close(entered)
		<-ctx.Done()
		return core.SynthResult{Model: req.Model, Cleanup: "opencode-synth-1"}, core.Fail(core.ErrCodeJudgeTimeout, "")
	})
	m, _ := newTestManager(t, ais, nil)
	c := startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "opencode"}))
	<-entered
	m.Close()
	got, _ := m.Get("phone", c.ID)
	if s := synthOf(t, got); s.Status != SynthFailed || s.Error.Code != core.ErrCodeCancelled || s.Error.Message != msgSynthClosed {
		t.Errorf("after Close: %+v", s)
	}
	waitFor(t, "the writer's cleanup", func() bool {
		for _, refs := range (cleanerAI{oc.fakeAI}).cleanupCalls() {
			if slices.Contains(refs, "opencode-synth-1") {
				return true
			}
		}
		return false
	})
	// After Close nothing starts.
	after, err := m.Start(withSynth(threeAnswers("phone", ""), &SynthSpec{}))
	if err != nil || synthOf(t, after).Status != SynthFailed || synthOf(t, after).Error.Code != core.ErrCodeCancelled {
		t.Errorf("Start after Close: %v %+v", err, after.Synthesis)
	}
}

// The writer's conversation is cleaned up after the synthesis, apart from
// the judges' cleanup, and WaitIdle waits for it.
func TestSynthCleanup(t *testing.T) {
	_, _, oc, ais := threeWriters()
	m, _ := newTestManager(t, ais, nil)
	c := waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "opencode"})).ID)
	if synthOf(t, c).Status != SynthDone {
		t.Fatalf("synthesis %+v", synthOf(t, c))
	}
	calls := (cleanerAI{oc.fakeAI}).cleanupCalls()
	if len(calls) != 2 || !slices.ContainsFunc(calls, func(refs []string) bool { return slices.Equal(refs, []string{"opencode-synth-1"}) }) {
		t.Errorf("cleanups = %v", calls)
	}
	// A writer that leaves nothing gets no cleanup of its own.
	_, _, oc2, ais2 := threeWriters()
	m2, _ := newTestManager(t, ais2, nil)
	waitIdle(t, m2, "phone", startDirect(t, m2, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID)
	if n := len((cleanerAI{oc2.fakeAI}).cleanupCalls()); n != 1 {
		t.Errorf("cleanups with claude writing: %d", n)
	}
}

// Synthesize: 409 while the comparison or its synthesis runs, a done one
// unchanged, a failed one written again with the AI asked for.
func TestSynthesizeCall(t *testing.T) {
	cl, cx, _, ais := threeWriters()
	release := make(chan struct{})
	cl.setJudge(func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		<-release
		return ruleJudge(req), nil
	})
	m, _ := newTestManager(t, ais, nil)
	c := startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"}))
	if _, err := m.Synthesize("phone", c.ID, ""); !errors.Is(err, ErrRunning) {
		t.Errorf("while judging: %v", err)
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
	close(release)
	<-entered
	if _, err := m.Synthesize("phone", c.ID, ""); !errors.Is(err, ErrRunning) {
		t.Errorf("while writing: %v", err)
	}
	if _, err := m.Retry("phone", c.ID); !errors.Is(err, ErrRunning) {
		t.Errorf("Retry while writing: %v", err)
	}
	close(done)
	c = waitIdle(t, m, "phone", c.ID)
	if s := synthOf(t, c); s.Status != SynthFailed || s.Error.Code != core.ErrCodeJudgeFailed || s.Error.Message != fakecli.UsageLimitClaude {
		t.Fatalf("usage limit: %+v", s)
	}
	if _, err := m.Synthesize("tablet", c.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("another device: %v", err)
	}
	if _, err := m.Synthesize("phone", "nope", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	got, err := m.Synthesize("phone", c.ID, "codex")
	if s := synthOf(t, got); err != nil || s.Status != SynthRunning || s.Error != nil || s.Attempt != 0 || s.Ms != 0 || s.Agent != "codex" {
		t.Fatalf("Synthesize: %v %+v", err, s)
	}
	c = waitIdle(t, m, "phone", c.ID)
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

// Retry: a round that runs judge requests again writes the combined
// answer again when it ends; nothing to retry leaves it; off stays off.
func TestSynthRetryResets(t *testing.T) {
	cl, cx, _, ais := threeWriters()
	limited := atomic.Bool{}
	limited.Store(true)
	cx.setJudge(func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		if limited.Load() {
			return core.JudgeResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeFailed, fakecli.UsageLimitCodex)
		}
		return ruleJudge(req), nil
	})
	m, _ := newTestManager(t, ais, nil)
	c := waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID)
	if s := synthOf(t, c); c.Status != Done || s.Status != SynthDone || len(cl.synthCalls()) != 1 {
		t.Fatalf("first: %s %+v", c.Status, s)
	}
	limited.Store(false)
	r, err := m.Retry("phone", c.ID)
	if err != nil || synthOf(t, r).Status != SynthWaiting || synthOf(t, r).Text != "" {
		t.Fatalf("Retry: %v %+v", err, r.Synthesis)
	}
	c = waitIdle(t, m, "phone", c.ID)
	if s := synthOf(t, c); s.Status != SynthDone || len(cl.synthCalls()) != 2 {
		t.Errorf("after the retry: %+v, %d calls", s, len(cl.synthCalls()))
	}
	// Nothing to retry: unchanged, no call.
	if r, err := m.Retry("phone", c.ID); err != nil || synthOf(t, r).Status != SynthDone || len(cl.synthCalls()) != 2 {
		t.Errorf("nothing to retry: %v %+v", err, r.Synthesis)
	}

	// Off stays off through a retry.
	limited.Store(true)
	c = waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
	limited.Store(false)
	if _, err := m.Retry("phone", c.ID); err != nil {
		t.Fatal(err)
	}
	if s := synthOf(t, waitIdle(t, m, "phone", c.ID)); s.Status != SynthOff || len(cl.synthCalls()) != 2 {
		t.Errorf("off: %+v", s)
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

// A broken comparison fails its synthesis with internal; the retry that
// makes the cards again writes it.
func TestSynthBroken(t *testing.T) {
	cl, _, _, ais := threeWriters()
	m, _ := newTestManager(t, ais, nil)
	var broken atomic.Bool
	broken.Store(true)
	m.aggregate = func(in judge.AggregateInput) judge.Summary {
		if broken.Load() && in.Ended {
			panic("aggregate bug")
		}
		return judge.Aggregate(in)
	}
	c := waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID)
	if s := synthOf(t, c); c.Status != Failed || s.Status != SynthFailed || s.Error.Code != core.ErrCodeInternal || len(cl.synthCalls()) != 0 {
		t.Fatalf("broken: %s %+v", c.Status, s)
	}
	broken.Store(false)
	if _, err := m.Retry("phone", c.ID); err != nil {
		t.Fatal(err)
	}
	if s := synthOf(t, waitIdle(t, m, "phone", c.ID)); s.Status != SynthDone || len(cl.synthCalls()) != 1 {
		t.Errorf("after the retry: %+v", s)
	}
}

// Names: a PC answer by its adapter's DisplayName, a pasted one by its
// app's name, and a 확인 필요 item's AI likewise.
func TestSynthNames(t *testing.T) {
	cl, _, _, ais := threeWriters()
	m, _ := newTestManager(t, ais, nil)
	s := threeAnswers("phone", "")
	s.Answers[2] = DirectAnswer{ID: "pasted", Agent: "gemini", Via: judge.ViaHandoff, Text: "The number is NUM 2."}
	s.Synth = &SynthSpec{Agent: "claude"}
	waitIdle(t, m, "phone", startDirect(t, m, s).ID)
	p := cl.synthCalls()[0].Prompt
	if !strings.Contains(p, `ANSWER: {"ai":"Gemini","text":"The number is NUM 2."}`) || !strings.Contains(p, `{"ais":["Gemini"]`) {
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
		b, _ := json.Marshal(map[string]string{"answer": "TEXT-MARKER-1234 " + fakecli.SynthText(req.Prompt)})
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
		!strings.Contains(js, `"text":"TEXT-MARKER-1234 `) || !strings.Contains(js, `"judged":true`) {
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
	for _, want := range []string{`"writer":"claude"`, `"judged":true`, `"ais":3`, `"items":1`, `"leftOut":0`, `"code":"judge_failed"`, `"unnamedSides":0`} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %s:\n%s", want, logs)
		}
	}
}

// Without a writer the synthesis fails with no_judge, also when no AI
// writes at all (adapters that only judge).
func TestSynthNoWriter(t *testing.T) {
	ais := threeAIs()
	m, _ := newTestManager(t, ais, nil)
	c := waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{})).ID)
	if s := synthOf(t, c); s.Status != SynthFailed || s.Error.Code != core.ErrCodeNoJudge || s.Error.Message != synthMessages[core.ErrCodeNoJudge] {
		t.Errorf("no writer: %+v", s)
	}
}

// ---- S5b review (R-35) ------------------------------------------------------

// The earlier questions are the ones every judge already got (part 20:
// earlier as for judging): a writer that is not a planned judge gets none,
// a writer that judged gets them.
func TestSynthEarlierOnlyToAJudgeWriter(t *testing.T) {
	const earlier = "EARLIER-ONLY-THE-JUDGES-GOT"
	for _, c := range []struct {
		writer string
		want   bool
	}{{"claude", false}, {"codex", true}} {
		cl, cx, oc, ais := threeWriters()
		m, _ := newTestManager(t, ais, nil)
		s := threeAnswers("phone", "")
		s.Judges = []string{"codex"}
		s.Earlier = []string{earlier}
		s.Synth = &SynthSpec{Agent: c.writer}
		got := synthOf(t, waitIdle(t, m, "phone", startDirect(t, m, s).ID))
		if got.Status != SynthDone || got.Agent != c.writer || len(cl.judgeCalls())+len(oc.judgeCalls()) != 0 || len(cx.judgeCalls()) == 0 {
			t.Fatalf("%s: synthesis %+v, judge calls claude %d codex %d opencode %d", c.writer, got,
				len(cl.judgeCalls()), len(cx.judgeCalls()), len(oc.judgeCalls()))
		}
		for _, j := range cx.judgeCalls() {
			if !strings.Contains(j.Prompt, earlier) {
				t.Errorf("%s: the judge codex lacks the earlier question", c.writer)
			}
		}
		w := cl
		if c.writer == "codex" {
			w = cx
		}
		p := w.synthCalls()[0].Prompt
		if has := strings.Contains(p, earlier); has != c.want || strings.Contains(p, "EARLIER QUESTIONS") != c.want {
			t.Errorf("%s: the combined-answer prompt holds the earlier question: %v, want %v", c.writer, has, c.want)
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

// A Retry whose failed requests can no longer run (their judge left)
// spawns nothing, so the combined answer stays as it was: no second call.
func TestSynthRetryWithoutSpawnKeepsSynth(t *testing.T) {
	cl, cx, _, ais := threeWriters()
	cx.setJudge(func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		return core.JudgeResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeFailed, fakecli.UsageLimitCodex)
	})
	m, _ := newTestManager(t, ais, nil)
	c := waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID)
	if s := synthOf(t, c); c.Status != Done || s.Status != SynthDone || len(cl.synthCalls()) != 1 {
		t.Fatalf("first: %s %+v", c.Status, s)
	}
	cx.setAvailable(false) // codex's failed requests now fail at once: agent_unavailable
	if _, err := m.Retry("phone", c.ID); err != nil {
		t.Fatal(err)
	}
	c = waitIdle(t, m, "phone", c.ID)
	if s := synthOf(t, c); s.Status != SynthDone || len(cl.synthCalls()) != 1 {
		t.Errorf("a retry that judged nothing wrote again: %+v, %d calls", s, len(cl.synthCalls()))
	}
}

// S5b decision 18: the comparison lives KeepFor from the end of its
// combined answer, not from the end of its judges.
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
	<-entered // the judges ended at t0
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
