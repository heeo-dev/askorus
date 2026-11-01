//go:build judgeeval

package main

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Synthetic opposites (plan part 2, perturb; design part 9 ③). For a
// direction X→Y with a gold same pair, a copy Y' of Y changes up to three
// numbers or dates that X's same claims share with Y, each at every
// occurrence, so those claims now meet a contradiction. It is
// deterministic: the same set gives byte-identical output (perturb-v1).

const (
	perturbVersion = "perturb-v1"
	setP06         = "p06"
	setPerturb     = "perturb"
	perturbMaxVals = 3 // shared values changed per copy
	perturbMaxOcc  = 3 // occurrences of one value
	perturbPerQ    = 2 // items per question in the first pass
)

type perturbChange struct {
	Kind string   `json:"kind"`
	Was  string   `json:"was"`
	Now  string   `json:"now"`
	At   [][2]int `json:"at"` // byte ranges of Now in the new answer
	old  [][2]int // byte ranges of Was in the original answer
}

type perturbClaim struct {
	N         int    `json:"n"`
	Label     string `json:"label"`     // the gold label against the new answer
	Synthetic bool   `json:"synthetic"` // a same claim that now meets a changed value: opposite
}

type perturbItem struct {
	ID      string          `json:"id"`
	Q       int             `json:"q"`
	Split   string          `json:"split"`
	From    string          `json:"from"`
	Against string          `json:"against"`
	Changes []perturbChange `json:"changes"`
	Answer  string          `json:"answer"`
	Claims  []perturbClaim  `json:"claims"`

	// Made by prepareItems:
	answer   judge.Answer // Answer split, as Against
	evidence []string     // units of answer that overlap a change: the evidence gold
}

type perturbFrom struct {
	Answers string `json:"answers"`
	Pairs   string `json:"pairs"`
}

type perturbSet struct {
	Set     string         `json:"set"`
	Version string         `json:"version"`
	From    perturbFrom    `json:"from"`
	Items   []*perturbItem `json:"items"`
}

// directions are the ordered pairs of AIs in Plan order: claude>codex,
// codex>claude, claude>opencode, opencode>claude, codex>opencode,
// opencode>codex.
func directions() [][2]string {
	var out [][2]string
	for i := range ais {
		for j := i + 1; j < len(ais); j++ {
			out = append(out, [2]string{ais[i], ais[j]}, [2]string{ais[j], ais[i]})
		}
	}
	return out
}

// canonical is the set's bytes: JSON indented by one space, nothing
// HTML-escaped, with a final line break.
func (s *perturbSet) canonical() []byte {
	b, _ := marshal(s, true)
	return b
}

// hash is the sha256 of the canonical bytes.
func (s *perturbSet) hash() string {
	sum := sha256.Sum256(s.canonical())
	return hex.EncodeToString(sum[:])
}

// makePerturbSet builds the perturb-v1 set of at most n items.
func makePerturbSet(d *dataSet, n int) *perturbSet {
	type cand struct {
		it  *perturbItem
		key string
	}
	var cands []cand
	for _, q := range d.questions {
		for _, dir := range directions() {
			if it := perturbCandidate(d, q.ID, dir[0], dir[1]); it != nil {
				sum := sha256.Sum256([]byte(perturbVersion + "|" + strconv.Itoa(q.ID) + "|" + dir[0] + "|" + dir[1]))
				cands = append(cands, cand{it, hex.EncodeToString(sum[:])})
			}
		}
	}
	slices.SortStableFunc(cands, func(a, b cand) int { return strings.Compare(a.key, b.key) })
	var picked []*perturbItem
	taken := map[*perturbItem]bool{}
	perQ := map[int]int{}
	for _, c := range cands {
		if perQ[c.it.Q] < perturbPerQ {
			perQ[c.it.Q]++
			taken[c.it] = true
			picked = append(picked, c.it)
		}
	}
	for _, c := range cands {
		if len(picked) >= n {
			break
		}
		if !taken[c.it] {
			taken[c.it] = true
			picked = append(picked, c.it)
		}
	}
	if len(picked) > n {
		picked = picked[:max(n, 0)]
	}
	dirIndex := func(it *perturbItem) int {
		return slices.Index(directions(), [2]string{it.From, it.Against})
	}
	slices.SortStableFunc(picked, func(a, b *perturbItem) int {
		return cmp.Or(cmp.Compare(a.Q, b.Q), cmp.Compare(dirIndex(a), dirIndex(b)))
	})
	if picked == nil {
		picked = []*perturbItem{}
	}
	return &perturbSet{Set: setPerturb, Version: perturbVersion,
		From: perturbFrom{Answers: d.names["answers"], Pairs: d.names["pairs"]}, Items: picked}
}

