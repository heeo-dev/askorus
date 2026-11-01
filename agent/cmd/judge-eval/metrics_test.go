//go:build judgeeval

package main

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// The hand-made run (plan part 5): the mini set judged by engine claude
// in all six directions, with verdicts picked so that one opposite is
// missed (codex2 against claude), one shows on the check card (claude2
// against codex, unsure), one pair is a false opposite (opencode1 against
// codex) and one request's first output was invalid (r3). Every number
// below is worked out by hand in the comments.

type v = verdictRec

func att(ms int64, in, out int) attemptRec {
	return attemptRec{N: 1, Ms: ms, In: in, Out: out, Model: "haiku"}
}

// Verdicts of hand-made runs, core unless said otherwise.
func hSame(id string, ev ...string) v {
	return v{ID: id, Core: true, Evidence: append([]string{}, ev...), Label: judge.LabelSame, Reason: "r"}
}

func hOpposite(id string, ev ...string) v {
	return v{ID: id, Core: true, Evidence: append([]string{}, ev...), Label: judge.LabelOpposite, Reason: "r"}
}

func hAbsent(id string, core bool) v {
	return v{ID: id, Core: core, Evidence: []string{}, Label: judge.LabelAbsent, Reason: "r"}
}

func hUnclear(id string) v {
	return v{ID: id, Core: true, Evidence: []string{}, Label: judge.LabelUnclear, Reason: "r"}
}

// handRow is one direction of a hand-made run.
type handRow struct {
	from, against string
	attempts      []attemptRec
	verdicts      []v
}

