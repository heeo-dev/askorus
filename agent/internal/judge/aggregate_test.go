package judge

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

// aggAns is a ready PC answer whose units, all judged, have the texts.
func aggAns(id string, texts ...string) Answer {
	a := Answer{ID: id, Agent: id, Via: ViaPC, Family: id, Status: AnswerReady, Units: []Unit{}}
	for i, s := range texts {
		a.Units = append(a.Units, Unit{ID: fmt.Sprintf("%s:%d", id, i+1), N: i + 1, Kind: UnitSentence, Text: s, Judged: true})
	}
	return a
}

// aggPlain are answers with n units each and no numbers in them.
func aggPlain(n int, ids ...string) []Answer {
	var out []Answer
	for _, id := range ids {
		texts := make([]string, n)
		for i := range texts {
			texts[i] = fmt.Sprintf("%s says thing %c.", id, 'A'+i)
		}
		out = append(out, aggAns(id, texts...))
	}
	return out
}

// aggJ is a core judgment by "judge", an AI that wrote none of the answers.
func aggJ(unit, against, label string, evidence ...string) Judgment {
	if evidence == nil {
		evidence = []string{}
	}
	return Judgment{Unit: unit, Against: against, Request: "r", Judge: "judge", Core: true, Label: label, Evidence: evidence, Flags: []string{}}
}

// aggUnsure and aggNotCore are j with unsure set or core cleared.
func aggUnsure(j Judgment) Judgment  { j.Unsure = true; return j }
func aggNotCore(j Judgment) Judgment { j.Core = false; return j }

// aggRequests are requests with the status for every ordered pair of
// answers, judged by "judge".
func aggRequests(answers []Answer, status string) []Request {
	var out []Request
	for _, x := range answers {
		for _, y := range answers {
			if x.ID != y.ID {
				out = append(out, Request{ID: fmt.Sprintf("r%d", len(out)+1), From: x.ID, Against: y.ID, Judge: "judge", Status: status, Attempt: 1})
			}
		}
	}
	return out
}

// aggDump writes each item on a line: its key, then each side as
// "[answers: units]" ("*" for a whole answer), then numbersDiffer, check
// and missing when set.
func aggDump(s Summary) []string {
	out := []string{}
	for _, it := range s.Items {
		var b strings.Builder
		b.WriteString(it.Key)
		for i, p := range it.Positions {
			if i > 0 {
				b.WriteString(" <>")
			}
			units := strings.Join(p.Units, " ")
			if units == "" {
				units = "*"
			}
			fmt.Fprintf(&b, " [%s: %s]", strings.Join(p.Agents, " "), units)
		}
		if it.NumbersDiffer {
			b.WriteString(" numbersDiffer")
		}
		if len(it.Check) > 0 {
			fmt.Fprintf(&b, " check=%s", strings.Join(it.Check, ","))
		}
		if len(it.Missing) > 0 {
			fmt.Fprintf(&b, " missing=%s", strings.Join(it.Missing, ","))
		}
		out = append(out, b.String())
	}
	return out
}

// aggStates are the units' states: "differ d:…" on an item, or the state.
func aggStates(s Summary) map[string]string {
	out := map[string]string{}
	for _, u := range s.Units {
		out[u.Unit] = strings.TrimSpace(u.State + " " + u.Item)
	}
	return out
}

// aggRun aggregates in, checks the invariants and checks that running it
// again with its own items as in.Prev changes nothing.
func aggRun(t testing.TB, in AggregateInput) Summary {
	t.Helper()
	s := Aggregate(in)
	aggCheck(t, in, s)
	again := in
	again.Prev = s.Items
	if s2 := Aggregate(again); !reflect.DeepEqual(s2, s) {
		t.Errorf("again with Prev:\n got %q\nwant %q", aggDump(s2), aggDump(s))
	}
	return s
}

// aggCheck checks what holds for every Summary: each unit once, on one
// item at most, every shown unit on a card once the requests have ended,
// keys that are unique and name their card, and arrays that are never nil.
func aggCheck(t testing.TB, in AggregateInput, s Summary) {
	t.Helper()
	var ids []string
	text := map[string]string{}
	answerOf := map[string]string{}
	judged := map[string]bool{}
	for _, a := range in.Answers {
		for _, u := range a.Units {
			ids = append(ids, u.ID)
			if _, dup := text[u.ID]; !dup {
				text[u.ID], answerOf[u.ID], judged[u.ID] = u.Text, a.ID, u.Judged
			}
		}
	}
	if len(s.Units) != len(ids) {
		t.Fatalf("%d unit states for %d units", len(s.Units), len(ids))
	}
	if s.Judgments == nil || s.Items == nil {
		t.Errorf("nil judgments or items")
	}
	prefix := map[string]string{CardDiffer: "d:", CardCheck: "c:", CardAgree: "a:", CardOnlyOne: "o:"}
	kindOf := map[string]string{}
	where := map[string]string{}
	var counts Counts
	last := 0
	for _, it := range s.Items {
		card := slices.Index(CardOrder, it.Kind)
		if card < last || card < 0 {
			t.Errorf("item %s of kind %q out of card order", it.Key, it.Kind)
		}
		last = max(last, card)
		if !in.Ended && it.Kind != CardDiffer {
			t.Errorf("item %s before every request ended", it.Key)
		}
		if _, dup := kindOf[it.Key]; dup || !strings.HasPrefix(it.Key, prefix[it.Kind]) {
			t.Errorf("key %q of a %s item: repeated or wrong prefix", it.Key, it.Kind)
		}
		kindOf[it.Key] = it.Kind
		if it.Check == nil || it.Missing == nil {
			t.Errorf("item %s: nil check or missing", it.Key)
		}
		for _, c := range it.Check {
			if !slices.Contains(in.Checks, c) {
				t.Errorf("item %s: check %s is not enabled", it.Key, c)
			}
		}
		switch it.Kind {
		case CardDiffer:
			counts.Differ++
			if len(it.Positions) < 2 {
				t.Errorf("differ item %s has %d sides", it.Key, len(it.Positions))
			}
		case CardCheck:
			counts.Check++
			if len(it.Check) == 0 {
				t.Errorf("check item %s has no check", it.Key)
			}
		case CardAgree:
			counts.Agree++
			if len(it.Positions) == 1 && len(it.Positions[0].Agents) < 2 {
				t.Errorf("agree item %s has one AI", it.Key)
			}
		case CardOnlyOne:
			counts.OnlyOne++
		}
		if it.Kind != CardDiffer && len(it.Positions) != 1 {
			t.Errorf("%s item %s has %d sides", it.Kind, it.Key, len(it.Positions))
		}
		if (it.Kind == CardAgree || it.Kind == CardOnlyOne) && len(it.Check) > 0 {
			t.Errorf("%s item %s is tagged %v", it.Kind, it.Key, it.Check)
		}
		sides := map[string]int{}
		for p, pos := range it.Positions {
			if pos.Agents == nil || pos.Units == nil || len(pos.Agents) == 0 {
				t.Errorf("item %s side %d: %+v", it.Key, p, pos)
			}
			for _, x := range pos.Agents {
				if q, ok := sides[x]; ok && q != p {
					t.Errorf("item %s: %s on sides %d and %d", it.Key, x, q, p)
				}
				sides[x] = p
			}
			for _, u := range pos.Units {
				if w, ok := where[u]; ok {
					t.Errorf("unit %s on items %s and %s", u, w, it.Key)
				}
				where[u] = it.Key
				if !slices.Contains(pos.Agents, answerOf[u]) {
					t.Errorf("item %s: unit %s on a side without %s", it.Key, u, answerOf[u])
				}
			}
			for _, sp := range pos.NumberSpans {
				n := len(utf16.Encode([]rune(text[sp.Unit])))
				if !slices.Contains(pos.Units, sp.Unit) || sp.Start < 0 || sp.Start >= sp.End || sp.End > n {
					t.Errorf("item %s: span %+v", it.Key, sp)
				}
			}
		}
	}
	for i, st := range s.Units {
		if st.Unit != ids[i] {
			t.Errorf("unit state %d is %s, want %s", i, st.Unit, ids[i])
		}
		if k, ok := where[st.Unit]; ok {
			if st.State != kindOf[k] || st.Item != k {
				t.Errorf("unit %s: state %s %s, but it is on %s", st.Unit, st.State, st.Item, k)
			}
			continue
		}
		if st.Item != "" {
			t.Errorf("unit %s: item %s it is not on", st.Unit, st.Item)
		}
		switch st.State {
		case StateHidden, StateNotCompared:
			if !in.Ended {
				t.Errorf("unit %s is %s before the end", st.Unit, st.State)
			}
			if st.State == StateNotCompared {
				counts.NotCompared++
			}
		case StateNotJudged:
			counts.NotJudged++
			if judged[st.Unit] {
				t.Errorf("judged unit %s is notJudged", st.Unit)
			}
		case StateWaiting:
			if in.Ended {
				t.Errorf("unit %s waits after the end", st.Unit)
			}
		default:
			t.Errorf("unit %s: state %q", st.Unit, st.State)
		}
	}
	if s.Counts != counts {
		t.Errorf("counts = %+v, want %+v", s.Counts, counts)
	}
	shown := map[string]bool{}
	opposed := map[string]bool{}
	has := map[string]bool{}
	for _, j := range s.Judgments {
		if j.Evidence == nil || j.Flags == nil || len(j.Evidence) > MaxEvidence {
			t.Errorf("judgment %+v", j)
		}
		for _, f := range j.Flags {
			if !slices.Contains(AllChecks, f) {
				t.Errorf("judgment %+v: flag %s", j, f)
			}
		}
		for _, e := range j.Evidence {
			if answerOf[e] != j.Against {
				t.Errorf("judgment %+v: evidence %s is not of %s", j, e, j.Against)
			}
		}
		if answerOf[j.Unit] == j.Against || !judged[j.Unit] {
			t.Errorf("judgment %+v should have been left out", j)
		}
		has[j.Unit] = true
		if j.Core {
			shown[j.Unit] = true
		}
		if j.Label == LabelOpposite {
			opposed[j.Unit], shown[j.Unit] = true, true
			for _, e := range j.Evidence {
				opposed[e], shown[e] = true, true
			}
		}
	}
	for u := range opposed {
		if kindOf[where[u]] != CardDiffer {
			t.Errorf("unit %s is in an opposite but not on 다른 말 (%q)", u, where[u])
		}
	}
	for u := range shown {
		if in.Ended && where[u] == "" {
			t.Errorf("shown unit %s is on no card", u)
		}
	}
	for _, st := range s.Units {
		if st.State == StateNotCompared && has[st.Unit] {
			t.Errorf("unit %s has judgments but is notCompared", st.Unit)
		}
	}
}

