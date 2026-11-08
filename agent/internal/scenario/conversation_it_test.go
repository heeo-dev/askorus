package scenario

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/api"
	"github.com/heeo-dev/askorus/agent/internal/compares"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
	"github.com/heeo-dev/askorus/agent/internal/judge"
	"github.com/heeo-dev/askorus/agent/internal/runs"
)

// IT-20 (#29, 11-06): the conversation record through the whole agent and
// the three real adapters around the fake CLIs. A question to all three
// with the combined answer, its debate, then a follow-up with the record
// (claude and codex in their sessions, opencode new to the conversation),
// and the follow-up's debate with the record.

const followUp = "그럼 상자 무게는?"

// debateOf is a POST /v1/debates body for a run's answers and its
// combined answer, with ctx when it is not nil.
func debateOf(key, q string, run api.RunStatus, combined string, ctx map[string]any) map[string]any {
	var answers []map[string]string
	for _, p := range run.Parts {
		answers = append(answers, map[string]string{"agent": p.Agent, "via": "pc", "model": p.Model, "text": p.Answer})
	}
	body := map[string]any{"clientDebateId": key, "question": q, "synthesis": combined, "answers": answers, "moderator": fakecli.RoleClaude, "tier": core.TierStandard}
	if ctx != nil {
		body["context"] = ctx
	}
	return body
}

// recordLines checks that a prompt has one nonce data block and gives its
// lines.
func recordLines(t *testing.T, who, prompt string) []string {
	t.Helper()
	nonce, opens, closes := nonceOf(prompt)
	if len(nonce) != 16 || opens != 1 || closes != 1 {
		t.Fatalf("%s: nonce %q, %d opens, %d closes", who, nonce, opens, closes)
	}
	return dataLines(prompt, nonce)
}

