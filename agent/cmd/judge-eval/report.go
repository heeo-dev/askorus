//go:build judgeeval

package main

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// The report (plan part 2, report): every view's numbers into
// <run>\report.json and report.md, and a short summary on the console.
// Only ids, codes and numbers are printed; -texts adds the unit texts of
// the opposites and false opposites for reading by hand.

type ruleResult struct {
	Rule  string  `json:"rule"`
	What  string  `json:"what"`
	Value float64 `json:"value"`
	Limit string  `json:"limit"`
	Pass  *bool   `json:"pass"` // nil: the runs do not cover it
}

type viewReport struct {
	Name            string                          `json:"name"`
	Engine          string                          `json:"engine,omitempty"`
	StandIn         string                          `json:"standIn,omitempty"`
	Natural         pairStats                       `json:"natural"`
	Synthetic       pairStats                       `json:"synthetic"`
	Cards           cardStats                       `json:"cards"`
	Requests        requestStats                    `json:"requests"`
	Evidence        evidenceStats                   `json:"evidence"`          // the natural pairs: 21 opposite, 30 same
	EvidenceSyn     evidenceStats                   `json:"evidenceSynthetic"` // the synthetic opposites
	Breakdowns      map[string]map[string]pairStats `json:"breakdowns"`
	Bootstrap       map[string]interval             `json:"bootstrap"`
	Rules           []ruleResult                    `json:"rules,omitempty"`
	MissingRequests int                             `json:"missingRequests"`
	MissingBySet    map[string]int                  `json:"missingBySet,omitempty"` // p06, perturb
}

type sweepRow struct {
	View          string   `json:"view"`
	Check         string   `json:"check"`
	NewlyShown    int      `json:"devMissedNewlyShown"`
	NewlyShownIDs []string `json:"devMissedNewlyShownIds"`
	BaseShare     float64  `json:"devClearCheckShareBase"`
	Share         float64  `json:"devClearCheckShare"`
	DeltaPoints   float64  `json:"deltaPoints"`
}

type coverage struct {
	P06All    bool  `json:"p06All"`
	Perturb   bool  `json:"perturb"`
	Questions []int `json:"questions"`
	Items     int   `json:"items"`
}

type report struct {
	Tool      string                `json:"tool"`
	Created   string                `json:"created"`
	Runs      []string              `json:"runs"`
	Checks    []string              `json:"checks"`
	Boot      int                   `json:"boot"`
	Seed      uint64                `json:"seed"`
	Data      map[string]string     `json:"data"`
	Engines   map[string]engineInfo `json:"engines"`
	ClaimFit  fitCounts             `json:"claimFit"`
	EvFit     fitCounts             `json:"evidenceFit"`
	Coverage  coverage              `json:"coverage"`
	Views     []viewReport          `json:"views"`
	Sweep     []sweepRow            `json:"sweep,omitempty"`
	Manifests []manifest            `json:"manifests"`
}

// coverageOf says what the runs cover.
func (u *universe) coverageOf() coverage {
	var c coverage
	p06 := map[int]bool{}
	for _, cp := range u.comps {
		if cp.item == nil {
			p06[cp.q] = true
		} else {
			c.Perturb = true
			c.Items++
		}
		if !slices.Contains(c.Questions, cp.q) {
			c.Questions = append(c.Questions, cp.q)
		}
	}
	slices.Sort(c.Questions)
	c.P06All = len(p06) == len(u.p.questions)
	return c
}

