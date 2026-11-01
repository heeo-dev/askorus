package judge

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// The parts together, as a comparison runs them. Each part is tested on
// its own; this file checks the seams: the ids Split gives are the ids
// BuildTask sends and Judgments maps back, Aggregate reads them on the
// right units, and the requests Plan makes are the ones Aggregate counts
// failures by.

// pipeFamilies are the companies of the P0-6 AIs, as their adapters name
// them.
var pipeFamilies = map[string]string{"claude": "anthropic", "codex": "openai", "opencode": "deepseek"}

// pipeClaim is the P0-6 claim of answer ai to question q whose text is
// text, compared as the claim fit compares a unit's shown text: without
// the list number an ordered item's first unit starts with, as the claims
// were copied without list markers.
func pipeClaim(t testing.TB, q int, ai, text string) (p06Claim, bool) {
	for _, c := range p06FindClaims(t, q, ai) {
		if splitNorm(c.Text) == splitNorm(splitDropNumber(text)) {
			return c, true
		}
	}
	return p06Claim{}, false
}

// pipeComparison runs P0-6 question q up to the judgments: the three
// answers split, the requests planned, and each request rendered and
// answered by a judge that reads the units' texts: a unit that is a claim
// is core and gets its gold label against the other answer, any other is
// absent and not core. No evidence is given.
func pipeComparison(t *testing.T, q int, gold map[string]string) AggregateInput {
	t.Helper()
	s := Spec{Question: p06FindQuestion(t, q).Text}
	for _, ai := range p06AIs {
		s.Judges = append(s.Judges, JudgeAI{Agent: ai, Family: pipeFamilies[ai], Model: ai + "-judge"})
		a := p06FindAnswer(t, q, ai)
		units, tail, dropped := SplitAnswer(ai, a.Answer, MaxUnits)
		if len(units) == 0 || dropped != 0 {
			t.Fatalf("Q%d %s: %d units, %d dropped", q, ai, len(units), dropped)
		}
		s.Answers = append(s.Answers, Answer{ID: ai, Agent: ai, Via: ViaPC, Family: pipeFamilies[ai], Model: a.Model, Status: AnswerReady, Units: units, Tail: tail, Text: a.Answer})
	}
	reqs, err := Plan(s)
	if err != nil || len(reqs) != 6 {
		t.Fatalf("Q%d: %d requests, %v", q, len(reqs), err)
	}
	// The gold replay's checks: the cards come from the labels alone, as
	// card() below counts them (aggGoldChecks).
	in := AggregateInput{Answers: s.Answers, Checks: aggGoldChecks, Ended: true}
	for _, r := range reqs {
		task := BuildTask(s, r)
		if Prompt(task, Nonce(task)) == "" {
			t.Fatalf("Q%d %s: no prompt", q, r.ID)
		}
		var entries []string
		for _, u := range task.Units {
			if u.ID == "" {
				continue // a context line
			}
			label, isCore := LabelAbsent, false
			if c, ok := pipeClaim(t, q, r.From, u.Text); ok {
				label, isCore = gold[fmt.Sprintf("%d-%s%d-%s", q, c.AI, c.N, r.Against)], true
			}
			entries = append(entries, validJ(u.ID, isCore, `[]`, label, false))
		}
		v, err := Validate(task, []byte("```json\n"+validOut(entries...)+"\n```"))
		if err != nil || len(v.Missing) != 0 || v.Dropped != 0 || len(v.Verdicts) != len(entries) {
			t.Fatalf("Q%d %s: %+v, %v", q, r.ID, v, err)
		}
		r.Status, r.Attempt = RequestDone, 1
		in.Requests = append(in.Requests, r)
		in.Judgments = append(in.Judgments, Judgments(r, v)...)
	}
	return in
}

func TestPipelineP06(t *testing.T) {
	gold := map[string]string{}     // "<q>-<ai><n>-<against>": the pair's label
	labels := map[string][]string{} // "<q>-<ai><n>": the labels of the claim's two pairs
	for _, p := range p06Pairs(t) {
		claim := fmt.Sprintf("%d-%s%d", p.Q, p.From, p.N)
		gold[claim+"-"+p.Against] = p.Label
		labels[claim] = append(labels[claim], p.Label)
	}
	// card is a claim's card as the gold replay counts it: any opposite is
	// 다른 말, else any same 같은 말, else 한 AI만.
	card := func(claim string) string {
		switch {
		case slices.Contains(labels[claim], LabelOpposite):
			return CardDiffer
		case slices.Contains(labels[claim], LabelSame):
			return CardAgree
		}
		return CardOnlyOne
	}
	total := map[string]int{}
	missing := 0
	for _, q := range p06Questions(t) {
		in := pipeComparison(t, q.ID, gold)
		s := aggRun(t, in)
		if len(s.Judgments) != len(in.Judgments) || s.Counts.NotCompared != 0 || s.Counts.NotJudged != 0 {
			t.Errorf("Q%d: %d of %d judgments usable, counts %+v", q.ID, len(s.Judgments), len(in.Judgments), s.Counts)
		}
		// Each unit is where the judge's reading of its text puts it: a
		// claim on its claim's card, any other unit hidden.
		states := aggStates(s)
		for _, a := range in.Answers {
			for _, u := range a.Units {
				want := StateHidden
				if c, ok := pipeClaim(t, q.ID, a.ID, u.Text); ok {
					want = card(fmt.Sprintf("%d-%s%d", q.ID, c.AI, c.N))
				}
				got, _, _ := strings.Cut(states[u.ID], " ")
				if got != want {
					t.Errorf("Q%d %s %q: %s, want %s", q.ID, u.ID, u.Text, states[u.ID], want)
				}
				total[got]++
			}
		}

		// The first request fails: its units lose that answer, and only the
		// items that stand on them name it.
		r1 := in.Requests[0]
		in.Requests[0].Status, in.Requests[0].Error = RequestFailed, &Error{Code: core.ErrCodeJudgeFailed}
		in.Judgments = slices.DeleteFunc(in.Judgments, func(j Judgment) bool { return j.Request == r1.ID })
		s = aggRun(t, in)
		if s.Counts.NotCompared != 0 {
			t.Errorf("Q%d without %s: %d units not compared", q.ID, r1.ID, s.Counts.NotCompared)
		}
		for _, it := range s.Items {
			if len(it.Missing) == 0 {
				continue
			}
			missing++
			onFrom := slices.ContainsFunc(it.Positions, func(p Position) bool { return slices.Contains(p.Agents, r1.From) })
			if !slices.Equal(it.Missing, []string{r1.Against}) || !onFrom {
				t.Errorf("Q%d without %s (%s against %s): %q", q.ID, r1.ID, r1.From, r1.Against, aggDump(Summary{Items: []Item{it}}))
			}
		}
	}
	t.Logf("units by state: %v; %d items name a failed request's answer", total, missing)
	if total[CardDiffer] == 0 || total[CardAgree] == 0 || total[CardOnlyOne] == 0 || missing == 0 {
		t.Errorf("a card or a missing answer never came up: %v, %d", total, missing)
	}
}
