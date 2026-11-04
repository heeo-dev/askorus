package compares

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

const topicsHead = "[Asksemble differ topics "

func isTopics(req core.SynthRequest) bool { return strings.HasPrefix(req.Prompt, topicsHead) }

// okWrite answers a topics prompt with fakecli's topics and anything else
// with its combined answer.
func okWrite(req core.SynthRequest) core.SynthResult {
	if !isTopics(req) {
		return okSynth(req)
	}
	b, _ := json.Marshal(map[string]string{"answer": fakecli.TopicsText(req.Prompt)})
	return core.SynthResult{Output: b, Model: req.Model, InputTokens: 12, OutputTokens: 4}
}

func withTopics(o *Options) { o.Topics = true }

func callsOf(w *writerAI, topics bool) []core.SynthRequest {
	var out []core.SynthRequest
	for _, r := range w.synthCalls() {
		if isTopics(r) == topics {
			out = append(out, r)
		}
	}
	return out
}

// The writer of the combined answer names the 다른 말 items at the quick
// tier, in a call of its own beside the combined answer.
func TestTopicsBesideTheSynthesis(t *testing.T) {
	cl, cx, oc, ais := threeWriters()
	m, logs := newTestManager(t, ais, withTopics)
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		return okWrite(req), nil
	})
	c := waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID)
	s := synthOf(t, c)
	if s.Status != SynthDone || s.Topics == nil {
		t.Fatalf("synthesis = %+v", s)
	}
	tp := *s.Topics
	if tp.Status != TopicsDone || tp.Agent != "claude" || tp.Model != "claude-quick" || tp.Attempt != 1 || tp.Error != nil || tp.InputTokens != 12 {
		t.Fatalf("topics = %+v", tp)
	}
	key := ""
	for _, it := range c.Items {
		if it.Kind == judge.CardDiffer {
			key = it.Key
		}
	}
	if want := []judge.DifferTopic{{Key: key, Topic: "Fake topic 1", Gists: []string{"gist 1", "gist 2"}}}; key == "" || !reflect.DeepEqual(tp.Items, want) {
		t.Errorf("items = %+v, want %+v", tp.Items, want)
	}
	topics, synths := callsOf(cl, true), callsOf(cl, false)
	if len(topics) != 1 || len(synths) != 1 || len(cx.synthCalls())+len(oc.synthCalls()) != 0 {
		t.Fatalf("calls: %d topics, %d synth", len(topics), len(synths))
	}
	p := topics[0]
	if p.Model != "claude-quick" || p.Effort != "low" || !strings.Contains(p.Prompt, `ITEM 1: {"sides":[{"ais":["Claude","Codex"],"said":["The number is NUM 1.","The number is NUM 1."]},{"ais":["OpenCode"],"said":["The number is NUM 2."]}]}`) ||
		!strings.Contains(p.Prompt, `QUESTION: "`+testQuestion+`"`) || strings.Contains(p.Prompt, "ANSWER:") {
		t.Errorf("topics prompt:\n%s", p.Prompt)
	}
	out := logs.String()
	if !strings.Contains(out, "topics ended") || !strings.Contains(out, `"named":1`) || strings.Contains(out, "Fake topic") || strings.Contains(out, "NUM 1") {
		t.Errorf("logs:\n%s", out)
	}
}

// Off (the default here), without a 다른 말 item or without judging there
// is no topics call.
func TestTopicsNone(t *testing.T) {
	t.Run("off", func(t *testing.T) {
		cl, _, _, ais := threeWriters()
		m, _ := newTestManager(t, ais, nil)
		s := synthOf(t, waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID))
		if s.Topics != nil || len(callsOf(cl, true)) != 0 {
			t.Errorf("topics while off: %+v", s.Topics)
		}
	})
	t.Run("no differ", func(t *testing.T) {
		cl, _, _, ais := threeWriters()
		m, _ := newTestManager(t, ais, withTopics)
		spec := threeAnswers("phone", "")
		spec.Answers[2].Text = "The number is NUM 1."
		s := synthOf(t, waitIdle(t, m, "phone", startDirect(t, m, withSynth(spec, &SynthSpec{Agent: "claude"})).ID))
		if s.Status != SynthDone || s.Topics != nil || len(callsOf(cl, true)) != 0 {
			t.Errorf("topics without a 다른 말 item: %+v", s.Topics)
		}
	})
}

