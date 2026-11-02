//go:build judgeeval

package main

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// The gold-replay self check (plan part 5): the gold engine answers from
// the gold labels through the whole loop (BuildTask, Prompt, Validate,
// Judgments, Aggregate), so whatever the tool measures, the gold must come
// out perfect. A miss here is a bug of the tool, not of a judge.

// goldReport runs the gold engine with extra flags and reports it.
func goldReport(t *testing.T, name string, extra ...string) (*universe, []viewReport) {
	t.Helper()
	data, out := committedData(t), t.TempDir()
	args := append([]string{"run", "-split", "all", "-engines", "gold", "-run", name, "-data", data, "-out", out}, extra...)
	if code, stdout, stderr := cli(t, context.Background(), args...); code != exitOK {
		t.Fatalf("exit %d: %s %s", code, stdout, stderr)
	}
	u, err := loadUniverse(out, data, []string{name})
	if err != nil {
		t.Fatal(err)
	}
	cov := u.coverageOf()
	var reps []viewReport
	for _, vs := range u.views() {
		reps = append(reps, makeViewReport(u, u.view(vs, handChecks), cov, 100, 1))
	}
	if len(reps) != 2 || reps[0].Name != engineGold || reps[1].Name != "product" || reps[1].StandIn != engineGold {
		t.Fatalf("views %v", reps)
	}
	return u, reps
}

func TestGoldReplay(t *testing.T) {
	u, reps := goldReport(t, "gold-replay")
	if cov := u.coverageOf(); !cov.P06All || len(cov.Questions) != 30 {
		t.Fatalf("coverage %+v", cov)
	}
	for _, vr := range reps {
		n, c := vr.Natural, vr.Cards
		if n.Pairs != 614 || n.Opposites != 21 || n.Differ != 21 || n.Strict != 21 || n.Missed != 0 || n.Check != 0 {
			t.Errorf("%s: pairs %d, opposites %d: differ %d strict %d check %d missed %d", vr.Name, n.Pairs, n.Opposites, n.Differ, n.Strict, n.Check, n.Missed)
		}
		if n.MacroF1 != 1 || n.FalseOpposites != 0 {
			t.Errorf("%s: macro F1 %v, false opposites %d", vr.Name, n.MacroF1, n.FalseOpposites)
		}
		want := map[string]int{judge.CardDiffer: 19, judge.CardAgree: 273, judge.CardOnlyOne: 15}
		for k, w := range want {
			if c.States[k] != w || c.Confusion[k][k] != w {
				t.Errorf("%s: %s %d (confusion %v), want %d", vr.Name, k, c.States[k], c.Confusion[k], w)
			}
		}
		if c.Claims != 307 || c.CoreRecallOR != 1 || c.CoreRecallAND != 1 {
			t.Errorf("%s: claims %d, core %v/%v", vr.Name, c.Claims, c.CoreRecallOR, c.CoreRecallAND)
		}
		if vr.Requests.Requests != 180 || vr.Requests.Done != 180 || vr.MissingRequests != 0 {
			t.Errorf("%s: requests %+v, missing %d", vr.Name, vr.Requests, vr.MissingRequests)
		}
		if b := vr.Bootstrap["missedNatural"]; b.Hi != 0 {
			t.Errorf("%s: bootstrap missed %+v", vr.Name, b)
		}
	}
	// Engine view rules apply to real engines only.
	if len(reps[0].Rules) != 0 {
		t.Error("rules on the gold engine")
	}
	// With the default checks (S6) the gold still shows every opposite on
	// 다른 말, but the number checks, without evidence against the whole
	// other answer, send 25 gold claims to 확인 필요: 22 of 같은 말 and 3 of
	// 한 AI만 (the library's claim-unit replay has 23).
	cov := u.coverageOf()
	for _, vs := range u.views() {
		vr := makeViewReport(u, u.view(vs, judge.DefaultChecks), cov, 0, 1)
		n, c := vr.Natural, vr.Cards
		if n.Differ != 21 || n.Check != 0 || n.Missed != 0 {
			t.Errorf("%s, default checks: differ %d check %d missed %d", vr.Name, n.Differ, n.Check, n.Missed)
		}
		want := map[string]int{judge.CardDiffer: 19, judge.CardCheck: 25, judge.CardAgree: 251, judge.CardOnlyOne: 12}
		for k, w := range want {
			if c.States[k] != w {
				t.Errorf("%s, default checks: %s %d, want %d (%v)", vr.Name, k, c.States[k], w, c.States)
			}
		}
	}
}

