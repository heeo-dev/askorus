//go:build judgeeval

package main

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Views (plan part 2). The cards are made by judge.Aggregate, as on the
// phone, from the recorded verdicts:
//   - the engine view of E: all of E's judgments in all six directions.
//     Aggregate drops a judgment by the From or Against answer's AI, so the
//     judge is relabelled "eval:E" in the copy it gets: then it keeps the
//     own-answer directions the product never asks (deviation 10), and the
//     view compares with the 614-pair gold.
//   - the product view: Plan's requests, each pair judged by the third AI,
//     with the judgments of that engine's records. It is whole only when
//     all three engines ran; a request without a record counts as failed.
//     A gold-only run stands the gold engine in for every judge.

// comp is one comparison: a P0-6 question, or a perturb item.
type comp struct {
	set    string
	q      int
	item   *perturbItem // nil for p06
	spec   judge.Spec
	dirs   [][2]string // the directions with jobs
	lang   string
	split  string
	claims []*eClaim
}

func (c *comp) itemID() string {
	if c.item == nil {
		return ""
	}
	return c.item.ID
}

// eClaim is a claim of one answer of a comparison.
type eClaim struct {
	comp  *comp
	ai    string
	n     int
	units []string // U(c): its units, "ai:n"
	pairs []*ePair // p06: against the two other answers; perturb: against Y'
	gold  string   // p06: the gold card (differ, agree or onlyOne)
	clear bool     // p06: both its pairs are in the clear group
}

func (c *eClaim) id() string { return claimKey(c.comp.q, c.ai, c.n) }

// ePair is a claim against one other answer, with its gold.
type ePair struct {
	id        string
	claim     *eClaim
	against   string
	gold      string
	synthetic bool // a synthetic opposite of a perturb item
	kept      bool // a perturb item's other claim: left out of every number
	groups    []string
	evGold    []string // the evidence gold: units of the against answer
	hasEv     bool
}

// universe is what a report reads: the comparisons of its runs and their
// records.
type universe struct {
	p        *prepared
	comps    []*comp
	records  map[string]record
	engines  []string // every engine of the runs, in engineNames order
	info     map[string]engineInfo
	manifest []manifest
	perturb  map[string]*perturbItem
}

// loadUniverse reads runs (folders under out) over the set in dataDir.
func loadUniverse(out, dataDir string, runs []string) (*universe, error) {
	d, err := loadData(dataDir)
	if err != nil {
		return nil, err
	}
	u := &universe{records: map[string]record{}, info: map[string]engineInfo{}, perturb: map[string]*perturbItem{}}
	files := d.fileHashes()
	maxUnits := 0
	seen := map[string]bool{}
	var sets []*perturbSet
	for _, name := range runs {
		dir := filepath.Join(out, name)
		m, err := readManifest(dir)
		if err != nil {
			return nil, fmt.Errorf("run %s: %w", name, err)
		}
		for f, sum := range m.Data.Files {
			if files[f] != sum {
				return nil, fmt.Errorf("run %s: data file %s is not the one the run used", name, f)
			}
		}
		if maxUnits != 0 && m.MaxUnits != maxUnits {
			return nil, fmt.Errorf("run %s: -max-units %d differs from the other runs' %d", name, m.MaxUnits, maxUnits)
		}
		maxUnits = m.MaxUnits
		recs, order, err := loadRecords(filepath.Join(dir, fileRequests), false)
		if err != nil {
			return nil, fmt.Errorf("run %s: %w", name, err)
		}
		// A job key names no tier or model: the same request in two runs
		// (a quick run and a standard one) would let the later one win.
		for _, k := range order {
			if _, dup := u.records[k]; dup {
				return nil, fmt.Errorf("run %s: request %s is in an earlier run too; report such runs apart", name, k)
			}
			u.records[k] = recs[k]
		}
		for _, e := range m.Engines {
			if !slices.Contains(u.engines, e) {
				u.engines = append(u.engines, e)
			}
		}
		for _, e := range slices.Sorted(maps.Keys(m.EngineInfo)) {
			inf := m.EngineInfo[e]
			was, ok := u.info[e]
			if !ok {
				u.info[e] = inf
				continue
			}
			if was.Model != inf.Model || was.Effort != inf.Effort || was.Family != inf.Family {
				return nil, fmt.Errorf("run %s: %s judged with %s/%s, an earlier run with %s/%s; report them apart", name, e, inf.Model, inf.Effort, was.Model, was.Effort)
			}
			if !slices.Contains(strings.Split(was.Version, ", "), inf.Version) {
				was.Version += ", " + inf.Version
				u.info[e] = was
			}
		}
		u.manifest = append(u.manifest, m)
		if m.Set == setPerturb {
			set := makePerturbSet(d, m.PerturbN)
			if set.hash() != m.Data.Perturb {
				return nil, fmt.Errorf("run %s: the perturb set made now is not the run's", name)
			}
			sets = append(sets, set)
		}
	}
	slices.SortStableFunc(u.engines, func(a, b string) int { return slices.Index(engineNames, a) - slices.Index(engineNames, b) })
	u.p = prepare(d, maxUnits)
	for _, m := range u.manifest {
		switch m.Set {
		case setP06:
			for _, q := range m.Questions {
				k := setP06 + "|" + strconv.Itoa(q)
				if !seen[k] {
					seen[k] = true
					u.comps = append(u.comps, u.p06Comp(q))
				}
			}
		case setPerturb:
			set := sets[0]
			sets = sets[1:]
			items := set.selectItems(m.Questions)
			u.p.prepareItems(items)
			for _, it := range items {
				k := setPerturb + "|" + it.ID
				if !seen[k] {
					seen[k] = true
					u.perturb[it.ID] = it
					u.comps = append(u.comps, u.itemComp(it))
				}
			}
		}
	}
	return u, nil
}

