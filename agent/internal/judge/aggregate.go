package judge

// Cards (part 5, 결과 모으기). Aggregate is the one place where judgments
// become the 정리 tab. The API and judge-eval call it on the same input,
// so what judge-eval measures is what the phone shows. It keeps nothing
// between calls: each call makes every card again from every judgment so
// far, and only the item keys carry over, through in.Prev.
//
// A judgment is one directed relation: unit u of answer X against the
// whole answer Y, with the units of Y it relied on as evidence. Two kinds
// of links come out of judgments:
//   - an opposite judgment links u to each evidence unit, or, without
//     evidence, to Y's whole answer (unless evidence already links u with a
//     unit of Y, in either direction);
//   - a same judgment with evidence links u to each evidence unit.
//
// The cards are made in this order, and a unit is on one item at most:
//  1. 다른 말: each connected group of opposite links is one item. Each
//     answer in the group starts on a side of its own, and a same link
//     joins two sides unless an opposite link runs between them, links in
//     reading order of their units. Once every request has ended, a unit
//     outside the group that a same link ties to one side joins that side,
//     unless its answer already stands on another side (C2-5: P0-6 Q12
//     reads [claude] against [codex, opencode]). Only units of the group
//     draw others in, so joining does not chain.
//  2. 확인 필요: a shown unit with an enabled check that counts on a
//     judgment (결정 2). Since S6 (11-01 user decision 2) the unsure mark
//     counts on an opposite (P3) or unclear judgment only: an unsure same
//     counts as same (P1) and an unsure absent as absent (P2, back to 한
//     AI만), unless that judgment's number check fires (numberMismatch on a
//     same, numbersAbsent on an absent). The number check is then the reason
//     when it is enabled; when it is not, the unsure mark stands in for it.
//  3. 같은 말: in reading order, a shown unit with a same judgment stands for
//     an item, and only units linked to it directly join (no chaining): the
//     first evidence unit of each of its same judgments, and units whose
//     same judgment against its answer cites it. A unit with an enabled
//     check never joins.
//  4. 한 AI만: the shown units left, whose judgments all say absent.
//
// Before every request has ended only 다른 말 is made, from the opposite
// groups alone: a later judgment may still cite a unit as opposing
// evidence or tie it to another side, and an item that jumped from one
// card to another would be worse than cards that wait a few seconds.

import (
	"cmp"
	"slices"
	"strconv"
)

// Aggregate turns the judgments of a comparison into the cards (part 5).
// It is pure, and the API and judge-eval both call it, so what judge-eval
// measures is what the phone shows.
//
// It uses only usable judgments: on a judged unit, against another ready
// answer, with a known label, and not by the author's or the against
// answer's AI. Evidence keeps the against answer's units, each once, at
// most MaxEvidence. It then sets every judgment's flags, enabled or not:
// unsure (or the label unclear); noEvidence, same or opposite without
// evidence; crossDirection, u same as v with v as evidence while v is
// opposite to u with u as evidence, or the other way round;
// numberMismatch, same while the numbers of u and of its evidence units
// (of the whole other answer when there is none) Mismatch; numbersAbsent,
// absent while u and the whole other answer Mismatch. Only the checks in
// in.Checks that count put a unit on the 확인 필요 card or into Item.Check:
// an unsure mark counts on an opposite or unclear judgment, and on a same
// or absent one only when its number check (numberMismatch, numbersAbsent)
// fired and is not enabled itself (S6, see counts).
//
// A unit is shown when any judge called it core or it is in an opposite
// judgment, as the judged unit or as evidence. The cards do not overlap
// and are made in this order:
//   - differ: each connected group of opposite edges (a unit to each of
//     its evidence units, or to the whole other answer when there is none)
//     is one item. Units joined by same with evidence stand on one side,
//     and, once in.Ended, a unit of another AI joins a side only when that
//     AI is on no other side (P0-6 Q12: [claude] against [codex,
//     opencode]). Mismatch between the sides sets NumbersDiffer, and
//     Unmatched against the other sides gives each side's NumberSpans.
//   - check: no opposite, but an enabled check that counts fired on a
//     judgment of the unit (결정 2), so a same whose numbers do not fit
//     never hides under 같은 말.
//   - agree: a unit with a same judgment represents an item, and only the
//     units linked to it directly join it, with no chaining: two or more
//     AIs, none against.
//   - onlyOne: every usable judgment is absent, unsure ones too unless
//     numbersAbsent fired (or unclear, when unsure is not enabled).
//
// Items come in CardOrder: differ items by their first unit, agree items
// with more AIs first, the others in reading order. A side lists its
// representative first.
//
// Missing names the answers, not on the item, that its units could not be
// compared with because their requests failed or returned nothing for
// them: the units of the opposite edges of a differ item, the
// representative of any other. Units with no usable judgment are
// notCompared and never go to the check card; units after the cap are
// notJudged. Differ items are made as soon as they appear; the other cards
// only once in.Ended.
//
// Keys: "d:" and the group's first opposite judgment, in the order
// judgments came: its unit and first evidence unit ("d:claude:3~codex:2"),
// or the whole answer it opposes without evidence ("d:claude:3~codex");
// "c:", "a:" or "o:" and the representative unit. An item keeps a key of
// in.Prev of its kind when that is its own key, or else the first such key
// whose item shared a unit with it, so keys stay while items grow or merge
// (in.Prev must hold the last items of every card, see AggregateInput).
// With the P0-6 gold labels as judgments (all core, no evidence) and the
// unsure check alone the claims come out 19 differ, 273 agree and 15
// onlyOne; with DefaultChecks the number checks, against the whole other
// answer, send 23 of them to check (19, 23, 253, 12).
func Aggregate(in AggregateInput) Summary {
	a := aggNew(in)
	a.judgments()
	a.differ()
	if in.Ended {
		a.check()
		a.agree()
		a.onlyOne()
	}
	a.keepKeys(in.Prev)
	return a.summary()
}