func TestGoldEvidence(t *testing.T) {
	_, reps := goldReport(t, "gold-ev", "-gold-evidence")
	for _, vr := range reps {
		if vr.Natural.Missed != 0 || vr.Natural.Differ != 21 {
			t.Errorf("%s: missed %d, differ %d", vr.Name, vr.Natural.Missed, vr.Natural.Differ)
		}
		e := vr.Evidence
		if e.Eligible != 51 || e.Hit3 != 51 || e.Top1 != 51 || vr.EvidenceSyn.Eligible != 0 {
			t.Errorf("%s: evidence %+v", vr.Name, e)
		}
	}
}

func TestGoldPerturb(t *testing.T) {
	u, reps := goldReport(t, "gold-perturb", "-set", "perturb", "-gold-evidence")
	if cov := u.coverageOf(); !cov.Perturb || cov.Items != 60 {
		t.Fatalf("coverage %+v", cov)
	}
	for _, vr := range reps {
		s := vr.Synthetic
		if s.Opposites < 60 || s.Missed != 0 || s.Differ != s.Opposites || s.Strict != s.Opposites || vr.Natural.Pairs != 0 {
			t.Errorf("%s: synthetic %d: differ %d strict %d missed %d", vr.Name, s.Opposites, s.Differ, s.Strict, s.Missed)
		}
		if e := vr.EvidenceSyn; e.Eligible != s.Opposites || e.Hit3 != e.Eligible || vr.Evidence.Eligible != 0 {
			t.Errorf("%s: synthetic evidence %+v", vr.Name, e)
		}
		if vr.Requests.Requests != 60 {
			t.Errorf("%s: %d requests", vr.Name, vr.Requests.Requests)
		}
		// No P0-6 comparison: no card numbers. The breakdowns hold the
		// synthetic pairs only, not the items' other (kept) claims (COR-4).
		if c := vr.Cards; c.Claims != 0 || c.Items != 0 || c.CheckItems != 0 {
			t.Errorf("%s: cards of perturb items %+v", vr.Name, c)
		}
		if k, g := vr.Breakdowns["kind"], vr.Breakdowns["group"]; len(k) != 1 || k["synthetic"].Pairs != s.Pairs || len(g) != 1 || g[groupSynthetic].Pairs != s.Pairs {
			t.Errorf("%s: breakdowns kind %v group %v, want the %d synthetic pairs", vr.Name, keys(k), keys(g), s.Pairs)
		}
		if b := vr.Bootstrap["missedSynthetic"]; b.Point != 0 || b.Hi != 0 {
			t.Errorf("%s: bootstrap synthetic missed %+v", vr.Name, b)
		}
	}
	t.Logf("synthetic opposites: %d", reps[0].Synthetic.Opposites)
}

func keys[V any](m map[string]V) []string { return slices.Sorted(maps.Keys(m)) }

// handChecks are the checks the gold replay and the hand-made cases count
// their cards with: unsure alone, DefaultChecks before S6. The gold labels
// are never unsure, so the gold cards come from the labels only; the
// number checks of DefaultChecks would fire on gold sames and absents that
// carry no evidence (TestGoldReplay counts how many).
var handChecks = []string{judge.CheckUnsure}