// makeViewReport computes every number of one view.
func makeViewReport(u *universe, v *viewResult, cov coverage, boot int, seed uint64) viewReport {
	vr := viewReport{Name: v.spec.name, Engine: v.spec.engine, StandIn: v.spec.standIn,
		Natural: statsOf(natural(v.pairs)), Synthetic: statsOf(synthetic(v.pairs)),
		Cards: cardsOf(v.claims, v.comps), Requests: requestsOf(v.records),
		Evidence: evidenceOf(v.pairs, v.comps, false), EvidenceSyn: evidenceOf(v.pairs, v.comps, true),
		Breakdowns: breakdowns(v.pairs, v.spec.engine == ""), Bootstrap: bootstrap(v, boot, seed), MissingRequests: v.missing}
	if len(v.missBy) > 0 {
		vr.MissingBySet = v.missBy
	}
	if slices.Contains(realEngines, v.spec.engine) {
		var p06 []record
		for _, r := range v.records {
			if r.Set == setP06 {
				p06 = append(p06, r)
			}
		}
		vr.Rules = rulesOf(vr, requestsOf(p06), cov, v.missBy)
	}
	return vr
}

// rulesOf checks rules 1a–g of design part 9 on an engine view: 1b on the
// perturb run, the others on p06 split all, whose requests p06 are (the
// view's request table may hold the perturb ones too). A rule needs every
// request of its set to have a record: a stopped run, or one of part of
// the engines, gives n/a, not a verdict over the requests it has.
func rulesOf(vr viewReport, p06 requestStats, cov coverage, missing map[string]int) []ruleResult {
	pass := func(ok, covered bool) *bool {
		if !covered {
			return nil
		}
		return &ok
	}
	all := cov.P06All && missing[setP06] == 0
	n := vr.Natural
	shown := n.Differ + n.Check
	clear := vr.Breakdowns["group"][groupClear]
	syn := vr.Synthetic
	synShown := ratio(syn.Differ+syn.Check, syn.Opposites)
	return []ruleResult{
		{"1a", "natural opposites shown", float64(shown), ">= 19 of 21", pass(shown >= 19 && n.Opposites == 21, all)},
		{"1b", "synthetic opposites shown", synShown, ">= 0.90", pass(synShown >= 0.90, cov.Perturb && syn.Opposites > 0 && missing[setPerturb] == 0)},
		{"1c", "false opposites among clear pairs", clear.FalseOppositeRate, "<= 0.03", pass(clear.FalseOppositeRate <= 0.03, all)},
		{"1d", "invalid first output", p06.InvalidRate, "<= 0.02", pass(p06.InvalidRate <= 0.02, all)},
		{"1e", "core recall (OR)", vr.Cards.CoreRecallOR, ">= 0.90", pass(vr.Cards.CoreRecallOR >= 0.90, all)},
		{"1f", "request median ms", p06.MsP50, "<= 20000", pass(p06.MsP50 <= 20000, all)},
		{"1g", "check items from unsure only", vr.Cards.UnsureOnlyShare, "<= 0.15", pass(vr.Cards.UnsureOnlyShare <= 0.15, all)},
	}
}

// sweep adds each other check alone and gives rule 2's deltas on dev.
func sweep(u *universe, specs []viewSpec, checks []string) []sweepRow {
	var rows []sweepRow
	devMissed := func(v *viewResult) map[string]bool {
		out := map[string]bool{}
		for _, po := range natural(v.pairs) {
			if po.p.claim.comp.split == splitDev && po.p.gold == judge.LabelOpposite && po.state != judge.CardDiffer && po.state != judge.CardCheck {
				out[po.p.id] = true
			}
		}
		return out
	}
	devShare := func(v *viewResult) float64 {
		n, k := 0, 0
		for _, co := range v.claims {
			if co.c.comp.item == nil && co.c.comp.split == splitDev && co.c.clear {
				n++
				if co.state == judge.CardCheck {
					k++
				}
			}
		}
		return ratio(k, n)
	}
	for _, vs := range specs {
		base := u.view(vs, checks)
		missed, share := devMissed(base), devShare(base)
		for _, c := range judge.AllChecks {
			if slices.Contains(checks, c) {
				continue
			}
			alt := u.view(vs, append(slices.Clone(checks), c))
			now := devMissed(alt)
			row := sweepRow{View: vs.name, Check: c, BaseShare: share, Share: devShare(alt), NewlyShownIDs: []string{}}
			for id := range missed {
				if !now[id] {
					row.NewlyShown++
					row.NewlyShownIDs = append(row.NewlyShownIDs, id)
				}
			}
			sort.Strings(row.NewlyShownIDs)
			row.DeltaPoints = 100 * (row.Share - row.BaseShare)
			rows = append(rows, row)
		}
	}
	return rows
}

