package judge

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Extras (#4). Some AIs end an answer with things that are not part of it:
// a closing section of messages or questions to send next, as a person's
// own global rules may ask for (OpenCode read "다음에 쓸 문장" from the
// user's AGENTS.md although the ask preamble says not to add it, and the
// comparison then counted it as what only one AI says), or a last paragraph
// offering more work ("원하시면 표로 정리해 드릴게요."). ExtrasStart finds
// where they begin, so judging and the combined answer leave them out and
// the phone folds them. The app's src/ask/extras.ts follows the same rules
// on the same cases (testdata/extras.json); change both together.

const (
	extrasHeadingChars = 40  // a longer heading line is a sentence that mentions one
	extrasOfferChars   = 200 // a longer last paragraph is no mere offer
)

var (
	// A section of messages or questions to send next, by its heading's
	// first words. "Next steps" and "다음 단계" are not here: a question can
	// ask for exactly that. (No \b: it is ASCII-only and Korean ends a word
	// with a non-word character.)
	extrasHeading = regexp.MustCompile(`(?i)^(다음에 (쓸|보낼|물어볼) (문장|말|질문)|다음 질문|후속 질문|추천 질문|이어서 (물어볼|할) (만한 )?(질문|말)|더 물어볼 (만한 )?(질문|것)|next (message|prompt)|suggested (follow-?up|question|prompt|next message)|follow-?up question|you (might|could|may) also ask|(questions|things) to ask next|what to ask next)`)
	// A last paragraph that offers more: it opens with a condition and the
	// AI offers to do something.
	extrasOfferOpen = regexp.MustCompile(`(?i)^(원하시면|원하면|원한다면|필요하시면|필요하면|필요하다면|궁금한 (점|게) (있으시면|있으면)|더 궁금하|if you('d| would)? (like|want|need)|let me know if|want me to|would you like me to|shall i|i can also)`)
	extrasOfferVerb = regexp.MustCompile(`(?i)(드릴게요|드릴까요|드릴 수 있|드리겠|해 ?줄게|해 ?줄까|알려 ?줄게|알려 ?드릴|말씀해 ?주세요|말해 ?줘|물어봐 ?주세요|\bi can\b|\bi could\b|\bi'll\b|\bi will\b|\blet me know\b|\bwant me to\b|\bwould you like me to\b|\bshall i\b|\bhappy to\b)`)
	extrasNumber    = regexp.MustCompile(`^\d{1,2}[.)]\s*`)
	extrasListLine  = regexp.MustCompile(`^\s*([-*+>]\s|\d{1,3}[.)]\s)`)
	extrasRule      = regexp.MustCompile(`^ {0,3}([-*_])( *[-*_]){2,} *$`)
	extrasFence     = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
)

// ExtrasStart is the byte offset in text where its trailing extras begin,
// or len(text) when it has none. From the end: a section under a heading
// line (a Markdown heading, a line that is only bold, or a short line
// ending in a colon) whose words name messages or questions to send next,
// with no other heading line after it; then a last paragraph that opens
// with a condition and offers more. Blank lines and a thematic break right
// before them go with them. An answer that would be left with nothing is
// left whole.
func ExtrasStart(text string) int {
	lines := extrasLines(text)
	cut := len(text)
	for i := len(lines) - 1; i >= 0; i-- {
		l := lines[i]
		if l.code || l.blank || extrasListLine.MatchString(l.text) {
			continue
		}
		h, ok := extrasHeadingText(l.text)
		if !ok {
			continue
		}
		if extrasHeading.MatchString(h) {
			cut = l.start
		}
		break
	}

	last := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i].start < cut && !lines[i].blank {
			last = i
			break
		}
	}
	if last >= 0 && !lines[last].code && !extrasRule.MatchString(lines[last].text) {
		first := last
		for first > 0 && !lines[first-1].blank && !lines[first-1].code && !extrasRule.MatchString(lines[first-1].text) {
			first--
		}
		if extrasOffer(text[lines[first].start:lines[last].end]) {
			cut = lines[first].start
		}
	}

	for i := len(lines) - 1; i >= 0; i-- {
		l := lines[i]
		if l.start >= cut {
			continue
		}
		if !l.blank && (l.code || !extrasRule.MatchString(l.text)) {
			break
		}
		cut = l.start
	}
	if cut == len(text) || strings.TrimSpace(text[:cut]) == "" {
		return len(text)
	}
	// The answer ends at its last character, not at a line break.
	return len(strings.TrimRight(text[:cut], " \t\r\n"))
}

type extrasLine struct {
	start, end int // the line without its line break
	text       string
	blank      bool
	code       bool // in a fenced code block, fences included
}

func extrasLines(text string) []extrasLine {
	var out []extrasLine
	fence := ""
	for pos := 0; pos < len(text); {
		end, next := len(text), len(text)
		if i := strings.IndexByte(text[pos:], '\n'); i >= 0 {
			end, next = pos+i, pos+i+1
		}
		raw := strings.TrimSuffix(text[pos:end], "\r")
		l := extrasLine{start: pos, end: pos + len(raw), text: raw, blank: strings.TrimSpace(raw) == ""}
		if fence != "" {
			l.code = true
			if t := strings.TrimSpace(raw); strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
				fence = ""
			}
		} else if m := extrasFence.FindStringSubmatch(raw); m != nil {
			l.code = true
			fence = m[1]
		}
		out = append(out, l)
		pos = next
	}
	return out
}

// extrasHeadingText gives a heading line's words, with its marks, a
// section number and a closing colon taken off.
func extrasHeadingText(line string) (string, bool) {
	t := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(t, "#"):
		t = strings.TrimLeft(t, "#")
		if t != "" && t[0] != ' ' {
			return "", false
		}
	case len(t) > 4 && (strings.HasPrefix(t, "**") && strings.HasSuffix(t, "**") || strings.HasPrefix(t, "__") && strings.HasSuffix(t, "__")):
	case strings.HasSuffix(t, ":") || strings.HasSuffix(t, "："):
	default:
		return "", false
	}
	t = strings.Trim(strings.TrimSpace(t), "*_ ")
	t = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(t, ":"), "："))
	t = strings.Trim(t, "*_ ")
	t = extrasNumber.ReplaceAllString(t, "")
	if t == "" || utf8.RuneCountInString(t) > extrasHeadingChars {
		return "", false
	}
	return t, true
}

func extrasOffer(para string) bool {
	t := strings.Trim(strings.TrimSpace(para), "*_ ")
	return utf8.RuneCountInString(t) <= extrasOfferChars && extrasOfferOpen.MatchString(t) && extrasOfferVerb.MatchString(t)
}