// aggAnswer is one answer of the comparison.
type aggAnswer struct {
	id, agent string
	ready     bool  // it has an answer (not AnswerMissing)
	units     []int // its units, in order
}

// aggUnit is one unit. Units are numbered in reading order (the answers in
// the turn's order, then their units in order), and items and sides are
// sorted by that number.
type aggUnit struct {
	id, text string
	judged   bool
	ans      int
}

// aggEdge is an opposite edge between two nodes. The nodes are the units,
// by number, and after them one node for each whole answer an opposite
// without evidence points at.
type aggEdge struct{ a, b int }

// aggPos is one side of an item while it is made.
type aggPos struct {
	answers []int // in the turn's order
	units   []int // the representative first: on 다른 말 the group's own units in reading order, then those that joined
	whole   []int // answers on this side only through their whole answer
	spans   []NumberSpan
}

// aggItem is an item while it is made.
type aggItem struct {
	kind, key     string
	first         int   // the unit it is sorted by
	basis         []int // the units whose comparisons Missing reports on
	pos           []aggPos
	numbersDiffer bool
	check         []string
	missing       []string
}

// aggState is one call of Aggregate.
type aggState struct {
	in       AggregateInput
	answers  []aggAnswer
	ansByID  map[string]int
	units    []aggUnit
	unitByID map[string]int
	enabled  map[string]bool
	requests [][][]string // statuses of the requests judging answer x against answer y

	js       []Judgment // the usable judgments: evidence cleaned, flags set
	jUnit    []int      // the judged unit of each
	jAgainst []int      // the answer each was judged against
	ofUnit   [][]int    // each unit's usable judgments, in the order they came
	tagged   []bool     // an enabled check that counts fired on the judgment

	nums    [][]Quantity // each unit's quantities, read once
	numRead []bool
	ansNums [][]Quantity // each answer's quantities, all its units together
	ansRead []bool
	ansNumM []numMatcher // each answer's quantities ready to compare with, made when first needed
	ansNumR []bool

	taken []bool // the unit is on an item
	items []aggItem
}

func aggNew(in AggregateInput) *aggState {
	a := &aggState{in: in, ansByID: map[string]int{}, unitByID: map[string]int{}, enabled: map[string]bool{}}
	for _, c := range in.Checks {
		a.enabled[c] = true
	}
	for i := range in.Answers {
		x := &in.Answers[i]
		k := len(a.answers)
		a.answers = append(a.answers, aggAnswer{id: x.ID, agent: x.Agent, ready: x.Status != AnswerMissing})
		if _, dup := a.ansByID[x.ID]; !dup {
			a.ansByID[x.ID] = k
		}
		for _, u := range x.Units {
			if _, dup := a.unitByID[u.ID]; !dup {
				a.unitByID[u.ID] = len(a.units)
			}
			a.answers[k].units = append(a.answers[k].units, len(a.units))
			a.units = append(a.units, aggUnit{id: u.ID, text: u.Text, judged: u.Judged, ans: k})
		}
	}
	n := len(a.answers)
	a.requests = make([][][]string, n)
	for x := range a.requests {
		a.requests[x] = make([][]string, n)
	}
	for _, r := range in.Requests {
		x, okX := a.ansByID[r.From]
		y, okY := a.ansByID[r.Against]
		if okX && okY && x != y {
			a.requests[x][y] = append(a.requests[x][y], r.Status)
		}
	}
	a.nums = make([][]Quantity, len(a.units))
	a.numRead = make([]bool, len(a.units))
	a.ansNums = make([][]Quantity, n)
	a.ansRead = make([]bool, n)
	a.ansNumM = make([]numMatcher, n)
	a.ansNumR = make([]bool, n)
	a.taken = make([]bool, len(a.units))
	return a
}