// p06Comp is question q with its claims and pairs.
func (u *universe) p06Comp(q int) *comp {
	p := u.p
	c := &comp{set: setP06, q: q, spec: p.spec(q), dirs: directions(), lang: p.qByID[q].Lang, split: p.splitOf[q]}
	for _, ai := range ais {
		for _, cr := range p.claimsOf(q, ai) {
			cl := &eClaim{comp: c, ai: ai, n: cr.N, units: p.fit[claimKey(q, ai, cr.N)].Units, clear: true}
			labels := map[string]bool{}
			for _, y := range ais {
				pr, ok := p.pairByID[pairID(q, ai, cr.N, y)]
				if !ok {
					continue
				}
				ep := &ePair{id: pr.ID, claim: cl, against: y, gold: pr.Label, groups: p.pairGroups(pr)}
				if ev, ok := p.evUnits[pr.ID]; ok {
					ep.evGold, ep.hasEv = ev, true
				}
				if !slices.Contains(ep.groups, groupClear) {
					cl.clear = false
				}
				labels[pr.Label] = true
				cl.pairs = append(cl.pairs, ep)
			}
			switch {
			case labels[judge.LabelOpposite]:
				cl.gold = judge.CardDiffer
			case labels[judge.LabelSame]:
				cl.gold = judge.CardAgree
			default:
				cl.gold = judge.CardOnlyOne
			}
			c.claims = append(c.claims, cl)
		}
	}
	return c
}

// itemComp is a perturb item: X's claims against Y'.
func (u *universe) itemComp(it *perturbItem) *comp {
	p := u.p
	c := &comp{set: setPerturb, q: it.Q, item: it, spec: p.itemSpec(it), dirs: [][2]string{{it.From, it.Against}},
		lang: p.qByID[it.Q].Lang, split: it.Split}
	for _, pc := range it.Claims {
		cl := &eClaim{comp: c, ai: it.From, n: pc.N, units: p.fit[claimKey(it.Q, it.From, pc.N)].Units}
		ep := &ePair{id: it.ID + ":" + strconv.Itoa(pc.N), claim: cl, against: it.Against, gold: pc.Label,
			synthetic: pc.Synthetic, kept: !pc.Synthetic, groups: []string{groupSynthetic}}
		if pc.Synthetic {
			ep.evGold, ep.hasEv = it.evidence, true
		}
		cl.pairs = []*ePair{ep}
		c.claims = append(c.claims, cl)
	}
	return c
}

// rec is the record of engine for direction d of c.
func (u *universe) rec(c *comp, engine string, d [2]string) (record, bool) {
	r, ok := u.records[jobKey(c.set, c.q, c.itemID(), engine, d[0], d[1])]
	return r, ok
}

// viewSpec names a view.
type viewSpec struct {
	name    string
	engine  string // the engine view's engine; "" for the product view
	standIn string // product view: the engine that answers for every judge ("" for the judge itself)
}

