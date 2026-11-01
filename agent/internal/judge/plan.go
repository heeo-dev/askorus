package judge

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// Who judges what (spec part 3). A request is the judged units of answer
// X, answer Y as context and a judge J. J is a PC AI in Spec.Judges that
// is neither X's nor Y's program (Agent) nor of X's or Y's company
// (Family), and both directions of a pair go to the same J. With three
// AIs each pair has one such judge, the third AI: six requests, two per
// AI. With more answers (pasted ones, P2-4) the pairs are spread over the
// judges as evenly as they can be.

// planCodeInternal is api.CodeInternal. Plan and Validate give it when
// the caller asks for what this build does not do: a judge-eval shape or
// label set, or answers whose ids repeat. The product never does.
const planCodeInternal = "internal"

// planHandoff maps the official apps whose answers can be pasted (P2-4)
// to their companies.
var planHandoff = map[string]string{
	"chatgpt": "openai",
	"claude":  "anthropic", // the Claude app
	"gemini":  "google",
}

// HandoffFamily is the company of an official app whose answer was pasted
// (P2-4): chatgpt is openai, claude (the Claude app) anthropic and gemini
// google. Any other name, "ChatGPT app" or "Claude Code" too, is of
// unknown company: "", so Plan trusts no judge with it (fail closed, 11-01
// user decision; the P2-4 API takes only the table's names). PC answers
// get their family from their adapter instead.
func HandoffFamily(app string) string {
	return planHandoff[strings.ToLower(strings.TrimSpace(app))]
}

// Plan makes the requests of a comparison (part 3). For every ordered pair
// of ready answers (X, Y), one request asks a judge how Y relates to each
// of X's judged units; a direction whose X has no judged unit has nothing
// to judge and gets no request. The judge is in s.Judges and is neither
// X's nor Y's AI (Agent) nor of X's or Y's company: its Family, and for a
// pasted answer also its app's by the handoff table (HandoffFamily). An
// empty agent or family matches every other, so an AI or answer of
// unknown company is never trusted to differ. Both directions of a pair go
// to the same judge. A judge whose own ask ended with ask_failed (often a usage
// limit) is taken only when no other may take the pair. Among the rest
// the pairs are spread as evenly as they can be, ties going to the earlier
// judge in s.Judges: three AIs make six requests, two per AI, each pair
// judged by the third AI, and three PC answers with a pasted one make
// twelve, four per AI. A judge that wrote no answer of the comparison is
// JudgeOnly. For a pair no judge may take, each direction gets a record
// {Judge: "", Status: failed, Error: no_judge}.
//
// Requests are numbered r1, r2, … pair by pair in answer order, X→Y
// before Y→X, and start queued with the judge's model. With fewer than two
// ready answers Plan returns an *Error with code need_two_answers. When no
// pair has a judge it returns the records and an *Error with code
// no_judge. This build plans ShapeDirected with LabelsV1 only (an empty
// Shape and Labels mean those); judge-eval adds the others.
func Plan(s Spec) ([]Request, error) {
	if !planBuilt(s.Shape, s.Labels) {
		return nil, &Error{Code: planCodeInternal, Message: "this build judges only shape directed with labels v1"}
	}
	var ready []Answer
	for _, a := range s.Answers {
		if a.Status == AnswerReady {
			ready = append(ready, a)
		}
	}
	if len(ready) < 2 {
		return nil, &Error{Code: core.ErrCodeNeedTwoAnswers, Message: "fewer than two answers to compare"}
	}
	// Every answer's id, a missing one's too: BuildTask and Aggregate find
	// answers by id.
	ids := map[string]bool{}
	for _, a := range s.Answers {
		if a.ID == "" || ids[a.ID] {
			return nil, &Error{Code: planCodeInternal, Message: "answer ids are empty or repeat"}
		}
		ids[a.ID] = true
	}
	judges := planJudges(s.Judges)
	var pairs []planPair
	for i := range ready {
		for j := i + 1; j < len(ready); j++ {
			p := planPair{x: i, y: j, toY: planHasJudged(ready[i]), toX: planHasJudged(ready[j])}
			if p.toY || p.toX {
				p.judges = planEligible(judges, ready[i], ready[j])
				pairs = append(pairs, p)
			}
		}
	}
	planAssign(pairs, len(judges))

	reqs := []Request{}
	judged := false
	add := func(from, against Answer, j int) {
		r := Request{ID: "r" + strconv.Itoa(len(reqs)+1), From: from.ID, Against: against.ID, Status: RequestQueued}
		if j < 0 {
			r.Status, r.Error = RequestFailed, &Error{Code: core.ErrCodeNoJudge}
		} else {
			r.Judge, r.Model = judges[j].Agent, judges[j].Model
			r.JudgeOnly = !planWrote(ready, judges[j].Agent)
			judged = true
		}
		reqs = append(reqs, r)
	}
	for _, p := range pairs {
		if p.toY {
			add(ready[p.x], ready[p.y], p.judge)
		}
		if p.toX {
			add(ready[p.y], ready[p.x], p.judge)
		}
	}
	if !judged && len(reqs) > 0 {
		return reqs, &Error{Code: core.ErrCodeNoJudge, Message: "no AI may judge these answers"}
	}
	return reqs, nil
}