// writeRun writes run name, engine claude over the mini set's question 1
// in data, into out.
func writeRun(t *testing.T, data, out, name string, rows []handRow) {
	t.Helper()
	d, err := loadData(data)
	if err != nil {
		t.Fatal(err)
	}
	p := prepare(d, judge.MaxUnits)
	dir := filepath.Join(out, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	m := manifest{Tool: "judge-eval", Format: manifestFormat, Run: name, Set: setP06, Split: splitDev, Questions: []int{1},
		Engines: []string{"claude"}, Tier: "quick", Shape: "directed", Labels: "v1", MaxUnits: judge.MaxUnits,
		EngineInfo: map[string]engineInfo{"claude": {Model: "haiku", Effort: "medium", Family: "anthropic", Version: "2.1.316"}},
		Data:       dataHashes{Files: d.fileHashes()}}
	if err := writeJSON(filepath.Join(dir, fileManifest), m); err != nil {
		t.Fatal(err)
	}
	st, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.close()
	for _, r := range rows {
		attempts := r.attempts
		if attempts == nil {
			attempts = []attemptRec{att(1000, 100, 10)}
		}
		task := judge.BuildTask(p.spec(1), judge.Request{From: r.from, Against: r.against})
		rec := record{Key: jobKey(setP06, 1, "", "claude", r.from, r.against), Set: setP06, Q: 1, Engine: "claude", From: r.from, Against: r.against,
			Model: "haiku", Effort: "medium", CLI: "2.1.316", Task: taskSHA(task), Status: statusDone, Attempts: attempts, Verdicts: r.verdicts, Missing: []string{}}
		if err := st.add(rec); err != nil {
			t.Fatal(err)
		}
	}
}

// handRun writes the run "hand" into out over the mini set in data.
// claude2 is core against codex only, so core OR and core AND differ
// (S4 review COR-5).
func handRun(t *testing.T, data, out string) {
	t.Helper()
	unsure := hSame("a2", "b2")
	unsure.Unsure = true
	invalid := att(500, 50, 5)
	invalid.Validate = "judge_bad_output"
	second := att(1500, 150, 15)
	second.N = 2
	writeRun(t, data, out, "hand", []handRow{
		{"claude", "codex", []attemptRec{att(1000, 100, 10)}, []v{hSame("a1"), unsure}},
		{"codex", "claude", []attemptRec{att(2000, 200, 20)}, []v{hSame("a1"), hAbsent("a2", false)}},
		{"claude", "opencode", []attemptRec{invalid, second}, []v{hSame("a1"), hAbsent("a2", false)}},
		{"opencode", "claude", []attemptRec{att(3000, 300, 30)}, []v{hSame("a1"), hAbsent("a2", false)}},
		{"codex", "opencode", []attemptRec{att(4000, 0, 0)}, []v{hSame("a1"), hAbsent("a2", false)}},
		{"opencode", "codex", []attemptRec{att(5000, 500, 50)}, []v{hOpposite("a1", "b1"), hAbsent("a2", false)}},
	})
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestMetricsByHand(t *testing.T) {
	data, out := writeMini(t, nil), t.TempDir()
	handRun(t, data, out)
	u, err := loadUniverse(out, data, []string{"hand"})
	if err != nil {
		t.Fatal(err)
	}
	cov := u.coverageOf()
	views := u.views()
	if len(views) != 1 || views[0].name != "claude" {
		t.Fatalf("views %+v: one engine, no product view", views)
	}
	view := u.view(views[0], handChecks)
	vr := makeViewReport(u, view, cov, 200, 20261101)
	n := vr.Natural

	// Pairs: same 3 of 4 right (opencode1-codex is opposite), the two
	// opposites predicted same (unsure) and absent, both absents right.
	wantConf := map[string]map[string]int{
		"same":     {"same": 3, "opposite": 1, "absent": 0, "unclear": 0, "none": 0},
		"opposite": {"same": 1, "opposite": 0, "absent": 1, "unclear": 0, "none": 0},
		"absent":   {"same": 0, "opposite": 0, "absent": 2, "unclear": 0, "none": 0},
	}
	if !reflect.DeepEqual(n.Confusion, wantConf) {
		t.Errorf("confusion %v", n.Confusion)
	}
	// F1: same P 3/4 R 3/4 = 0.75; opposite 0; absent P 2/3 R 1 = 0.8.
	if !near(n.MacroF1, (0.75+0+0.8)/3) || !near(n.Classes["absent"].F1, 0.8) || n.Classes["opposite"].Support != 2 {
		t.Errorf("macro F1 %v, classes %+v", n.MacroF1, n.Classes)
	}
	// The clear group (claude1 both, claude2-opencode, opencode1 both) has
	// no gold opposite, so its macro F1 is over same and absent only:
	// same P 3/3 R 3/4 = 6/7, absent 1 (COR-4).
	if cl := vr.Breakdowns["group"][groupClear]; !near(cl.MacroF1, (6.0/7+1)/2) || cl.Classes["opposite"].Support != 0 || cl.Pairs != 5 {
		t.Errorf("clear group macro F1 %v, classes %+v", cl.MacroF1, cl.Classes)
	}
	if n.FalseOpposites != 1 || n.NonOpposites != 6 || !near(n.FalseOppositeRate, 1.0/6) || !slices.Equal(n.FalseOppositeIDs, []string{"1-opencode1-codex"}) {
		t.Errorf("false opposites %d/%d %v", n.FalseOpposites, n.NonOpposites, n.FalseOppositeIDs)
	}
	if n.Opposites != 2 || n.OppositeLabel != 0 || n.OppositeOrUnsure != 1 || !near(n.OrUnsureRecall, 0.5) {
		t.Errorf("opposite recall %+v", n)
	}
	// Cards: claude2 is on check, codex2 is hidden: missed. claude2's same
	// is unsure and its numbers clash with its evidence (605㎢ against
	// 700㎢), so numberMismatch fires; with handChecks it is not enabled
	// and the unsure mark stands in for it (S6, P1). The split read no
	// number in "605㎢" before S6R-1, and claude2 then passed as 같은 말.
	if n.Differ != 0 || n.Check != 1 || n.Missed != 1 || n.Strict != 0 ||
		!slices.Equal(n.MissedIDs, []string{"1-codex2-claude"}) || !slices.Equal(n.CheckIDs, []string{"1-claude2-codex"}) {
		t.Errorf("shown: differ %d check %d missed %d strict %d %v %v", n.Differ, n.Check, n.Missed, n.Strict, n.MissedIDs, n.CheckIDs)
	}
	c := vr.Cards
	wantCard := map[string]map[string]int{
		judge.CardDiffer:  {judge.CardCheck: 1, judge.StateHidden: 1},
		judge.CardAgree:   {judge.CardAgree: 1, judge.CardDiffer: 1},
		judge.CardOnlyOne: {},
	}
	if c.Claims != 4 || !reflect.DeepEqual(c.Confusion, wantCard) {
		t.Errorf("card confusion %v", c.Confusion)
	}
	// Core: codex2 has no core judgment, claude2 is core against codex only
	// (OR, not AND), claude1 and opencode1 are core in both directions.
	if c.CoreOR != 3 || c.CoreAND != 2 || !near(c.CoreRecallOR, 0.75) || !near(c.CoreRecallAND, 0.5) {
		t.Errorf("core %d/%d", c.CoreOR, c.CoreAND)
	}
	// Items: differ opencode:1 against codex:1, check claude:2 (unsure
	// only, as numberMismatch is not enabled), agree claude:1.
	if c.Items != 3 || c.CheckItems != 1 || c.UnsureOnly != 1 || !near(c.CheckRate, 1.0/3) || !near(c.UnsureOnlyShare, 1.0/3) {
		t.Errorf("items %d, check %d, unsure only %d", c.Items, c.CheckItems, c.UnsureOnly)
	}
	// Units on a card: claude 2, codex 1 (codex:1 agrees), opencode 1:
	// median 1. Core in both directions or in an opposite: claude 1
	// (claude:2 is core against codex only), codex 1 (codex:1 is the
	// opposite's evidence), opencode 1.
	if c.VisibleOR != 1 || c.VisibleAND != 1 || len(view.comps) != 1 ||
		!slices.Equal(view.comps[0].visibleOR, []int{2, 1, 1}) || !slices.Equal(view.comps[0].visibleAND, []int{1, 1, 1}) {
		t.Errorf("visible %v/%v, per answer %v/%v", c.VisibleOR, c.VisibleAND, view.comps[0].visibleOR, view.comps[0].visibleAND)
	}
	// clear claims: claude1 (both pairs clear) and opencode1; neither on check.
	if c.ClearClaims != 2 || c.ClearCheck != 0 {
		t.Errorf("clear claims %d, on check %d", c.ClearClaims, c.ClearCheck)
	}

	// Requests: ms 1000, 2000, 2000, 3000, 4000, 5000; p50 is the 3rd,
	// p90 the 6th. Attempts 500, 1000, 1500, 2000, 3000, 4000, 5000.
	q := vr.Requests
	if q.Requests != 6 || q.FirstInvalid != 1 || !near(q.InvalidRate, 1.0/6) || q.Retries != 1 || q.Attempts != 7 {
		t.Errorf("requests %+v", q)
	}
	if q.MsP50 != 2000 || q.MsP90 != 5000 || q.AttemptP50 != 2000 || q.AttemptP90 != 5000 {
		t.Errorf("ms p50 %v p90 %v, attempts %v %v", q.MsP50, q.MsP90, q.AttemptP50, q.AttemptP90)
	}
	if q.InSum != 1300 || q.OutSum != 130 || q.InMedian != 200 || q.OutMedian != 20 || q.ZeroTokens != 1 {
		t.Errorf("tokens %d/%d medians %v/%v zero %d", q.InSum, q.OutSum, q.InMedian, q.OutMedian, q.ZeroTokens)
	}
	// Evidence: only opencode1-claude (same, predicted same) is eligible,
	// and its judgment cites nothing. Same or opposite judgments: 7, of
	// which 5 cite nothing.
	e := vr.Evidence
	if e.Eligible != 1 || e.Hit3 != 0 || e.SameOpposite != 7 || e.NoEvidence != 5 {
		t.Errorf("evidence %+v", e)
	}

	// Breakdowns: claude's company wrote six of the pairs' claims or
	// answers; the two others are codex2-opencode and opencode1-codex.
	own, others := vr.Breakdowns["own"]["own"], vr.Breakdowns["own"]["others"]
	if own.Pairs != 6 || own.Missed != 1 || own.Opposites != 2 || others.Pairs != 2 || others.FalseOpposites != 1 {
		t.Errorf("own %+v\nothers %+v", own, others)
	}
	if ko := vr.Breakdowns["lang"]["ko"]; ko.Pairs != 8 || len(vr.Breakdowns["lang"]) != 1 {
		t.Errorf("lang %+v", vr.Breakdowns["lang"])
	}
	if g := vr.Breakdowns["group"]; g["hard"].Pairs != 1 || g["boundary"].Pairs != 3 || g["clear"].Pairs != 5 {
		t.Errorf("groups %+v", g)
	}

	// Bootstrap: one question gives a point interval.
	for name, iv := range vr.Bootstrap {
		if iv.Lo != iv.Point || iv.Hi != iv.Point {
			t.Errorf("%s: %+v is no point interval", name, iv)
		}
	}
	if b := vr.Bootstrap["missedNatural"]; b.Point != 1 {
		t.Errorf("bootstrap missed %+v", b)
	}
	if b := vr.Bootstrap["macroF1"]; !near(b.Point, n.MacroF1) {
		t.Errorf("bootstrap F1 %+v", b)
	}
	// Clear pairs: five, all non-opposite, one false opposite; core OR 3/4.
	if b := vr.Bootstrap["clearFalseOppositeRate"]; !near(b.Point, 0.2) {
		t.Errorf("bootstrap clear false opposites %+v", b)
	}
	if b := vr.Bootstrap["coreRecallOr"]; !near(b.Point, 0.75) {
		t.Errorf("bootstrap core recall %+v", b)
	}
	// The mini set's one question is its whole set, so the rules apply
	// (1b needs a perturb run): only 1f (median 2 s) passes. Values (COR-9):
	// 1a shown = differ 0 + check 1; 1c 1/5 clear pairs; 1d 1/6 requests;
	// 1e core OR (AND is 0.5); 1f the median of the summed attempts (p90
	// is 5000); 1g the unsure-only check item of three items.
	want := map[string]struct {
		state string
		value float64
	}{"1a": {"fail", 1}, "1b": {"n/a", 0}, "1c": {"fail", 0.2}, "1d": {"fail", 1.0 / 6}, "1e": {"fail", 0.75}, "1f": {"pass", 2000}, "1g": {"fail", 1.0 / 3}}
	for _, r := range vr.Rules {
		got := "n/a"
		if r.Pass != nil {
			got = map[bool]string{true: "pass", false: "fail"}[*r.Pass]
		}
		if w := want[r.Rule]; got != w.state || !near(r.Value, w.value) {
			t.Errorf("rule %s: %s (%v), want %s (%v)", r.Rule, got, r.Value, w.state, w.value)
		}
	}
	if len(vr.Rules) != 7 {
		t.Errorf("rules %+v", vr.Rules)
	}

	// -checks makes the cards again without judging: with noEvidence the
	// agree item of claude:1 (no evidence) goes to check. Check items:
	// claude:1 (noEvidence) and claude:2 (unsure only) of three, so rule
	// 1g is 1/3 while the check rate is 2/3 (COR-4, COR-9).
	vr2 := makeViewReport(u, u.view(views[0], []string{judge.CheckUnsure, judge.CheckNoEvidence}), cov, 0, 1)
	c2 := vr2.Cards
	if c2.States[judge.CardCheck] != 2 || c2.States[judge.CardAgree] != 0 || c2.Items != 3 || c2.CheckItems != 2 || c2.UnsureOnly != 1 ||
		!near(c2.CheckRate, 2.0/3) || !near(c2.UnsureOnlyShare, 1.0/3) {
		t.Errorf("with noEvidence: %v, items %d, check %d, unsure only %d", c2.States, c2.Items, c2.CheckItems, c2.UnsureOnly)
	}
	if r := vr2.Rules[6]; r.Rule != "1g" || !near(r.Value, 1.0/3) {
		t.Errorf("with noEvidence: rule %s %v", r.Rule, r.Value)
	}
	// Clear claims: claude1 is on check now, opencode1 is not.
	if c2.ClearCheck != 1 || !near(c2.ClearCheckShare, 0.5) {
		t.Errorf("with noEvidence: clear on check %d", c2.ClearCheck)
	}

	// With the product's checks (S6R-1) claude2 is on check for
	// numberMismatch itself, so the item is no unsure-only one and 1g is 0.
	vr3 := makeViewReport(u, u.view(views[0], judge.DefaultChecks), cov, 0, 1)
	n3, c3 := vr3.Natural, vr3.Cards
	if n3.Check != 1 || n3.Missed != 1 || !slices.Equal(n3.CheckIDs, []string{"1-claude2-codex"}) || !slices.Equal(n3.MissedIDs, []string{"1-codex2-claude"}) ||
		c3.CheckItems != 1 || c3.UnsureOnly != 0 {
		t.Errorf("with DefaultChecks: check %v missed %v, check items %d, unsure only %d", n3.CheckIDs, n3.MissedIDs, c3.CheckItems, c3.UnsureOnly)
	}
	var reasons [][]string
	for _, it := range u.view(views[0], judge.DefaultChecks).comps[0].items {
		if it.Kind == judge.CardCheck {
			reasons = append(reasons, it.Check)
		}
	}
	if !reflect.DeepEqual(reasons, [][]string{{judge.CheckNumberMismatch}}) {
		t.Errorf("with DefaultChecks: check reasons %v, want [[numberMismatch]]", reasons)
	}
}

func TestBootstrapSeeded(t *testing.T) {
	d, err := loadData(committedData(t))
	if err != nil {
		t.Fatal(err)
	}
	// A view of every question with predictions that vary by question.
	u := &universe{p: prepare(d, judge.MaxUnits), info: map[string]engineInfo{}}
	for _, q := range d.questionsOf(splitAll) {
		u.comps = append(u.comps, u.p06Comp(q))
	}
	res := &viewResult{}
	for _, c := range u.comps {
		for _, cl := range c.claims {
			res.claims = append(res.claims, &claimOut{c: cl, coreOR: cl.n%2 == 0})
			for _, p := range cl.pairs {
				pred := p.gold
				if c.q%3 == 0 {
					pred = judge.LabelSame
				}
				res.pairs = append(res.pairs, &pairOut{p: p, pred: pred, state: map[bool]string{true: judge.CardDiffer, false: judge.CardAgree}[pred == judge.LabelOpposite]})
			}
		}
	}
	a, b := bootstrap(res, 500, 7), bootstrap(res, 500, 7)
	if !reflect.DeepEqual(a, b) {
		t.Error("the same seed gave other intervals")
	}
	if c := bootstrap(res, 500, 8); reflect.DeepEqual(a, c) {
		t.Error("another seed gave the same intervals")
	}
	if iv := a["missedNatural"]; iv.Lo > iv.Point || iv.Hi < iv.Point || iv.Lo == iv.Hi {
		t.Errorf("missed %+v", iv)
	}
}

func TestNearestRank(t *testing.T) {
	v := []float64{5, 1, 4, 2, 3}
	if nearestRank(v, 0.5) != 3 || nearestRank(v, 0.9) != 5 || nearestRank(v, 0.2) != 1 || nearestRank(nil, 0.5) != 0 {
		t.Error("nearest rank")
	}
}

// COR-4: invalid first output (rule 1d) is the adapter's judge_bad_output,
// Validate's refusal, or ids missing, on the first attempt only; a
// request's time is the sum of its attempts.
func TestRequestsByHand(t *testing.T) {
	a := func(ms int64, edit func(*attemptRec)) attemptRec {
		r := attemptRec{N: 1, Ms: ms, In: 10, Out: 1}
		if edit != nil {
			edit(&r)
		}
		return r
	}
	done := func(atts ...attemptRec) record {
		return record{Status: statusDone, Model: "m", CLI: "1", Attempts: atts}
	}
	failed := done(a(4000, func(r *attemptRec) { r.Code = core.ErrCodeJudgeTimeout }))
	failed.Status, failed.Code = statusFailed, core.ErrCodeJudgeTimeout
	recs := []record{
		done(a(100, func(r *attemptRec) { r.Missing = 2 }), a(900, nil)),                           // missed ids: invalid, 1000 ms
		done(a(300, func(r *attemptRec) { r.Code = core.ErrCodeJudgeBadOutput }), a(900, nil)),     // adapter: invalid, 1200 ms
		done(a(500, func(r *attemptRec) { r.Validate = core.ErrCodeJudgeBadOutput }), a(950, nil)), // Validate: invalid, 1450 ms
		done(a(1100, nil), a(1, func(r *attemptRec) { r.Missing = 1 })),                            // only the second missed ids
		failed, // a timeout is no invalid output
		{Status: statusFailed, Code: core.ErrCodeInternal},
	}
	q := requestsOf(recs)
	if q.Requests != 6 || q.Done != 4 || q.Failed != 2 || q.FirstInvalid != 3 || !near(q.InvalidRate, 0.5) || q.Retries != 4 {
		t.Errorf("requests %+v", q)
	}
	// Sums: 0, 1000, 1101, 1200, 1450, 4000. Last attempts only would be
	// 0, 1, 900, 900, 950, 4000.
	if q.MsP50 != 1101 || q.MsP90 != 4000 || q.Codes[core.ErrCodeJudgeTimeout] != 1 || q.Codes[core.ErrCodeInternal] != 1 {
		t.Errorf("ms p50 %v p90 %v, codes %v", q.MsP50, q.MsP90, q.Codes)
	}
}

// COR-8: what each question adds to the bootstrap's numbers, by hand.
func TestBootstrapByHand(t *testing.T) {
	q1, q2, q3 := &comp{set: setP06, q: 1}, &comp{set: setP06, q: 2}, &comp{set: setP06, q: 3}
	item := &comp{set: setPerturb, q: 1, item: &perturbItem{ID: "p1"}}
	pair := func(c *comp, gold, pred, state, group string) *pairOut {
		p := &ePair{id: gold + pred, claim: &eClaim{comp: c}, gold: gold, groups: []string{group}, synthetic: c.item != nil}
		return &pairOut{p: p, pred: pred, state: state}
	}
	const (
		same, opp, abs   = judge.LabelSame, judge.LabelOpposite, judge.LabelAbsent
		differ, agree    = judge.CardDiffer, judge.CardAgree
		only, hidden     = judge.CardOnlyOne, judge.StateHidden
		clear, edge, syn = groupClear, groupBoundary, groupSynthetic
	)
	kept := pair(item, opp, same, agree, syn)
	kept.p.kept, kept.p.synthetic = true, false
	res := &viewResult{pairs: []*pairOut{
		pair(q1, opp, abs, hidden, edge),   // missed
		pair(q1, same, opp, differ, clear), // a clear false opposite
		pair(q1, same, same, agree, clear),
		pair(q2, opp, opp, differ, edge),
		pair(q2, abs, abs, only, clear),
		pair(q2, abs, opp, differ, edge), // a false opposite outside the clear group
		pair(q3, same, same, agree, clear),
		pair(item, opp, same, agree, syn), // a synthetic opposite missed
		pair(item, opp, opp, differ, syn),
		kept, // a perturb item's other claim: in no number
	}}
	claim := func(c *comp, or, and bool) *claimOut { return &claimOut{c: &eClaim{comp: c}, coreOR: or, coreAND: and} }
	res.claims = []*claimOut{claim(q1, true, false), claim(q1, false, false), claim(q2, true, false), claim(q3, false, true), claim(item, true, true)}
	// Natural confusion: same 2/3 right, opposite 1/2 (3 predicted),
	// absent 1/2 (2 predicted): F1 0.8, 0.4, 0.5.
	want := map[string]float64{"missedNatural": 1, "missedSynthetic": 1, "macroF1": (0.8 + 0.4 + 0.5) / 3,
		// clear non-opposites 4, one predicted opposite (all non-opposites:
		// 2 of 5); core OR 2 of the 4 natural claims (AND: 1).
		"clearFalseOppositeRate": 0.25, "coreRecallOr": 0.5}
	pt := bootstrap(res, 0, 1)
	for name, w := range want {
		if iv := pt[name]; !near(iv.Point, w) || iv.Lo != iv.Point || iv.Hi != iv.Point {
			t.Errorf("%s %+v, want %v", name, iv, w)
		}
	}
	if f1 := statsOf(natural(res.pairs)).MacroF1; !near(f1, want["macroF1"]) {
		t.Errorf("statsOf macro F1 %v", f1)
	}
	// Drawn by question, every interval holds its point.
	for name, iv := range bootstrap(res, 2000, 20261101) {
		if !near(iv.Point, want[name]) || iv.Lo > iv.Point || iv.Hi < iv.Point {
			t.Errorf("%s %+v", name, iv)
		}
	}
}

// COR-8: the interval is the 2.5th and 97.5th percentile by nearest rank.
func TestBootInterval(t *testing.T) {
	for _, c := range []struct {
		n      int
		lo, hi float64
	}{{2000, 49, 1949}, {500, 12, 487}, {40, 0, 38}, {1, 0, 0}} {
		draws := make([]float64, c.n)
		for i := range draws {
			draws[i] = float64(c.n - 1 - i) // the draws come unsorted
		}
		if lo, hi := bootInterval(draws); lo != c.lo || hi != c.hi {
			t.Errorf("%d draws: [%v, %v], want [%v, %v]", c.n, lo, hi, c.lo, c.hi)
		}
	}
}
