package judge

import (
	"bytes"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// The conversation record (#29): a golden file pins the prompts that carry
// it; without one every prompt is as before.

var conversationUpdate = flag.Bool("conversation.update", false, "rewrite testdata/conversation-v1.golden")

const conversationGoldenFile = "testdata/conversation-v1.golden"

func conversationFixture() *Conversation {
	return &Conversation{
		Earlier:      []string{"첫 배는 몇 시에 떠?"},
		Settled:      []Settled{{Topic: "첫 배 시간", Result: OutcomeAgreed, Line: "첫 배는 8시입니다."}},
		LastQuestion: "첫 배는 몇 시에 떠?",
		Last:         "첫 배는 8시입니다. 표는 터미널에서 삽니다.",
	}
}

func TestConversationPromptsGolden(t *testing.T) {
	task := debateTaskFixture()
	task.Question = "그럼 마지막 배는?"
	task.Conversation = conversationFixture()
	closed := debatePointFixture(NatureFact)
	closed.Result, closed.Line = OutcomeAgreed, "마지막 배는 6시입니다."
	var b bytes.Buffer
	for _, part := range []struct{ name, text string }{
		{"synth-v6", SynthPrompt(task.SynthTask, debateNonceA)},
		{"debate-plan v3", DebatePlanPrompt(task, debateNonceA)},
		{"debate-v5", AfterDebatePrompt(task.SynthTask, []DebatePoint{closed}, debateNonceA)},
	} {
		if part.text == "" {
			t.Fatalf("%s: empty", part.name)
		}
		b.WriteString("==== " + part.name + "\n" + part.text)
	}
	got := b.String()
	if *conversationUpdate {
		if err := os.WriteFile(conversationGoldenFile, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(conversationGoldenFile)
	if err != nil {
		t.Fatalf("%v (run with -args -conversation.update to write it)", err)
	}
	if want := strings.ReplaceAll(string(raw), "\r\n", "\n"); got != want {
		t.Errorf("the prompts differ from %s; if the change is meant, run with -args -conversation.update and review the diff.\ngot:\n%s", conversationGoldenFile, got)
	}
}

func TestConversationPrompts(t *testing.T) {
	c := conversationFixture()
	task := debateTaskFixture()
	task.Question = "그럼 마지막 배는?"
	plain := SynthPrompt(task.SynthTask, debateNonceA)

	// Each prompt names its version, and keeps its rules after the data.
	task.Conversation = c
	synth := SynthPrompt(task.SynthTask, debateNonceA)
	end := "</data-" + debateNonceA + ">\n"
	switch {
	case !strings.HasPrefix(plain, synthLabel) || !strings.HasPrefix(synth, synthLabelV6):
		t.Errorf("labels: %.40q / %.40q", plain, synth)
	case strings.Index(synth, "PREVIOUS ANSWER: ") > strings.Index(synth, end) || !strings.Contains(synth, end+"CONVERSATION. "):
		t.Errorf("synth-v6 layout:\n%s", synth)
	case strings.Count(synth, "EARLIER QUESTIONS") != 1:
		t.Error("a writer without earlier questions of its own did not get the record's")
	case SynthRulesFor(task.SynthTask) != "synth-v6" || SynthRulesFor(SynthTask{}) != SynthRules:
		t.Error("SynthRulesFor")
	}
	// A writer with earlier questions of its own gets no others.
	own := task.SynthTask
	own.Earlier = []string{"배표는 어디서 사?"}
	if p := SynthPrompt(own, debateNonceA); strings.Count(p, "EARLIER QUESTIONS") != 1 || strings.Contains(p, "첫 배는 몇 시에 떠?\"]") {
		t.Errorf("earlier questions twice:\n%s", p)
	}
	// The plan gets the settled points only; without any, it stays v2.
	plan := DebatePlanPrompt(task, debateNonceA)
	if !strings.HasPrefix(plan, planLabelV3) || !strings.Contains(plan, `SETTLED: {"topic":"첫 배 시간"`) || strings.Contains(plan, "PREVIOUS ANSWER") || !strings.Contains(plan, "재검토:") {
		t.Errorf("plan v3:\n%s", plan)
	}
	task.Conversation = &Conversation{Last: "x"}
	if p := DebatePlanPrompt(task, debateNonceA); !strings.HasPrefix(p, planLabel) || strings.Contains(p, "SETTLED") {
		t.Errorf("a plan without settled points: %.60q", p)
	}
	// A nonce in the record makes no prompt.
	task.Conversation = &Conversation{Last: "see " + debateNonceA}
	if SynthPrompt(task.SynthTask, debateNonceA) != "" || AfterDebatePrompt(task.SynthTask, nil, debateNonceA) != "" {
		t.Error("a nonce inside the record was accepted")
	}
}

func TestAskWithConversation(t *testing.T) {
	if got := AskWithConversation("q", nil, true); got != core.AskPrompt("q") {
		t.Errorf("no record: %q", got)
	}
	c := conversationFixture()
	known := AskWithConversation("그럼 마지막 배는?", c, false)
	fresh := AskWithConversation("그럼 마지막 배는?", c, true)
	for _, p := range []string{known, fresh} {
		if !strings.HasPrefix(p, core.AskPreamble) || !strings.HasSuffix(p, "The new question:\n그럼 마지막 배는?") ||
			!strings.Contains(p, `SETTLED: {"topic":"첫 배 시간","result":"agreed","line":"첫 배는 8시입니다."}`) || !strings.Contains(p, `PREVIOUS ANSWER: "첫 배는 8시입니다. 표는 터미널에서 삽니다."`) {
			t.Errorf("ask:\n%s", p)
		}
	}
	// Only an AI new to the conversation gets the earlier questions.
	if strings.Contains(known, "EARLIER QUESTIONS") || !strings.Contains(fresh, `EARLIER QUESTIONS (context only, do not answer them): ["첫 배는 몇 시에 떠?"]`) {
		t.Errorf("earlier questions:\n%s\n---\n%s", known, fresh)
	}
}
