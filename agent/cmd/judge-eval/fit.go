//go:build judgeeval

package main

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Claims and evidence to units (plan part 2, fit). The answers are split
// by judge.SplitAnswer, as the product splits them; a claim maps to one
// unit or a run of consecutive units, compared after normalization (the
// splitNorm of internal/judge's split_test.go) on a unit's Text and on its
// raw span without links, both without the list number an ordered item's
// first unit keeps. Evidence may also sit inside one unit.

// Fit modes.
const (
	fitOne    = "unit"
	fitRun    = "run"
	fitInside = "inside"
	fitNone   = "none"
)

// fitNorm drops white space and the marks |*_`~, as split_test's
// splitNorm does.
func fitNorm(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune("|*_`~", r) {
			return -1
		}
		return r
	}, s)
}

// fitLinkRe is a Markdown link with brackets around it, which claims were
// copied without (split_test's splitLinkRe).
var fitLinkRe = regexp.MustCompile(`\(?!?\[[^\]]*\]\([^)]*\)\)?`)

// fitLinkTextRe is a Markdown link, whose text evidence keeps.
var fitLinkTextRe = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)

// fitNumberRe is a list number that starts a text.
var fitNumberRe = regexp.MustCompile(`^[0-9]{1,3}[.)][ \t]+`)

func fitDropNumber(s string) string { return fitNumberRe.ReplaceAllString(s, "") }

// unitForms are an answer's units, normalized two ways for claims and two
// for evidence.
type unitForms struct {
	text, raw     []string // claims: Text, and the raw span without links
	evText, evRaw []string // evidence: Text, and the raw span with links as their text
}

func formsOf(answer string, units []judge.Unit) unitForms {
	var f unitForms
	for _, u := range units {
		raw := fitDropNumber(judge.SliceUTF16(answer, u.Start, u.End))
		text := fitNorm(fitDropNumber(u.Text))
		f.text = append(f.text, text)
		f.raw = append(f.raw, fitNorm(fitLinkRe.ReplaceAllString(raw, "")))
		f.evText = append(f.evText, text)
		f.evRaw = append(f.evRaw, fitNorm(fitLinkTextRe.ReplaceAllString(raw, "$1")))
	}
	return f
}

// fitOneOf finds want as one unit: its index.
func fitOneOf(want string, units []string) []int {
	if i := slices.Index(units, want); i >= 0 && want != "" {
		return []int{i}
	}
	return nil
}

// fitRunOf finds want as a run of two or more consecutive units.
func fitRunOf(want string, units []string) []int {
	for i := range units {
		acc := units[i]
		for j := i + 1; j < len(units) && strings.HasPrefix(want, acc) && len(acc) < len(want); j++ {
			acc += units[j]
			if acc == want {
				out := make([]int, 0, j-i+1)
				for k := i; k <= j; k++ {
					out = append(out, k)
				}
				return out
			}
		}
	}
	return nil
}

// fitInsideOf finds want inside one unit.
func fitInsideOf(want string, units []string) []int {
	for i, u := range units {
		if want != "" && strings.Contains(u, want) {
			return []int{i}
		}
	}
	return nil
}

// fitIn places want among the units in forms a and b: the mode and the
// unit indexes. inside allows "inside one unit".
func fitIn(want string, a, b []string, inside bool) (string, []int) {
	type finder struct {
		mode string
		f    func(string, []string) []int
	}
	finders := []finder{{fitOne, fitOneOf}, {fitRun, fitRunOf}}
	if inside {
		finders = append(finders, finder{fitInside, fitInsideOf})
	}
	for _, fd := range finders {
		if idx := fd.f(want, a); idx != nil {
			return fd.mode, idx
		}
		if idx := fd.f(want, b); idx != nil {
			return fd.mode, idx
		}
	}
	return fitNone, nil
}

// claimFit is where a claim sits: its units (ids "ai:n").
type claimFit struct {
	Mode  string
	Units []string
}

