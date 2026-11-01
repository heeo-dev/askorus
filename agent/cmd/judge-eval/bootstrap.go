//go:build judgeeval

package main

import (
	"math/rand/v2"
	"slices"
	"sort"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Bootstrap (plan part 2, bootstrap.go): the run's questions are drawn
// with replacement B times with a seeded PCG, and four numbers are
// computed again on each draw. The 95% interval is the 2.5th and 97.5th
// percentile by nearest rank. One question gives a point interval.

// interval is a number and its bootstrap interval.
type interval struct {
	Point float64 `json:"point"`
	Lo    float64 `json:"lo"`
	Hi    float64 `json:"hi"`
}

// bootQuestion is what one question adds to the four numbers.
type bootQuestion struct {
	conf        [3][5]int // natural pairs: gold × prediction, in goldClasses × predClasses order
	missedNat   int
	missedSyn   int
	clearFalse  int // clear pairs predicted opposite
	clearNonOpp int // clear pairs whose gold is not opposite
	claims      int
	coreOR      int
}

func (b *bootQuestion) add(o *bootQuestion) {
	for i := range b.conf {
		for j := range b.conf[i] {
			b.conf[i][j] += o.conf[i][j]
		}
	}
	b.missedNat += o.missedNat
	b.missedSyn += o.missedSyn
	b.clearFalse += o.clearFalse
	b.clearNonOpp += o.clearNonOpp
	b.claims += o.claims
	b.coreOR += o.coreOR
}

// bootNames are the numbers the bootstrap recomputes.
var bootNames = []string{"missedNatural", "missedSynthetic", "macroF1", "clearFalseOppositeRate", "coreRecallOr"}

func (b *bootQuestion) values() []float64 {
	conf := map[string]map[string]int{}
	for i, g := range goldClasses {
		conf[g] = map[string]int{}
		for j, p := range predClasses {
			conf[g][p] = b.conf[i][j]
		}
	}
	f1, _ := macroF1(conf)
	return []float64{float64(b.missedNat), float64(b.missedSyn), f1, ratio(b.clearFalse, b.clearNonOpp), ratio(b.coreOR, b.claims)}
}

// bootstrap computes the intervals of a view.
func bootstrap(v *viewResult, b int, seed uint64) map[string]interval {
	per := map[int]*bootQuestion{}
	get := func(q int) *bootQuestion {
		if per[q] == nil {
			per[q] = &bootQuestion{}
		}
		return per[q]
	}
	for _, po := range v.pairs {
		if po.p.kept {
			continue
		}
		bq := get(po.p.claim.comp.q)
		missed := po.p.gold == judge.LabelOpposite && po.state != judge.CardDiffer && po.state != judge.CardCheck
		if po.p.synthetic {
			if missed {
				bq.missedSyn++
			}
			continue
		}
		bq.conf[slices.Index(goldClasses, po.p.gold)][slices.Index(predClasses, po.pred)]++
		if missed {
			bq.missedNat++
		}
		if slices.Contains(po.p.groups, groupClear) && po.p.gold != judge.LabelOpposite {
			bq.clearNonOpp++
			if po.pred == judge.LabelOpposite {
				bq.clearFalse++
			}
		}
	}
	for _, co := range v.claims {
		if co.c.comp.item != nil {
			continue
		}
		bq := get(co.c.comp.q)
		bq.claims++
		if co.coreOR {
			bq.coreOR++
		}
	}
	qs := make([]int, 0, len(per))
	for q := range per {
		qs = append(qs, q)
	}
	sort.Ints(qs)
	var all bootQuestion
	for _, q := range qs {
		all.add(per[q])
	}
	point := all.values()
	out := map[string]interval{}
	if len(qs) == 0 || b <= 0 {
		for i, name := range bootNames {
			out[name] = interval{Point: point[i], Lo: point[i], Hi: point[i]}
		}
		return out
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	draws := make([][]float64, len(bootNames))
	for range b {
		var s bootQuestion
		for range qs {
			s.add(per[qs[rng.IntN(len(qs))]])
		}
		for i, x := range s.values() {
			draws[i] = append(draws[i], x)
		}
	}
	for i, name := range bootNames {
		lo, hi := bootInterval(draws[i])
		out[name] = interval{Point: point[i], Lo: lo, Hi: hi}
	}
	return out
}

// bootInterval is the 95% interval of draws: the 2.5th and 97.5th
// percentile by nearest rank (draws 50 and 1,950 of 2,000).
func bootInterval(draws []float64) (lo, hi float64) {
	return nearestRank(draws, 0.025), nearestRank(draws, 0.975)
}
