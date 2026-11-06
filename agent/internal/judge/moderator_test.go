package judge

import (
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// The moderator ask (#25): a golden file pins the prompt.

var moderatorUpdate = flag.Bool("moderator.update", false, "rewrite testdata/moderator-ask-v1.golden")

const moderatorGoldenFile = "testdata/moderator-ask-v1.golden"

func moderatorAnswersFixture() []ModeratorAnswer {
	return []ModeratorAnswer{{AI: "Claude", Text: "첫 배는 7시입니다."}, {AI: "Codex", Text: "First ferry at 8 a.m."}}
}

func TestModeratorAskGolden(t *testing.T) {
	got := moderatorAskPrompt("AI들이 뭐가 달랐지?", conversationFixture(), moderatorAnswersFixture(), debateNonceA)
	if *moderatorUpdate {
		if err := os.WriteFile(moderatorGoldenFile, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(moderatorGoldenFile)
	if err != nil {
		t.Fatalf("%v (run with -args -moderator.update to write it)", err)
	}
	if want := strings.ReplaceAll(string(raw), "\r\n", "\n"); got != want {
		t.Errorf("the prompt differs from %s; if the change is meant, run with -args -moderator.update and review the diff.\ngot:\n%s", moderatorGoldenFile, got)
	}
}

func TestModeratorAskPrompt(t *testing.T) {
	answers := moderatorAnswersFixture()
	answers[1].Text = strings.Repeat("가", MaxModeratorAnswerBytes) // 3 bytes each
	p := ModeratorAskPrompt("AI들이 뭐가 달랐지?", conversationFixture(), answers)
	if !strings.HasPrefix(p, core.AskPreamble) || !strings.Contains(p, "[Asksemble moderator-ask v1]") || !strings.HasSuffix(p, "The new question:\nAI들이 뭐가 달랐지?") {
		t.Fatalf("prompt:\n%.400s", p)
	}
	// One data block: the record (with the earlier questions) and the answers.
	for _, line := range []string{"EARLIER QUESTIONS (context only, do not answer them): ", "SETTLED: ", "PREVIOUS QUESTION: ", "PREVIOUS ANSWER: ", `ANSWER: {"ai":"Claude","text":"첫 배는 7시입니다."}`} {
		if strings.Count(p, line) != 1 {
			t.Errorf("%q appears %d times", line, strings.Count(p, line))
		}
	}
	if strings.Count(p, "\n<data-") != 1 || strings.Count(p, "\n</data-") != 1 {
		t.Error("not one data block")
	}
	// Each answer is cut to its limit, at a character; the caller's answers stay as they were.
	if !strings.Contains(p, strings.Repeat("가", MaxModeratorAnswerBytes/3)+`"}`) || strings.Contains(p, strings.Repeat("가", MaxModeratorAnswerBytes/3+1)) {
		t.Error("the long answer is not cut at 8 KiB")
	}
	if len(answers[1].Text) != 3*MaxModeratorAnswerBytes {
		t.Error("the caller's answer was cut in place")
	}
	// Nothing to give: the head and the question only.
	if p := ModeratorAskPrompt("q", nil, nil); strings.Contains(p, "\n<data-") || !strings.HasSuffix(p, "The new question:\nq") {
		t.Errorf("empty: %q", p)
	}
}