// A reply that does not name every item is asked once more; twice is a
// failure that leaves the combined answer done.
func TestTopicsBadReply(t *testing.T) {
	for _, c := range []struct {
		name   string
		bad    int // the topics calls that answer badly
		status string
	}{{"once", 1, TopicsDone}, {"twice", 2, TopicsFailed}} {
		t.Run(c.name, func(t *testing.T) {
			cl, _, _, ais := threeWriters()
			m, _ := newTestManager(t, ais, withTopics)
			n := 0
			cl.setSynth(func(ctx context.Context, req core.SynthRequest, _ int) (core.SynthResult, error) {
				if isTopics(req) {
					if n++; n <= c.bad {
						b, _ := json.Marshal(map[string]string{"answer": "Fake topic 1 | gist 1"}) // a gist short
						return core.SynthResult{Output: b, Model: req.Model}, nil
					}
				}
				return okWrite(req), nil
			})
			s := synthOf(t, waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID))
			tp := s.Topics
			if s.Status != SynthDone || tp == nil || tp.Status != c.status || tp.Attempt != min(c.bad+1, 2) || len(callsOf(cl, true)) != min(c.bad+1, 2) {
				t.Fatalf("synthesis %+v, topics %+v", s, tp)
			}
			if c.status == TopicsFailed && (tp.Error == nil || tp.Error.Code != core.ErrCodeJudgeBadOutput || tp.Items != nil || strings.Contains(tp.Error.Message, "gist")) {
				t.Errorf("failed topics: %+v", tp)
			}
			if p := callsOf(cl, true); len(p) == 2 && nonceOf(p[0].Prompt) == nonceOf(p[1].Prompt) {
				t.Error("the second ask kept its nonce")
			}
		})
	}
}

// The topics run beside the combined answer: the comparison stays busy
// until both end, and the combined answer does not wait for them.
func TestTopicsBusyAndBeside(t *testing.T) {
	cl, _, _, ais := threeWriters()
	m, _ := newTestManager(t, ais, withTopics)
	release := make(chan struct{})
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, _ int) (core.SynthResult, error) {
		if isTopics(req) {
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		return okWrite(req), nil
	})
	c := startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"}))
	var got Comparison
	waitFor(t, "the combined answer while the topics run", func() bool {
		got, _ = m.Get("phone", c.ID)
		return got.Synthesis != nil && got.Synthesis.Status == SynthDone
	})
	if tp := got.Synthesis.Topics; tp == nil || tp.Status != TopicsRunning {
		t.Fatalf("topics = %+v", tp)
	}
	if _, err := m.Retry("phone", c.ID); err != ErrRunning {
		t.Errorf("retry while the topics run: %v", err)
	}
	close(release)
	got = waitIdle(t, m, "phone", c.ID)
	if tp := got.Synthesis.Topics; tp.Status != TopicsDone || len(tp.Items) != 1 {
		t.Errorf("topics = %+v", tp)
	}
}

// Close ends topics that are being named: cancelled, as the agent stops.
func TestTopicsClose(t *testing.T) {
	cl, _, _, ais := threeWriters()
	m, _ := newTestManager(t, ais, withTopics)
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, _ int) (core.SynthResult, error) {
		if isTopics(req) {
			<-ctx.Done()
			return core.SynthResult{}, core.Fail(core.ErrCodeCancelled, "")
		}
		return okWrite(req), nil
	})
	c := startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"}))
	waitFor(t, "the topics call", func() bool { return len(callsOf(cl, true)) == 1 })
	m.Close()
	got, ok := m.Get("phone", c.ID)
	if tp := got.Synthesis.Topics; !ok || tp == nil || tp.Status != TopicsFailed || tp.Error.Code != core.ErrCodeCancelled {
		t.Errorf("topics = %+v", tp)
	}
}
