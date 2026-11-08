package compares

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
	"github.com/heeo-dev/askorus/agent/internal/judge"
)

// specReply is a moderator's good reply, with the model asked for.
func specReply(req core.SynthRequest) core.SynthResult {
	return core.SynthResult{Output: []byte(fakecli.RefineSpec), Model: req.Model}
}

func TestRefineGivesTheSpecAndTemplate(t *testing.T) {
	cl, _, _, ais := threeWriters()
	m, _ := newTestManager(t, ais, nil)
	cl.setSynth(func(_ context.Context, req core.SynthRequest, _ int) (core.SynthResult, error) {
		return specReply(req), nil
	})
	got := m.Refine(context.Background(), RefineSpec{Question: "가격과 높이는?", Earlier: []string{"앞 질문"}, Asked: []string{"claude", "codex"}, Agent: "claude"})
	if got.Status != RefineDone || got.Agent != "claude" || got.Model != "claude-quick" || got.Spec == nil || got.Spec.Kind != "compare" || got.Template != judge.AnswerTemplate(*got.Spec) {
		t.Fatalf("%+v", got)
	}
	calls := cl.synthCalls()
	if len(calls) != 1 || !calls[0].Refine || calls[0].Model != "claude-quick" || calls[0].Effort != "" || !strings.Contains(calls[0].Prompt, `"앞 질문"`) {
		t.Fatalf("calls %+v", calls)
	}
}

func TestRefineAsksAgainOnceThenFails(t *testing.T) {
	cl, _, _, ais := threeWriters()
	m, _ := newTestManager(t, ais, nil)
	cl.setSynth(func(_ context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		if n == 1 {
			return core.SynthResult{Output: []byte("no json"), Model: req.Model}, nil
		}
		return specReply(req), nil
	})
	spec := RefineSpec{Question: "q", Asked: []string{"claude"}}
	if got := m.Refine(context.Background(), spec); got.Status != RefineDone || len(cl.synthCalls()) != 2 {
		t.Fatalf("after one bad reply: %+v", got)
	}
	cl.setSynth(func(_ context.Context, req core.SynthRequest, _ int) (core.SynthResult, error) {
		return core.SynthResult{Output: []byte("no json"), Model: req.Model}, nil
	})
	if got := m.Refine(context.Background(), spec); got.Status != RefineFailed || got.Error == nil || got.Error.Code != core.ErrCodeJudgeBadOutput || got.Spec != nil {
		t.Fatalf("after two: %+v", got)
	}
	// Another company's model is not this AI.
	cl.fams = map[string]string{"gpt-x": "openai"}
	cl.setSynth(func(_ context.Context, req core.SynthRequest, _ int) (core.SynthResult, error) {
		return core.SynthResult{Output: []byte(fakecli.RefineSpec), Model: "gpt-x"}, nil
	})
	if got := m.Refine(context.Background(), spec); got.Status != RefineFailed || got.Error.Code != core.ErrCodeBadModel {
		t.Fatalf("other company: %+v", got)
	}
}

func TestRefineWithinItsLimit(t *testing.T) {
	old := refineLimit
	refineLimit = 80 * time.Millisecond
	t.Cleanup(func() { refineLimit = old })
	cl, _, _, ais := threeWriters()
	m, _ := newTestManager(t, ais, nil)
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, _ int) (core.SynthResult, error) {
		<-ctx.Done()
		return core.SynthResult{Model: req.Model}, ctx.Err()
	})
	start := time.Now()
	got := m.Refine(context.Background(), RefineSpec{Question: "q", Asked: []string{"claude"}})
	if got.Status != RefineFailed || got.Error.Code != core.ErrCodeJudgeTimeout || time.Since(start) > 2*time.Second || len(cl.synthCalls()) != 1 {
		t.Fatalf("%+v after %v, %d calls", got, time.Since(start), len(cl.synthCalls()))
	}
}

func TestRefineModeratorIsAnAskedAI(t *testing.T) {
	cl, cx, _, ais := threeWriters()
	m, _ := newTestManager(t, ais, nil)
	for _, w := range []*writerAI{cl, cx} {
		w.setSynth(func(_ context.Context, req core.SynthRequest, _ int) (core.SynthResult, error) {
			return specReply(req), nil
		})
	}
	// Claude was picked but not asked: the first asked AI moderates.
	if got := m.Refine(context.Background(), RefineSpec{Question: "q", Asked: []string{"codex"}, Agent: "claude"}); got.Agent != "codex" || len(cl.synthCalls()) != 0 {
		t.Fatalf("%+v", got)
	}
	// No asked AI may moderate.
	if got := m.Refine(context.Background(), RefineSpec{Question: "q", Asked: []string{"gemini"}}); got.Status != RefineFailed || got.Error.Code != core.ErrCodeNoJudge {
		t.Fatalf("%+v", got)
	}
}

func TestRunRefinerRefinesOnceForEveryAI(t *testing.T) {
	cl, _, _, ais := threeWriters()
	m, _ := newTestManager(t, ais, nil)
	cl.setSynth(func(_ context.Context, req core.SynthRequest, _ int) (core.SynthResult, error) {
		return specReply(req), nil
	})
	var mu sync.Mutex
	var reports []string
	rr := m.NewRunRefiner(RefineSpec{Question: "가격과 높이는?", Asked: []string{"claude"}}, func(x Refined) {
		mu.Lock()
		reports = append(reports, x.Status)
		mu.Unlock()
	})
	var wg sync.WaitGroup
	qs := make([]string, 3)
	for i := range qs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			qs[i], _ = rr.Question(context.Background())
		}()
	}
	wg.Wait()
	want := judge.RefinedQuestion("가격과 높이는?", judge.QuestionSpec{Kind: "compare", Intent: "상자의 가격과 높이", Depth: "short", Language: "ko"})
	for _, q := range qs {
		if q != want {
			t.Fatalf("question %q", q)
		}
	}
	if len(cl.synthCalls()) != 1 || strings.Join(reports, ",") != "running,done" {
		t.Fatalf("%d calls, reports %v", len(cl.synthCalls()), reports)
	}
	// A failed refining gives the question as written; a wait that ends gives its error.
	cl.setSynth(func(_ context.Context, req core.SynthRequest, _ int) (core.SynthResult, error) {
		return core.SynthResult{}, core.Fail(core.ErrCodeJudgeFailed, "usage limit")
	})
	rr = m.NewRunRefiner(RefineSpec{Question: "q", Asked: []string{"claude"}}, func(Refined) {})
	if q, err := rr.Question(context.Background()); q != "q" || err != nil {
		t.Fatalf("%q %v", q, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	slow := m.NewRunRefiner(RefineSpec{Question: "q", Asked: []string{"claude"}}, func(Refined) {})
	cl.setSynth(func(c context.Context, req core.SynthRequest, _ int) (core.SynthResult, error) {
		time.Sleep(50 * time.Millisecond)
		return specReply(req), nil
	})
	if _, err := slow.Question(ctx); err == nil {
		t.Fatal("an ended wait gave a question")
	}
}