// views are the views the runs allow: one per engine, and the product view
// when all three engines ran (or gold stands in).
func (u *universe) views() []viewSpec {
	var out []viewSpec
	for _, e := range u.engines {
		out = append(out, viewSpec{name: e, engine: e})
	}
	all := true
	for _, e := range realEngines {
		all = all && slices.Contains(u.engines, e)
	}
	switch {
	case all:
		out = append(out, viewSpec{name: "product"})
	case slices.Contains(u.engines, engineGold):
		out = append(out, viewSpec{name: "product", standIn: engineGold})
	}
	return out
}

// pairOut is what a view made of one pair.
type pairOut struct {
	p      *ePair
	pred   string // same, opposite, absent, unclear, or none
	unsure bool
	core   bool
	state  string // its claim's state
	strict bool   // a differ item has the claim's unit on one side and the against answer on another
	judge  string // the AI that judged the direction
	own    bool   // the judge's company wrote the claim or the against answer
	evOK   bool   // has evidence gold and pred equals the gold label
	hit3   bool
	top1   bool
}

// claimOut is what a view made of one claim.
type claimOut struct {
	c       *eClaim
	state   string
	coreOR  bool
	coreAND bool
}

// compOut is what a view made of one comparison.
type compOut struct {
	c          *comp
	items      []judge.Item
	judgments  []judge.Judgment
	visibleOR  []int // per answer: units on a card
	visibleAND []int // per answer: units core in both directions, or in an opposite
}

// viewResult is one view of the runs.
type viewResult struct {
	spec    viewSpec
	pairs   []*pairOut
	claims  []*claimOut
	comps   []*compOut
	records []record       // the records it used
	missing int            // requests it wanted that have no record
	missBy  map[string]int // missing by set (p06, perturb)
}

// stateRank orders claim states, best first.
var stateRank = []string{judge.CardDiffer, judge.CardCheck, judge.CardAgree, judge.CardOnlyOne,
	judge.StateHidden, judge.StateNotCompared, judge.StateNotJudged, judge.StateWaiting}

func betterState(a, b string) string {
	ia, ib := slices.Index(stateRank, a), slices.Index(stateRank, b)
	if ia < 0 {
		return b
	}
	if ib < 0 || ia < ib {
		return a
	}
	return b
}

// planJudges are the judges the product view plans with.
func (u *universe) planJudges() []judge.JudgeAI {
	fam := judgeFamilies()
	var out []judge.JudgeAI
	for _, e := range realEngines {
		j := judge.JudgeAI{Agent: e, Family: fam[e]}
		if inf, ok := u.info[e]; ok && inf.Family != "" {
			j.Family, j.Model, j.Effort = inf.Family, inf.Model, inf.Effort
		}
		out = append(out, j)
	}
	return out
}

// view makes view vs with the enabled checks.
func (u *universe) view(vs viewSpec, checks []string) *viewResult {
	res := &viewResult{spec: vs, missBy: map[string]int{}}
	for _, c := range u.comps {
		var reqs []judge.Request
		var js []judge.Judgment
		judgeOf := map[[2]string]string{}
		add := func(id string, d [2]string, label, engine string) {
			r := judge.Request{ID: id, From: d[0], Against: d[1], Judge: label, Status: judge.RequestFailed}
			if rec, ok := u.rec(c, engine, d); ok {
				res.records = append(res.records, rec)
				if rec.Status == statusDone {
					r.Status = judge.RequestDone
					js = append(js, judge.Judgments(r, rec.validated())...)
				}
			} else {
				res.missing++
				res.missBy[c.set]++
			}
			reqs = append(reqs, r)
		}
		if vs.engine != "" {
			for i, d := range c.dirs {
				judgeOf[d] = vs.engine
				add("r"+strconv.Itoa(i+1), d, "eval:"+vs.engine, vs.engine)
			}
		} else {
			spec := c.spec
			spec.Judges = u.planJudges()
			plan, _ := judge.Plan(spec)
			for _, r := range plan {
				d := [2]string{r.From, r.Against}
				engine := r.Judge
				if vs.standIn != "" {
					engine = vs.standIn
				}
				judgeOf[d] = r.Judge
				if r.Judge == "" || !slices.Contains(c.dirs, d) {
					reqs = append(reqs, judge.Request{ID: r.ID, From: r.From, Against: r.Against, Judge: r.Judge, Status: judge.RequestFailed})
					continue
				}
				add(r.ID, d, r.Judge, engine)
			}
		}
		s := judge.Aggregate(judge.AggregateInput{Answers: c.spec.Answers, Requests: reqs, Judgments: js, Checks: checks, Ended: true})
		res.addComp(u, c, s, judgeOf)
	}
	return res
}

