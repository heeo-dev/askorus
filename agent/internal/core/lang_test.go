package core

import (
	"strings"
	"testing"
)

// #39: the prompts name the question's language when its letters make it plain.
func TestQuestionLanguage(t *testing.T) {
	for q, want := range map[string]string{
		"Which season is best for a first trip to Japan? Pick one season, answer in two sentences.": "English",
		"Recommend exactly one science fiction novel for a first time reader and explain why":       "English",
		"최근 AI모델 랭킹 알려줘":                                  "Korean",
		"Claude Code랑 Codex 중 뭐가 나아?":                     "Korean",
		"日本で初めて旅行するならどの季節がいい？":                            "Japanese",
		"第一次去日本旅行哪个季节最好？":                                 "Chinese",
		"¿Cuál es la mejor estación para viajar a Japón?": "", // Spanish: letters past ASCII
		"Wie geht es dir": "", // German in plain letters, no English word
		"2+2":             "",
		"":                "",
		"GPT-5 vs Claude": "English", // 'vs'
	} {
		if got := QuestionLanguage(q); got != want {
			t.Errorf("QuestionLanguage(%q) = %q, want %q", q, got, want)
		}
	}
}

// #39: the ask prompt names the language at the start and once more after the
// question; with no plain language it keeps the general wording.
func TestAskPromptNamesTheLanguage(t *testing.T) {
	en := AskPrompt("What is the boiling point of water?")
	if !strings.Contains(en, "Write the answer in English, the language of the question below") || !strings.HasSuffix(en, "What is the boiling point of water?\n\n(Write the answer in English.)") {
		t.Errorf("English ask prompt:\n%s", en)
	}
	ko := AskPrompt("물의 끓는점은?")
	if !strings.Contains(ko, "Write the answer in Korean, the language of the question below") || !strings.HasSuffix(ko, "물의 끓는점은?\n\n(Write the answer in Korean.)") {
		t.Errorf("Korean ask prompt:\n%s", ko)
	}
	if got := AskPrompt("2+2"); got != AskPreamble+"\n\n2+2" {
		t.Errorf("unknown language: %q", got)
	}
	// A whole prompt (a debate turn) is named by its question, not by its data.
	turn := AskPromptIn("English", "[Askorus debate-turn v2] ... YOUR ANSWER: 한국어 답 ...")
	if !strings.Contains(turn, "in English, the language of the question") || strings.Contains(turn, "Korean") {
		t.Errorf("turn prompt:\n%s", turn)
	}
}