// perturbNorm is a quantity's text for finding its other occurrences.
func perturbNorm(s string) string { return strings.ToLower(fitNorm(s)) }

// perturbCandidate is the item of direction X→Y of question q, or nil
// when no clean shared value could be changed. Besides the plan's rules,
// the S4 hand check (docs/phase2-plan.md part 18) leaves alone a value that
// the question gives, one tied to a parenthetical (perturbTied), an as-of
// date (perturbScope) and a limit, and shares a duration or a year only when
// it is the same statement (perturbShares).
func perturbCandidate(d *dataSet, q int, x, y string) *perturbItem {
	var same []claimRec
	for _, c := range d.claimsOf(q, x) {
		if p, ok := d.pairByID[pairID(q, x, c.N, y)]; ok && p.Label == judge.LabelSame {
			same = append(same, c)
		}
	}
	if len(same) == 0 {
		return nil
	}
	yText := d.answers[ansKey(q, y)].Answer
	yq := judge.Numbers(yText)
	yTied, yInside := perturbTied(yText, yq)
	given := judge.Numbers(d.qByID[q].Text) // the question's own numbers
	var claimQ []judge.Quantity             // quantities of the same claims that can be shared
	for _, c := range same {
		claimQ = append(claimQ, perturbClaimQ(c.Text)...)
	}
	var allClaimQ []judge.Quantity // of every claim of X
	for _, c := range d.claimsOf(q, x) {
		allClaimQ = append(allClaimQ, judge.Numbers(c.Text)...)
	}
	xq := judge.Numbers(d.answers[ansKey(q, x)].Answer)

	var changes []perturbChange
	var olds []judge.Quantity // the original value of each change
	var repls []perturbRepl   // every occurrence changed so far, in the original's offsets
	seen := map[string]bool{}
	for _, v := range yq {
		if len(changes) == perturbMaxVals {
			break
		}
		var matched []judge.Quantity // claim quantities v shares
		for _, c := range claimQ {
			if perturbShares(v, c) {
				matched = append(matched, c)
			}
		}
		key := v.Kind + "|" + perturbNorm(v.Text)
		if len(matched) == 0 || seen[key] {
			continue
		}
		seen[key] = true
		if slices.ContainsFunc(given, func(g judge.Quantity) bool { return judge.Compatible(v, g) }) {
			continue // a given of the question (2026년, 2리터, $1.10)
		}
		if perturbLimitRe.MatchString(v.Text) {
			continue // a limit (under about 4–5%, 400mg 이하): a new one narrows or widens, not contradicts
		}
		var occ []judge.Quantity
		clean := true
		for oi, o := range yq {
			switch {
			case o.Kind == v.Kind && perturbNorm(o.Text) == perturbNorm(v.Text):
				occ = append(occ, o)
				if yTied[oi] || yInside[oi] || perturbScope(yText, o) {
					clean = false // restated in a parenthetical, restating, or an as-of date
				}
			case o.Kind == v.Kind && slices.ContainsFunc(matched, func(c judge.Quantity) bool { return judge.Compatible(o, c) }):
				clean = false // a twin written another way would still agree
			}
		}
		if !clean || len(occ) == 0 || len(occ) > perturbMaxOcc {
			continue
		}
		var against []judge.Quantity // what the new value must not fit
		against = append(against, v)
		for _, c := range allClaimQ {
			if judge.Compatible(v, c) {
				against = append(against, c)
			}
		}
		for _, o := range xq {
			if o.Kind == v.Kind {
				against = append(against, o)
			}
		}
		for _, now := range perturbTexts(v) {
			if now == "" || now == v.Text {
				continue
			}
			try := slices.Clone(repls)
			for _, o := range occ {
				try = append(try, perturbRepl{start: o.Start, end: o.End, now: now, change: len(changes)})
			}
			if perturbCheck(yText, try, len(changes), v.Kind, against) {
				changes = append(changes, perturbChange{Kind: v.Kind, Was: v.Text, Now: now})
				olds = append(olds, v)
				repls = try
				break
			}
		}
	}
	cur, at := perturbBuild(yText, repls)
	for i, r := range repls {
		changes[r.change].At = append(changes[r.change].At, at[i])
		changes[r.change].old = append(changes[r.change].old, [2]int{r.start, r.end})
	}
	if len(changes) == 0 {
		return nil
	}
	it := &perturbItem{ID: "p" + strconv.Itoa(q) + "-" + x + "-" + y, Q: q, Split: d.splitOf[q], From: x, Against: y,
		Changes: changes, Answer: cur}
	synthetic := 0
	for _, c := range d.claimsOf(q, x) {
		p := d.pairByID[pairID(q, x, c.N, y)]
		pc := perturbClaim{N: c.N, Label: p.Label}
		if p.Label == judge.LabelSame {
			for _, cq := range perturbClaimQ(c.Text) {
				if slices.ContainsFunc(olds, func(o judge.Quantity) bool { return perturbShares(o, cq) }) {
					pc.Label, pc.Synthetic = judge.LabelOpposite, true
				}
			}
		}
		if pc.Synthetic {
			synthetic++
		}
		it.Claims = append(it.Claims, pc)
	}
	if synthetic == 0 {
		return nil
	}
	return it
}