// addComp turns one comparison's cards into outcomes.
func (res *viewResult) addComp(u *universe, c *comp, s judge.Summary, judgeOf map[[2]string]string) {
	states := map[string]string{}
	for _, us := range s.Units {
		states[us.Unit] = us.State
	}
	byUnit := map[string][]judge.Judgment{}
	inOpp := map[string]bool{}
	for _, j := range s.Judgments {
		byUnit[j.Unit] = append(byUnit[j.Unit], j)
		if j.Label == judge.LabelOpposite {
			inOpp[j.Unit] = true
			for _, e := range j.Evidence {
				inOpp[e] = true
			}
		}
	}
	coreAND := func(unit, ai string) bool {
		for _, a := range c.spec.Answers {
			if a.ID == ai || a.Status != judge.AnswerReady {
				continue
			}
			ok := false
			for _, j := range byUnit[unit] {
				if j.Against == a.ID && j.Core {
					ok = true
				}
			}
			if !ok {
				return false
			}
		}
		return true
	}
	co := &compOut{c: c, items: s.Items, judgments: s.Judgments}
	for _, a := range c.spec.Answers {
		or, and := 0, 0
		for _, un := range a.Units {
			switch states[un.ID] {
			case judge.CardDiffer, judge.CardCheck, judge.CardAgree, judge.CardOnlyOne:
				or++
			}
			if un.Judged && (coreAND(un.ID, a.ID) || inOpp[un.ID]) {
				and++
			}
		}
		co.visibleOR, co.visibleAND = append(co.visibleOR, or), append(co.visibleAND, and)
	}
	res.comps = append(res.comps, co)
	family := map[string]string{}
	for _, a := range c.spec.Answers {
		family[a.ID] = a.Family
	}
	for _, cl := range c.claims {
		out := &claimOut{c: cl, state: judge.StateNotJudged}
		for _, un := range cl.units {
			out.state = betterState(out.state, states[un])
			for _, j := range byUnit[un] {
				out.coreOR = out.coreOR || j.Core
			}
			out.coreAND = out.coreAND || coreAND(un, cl.ai)
		}
		res.claims = append(res.claims, out)
		for _, p := range cl.pairs {
			po := &pairOut{p: p, pred: "none", state: out.state, judge: judgeOf[[2]string{cl.ai, p.against}]}
			if res.spec.engine != "" {
				if inf, ok := u.info[res.spec.engine]; ok && inf.Family != "" {
					po.own = inf.Family == family[cl.ai] || inf.Family == family[p.against]
				}
			}
			var mine []judge.Judgment
			for _, un := range cl.units {
				for _, j := range byUnit[un] {
					if j.Against == p.against {
						mine = append(mine, j)
					}
				}
			}
			has := map[string]bool{}
			for _, j := range mine {
				has[j.Label] = true
				po.unsure = po.unsure || j.Unsure
				po.core = po.core || j.Core
			}
			for _, l := range []string{judge.LabelOpposite, judge.LabelUnclear, judge.LabelSame, judge.LabelAbsent} {
				if has[l] {
					po.pred = l
					break
				}
			}
			if p.gold == judge.LabelOpposite {
				po.strict = strictDiffer(s.Items, cl.units, p.against)
			}
			if p.hasEv && po.pred == p.gold && (p.gold == judge.LabelSame || p.gold == judge.LabelOpposite) {
				po.evOK = true
				first := true
				for _, j := range mine {
					if j.Label != po.pred {
						continue
					}
					for _, e := range j.Evidence {
						if slices.Contains(p.evGold, e) {
							po.hit3 = true
						}
					}
					if first && len(j.Evidence) > 0 {
						po.top1 = slices.Contains(p.evGold, j.Evidence[0])
						first = false
					}
				}
			}
			res.pairs = append(res.pairs, po)
		}
	}
}

// strictDiffer reports whether a differ item has a unit of units on one
// side and answer against on another.
func strictDiffer(items []judge.Item, units []string, against string) bool {
	for _, it := range items {
		if it.Kind != judge.CardDiffer {
			continue
		}
		for i, pos := range it.Positions {
			if !slices.ContainsFunc(pos.Units, func(un string) bool { return slices.Contains(units, un) }) {
				continue
			}
			for k, other := range it.Positions {
				if k != i && slices.Contains(other.Agents, against) {
					return true
				}
			}
		}
	}
	return false
}
