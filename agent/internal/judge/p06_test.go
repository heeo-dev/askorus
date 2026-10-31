package judge

// This file holds the loaders for the P0-6 judgment set that the tests of
// every part share, and the tests of types.go.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

// The P0-6 set (docs/judge, read only): 30 questions, the 90 answers
// Claude, Codex and OpenCode gave on 2026-10-30, and 307 key claims taken
// from them with 614 labelled pairs (a claim against another AI's whole
// answer).
const (
	p06Dir           = "../../../docs/judge" // from this package's folder
	p06QuestionsName = "questions.json"
	p06AnswersName   = "answers-2026-10-30.json"
	p06PairsName     = "pairs-2026-10-30.json"
)

// p06AIs are the AIs of the set, in the turn's order.
var p06AIs = []string{"claude", "codex", "opencode"}

// p06Question is one question of questions.json.
type p06Question struct {
	ID    int    `json:"id"`
	Group string `json:"group"` // A facts, B recent facts, C contested, D lists, E traps, F opinions
	Lang  string `json:"lang"`  // ko or en
	Text  string `json:"text"`
	Aim   string `json:"aim"`
}

// p06Answer is one AI's answer to one question.
type p06Answer struct {
	Q       int     `json:"id"` // the question's id
	AI      string  `json:"ai"`
	Model   string  `json:"model"`
	Seconds float64 `json:"seconds"`
	Session string  `json:"session"`
	Answer  string  `json:"answer"` // as the AI gave it, Markdown and all
	Error   string  `json:"error"`  // empty in this set
}

// p06Claim is a key claim, copied from an answer with list markers, "**"
// and inline source links left out (table rows with cells joined by
// " | ").
type p06Claim struct {
	Q    int    `json:"q"`
	AI   string `json:"ai"`
	N    int    `json:"n"` // 1, 2, … within the answer
	Text string `json:"text"`
}

// p06Pair is a claim against another AI's whole answer. Label is the gold
// label; Hard marks pairs careful readers could still disagree on. Never
// give a judge anything but the question, the claim and the answers.
type p06Pair struct {
	ID      string   `json:"id"` // "<q>-<from><n>-<against>"
	Q       int      `json:"q"`
	From    string   `json:"from"`
	N       int      `json:"n"`
	Against string   `json:"against"`
	Draft   string   `json:"draft"`
	Doubt   bool     `json:"doubt"`
	Reason  string   `json:"reason"`
	Review  bool     `json:"review"`
	Label   string   `json:"label"` // same, opposite or absent
	Check   string   `json:"check"` // unanimous or judged
	Hard    bool     `json:"hard"`
	Blind   []string `json:"blind"` // the two blind labels, judged pairs only
	Note    string   `json:"note"`  // the adjudicator's reason, judged pairs only
}

// p06Cache reads one file of the set once for every test.
type p06Cache[T any] struct {
	once sync.Once
	val  T
	err  error
}

func (c *p06Cache[T]) load(t testing.TB, name string) T {
	t.Helper()
	c.once.Do(func() {
		raw, err := os.ReadFile(filepath.Join(p06Dir, name))
		if err == nil {
			err = json.Unmarshal(raw, &c.val)
		}
		c.err = err
	})
	if c.err != nil {
		t.Fatalf("P0-6 set: %v", c.err)
	}
	return c.val
}

var (
	p06QuestionsFile p06Cache[struct {
		Questions []p06Question `json:"questions"`
	}]
	p06AnswersFile p06Cache[struct {
		Answers []p06Answer `json:"answers"`
	}]
	p06PairsFile p06Cache[struct {
		Claims []p06Claim `json:"claims"`
		Pairs  []p06Pair  `json:"pairs"`
	}]
)

// p06Questions are the 30 questions, by id. The slices these loaders
// return are copies; the strings in them are shared.
func p06Questions(t testing.TB) []p06Question {
	t.Helper()
	return slices.Clone(p06QuestionsFile.load(t, p06QuestionsName).Questions)
}

// p06Answers are the 90 answers, by question and then claude, codex,
// opencode.
func p06Answers(t testing.TB) []p06Answer {
	t.Helper()
	return slices.Clone(p06AnswersFile.load(t, p06AnswersName).Answers)
}