// judgments keeps the usable judgments and sets their flags.
func (a *aggState) judgments() {
	a.ofUnit = make([][]int, len(a.units))
	for _, j := range a.in.Judgments {
		u, ok := a.unitByID[j.Unit]
		if !ok || !a.units[u].judged {
			continue
		}
		x := a.units[u].ans
		y, ok := a.ansByID[j.Against]
		if !ok || y == x || !a.answers[x].ready || !a.answers[y].ready {
			continue
		}
		if j.Judge != "" && (j.Judge == a.answers[x].agent || j.Judge == a.answers[y].agent) {
			continue // the planner never asks an AI about its own answer
		}
		switch j.Label {
		case LabelSame, LabelOpposite, LabelAbsent, LabelUnclear:
		default:
			continue
		}
		ev := make([]string, 0, MaxEvidence)
		for _, e := range j.Evidence {
			v, ok := a.unitByID[e]
			if ok && a.units[v].ans == y && !slices.Contains(ev, e) && len(ev) < MaxEvidence {
				ev = append(ev, e)
			}
		}
		j.Evidence = ev
		j.Flags = []string{}
		a.ofUnit[u] = append(a.ofUnit[u], len(a.js))
		a.js = append(a.js, j)
		a.jUnit = append(a.jUnit, u)
		a.jAgainst = append(a.jAgainst, y)
	}
	a.tagged = make([]bool, len(a.js))
	for k := range a.js {
		a.js[k].Flags = a.flags(k)
		for _, f := range a.js[k].Flags {
			a.tagged[k] = a.tagged[k] || a.counts(k, f)
		}
	}
}

// flags are the checks that fire on judgment k, in AllChecks order.
func (a *aggState) flags(k int) []string {
	j := &a.js[k]
	u, y := a.jUnit[k], a.jAgainst[k]
	out := []string{}
	if j.Unsure || j.Label == LabelUnclear {
		out = append(out, CheckUnsure)
	}
	if (j.Label == LabelSame || j.Label == LabelOpposite) && len(j.Evidence) == 0 {
		out = append(out, CheckNoEvidence)
	}
	if a.crossDirection(k) {
		out = append(out, CheckCrossDirection)
	}
	switch j.Label {
	case LabelSame:
		// The numbers of the units the judge called the same; with none
		// named, those of the whole other answer.
		mismatch := false
		if len(j.Evidence) > 0 {
			var other []Quantity
			for _, e := range j.Evidence {
				other = append(other, a.unitNums(a.unitByID[e])...)
			}
			mismatch = Mismatch(a.unitNums(u), other)
		} else {
			mismatch = a.answerMatch(y).mismatch(a.unitNums(u))
		}
		if mismatch {
			out = append(out, CheckNumberMismatch)
		}
	case LabelAbsent:
		if a.answerMatch(y).mismatch(a.unitNums(u)) {
			out = append(out, CheckNumbersAbsent)
		}
	}
	return out
}

// crossDirection reports whether the other direction contradicts judgment
// k: u is same as v (citing v) while v is opposite to u (citing u), or the
// other way round.
func (a *aggState) crossDirection(k int) bool {
	var want string
	switch a.js[k].Label {
	case LabelSame:
		want = LabelOpposite
	case LabelOpposite:
		want = LabelSame
	default:
		return false
	}
	u := a.jUnit[k]
	for _, e := range a.js[k].Evidence {
		for _, m := range a.ofUnit[a.unitByID[e]] {
			if a.jAgainst[m] == a.units[u].ans && a.js[m].Label == want && slices.Contains(a.js[m].Evidence, a.units[u].id) {
				return true
			}
		}
	}
	return false
}

