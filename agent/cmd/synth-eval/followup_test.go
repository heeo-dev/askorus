//go:build syntheval

package main

import (
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/judge"
)

// C11: side B gives the record to the AIs, the combined answer and the
// plan as data; side A gives none; both give the AI its own last turn.
func TestFollowupPrompts(t *testing.T) {
	r := Record{Question: Question{ID: "q", Text: "첫 질문"}, Synth: &SynthRec{Status: "done", Text: "토론 뒤 정리"}, Debate: &DebateRec{Points: []DebatePoint{
		{ID: "p1", Topic: "가격", Debate: true, Result: "agreed", Line: "80~100만 원"},
		{ID: "p2", Topic: "토론 안 함", Debate: false},
		{ID: "p3", Topic: "결과 없음", Debate: true},
	}}}
	s := settledOf(r)
	if len(s) != 1 || s[0].Topic != "가격" || s[0].Line != "80~100만 원" {
		t.Fatalf("settled = %+v", s)
	}
	c := &ContextRec{Earlier: []string{r.Question.Text}, Settled: s, Last: "토론 뒤 정리"}

	a := followupAsk(c, "첫 질문", "내 답", "이어서", false)
	b := followupAsk(c, "첫 질문", "내 답", "이어서", true)
	for _, p := range []string{a, b} {
		if !strings.Contains(p, "YOUR ANSWER: 내 답") || !strings.HasSuffix(p, "The new question:\n이어서") {
			t.Errorf("own turn and question: %q", p)
		}
	}
	if strings.Contains(a, "SETTLED") || !strings.Contains(b, `SETTLED: {"line":"80~100만 원","result":"agreed","topic":"가격"}`) || !strings.Contains(b, `PREVIOUS ANSWER: "토론 뒤 정리"`) {
		t.Errorf("record lines:\nA %q\nB %q", a, b)
	}

	task := judge.SynthTask{Question: "이어서", Earlier: []string{"첫 질문"}, Answers: []judge.SynthAnswer{{AI: "Claude", Text: "x"}, {AI: "Codex", Text: "y"}}}
	n := judge.SynthNonce(task)
	plain := judge.SynthPrompt(task, n)
	with := withConversation(plain, n, c, followupConversation)
	end := "</data-" + n + ">\n"
	if i := strings.Index(with, `SETTLED: {"line"`); i < 0 || i > strings.Index(with, end) || !strings.Contains(with, end+followupConversation) {
		t.Errorf("synth with the record:\n%s", with)
	}
	if withConversation("no block", n, c, followupConversation) != "" {
		t.Error("a prompt without its data block was kept")
	}
}