// fitCounts counts claims or evidence entries by mode.
type fitCounts struct {
	One, Run, Inside, None int
	Fails                  []string // ids of what did not map
}

func (c *fitCounts) add(mode, id string) {
	switch mode {
	case fitOne:
		c.One++
	case fitRun:
		c.Run++
	case fitInside:
		c.Inside++
	default:
		c.None++
		c.Fails = append(c.Fails, id)
	}
}

// prepared is the set split into units with maxUnits, the claims and
// evidence placed on them.
type prepared struct {
	*dataSet
	maxUnits int
	answers  map[string]judge.Answer // ansKey → answer, ID and Agent the AI
	forms    map[string]unitForms
	fit      map[string]claimFit // claimKey → fit
	fitStats fitCounts
	evUnits  map[string][]string // pair id → its evidence gold: units of the against answer, best first
	evStats  fitCounts           // per evidence entry; Fails holds "<pair id>#<entry>"
}

// splitAnswer is one AI's answer split as the product splits it (the
// compares answer, without the Manager).
func splitAnswer(ai, model, text string, maxUnits int) judge.Answer {
	a := judge.Answer{ID: ai, Agent: ai, Via: judge.ViaPC, Family: answerFamily(ai, model), Model: model,
		Status: judge.AnswerReady, Units: []judge.Unit{}, Text: text}
	units, tail, _ := judge.SplitAnswer(ai, text, maxUnits)
	if units != nil {
		a.Units = units
	}
	a.Tail = tail
	for _, u := range a.Units {
		if !u.Judged {
			a.NotJudged++
		}
	}
	return a
}

// prepare splits every answer and places the claims and the evidence.
func prepare(d *dataSet, maxUnits int) *prepared {
	p := &prepared{dataSet: d, maxUnits: maxUnits, answers: map[string]judge.Answer{}, forms: map[string]unitForms{},
		fit: map[string]claimFit{}, evUnits: map[string][]string{}}
	for k, a := range d.answers {
		ans := splitAnswer(a.AI, a.Model, a.Answer, maxUnits)
		p.answers[k] = ans
		p.forms[k] = formsOf(a.Answer, ans.Units)
	}
	for _, c := range d.claims {
		k := ansKey(c.Q, c.AI)
		f := p.forms[k]
		mode, idx := fitIn(fitNorm(c.Text), f.text, f.raw, true)
		id := claimKey(c.Q, c.AI, c.N)
		p.fitStats.add(mode, id)
		p.fit[id] = claimFit{Mode: mode, Units: unitIDs(p.answers[k], idx)}
	}
	for _, e := range d.evidence.Entries {
		pr := d.pairByID[e.ID]
		k := ansKey(pr.Q, pr.Against)
		f := p.forms[k]
		var units []string
		for i, s := range e.Evidence {
			want := fitNorm(fitDropNumber(fitLinkTextRe.ReplaceAllString(strings.TrimSpace(s), "$1")))
			mode, idx := fitIn(want, f.evText, f.evRaw, true)
			p.evStats.add(mode, e.ID+"#"+strconv.Itoa(i+1))
			for _, id := range unitIDs(p.answers[k], idx) {
				if !slices.Contains(units, id) {
					units = append(units, id)
				}
			}
		}
		p.evUnits[e.ID] = units
	}
	return p
}

// unitIDs are the ids of the units at idx.
func unitIDs(a judge.Answer, idx []int) []string {
	out := []string{}
	for _, i := range idx {
		out = append(out, a.Units[i].ID)
	}
	return out
}

// unitN is the n of a unit id "ai:n".
func unitN(id string) int {
	_, n, _ := strings.Cut(id, ":")
	v, _ := strconv.Atoi(n)
	return v
}

// spec is question q as one comparison of the three answers.
func (p *prepared) spec(q int) judge.Spec {
	s := judge.Spec{Question: p.qByID[q].Text}
	for _, ai := range ais {
		s.Answers = append(s.Answers, p.answers[ansKey(q, ai)])
	}
	return s
}