func TestAggregateCards(t *testing.T) {
	duo := aggPlain(3, "claude", "codex")
	trio := aggPlain(3, "claude", "codex", "opencode")
	four := aggPlain(3, "claude", "codex", "opencode", "gemini")
	for _, c := range []struct {
		name      string
		answers   []Answer
		judgments []Judgment
		checks    []string // DefaultChecks when nil
		want      []string
		states    map[string]string
	}{
		{
			name:    "same both ways is one 같은 말 item",
			answers: duo,
			judgments: []Judgment{
				aggJ("claude:1", "codex", LabelSame, "codex:1"),
				aggJ("codex:1", "claude", LabelSame, "claude:1"),
			},
			want:   []string{"a:claude:1 [claude codex: claude:1 codex:1]"},
			states: map[string]string{"codex:1": "agree a:claude:1", "claude:2": StateNotCompared},
		},
		{
			name:    "같은 말 takes only units linked to its representative",
			answers: trio,
			judgments: []Judgment{
				aggJ("claude:1", "codex", LabelSame, "codex:1"),
				aggJ("claude:1", "opencode", LabelAbsent),
				aggJ("codex:1", "claude", LabelSame, "claude:1"),
				aggJ("codex:1", "opencode", LabelSame, "opencode:1"),
				aggJ("opencode:1", "codex", LabelSame, "codex:1"),
				aggJ("opencode:1", "claude", LabelAbsent),
			},
			want: []string{"a:claude:1 [claude codex: claude:1 codex:1]", "a:opencode:1 [codex opencode: opencode:1]"},
		},
		{
			name:    "more AIs first on 같은 말",
			answers: trio,
			judgments: []Judgment{
				aggJ("claude:1", "codex", LabelSame, "codex:1"),
				aggJ("claude:1", "opencode", LabelAbsent),
				aggJ("claude:2", "codex", LabelSame, "codex:2"),
				aggJ("claude:2", "opencode", LabelSame, "opencode:2"),
			},
			want: []string{"a:claude:2 [claude codex opencode: claude:2 codex:2 opencode:2]", "a:claude:1 [claude codex: claude:1 codex:1]"},
		},
		{
			// S6 (11-01 user decision 2, P1): an unsure same counts as same;
			// TestAggregateUnsureRule has the exceptions.
			name:    "an unsure same counts as same",
			answers: duo,
			judgments: []Judgment{
				aggUnsure(aggJ("claude:1", "codex", LabelSame, "codex:1")),
				aggJ("codex:1", "claude", LabelSame, "claude:1"),
			},
			want: []string{"a:claude:1 [claude codex: claude:1 codex:1]"},
		},
		{
			// S6 (P2): an unsure absent counts as absent, back to 한 AI만
			// (undoing S1 review decision 7).
			name:    "an unsure absent counts as absent",
			answers: duo,
			judgments: []Judgment{
				aggUnsure(aggJ("claude:1", "codex", LabelAbsent)),
			},
			want: []string{"o:claude:1 [claude: claude:1]"},
		},
		{
			name:    "an unsure absent leaves a sure same on 같은 말",
			answers: trio,
			judgments: []Judgment{
				aggJ("claude:1", "codex", LabelSame, "codex:1"),
				aggUnsure(aggJ("claude:1", "opencode", LabelAbsent)),
			},
			want: []string{"a:claude:1 [claude codex: claude:1 codex:1]"},
		},
		{
			name:    "unclear counts as unsure",
			answers: duo,
			judgments: []Judgment{
				aggJ("claude:1", "codex", LabelUnclear),
			},
			want: []string{"c:claude:1 [claude: claude:1] check=unsure"},
		},
		{
			name:    "disabled checks never tag",
			answers: duo,
			checks:  []string{},
			judgments: []Judgment{
				aggUnsure(aggJ("claude:1", "codex", LabelSame, "codex:1")),
				aggJ("claude:2", "codex", LabelUnclear),
			},
			want: []string{"a:claude:1 [claude codex: claude:1 codex:1]", "o:claude:2 [claude: claude:2]"},
		},
		{
			name:    "an opposite with unsure stays on 다른 말",
			answers: duo,
			judgments: []Judgment{
				aggUnsure(aggJ("claude:1", "codex", LabelOpposite, "codex:1")),
			},
			want:   []string{"d:claude:1~codex:1 [claude: claude:1] <> [codex: codex:1] check=unsure"},
			states: map[string]string{"codex:1": "differ d:claude:1~codex:1"},
		},
		{
			name:    "an opposite no judge called core is shown",
			answers: duo,
			judgments: []Judgment{
				aggNotCore(aggJ("claude:1", "codex", LabelOpposite, "codex:2")),
				aggNotCore(aggJ("claude:2", "codex", LabelSame, "codex:1")),
			},
			want:   []string{"d:claude:1~codex:2 [claude: claude:1] <> [codex: codex:2]"},
			states: map[string]string{"claude:2": StateHidden},
		},
		{
			name:    "mirrored opposites are one item",
			answers: duo,
			judgments: []Judgment{
				aggJ("codex:2", "claude", LabelOpposite, "claude:1"),
				aggJ("claude:1", "codex", LabelOpposite, "codex:2"),
			},
			want: []string{"d:codex:2~claude:1 [claude: claude:1] <> [codex: codex:2]"},
		},
		{
			name:    "three values are three sides",
			answers: trio,
			judgments: []Judgment{
				aggJ("claude:1", "codex", LabelOpposite, "codex:1"),
				aggJ("claude:1", "opencode", LabelOpposite, "opencode:1"),
				aggJ("codex:1", "opencode", LabelOpposite, "opencode:1"),
			},
			want: []string{"d:claude:1~codex:1 [claude: claude:1] <> [codex: codex:1] <> [opencode: opencode:1]"},
		},
		{
			name:    "same with evidence joins two sides",
			answers: trio,
			judgments: []Judgment{
				aggJ("claude:1", "codex", LabelOpposite, "codex:1"),
				aggJ("claude:1", "opencode", LabelOpposite, "opencode:1"),
				aggJ("codex:1", "opencode", LabelSame, "opencode:1"),
			},
			want: []string{"d:claude:1~codex:1 [claude: claude:1] <> [codex opencode: codex:1 opencode:1]"},
		},
		{
			name:    "same without evidence joins nothing",
			answers: trio,
			judgments: []Judgment{
				aggJ("claude:1", "codex", LabelOpposite, "codex:1"),
				aggJ("claude:1", "opencode", LabelOpposite, "opencode:1"),
				aggJ("codex:1", "opencode", LabelSame),
			},
			want: []string{"d:claude:1~codex:1 [claude: claude:1] <> [codex: codex:1] <> [opencode: opencode:1]"},
		},
		{
			name:    "a same between opposed sides does not join them",
			answers: duo,
			judgments: []Judgment{
				aggJ("claude:1", "codex", LabelOpposite, "codex:1"),
				aggJ("claude:2", "codex", LabelOpposite, "codex:2"),
				aggJ("codex:2", "claude", LabelSame, "claude:1"),
				aggJ("claude:2", "codex", LabelOpposite, "codex:1"),
			},
			want: []string{"d:claude:1~codex:1 [claude: claude:1 claude:2] <> [codex: codex:1 codex:2]"},
		},
		{
			name:    "a unit of an AI on no side joins the side it says the same as",
			answers: four,
			judgments: []Judgment{
				aggJ("claude:1", "codex", LabelOpposite, "codex:1"),
				aggJ("gemini:1", "claude", LabelSame, "claude:1"),
				// opencode:1 says the same as gemini:1, which only joined: no chaining.
				aggJ("opencode:1", "gemini", LabelSame, "gemini:1"),
				aggJ("opencode:1", "claude", LabelAbsent),
				aggJ("opencode:1", "codex", LabelAbsent),
			},
			want: []string{
				// claude:1 and codex:1 were not compared with OpenCode, but
				// gemini:1, which only joined, does not count.
				"d:claude:1~codex:1 [claude gemini: claude:1 gemini:1] <> [codex: codex:1] missing=opencode",
				"a:opencode:1 [opencode gemini: opencode:1]",
			},
		},
		{
			name:    "a unit that joins comes after the side's own units",
			answers: trio,
			judgments: []Judgment{
				aggJ("codex:1", "opencode", LabelOpposite, "opencode:1"),
				aggJ("claude:1", "opencode", LabelSame, "opencode:1"),
				aggJ("claude:1", "codex", LabelAbsent),
			},
			want: []string{"d:codex:1~opencode:1 [codex: codex:1] <> [claude opencode: opencode:1 claude:1]"},
		},
		{
			name:    "a unit of an AI on the other side does not join",
			answers: trio,
			judgments: []Judgment{
				aggJ("claude:3", "codex", LabelOpposite, "codex:2"),
				aggJ("opencode:2", "claude", LabelOpposite, "claude:3"),
				aggJ("opencode:3", "claude", LabelSame, "claude:3"),
				aggJ("opencode:3", "codex", LabelAbsent),
				aggJ("claude:2", "codex", LabelSame, "codex:2"),
				aggJ("claude:2", "opencode", LabelAbsent),
			},
			want: []string{
				"d:claude:3~codex:2 [claude: claude:3] <> [codex: codex:2] <> [opencode: opencode:2]",
				"a:claude:2 [claude codex: claude:2]",
				"a:opencode:3 [claude opencode: opencode:3]",
			},
		},
		{
			name:    "a unit tied to two sides joins neither",
			answers: trio,
			judgments: []Judgment{
				aggJ("claude:1", "codex", LabelOpposite, "codex:1"),
				aggJ("opencode:1", "claude", LabelSame, "claude:1"),
				aggJ("opencode:1", "codex", LabelSame, "codex:1"),
			},
			want: []string{
				"d:claude:1~codex:1 [claude: claude:1] <> [codex: codex:1] missing=opencode",
				"a:opencode:1 [claude codex opencode: opencode:1]",
			},
		},
		{
			name:    "an opposite without evidence stands against the whole answer",
			answers: duo,
			judgments: []Judgment{
				aggJ("claude:1", "codex", LabelOpposite),
			},
			want: []string{"d:claude:1~codex [claude: claude:1] <> [codex: *]"},
		},
		{
			// The key is still the first opposite judgment's: the whole
			// answer is the item's older name.
			name:    "evidence the other way takes the place of the whole answer",
			answers: duo,
			judgments: []Judgment{
				aggJ("claude:1", "codex", LabelOpposite),
				aggJ("codex:2", "claude", LabelOpposite, "claude:1"),
			},
			want: []string{"d:claude:1~codex [claude: claude:1] <> [codex: codex:2]"},
		},
		{
			name:    "judgments by the author's AI, on unknown units or with unknown labels are left out",
			answers: duo,
			judgments: []Judgment{
				func() Judgment {
					j := aggJ("claude:1", "codex", LabelOpposite, "codex:1")
					j.Judge = "claude"
					return j
				}(),
				func() Judgment { j := aggJ("claude:2", "codex", LabelSame, "codex:1"); j.Judge = "codex"; return j }(),
				aggJ("claude:3", "codex", "maybe", "codex:1"),
				aggJ("claude:9", "codex", LabelSame, "codex:1"),
				aggJ("claude:1", "claude", LabelSame, "claude:2"),
				aggJ("claude:1", "gemini", LabelSame),
			},
			want:   []string{},
			states: map[string]string{"claude:1": StateNotCompared, "claude:2": StateNotCompared, "claude:3": StateNotCompared, "codex:1": StateNotCompared},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			checks := c.checks
			if checks == nil {
				checks = DefaultChecks
			}
			in := AggregateInput{Answers: c.answers, Requests: aggRequests(c.answers, RequestDone), Judgments: c.judgments, Checks: checks, Ended: true}
			s := aggRun(t, in)
			if got := aggDump(s); !slices.Equal(got, c.want) {
				t.Errorf("items:\n got %q\nwant %q", got, c.want)
			}
			states := aggStates(s)
			for u, want := range c.states {
				if states[u] != want {
					t.Errorf("unit %s = %q, want %q", u, states[u], want)
				}
			}
		})
	}
}