// counts reports whether flag f of judgment k may put its unit on the 확인
// 필요 card and into Item.Check: f is enabled, and for the unsure mark the
// rule of S6 (11-01 user decision 2). On an opposite (P3) or unclear
// judgment it counts. On a same (P1) or absent (P2) one it counts only when
// that judgment's number check fired (numberMismatch on a same,
// numbersAbsent on an absent), and then only while that check is not
// enabled: an enabled number check counts itself and is the reason the
// phone shows, not 'unsure'. Either way the unit lands on the same card.
func (a *aggState) counts(k int, f string) bool {
	if !a.enabled[f] {
		return false
	}
	if f != CheckUnsure {
		return true
	}
	j := &a.js[k]
	var number string
	switch j.Label {
	case LabelSame:
		number = CheckNumberMismatch
	case LabelAbsent:
		number = CheckNumbersAbsent
	default:
		return true
	}
	return slices.Contains(j.Flags, number) && !a.enabled[number]
}

// unitNums are the quantities in unit u's text.
func (a *aggState) unitNums(u int) []Quantity {
	if !a.numRead[u] {
		a.nums[u], a.numRead[u] = Numbers(a.units[u].text), true
	}
	return a.nums[u]
}

// answerNums are the quantities in all units of answer y.
func (a *aggState) answerNums(y int) []Quantity {
	if !a.ansRead[y] {
		var all []Quantity
		for _, u := range a.answers[y].units {
			all = append(all, a.unitNums(u)...)
		}
		a.ansNums[y], a.ansRead[y] = all, true
	}
	return a.ansNums[y]
}

// answerMatch is answerNums(y) made ready once to compare units with: a
// large answer is indexed, not scanned for each unit.
func (a *aggState) answerMatch(y int) numMatcher {
	if !a.ansNumR[y] {
		a.ansNumM[y], a.ansNumR[y] = numMatchFor(a.answerNums(y), len(a.units)*numScanMost), true
	}
	return a.ansNumM[y]
}

// differ makes the 다른 말 items.
func (a *aggState) differ() {
	nu := len(a.units)
	// Units that evidence already ties to a unit of an answer by an
	// opposite, in either direction: an opposite without evidence adds no
	// whole-answer node for them.
	tied := map[[2]int]bool{}
	for k, j := range a.js {
		if j.Label != LabelOpposite {
			continue
		}
		for _, e := range j.Evidence {
			v := a.unitByID[e]
			tied[[2]int{a.jUnit[k], a.units[v].ans}] = true
			tied[[2]int{v, a.units[a.jUnit[k]].ans}] = true
		}
	}
	nodeAns := make([]int, nu)
	for u := range a.units {
		nodeAns[u] = a.units[u].ans
	}
	var edges []aggEdge
	for k, j := range a.js {
		if j.Label != LabelOpposite {
			continue
		}
		u, y := a.jUnit[k], a.jAgainst[k]
		for _, e := range j.Evidence {
			edges = append(edges, aggEdge{u, a.unitByID[e]})
		}
		if len(j.Evidence) == 0 && !tied[[2]int{u, y}] {
			nodeAns = append(nodeAns, y)
			edges = append(edges, aggEdge{u, len(nodeAns) - 1})
		}
	}
	if len(edges) == 0 {
		return
	}

	// Connected groups. The root of a group is its smallest node, which is
	// its first unit: every edge has a judged unit, and units come before
	// whole-answer nodes.
	parent := make([]int, len(nodeAns))
	for i := range parent {
		parent[i] = i
	}
	find := func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	for _, e := range edges {
		ra, rb := find(e.a), find(e.b)
		if ra != rb {
			parent[max(ra, rb)] = min(ra, rb)
		}
	}
	at := map[int]int{} // root → group
	var roots []int     // of each group
	for _, e := range edges {
		r := find(e.a)
		if _, ok := at[r]; ok {
			continue
		}
		at[r] = len(roots)
		roots = append(roots, r)
	}
	// Each group is named by its first opposite judgment in the order they
	// came, also one whose edge to a whole answer evidence has since taken
	// away: every opposite judgment's unit is in a group, and a group only
	// grows, so its name stays and the older one wins when groups merge.
	names := make([]string, len(roots))
	for k, j := range a.js {
		if j.Label != LabelOpposite {
			continue
		}
		g := at[find(a.jUnit[k])]
		if names[g] == "" {
			target := a.answers[a.jAgainst[k]].id
			if len(j.Evidence) > 0 {
				target = j.Evidence[0]
			}
			names[g] = a.units[a.jUnit[k]].id + "~" + target
		}
	}
	group := make([]int, len(nodeAns)) // the group of each node, -1 for none
	nodes := make([][]int, len(roots))
	for n := range nodeAns {
		g, ok := at[find(n)]
		if !ok {
			g = -1
		} else {
			nodes[g] = append(nodes[g], n)
		}
		group[n] = g
	}
	groupEdges := make([][]int, len(roots))
	for i, e := range edges {
		g := group[e.a]
		groupEdges[g] = append(groupEdges[g], i)
	}

	// Items in the order of their first unit; once every request has
	// ended, each draws in the units outside every group that a same link
	// ties to one of its sides. Not before: a later judgment may tie such a
	// unit to a second side or put its AI on another, and a unit that left
	// 다른 말 again would move between cards while the phone shows them.
	order := make([]int, len(roots))
	for g := range order {
		order[g] = g
	}
	slices.SortFunc(order, func(p, q int) int { return cmp.Compare(roots[p], roots[q]) })
	start := len(a.items)
	for _, g := range order {
		it := aggItem{kind: CardDiffer, key: "d:" + names[g], first: roots[g]}
		it.pos = a.sides(nodes[g], groupEdges[g], edges, nodeAns, group, g)
		for _, n := range nodes[g] {
			if n < nu {
				a.taken[n] = true
				it.basis = append(it.basis, n)
			}
		}
		a.items = append(a.items, it)
	}
	for i := start; i < len(a.items); i++ {
		it := &a.items[i]
		if a.in.Ended {
			a.absorb(it, group)
		}
		for p := range it.pos {
			it.pos[p].whole = nil
			for _, x := range it.pos[p].answers {
				if !slices.ContainsFunc(it.pos[p].units, func(u int) bool { return a.units[u].ans == x }) {
					it.pos[p].whole = append(it.pos[p].whole, x)
				}
			}
		}
		a.numbers(it)
		it.check = a.checksOf(it)
		it.missing = a.missing(it)
	}
}

