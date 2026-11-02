//go:build judgeeval

package main

import (
	"slices"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Hand-made runs of the mini set for what the hand run of metrics_test.go
// cannot show (S4 review COR-6, COR-7, COR-9): claims of more than one
// unit, a differ item that does not hold the gold pair's other answer,
// evidence cited after a wrong unit, and the check sweep.

// handView loads run name and makes engine claude's view with checks.
func handView(t *testing.T, data, out, name string, checks []string, units map[string][]string) (*universe, *viewResult) {
	t.Helper()
	u, err := loadUniverse(out, data, []string{name})
	if err != nil {
		t.Fatal(err)
	}
	// U(c) is fit.go's; here some claims are made to span two units, as 15
	// claims of P0-6 do.
	for _, cl := range u.comps[0].claims {
		if us, ok := units[cl.id()]; ok {
			cl.units = us
		}
	}
	return u, u.view(u.views()[0], checks)
}

func pairOf(t *testing.T, v *viewResult, id string) *pairOut {
	t.Helper()
	for _, po := range v.pairs {
		if po.p.id == id {
			return po
		}
	}
	t.Fatalf("no pair %s", id)
	return nil
}

func claimOf(t *testing.T, v *viewResult, id string) *claimOut {
	t.Helper()
	for _, co := range v.claims {
		if co.c.id() == id {
			return co
		}
	}
	t.Fatalf("no claim %s", id)
	return nil
}

// COR-6, COR-7: claude2 is made to span claude:1 (judged same against
// codex) and claude:2 (opposite, citing the gold codex:2); opencode1 spans
// opencode:1 (same against claude) and opencode:2 (unclear).
func TestMultiUnitClaims(t *testing.T) {
	data, out := writeMini(t, nil), t.TempDir()
	writeRun(t, data, out, "multi", []handRow{
		{"claude", "codex", nil, []v{hSame("a1", "b1"), hOpposite("a2", "b2")}},
		{"codex", "claude", nil, []v{hSame("a1", "b1"), hAbsent("a2", true)}},
		{"claude", "opencode", nil, []v{hSame("a1", "b1"), hAbsent("a2", true)}},
		{"opencode", "claude", nil, []v{hSame("a1", "b1"), hUnclear("a2")}},
		{"codex", "opencode", nil, []v{hSame("a1", "b1"), hAbsent("a2", true)}},
		{"opencode", "codex", nil, []v{hSame("a1", "b1"), hAbsent("a2", true)}},
	})
	u, v := handView(t, data, out, "multi", handChecks, map[string][]string{
		claimKey(1, "claude", 2):   {"claude:1", "claude:2"},
		claimKey(1, "opencode", 1): {"opencode:1", "opencode:2"},
	})
	// claude:1 agrees, claude:2 differs: the claim takes its best state.
	if co := claimOf(t, v, "1-claude2"); co.state != judge.CardDiffer {
		t.Errorf("1-claude2 %s, want differ (claude:1 agree, claude:2 differ)", co.state)
	}
	// Against codex: same on claude:1, opposite on claude:2: opposite; the
	// differ item has claude:2 on one side and codex on the other: strict.
	// Its evidence cites the gold codex:2 first: hit@3 and top-1.
	po := pairOf(t, v, "1-claude2-codex")
	if po.pred != judge.LabelOpposite || po.state != judge.CardDiffer || !po.strict || !po.evOK || !po.hit3 || !po.top1 {
		t.Errorf("1-claude2-codex: pred %s state %s strict %v evidence %v %v %v", po.pred, po.state, po.strict, po.evOK, po.hit3, po.top1)
	}
	// Against claude: same on opencode:1, unclear on opencode:2: unclear
	// ranks above same. opencode:2 is on check (unsure), above agree.
	if po := pairOf(t, v, "1-opencode1-claude"); po.pred != judge.LabelUnclear || po.state != judge.CardCheck {
		t.Errorf("1-opencode1-claude: pred %s state %s, want unclear check", po.pred, po.state)
	}
	vr := makeViewReport(u, v, u.coverageOf(), 0, 1)
	// codex2 against claude is differ too (the same item), strict.
	if n := vr.Natural; n.Differ != 2 || n.Strict != 2 || n.Missed != 0 {
		t.Errorf("natural: differ %d strict %d missed %d", n.Differ, n.Strict, n.Missed)
	}
	if e := vr.Evidence; e.Eligible != 1 || e.Hit3 != 1 || e.Top1 != 1 {
		t.Errorf("evidence %+v", e)
	}
}

// COR-6, COR-7: codex2's gold opposite is against claude, but its unit is
// on a differ item against opencode only: differ, not strict. opencode1
// cites claude:2 before the gold claude:1: hit@3, not top-1.
func TestStrictNeedsTheOtherAnswer(t *testing.T) {
	data, out := writeMini(t, nil), t.TempDir()
	writeRun(t, data, out, "strict", []handRow{
		{"claude", "codex", nil, []v{hSame("a1", "b1"), hAbsent("a2", true)}},
		{"codex", "claude", nil, []v{hSame("a1", "b1"), hAbsent("a2", true)}},
		{"claude", "opencode", nil, []v{hSame("a1", "b1"), hAbsent("a2", true)}},
		{"opencode", "claude", nil, []v{hSame("a1", "b2", "b1"), hAbsent("a2", true)}},
		{"codex", "opencode", nil, []v{hSame("a1", "b1"), hOpposite("a2", "b2")}},
		{"opencode", "codex", nil, []v{hSame("a1", "b1"), hAbsent("a2", true)}},
	})
	u, v := handView(t, data, out, "strict", handChecks, nil)
	if po := pairOf(t, v, "1-codex2-claude"); po.state != judge.CardDiffer || po.strict {
		t.Errorf("1-codex2-claude: state %s strict %v, want differ, not strict", po.state, po.strict)
	}
	if po := pairOf(t, v, "1-opencode1-claude"); po.pred != judge.LabelSame || !po.evOK || !po.hit3 || po.top1 {
		t.Errorf("1-opencode1-claude: pred %s evidence %v hit@3 %v top-1 %v", po.pred, po.evOK, po.hit3, po.top1)
	}
	vr := makeViewReport(u, v, u.coverageOf(), 0, 1)
	// claude2 against codex: absent in both directions, onlyOne: missed.
	if n := vr.Natural; n.Differ != 1 || n.Strict != 0 || n.Missed != 1 || !slices.Equal(n.MissedIDs, []string{"1-claude2-codex"}) {
		t.Errorf("natural: differ %d strict %d missed %d %v", n.Differ, n.Strict, n.Missed, n.MissedIDs)
	}
	if e := vr.Evidence; e.Eligible != 1 || e.Hit3 != 1 || e.Top1 != 0 || !near(e.Hit3Rate, 1) || e.Top1Rate != 0 {
		t.Errorf("evidence %+v", e)
	}
}

// COR-9: -sweep-checks adds each other check alone. The run is the hand
// run with codex2 core against claude, so that a check can show it: with
// unsure only it is onlyOne, missed. The mini set writes its areas with
// the one glyph ㎢, which judge.Numbers reads since S6R-1 (the test once
// rewrote them as km² to get around that).
func TestSweepChecksByHand(t *testing.T) {
	data, out := writeMini(t, nil), t.TempDir()
	unsure := hSame("a2", "b2")
	unsure.Unsure = true
	writeRun(t, data, out, "sweep", []handRow{
		{"claude", "codex", nil, []v{hSame("a1"), unsure}},
		{"codex", "claude", nil, []v{hSame("a1"), hAbsent("a2", true)}},
		{"claude", "opencode", nil, []v{hSame("a1"), hAbsent("a2", false)}},
		{"opencode", "claude", nil, []v{hSame("a1"), hAbsent("a2", false)}},
		{"codex", "opencode", nil, []v{hSame("a1"), hAbsent("a2", false)}},
		{"opencode", "codex", nil, []v{hOpposite("a1", "b1"), hAbsent("a2", false)}},
	})
	u, v := handView(t, data, out, "sweep", handChecks, nil)
	if co := claimOf(t, v, "1-codex2"); co.state != judge.CardOnlyOne {
		t.Fatalf("1-codex2 %s, want onlyOne", co.state)
	}
	// Dev is question 1; its clear claims are claude1 (agree) and opencode1
	// (differ), neither on check.
	//   - noEvidence: claude:1's same judgments cite nothing, so claude1
	//     goes to check: clear check share 0 → 1/2, +50 points. codex:2 is
	//     absent: no flag.
	//   - crossDirection: no same is answered by an opposite citing back.
	//   - numberMismatch: only claude:2 (605㎢ against 700㎢), already on
	//     check; 940만 명 fits everywhere.
	//   - numbersAbsent: codex:2 (700㎢) is absent against claude, whose
	//     answer has 605㎢: codex2 goes to check, 1-codex2-claude is
	//     shown. claude:2 against opencode: opencode has no area.
	want := []sweepRow{
		{View: "claude", Check: judge.CheckNoEvidence, NewlyShownIDs: []string{}, Share: 0.5, DeltaPoints: 50},
		{View: "claude", Check: judge.CheckCrossDirection, NewlyShownIDs: []string{}},
		{View: "claude", Check: judge.CheckNumberMismatch, NewlyShownIDs: []string{}},
		{View: "claude", Check: judge.CheckNumbersAbsent, NewlyShown: 1, NewlyShownIDs: []string{"1-codex2-claude"}},
	}
	got := sweep(u, u.views(), handChecks)
	if len(got) != len(want) {
		t.Fatalf("sweep %+v", got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.View != w.View || g.Check != w.Check || g.NewlyShown != w.NewlyShown || !slices.Equal(g.NewlyShownIDs, w.NewlyShownIDs) ||
			g.BaseShare != 0 || !near(g.Share, w.Share) || !near(g.DeltaPoints, w.DeltaPoints) {
			t.Errorf("sweep %s: %+v, want %+v", w.Check, g, w)
		}
	}
}
