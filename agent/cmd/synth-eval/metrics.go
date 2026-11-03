//go:build syntheval

package main

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Metrics are what a record gives that can be counted without judging the
// writing (#12): times, the combined answer's shape, English mixed into a
// Korean answer, extras left in, and 다른 말 sides it does not name. Whether
// its first sentence really answers is for a person (or later a judge
// model) to say; firstSentence only gives its length.
type Metrics struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Answered int    `json:"answered"` // answers done
	Asked    int    `json:"asked"`
	RefineMs int64  `json:"refineMs"` // the moderator's time before the AIs answered (C2)
	Kind     string `json:"kind,omitempty"`
	Depth    string `json:"depth,omitempty"`
	AskMs    int64  `json:"askMs"` // the slowest answer done, refining taken out
	// CompareMs is from the last answer to the judges' end, SynthMs the
	// writer's own time; TotalMs is refining and the three in a row.
	CompareMs int64 `json:"compareMs"`
	SynthMs   int64 `json:"synthMs"`
	TotalMs   int64 `json:"totalMs"`
	Differ    int   `json:"differ"`
	Check     int   `json:"check"`
	Agree     int   `json:"agree"`
	OnlyOne   int   `json:"onlyOne"`
	// SynthOK is a combined answer written; the rest describe it.
	SynthOK       bool    `json:"synthOk"`
	SynthStatus   string  `json:"synthStatus"`
	Chars         int     `json:"chars"`
	Lines         int     `json:"lines"`
	Table         bool    `json:"table"`
	Headings      int     `json:"headings"`
	Bullets       int     `json:"bullets"`
	FirstSentence int     `json:"firstSentence"` // characters of its first sentence
	StartsHeading bool    `json:"startsWithHeading"`
	Korean        bool    `json:"korean"`     // the question is Korean
	LatinShare    float64 `json:"latinShare"` // of its letters, Latin (Korean questions; AI names, "AI", code and links left out)
	Extras        bool    `json:"extras"`     // the combined answer ends with extras (judge.ExtrasStart)
	AnswerExtras  int     `json:"answerExtras"`
	Unnamed       int     `json:"unnamedSides"`
	Stop          string  `json:"stop,omitempty"`
}

var (
	tableSepRe  = regexp.MustCompile(`^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)*\|?\s*$`)
	headingRe   = regexp.MustCompile(`^#{1,6}\s`)
	bulletRe    = regexp.MustCompile(`^\s*([-*+]|\d+[.)])\s`)
	sentenceEnd = regexp.MustCompile(`[.!?。！？](\s|$)`)
	codeSpanRe  = regexp.MustCompile("(?s)```.*?```|`[^`\n]*`")
	linkRe      = regexp.MustCompile(`https?://\S+|\]\([^)]*\)`)
	namesRe     = regexp.MustCompile(`(?i)\{\{[^{}]*\}\}|\b(claude|codex|opencode|gemini|antigravity|kimi|chatgpt|ai)\b`)
)

func metricsOf(r Record) Metrics {
	m := Metrics{ID: r.Question.ID, Question: r.Question.Text, Asked: len(r.Answers), Stop: r.Stop, Korean: hasHangul(r.Question.Text)}
	for _, a := range r.Answers {
		if a.Status != "done" {
			continue
		}
		m.Answered++
		m.AskMs = max(m.AskMs, a.Ms)
		if judge.ExtrasStart(a.Text) < len(a.Text) {
			m.AnswerExtras++
		}
	}
	if c := r.Compare; c != nil {
		m.CompareMs = c.Ms
		m.Differ, m.Check, m.Agree, m.OnlyOne = c.Counts.Differ, c.Counts.Check, c.Counts.Agree, c.Counts.OnlyOne
	}
	if s := r.Synth; s != nil {
		m.SynthStatus = s.Status
		m.SynthMs = s.Ms
		m.Unnamed = s.Unnamed
		if s.Status == "done" && strings.TrimSpace(s.Text) != "" {
			m.SynthOK = true
			shape(&m, s.Text)
			if m.Korean {
				m.LatinShare = latinShare(s.Text)
			}
			m.Extras = judge.ExtrasStart(s.Text) < len(s.Text)
		}
	}
	if rf := r.Refine; rf != nil {
		m.RefineMs, m.Kind, m.Depth = rf.Ms, rf.Kind, rf.Depth
		// Every AI waits for the spec inside its part, so a part's time
		// holds refining: take it out, to keep AskMs the answering alone.
		if !rf.Before {
			m.AskMs = max(0, m.AskMs-rf.Ms)
		}
	}
	m.TotalMs = m.RefineMs + m.AskMs + m.CompareMs + m.SynthMs
	return m
}

// shape counts the combined answer's lines, tables, headings and bullets,
// and measures its first sentence (headings skipped).
func shape(m *Metrics, text string) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	m.Chars = len([]rune(strings.TrimSpace(text)))
	first := ""
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		m.Lines++
		switch {
		case headingRe.MatchString(t):
			m.Headings++
			if first == "" && m.Lines == 1 {
				m.StartsHeading = true
			}
			continue
		case bulletRe.MatchString(l):
			m.Bullets++
		case tableSepRe.MatchString(t) && strings.Contains(t, "-") && i > 0 && strings.Contains(lines[i-1], "|"):
			m.Table = true
		}
		if first == "" && !strings.HasPrefix(t, "|") {
			first = t
		}
	}
	first = strings.TrimSpace(strings.Trim(first, "*_"))
	if loc := sentenceEnd.FindStringIndex(first); loc != nil {
		first = first[:loc[0]+1]
	}
	m.FirstSentence = len([]rune(first))
}

func hasHangul(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}

// latinShare is the share of Latin letters among Hangul and Latin ones,
// with code, links and the AIs' names taken out first (#5: English mixed
// into a Korean combined answer).
func latinShare(text string) float64 {
	t := codeSpanRe.ReplaceAllString(text, " ")
	t = linkRe.ReplaceAllString(t, " ")
	t = namesRe.ReplaceAllString(t, " ")
	var latin, hangul int
	for _, r := range t {
		switch {
		case unicode.Is(unicode.Hangul, r):
			hangul++
		case r < 0x250 && unicode.IsLetter(r):
			latin++
		}
	}
	if latin+hangul == 0 {
		return 0
	}
	return float64(latin) / float64(latin+hangul)
}