// The unsure rule of S6 (11-01 user decision 2), with the default checks
// unless a pass names others. P1: an unsure same counts as same unless
// numberMismatch fires on it; P2: an unsure absent counts as absent unless
// numbersAbsent fires on it; P3: an unsure opposite is as before. The
// number check that fired is the reason (Item.Check), not unsure; with that
// check off, unsure stands in for it. Every case fails on the rule before
// S6, where any unsure mark counted.
func TestAggregateUnsureRule(t *testing.T) {
	population := []Answer{aggAns("claude", "서울 인구는 약 970만 명 안팎입니다."), aggAns("codex", "서울 인구는 약 956만 명입니다.")}
	coffee := []Answer{aggAns("claude", "커피는 하루 3잔이면 충분합니다."), aggAns("codex", "Plain.", "커피는 하루 5잔까지 마셔도 됩니다.")}
	sameNumbers := []Answer{aggAns("claude", "서울 인구는 약 950만 명입니다."), aggAns("codex", "서울 인구는 약 950만 명입니다.")}
	duo := aggPlain(3, "claude", "codex")
	type pass struct {
		checks []string // DefaultChecks when nil
		want   []string
	}
	for _, c := range []struct {
		name      string
		answers   []Answer
		judgments []Judgment
		passes    []pass
	}{
		{
			name:      "P1: an unsure same whose numbers fit is 같은 말",
			answers:   sameNumbers,
			judgments: []Judgment{aggUnsure(aggJ("claude:1", "codex", LabelSame, "codex:1")), aggUnsure(aggJ("codex:1", "claude", LabelSame, "claude:1"))},
			passes: []pass{
				{nil, []string{"a:claude:1 [claude codex: claude:1 codex:1]"}},
				{[]string{CheckUnsure}, []string{"a:claude:1 [claude codex: claude:1 codex:1]"}},
			},
		},
		{
			name:      "P1 exception: an unsure same with numberMismatch is 확인 필요 for the numbers",
			answers:   population,
			judgments: []Judgment{aggUnsure(aggJ("claude:1", "codex", LabelSame, "codex:1"))},
			passes: []pass{
				{nil, []string{"c:claude:1 [claude: claude:1] check=numberMismatch"}},
				// numberMismatch off: the unsure mark stands in for it.
				{[]string{CheckUnsure}, []string{"c:claude:1 [claude: claude:1] check=unsure"}},
				{[]string{CheckUnsure, CheckNumbersAbsent}, []string{"c:claude:1 [claude: claude:1] check=unsure"}},
			},
		},
		{
			name:      "P1 exception without evidence: the whole other answer's numbers",
			answers:   population,
			judgments: []Judgment{aggUnsure(aggJ("claude:1", "codex", LabelSame))},
			passes: []pass{
				{nil, []string{"c:claude:1 [claude: claude:1] check=numberMismatch"}},
				{[]string{CheckUnsure, CheckNoEvidence, CheckNumberMismatch}, []string{"c:claude:1 [claude: claude:1] check=noEvidence,numberMismatch"}},
			},
		},
		{
			// S6R-1: the numbers are written with one-glyph units, which
			// Numbers once could not read, so the pair passed as 같은 말.
			name:    "P1 exception with one-glyph units (㎢, ㎏)",
			answers: []Answer{aggAns("claude", "서울 면적은 605㎢입니다.", "쌀 한 포대는 2㎏입니다."), aggAns("codex", "서울 면적은 700㎢입니다.", "쌀 한 포대는 5㎏입니다.")},
			judgments: []Judgment{
				aggUnsure(aggJ("claude:1", "codex", LabelSame, "codex:1")),
				aggUnsure(aggJ("claude:2", "codex", LabelSame)),
			},
			passes: []pass{
				{nil, []string{"c:claude:1 [claude: claude:1] check=numberMismatch", "c:claude:2 [claude: claude:2] check=numberMismatch"}},
				{[]string{CheckUnsure}, []string{"c:claude:1 [claude: claude:1] check=unsure", "c:claude:2 [claude: claude:2] check=unsure"}},
			},
		},
		{
			name:      "P2: an unsure absent with no other numbers is 한 AI만",
			answers:   duo,
			judgments: []Judgment{aggUnsure(aggJ("claude:1", "codex", LabelAbsent)), aggUnsure(aggJ("codex:1", "claude", LabelAbsent))},
			passes: []pass{
				{nil, []string{"o:claude:1 [claude: claude:1]", "o:codex:1 [codex: codex:1]"}},
				{[]string{CheckUnsure}, []string{"o:claude:1 [claude: claude:1]", "o:codex:1 [codex: codex:1]"}},
			},
		},
		{
			name:      "P2 exception: an unsure absent with numbersAbsent is 확인 필요 for the numbers",
			answers:   coffee,
			judgments: []Judgment{aggUnsure(aggJ("claude:1", "codex", LabelAbsent))},
			passes: []pass{
				{nil, []string{"c:claude:1 [claude: claude:1] check=numbersAbsent"}},
				{[]string{CheckUnsure}, []string{"c:claude:1 [claude: claude:1] check=unsure"}},
			},
		},
		{
			// A sure same of the same unit does not help: the absent's
			// number check still counts.
			name:    "P2 exception beside a sure same",
			answers: append(slices.Clone(coffee), aggAns("opencode", "커피는 하루 3잔이면 충분합니다.")),
			judgments: []Judgment{
				aggJ("claude:1", "opencode", LabelSame, "opencode:1"),
				aggUnsure(aggJ("claude:1", "codex", LabelAbsent)),
			},
			passes: []pass{{nil, []string{"c:claude:1 [claude: claude:1] check=numbersAbsent"}}},
		},
		{
			// P3: an unsure opposite marks its 다른 말 item as before; an
			// unsure same on a unit of an item does not.
			name:    "P3: only an unsure opposite marks 다른 말",
			answers: duo,
			judgments: []Judgment{
				aggJ("claude:1", "codex", LabelOpposite, "codex:1"),
				aggUnsure(aggJ("codex:1", "claude", LabelSame, "claude:3")),
				aggUnsure(aggJ("claude:2", "codex", LabelOpposite, "codex:2")),
			},
			passes: []pass{
				{nil, []string{"d:claude:1~codex:1 [claude: claude:1] <> [codex: codex:1]", "d:claude:2~codex:2 [claude: claude:2] <> [codex: codex:2] check=unsure"}},
				{[]string{CheckUnsure}, []string{"d:claude:1~codex:1 [claude: claude:1] <> [codex: codex:1]", "d:claude:2~codex:2 [claude: claude:2] <> [codex: codex:2] check=unsure"}},
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, p := range c.passes {
				checks := p.checks
				if checks == nil {
					checks = DefaultChecks
				}
				in := AggregateInput{Answers: c.answers, Requests: aggRequests(c.answers, RequestDone), Judgments: c.judgments, Checks: checks, Ended: true}
				if got := aggDump(aggRun(t, in)); !slices.Equal(got, p.want) {
					t.Errorf("checks %v:\n got %q\nwant %q", checks, got, p.want)
				}
			}
		})
	}
	if !slices.Equal(DefaultChecks, []string{CheckUnsure, CheckNumberMismatch, CheckNumbersAbsent}) {
		t.Errorf("DefaultChecks = %v", DefaultChecks)
	}
}

