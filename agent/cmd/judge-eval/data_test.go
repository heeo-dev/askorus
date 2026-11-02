//go:build judgeeval

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// sameEvidenceIDs are the 30 same pairs with evidence gold (plan part 1):
// per question, the clear same pair whose id has the smallest SHA-256.
var sameEvidenceIDs = []string{
	"1-claude3-opencode", "2-claude2-opencode", "3-codex3-opencode", "4-opencode1-codex", "5-claude2-opencode",
	"6-codex4-claude", "7-claude2-opencode", "8-codex2-opencode", "9-claude1-codex", "10-claude1-codex",
	"11-codex1-claude", "12-codex2-opencode", "13-claude4-opencode", "14-claude4-opencode", "15-claude4-opencode",
	"16-codex2-opencode", "17-opencode1-claude", "18-claude1-opencode", "19-claude4-opencode", "20-opencode2-claude",
	"21-codex3-opencode", "22-opencode4-codex", "23-opencode2-codex", "24-opencode1-codex", "25-claude1-opencode",
	"26-opencode3-claude", "27-codex3-claude", "28-claude3-opencode", "29-codex4-claude", "30-opencode2-codex",
}

func TestCommittedData(t *testing.T) {
	d, err := loadData(committedData(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.questions) != 30 || len(d.answers) != 90 || len(d.claims) != 307 || len(d.pairs) != 614 {
		t.Fatalf("set %d/%d/%d/%d", len(d.questions), len(d.answers), len(d.claims), len(d.pairs))
	}
	if !slices.Equal(d.split.Dev, []int{1, 2, 4, 7, 10, 12, 13, 15, 17, 19, 21, 23, 25, 28, 30}) {
		t.Errorf("dev %v", d.split.Dev)
	}
	for name, want := range map[string]splitCounts{
		splitDev:  {Questions: 15, English: 4, Claims: 152, Pairs: 304, Opposite: 11, Hard: 8},
		splitTest: {Questions: 15, English: 4, Claims: 155, Pairs: 310, Opposite: 10, Hard: 10},
	} {
		if got := d.countSplit(d.questionsOf(name)); got != want {
			t.Errorf("%s: %+v, want %+v", name, got, want)
		}
	}
	// Groups: the rule recomputed here from the pairs, apart from the
	// loader's.
	var hard, boundary []string
	clear := 0
	for _, p := range d.pairs {
		if p.Hard {
			hard = append(hard, p.ID)
		}
		if p.Doubt || (p.Review && p.Label != "same") {
			boundary = append(boundary, p.ID)
		} else {
			clear++
			if p.Label == "opposite" || p.Hard {
				t.Errorf("clear pair %s is opposite or hard", p.ID)
			}
		}
	}
	if len(hard) != 18 || len(boundary) != 123 || clear != 491 {
		t.Errorf("groups %d/%d/%d", len(hard), len(boundary), clear)
	}
	if !slices.Equal(d.groups.Hard, hard) || !slices.Equal(d.groups.Boundary, boundary) {
		t.Error("eval-groups.json is not the rule's")
	}
	if c := *d.groups.Counts; c != (groupCounts{Hard: 18, Boundary: 123, Clear: 491}) {
		t.Errorf("counts %+v", c)
	}
	// Evidence: the 21 opposites, then the 30 same picks.
	if len(d.evidence.Entries) != 51 {
		t.Fatalf("%d evidence entries", len(d.evidence.Entries))
	}
	opp := 0
	var same []string
	for _, e := range d.evidence.Entries {
		switch e.Label {
		case judge.LabelOpposite:
			opp++
		case judge.LabelSame:
			same = append(same, e.ID)
		}
	}
	if opp != 21 || !slices.Equal(same, sameEvidenceIDs) || !slices.Equal(d.sameEvidencePicks(), sameEvidenceIDs) {
		t.Errorf("evidence: %d opposite, same %v", opp, same)
	}
	p := prepare(d, judge.MaxUnits)
	if s := p.evStats; s.None != 0 || len(s.Fails) != 0 || s.One+s.Run+s.Inside < 51 {
		t.Errorf("evidence mapping %+v", s)
	}
	for _, e := range d.evidence.Entries {
		if len(p.evUnits[e.ID]) == 0 {
			t.Errorf("%s maps to no unit", e.ID)
		}
	}
	if len(d.hashes) != 6 {
		t.Errorf("%d hashed files", len(d.hashes))
	}
}

func TestMiniData(t *testing.T) {
	d, err := loadData(writeMini(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if d.names["pairs"] != "pairs-mini.json" || d.names["answers"] != "answers-mini.json" {
		t.Errorf("names %v", d.names)
	}
	p := prepare(d, judge.MaxUnits)
	if p.fitStats.One != 4 || p.evStats.One != 3 || p.evStats.None != 0 {
		t.Errorf("fit %+v, evidence %+v", p.fitStats, p.evStats)
	}
	if got := p.evUnits["1-claude2-codex"]; !slices.Equal(got, []string{"codex:2"}) {
		t.Errorf("evidence units %v", got)
	}
}

func TestBadEvidenceIsRecorded(t *testing.T) {
	dir := writeMini(t, func(f map[string]map[string]any) {
		ev := f["eval-evidence.json"]["pairs"].([]any)
		ev[0].(map[string]any)["evidence"] = []any{"서울 면적은 700㎢입니다.", "이 문장은 답에 없습니다."}
	})
	d, err := loadData(dir)
	if err != nil {
		t.Fatalf("a sentence that maps nowhere is fatal: %v", err)
	}
	p := prepare(d, judge.MaxUnits)
	if p.evStats.None != 1 || !slices.Equal(p.evStats.Fails, []string{"1-claude2-codex#2"}) {
		t.Errorf("evidence %+v", p.evStats)
	}
	if got := p.evUnits["1-claude2-codex"]; !slices.Equal(got, []string{"codex:2"}) {
		t.Errorf("the entry that maps still counts: %v", got)
	}
}

func TestDataErrors(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(f map[string]map[string]any)
		want string
	}{
		{"split counts missing", func(f map[string]map[string]any) { delete(f["eval-split.json"], "counts") }, "no counts"},
		{"split count wrong", func(f map[string]map[string]any) {
			f["eval-split.json"]["counts"].(map[string]any)["dev"].(map[string]any)["pairs"] = 9
		}, "counts"},
		{"question in both halves", func(f map[string]map[string]any) { f["eval-split.json"]["test"] = []any{1} }, "twice"},
		{"question in neither half", func(f map[string]map[string]any) { f["eval-split.json"]["dev"] = []any{} }, "neither"},
		{"group counts missing", func(f map[string]map[string]any) { delete(f["eval-groups.json"], "counts") }, "no counts"},
		{"group count wrong", func(f map[string]map[string]any) {
			f["eval-groups.json"]["counts"].(map[string]any)["clear"] = 4
		}, "counts"},
		{"boundary not the rule", func(f map[string]map[string]any) {
			f["eval-groups.json"]["boundary"] = []any{"1-codex2-claude"}
		}, "boundary"},
		{"evidence label wrong", func(f map[string]map[string]any) {
			f["eval-evidence.json"]["pairs"].([]any)[0].(map[string]any)["label"] = "same"
		}, "label"},
		{"evidence of another same pair", func(f map[string]map[string]any) {
			f["eval-evidence.json"]["pairs"].([]any)[2].(map[string]any)["id"] = "1-claude1-codex"
			f["eval-evidence.json"]["pairs"].([]any)[2].(map[string]any)["label"] = "same"
		}, "entries"},
		{"four evidence sentences", func(f map[string]map[string]any) {
			f["eval-evidence.json"]["pairs"].([]any)[0].(map[string]any)["evidence"] = []any{"a", "b", "c", "d"}
		}, "1 to 3"},
		{"pairs file of another name", func(f map[string]map[string]any) { f["eval-groups.json"]["pairs"] = "pairs-other.json" }, "different"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := loadData(writeMini(t, c.edit))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err %v, want %q", err, c.want)
			}
		})
	}
}

func TestEvidenceFileKeys(t *testing.T) {
	// The plan's format names the pairs file and the entries both "pairs";
	// the loader reads either order, and "pairsFile".
	for _, raw := range []string{
		`{"set":"s","pairs":"p.json","answers":"a.json","pairs":[{"id":"x","label":"same","evidence":["e"]}]}`,
		`{"pairs":[{"id":"x","label":"same","evidence":["e"]}],"pairs":"p.json","answers":"a.json"}`,
		`{"pairsFile":"p.json","answers":"a.json","pairs":[{"id":"x","label":"same","evidence":["e"]}]}`,
	} {
		var e evidenceFile
		if err := e.UnmarshalJSON([]byte(raw)); err != nil || e.Pairs != "p.json" || e.Answers != "a.json" || len(e.Entries) != 1 || e.Entries[0].ID != "x" {
			t.Errorf("%s: %+v %v", raw, e, err)
		}
	}
}
