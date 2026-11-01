//go:build judgeeval

package main

import (
	"math"
	"slices"
	"sort"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Metrics (plan part 2, metrics.go; design part 9 재는 것). A pair is a
// claim against one other answer, a claim's units are U(c) from fit.go.
// The headline number is the opposites missed: a gold opposite whose
// claim is on neither the differ nor the check card.

// Classes of a pair's prediction; the gold is one of the first three.
var (
	goldClasses = []string{judge.LabelSame, judge.LabelOpposite, judge.LabelAbsent}
	predClasses = []string{judge.LabelSame, judge.LabelOpposite, judge.LabelAbsent, judge.LabelUnclear, "none"}
)

type prf struct {
	P       float64 `json:"p"`
	R       float64 `json:"r"`
	F1      float64 `json:"f1"`
	Support int     `json:"support"`
}

// pairStats are the pair-level and opposite numbers of a set of pairs.
type pairStats struct {
	Pairs             int                       `json:"pairs"`
	Confusion         map[string]map[string]int `json:"confusion"` // gold → prediction → pairs
	Classes           map[string]prf            `json:"classes"`
	MacroF1           float64                   `json:"macroF1"`
	FalseOpposites    int                       `json:"falseOpposites"`
	NonOpposites      int                       `json:"nonOpposites"`
	FalseOppositeRate float64                   `json:"falseOppositeRate"`
	FalseOppositeIDs  []string                  `json:"falseOppositeIds"`
	Opposites         int                       `json:"opposites"`
	OppositeLabel     int                       `json:"oppositeLabel"`    // predicted opposite
	OppositeOrUnsure  int                       `json:"oppositeOrUnsure"` // predicted opposite, or unsure
	OppositeRecall    float64                   `json:"oppositeRecall"`
	OrUnsureRecall    float64                   `json:"oppositeOrUnsureRecall"`
	Differ            int                       `json:"differ"` // opposites shown as differ
	Check             int                       `json:"check"`  // shown on the check card
	Missed            int                       `json:"missed"`
	Strict            int                       `json:"strict"` // differ with the against answer on another side
	MissedIDs         []string                  `json:"missedIds"`
	CheckIDs          []string                  `json:"checkIds"`
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// confusionOf counts gold × prediction.
func confusionOf(pairs []*pairOut) map[string]map[string]int {
	conf := map[string]map[string]int{}
	for _, g := range goldClasses {
		conf[g] = map[string]int{}
		for _, p := range predClasses {
			conf[g][p] = 0
		}
	}
	for _, po := range pairs {
		if conf[po.p.gold] != nil {
			conf[po.p.gold][po.pred]++
		}
	}
	return conf
}

// macroF1 is the mean F1 over the gold classes the confusion has; a
// prediction of none or unclear is wrong for every class.
func macroF1(conf map[string]map[string]int) (float64, map[string]prf) {
	classes := map[string]prf{}
	sum, n := 0.0, 0
	for _, g := range goldClasses {
		support, predicted := 0, 0
		for _, p := range predClasses {
			support += conf[g][p]
		}
		for _, g2 := range goldClasses {
			predicted += conf[g2][g]
		}
		tp := conf[g][g]
		c := prf{P: ratio(tp, predicted), R: ratio(tp, support), Support: support}
		if c.P+c.R > 0 {
			c.F1 = 2 * c.P * c.R / (c.P + c.R)
		}
		classes[g] = c
		if support > 0 {
			sum += c.F1
			n++
		}
	}
	if n == 0 {
		return 0, classes
	}
	return sum / float64(n), classes
}

// statsOf are the numbers of pairs (kept perturb pairs never come here).
func statsOf(pairs []*pairOut) pairStats {
	s := pairStats{Pairs: len(pairs), Confusion: confusionOf(pairs), FalseOppositeIDs: []string{}, MissedIDs: []string{}, CheckIDs: []string{}}
	s.MacroF1, s.Classes = macroF1(s.Confusion)
	for _, po := range pairs {
		if po.p.gold != judge.LabelOpposite {
			s.NonOpposites++
			if po.pred == judge.LabelOpposite {
				s.FalseOpposites++
				s.FalseOppositeIDs = append(s.FalseOppositeIDs, po.p.id)
			}
			continue
		}
		s.Opposites++
		if po.pred == judge.LabelOpposite {
			s.OppositeLabel++
		}
		if po.pred == judge.LabelOpposite || po.unsure {
			s.OppositeOrUnsure++
		}
		switch po.state {
		case judge.CardDiffer:
			s.Differ++
			if po.strict {
				s.Strict++
			}
		case judge.CardCheck:
			s.Check++
			s.CheckIDs = append(s.CheckIDs, po.p.id)
		default:
			s.Missed++
			s.MissedIDs = append(s.MissedIDs, po.p.id)
		}
	}
	s.FalseOppositeRate = ratio(s.FalseOpposites, s.NonOpposites)
	s.OppositeRecall = ratio(s.OppositeLabel, s.Opposites)
	s.OrUnsureRecall = ratio(s.OppositeOrUnsure, s.Opposites)
	return s
}

// cardStats are the claim- and card-level numbers of the natural
// comparisons.
type cardStats struct {
	Claims          int                       `json:"claims"`
	States          map[string]int            `json:"states"`
	Confusion       map[string]map[string]int `json:"confusion"` // gold card → claim state
	CoreOR          int                       `json:"coreOr"`
	CoreAND         int                       `json:"coreAnd"`
	CoreRecallOR    float64                   `json:"coreRecallOr"`
	CoreRecallAND   float64                   `json:"coreRecallAnd"`
	ClearClaims     int                       `json:"clearClaims"`
	ClearCheck      int                       `json:"clearCheck"`
	ClearCheckShare float64                   `json:"clearCheckShare"`
	Items           int                       `json:"items"`
	CheckItems      int                       `json:"checkItems"`
	UnsureOnly      int                       `json:"unsureOnlyItems"`
	CheckRate       float64                   `json:"checkRate"`
	UnsureOnlyShare float64                   `json:"unsureOnlyShare"`
	VisibleOR       float64                   `json:"visibleMedianOr"`  // units on a card per answer, median
	VisibleAND      float64                   `json:"visibleMedianAnd"` // the same with core in both directions
}

func cardsOf(claims []*claimOut, comps []*compOut) cardStats {
	s := cardStats{States: map[string]int{}, Confusion: map[string]map[string]int{}}
	for _, g := range []string{judge.CardDiffer, judge.CardAgree, judge.CardOnlyOne} {
		s.Confusion[g] = map[string]int{}
	}
	for _, co := range claims {
		if co.c.comp.item != nil {
			continue
		}
		s.Claims++
		s.States[co.state]++
		s.Confusion[co.c.gold][co.state]++
		if co.coreOR {
			s.CoreOR++
		}
		if co.coreAND {
			s.CoreAND++
		}
		if co.c.clear {
			s.ClearClaims++
			if co.state == judge.CardCheck {
				s.ClearCheck++
			}
		}
	}
	var or, and []float64
	for _, cp := range comps {
		if cp.c.item != nil {
			continue
		}
		for _, it := range cp.items {
			s.Items++
			if it.Kind == judge.CardCheck {
				s.CheckItems++
				if !slices.ContainsFunc(it.Check, func(c string) bool { return c != judge.CheckUnsure }) {
					s.UnsureOnly++
				}
			}
		}
		for i := range cp.visibleOR {
			or = append(or, float64(cp.visibleOR[i]))
			and = append(and, float64(cp.visibleAND[i]))
		}
	}
	s.CoreRecallOR, s.CoreRecallAND = ratio(s.CoreOR, s.Claims), ratio(s.CoreAND, s.Claims)
	s.ClearCheckShare = ratio(s.ClearCheck, s.ClearClaims)
	s.CheckRate, s.UnsureOnlyShare = ratio(s.CheckItems, s.Items), ratio(s.UnsureOnly, s.Items)
	s.VisibleOR, s.VisibleAND = nearestRank(or, 0.5), nearestRank(and, 0.5)
	return s
}

// nearestRank is the q quantile by nearest rank: the ceil(q·n)-th smallest.
func nearestRank(v []float64, q float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := slices.Clone(v)
	sort.Float64s(s)
	k := int(math.Ceil(q*float64(len(s)))) - 1
	return s[min(max(k, 0), len(s)-1)]
}

// requestStats are the request-level numbers of a set of records.
type requestStats struct {
	Requests     int            `json:"requests"`
	Done         int            `json:"done"`
	Failed       int            `json:"failed"`
	Codes        map[string]int `json:"codes"` // final failures by code
	FirstInvalid int            `json:"firstInvalid"`
	InvalidRate  float64        `json:"invalidRate"`
	Retries      int            `json:"retries"`
	Dropped      int            `json:"dropped"`
	Attempts     int            `json:"attempts"`
	MsP50        float64        `json:"msP50"`
	MsP90        float64        `json:"msP90"`
	AttemptP50   float64        `json:"attemptMsP50"`
	AttemptP90   float64        `json:"attemptMsP90"`
	InSum        int            `json:"inputTokens"`
	OutSum       int            `json:"outputTokens"`
	InMedian     float64        `json:"inputMedian"`
	OutMedian    float64        `json:"outputMedian"`
	ZeroTokens   int            `json:"zeroTokenRequests"`
	Models       map[string]int `json:"models"`
	CLIs         map[string]int `json:"clis"`
}

// invalidFirst reports whether a request's first attempt was invalid
// output: the adapter's judge_bad_output, a refused output, or missed ids.
func invalidFirst(r record) bool {
	if len(r.Attempts) == 0 {
		return false
	}
	a := r.Attempts[0]
	return a.Code == core.ErrCodeJudgeBadOutput || a.Validate != "" || a.Missing > 0
}

func requestsOf(recs []record) requestStats {
	s := requestStats{Codes: map[string]int{}, Models: map[string]int{}, CLIs: map[string]int{}}
	var ms, attemptMs, in, out []float64
	for _, r := range recs {
		s.Requests++
		if r.Status == statusDone {
			s.Done++
		} else {
			s.Failed++
			s.Codes[r.Code]++
		}
		if invalidFirst(r) {
			s.FirstInvalid++
		}
		if len(r.Attempts) > 1 {
			s.Retries++
		}
		var total int64
		ti, to := 0, 0
		for _, a := range r.Attempts {
			s.Attempts++
			total += a.Ms
			attemptMs = append(attemptMs, float64(a.Ms))
			ti += a.In
			to += a.Out
			s.Dropped += a.Dropped
		}
		ms = append(ms, float64(total))
		in, out = append(in, float64(ti)), append(out, float64(to))
		s.InSum += ti
		s.OutSum += to
		if ti+to == 0 {
			s.ZeroTokens++
		}
		s.Models[r.Model]++
		s.CLIs[r.CLI]++
	}
	s.InvalidRate = ratio(s.FirstInvalid, s.Requests)
	s.MsP50, s.MsP90 = nearestRank(ms, 0.5), nearestRank(ms, 0.9)
	s.AttemptP50, s.AttemptP90 = nearestRank(attemptMs, 0.5), nearestRank(attemptMs, 0.9)
	s.InMedian, s.OutMedian = nearestRank(in, 0.5), nearestRank(out, 0.5)
	return s
}

// evidenceStats are the evidence numbers: on pairs with evidence gold
// whose prediction is the gold label, did the judge cite the gold units.
type evidenceStats struct {
	Eligible       int     `json:"eligible"`
	Hit3           int     `json:"hit3"`
	Top1           int     `json:"top1"`
	Hit3Rate       float64 `json:"hit3Rate"`
	Top1Rate       float64 `json:"top1Rate"`
	SameOpposite   int     `json:"sameOrOppositeJudgments"`
	NoEvidence     int     `json:"noEvidence"`
	NoEvidenceRate float64 `json:"noEvidenceRate"`
}

// evidenceOf are the evidence numbers of the natural comparisons, or with
// synth of the perturb items.
func evidenceOf(pairs []*pairOut, comps []*compOut, synth bool) evidenceStats {
	var s evidenceStats
	for _, po := range pairs {
		if po.evOK && po.p.synthetic == synth {
			s.Eligible++
			if po.hit3 {
				s.Hit3++
			}
			if po.top1 {
				s.Top1++
			}
		}
	}
	for _, cp := range comps {
		if (cp.c.item != nil) != synth {
			continue
		}
		for _, j := range cp.judgments {
			if j.Label == judge.LabelSame || j.Label == judge.LabelOpposite {
				s.SameOpposite++
				if len(j.Evidence) == 0 {
					s.NoEvidence++
				}
			}
		}
	}
	s.Hit3Rate, s.Top1Rate = ratio(s.Hit3, s.Eligible), ratio(s.Top1, s.Eligible)
	s.NoEvidenceRate = ratio(s.NoEvidence, s.SameOpposite)
	return s
}

// natural are the pairs of the P0-6 questions; synthetic the perturb
// opposites (kept perturb pairs are in neither).
func natural(pairs []*pairOut) []*pairOut {
	return filterPairs(pairs, func(po *pairOut) bool { return !po.p.synthetic && !po.p.kept })
}

func synthetic(pairs []*pairOut) []*pairOut {
	return filterPairs(pairs, func(po *pairOut) bool { return po.p.synthetic })
}

func filterPairs(pairs []*pairOut, keep func(*pairOut) bool) []*pairOut {
	var out []*pairOut
	for _, po := range pairs {
		if keep(po) {
			out = append(out, po)
		}
	}
	return out
}

// breakdowns split the pairs (natural and synthetic) by language, kind,
// judge AI, group, split and own/others.
func breakdowns(pairs []*pairOut, product bool) map[string]map[string]pairStats {
	dims := map[string]func(*pairOut) []string{
		"lang": func(po *pairOut) []string { return []string{po.p.claim.comp.lang} },
		"kind": func(po *pairOut) []string {
			return []string{map[bool]string{true: "synthetic", false: "natural"}[po.p.synthetic]}
		},
		"group": func(po *pairOut) []string { return po.p.groups },
		"split": func(po *pairOut) []string { return []string{po.p.claim.comp.split} },
	}
	if product {
		dims["judge"] = func(po *pairOut) []string { return []string{po.judge} }
	} else {
		dims["own"] = func(po *pairOut) []string { return []string{map[bool]string{true: "own", false: "others"}[po.own]} }
	}
	all := filterPairs(pairs, func(po *pairOut) bool { return !po.p.kept })
	out := map[string]map[string]pairStats{}
	for dim, f := range dims {
		groups := map[string][]*pairOut{}
		for _, po := range all {
			for _, g := range f(po) {
				groups[g] = append(groups[g], po)
			}
		}
		out[dim] = map[string]pairStats{}
		for g, ps := range groups {
			out[dim][g] = statsOf(ps)
		}
	}
	return out
}