func TestIT20ConversationRecord(t *testing.T) {
	r := newRig(t, func(o *rigOptions) { o.env = map[string]string{fakecli.EnvDebate: fakecli.DebateNext} })
	r.addDevice()
	if c := r.info().Context; c == nil || c.MaxSettled != 12 || c.MaxLastBytes != 8192 {
		t.Fatalf("info context = %+v", c)
	}

	// Turn 1 and its debate, as before #29.
	run1, c1, _ := r.synthRun(map[string]any{"synthesis": map[string]string{"agent": fakecli.RoleClaude}})
	s1 := synthOf(t, c1)
	if s1.Status != compares.SynthDone {
		t.Fatalf("turn 1's combined answer: %+v", s1)
	}
	var d1 api.DebateStatus
	r.callJSON("POST", "/v1/debates", debateOf("d1", question, run1, s1.Text, nil), 202, &d1)
	d1, b := r.waitDebate(d1.ID)
	if d1.Status != compares.DebateDone || d1.After == nil || len(d1.Points) != 1 || d1.Points[0].Line == "" {
		t.Fatalf("debate 1: %s", b)
	}
	p := d1.Points[0]
	ctx := map[string]any{
		"earlier": []string{question},
		"settled": []map[string]string{{"topic": p.Topic, "result": p.Result, "line": p.Line}},
		"last":    d1.After.Text,
	}
	settledLine, _ := json.Marshal(judge.Settled{Topic: p.Topic, Result: p.Result, Line: p.Line})
	lastLine, _ := json.Marshal(d1.After.Text)
	earlierLine, _ := json.Marshal([]string{question})

	// Turn 2: a follow-up with the record. Claude and codex go on in their
	// sessions; opencode is new to the conversation.
	from := len(fakecli.Calls(t))
	sessions := map[string]string{}
	for _, part := range run1.Parts {
		sessions[part.Agent] = part.Session
	}
	st := r.ask([]map[string]string{
		{"agent": fakecli.RoleClaude, "session": sessions[fakecli.RoleClaude]},
		{"agent": fakecli.RoleCodex, "session": sessions[fakecli.RoleCodex]},
		{"agent": fakecli.RoleOpenCode},
	}, map[string]any{
		"tier": core.TierStandard, "prompt": followUp, "context": ctx,
		"compare": map[string]any{"earlier": []string{question}, "judges": agents, "synthesis": map[string]string{"agent": fakecli.RoleClaude}},
	})
	run2 := r.waitRun(st.ID)
	if run2.Status != runs.Done {
		t.Fatalf("turn 2: %+v", run2)
	}
	asks := calls(t, from, kindAsk, "")
	if len(asks) != 3 {
		t.Fatalf("%d asks", len(asks))
	}
	for _, c := range asks {
		data := recordLines(t, c.Role, c.Stdin)
		if !strings.HasSuffix(strings.TrimRight(c.Stdin, "\n"), "The new question:\n"+followUp) ||
			!strings.Contains(strings.Join(data, "\n"), "SETTLED: "+string(settledLine)) || !strings.Contains(strings.Join(data, "\n"), "PREVIOUS ANSWER: "+string(lastLine)) {
			t.Errorf("%s's ask: %q", c.Role, c.Stdin)
		}
		earlier := len(linesWith(data, "EARLIER QUESTIONS (context only, do not answer them): "+string(earlierLine)))
		if want := map[bool]int{true: 1, false: 0}[c.Role == fakecli.RoleOpenCode]; earlier != want {
			t.Errorf("%s got the earlier questions %d times", c.Role, earlier)
		}
		for _, a := range c.Args {
			if strings.Contains(a, "상자") || strings.Contains(a, p.Line) {
				t.Errorf("%s: the prompt in an argument: %q", c.Role, c.Args)
			}
		}
	}
	c2, b := r.waitComparison(st.ComparisonID)
	s2 := synthOf(t, c2)
	writes := calls(t, from, kindSynth, "")
	if s2.Status != compares.SynthDone || len(writes) != 1 || !strings.HasPrefix(writes[0].Stdin, "[Askorus synthesis v6]") {
		t.Fatalf("turn 2's combined answer: %s", b)
	}
	data := recordLines(t, "writer", writes[0].Stdin)
	if len(linesWith(data, "SETTLED: ")) != 1 || len(linesWith(data, "PREVIOUS ANSWER: ")) != 1 || len(linesWith(data, "EARLIER QUESTIONS")) != 1 || len(linesWith(data, "ANSWER: ")) != 3 {
		t.Errorf("synth-v6 data: %q", data)
	}

	// Turn 2's debate with the record: debate-plan v3 (the settled point,
	// not the last answer) and debate-v5.
	from = len(fakecli.Calls(t))
	var d2 api.DebateStatus
	r.callJSON("POST", "/v1/debates", debateOf("d2", followUp, run2, s2.Text, ctx), 202, &d2)
	if d2, b = r.waitDebate(d2.ID); d2.Status != compares.DebateDone || d2.After == nil {
		t.Fatalf("debate 2: %s", b)
	}
	var heads []string
	for _, c := range calls(t, from, kindSynth, "") {
		heads = append(heads, strings.SplitN(c.Stdin, "]", 2)[0]+"]")
		switch {
		case strings.HasPrefix(c.Stdin, "[Askorus debate-plan v3]"):
			data := recordLines(t, "plan", c.Stdin)
			if len(linesWith(data, "SETTLED: ")) != 1 || len(linesWith(data, "PREVIOUS")) != 0 || !strings.Contains(c.Stdin, "재검토:") {
				t.Errorf("plan v3: %q", c.Stdin)
			}
		case strings.HasPrefix(c.Stdin, "[Askorus synthesis debate-v5]"):
			data := recordLines(t, "after", c.Stdin)
			if len(linesWith(data, "SETTLED: ")) != 1 || len(linesWith(data, "PREVIOUS ANSWER: ")) != 1 {
				t.Errorf("debate-v5: %q", data)
			}
		}
	}
	if strings.Join(heads, ",") != "[Askorus debate-plan v3],[Askorus debate-check v1],[Askorus debate-check v1],[Askorus synthesis debate-v5]" {
		t.Errorf("moderator calls %v", heads)
	}
	onlyAsksAndWriters(t)

	// The log has counts, never the record's text.
	logs := r.logs.String()
	for _, secret := range []string{"상자", p.Line, fakecli.DebateAfterText, fakecli.SynthLead} {
		if strings.Contains(logs, secret) {
			t.Errorf("the agent log holds %q", secret)
		}
	}
	for _, line := range []string{`"rules":"synth-v6"`, `"rules":"debate-v5"`, `"settled":1`, `"lastBytes":`} {
		if !strings.Contains(logs, line) {
			t.Errorf("the agent log lacks %s", line)
		}
	}
}