// sides splits group g into sides. Each answer starts on a side of its own
// (an answer never stands against itself), and a same link with evidence
// between two units of the group joins their sides unless an opposite edge
// already runs between them, in reading order of the links' units.
func (a *aggState) sides(nodes, groupEdges []int, edges []aggEdge, nodeAns, group []int, g int) []aggPos {
	side := make([]int, len(a.answers)) // union-find over answers; -1 for answers not in the group
	for i := range side {
		side[i] = -1
	}
	for _, n := range nodes {
		side[nodeAns[n]] = nodeAns[n]
	}
	find := func(x int) int {
		for side[x] != x {
			x = side[x]
		}
		return x
	}
	opposed := func(p, q int) bool {
		for _, i := range groupEdges {
			s, t := find(nodeAns[edges[i].a]), find(nodeAns[edges[i].b])
			if s == p && t == q || s == q && t == p {
				return true
			}
		}
		return false
	}
	// Same links in reading order of their units, not in the order the
	// judgments came, so that which request ended first does not decide
	// who stands where when two links cannot both join (four answers or
	// more).
	var links [][2]int
	for k, j := range a.js {
		u := a.jUnit[k]
		if j.Label != LabelSame || group[u] != g {
			continue
		}
		for _, e := range j.Evidence {
			if v := a.unitByID[e]; group[v] == g {
				links = append(links, [2]int{min(u, v), max(u, v)})
			}
		}
	}
	slices.SortFunc(links, func(p, q [2]int) int { return cmp.Or(cmp.Compare(p[0], q[0]), cmp.Compare(p[1], q[1])) })
	for _, l := range links {
		p, q := find(a.units[l[0]].ans), find(a.units[l[1]].ans)
		if p != q && !opposed(p, q) {
			side[max(p, q)] = min(p, q)
		}
	}
	var pos []aggPos
	at := map[int]int{} // side root → index in pos
	for x := range side {
		if side[x] < 0 {
			continue
		}
		r := find(x)
		i, ok := at[r]
		if !ok {
			i = len(pos)
			at[r] = i
			pos = append(pos, aggPos{})
		}
		pos[i].answers = append(pos[i].answers, x)
	}
	for _, n := range nodes {
		if n < len(a.units) {
			i := at[find(nodeAns[n])]
			pos[i].units = append(pos[i].units, n)
		}
	}
	a.sortSides(pos)
	return pos
}