func TestAggregateFlags(t *testing.T) {
	answers := []Answer{
		aggAns("claude", "서울 인구는 약 970만 명 안팎입니다.", "커피는 하루 3잔이면 충분합니다.", "Plain words.", "Other words.", "More words."),
		aggAns("codex", "서울 인구는 약 956만 명입니다.", "커피는 하루 5잔까지 마셔도 됩니다.", "Plain too."),
	}
	in := AggregateInput{
		Answers:  answers,
		Requests: aggRequests(answers, RequestDone),
		Judgments: []Judgment{
			aggJ("claude:1", "codex", LabelSame, "codex:1"),
			aggJ("claude:2", "codex", LabelAbsent),
			aggJ("claude:3", "codex", LabelSame, "codex:3"),
			aggJ("codex:3", "claude", LabelOpposite, "claude:3"),
			aggJ("claude:4", "codex", LabelSame),
			aggUnsure(aggJ("claude:5", "codex", LabelOpposite)),
		},
		Ended: true,
	}
	s := aggRun(t, in)
	want := map[string][]string{
		"claude:1": {CheckNumberMismatch},
		"claude:2": {CheckNumbersAbsent},
		"claude:3": {CheckCrossDirection},
		"codex:3":  {CheckCrossDirection},
		"claude:4": {CheckNoEvidence},
		"claude:5": {CheckUnsure, CheckNoEvidence},
	}
	if len(s.Judgments) != len(want) {
		t.Fatalf("%d judgments", len(s.Judgments))
	}
	for _, j := range s.Judgments {
		if !slices.Equal(j.Flags, want[j.Unit]) {
			t.Errorf("%s flags = %q, want %q", j.Unit, j.Flags, want[j.Unit])
		}
	}
	// Flags are computed whether or not their checks are enabled; none of
	// these is, so nothing goes to 확인 필요.
	if s.Counts.Check != 0 {
		t.Errorf("check items: %q", aggDump(s))
	}
}

func TestAggregateCleansEvidence(t *testing.T) {
	answers := []Answer{aggAns("claude", "A."), aggAns("codex", "B.", "C.", "D.", "E.")}
	in := AggregateInput{
		Answers:  answers,
		Requests: aggRequests(answers, RequestDone),
		Judgments: []Judgment{
			aggJ("claude:1", "codex", LabelSame, "claude:1", "codex:2", "codex:9", "codex:2", "codex:4", "codex:1", "codex:3"),
		},
		Ended: true,
	}
	before := slices.Clone(in.Judgments[0].Evidence)
	s := aggRun(t, in)
	if got := s.Judgments[0].Evidence; !slices.Equal(got, []string{"codex:2", "codex:4", "codex:1"}) {
		t.Errorf("evidence = %q", got)
	}
	if !slices.Equal(in.Judgments[0].Evidence, before) {
		t.Errorf("the input changed: %q", in.Judgments[0].Evidence)
	}
	// The first evidence unit is the one that joins 같은 말.
	if got := aggDump(s); !slices.Equal(got, []string{"a:claude:1 [claude codex: claude:1 codex:2]"}) {
		t.Errorf("items %q", got)
	}
}

// aggQ12Evidence is evidence for the P0-6 Q12 pairs, picked by hand from
// each pair's reason: the claims of the other AI the reason quotes, best
// first.
var aggQ12Evidence = map[string][]string{
	"12-claude1-codex":    {"codex:1"},              // codex: 최신은 "아이폰 듀오"
	"12-claude1-opencode": {"opencode:1"},           // opencode: 최신은 "아이폰 듀오"
	"12-claude2-codex":    {"codex:2"},              // codex: 듀오도 "9월 9일 공개"
	"12-claude2-opencode": {"opencode:3"},           // opencode: 18 프로 "2026년 9월 9일 공개"
	"12-claude3-codex":    {"codex:2"},              // codex: "10월 23일 출시"
	"12-claude3-opencode": {"opencode:3"},           // opencode: 18 프로 "9월 18일 출시"
	"12-claude4-opencode": {"opencode:4"},           // opencode: 일반형 아이폰 18 라인업에서 빠짐
	"12-codex1-claude":    {"claude:1"},             // claude: 최신은 iPhone 18 Pro
	"12-codex1-opencode":  {"opencode:1"},           // opencode: 최신은 아이폰 듀오
	"12-codex2-claude":    {"claude:3"},             // claude: 출시 "2026년 9월 18일"
	"12-codex2-opencode":  {"opencode:2"},           // opencode: "9월 9일 공개", "10월 23일 출시"
	"12-opencode1-claude": {"claude:1"},             // claude: 최신은 iPhone 18 Pro
	"12-opencode1-codex":  {"codex:1"},              // codex: 최근 출시는 아이폰 듀오
	"12-opencode2-claude": {"claude:3"},             // claude: 최신 출시 "9월 18일"
	"12-opencode2-codex":  {"codex:2"},              // codex: "9월 9일 공개", "10월 23일 출시"
	"12-opencode3-claude": {"claude:3", "claude:2"}, // claude: "9월 18일" 출시, "9월 9일" 발표
	"12-opencode4-claude": {"claude:4"},             // claude: 일반 iPhone 18은 이번 가을에 나오지 않음
}

// aggP06Input is question q of the P0-6 set as one comparison: each gold
// claim is a unit, and the gold label of each pair is the judgment of the
// third AI, core, with the evidence given (none when nil).
func aggP06Input(t testing.TB, q int, evidence map[string][]string) AggregateInput {
	t.Helper()
	in := AggregateInput{Checks: DefaultChecks, Ended: true}
	for _, ai := range p06AIs {
		a := Answer{ID: ai, Agent: ai, Via: ViaPC, Status: AnswerReady, Units: []Unit{}}
		for _, c := range p06FindClaims(t, q, ai) {
			a.Units = append(a.Units, Unit{ID: fmt.Sprintf("%s:%d", ai, c.N), N: c.N, Kind: UnitSentence, Text: c.Text, Judged: true})
		}
		in.Answers = append(in.Answers, a)
	}
	third := func(x, y string) string {
		for _, ai := range p06AIs {
			if ai != x && ai != y {
				return ai
			}
		}
		return ""
	}
	request := map[[2]string]string{}
	for _, x := range p06AIs {
		for _, y := range p06AIs {
			if x != y {
				id := fmt.Sprintf("r%d", len(in.Requests)+1)
				request[[2]string{x, y}] = id
				in.Requests = append(in.Requests, Request{ID: id, From: x, Against: y, Judge: third(x, y), Status: RequestDone, Attempt: 1})
			}
		}
	}
	for _, p := range p06Pairs(t) {
		if p.Q != q {
			continue
		}
		ev := evidence[p.ID]
		if ev == nil {
			ev = []string{}
		}
		in.Judgments = append(in.Judgments, Judgment{
			Unit: fmt.Sprintf("%s:%d", p.From, p.N), Against: p.Against, Request: request[[2]string{p.From, p.Against}],
			Judge: third(p.From, p.Against), Core: true, Label: p.Label, Evidence: ev, Flags: []string{},
		})
	}
	return in
}

// aggGoldChecks are the checks the gold replay counts its cards with: the
// unsure check alone. The gold labels are never unsure, so the cards come
// from the labels only. The number checks of DefaultChecks would fire on
// gold sames and absents that carry no evidence (TestAggregateGoldReplay).
var aggGoldChecks = []string{CheckUnsure}

func TestAggregateGoldReplay(t *testing.T) {
	// Claim level (C2-9): each claim's state, with no evidence. judge-data
	// derives the same numbers: any opposite is 다른 말, else any same 같은
	// 말, else 한 AI만.
	total := map[string]int{}
	byGroup := map[string]map[string]int{}
	var counts Counts
	withDefaults := map[string]int{}
	for _, q := range p06Questions(t) {
		in := aggP06Input(t, q.ID, nil)
		for _, u := range aggRun(t, in).Units {
			withDefaults[u.State]++
		}
		in.Checks = aggGoldChecks
		s := aggRun(t, in)
		if byGroup[q.Group] == nil {
			byGroup[q.Group] = map[string]int{}
		}
		for _, u := range s.Units {
			total[u.State]++
			byGroup[q.Group][u.State]++
		}
		counts.Differ += s.Counts.Differ
		counts.Check += s.Counts.Check
		counts.Agree += s.Counts.Agree
		counts.OnlyOne += s.Counts.OnlyOne
	}
	if want := map[string]int{CardDiffer: 19, CardAgree: 273, CardOnlyOne: 15}; !reflect.DeepEqual(total, want) {
		t.Errorf("claims = %v, want %v", total, want)
	}
	// With the default checks (S6) the number checks, without evidence
	// against the whole other answer, send 23 gold claims to 확인 필요: 20 of
	// 같은 말 and 3 of 한 AI만. 다른 말 stays 19.
	if want := map[string]int{CardDiffer: 19, CardCheck: 23, CardAgree: 253, CardOnlyOne: 12}; !reflect.DeepEqual(withDefaults, want) {
		t.Errorf("claims with the default checks = %v, want %v", withDefaults, want)
	}
	// Without evidence each claim is an item of its own.
	if want := (Counts{Differ: 19, Agree: 273, OnlyOne: 15}); counts != want {
		t.Errorf("items = %+v, want %+v", counts, want)
	}
	for g, want := range map[string][3]int{"A": {59, 4, 2}, "B": {34, 8, 1}, "C": {63, 6, 2}, "D": {41, 0, 7}, "E": {31, 0, 1}, "F": {45, 1, 2}} {
		got := byGroup[g]
		if got[CardAgree] != want[0] || got[CardDiffer] != want[1] || got[CardOnlyOne] != want[2] {
			t.Errorf("group %s: %v, want agree %d, differ %d, onlyOne %d", g, got, want[0], want[1], want[2])
		}
	}

	// While requests run only 다른 말 is made: the same 19 claims, the rest wait.
	waiting := map[string]int{}
	for _, q := range p06Questions(t) {
		in := aggP06Input(t, q.ID, nil)
		in.Checks = aggGoldChecks
		in.Ended = false
		for _, u := range aggRun(t, in).Units {
			waiting[u.State]++
		}
	}
	if want := map[string]int{CardDiffer: 19, StateWaiting: 288}; !reflect.DeepEqual(waiting, want) {
		t.Errorf("before the end: %v, want %v", waiting, want)
	}
}

