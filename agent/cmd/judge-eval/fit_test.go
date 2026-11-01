//go:build judgeeval

package main

import (
	"slices"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

func TestClaimFit(t *testing.T) {
	// The same numbers as internal/judge's TestSplitP06ClaimFit.
	d, err := loadData(committedData(t))
	if err != nil {
		t.Fatal(err)
	}
	p := prepare(d, judge.MaxUnits)
	if f := p.fitStats; f.One != 292 || f.Run != 15 || f.Inside != 0 || f.None != 0 {
		t.Errorf("fit %d/%d/%d/%d %v; want 292/15/0/0", f.One, f.Run, f.Inside, f.None, f.Fails)
	}
	units := 0
	for _, a := range p.answers {
		units += len(a.Units)
		for _, u := range a.Units {
			if !u.Judged {
				t.Errorf("%s is past the cap", u.ID)
			}
		}
	}
	if units != 1060 {
		t.Errorf("%d units, want 1060", units)
	}
	for id, f := range p.fit {
		if len(f.Units) == 0 {
			t.Errorf("claim %s has no unit", id)
		}
		if f.Mode == fitRun && len(f.Units) < 2 {
			t.Errorf("claim %s is a run of %d", id, len(f.Units))
		}
	}
	// A run is consecutive units.
	for id, f := range p.fit {
		for i := 1; i < len(f.Units); i++ {
			if unitN(f.Units[i]) != unitN(f.Units[i-1])+1 {
				t.Errorf("claim %s: units %v", id, f.Units)
			}
		}
	}
}

func TestFitModes(t *testing.T) {
	text := "1. 서울은 **크다**. 부산은 [바다](https://x.example)가 좋다.\n\n| 도시 | 인구 |\n| --- | --- |\n| 서울 | 940만 |\n"
	units, _, _ := judge.SplitAnswer("a", text, 0)
	f := formsOf(text, units)
	for _, c := range []struct {
		want  string
		mode  string
		units []int
	}{
		{"서울은 크다.", fitOne, []int{0}},
		{"서울은 크다. 부산은 바다가 좋다.", fitRun, []int{0, 1}},
		{"부산은 바다가", fitInside, []int{1}},
		{"서울 | 940만", fitOne, []int{2}},
		{"대구는 덥다.", fitNone, nil},
	} {
		mode, idx := fitIn(fitNorm(fitDropNumber(fitLinkTextRe.ReplaceAllString(c.want, "$1"))), f.evText, f.evRaw, true)
		if mode != c.mode || !slices.Equal(idx, c.units) {
			t.Errorf("%q: %s %v, want %s %v (units %d)", c.want, mode, idx, c.mode, c.units, len(units))
		}
	}
	// Claims are copied without links: the raw form drops them.
	if mode, _ := fitIn(fitNorm("부산은 가 좋다."), f.text, f.raw, false); mode != fitOne {
		t.Errorf("a claim without its link: %s", mode)
	}
	if mode, _ := fitIn(fitNorm("부산은"), f.text, f.raw, false); mode != fitNone {
		t.Errorf("claims never sit inside a unit: %s", mode)
	}
}
