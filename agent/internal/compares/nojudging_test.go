package compares

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/runs"
)

// C6 (#19, 2026-11-05, the user's decision): with NoJudging a comparison
// asks no judge and makes no card; it ends at once and the moderator
// writes the combined answer from the answers alone, with synth-v5.

func noJudging(o *Options) { o.NoJudging = true }

// A run's comparison: no judge call, no request, no card, done; the
// writer is the moderator asked for, its prompt synth-v5 with every answer
// and nothing of a comparison.
func TestNoJudgingRun(t *testing.T) {
	cl, cx, oc, ais := threeWriters()
	fr := &fakeRuns{}
	m, logs := newTestManager(t, ais, func(o *Options) { o.Runs = fr; o.NoJudging = true; o.Topics = true })
	fr.set(endedRun("run-1", threeParts()...))
	rs := runSpec("phone", "run-1")
	rs.Synth = &SynthSpec{Agent: "codex"}
	id, err := m.Attach(rs)
	if err != nil {
		t.Fatal(err)
	}
	c := waitIdle(t, m, "phone", id)
	s := synthOf(t, c)
	if c.Status != Done || c.Error != nil || len(c.Requests) != 0 || len(c.Items) != 0 || len(c.Judgments) != 0 || c.Counts != (Comparison{}).Counts {
		t.Fatalf("comparison %s %+v: requests %d items %d", c.Status, c.Error, len(c.Requests), len(c.Items))
	}
	if s.Status != SynthDone || s.Agent != "codex" || s.Judged || s.Text != fakecli.SynthLead || s.Topics != nil {
		t.Fatalf("synthesis %+v", s)
	}
	if n := judgeTotal(ais); n != 0 {
		t.Errorf("%d judge calls", n)
	}
	if len(cl.synthCalls())+len(oc.synthCalls()) != 0 || len(cx.synthCalls()) != 1 {
		t.Fatalf("synth calls claude %d codex %d opencode %d", len(cl.synthCalls()), len(cx.synthCalls()), len(oc.synthCalls()))
	}
	p := cx.synthCalls()[0].Prompt
	if !strings.HasPrefix(p, "[Asksemble synthesis v5] ") || strings.Contains(p, "COMPARED") || strings.Contains(p, "DIFFER: ") ||
		strings.Count(p, "\nANSWER: ") != 3 || !strings.Contains(p, "NUM 2") {
		t.Errorf("prompt:\n%s", p)
	}
	b, _ := json.Marshal(c)
	for _, want := range []string{`"requests":[]`, `"items":[]`, `"judgments":[]`, `"status":"done"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("JSON lacks %s: %s", want, b)
		}
	}
	if l := logs.String(); !strings.Contains(l, `"rules":"synth-v5"`) || !strings.Contains(l, `"requests":0`) {
		t.Errorf("logs:\n%s", l)
	}
}

// Two AIs asked: they have a combined answer too (#19's first problem),
// written by one of them.
func TestNoJudgingTwoAIs(t *testing.T) {
	cl, cx, _, ais := threeWriters()
	fr := &fakeRuns{}
	m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr; o.NoJudging = true })
	fr.set(endedRun("run-2", part("claude", "The number is NUM 1.", "claude-m"), part("codex", "The number is NUM 2.", "codex-m")))
	rs := runSpec("phone", "run-2")
	rs.Asked, rs.Judges, rs.Synth = []string{"claude", "codex"}, []string{"claude", "codex"}, &SynthSpec{Agent: "claude"}
	id, _ := m.Attach(rs)
	c := waitIdle(t, m, "phone", id)
	if s := synthOf(t, c); c.Status != Done || s.Status != SynthDone || s.Agent != "claude" || judgeTotal(ais) != 0 {
		t.Fatalf("two AIs: %s %+v", c.Status, s)
	}
	if p := cl.synthCalls()[0].Prompt; strings.Count(p, "\nANSWER: ") != 2 || len(cx.synthCalls()) != 0 {
		t.Errorf("prompt:\n%s", p)
	}
}

// One answer: need_two_answers, as with judging, and no call.
func TestNoJudgingNeedTwo(t *testing.T) {
	_, _, _, ais := threeWriters()
	fr := &fakeRuns{}
	m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr; o.NoJudging = true })
	failed := runs.Part{Agent: "codex", Status: runs.Failed, Error: &runs.Error{Code: core.ErrCodeAskFailed}}
	fr.set(endedRun("run-3", part("claude", "The number is NUM 1.", "claude-m"), failed))
	rs := runSpec("phone", "run-3")
	rs.Asked, rs.Judges, rs.Synth = []string{"claude", "codex"}, []string{"claude", "codex"}, &SynthSpec{Agent: "claude"}
	id, _ := m.Attach(rs)
	c := waitIdle(t, m, "phone", id)
	s := synthOf(t, c)
	if c.Status != Failed || c.Error == nil || c.Error.Code != core.ErrCodeNeedTwoAnswers || s.Error == nil || s.Error.Code != core.ErrCodeNeedTwoAnswers {
		t.Fatalf("one answer: %s %+v %+v", c.Status, c.Error, s)
	}
	if synthTotal(ais)+judgeTotal(ais) != 0 {
		t.Errorf("calls: synth %d judge %d", synthTotal(ais), judgeTotal(ais))
	}
}

// The earlier questions reach the writer when it is among the AIs the
// phone listed (as when they judged), and no other.
func TestNoJudgingEarlier(t *testing.T) {
	const earlier = "EARLIER-QUESTION-OF-THE-MODERATOR"
	for _, c := range []struct {
		writer string
		judges []string
		want   bool
	}{{"claude", []string{"claude"}, true}, {"claude", []string{"codex"}, false}} {
		cl, cx, _, ais := threeWriters()
		m, _ := newTestManager(t, ais, noJudging)
		s := threeAnswers("phone", "")
		s.Judges, s.Earlier, s.Synth = c.judges, []string{earlier}, &SynthSpec{Agent: c.writer}
		got := synthOf(t, waitIdle(t, m, "phone", startDirect(t, m, s).ID))
		if got.Status != SynthDone || got.Agent != c.writer || judgeTotal(ais) != 0 || len(cx.synthCalls()) != 0 {
			t.Fatalf("%v: %+v", c.judges, got)
		}
		if p := cl.synthCalls()[0].Prompt; strings.Contains(p, earlier) != c.want {
			t.Errorf("%v: earlier in the prompt %v, want %v", c.judges, !c.want, c.want)
		}
	}
}