func TestAggregateQ12Sides(t *testing.T) {
	// P0-6 Q12 with hand-picked evidence (C2-5). OpenCode's "18 프로 9월 18일
	// 출시" (opencode:3) says the same as Claude's release date, and Claude's
	// "발표 9월 9일" (claude:2) the same as Codex's; neither joins, as each AI
	// already stands on the other side.
	s := aggRun(t, aggP06Input(t, 12, aggQ12Evidence))
	want := []string{
		"d:claude:1~codex:1 [claude: claude:1] <> [codex opencode: codex:1 opencode:1]",
		"d:claude:3~codex:2 [claude: claude:3] <> [codex opencode: codex:2 opencode:2] numbersDiffer",
		"a:claude:2 [claude codex opencode: claude:2 opencode:3]",
		"a:claude:4 [claude opencode: claude:4 opencode:4]",
	}
	if got := aggDump(s); !slices.Equal(got, want) {
		t.Errorf("items:\n got %q\nwant %q", got, want)
	}
}

func TestAggregateNumbers(t *testing.T) {
	t.Run("numberMismatch adds 확인 필요, on by default", func(t *testing.T) {
		answers := []Answer{aggAns("claude", "서울 인구는 약 970만 명 안팎입니다."), aggAns("codex", "서울 인구는 약 956만 명입니다.")}
		in := AggregateInput{
			Answers:  answers,
			Requests: aggRequests(answers, RequestDone),
			Judgments: []Judgment{
				aggJ("claude:1", "codex", LabelSame, "codex:1"),
				aggJ("codex:1", "claude", LabelSame, "claude:1"),
			},
			Checks: []string{CheckUnsure},
			Ended:  true,
		}
		if got := aggDump(aggRun(t, in)); !slices.Equal(got, []string{"a:claude:1 [claude codex: claude:1 codex:1]"}) {
			t.Errorf("unsure only: %q", got)
		}
		want := []string{"c:claude:1 [claude: claude:1] check=numberMismatch", "c:codex:1 [codex: codex:1] check=numberMismatch"}
		for _, checks := range [][]string{{CheckUnsure, CheckNumberMismatch}, DefaultChecks} {
			in.Checks = checks
			if got := aggDump(aggRun(t, in)); !slices.Equal(got, want) {
				t.Errorf("checks %v: %q, want %q", checks, got, want)
			}
		}
	})
	t.Run("numbersAbsent adds 확인 필요, on by default", func(t *testing.T) {
		answers := []Answer{aggAns("claude", "커피는 하루 3잔이면 충분합니다."), aggAns("codex", "Plain.", "커피는 하루 5잔까지 마셔도 됩니다.")}
		in := AggregateInput{
			Answers:   answers,
			Requests:  aggRequests(answers, RequestDone),
			Judgments: []Judgment{aggJ("claude:1", "codex", LabelAbsent)},
			Checks:    []string{CheckUnsure},
			Ended:     true,
		}
		if got := aggDump(aggRun(t, in)); !slices.Equal(got, []string{"o:claude:1 [claude: claude:1]"}) {
			t.Errorf("unsure only: %q", got)
		}
		in.Checks = DefaultChecks
		if got := aggDump(aggRun(t, in)); !slices.Equal(got, []string{"c:claude:1 [claude: claude:1] check=numbersAbsent"}) {
			t.Errorf("default checks: %q", got)
		}
		in.Checks = AllChecks
		if got := aggDump(aggRun(t, in)); !slices.Equal(got, []string{"c:claude:1 [claude: claude:1] check=numbersAbsent"}) {
			t.Errorf("all checks: %q", got)
		}
	})
	t.Run("sides with numbers that differ", func(t *testing.T) {
		answers := []Answer{
			aggAns("claude", "🍎 출시일은 2026년 9월 18일입니다."),
			aggAns("codex", "출시일은 2026년 10월 23일입니다.", "가격은 199만 원입니다."),
		}
		in := AggregateInput{
			Answers:   answers,
			Requests:  aggRequests(answers, RequestDone),
			Judgments: []Judgment{aggJ("claude:1", "codex", LabelOpposite, "codex:1")},
			Ended:     true,
		}
		s := aggRun(t, in)
		if len(s.Items) != 1 || !s.Items[0].NumbersDiffer {
			t.Fatalf("items %q", aggDump(s))
		}
		// "2026년 9월 18일" after an emoji, which is two UTF-16 units.
		if got := s.Items[0].Positions[0].NumberSpans; !reflect.DeepEqual(got, []NumberSpan{{Unit: "claude:1", Start: 8, End: 20}}) {
			t.Errorf("claude's spans = %+v", got)
		}
		if got := s.Items[0].Positions[1].NumberSpans; !reflect.DeepEqual(got, []NumberSpan{{Unit: "codex:1", Start: 5, End: 18}}) {
			t.Errorf("codex's spans = %+v", got)
		}
		// Against the whole answer, its numbers count but have no unit to
		// draw them in.
		in.Judgments = []Judgment{aggJ("claude:1", "codex", LabelOpposite)}
		s = aggRun(t, in)
		if got := aggDump(s); !slices.Equal(got, []string{"d:claude:1~codex [claude: claude:1] <> [codex: *] numbersDiffer"}) {
			t.Errorf("whole answer: %q", got)
		}
		if got := s.Items[0].Positions[0].NumberSpans; len(got) != 1 || got[0].Start != 8 {
			t.Errorf("whole answer: claude's spans = %+v", got)
		}
	})
	t.Run("a partial disagreement draws only the numbers that differ", func(t *testing.T) {
		// P0-6 Q2: the aphelion agrees, so the sides' numbers do not
		// Mismatch, but the perihelion values fit nothing on the other side.
		answers := []Answer{
			aggAns("claude", "근일점(1월경)에는 약 8분 10초, 원일점(7월경)에는 약 8분 27초 정도입니다."),
			aggAns("codex", "지구 공전 궤도가 타원이어서 실제로는 약 8분 3초(근일점) ~ 8분 27초(원일점) 사이에서 달라집니다."),
		}
		in := AggregateInput{
			Answers:   answers,
			Requests:  aggRequests(answers, RequestDone),
			Judgments: []Judgment{aggJ("claude:1", "codex", LabelOpposite, "codex:1"), aggJ("codex:1", "claude", LabelOpposite, "claude:1")},
			Ended:     true,
		}
		s := aggRun(t, in)
		if len(s.Items) != 1 || s.Items[0].NumbersDiffer {
			t.Fatalf("items %q", aggDump(s))
		}
		if got := s.Items[0].Positions[0].NumberSpans; !reflect.DeepEqual(got, []NumberSpan{{Unit: "claude:1", Start: 11, End: 19}}) {
			t.Errorf("claude's spans = %+v (약 8분 10초)", got)
		}
		if got := s.Items[0].Positions[1].NumberSpans; !reflect.DeepEqual(got, []NumberSpan{{Unit: "codex:1", Start: 21, End: 28}}) {
			t.Errorf("codex's spans = %+v (약 8분 3초)", got)
		}
	})
	t.Run("sides whose numbers fit", func(t *testing.T) {
		answers := []Answer{
			aggAns("claude", "2026년 10월 30일 기준으로 가장 최신 아이폰은 iPhone 18 Pro입니다."),
			aggAns("codex", "2026년 10월 30일 기준 가장 최근에 출시된 아이폰은 아이폰 듀오입니다."),
		}
		in := AggregateInput{
			Answers:   answers,
			Requests:  aggRequests(answers, RequestDone),
			Judgments: []Judgment{aggJ("claude:1", "codex", LabelOpposite, "codex:1")},
			Ended:     true,
		}
		s := aggRun(t, in)
		if len(s.Items) != 1 || s.Items[0].NumbersDiffer || s.Items[0].Positions[0].NumberSpans != nil || s.Items[0].Positions[1].NumberSpans != nil {
			t.Errorf("items %q, %+v", aggDump(s), s.Items)
		}
	})
}