// perturbTied marks the quantities of text tied to a parenthetical: one
// that holds a digit and opens right after a quantity, with at most one
// space between. tied[i] is that quantity, inside[i] one in the
// parenthetical. The parenthetical restates or qualifies the number
// (1446년(세종 28년), 8분 20초(약 499초), 2월 28일(2027년은 평년),
// 15,000–30,000 km (about 1–2 years …)), so changing either side alone
// would leave the text at odds with itself; neither is changed.
func perturbTied(text string, qs []judge.Quantity) (tied, inside []bool) {
	tied, inside = make([]bool, len(qs)), make([]bool, len(qs))
	for i, q := range qs {
		open := q.End
		if open < len(text) && text[open] == ' ' {
			open++
		}
		if open >= len(text) || text[open] != '(' {
			continue
		}
		end := strings.IndexAny(text[open:], ")\n")
		if end < 0 || text[open+end] != ')' {
			continue
		}
		end += open
		if !strings.ContainsAny(text[open:end], "0123456789") {
			continue
		}
		tied[i] = true
		for k, o := range qs {
			if o.Start > open && o.End <= end {
				inside[k] = true
			}
		}
	}
	return tied, inside
}

// perturbScope reports whether year or date q is an as-of date that only
// dates what its sentence says: 기준 or 현재 follows it, or 기준 or "as of"
// comes right before it (2026년 9월 말 기준; 통계 기준, 2026년 9월 말; as of
// October 2026). A new as-of date moves the statement to another time
// instead of contradicting it, so it is neither shared nor changed.
func perturbScope(text string, q judge.Quantity) bool {
	if q.Kind != judge.NumYear && q.Kind != judge.NumDate {
		return false
	}
	const marks = " *_,"
	after := strings.TrimLeft(text[q.End:], marks)
	before := strings.ToLower(strings.TrimRight(text[:q.Start], marks))
	return strings.HasPrefix(after, "기준") || strings.HasPrefix(after, "현재") ||
		strings.HasSuffix(before, "기준") || strings.HasSuffix(before, "as of")
}

