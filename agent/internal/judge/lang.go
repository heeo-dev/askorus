package judge

import (
	"strings"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// The question's language in the moderator's and the debaters' prompts
// (#39, 11-09): an English question now and then got Korean from Claude,
// in a debater's turn, in the plan's topics and questions and in a point's
// outcome line, with Korean answers or Korean words in the prompt. When
// the question's letters make its language plain (core.QuestionLanguage)
// the prompts name it, before the data and in the task; the Korean wording
// rule (다르다, never 갈리다) is only in prompts for a Korean question or one
// whose language is not plain.

// theQuestionsLanguage is the phrase the prompts were written with.
const theQuestionsLanguage = "in the language of the QUESTION"

// inLanguage is theQuestionsLanguage with the language named when it is
// plain: "in English, the language of the QUESTION".
func inLanguage(question string) string {
	if l := core.QuestionLanguage(question); l != "" {
		return "in " + l + ", the language of the QUESTION"
	}
	return theQuestionsLanguage
}

// namedLanguage is s with each theQuestionsLanguage naming the language.
func namedLanguage(s, question string) string {
	return strings.ReplaceAll(s, theQuestionsLanguage, inLanguage(question))
}

// languageLine is a line before the data that names the question's
// language; "" when it is not plain.
func languageLine(question string) string {
	if l := core.QuestionLanguage(question); l != "" {
		return "Write in " + l + ": the QUESTION is in " + l + ", whatever language the answers and the rest of the data are in.\n"
	}
	return ""
}

// koreanWording reports whether a prompt for question carries the Korean
// wording rule: for a Korean question, or one whose language is not plain.
func koreanWording(question string) bool {
	l := core.QuestionLanguage(question)
	return l == "" || l == "Korean"
}