func TestAggregateFailures(t *testing.T) {
	answers := aggPlain(3, "claude", "codex", "opencode")
	failed := &Error{Code: "judge_failed", Message: "You've hit your usage limit"}
	in := AggregateInput{
		Answers: answers,
		Requests: []Request{
			{ID: "r1", From: "claude", Against: "codex", Judge: "opencode", Status: RequestDone},
			{ID: "r2", From: "codex", Against: "claude", Judge: "opencode", Status: RequestDone},
			{ID: "r3", From: "claude", Against: "opencode", Judge: "codex", Status: RequestFailed, Error: failed},
			{ID: "r4", From: "opencode", Against: "claude", Judge: "codex", Status: RequestFailed, Error: failed},
			{ID: "r5", From: "codex", Against: "opencode", Judge: "claude", Status: RequestDone},
			{ID: "r6", From: "opencode", Against: "codex", Judge: "claude", Status: RequestFailed, Error: failed},
		},
		Judgments: []Judgment{
			aggJ("claude:1", "codex", LabelAbsent),
			aggJ("claude:2", "codex", LabelSame, "codex:2"),
			aggJ("codex:1", "claude", LabelAbsent),
			aggJ("codex:1", "opencode", LabelAbsent),
			aggJ("codex:2", "claude", LabelSame, "claude:2"),
			aggJ("codex:2", "opencode", LabelAbsent),
		},
		Checks: AllChecks, // even with every check on, a failure is never 확인 필요
		Ended:  true,
	}
	s := aggRun(t, in)
	want := []string{
		"a:claude:2 [claude codex: claude:2 codex:2] missing=opencode",
		"o:claude:1 [claude: claude:1] missing=opencode",
		"o:codex:1 [codex: codex:1]",
	}
	if got := aggDump(s); !slices.Equal(got, want) {
		t.Errorf("items:\n got %q\nwant %q", got, want)
	}
	states := aggStates(s)
	for _, u := range []string{"claude:3", "codex:3", "opencode:1", "opencode:2", "opencode:3"} {
		if states[u] != StateNotCompared {
			t.Errorf("%s = %q, want notCompared", u, states[u])
		}
	}
	if s.Counts.NotCompared != 5 || s.Counts.Check != 0 {
		t.Errorf("counts = %+v", s.Counts)
	}

	// A pair with no judge (a no_judge record) counts as failed.
	duo := aggPlain(1, "claude", "codex")
	in = AggregateInput{
		Answers: duo,
		Requests: []Request{
			{ID: "r1", From: "claude", Against: "codex", Status: RequestFailed, Error: &Error{Code: "no_judge"}},
			{ID: "r2", From: "codex", Against: "claude", Status: RequestFailed, Error: &Error{Code: "no_judge"}},
		},
		Checks: DefaultChecks,
		Ended:  true,
	}
	s = aggRun(t, in)
	if len(s.Items) != 0 || s.Counts.NotCompared != 2 {
		t.Errorf("no judge: %q %+v", aggDump(s), s.Counts)
	}

	// A missing answer is not compared and not named as missing.
	trio := aggPlain(1, "claude", "codex", "opencode")
	trio[2].Status, trio[2].Units = AnswerMissing, []Unit{}
	in = AggregateInput{
		Answers:   trio,
		Requests:  aggRequests(trio[:2], RequestDone),
		Judgments: []Judgment{aggJ("claude:1", "codex", LabelAbsent), aggJ("claude:1", "opencode", LabelSame)},
		Checks:    DefaultChecks,
		Ended:     true,
	}
	if got := aggDump(aggRun(t, in)); !slices.Equal(got, []string{"o:claude:1 [claude: claude:1]"}) {
		t.Errorf("missing answer: %q", got)
	}
}

func TestAggregateCap(t *testing.T) {
	answers := aggPlain(4, "claude", "codex")
	answers[0].Units[2].Judged = false
	answers[0].Units[3].Judged = false
	answers[0].NotJudged = 2
	in := AggregateInput{
		Answers:  answers,
		Requests: aggRequests(answers, RequestDone),
		Judgments: []Judgment{
			aggJ("claude:1", "codex", LabelAbsent),
			aggJ("codex:1", "claude", LabelOpposite, "claude:4"), // a unit past the cap is still evidence
			aggJ("claude:3", "codex", LabelSame, "codex:2"),      // never asked: left out
		},
		Checks: DefaultChecks,
		Ended:  true,
	}
	s := aggRun(t, in)
	want := []string{"d:codex:1~claude:4 [claude: claude:4] <> [codex: codex:1]", "o:claude:1 [claude: claude:1]"}
	if got := aggDump(s); !slices.Equal(got, want) {
		t.Errorf("items:\n got %q\nwant %q", got, want)
	}
	states := aggStates(s)
	if states["claude:3"] != StateNotJudged || states["claude:2"] != StateNotCompared {
		t.Errorf("states %v", states)
	}
	if s.Counts.NotJudged != 1 || len(s.Judgments) != 2 {
		t.Errorf("counts %+v, %d judgments", s.Counts, len(s.Judgments))
	}
}

func TestAggregateBeforeTheEnd(t *testing.T) {
	answers := aggPlain(3, "claude", "codex", "opencode")
	requests := aggRequests(answers, RequestQueued)
	requests[0].Status = RequestDone    // claude against codex
	requests[1].Status = RequestRunning // claude against opencode
	in := AggregateInput{
		Answers:  answers,
		Requests: requests,
		Judgments: []Judgment{
			aggJ("claude:1", "codex", LabelOpposite, "codex:1"),
			aggJ("claude:2", "codex", LabelSame, "codex:2"),
			aggJ("claude:3", "codex", LabelAbsent),
		},
		Checks: DefaultChecks,
	}
	s := aggRun(t, in)
	if got := aggDump(s); !slices.Equal(got, []string{"d:claude:1~codex:1 [claude: claude:1] <> [codex: codex:1]"}) {
		t.Errorf("items %q", got)
	}
	for u, st := range aggStates(s) {
		if u != "claude:1" && u != "codex:1" && st != StateWaiting {
			t.Errorf("%s = %q, want waiting", u, st)
		}
	}
	// A request that failed already is missing at once.
	requests[1].Status = RequestFailed
	if got := aggDump(aggRun(t, in)); !slices.Equal(got, []string{"d:claude:1~codex:1 [claude: claude:1] <> [codex: codex:1] missing=opencode"}) {
		t.Errorf("after a failure: %q", got)
	}
	// Once every request has ended the other cards come, and the 다른 말
	// item stays as it was.
	for i := range requests {
		requests[i].Status = RequestDone
	}
	in.Ended = true
	want := []string{
		"d:claude:1~codex:1 [claude: claude:1] <> [codex: codex:1] missing=opencode",
		"a:claude:2 [claude codex: claude:2 codex:2] missing=opencode",
		"o:claude:3 [claude: claude:3] missing=opencode",
	}
	if got := aggDump(aggRun(t, in)); !slices.Equal(got, want) {
		t.Errorf("at the end:\n got %q\nwant %q", got, want)
	}
}

func TestAggregateKeys(t *testing.T) {
	answers := aggPlain(3, "claude", "codex", "opencode")
	// While the requests run, judgment after judgment.
	step := func(prev []Item, js ...Judgment) Summary {
		t.Helper()
		return aggRun(t, AggregateInput{Answers: answers, Requests: aggRequests(answers, RequestRunning), Judgments: js,
			Checks: DefaultChecks, Prev: prev})
	}
	j1 := aggJ("opencode:2", "claude", LabelOpposite, "claude:2")
	j2 := aggJ("claude:1", "codex", LabelOpposite, "codex:1")
	j3 := aggJ("codex:1", "opencode", LabelOpposite, "opencode:2")

	s1 := step(nil, j1)
	s2 := step(s1.Items, j1, j2)
	want := []string{
		"d:claude:1~codex:1 [claude: claude:1] <> [codex: codex:1]",
		"d:opencode:2~claude:2 [claude: claude:2] <> [opencode: opencode:2]",
	}
	if got := aggDump(s2); !slices.Equal(got, want) {
		t.Errorf("two items:\n got %q\nwant %q", got, want)
	}
	// j3 joins the two groups: the item keeps the older key, the one that
	// came first, although the other item comes first on the card.
	s3 := step(s2.Items, j1, j2, j3)
	want = []string{"d:opencode:2~claude:2 [claude: claude:1 claude:2] <> [codex: codex:1] <> [opencode: opencode:2]"}
	if got := aggDump(s3); !slices.Equal(got, want) {
		t.Errorf("merged:\n got %q\nwant %q", got, want)
	}
	// Without the earlier items the key is the same: the first edge decides.
	if got := aggDump(step(nil, j1, j2, j3)); !slices.Equal(got, want) {
		t.Errorf("merged without prev: %q", got)
	}

	// The first opposite judgment names its group even after evidence the
	// other way took its edge to the whole answer away: merged with a
	// younger item, the group keeps the older key.
	k1 := aggJ("claude:1", "codex", LabelOpposite)
	k2 := aggJ("codex:2", "opencode", LabelOpposite, "opencode:1")
	k3 := aggJ("codex:2", "claude", LabelOpposite, "claude:1")
	t1 := step(nil, k1)
	t2 := step(t1.Items, k1, k2)
	if got := aggDump(t2); !slices.Equal(got, []string{"d:claude:1~codex [claude: claude:1] <> [codex: *]", "d:codex:2~opencode:1 [codex: codex:2] <> [opencode: opencode:1]"}) {
		t.Fatalf("two items: %q", got)
	}
	want = []string{"d:claude:1~codex [claude: claude:1] <> [codex: codex:2] <> [opencode: opencode:1]"}
	if got := aggDump(step(t2.Items, k1, k2, k3)); !slices.Equal(got, want) {
		t.Errorf("merged after the whole answer's edge went:\n got %q\nwant %q", got, want)
	}

	// A representative that changes keeps the item's key: after a retry,
	// claude:1 comes before codex:1, which stood for the item. While the
	// retry runs only 다른 말 is made, so the caller passes the last items
	// and those of the last call with Ended (AggregateInput.Prev).
	duo := aggPlain(1, "claude", "codex")
	requests := aggRequests(duo, RequestDone)
	requests[0].Status = RequestFailed
	first := []Judgment{aggJ("codex:1", "claude", LabelSame, "claude:1")}
	before := aggRun(t, AggregateInput{Answers: duo, Requests: requests, Checks: DefaultChecks, Ended: true, Judgments: first})
	if got := aggDump(before); !slices.Equal(got, []string{"a:codex:1 [claude codex: codex:1 claude:1]"}) {
		t.Fatalf("before the retry: %q", got)
	}
	requests[0].Status = RequestRunning
	during := aggRun(t, AggregateInput{Answers: duo, Requests: requests, Checks: DefaultChecks, Judgments: first, Prev: before.Items})
	if len(during.Items) != 0 {
		t.Fatalf("while the retry runs: %q", aggDump(during))
	}
	requests[0].Status = RequestDone
	after := AggregateInput{Answers: duo, Requests: requests, Checks: DefaultChecks, Ended: true,
		Judgments: append(slices.Clone(first), aggJ("claude:1", "codex", LabelSame, "codex:1"))}
	if got := aggDump(Aggregate(after)); !slices.Equal(got, []string{"a:claude:1 [claude codex: claude:1 codex:1]"}) {
		t.Errorf("after the retry, no prev: %q", got)
	}
	after.Prev = append(slices.Clone(during.Items), before.Items...)
	if got := aggDump(aggRun(t, after)); !slices.Equal(got, []string{"a:codex:1 [claude codex: claude:1 codex:1]"}) {
		t.Errorf("after the retry: %q", got)
	}
	// A key of another card is not taken over.
	after.Prev = []Item{{Key: "o:codex:1", Kind: CardOnlyOne, Positions: []Position{{Agents: []string{"codex"}, Units: []string{"codex:1"}}}}}
	if got := aggDump(aggRun(t, after)); !slices.Equal(got, []string{"a:claude:1 [claude codex: claude:1 codex:1]"}) {
		t.Errorf("another card's key: %q", got)
	}
}