// cmdReport is the report subcommand.
func cmdReport(o reportOptions, stdout, stderr io.Writer) int {
	u, err := loadUniverse(o.out, o.data, o.runs)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	cov := u.coverageOf()
	rep := report{Tool: "judge-eval", Created: time.Now().UTC().Format(time.RFC3339), Runs: o.runs, Checks: o.checks,
		Boot: o.boot, Seed: o.seed, Data: u.p.fileHashes(), Engines: u.info, ClaimFit: u.p.fitStats, EvFit: u.p.evStats,
		Coverage: cov, Manifests: u.manifest}
	specs := u.views()
	results := map[string]*viewResult{}
	for _, vs := range specs {
		v := u.view(vs, o.checks)
		results[vs.name] = v
		rep.Views = append(rep.Views, makeViewReport(u, v, cov, o.boot, o.seed))
	}
	if o.sweepChecks {
		rep.Sweep = sweep(u, specs, o.checks)
	}
	dir := filepath.Join(o.out, o.runs[0])
	if err := writeJSON(filepath.Join(dir, fileReport), rep); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	md := markdown(rep)
	if err := os.WriteFile(filepath.Join(dir, fileReportMD), []byte(md), 0o600); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	if o.asJSON {
		b, _ := marshal(rep, true)
		stdout.Write(b)
	} else {
		io.WriteString(stdout, summary(rep))
		fmt.Fprintf(stdout, "wrote %s and %s\n", filepath.Join(dir, fileReport), fileReportMD)
	}
	if o.texts {
		printTexts(stdout, u, specs, results)
	}
	return exitOK
}

func pct(x float64) string { return fmt.Sprintf("%.1f%%", 100*x) }

// summary is the console's short form.
func summary(r report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "report %s: checks %s, %d questions, %d perturb items\n", strings.Join(r.Runs, ","), strings.Join(r.Checks, ","), len(r.Coverage.Questions), r.Coverage.Items)
	for _, v := range r.Views {
		n := v.Natural
		fmt.Fprintf(&b, "  %-9s natural opposites %d: differ %d (strict %d), check %d, missed %d; false opposites %d/%d; macro F1 %.3f; core OR %s AND %s\n",
			v.Name, n.Opposites, n.Differ, n.Strict, n.Check, n.Missed, n.FalseOpposites, n.NonOpposites, n.MacroF1,
			pct(v.Cards.CoreRecallOR), pct(v.Cards.CoreRecallAND))
		if v.Synthetic.Opposites > 0 {
			s := v.Synthetic
			fmt.Fprintf(&b, "  %-9s synthetic opposites %d: differ %d (strict %d), check %d, missed %d\n", "", s.Opposites, s.Differ, s.Strict, s.Check, s.Missed)
		}
		q := v.Requests
		fmt.Fprintf(&b, "  %-9s requests %d (done %d, failed %d %v), invalid first %d (%s), retries %d, ms p50 %.0f p90 %.0f, tokens in %d out %d, zero-token %d\n",
			"", q.Requests, q.Done, q.Failed, q.Codes, q.FirstInvalid, pct(q.InvalidRate), q.Retries, q.MsP50, q.MsP90, q.InSum, q.OutSum, q.ZeroTokens)
		if len(n.MissedIDs) > 0 {
			fmt.Fprintf(&b, "  %-9s missed %s\n", "", strings.Join(n.MissedIDs, " "))
		}
		if v.MissingRequests > 0 {
			fmt.Fprintf(&b, "  %-9s %d requests have no record %v: the rules of their set are n/a\n", "", v.MissingRequests, v.MissingBySet)
		}
	}
	return b.String()
}