// p06Claims are the 307 key claims, by question, AI and n.
func p06Claims(t testing.TB) []p06Claim {
	t.Helper()
	return slices.Clone(p06PairsFile.load(t, p06PairsName).Claims)
}

// p06Pairs are the 614 labelled pairs, two per claim.
func p06Pairs(t testing.TB) []p06Pair {
	t.Helper()
	return slices.Clone(p06PairsFile.load(t, p06PairsName).Pairs)
}

// p06FindQuestion is question q.
func p06FindQuestion(t testing.TB, q int) p06Question {
	t.Helper()
	for _, x := range p06QuestionsFile.load(t, p06QuestionsName).Questions {
		if x.ID == q {
			return x
		}
	}
	t.Fatalf("P0-6 has no question %d", q)
	return p06Question{}
}

// p06FindAnswer is the answer ai gave to question q.
func p06FindAnswer(t testing.TB, q int, ai string) p06Answer {
	t.Helper()
	for _, a := range p06AnswersFile.load(t, p06AnswersName).Answers {
		if a.Q == q && a.AI == ai {
			return a
		}
	}
	t.Fatalf("P0-6 has no answer of %s to question %d", ai, q)
	return p06Answer{}
}

// p06FindClaims are the claims taken from ai's answer to question q, in
// order of n.
func p06FindClaims(t testing.TB, q int, ai string) []p06Claim {
	t.Helper()
	var out []p06Claim
	for _, c := range p06PairsFile.load(t, p06PairsName).Claims {
		if c.Q == q && c.AI == ai {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b p06Claim) int { return a.N - b.N })
	return out
}

func TestP06Set(t *testing.T) {
	qs, as, cs, ps := p06Questions(t), p06Answers(t), p06Claims(t), p06Pairs(t)
	if len(qs) != 30 || len(as) != 90 || len(cs) != 307 || len(ps) != 614 {
		t.Fatalf("questions %d, answers %d, claims %d, pairs %d; want 30, 90, 307, 614", len(qs), len(as), len(cs), len(ps))
	}
	for _, q := range qs {
		if q.Text == "" || (q.Lang != "ko" && q.Lang != "en") {
			t.Errorf("question %+v", q)
		}
		for _, ai := range p06AIs {
			if a := p06FindAnswer(t, q.ID, ai); a.Answer == "" || a.Model == "" || a.Error != "" {
				t.Errorf("answer of %s to %d: model %q, %d bytes, error %q", ai, q.ID, a.Model, len(a.Answer), a.Error)
			}
			if len(p06FindClaims(t, q.ID, ai)) == 0 {
				t.Errorf("no claim from %s on question %d", ai, q.ID)
			}
		}
	}
	if got := p06FindQuestion(t, 12); got.Group != "B" {
		t.Errorf("question 12 = %+v", got)
	}

	// Every claim has exactly two pairs, one against each other AI, and
	// the labels are those the set reports: 502 same, 91 absent, 21
	// opposite.
	perClaim := map[string]int{}
	labels := map[string]int{}
	for _, p := range ps {
		if want := fmt.Sprintf("%d-%s%d-%s", p.Q, p.From, p.N, p.Against); p.ID != want || p.From == p.Against {
			t.Errorf("pair %q: want id %q", p.ID, want)
		}
		perClaim[fmt.Sprintf("%d-%s%d", p.Q, p.From, p.N)]++
		labels[p.Label]++
	}
	for _, c := range cs {
		if n := perClaim[fmt.Sprintf("%d-%s%d", c.Q, c.AI, c.N)]; n != 2 || c.Text == "" {
			t.Errorf("claim %d-%s%d: %d pairs, text %q", c.Q, c.AI, c.N, n, c.Text)
		}
	}
	if labels[LabelSame] != 502 || labels[LabelAbsent] != 91 || labels[LabelOpposite] != 21 || len(labels) != 3 {
		t.Errorf("labels = %v", labels)
	}
}

func TestJudgeErrorShowsOnlyTheCode(t *testing.T) {
	var err error = &Error{Code: "judge_failed", Message: "You've hit your usage limit"}
	if err.Error() != "judge_failed" {
		t.Errorf("Error() = %q", err.Error())
	}
	raw, _ := json.Marshal(err)
	if string(raw) != `{"code":"judge_failed","message":"You've hit your usage limit"}` {
		t.Errorf("JSON = %s", raw)
	}
}