// aggRandom is a random comparison of three answers with three units each:
// opposites with and without evidence, sames with evidence, absents.
func aggRandom(rng *rand.Rand, ids ...string) ([]Answer, []Judgment) {
	answers := aggPlain(3, ids...)
	var js []Judgment
	for range 3 + rng.IntN(8) {
		x := rng.IntN(len(ids))
		y := (x + 1 + rng.IntN(len(ids)-1)) % len(ids)
		u, other := fmt.Sprintf("%s:%d", ids[x], 1+rng.IntN(3)), func() string { return fmt.Sprintf("%s:%d", ids[y], 1+rng.IntN(3)) }
		switch rng.IntN(5) {
		case 0, 1:
			var ev []string
			for range rng.IntN(3) {
				ev = append(ev, other())
			}
			js = append(js, aggJ(u, ids[y], LabelOpposite, ev...))
		case 2, 3:
			js = append(js, aggJ(u, ids[y], LabelSame, other()))
		default:
			js = append(js, aggJ(u, ids[y], LabelAbsent))
		}
	}
	return answers, js
}

// aggShare reports whether two items have a unit in common.
func aggShare(p, q Item) bool {
	for _, s := range p.Positions {
		for _, u := range s.Units {
			for _, s2 := range q.Positions {
				if slices.Contains(s2.Units, u) {
					return true
				}
			}
		}
	}
	return false
}

// Whatever order judgments come in, one at a time with the last items as
// Prev, a 다른 말 item that takes in earlier items keeps the oldest of
// their keys.
func TestAggregateKeysAnyOrder(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 7))
	merges := 0
	for c := range 300 {
		answers, js := aggRandom(rng, "claude", "codex", "opencode")
		for range 10 {
			order := rng.Perm(len(js))
			var prev []Item
			born := map[string]int{} // key → the step it first came at
			for n := range order {
				var seen []Judgment
				for _, k := range order[:n+1] {
					seen = append(seen, js[k])
				}
				s := Aggregate(AggregateInput{Answers: answers, Requests: aggRequests(answers, RequestRunning), Judgments: seen, Checks: DefaultChecks, Prev: prev})
				for _, it := range s.Items {
					oldest, shared := "", 0
					for _, p := range prev {
						if aggShare(p, it) {
							shared++
							if oldest == "" || born[p.Key] < born[oldest] {
								oldest = p.Key
							}
						}
					}
					if shared > 1 {
						merges++
					}
					if oldest != "" && it.Key != oldest {
						t.Fatalf("comparison %d, order %v, step %d: item %q, the oldest key it took in is %q", c, order, n, aggDump(Summary{Items: []Item{it}}), oldest)
					}
					if _, ok := born[it.Key]; !ok {
						born[it.Key] = n
					}
				}
				prev = s.Items
			}
		}
	}
	if merges < 100 {
		t.Errorf("only %d merges", merges)
	}
}

// A unit on a 다른 말 item while requests run is on one at the end,
// whatever order the requests end in: a unit outside the opposite groups
// joins a side only once every request has ended.
func TestAggregateDifferStays(t *testing.T) {
	trio := aggPlain(1, "claude", "codex", "opencode")
	js := []Judgment{aggJ("claude:1", "codex", LabelOpposite, "codex:1"), aggJ("opencode:1", "claude", LabelSame, "claude:1")}
	running := aggRun(t, AggregateInput{Answers: trio, Requests: aggRequests(trio, RequestRunning), Judgments: js, Checks: DefaultChecks})
	if got := aggDump(running); !slices.Equal(got, []string{"d:claude:1~codex:1 [claude: claude:1] <> [codex: codex:1]"}) {
		t.Errorf("while requests run: %q", got)
	}
	// opencode:1 says the same as both sides: it joins neither.
	end := aggRun(t, AggregateInput{Answers: trio, Requests: aggRequests(trio, RequestDone), Checks: DefaultChecks, Ended: true,
		Judgments: append(slices.Clone(js), aggJ("opencode:1", "codex", LabelSame, "codex:1")), Prev: running.Items})
	if got := aggDump(end); !slices.Equal(got, []string{"d:claude:1~codex:1 [claude: claude:1] <> [codex: codex:1] missing=opencode", "a:opencode:1 [claude codex opencode: opencode:1]"}) {
		t.Errorf("at the end: %q", got)
	}

	// P0-6 Q12 with its evidence, the six requests ending in every order.
	in := aggP06Input(t, 12, aggQ12Evidence)
	orders := 0
	for order := range aggPermutations(len(in.Requests)) {
		orders++
		var prev []Item
		differ := map[string]bool{}
		for n := range order {
			step := in
			step.Requests = slices.Clone(in.Requests)
			step.Judgments = nil
			for i := range step.Requests {
				step.Requests[i].Status = RequestRunning
			}
			for _, k := range order[:n+1] {
				step.Requests[k].Status = RequestDone
				for _, j := range in.Judgments {
					if j.Request == in.Requests[k].ID {
						step.Judgments = append(step.Judgments, j)
					}
				}
			}
			step.Ended, step.Prev = n == len(order)-1, prev
			s := Aggregate(step)
			states := aggStates(s)
			for u := range differ {
				if !strings.HasPrefix(states[u], CardDiffer) {
					t.Fatalf("order %v, step %d: %s left 다른 말: %s", order, n, u, states[u])
				}
			}
			for u, st := range states {
				if strings.HasPrefix(st, CardDiffer) {
					differ[u] = true
				}
			}
			prev = s.Items
		}
	}
	if orders != 720 {
		t.Errorf("%d orders", orders)
	}
}

// aggPermutations yields every order of n items.
func aggPermutations(n int) func(yield func([]int) bool) {
	return func(yield func([]int) bool) {
		p := make([]int, n)
		for i := range p {
			p[i] = i
		}
		var gen func(k int) bool
		gen = func(k int) bool {
			if k == n {
				return yield(slices.Clone(p))
			}
			for i := k; i < n; i++ {
				p[k], p[i] = p[i], p[k]
				if !gen(k + 1) {
					return false
				}
				p[k], p[i] = p[i], p[k]
			}
			return true
		}
		gen(0)
	}
}

// With four answers, two same links can each join a side but not both: the
// one in reading order wins, whichever request ended first.
func TestAggregateSidesAnyOrder(t *testing.T) {
	four := aggPlain(1, "claude", "codex", "opencode", "gemini")
	base := []Judgment{
		aggJ("claude:1", "codex", LabelOpposite, "codex:1"),
		aggJ("codex:1", "opencode", LabelOpposite, "opencode:1"),
		aggJ("opencode:1", "gemini", LabelOpposite, "gemini:1"),
	}
	links := []Judgment{aggJ("claude:1", "gemini", LabelSame, "gemini:1"), aggJ("claude:1", "opencode", LabelSame, "opencode:1")}
	want := []string{"d:claude:1~codex:1 [claude opencode: claude:1 opencode:1] <> [codex: codex:1] <> [gemini: gemini:1]"}
	for _, js := range [][]Judgment{append(slices.Clone(base), links...), append(slices.Clone(base), links[1], links[0])} {
		in := AggregateInput{Answers: four, Requests: aggRequests(four, RequestDone), Judgments: js, Checks: DefaultChecks, Ended: true}
		if got := aggDump(aggRun(t, in)); !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	}

	// Random comparisons of four answers: the cards, keys aside, are the
	// same in any order.
	rng := rand.New(rand.NewPCG(5, 11))
	noKeys := func(s Summary) string {
		var lines []string
		for _, l := range aggDump(s) {
			lines = append(lines, l[strings.IndexByte(l, ' '):])
		}
		return strings.Join(lines, "\n")
	}
	for c := range 300 {
		answers, js := aggRandom(rng, "claude", "codex", "opencode", "gemini")
		in := AggregateInput{Answers: answers, Requests: aggRequests(answers, RequestDone), Judgments: js, Checks: DefaultChecks, Ended: true}
		first := noKeys(Aggregate(in))
		for range 20 {
			shuffled := slices.Clone(js)
			rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
			in.Judgments = shuffled
			if got := noKeys(Aggregate(in)); got != first {
				t.Fatalf("comparison %d: the cards depend on the order:\n%s\nagainst\n%s", c, got, first)
			}
		}
	}
}

func TestAggregateIsPure(t *testing.T) {
	in := aggP06Input(t, 12, aggQ12Evidence)
	in.Checks = AllChecks
	raw, _ := json.Marshal(in)
	s1 := Aggregate(in)
	s2 := Aggregate(in)
	if !reflect.DeepEqual(s1, s2) {
		t.Errorf("two calls differ")
	}
	if after, _ := json.Marshal(in); string(after) != string(raw) {
		t.Errorf("the input changed")
	}
}

