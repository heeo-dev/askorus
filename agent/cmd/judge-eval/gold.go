//go:build judgeeval

package main

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// The gold engine: an in-process core.Judger that answers from the gold
// labels, with no model call. It goes through the whole loop (Prompt,
// Validate, Judgments, Aggregate), so a run with it checks the tool end to
// end: every gold opposite must come out shown as differ.

// goldModel is the gold engine's model and family.
const goldModel = "gold"

// goldUnit is the gold verdict on one unit of the From answer.
type goldUnit struct {
	label    string
	evidence []int // units of the Against answer, by n
}

// goldJudger judges one job's task.
type goldJudger struct {
	units map[int]goldUnit // by n of the From answer's unit; the rest are absent and not core
}

var _ core.Judger = goldJudger{}

func (g goldJudger) JudgeDefault() core.AskModel { return core.AskModel{Model: goldModel} }

func (g goldJudger) Family(model string) string {
	if model == goldModel {
		return goldModel
	}
	return ""
}

// goldVerdict is one judgment in core.JudgeSchemaV1's field order.
type goldVerdict struct {
	ID       string   `json:"id"`
	Core     bool     `json:"core"`
	Evidence []string `json:"evidence"`
	Reason   string   `json:"reason"`
	Label    string   `json:"label"`
	Unsure   bool     `json:"unsure"`
}

// Judge answers every unit of the task: the gold label of a claim's unit,
// core, with the gold evidence when the job carries it; absent and not
// core for a unit of no claim.
func (g goldJudger) Judge(ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return core.JudgeResult{}, core.Fail(core.ErrCodeInternal, "gold judge: empty prompt")
	}
	if err := ctx.Err(); err != nil {
		return core.JudgeResult{}, core.Fail(core.ErrCodeJudgeTimeout, "")
	}
	out := struct {
		Judgments []goldVerdict `json:"judgments"`
	}{Judgments: []goldVerdict{}}
	for _, u := range req.Task.Units {
		n, ok := strings.CutPrefix(u.ID, "a")
		if !ok || u.ID == "" {
			continue
		}
		k, _ := strconv.Atoi(n)
		v := goldVerdict{ID: u.ID, Evidence: []string{}, Reason: "gold", Label: judge.LabelAbsent}
		if gu, ok := g.units[k]; ok {
			v.Core, v.Label = true, gu.label
			for _, e := range gu.evidence {
				v.Evidence = append(v.Evidence, "b"+strconv.Itoa(e))
			}
		}
		out.Judgments = append(out.Judgments, v)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return core.JudgeResult{}, core.Fail(core.ErrCodeInternal, "gold judge: marshal")
	}
	return core.JudgeResult{Output: b, Model: goldModel}, nil
}

// goldRank orders labels when a unit is in two claims: opposite wins.
func goldRank(label string) int {
	switch label {
	case judge.LabelOpposite:
		return 3
	case judge.LabelSame:
		return 2
	}
	return 1
}

// goldPut sets the gold verdict of unit n unless a stronger one is there.
func goldPut(m map[int]goldUnit, n int, gu goldUnit) {
	if old, ok := m[n]; ok && goldRank(old.label) >= goldRank(gu.label) {
		return
	}
	m[n] = gu
}

// goldFor is the gold judger of a job: the labels of its From answer's
// claims against its Against answer, the evidence gold too with
// withEvidence.
func (p *prepared) goldFor(jb *job, withEvidence bool, items map[string]*perturbItem) goldJudger {
	g := goldJudger{units: map[int]goldUnit{}}
	from, against := jb.req.From, jb.req.Against
	if jb.item != "" {
		it := items[jb.item]
		if it == nil {
			return g
		}
		for _, c := range it.Claims {
			fit := p.fit[claimKey(it.Q, it.From, c.N)]
			gu := goldUnit{label: c.Label}
			if withEvidence && c.Synthetic {
				for _, id := range it.evidence {
					gu.evidence = append(gu.evidence, unitN(id))
				}
			}
			for _, id := range fit.Units {
				goldPut(g.units, unitN(id), gu)
			}
		}
		return g
	}
	for _, c := range p.claimsOf(jb.q, from) {
		pr, ok := p.pairByID[pairID(jb.q, from, c.N, against)]
		if !ok {
			continue
		}
		gu := goldUnit{label: pr.Label}
		if withEvidence && pr.Label != judge.LabelAbsent {
			for _, id := range p.evUnits[pr.ID] {
				gu.evidence = append(gu.evidence, unitN(id))
			}
		}
		for _, id := range p.fit[claimKey(jb.q, from, c.N)].Units {
			goldPut(g.units, unitN(id), gu)
		}
	}
	return g
}
