package core

import (
	"strings"
	"unicode"
)

// QuestionLanguage names the language a question is written in when its
// letters make it plain, so prompts can say "in English" instead of "in
// the language of the question" (#39: Claude now and then answered an
// English question in Korean, with Korean in its context). Hangul makes
// it Korean, kana Japanese, Han without either Chinese, and only ASCII
// letters with at least one common English word English; anything else is
// "" and the prompts keep the general wording.
func QuestionLanguage(q string) string {
	var hangul, kana, han, other bool
	for _, r := range q {
		switch {
		case unicode.Is(unicode.Hangul, r):
			hangul = true
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			kana = true
		case unicode.Is(unicode.Han, r):
			han = true
		case unicode.IsLetter(r) && r > unicode.MaxASCII:
			other = true
		}
	}
	switch {
	case hangul:
		return "Korean"
	case kana:
		return "Japanese"
	case han:
		return "Chinese"
	case other:
		return ""
	}
	for _, w := range strings.FieldsFunc(strings.ToLower(q), func(r rune) bool { return !unicode.IsLetter(r) && r != '\'' }) {
		if englishWords[w] {
			return "English"
		}
	}
	return ""
}

// englishWords are words common in English questions and rare as whole
// words in other languages written in plain ASCII letters.
var englishWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`the a an is are was were be what which who whom whose how why when where should would could can do does did
		i i'm my me you your we our it its this that these those there for to of in on with and or but not best better than
		tell give explain recommend compare help please about between vs`) {
		englishWords[w] = true
	}
}