// perturbShares reports whether value v of Y and quantity c of a claim
// state the same number: Compatible, and for a duration the same unit (1년
// and 6개월 overlap once rounded, but are not one statement) and for a year
// the same year (1876년 is not 1877년경).
func perturbShares(v, c judge.Quantity) bool {
	if !judge.Compatible(v, c) {
		return false
	}
	switch v.Kind {
	case judge.NumDuration:
		return perturbUnit(v.Text) == perturbUnit(c.Text)
	case judge.NumYear:
		return slices.Equal(perturbYears(v.Text), perturbYears(c.Text))
	}
	return true
}

// perturbUnit is the word right after the last number of a quantity's
// text: 년, 개월, 초, years.
func perturbUnit(s string) string {
	i := strings.LastIndexAny(s, "0123456789")
	if i < 0 {
		return ""
	}
	rest := strings.TrimLeft(s[i+1:], " ")
	if end := strings.IndexFunc(rest, func(r rune) bool { return !unicode.IsLetter(r) }); end >= 0 {
		rest = rest[:end]
	}
	return strings.ToLower(rest)
}

// perturbYears are the four-digit numbers of a year's text.
func perturbYears(s string) []string {
	var out []string
	for _, r := range perturbDigits.FindAllString(s, -1) {
		if len(r) == 4 {
			out = append(out, r)
		}
	}
	return out
}

// perturbLimitRe finds a limit word in a quantity's text.
var perturbLimitRe = regexp.MustCompile(`(?i)\b(?:under|below|over|above|less than|more than|fewer than|at least|at most|up to|no more than|no less than)\b|이하|이상|미만|초과|이내`)

// perturbClaimQ are the quantities of a claim that a value of Y can share:
// not one inside a parenthetical tied to a number (it only restates it),
// nor an as-of date.
func perturbClaimQ(text string) []judge.Quantity {
	qs := judge.Numbers(text)
	_, inside := perturbTied(text, qs)
	var kept []judge.Quantity
	for i, q := range qs {
		if !inside[i] && !perturbScope(text, q) {
			kept = append(kept, q)
		}
	}
	return kept
}

// perturbRepl is one occurrence of a value to change: its byte range in
// the original answer and its new text.
type perturbRepl struct {
	start, end int
	now        string
	change     int // the index of its change
}

// perturbBuild makes the new answer from the original and the
// replacements, and gives the byte range of each replacement in it. The
// rest of the answer stays byte for byte.
func perturbBuild(orig string, repls []perturbRepl) (string, [][2]int) {
	order := make([]int, len(repls))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return cmp.Compare(repls[a].start, repls[b].start) })
	at := make([][2]int, len(repls))
	var b strings.Builder
	pos := 0
	for _, i := range order {
		r := repls[i]
		b.WriteString(orig[pos:r.start])
		s := b.Len()
		b.WriteString(r.now)
		at[i] = [2]int{s, b.Len()}
		pos = r.end
	}
	b.WriteString(orig[pos:])
	return b.String(), at
}

// perturbCheck reports whether change's new text reads back right in the
// answer the replacements make: at each of its occurrences Numbers finds
// one quantity of kind spanning exactly the new text, which fits nothing
// in against.
func perturbCheck(orig string, repls []perturbRepl, change int, kind string, against []judge.Quantity) bool {
	for i, r := range repls {
		for _, o := range repls[i+1:] {
			if r.start < o.end && o.start < r.end {
				return false
			}
		}
	}
	next, at := perturbBuild(orig, repls)
	qs := judge.Numbers(next)
	for i, r := range repls {
		if r.change != change {
			continue
		}
		k := slices.IndexFunc(qs, func(q judge.Quantity) bool { return q.Start == at[i][0] && q.End == at[i][1] })
		if k < 0 || qs[k].Kind != kind {
			return false
		}
		for _, a := range against {
			if judge.Compatible(qs[k], a) {
				return false
			}
		}
	}
	return true
}