func TestAggregateJSON(t *testing.T) {
	// Arrays the phone reads are [] when empty, never null.
	answers := aggPlain(1, "claude", "codex")
	s := aggRun(t, AggregateInput{Answers: answers, Requests: aggRequests(answers, RequestDone), Ended: true,
		Judgments: []Judgment{{Unit: "claude:1", Against: "codex", Judge: "judge", Core: true, Label: LabelOpposite}}})
	raw, err := json.Marshal(struct {
		J []Judgment `json:"judgments"`
		I []Item     `json:"items"`
	}{s.Judgments, s.Items})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"judgments":[{"unit":"claude:1","against":"codex","request":"","judge":"judge","core":true,"label":"opposite","evidence":[],"unsure":false,"flags":["noEvidence"]}],` +
		`"items":[{"key":"d:claude:1~codex","kind":"differ","positions":[{"agents":["claude"],"units":["claude:1"]},{"agents":["codex"],"units":[]}],"check":[],"missing":[]}]}`
	if string(raw) != want {
		t.Errorf("JSON\n got %s\nwant %s", raw, want)
	}
	empty := Aggregate(AggregateInput{})
	if raw, _ := json.Marshal(empty.Items); string(raw) != "[]" || empty.Judgments == nil || empty.Units == nil {
		t.Errorf("empty: %+v", empty)
	}
}

// aggFuzzInput makes a comparison from fuzz bytes: two to four answers of
// up to four units (some past the cap, some with numbers), requests in any
// status, and judgments that may be on unknown units, by the author, with
// odd labels or with evidence of the wrong answer.
func aggFuzzInput(data []byte) AggregateInput {
	next := func() int {
		if len(data) == 0 {
			return 0
		}
		b := data[0]
		data = data[1:]
		return int(b)
	}
	names := []string{"claude", "codex", "opencode", "gemini"}
	texts := []string{
		"서울 인구는 약 970만 명 안팎입니다.", "약 956만 명입니다.", "출시일은 2026년 9월 18일입니다.", "10월 23일 출시됐습니다.",
		"커피는 하루 3잔이면 충분합니다.", "🍎 임신 중에는 200mg 미만입니다.", "It takes about 8 minutes 20 seconds.", "No numbers here.", "", "v1.99.1",
	}
	var in AggregateInput
	n := 2 + next()%3
	units := make([][]string, n)
	var all []string
	for i := range n {
		a := Answer{ID: names[i], Agent: names[i], Via: ViaPC, Status: AnswerReady, Units: []Unit{}}
		if next()%8 == 7 {
			a.Status = AnswerMissing
		}
		for k := range next() % 5 {
			u := Unit{ID: fmt.Sprintf("%s:%d", names[i], k+1), N: k + 1, Kind: UnitSentence, Text: texts[next()%len(texts)], Judged: next()%6 != 0}
			a.Units = append(a.Units, u)
			units[i] = append(units[i], u.ID)
			all = append(all, u.ID)
		}
		in.Answers = append(in.Answers, a)
	}
	statuses := []string{RequestQueued, RequestRunning, RequestDone, RequestFailed, RequestDone}
	for x := range n {
		for y := range n {
			if x != y && next()%6 != 0 {
				in.Requests = append(in.Requests, Request{ID: fmt.Sprintf("r%d", len(in.Requests)+1), From: names[x], Against: names[y], Judge: names[(x+y+1)%n], Status: statuses[next()%len(statuses)]})
			}
		}
	}
	pick := func(from []string) string {
		if len(from) == 0 || next()%11 == 10 {
			return "nobody:1"
		}
		return from[next()%len(from)]
	}
	labels := []string{LabelSame, LabelOpposite, LabelAbsent, LabelSame, LabelAbsent, LabelOpposite, LabelUnclear, "maybe"}
	judges := []string{"judge", "judge", "judge", "claude", "codex"}
	for range next() % 32 {
		y := next() % n
		j := Judgment{Unit: pick(all), Against: names[y], Request: "r", Judge: judges[next()%len(judges)], Label: labels[next()%len(labels)]}
		b := next()
		j.Core, j.Unsure = b&1 != 0, b&2 != 0
		for range next() % 5 {
			if next()%4 == 0 {
				j.Evidence = append(j.Evidence, pick(all))
			} else {
				j.Evidence = append(j.Evidence, pick(units[y]))
			}
		}
		in.Judgments = append(in.Judgments, j)
	}
	b := next()
	in.Ended = b&1 != 0
	for i, c := range AllChecks {
		if b&(2<<i) != 0 {
			in.Checks = append(in.Checks, c)
		}
	}
	return in
}

func FuzzAggregate(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 0, 3, 2, 1, 3, 1, 4, 1, 3, 2, 0, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 12, 0, 0, 0, 1, 3, 1, 0, 1, 1, 0, 2, 0, 1, 1, 0, 0, 5, 255})
	f.Add([]byte("an odd comparison with many judgments in it, all in one line of fuzz input bytes"))
	f.Fuzz(func(t *testing.T, data []byte) {
		in := aggFuzzInput(data)
		aggRun(t, in)
		// Half the judgments came first. Each 다른 말 item of then stays on
		// 다른 말 (its representative does), and keeps its key unless it has
		// taken in another of those items.
		half := len(in.Judgments) / 2
		if half == 0 {
			return
		}
		early := in
		early.Judgments, early.Ended = in.Judgments[:half], false
		earlyItems := Aggregate(early).Items
		rep := map[string]string{} // representative → key
		for _, it := range earlyItems {
			rep[it.Positions[0].Units[0]] = it.Key
		}
		later := in
		later.Prev = earlyItems
		found := 0
		for _, it := range aggRun(t, later).Items {
			var keys []string
			for _, p := range it.Positions {
				for _, u := range p.Units {
					if k, ok := rep[u]; ok {
						keys = append(keys, k)
					}
				}
			}
			found += len(keys)
			if len(keys) > 0 && it.Kind != CardDiffer {
				t.Errorf("item %s of kind %s holds the 다른 말 items %q", it.Key, it.Kind, keys)
			}
			if len(keys) == 1 && it.Key != keys[0] {
				t.Errorf("item %s was %s", it.Key, keys[0])
			}
		}
		if found != len(rep) {
			t.Errorf("%d of %d 다른 말 items are left", found, len(rep))
		}
	})
}

// Answers that are lists of numbers which never fit the other answers' (a
// unit about 55 of them, an answer about 6,000) stay quick: every judged
// unit is absent against a whole answer, and Claude's opposites without
// evidence make items whose sides are two whole answers. Compared pair by
// pair this took tens of seconds.
func TestAggregateDenseNumbers(t *testing.T) {
	names := []string{"claude", "codex", "opencode"}
	var answers []Answer
	quantities := 0
	for x, id := range names {
		var texts []string
		for i, size := 0, 0; size < MaxAnswerBytes; i++ {
			var b strings.Builder
			for k := 0; b.Len() < 590; k++ {
				fmt.Fprintf(&b, "%d분 ", 1000+x*100000+i*200+k)
			}
			texts = append(texts, b.String())
			size += b.Len()
			quantities += len(Numbers(b.String()))
		}
		a := aggAns(id, texts...)
		for k := range a.Units {
			a.Units[k].Judged = k < MaxUnits
		}
		answers = append(answers, a)
	}
	var js []Judgment
	for x, a := range answers {
		for y, b := range answers {
			for k := range MaxUnits {
				switch {
				case x == y:
				case x == 0:
					js = append(js, aggJ(a.Units[k].ID, b.ID, LabelOpposite))
				default:
					js = append(js, aggJ(a.Units[k].ID, b.ID, LabelAbsent))
				}
			}
		}
	}
	in := AggregateInput{Answers: answers, Requests: aggRequests(answers, RequestDone), Judgments: js, Checks: AllChecks, Ended: true}
	best := time.Duration(1 << 62)
	var s Summary
	for range 3 { // the best of three, as other packages may be testing at once
		start := time.Now()
		s = Aggregate(in)
		if best = min(best, time.Since(start)); best < time.Second {
			break
		}
	}
	aggCheck(t, in, s)
	t.Logf("%d quantities, %d judgments: %v, %d items", quantities, len(js), best, len(s.Items))
	if best > time.Second {
		t.Errorf("took %v", best)
	}
	if s.Counts.Differ != MaxUnits || s.Counts.Check != 2*MaxUnits || !s.Items[0].NumbersDiffer {
		t.Errorf("counts %+v; first item %q", s.Counts, aggDump(Summary{Items: s.Items[:1]}))
	}
}

func TestAggregateLarge(t *testing.T) {
	// Six answers of MaxAnswerBytes, every unit full of numbers, 40 judged
	// units each against every other answer: the most a comparison can
	// bring, as numbersAbsent and numberMismatch without evidence compare a
	// unit with a whole answer.
	names := []string{"a", "b", "c", "d", "e", "f"}
	var answers []Answer
	quantities := 0
	for _, id := range names {
		var texts []string
		for size, i := 0, 0; size < MaxAnswerBytes; i++ {
			s := fmt.Sprintf("값은 %d만 %d천 명, %d.%d%%, 2026년 %d월 %d일, 약 %d분 %d초입니다.", i, i%10, i%100, i%10, 1+i%12, 1+i%28, i%60, i%60)
			texts = append(texts, s)
			size += len(s) + 1
			quantities += len(Numbers(s))
		}
		a := aggAns(id, texts...)
		for k := range a.Units {
			a.Units[k].Judged = k < MaxUnits
		}
		answers = append(answers, a)
	}
	labels := []string{LabelAbsent, LabelSame, LabelAbsent, LabelOpposite, LabelSame}
	var js []Judgment
	for x, a := range answers {
		for y, b := range answers {
			if x == y {
				continue
			}
			for k := range MaxUnits {
				var ev []string
				if k%3 != 0 { // a third without evidence
					ev = []string{b.Units[(k*7)%len(b.Units)].ID}
				}
				js = append(js, aggJ(a.Units[k].ID, b.ID, labels[(k+x+y)%len(labels)], ev...))
			}
		}
	}
	in := AggregateInput{Answers: answers, Requests: aggRequests(answers, RequestDone), Judgments: js, Checks: AllChecks, Ended: true}
	best := time.Duration(1 << 62)
	var s Summary
	for range 3 {
		start := time.Now()
		s = Aggregate(in)
		best = min(best, time.Since(start))
	}
	aggCheck(t, in, s)
	t.Logf("%d units, %d quantities, %d judgments: best of 3 %v, %d items", len(s.Units), quantities, len(js), best, len(s.Items))
	if best > 2*time.Second {
		t.Errorf("took %v", best)
	}
}