// planBuilt reports whether this build judges with that shape and label
// set: only the product's, ShapeDirected with LabelsV1.
func planBuilt(shape, labels string) bool {
	return (shape == "" || shape == ShapeDirected) && (labels == "" || labels == LabelsV1)
}

// planPair is a pair of ready answers and the judge that takes it.
type planPair struct {
	x, y     int   // the answers, as indexes into the ready answers; x comes first
	toY, toX bool  // x's units are judged against y; y's against x
	judges   []int // the judges that may take the pair, as indexes into the judges, in order
	judge    int   // the one that takes it, or -1
}

// planJudges are the judges in order, each AI once.
func planJudges(in []JudgeAI) []JudgeAI {
	var out []JudgeAI
	for _, j := range in {
		if j.Agent == "" || planListed(out, j.Agent) {
			continue
		}
		out = append(out, j)
	}
	return out
}

// planListed reports whether agent is among judges already.
func planListed(judges []JudgeAI, agent string) bool {
	for _, j := range judges {
		if strings.EqualFold(j.Agent, agent) {
			return true
		}
	}
	return false
}

// planHasJudged reports whether a has a unit to judge.
func planHasJudged(a Answer) bool {
	for _, u := range a.Units {
		if u.Judged {
			return true
		}
	}
	return false
}

// planWrote reports whether the AI agent wrote one of the ready answers
// on the PC. A pasted answer is the app's, not the PC AI's.
func planWrote(ready []Answer, agent string) bool {
	for _, a := range ready {
		if a.Via != ViaHandoff && strings.EqualFold(a.Agent, agent) {
			return true
		}
	}
	return false
}

// planEligible lists the judges that may take the pair (x, y), in order.
// A judge whose ask ended with ask_failed is listed only when no other
// judge may take the pair.
func planEligible(judges []JudgeAI, x, y Answer) []int {
	var best []int
	bestRank := 2
	for k, j := range judges {
		if !planMay(j, x) || !planMay(j, y) {
			continue
		}
		rank := 0
		if j.AskCode == core.ErrCodeAskFailed {
			rank = 1
		}
		if rank < bestRank {
			best, bestRank = nil, rank
		}
		if rank == bestRank {
			best = append(best, k)
		}
	}
	return best
}

// planMay reports whether judge j may judge answer a: another program of
// another company, both known. A pasted answer is of its app's company
// too, by the handoff table, whatever its Family says, so a slip of the
// caller never lets a company judge its own app's answer.
func planMay(j JudgeAI, a Answer) bool {
	if j.Agent == "" || a.Agent == "" || j.Family == "" || strings.EqualFold(j.Agent, a.Agent) {
		return false
	}
	families := []string{a.Family}
	if a.Via == ViaHandoff {
		families = []string{HandoffFamily(a.Agent)}
		if a.Family != "" {
			families = append(families, a.Family)
		}
	}
	for _, f := range families {
		if f == "" || strings.EqualFold(j.Family, f) {
			return false
		}
	}
	return true
}