var (
	perturbRunRe    = regexp.MustCompile(`[0-9][0-9,]*(?:\.[0-9]+)?`)
	perturbDigits   = regexp.MustCompile(`[0-9]+`)
	perturbDecadeRe = regexp.MustCompile(`[0-9]0'?s\b`)
	perturbRangeRe  = regexp.MustCompile(`^\s*(?:~|-|–|—|∼|〜|to|에서|부터)\s*$`)
)

// perturbTexts are the new texts to try for value v, in order (plan part
// 2): a year ±3, ±7; a date's day +7 (−7 after the 21st), or its month +2
// without a day; a version's last part +2; any other kind the first
// significant digit of its first number +2, −2, +4, −4, within 1–9, and
// both ends of a range alike.
func perturbTexts(v judge.Quantity) []string {
	switch v.Kind {
	case judge.NumYear:
		return perturbYear(v.Text)
	case judge.NumDate:
		return perturbDate(v.Text)
	case judge.NumVersion:
		runs := perturbDigits.FindAllStringIndex(v.Text, -1)
		if len(runs) == 0 {
			return nil
		}
		r := runs[len(runs)-1]
		n, _ := strconv.Atoi(v.Text[r[0]:r[1]])
		return []string{v.Text[:r[0]] + strconv.Itoa(n+2) + v.Text[r[1]:]}
	}
	return perturbDigit(v.Text)
}

// perturbYear moves every four-digit year of text (both ends of a range)
// by ±3, then ±7. A decade or century (1990년대, the 1990s) is left alone.
func perturbYear(text string) []string {
	if strings.Contains(text, "년대") || perturbDecadeRe.MatchString(text) {
		return nil
	}
	runs := perturbDigits.FindAllStringIndex(text, -1)
	var years [][]int
	for _, r := range runs {
		if r[1]-r[0] == 4 {
			years = append(years, r)
		}
	}
	if len(years) == 0 {
		return nil
	}
	var out []string
	for _, delta := range []int{3, -3, 7, -7} {
		out = append(out, perturbReplace(text, years, func(s string) (string, bool) {
			n, _ := strconv.Atoi(s)
			return strconv.Itoa(n + delta), true
		}))
	}
	return out
}

// perturbReplace replaces each run of text by f's result; "" when f
// refuses one.
func perturbReplace(text string, runs [][]int, f func(string) (string, bool)) string {
	var b strings.Builder
	pos := 0
	for _, r := range runs {
		s, ok := f(text[r[0]:r[1]])
		if !ok {
			return ""
		}
		b.WriteString(text[pos:r[0]])
		b.WriteString(s)
		pos = r[1]
	}
	b.WriteString(text[pos:])
	return b.String()
}

var perturbMonths = []string{"january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december"}

// perturbMonthName finds an English month name in text: its span and its
// month (1–12).
func perturbMonthName(text string) (start, end, month int, ok bool) {
	lower := strings.ToLower(text)
	for m, name := range perturbMonths {
		for _, form := range []string{name, "sept", name[:3]} {
			if form == "sept" && m != 8 {
				continue
			}
			if i := strings.Index(lower, form); i >= 0 {
				if (i == 0 || !isLetter(lower[i-1])) && (i+len(form) >= len(lower) || !isLetter(lower[i+len(form)])) {
					return i, i + len(form), m + 1, true
				}
			}
		}
	}
	return 0, 0, 0, false
}

func isLetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