// sortSides orders sides by their first unit; a side with no unit (a whole
// answer) goes after them, by answer.
func (a *aggState) sortSides(pos []aggPos) {
	firstOf := func(p aggPos) int {
		if len(p.units) > 0 {
			return p.units[0]
		}
		return len(a.units) + p.answers[0]
	}
	slices.SortStableFunc(pos, func(p, q aggPos) int { return cmp.Compare(firstOf(p), firstOf(q)) })
}

// absorb draws into item it the units outside every group that a same link
// with evidence ties to exactly one of its sides, in reading order. A unit
// joins only when its answer stands on no other side of the item.
func (a *aggState) absorb(it *aggItem, group []int) {
	sideOf := map[int]int{} // the item's own units → side
	for p := range it.pos {
		for _, u := range it.pos[p].units {
			sideOf[u] = p
		}
	}
	links := map[int][]int{} // outside unit → sides it is tied to
	tie := func(w, v int) {
		if p, ok := sideOf[v]; ok && group[w] < 0 && !a.taken[w] && !slices.Contains(links[w], p) {
			links[w] = append(links[w], p)
		}
	}
	for k, j := range a.js {
		if j.Label != LabelSame {
			continue
		}
		u := a.jUnit[k]
		for _, e := range j.Evidence {
			v := a.unitByID[e]
			tie(u, v)
			tie(v, u)
		}
	}
	ws := make([]int, 0, len(links))
	for w := range links {
		ws = append(ws, w)
	}
	slices.Sort(ws)
	for _, w := range ws {
		if len(links[w]) != 1 {
			continue // tied to two sides: it settles nothing
		}
		p, x := links[w][0], a.units[w].ans
		elsewhere := false
		for q := range it.pos {
			elsewhere = elsewhere || q != p && slices.Contains(it.pos[q].answers, x)
		}
		if elsewhere {
			continue
		}
		s := &it.pos[p]
		s.units = append(s.units, w) // after the group's own units, so one of those stays the representative
		if i, found := slices.BinarySearch(s.answers, x); !found {
			s.answers = slices.Insert(s.answers, i, x)
		}
		a.taken[w] = true
	}
}

// numbers sets NumbersDiffer when two sides' numbers Mismatch, and gives
// each unit its numbers that fit none of the other sides' (Unmatched). A
// side that is a whole answer brings all of that answer's numbers, ready
// to compare with once for every item (answerMatch), so two whole answers
// compare in time near their size, not its square.
func (a *aggState) numbers(it *aggItem) {
	qs := make([][]Quantity, len(it.pos))
	total := 0
	for p, s := range it.pos {
		for _, u := range s.units {
			qs[p] = append(qs[p], a.unitNums(u)...)
		}
		for _, y := range s.whole {
			qs[p] = append(qs[p], a.answerNums(y)...)
		}
		total += len(qs[p])
	}
	match := make([]numMatcher, len(qs))
	for p, s := range it.pos {
		if len(s.units) == 0 && len(s.whole) == 1 {
			match[p] = a.answerMatch(s.whole[0])
		} else {
			match[p] = numMatchFor(qs[p], total-len(qs[p]))
		}
	}
	for q := 1; q < len(qs) && !it.numbersDiffer; q++ {
		for p := 0; p < q && !it.numbersDiffer; p++ {
			it.numbersDiffer = match[q].mismatch(qs[p])
		}
	}
	for p := range it.pos {
		for _, u := range it.pos[p].units {
			mine := a.unitNums(u)
			for i, x := range mine {
				// Unmatched against the other sides together.
				compared, fits := false, false
				for q := range match {
					if q != p {
						c, f := match[q].compare(x)
						compared, fits = compared || c, fits || f
					}
				}
				if !compared || fits {
					continue
				}
				text := a.units[u].text
				it.pos[p].spans = append(it.pos[p].spans, NumberSpan{
					Unit:  a.units[u].id,
					Start: UTF16Offset(text, mine[i].Start),
					End:   UTF16Offset(text, mine[i].End),
				})
			}
		}
	}
}

// checksOf are the enabled checks that fired on the judgments of the item's
// units, in AllChecks order.
func (a *aggState) checksOf(it *aggItem) []string {
	fired := map[string]bool{}
	for _, s := range it.pos {
		for _, u := range s.units {
			for _, k := range a.ofUnit[u] {
				for _, f := range a.js[k].Flags {
					if a.counts(k, f) {
						fired[f] = true
					}
				}
			}
		}
	}
	out := []string{}
	for _, c := range AllChecks {
		if fired[c] {
			out = append(out, c)
		}
	}
	return out
}