// markdown is report.md.
func markdown(r report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# judge-eval report\n\nRuns: %s. Made %s. Checks: %s. Bootstrap %d draws, seed %d.\n\n",
		strings.Join(r.Runs, ", "), r.Created, strings.Join(r.Checks, ", "), r.Boot, r.Seed)
	fmt.Fprintf(&b, "Coverage: %d questions %v, p06 all: %v, perturb items: %d.\n\n", len(r.Coverage.Questions), r.Coverage.Questions, r.Coverage.P06All, r.Coverage.Items)
	fmt.Fprintf(&b, "Claim fit: %d one unit, %d runs, %d inside, %d none. Evidence fit: %d unit, %d run, %d inside, %d none %v.\n\n",
		r.ClaimFit.One, r.ClaimFit.Run, r.ClaimFit.Inside, r.ClaimFit.None, r.EvFit.One, r.EvFit.Run, r.EvFit.Inside, r.EvFit.None, r.EvFit.Fails)
	b.WriteString("## Engines\n\n| engine | model | effort | family | CLI |\n| --- | --- | --- | --- | --- |\n")
	names := make([]string, 0, len(r.Engines))
	for k := range r.Engines {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		e := r.Engines[k]
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", k, e.Model, e.Effort, e.Family, e.Version)
	}
	b.WriteString("\n## Opposites (shown as differ / check / missed)\n\n| view | natural | differ | strict | check | missed | synthetic | differ | check | missed |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, v := range r.Views {
		n, s := v.Natural, v.Synthetic
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %d | %d | %d | %d |\n", v.Name, n.Opposites, n.Differ, n.Strict, n.Check, n.Missed, s.Opposites, s.Differ, s.Check, s.Missed)
	}
	b.WriteString("\n## Pairs\n\n| view | pairs | macro F1 | false opposites | rate | opposite label | opposite or unsure |\n| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, v := range r.Views {
		n := v.Natural
		fmt.Fprintf(&b, "| %s | %d | %.3f | %d/%d | %s | %d/%d | %d/%d |\n", v.Name, n.Pairs, n.MacroF1, n.FalseOpposites, n.NonOpposites,
			pct(n.FalseOppositeRate), n.OppositeLabel, n.Opposites, n.OppositeOrUnsure, n.Opposites)
	}
	b.WriteString("\n## Cards (natural claims)\n\n| view | claims | differ | check | agree | onlyOne | hidden | notCompared | core OR | core AND | check rate | unsure only | clear check share | visible OR | visible AND |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, v := range r.Views {
		c := v.Cards
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %d | %d | %s | %s | %s | %s | %s | %.0f | %.0f |\n", v.Name, c.Claims,
			c.States[judge.CardDiffer], c.States[judge.CardCheck], c.States[judge.CardAgree], c.States[judge.CardOnlyOne],
			c.States[judge.StateHidden], c.States[judge.StateNotCompared], pct(c.CoreRecallOR), pct(c.CoreRecallAND),
			pct(c.CheckRate), pct(c.UnsureOnlyShare), pct(c.ClearCheckShare), c.VisibleOR, c.VisibleAND)
	}
	b.WriteString("\n## Card confusion (gold card → claim state)\n\n")
	for _, v := range r.Views {
		fmt.Fprintf(&b, "- %s:", v.Name)
		for _, g := range []string{judge.CardDiffer, judge.CardAgree, judge.CardOnlyOne} {
			fmt.Fprintf(&b, " %s %v;", g, v.Cards.Confusion[g])
		}
		b.WriteString("\n")
	}
	b.WriteString("\n## Requests\n\n| view | requests | done | failed | codes | invalid first | retries | dropped | ms p50 | ms p90 | attempt p50 | attempt p90 | in | out | in median | out median | zero-token |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, v := range r.Views {
		q := v.Requests
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %v | %d (%s) | %d | %d | %.0f | %.0f | %.0f | %.0f | %d | %d | %.0f | %.0f | %d |\n", v.Name, q.Requests, q.Done, q.Failed, q.Codes,
			q.FirstInvalid, pct(q.InvalidRate), q.Retries, q.Dropped, q.MsP50, q.MsP90, q.AttemptP50, q.AttemptP90, q.InSum, q.OutSum, q.InMedian, q.OutMedian, q.ZeroTokens)
	}
	b.WriteString("\n## Evidence (pairs with evidence gold whose prediction is right)\n\n| view | pairs | eligible | hit@3 | top-1 | no evidence among same/opposite |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, v := range r.Views {
		for _, row := range []struct {
			kind string
			e    evidenceStats
		}{{"natural", v.Evidence}, {"synthetic", v.EvidenceSyn}} {
			e := row.e
			if e.Eligible == 0 && e.SameOpposite == 0 {
				continue
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %s | %s | %d/%d (%s) |\n", v.Name, row.kind, e.Eligible, pct(e.Hit3Rate), pct(e.Top1Rate), e.NoEvidence, e.SameOpposite, pct(e.NoEvidenceRate))
		}
	}
	b.WriteString("\n## Breakdowns (missed / opposites, false opposites / non-opposites, macro F1)\n\n")
	for _, v := range r.Views {
		fmt.Fprintf(&b, "### %s\n\n", v.Name)
		dims := make([]string, 0, len(v.Breakdowns))
		for d := range v.Breakdowns {
			dims = append(dims, d)
		}
		sort.Strings(dims)
		for _, d := range dims {
			keys := make([]string, 0, len(v.Breakdowns[d]))
			for k := range v.Breakdowns[d] {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			fmt.Fprintf(&b, "- %s:", d)
			for _, k := range keys {
				s := v.Breakdowns[d][k]
				fmt.Fprintf(&b, " %s %d/%d, %d/%d, %.3f;", k, s.Missed, s.Opposites, s.FalseOpposites, s.NonOpposites, s.MacroF1)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("## Bootstrap (95%, by question)\n\n| view | " + strings.Join(bootNames, " | ") + " |\n|" + strings.Repeat(" --- |", len(bootNames)+1) + "\n")
	for _, v := range r.Views {
		fmt.Fprintf(&b, "| %s |", v.Name)
		for _, name := range bootNames {
			iv := v.Bootstrap[name]
			fmt.Fprintf(&b, " %.3f [%.3f, %.3f] |", iv.Point, iv.Lo, iv.Hi)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n## Rules 1a-g (design part 9)\n\n")
	for _, v := range r.Views {
		if len(v.Rules) == 0 {
			continue
		}
		fmt.Fprintf(&b, "- %s", v.Name)
		if len(v.MissingBySet) > 0 {
			var miss []string
			for _, set := range slices.Sorted(maps.Keys(v.MissingBySet)) {
				miss = append(miss, fmt.Sprintf("%s %d", set, v.MissingBySet[set]))
			}
			fmt.Fprintf(&b, " (requests without a record: %s)", strings.Join(miss, ", "))
		}
		b.WriteString(":")
		for _, rr := range v.Rules {
			state := "n/a"
			if rr.Pass != nil {
				state = map[bool]string{true: "pass", false: "FAIL"}[*rr.Pass]
			}
			fmt.Fprintf(&b, " %s %s (%.3g, %s);", rr.Rule, state, rr.Value, rr.Limit)
		}
		b.WriteString("\n")
	}
	if len(r.Sweep) > 0 {
		b.WriteString("\n## Check sweep (rule 2, dev)\n\n| view | check | missed newly shown | clear check share | delta (points) |\n| --- | --- | --- | --- | --- |\n")
		for _, s := range r.Sweep {
			fmt.Fprintf(&b, "| %s | %s | %d %v | %s → %s | %+.1f |\n", s.View, s.Check, s.NewlyShown, s.NewlyShownIDs, pct(s.BaseShare), pct(s.Share), s.DeltaPoints)
		}
	}
	b.WriteString("\n## Missed and check ids\n\n")
	for _, v := range r.Views {
		fmt.Fprintf(&b, "- %s: missed %v; check %v; false opposites %v; synthetic missed %v\n", v.Name, v.Natural.MissedIDs, v.Natural.CheckIDs, v.Natural.FalseOppositeIDs, v.Synthetic.MissedIDs)
	}
	return b.String()
}

// printTexts prints, for reading by hand, the unit texts of every gold
// opposite and every false opposite of each view, and how they were
// judged. Content: only with -texts.
func printTexts(w io.Writer, u *universe, specs []viewSpec, results map[string]*viewResult) {
	text := map[string]string{}
	for _, c := range u.comps {
		for _, a := range c.spec.Answers {
			for _, un := range a.Units {
				text[c.set+"|"+c.itemID()+"|"+un.ID] = un.Text
			}
		}
	}
	for _, vs := range specs {
		v := results[vs.name]
		fmt.Fprintf(w, "\n== %s\n", vs.name)
		byComp := map[*comp]*compOut{}
		for _, co := range v.comps {
			byComp[co.c] = co
		}
		for _, po := range v.pairs {
			if po.p.kept || (po.p.gold != judge.LabelOpposite && po.pred != judge.LabelOpposite) {
				continue
			}
			c := po.p.claim.comp
			fmt.Fprintf(w, "%s gold %s pred %s state %s unsure %v\n", po.p.id, po.p.gold, po.pred, po.state, po.unsure)
			for _, un := range po.p.claim.units {
				fmt.Fprintf(w, "  claim %s: %s\n", un, text[c.set+"|"+c.itemID()+"|"+un])
				for _, j := range byComp[c].judgments {
					if j.Unit == un && j.Against == po.p.against {
						fmt.Fprintf(w, "    %s %s unsure %v core %v flags %v\n", j.Judge, j.Label, j.Unsure, j.Core, j.Flags)
						for _, e := range j.Evidence {
							fmt.Fprintf(w, "      %s: %s\n", e, text[c.set+"|"+c.itemID()+"|"+e])
						}
					}
				}
			}
			for _, e := range po.p.evGold {
				fmt.Fprintf(w, "  gold evidence %s: %s\n", e, text[c.set+"|"+c.itemID()+"|"+e])
			}
		}
	}
}

// cmdData is the data subcommand: what the loader made of the set.
func cmdData(o dataOptions, stdout, stderr io.Writer) int {
	d, err := loadData(o.data)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	p := prepare(d, o.maxUnits)
	fmt.Fprintf(stdout, "set %s: %d questions, %d answers, %d claims, %d pairs\n", d.split.Set, len(d.questions), len(d.answers), len(d.claims), len(d.pairs))
	for _, name := range []string{splitDev, splitTest} {
		c := d.split.Counts[name]
		fmt.Fprintf(stdout, "  %s %v: %d english, %d claims, %d pairs, %d opposite, %d hard\n", name, d.questionsOf(name), c.English, c.Claims, c.Pairs, c.Opposite, c.Hard)
	}
	g := d.groups.Counts
	fmt.Fprintf(stdout, "  groups: hard %d, boundary %d (hard included), clear %d\n", g.Hard, g.Boundary, g.Clear)
	units, most := 0, 0
	for _, a := range p.answers {
		units += len(a.Units)
		most = max(most, len(a.Units))
	}
	fmt.Fprintf(stdout, "  units: %d, at most %d in one answer (max units %d)\n", units, most, o.maxUnits)
	f := p.fitStats
	fmt.Fprintf(stdout, "  claim fit: %d one unit, %d runs, %d inside, %d none %v\n", f.One, f.Run, f.Inside, f.None, f.Fails)
	e := p.evStats
	fmt.Fprintf(stdout, "  evidence: %d pairs, %d entries: %d unit, %d run, %d inside, %d failed %v\n", len(d.evidence.Entries), e.One+e.Run+e.Inside+e.None, e.One, e.Run, e.Inside, e.None, e.Fails)
	cards := map[string]int{}
	for _, c := range d.claims {
		labels := map[string]bool{}
		for _, y := range ais {
			if pr, ok := d.pairByID[pairID(c.Q, c.AI, c.N, y)]; ok {
				labels[pr.Label] = true
			}
		}
		switch {
		case labels[judge.LabelOpposite]:
			cards[judge.CardDiffer]++
		case labels[judge.LabelSame]:
			cards[judge.CardAgree]++
		default:
			cards[judge.CardOnlyOne]++
		}
	}
	fmt.Fprintf(stdout, "  gold cards: differ %d, agree %d, onlyOne %d\n", cards[judge.CardDiffer], cards[judge.CardAgree], cards[judge.CardOnlyOne])
	return exitOK
}

// cmdPerturb is the perturb subcommand: it writes the set and shows items.
func cmdPerturb(o perturbOptions, stdout, stderr io.Writer) int {
	d, err := loadData(o.data)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	set := makePerturbSet(d, o.n)
	if err := os.MkdirAll(filepath.Dir(o.file), 0o700); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	if err := os.WriteFile(o.file, set.canonical(), 0o600); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	syn, perQ := 0, map[int]int{}
	for _, it := range set.Items {
		perQ[it.Q]++
		for _, c := range it.Claims {
			if c.Synthetic {
				syn++
			}
		}
	}
	fmt.Fprintf(stdout, "%s: %d items over %d questions, %d synthetic opposites, sha256 %s\nwrote %s\n", perturbVersion, len(set.Items), len(perQ), syn, set.hash(), o.file)
	if o.show > 0 {
		p := prepare(d, judge.MaxUnits)
		items := set.Items[:min(o.show, len(set.Items))]
		p.prepareItems(items)
		for _, it := range items {
			showItem(stdout, p, it)
		}
	}
	return exitOK
}

// showItem prints one perturb item for checking by hand: content.
func showItem(w io.Writer, p *prepared, it *perturbItem) {
	fmt.Fprintf(w, "\n== %s (%s)\n", it.ID, it.Split)
	fmt.Fprintf(w, "question: %s\n", p.qByID[it.Q].Text)
	for _, ch := range it.Changes {
		fmt.Fprintf(w, "change %s: %q -> %q at %d places\n", ch.Kind, ch.Was, ch.Now, len(ch.At))
	}
	for _, c := range it.Claims {
		if c.Synthetic {
			fmt.Fprintf(w, "claim %s%d: %s\n", it.From, c.N, p.claimByID[claimKey(it.Q, it.From, c.N)].Text)
		}
	}
	orig := p.answers[ansKey(it.Q, it.Against)]
	unitAt := func(a judge.Answer, text string, r [2]int) *judge.Unit {
		s, e := judge.UTF16Offset(text, r[0]), judge.UTF16Offset(text, r[1])
		for i := range a.Units {
			if a.Units[i].Start < e && s < a.Units[i].End {
				return &a.Units[i]
			}
		}
		return nil
	}
	shown := map[string]bool{}
	for _, ch := range it.Changes {
		for k := range ch.At {
			after := unitAt(it.answer, it.Answer, ch.At[k])
			before := unitAt(orig, orig.Text, ch.old[k])
			if after == nil || shown[after.ID] {
				continue
			}
			shown[after.ID] = true
			if before != nil {
				fmt.Fprintf(w, "  %s before: %s\n", before.ID, before.Text)
			}
			fmt.Fprintf(w, "  %s after:  %s\n", after.ID, after.Text)
		}
	}
}