// perturbDate moves the day by 7, or the month by 2 when there is no day.
func perturbDate(text string) []string {
	runs := perturbDigits.FindAllStringIndex(text, -1)
	var days, months [][]int
	var rest [][]int // runs that are neither a year nor marked
	for _, r := range runs {
		after := strings.TrimLeft(text[r[1]:], " ")
		switch {
		case r[1]-r[0] == 4:
		case strings.HasPrefix(after, "일"):
			days = append(days, r)
		case strings.HasPrefix(after, "월"):
			months = append(months, r)
		default:
			rest = append(rest, r)
		}
	}
	ms, me, month, named := perturbMonthName(text)
	switch {
	case len(days) > 0 || len(months) > 0:
		if len(rest) > 0 {
			return nil
		}
	case named:
		days, rest = rest, nil
	case len(rest) == 2:
		months, days, rest = rest[:1], rest[1:], nil // 2026-09-18, 2026. 9. 18.
	default:
		return nil
	}
	if len(days) > 0 {
		delta := 7
		for _, r := range days {
			if n, _ := strconv.Atoi(text[r[0]:r[1]]); n > 21 {
				delta = -7
			}
		}
		return []string{perturbReplace(text, days, func(s string) (string, bool) {
			n, _ := strconv.Atoi(s)
			n += delta
			if n < 1 || n > 31 {
				return "", false
			}
			return perturbPad(strconv.Itoa(n), s), true
		})}
	}
	if len(months) > 0 {
		return []string{perturbReplace(text, months, func(s string) (string, bool) {
			n, _ := strconv.Atoi(s)
			return perturbPad(strconv.Itoa((n+1)%12+1), s), true
		})}
	}
	if named {
		name := perturbMonths[(month+1)%12]
		was := text[ms:me]
		if len(was) <= 4 {
			name = name[:3]
		}
		if was[0] >= 'A' && was[0] <= 'Z' {
			name = strings.ToUpper(name[:1]) + name[1:]
		}
		return []string{text[:ms] + name + text[me:]}
	}
	return nil
}

// perturbPad keeps the width of was when it had a leading zero ("09").
func perturbPad(s, was string) string {
	for len(was) > 1 && was[0] == '0' && len(s) < len(was) {
		s = "0" + s
	}
	return s
}

// perturbDigit changes the first significant digit of the first number
// of text (and of the range's other end) by +2, −2, +4 or −4.
func perturbDigit(text string) []string {
	runs := perturbRunRe.FindAllStringIndex(text, -1)
	if len(runs) == 0 {
		return nil
	}
	ends := runs[:1]
	if len(runs) >= 2 && perturbRangeRe.MatchString(text[runs[0][1]:runs[1][0]]) {
		ends = runs[:2]
	}
	var out []string
	for _, delta := range []int{2, -2, 4, -4} {
		s := perturbReplace(text, ends, func(num string) (string, bool) {
			i := strings.IndexAny(num, "123456789")
			if i < 0 {
				return "", false
			}
			d := int(num[i]-'0') + delta
			if d < 1 || d > 9 {
				return "", false
			}
			return num[:i] + strconv.Itoa(d) + num[i+1:], true
		})
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// selectItems are the items of the run's questions.
func (s *perturbSet) selectItems(questions []int) []*perturbItem {
	var out []*perturbItem
	for _, it := range s.Items {
		if slices.Contains(questions, it.Q) {
			out = append(out, it)
		}
	}
	return out
}

// prepareItems splits each item's new answer and finds its evidence gold.
func (p *prepared) prepareItems(items []*perturbItem) {
	for _, it := range items {
		model := p.answers[ansKey(it.Q, it.Against)].Model
		it.answer = splitAnswer(it.Against, model, it.Answer, p.maxUnits)
		it.evidence = nil
		for _, u := range it.answer.Units {
			for _, ch := range it.Changes {
				for _, r := range ch.At {
					s, e := judge.UTF16Offset(it.Answer, r[0]), judge.UTF16Offset(it.Answer, r[1])
					if u.Start < e && s < u.End && !slices.Contains(it.evidence, u.ID) {
						it.evidence = append(it.evidence, u.ID)
					}
				}
			}
		}
	}
}

// itemSpec is an item as one comparison: X's answer and Y', in the AIs'
// order.
func (p *prepared) itemSpec(it *perturbItem) judge.Spec {
	s := judge.Spec{Question: p.qByID[it.Q].Text}
	for _, ai := range ais {
		switch ai {
		case it.From:
			s.Answers = append(s.Answers, p.answers[ansKey(it.Q, it.From)])
		case it.Against:
			s.Answers = append(s.Answers, it.answer)
		}
	}
	return s
}
