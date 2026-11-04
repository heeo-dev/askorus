package scenario

// IT-17 (docs/testing.md): the topics of the 다른 말 items (C5, #11)
// through the whole agent: ask → compare → the combined answer, and beside
// it the same AI names the items at the quick tier.

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/compares"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// IT-17: with Claude writing, one topics call by Claude's quick model
// (haiku) with the synth command; its stdin is one nonce block of the
// question and an ITEM line per 다른 말 item, without the answers; the
// snapshot's synthesis.topics is done with the item's key, a topic and a
// gist per side; the log holds no topic.
func TestIT17Topics(t *testing.T) {
	r := newRig(t, nil)
	r.addDevice()
	_, c, snap := r.synthRun(map[string]any{"synthesis": map[string]any{"agent": "claude"}})
	s := synthOf(t, c)
	if s.Status != compares.SynthDone || s.Topics == nil {
		t.Fatalf("synthesis %+v", s)
	}
	tp := *s.Topics
	if tp.Status != compares.TopicsDone || tp.Agent != fakecli.RoleClaude || tp.Model != "claude-haiku-fake" || tp.Attempt != 1 || tp.Error != nil {
		t.Errorf("topics %+v", tp)
	}
	var keys []string
	for _, it := range c.Items {
		if it.Kind == judge.CardDiffer {
			keys = append(keys, it.Key)
		}
	}
	if len(keys) != 1 || len(tp.Items) != 1 || tp.Items[0].Key != keys[0] || tp.Items[0].Topic != "Fake topic 1" ||
		!slices.Equal(tp.Items[0].Gists, []string{"gist 1", "gist 2"}) {
		t.Errorf("topic items %+v for keys %v", tp.Items, keys)
	}

	topics := calls(t, 0, kindTopics, "")
	if len(topics) != 1 || topics[0].Role != fakecli.RoleClaude || len(calls(t, 0, kindSynth, "")) != 1 {
		t.Fatalf("%d topics calls, %d synth calls", len(topics), len(calls(t, 0, kindSynth, "")))
	}
	tc := topics[0]
	wantArgs := []string{"-p", "--output-format", "json", "--model", "haiku", "--effort", "medium",
		"--tools", "", "--strict-mcp-config", "--no-session-persistence", "--safe-mode", "--json-schema", core.SynthSchemaV1}
	if !slices.Equal(tc.Args, wantArgs) {
		t.Errorf("claude topics args %q", tc.Args)
	}
	nonce, opens, closes := nonceOf(tc.Stdin)
	if len(nonce) != 16 || opens != 1 || closes != 1 {
		t.Fatalf("topics stdin: nonce %q, %d opens, %d closes", nonce, opens, closes)
	}
	data := dataLines(tc.Stdin, nonce)
	q, _ := json.Marshal(question)
	if len(data) != 2 || data[0] != "QUESTION: "+string(q) || !strings.HasPrefix(data[1], "ITEM 1: ") || !json.Valid([]byte(strings.TrimPrefix(data[1], "ITEM 1: "))) {
		t.Errorf("topics data lines %q", data)
	}
	for _, a := range answers {
		if strings.Contains(tc.Stdin, a) {
			t.Error("the topics prompt holds a whole answer")
		}
	}
	logs := r.logs.String()
	if !strings.Contains(logs, "topics started") || !strings.Contains(logs, "topics ended") || strings.Contains(logs, "Fake topic") || strings.Contains(logs, nonce) {
		t.Error("the log lacks the topics lines or holds their content")
	}
	if strings.Contains(string(snap), nonce) {
		t.Error("the snapshot holds the topics nonce")
	}
	r.waitIdle(c.ID)
}

// IT-17: a topics reply that does not fit twice fails the topics only; the
// combined answer is done. With topics off there is no call and no field.
func TestIT17TopicsFailAndOff(t *testing.T) {
	t.Run("bad twice", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) { o.env = map[string]string{fakecli.EnvTopics: fakecli.SynthProse} })
		r.addDevice()
		_, c, _ := r.synthRun(map[string]any{"synthesis": map[string]any{"agent": "claude"}})
		s := synthOf(t, c)
		if s.Status != compares.SynthDone || s.Topics == nil || s.Topics.Status != compares.TopicsFailed || s.Topics.Attempt != 2 ||
			s.Topics.Error == nil || s.Topics.Error.Code != core.ErrCodeJudgeBadOutput || s.Topics.Items != nil {
			t.Errorf("synthesis %+v, topics %+v", s, s.Topics)
		}
		if n := len(calls(t, 0, kindTopics, "")); n != 2 {
			t.Errorf("%d topics calls", n)
		}
		r.waitIdle(c.ID)
	})
	t.Run("opencode writes and cleans up", func(t *testing.T) {
		r := newRig(t, nil)
		r.addDevice()
		_, c, _ := r.synthRun(map[string]any{"synthesis": map[string]any{"agent": "opencode"}})
		s := synthOf(t, c)
		if s.Status != compares.SynthDone || s.Topics == nil || s.Topics.Status != compares.TopicsDone || s.Topics.Agent != fakecli.RoleOpenCode {
			t.Errorf("synthesis %+v, topics %+v", s, s.Topics)
		}
		r.waitIdle(c.ID)
		if ss := fakecli.Sessions(t); len(ss) != 1 || ss[0].Title != fakecli.AskTitle {
			t.Errorf("opencode sessions after the topics: %+v", ss)
		}
	})
	t.Run("off", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) { o.noTopics = true })
		r.addDevice()
		_, c, b := r.synthRun(map[string]any{"synthesis": map[string]any{"agent": "claude"}})
		if s := synthOf(t, c); s.Status != compares.SynthDone || s.Topics != nil || strings.Contains(string(b), `"topics"`) {
			t.Errorf("synthesis %+v", s)
		}
		if n := len(calls(t, 0, kindTopics, "")); n != 0 {
			t.Errorf("%d topics calls while off", n)
		}
		r.waitIdle(c.ID)
	})
}