// missing are the ready answers, in the turn's order and not on the item,
// that a unit of its basis could not be compared with. A unit that only
// joined the item does not count: the units it says the same as were
// compared for it.
func (a *aggState) missing(it *aggItem) []string {
	out := []string{}
	for y := range a.answers {
		on := slices.ContainsFunc(it.pos, func(s aggPos) bool { return slices.Contains(s.answers, y) })
		if !on && slices.ContainsFunc(it.basis, func(u int) bool { return a.uncompared(u, y) }) {
			out = append(out, a.answers[y].id)
		}
	}
	return out
}

// uncompared reports whether judged unit u has no usable judgment against
// ready answer y although every request that could give one has ended.
func (a *aggState) uncompared(u, y int) bool {
	x := a.units[u].ans
	if !a.units[u].judged || x == y || !a.answers[x].ready || !a.answers[y].ready || !a.pairEnded(x, y) {
		return false
	}
	for _, k := range a.ofUnit[u] {
		if a.jAgainst[k] == y {
			return false
		}
	}
	return true
}

// pairEnded reports whether every request judging x's units against y has
// ended: those from x against y, or in the pair shape the one from y
// against x, which judges both ways.
func (a *aggState) pairEnded(x, y int) bool {
	if a.in.Ended {
		return true
	}
	st := a.requests[x][y]
	if len(st) == 0 {
		st = a.requests[y][x]
	}
	for _, s := range st {
		if s != RequestDone && s != RequestFailed {
			return false
		}
	}
	return len(st) > 0
}

// core reports whether any judge called unit u core: then it is shown,
// unless an opposite has already put it on a 다른 말 item.
func (a *aggState) core(u int) bool {
	return slices.ContainsFunc(a.ofUnit[u], func(k int) bool { return a.js[k].Core })
}

// says reports whether some judgment of unit u has the label.
func (a *aggState) says(u int, label string) bool {
	return slices.ContainsFunc(a.ofUnit[u], func(k int) bool { return a.js[k].Label == label })
}

// isTagged reports whether an enabled check that counts fired on a
// judgment of unit u.
func (a *aggState) isTagged(u int) bool {
	return slices.ContainsFunc(a.ofUnit[u], func(k int) bool { return a.tagged[k] })
}

// single is an item of one unit on one side.
func (a *aggState) single(kind, prefix string, u int) aggItem {
	it := aggItem{kind: kind, key: prefix + a.units[u].id, first: u, basis: []int{u},
		pos: []aggPos{{answers: []int{a.units[u].ans}, units: []int{u}}}}
	it.check = a.checksOf(&it)
	it.missing = a.missing(&it)
	return it
}

// check makes the 확인 필요 items: shown units with an enabled check that
// counts.
func (a *aggState) check() {
	for u := range a.units {
		if !a.taken[u] && a.core(u) && a.isTagged(u) {
			a.taken[u] = true
			a.items = append(a.items, a.single(CardCheck, "c:", u))
		}
	}
}

// agree makes the 같은 말 items, with more AIs first.
func (a *aggState) agree() {
	start := len(a.items)
	joins := func(v int) bool { return !a.taken[v] && !a.isTagged(v) }
	for r := range a.units {
		if a.taken[r] || !a.core(r) || !a.says(r, LabelSame) {
			continue
		}
		a.taken[r] = true
		x := a.units[r].ans
		answers := []int{x}
		var members []int
		for _, k := range a.ofUnit[r] {
			if a.js[k].Label != LabelSame {
				continue
			}
			answers = append(answers, a.jAgainst[k])
			if ev := a.js[k].Evidence; len(ev) > 0 {
				if v := a.unitByID[ev[0]]; joins(v) {
					a.taken[v] = true
					members = append(members, v)
				}
			}
		}
		for k, j := range a.js {
			if w := a.jUnit[k]; j.Label == LabelSame && a.jAgainst[k] == x && slices.Contains(j.Evidence, a.units[r].id) && joins(w) {
				a.taken[w] = true
				members = append(members, w)
			}
		}
		for _, m := range members {
			answers = append(answers, a.units[m].ans)
		}
		slices.Sort(answers)
		slices.Sort(members)
		it := aggItem{kind: CardAgree, key: "a:" + a.units[r].id, first: r, basis: []int{r},
			pos: []aggPos{{answers: slices.Compact(answers), units: append([]int{r}, members...)}}}
		it.check = a.checksOf(&it)
		it.missing = a.missing(&it)
		a.items = append(a.items, it)
	}
	slices.SortStableFunc(a.items[start:], func(p, q aggItem) int {
		return cmp.Or(cmp.Compare(len(q.pos[0].answers), len(p.pos[0].answers)), cmp.Compare(p.first, q.first))
	})
}