// planAssign gives every pair one of its judges, so that the judges' loads
// (requests taken) are as even as they can be. When every pair makes as
// many requests as the others (two, or one each) it is the semi-matching
// algorithm ASM1 (Harvey, Ladner, Lovász and Tamir 2006): pair by pair, it
// searches the judges the pair can reach, directly or by moving pairs
// already given to one judge to another of theirs, takes the least loaded
// one (the earlier in order on a tie) and moves the pairs on the way one
// step along. That leaves no way to lower one judge's load by raising a
// lighter one's, so the loads are as even as possible: the most loaded
// judge has as few pairs as can be. When a pair whose one answer has no
// judged unit makes one request beside pairs that make two, planWeighed
// evens the requests instead. Everything goes in a fixed order, so the
// same input gives the same plan.
func planAssign(pairs []planPair, nJudges int) {
	alike := map[int]bool{} // requests of the pairs a judge may take
	for _, p := range pairs {
		if len(p.judges) > 0 {
			alike[planRequests(p)] = true
		}
	}
	if len(alike) > 1 && planWeighed(pairs, nJudges) {
		return
	}
	planASM1(pairs, nJudges)
}

// planRequests is the number of requests pair p makes.
func planRequests(p planPair) int {
	n := 0
	if p.toY {
		n++
	}
	if p.toX {
		n++
	}
	return n
}

// planMostLoads bounds the loads planWeighed keeps at one step: more than
// that (five judges or more and many pairs) and ASM1 decides.
const planMostLoads = 1 << 14

// planWeighed gives every pair one of its judges so that, counted in
// requests, the busiest judge has as few as can be and then the squared
// loads add up to the least: it goes pair by pair through every load the
// judges can reach (the loads are small, and so are their sums), keeping
// for each the first assignment in judge order, so ties go to earlier
// judges. It reports false, assigning nothing, past planMostLoads loads.
func planWeighed(pairs []planPair, nJudges int) bool {
	type state struct {
		load  []uint8
		judge []int8 // of each pair so far, -1 for none
	}
	cur := map[string]state{string(make([]byte, nJudges)): {load: make([]uint8, nJudges)}}
	for p := range pairs {
		next := map[string]state{}
		keep := func(st state, k int) {
			load, judge := slices.Clone(st.load), append(slices.Clone(st.judge), int8(k))
			if k >= 0 {
				load[k] += uint8(planRequests(pairs[p]))
			}
			if old, ok := next[string(load)]; !ok || slices.Compare(judge, old.judge) < 0 {
				next[string(load)] = state{load, judge}
			}
		}
		for _, st := range cur {
			if len(pairs[p].judges) == 0 {
				keep(st, -1)
			}
			for _, k := range pairs[p].judges {
				keep(st, k)
			}
		}
		if len(next) > planMostLoads {
			return false
		}
		cur = next
	}
	var best state
	bestMost, bestSquares := -1, 0
	for _, st := range cur {
		most, squares := 0, 0
		for _, l := range st.load {
			most, squares = max(most, int(l)), squares+int(l)*int(l)
		}
		if bestMost < 0 || cmp.Or(cmp.Compare(most, bestMost), cmp.Compare(squares, bestSquares), slices.Compare(st.judge, best.judge)) < 0 {
			best, bestMost, bestSquares = st, most, squares
		}
	}
	for p := range pairs {
		pairs[p].judge = int(best.judge[p])
	}
	return true
}

// planASM1 is planAssign for pairs that all make as many requests.
func planASM1(pairs []planPair, nJudges int) {
	load := make([]int, nJudges)
	via := make([]int, nJudges) // the pair a judge was reached through
	seen := make([]bool, nJudges)
	queue := make([]int, 0, nJudges)
	for p := range pairs {
		pairs[p].judge = -1
		if len(pairs[p].judges) == 0 {
			continue
		}
		clear(seen)
		queue = queue[:0]
		for _, k := range pairs[p].judges {
			seen[k], via[k] = true, p
			queue = append(queue, k)
		}
		for i := 0; i < len(queue); i++ {
			for q := range pairs[:p] {
				if pairs[q].judge != queue[i] {
					continue
				}
				for _, m := range pairs[q].judges {
					if !seen[m] {
						seen[m], via[m] = true, q
						queue = append(queue, m)
					}
				}
			}
		}
		best := -1
		for k := range load {
			if seen[k] && (best < 0 || load[k] < load[best]) {
				best = k
			}
		}
		for k := best; ; {
			q := via[k]
			prev := pairs[q].judge
			pairs[q].judge = k
			if q == p {
				break
			}
			k = prev
		}
		load[best]++
	}
}
