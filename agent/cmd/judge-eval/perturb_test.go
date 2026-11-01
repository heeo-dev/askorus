//go:build judgeeval

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

func TestPerturbDeterministic(t *testing.T) {
	d, err := loadData(committedData(t))
	if err != nil {
		t.Fatal(err)
	}
	a, b := makePerturbSet(d, 60), makePerturbSet(d, 60)
	if !bytes.Equal(a.canonical(), b.canonical()) {
		t.Fatal("two generations differ")
	}
	pin, err := os.ReadFile(filepath.Join("testdata", "perturb-v1.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	if got := a.hash(); got != strings.TrimSpace(string(pin)) {
		t.Errorf("perturb-v1 is now %s, pinned %s. Change testdata/perturb-v1.sha256 only on purpose: a new set is a new version, and runs made with the old one no longer resume or report.",
			got, strings.TrimSpace(string(pin)))
	}
	if len(a.Items) != 60 {
		t.Errorf("%d items", len(a.Items))
	}
	for _, n := range []int{1, 10, 59} {
		if got := len(makePerturbSet(d, n).Items); got != n {
			t.Errorf("-n %d gives %d items", n, got)
		}
	}
}

func TestPerturbChanges(t *testing.T) {
	d, err := loadData(committedData(t))
	if err != nil {
		t.Fatal(err)
	}
	set := makePerturbSet(d, 60)
	ids := map[string]bool{}
	perQ := map[int]int{}
	synthetic := 0
	for i, it := range set.Items {
		if ids[it.ID] {
			t.Errorf("%s repeats", it.ID)
		}
		ids[it.ID] = true
		perQ[it.Q]++
		if i > 0 && set.Items[i-1].Q > it.Q {
			t.Errorf("%s is out of order", it.ID)
		}
		if it.Split != d.splitOf[it.Q] {
			t.Errorf("%s: split %s", it.ID, it.Split)
		}
		orig := d.answers[ansKey(it.Q, it.Against)].Answer
		if len(it.Changes) == 0 || len(it.Changes) > perturbMaxVals {
			t.Errorf("%s: %d changes", it.ID, len(it.Changes))
		}
		// Every change reads back as one quantity of its kind that fits
		// neither the old value nor a claim quantity that fit it, nor any
		// quantity of the kind in X's answer.
		qs := judge.Numbers(it.Answer)
		oldQs := judge.Numbers(orig)
		xq := judge.Numbers(d.answers[ansKey(it.Q, it.From)].Answer)
		var claimQ []judge.Quantity
		for _, c := range d.claimsOf(it.Q, it.From) {
			claimQ = append(claimQ, judge.Numbers(c.Text)...)
		}
		type span struct {
			oldS, oldE, newS, newE int
		}
		var spans []span
		for _, ch := range it.Changes {
			if len(ch.At) == 0 || len(ch.At) > perturbMaxOcc || len(ch.At) != len(ch.old) {
				t.Errorf("%s: %q at %d places", it.ID, ch.Was, len(ch.At))
			}
			for k, r := range ch.At {
				spans = append(spans, span{ch.old[k][0], ch.old[k][1], r[0], r[1]})
				if got := it.Answer[r[0]:r[1]]; got != ch.Now {
					t.Errorf("%s: %q at %v, want %q", it.ID, got, r, ch.Now)
				}
				if got := orig[ch.old[k][0]:ch.old[k][1]]; got != ch.Was {
					t.Errorf("%s: the original has %q, want %q", it.ID, got, ch.Was)
				}
				nk := slices.IndexFunc(qs, func(q judge.Quantity) bool { return q.Start == r[0] && q.End == r[1] })
				ok := slices.IndexFunc(oldQs, func(q judge.Quantity) bool { return q.Start == ch.old[k][0] && q.End == ch.old[k][1] })
				if nk < 0 || ok < 0 || qs[nk].Kind != ch.Kind || oldQs[ok].Kind != ch.Kind {
					t.Errorf("%s: %q -> %q does not read back as %s", it.ID, ch.Was, ch.Now, ch.Kind)
					continue
				}
				nq, oq := qs[nk], oldQs[ok]
				if judge.Compatible(nq, oq) {
					t.Errorf("%s: %q fits the old %q", it.ID, ch.Now, ch.Was)
				}
				for _, c := range claimQ {
					if judge.Compatible(c, oq) && judge.Compatible(c, nq) {
						t.Errorf("%s: %q still fits claim quantity %q", it.ID, ch.Now, c.Text)
					}
				}
				for _, x := range xq {
					if x.Kind == nq.Kind && judge.Compatible(x, nq) {
						t.Errorf("%s: %q fits %q of X's answer", it.ID, ch.Now, x.Text)
					}
				}
			}
		}
		// The rest of Y is byte for byte the same.
		slices.SortFunc(spans, func(a, b span) int { return a.oldS - b.oldS })
		po, pn := 0, 0
		for _, s := range spans {
			if orig[po:s.oldS] != it.Answer[pn:s.newS] {
				t.Errorf("%s: text between changes differs", it.ID)
			}
			po, pn = s.oldE, s.newE
		}
		if orig[po:] != it.Answer[pn:] {
			t.Errorf("%s: the end differs", it.ID)
		}
		// At least one synthetic opposite, each a same claim before.
		n := 0
		for _, c := range it.Claims {
			p := d.pairByID[pairID(it.Q, it.From, c.N, it.Against)]
			if c.Synthetic {
				n++
				if p.Label != judge.LabelSame || c.Label != judge.LabelOpposite {
					t.Errorf("%s: claim %d synthetic from %s", it.ID, c.N, p.Label)
				}
			} else if c.Label != p.Label {
				t.Errorf("%s: kept claim %d is %s, gold %s", it.ID, c.N, c.Label, p.Label)
			}
		}
		if n == 0 {
			t.Errorf("%s has no synthetic opposite", it.ID)
		}
		synthetic += n
	}
	for q, n := range perQ {
		_ = q
		if n > 6 {
			t.Errorf("question %d has %d items", q, n)
		}
	}
	t.Logf("%d items over %d questions, %d synthetic opposites", len(set.Items), len(perQ), synthetic)
	// The evidence gold is the units of Y' over the changes.
	p := prepare(d, judge.MaxUnits)
	p.prepareItems(set.Items)
	for _, it := range set.Items {
		if len(it.evidence) == 0 {
			t.Errorf("%s has no evidence gold", it.ID)
		}
		for _, id := range it.evidence {
			u := it.answer.Units[unitN(id)-1]
			if !slices.ContainsFunc(it.Changes, func(ch perturbChange) bool { return strings.Contains(u.Text, ch.Now) }) {
				t.Errorf("%s: evidence %s %q holds no change", it.ID, id, u.Text)
			}
		}
	}
}

func TestPerturbTexts(t *testing.T) {
	q := func(text string) judge.Quantity {
		qs := judge.Numbers(text)
		if len(qs) != 1 {
			t.Fatalf("%q: %d quantities", text, len(qs))
		}
		return qs[0]
	}
	for _, c := range []struct{ text, want string }{
		{"1443년", "1446년"},
		{"2026년 9월 18일", "2026년 9월 25일"}, {"2026년 9월 28일", "2026년 9월 21일"},
		{"9월 9일", "9월 16일"},
		{"2026년 11월", "2026년 1월"},
		{"Sep 18", "Sep 25"},
		{"September 2026", "November 2026"},
		{"2026-09-18", "2026-09-25"}, {"2026-09-05", "2026-09-12"}, {"2026. 9. 5.", "2026. 9. 12."},
		{"1.99.1", "1.99.3"},
		{"30%", "50%"},
		{"970만 명", "770만 명"},
		{"약 8분 20초", "약 6분 20초"},
		{"200~300mg", "400~500mg"},
		{"$1.5 billion", "$3.5 billion"},
		{"0.5%", "0.7%"},
	} {
		got := perturbTexts(q(c.text))
		if len(got) == 0 || got[0] != c.want {
			t.Errorf("%q: %q, want %q first", c.text, got, c.want)
		}
	}
	if got := perturbTexts(q("1990년대")); got != nil {
		t.Errorf("a decade is changed: %q", got)
	}
}

// The rules added at the S4 hand check (docs/phase2-plan.md part 18): a
// number tied to a parenthetical, an as-of date, a limit, a given of the
// question and a loose match are never changed.
func TestPerturbHandCheckRules(t *testing.T) {
	texts := func(text string, pick func(i int, q judge.Quantity) bool) []string {
		var out []string
		for i, q := range judge.Numbers(text) {
			if pick(i, q) {
				out = append(out, q.Text)
			}
		}
		return out
	}
	for _, c := range []struct {
		text         string
		tied, inside []string
	}{
		{"1446년(세종 28년)에 반포", []string{"1446년"}, []string{"28년"}},
		{"약 8분 20초(약 499초)가 걸립니다", []string{"약 8분 20초"}, []string{"약 499초"}},
		{"2월 28일(2027년은 평년) + 3월 1일", []string{"2월 28일"}, []string{"2027년"}},
		{"15,000–30,000 km (about 1–2 years of driving)", []string{"15,000–30,000 km"}, []string{"about 1–2 years"}},
		{"1443년 (완성은 1444년으로 보는 견해)", []string{"1443년"}, []string{"1444년"}},
		{"지구는 1월 초에 가장 가깝지만(근일점), 겨울", nil, nil},
		{"지구 저궤도(약 400km 상공)", nil, nil},
	} {
		qs := judge.Numbers(c.text)
		tied, inside := perturbTied(c.text, qs)
		gotT := texts(c.text, func(i int, _ judge.Quantity) bool { return tied[i] })
		gotI := texts(c.text, func(i int, _ judge.Quantity) bool { return inside[i] })
		if !slices.Equal(gotT, c.tied) || !slices.Equal(gotI, c.inside) {
			t.Errorf("%q: tied %q inside %q, want %q %q", c.text, gotT, gotI, c.tied, c.inside)
		}
	}
	for _, c := range []struct {
		text  string
		scope []string
	}{
		{"2026년 9월 말 기준 서울의 주민등록인구는 9,279,861명", []string{"2026년 9월 말"}},
		{"주민등록인구통계 기준, **2026년 9월 말** 서울특별시 인구는 9,279,861명", []string{"2026년 9월 말"}},
		{"As of October 2026, the tallest building", []string{"October 2026"}},
		{"2026년 10월 현재 기준금리는 3.75~4.00%", []string{"2026년 10월"}},
		{"2026년 9월 9일 공개, 9월 18일 출시. 국내 출고가는 199만 원", nil},
	} {
		got := texts(c.text, func(_ int, q judge.Quantity) bool { return perturbScope(c.text, q) })
		if !slices.Equal(got, c.scope) {
			t.Errorf("%q: as-of %q, want %q", c.text, got, c.scope)
		}
	}
	q := func(text string) judge.Quantity {
		qs := judge.Numbers(text)
		if len(qs) != 1 {
			t.Fatalf("%q: %d quantities", text, len(qs))
		}
		return qs[0]
	}
	for _, c := range []struct {
		a, b  string
		share bool
	}{
		{"1년", "6개월", false}, {"1876년", "1877년경", false}, {"약 8분 20초", "약 8분 20초", true},
		{"2일", "2~3일", true}, {"1443년", "1443년", true}, {"약 23.5도", "약 23.5°", true},
		{"약 930만 명", "약 928만 명", true},
	} {
		if !judge.Compatible(q(c.a), q(c.b)) {
			t.Fatalf("%q and %q are not even Compatible", c.a, c.b)
		}
		if got := perturbShares(q(c.a), q(c.b)); got != c.share {
			t.Errorf("%q shares %q: %v, want %v", c.a, c.b, got, c.share)
		}
	}
	for text, limit := range map[string]bool{"under about 4–5%": true, "below ~4%": true, "400mg 이하": true, "약 3.88%": false, "대략 2~4잔 정도": false} {
		if got := perturbLimitRe.MatchString(q(text).Text); got != limit {
			t.Errorf("%q: limit %v, want %v", text, got, limit)
		}
	}
	// On the committed set: nothing the question gives, nothing tied, no
	// as-of date and no limit was changed.
	d, err := loadData(committedData(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range makePerturbSet(d, 60).Items {
		orig := d.answers[ansKey(it.Q, it.Against)].Answer
		oq := judge.Numbers(orig)
		tied, inside := perturbTied(orig, oq)
		given := judge.Numbers(d.qByID[it.Q].Text)
		for _, ch := range it.Changes {
			for _, r := range ch.old {
				k := slices.IndexFunc(oq, func(o judge.Quantity) bool { return o.Start == r[0] && o.End == r[1] })
				if k < 0 {
					t.Fatalf("%s: %q not found", it.ID, ch.Was)
				}
				o := oq[k]
				if tied[k] || inside[k] || perturbScope(orig, o) || perturbLimitRe.MatchString(o.Text) ||
					slices.ContainsFunc(given, func(g judge.Quantity) bool { return judge.Compatible(o, g) }) {
					t.Errorf("%s: %q should not change", it.ID, ch.Was)
				}
			}
		}
	}
}