// onlyOne makes the 한 AI만 items: the shown units left, whose judgments
// all say absent (an unsure absent too, unless numbersAbsent fired).
func (a *aggState) onlyOne() {
	for u := range a.units {
		if !a.taken[u] && a.core(u) {
			a.taken[u] = true
			a.items = append(a.items, a.single(CardOnlyOne, "o:", u))
		}
	}
}

// keepKeys gives items the keys of in.Prev. An item keeps its own key when
// it was there before: when two items merge, that is the older key, since
// a group's first opposite judgment stays first as judgments are added.
// Otherwise it takes the first earlier key of its kind whose item had its
// representative on this item, or failing that any unit, unless another
// item has that key or claimed it first. Keys are unique even for broken
// input.
func (a *aggState) keepKeys(prev []Item) {
	own := map[string]bool{}
	for _, it := range a.items {
		own[it.key] = true
	}
	kindOf := map[string]string{}
	for _, p := range prev {
		if _, ok := kindOf[p.Key]; !ok {
			kindOf[p.Key] = p.Kind
		}
	}
	used := map[string]bool{}
	kept := make([]bool, len(a.items))
	for i, it := range a.items {
		if kind, ok := kindOf[it.key]; ok && kind == it.kind {
			kept[i], used[it.key] = true, true
		}
	}
	mine := make([]map[string]bool, len(a.items))
	for i, it := range a.items {
		mine[i] = map[string]bool{}
		for _, s := range it.pos {
			for _, u := range s.units {
				mine[i][a.units[u].id] = true
			}
		}
	}
	for _, byRep := range []bool{true, false} {
		for i := range a.items {
			if kept[i] {
				continue
			}
			for _, p := range prev {
				if p.Key == "" || p.Kind != a.items[i].kind || used[p.Key] || own[p.Key] || !aggShares(p, mine[i], byRep) {
					continue
				}
				a.items[i].key, used[p.Key], kept[i] = p.Key, true, true
				break
			}
		}
	}
	seen := map[string]int{}
	for i := range a.items {
		k := a.items[i].key
		if seen[k]++; seen[k] > 1 {
			a.items[i].key = k + "#" + strconv.Itoa(seen[k])
		}
	}
}

// aggShares reports whether item p had a unit in mine: its representative
// (the first unit of its first side), or with rep false any unit.
func aggShares(p Item, mine map[string]bool, rep bool) bool {
	for _, s := range p.Positions {
		for _, u := range s.Units {
			if mine[u] {
				return true
			}
			if rep {
				return false
			}
		}
	}
	return false
}

// summary puts the result in its API shapes.
func (a *aggState) summary() Summary {
	s := Summary{Judgments: a.js, Items: []Item{}, Units: make([]UnitState, len(a.units))}
	if s.Judgments == nil {
		s.Judgments = []Judgment{}
	}
	for u := range a.units {
		s.Units[u].Unit = a.units[u].id
	}
	for _, it := range a.items {
		item := Item{Key: it.key, Kind: it.kind, Positions: make([]Position, len(it.pos)),
			NumbersDiffer: it.numbersDiffer, Check: it.check, Missing: it.missing}
		for p, side := range it.pos {
			pos := Position{Agents: make([]string, len(side.answers)), Units: make([]string, len(side.units)), NumberSpans: side.spans}
			for i, x := range side.answers {
				pos.Agents[i] = a.answers[x].id
			}
			for i, u := range side.units {
				pos.Units[i] = a.units[u].id
				s.Units[u].State, s.Units[u].Item = it.kind, it.key
			}
			item.Positions[p] = pos
		}
		s.Items = append(s.Items, item)
		switch it.kind {
		case CardDiffer:
			s.Counts.Differ++
		case CardCheck:
			s.Counts.Check++
		case CardAgree:
			s.Counts.Agree++
		case CardOnlyOne:
			s.Counts.OnlyOne++
		}
	}
	for u := range a.units {
		st := &s.Units[u]
		switch {
		case st.State != "":
		case !a.units[u].judged:
			st.State = StateNotJudged
			s.Counts.NotJudged++
		case !a.in.Ended:
			st.State = StateWaiting
		case len(a.ofUnit[u]) == 0:
			st.State = StateNotCompared
			s.Counts.NotCompared++
		default:
			st.State = StateHidden
		}
	}
	return s
}
