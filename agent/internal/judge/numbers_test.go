package judge

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// numUpdate rewrites the measured results in testdata/numbers-p06.json:
// go test -run TestNumbersP06 ./internal/judge/ -numbers.update
var numUpdate = flag.Bool("numbers.update", false, "rewrite the measured results in testdata/numbers-p06.json")

// numOne is the only quantity of text; the test stops when there is not
// exactly one.
func numOne(t *testing.T, text string) Quantity {
	t.Helper()
	qs := Numbers(text)
	if len(qs) != 1 {
		t.Fatalf("Numbers(%q) = %s, want one quantity", text, numDump(qs))
	}
	return qs[0]
}

// numDump shows quantities in a failure message.
func numDump(qs []Quantity) string {
	var b strings.Builder
	b.WriteString("[")
	for i, q := range qs {
		if i > 0 {
			b.WriteString(", ")
		}
		v := q.num
		fmt.Fprintf(&b, "%s %q %s [%g %g | %g %g] as written %g..%g", q.Kind, q.Text, v.class, v.lo, v.loHi, v.hiLo, v.hi, v.nomLo, v.nomHi)
		switch v.shape {
		case numLimit:
			fmt.Fprintf(&b, " limit upper=%v", v.upper)
		case numRangeLimit:
			fmt.Fprintf(&b, " range-limit upper=%v", v.upper)
		}
		if v.ver != nil {
			fmt.Fprintf(&b, " version %v", v.ver)
		}
	}
	b.WriteString("]")
	return b.String()
}

// numKindsTexts lists kind and text of each quantity: "date 9월 18일".
func numKindsTexts(qs []Quantity) []string {
	out := []string{}
	for _, q := range qs {
		out = append(out, q.Kind+" "+q.Text)
	}
	return out
}

func TestNumbersPart5Cases(t *testing.T) {
	// The pairs part 5 names, and the composite final.json adds.
	for _, c := range []struct {
		a, b string
		fit  bool
	}{
		{"약 8분 20초", "8분 19초", true},
		{"약 930만", "9,279,861명", true},
		{"1억 5천만 km", "1억 4,960만 km", true},
		{"약 8분 10초", "약 8분 3초", false},
		{"970만 안팎", "약 956만", false},
		{"200~300mg/일 이하", "200mg 미만", false},
		{"9월 18일", "10월 23일", false},
	} {
		t.Run(c.a+" vs "+c.b, func(t *testing.T) {
			a, b := numOne(t, c.a), numOne(t, c.b)
			if got := Compatible(a, b); got != c.fit {
				t.Errorf("Compatible = %v, want %v\n a %s\n b %s", got, c.fit, numDump([]Quantity{a}), numDump([]Quantity{b}))
			}
			if got := Compatible(b, a); got != c.fit {
				t.Errorf("Compatible the other way = %v, want %v", got, c.fit)
			}
			if got := Mismatch([]Quantity{a}, []Quantity{b}); got == c.fit {
				t.Errorf("Mismatch = %v, want %v", got, !c.fit)
			}
		})
	}
}

func TestNumbersRead(t *testing.T) {
	for _, c := range []struct {
		text string
		want []string // kind and text of each quantity
	}{
		// Korean multipliers and composites.
		{"서울 인구는 약 930만 명입니다.", []string{"count 약 930만 명"}},
		{"평균 거리 약 1억 4,960만 km를", []string{"measure 약 1억 4,960만 km"}},
		{"1억 2천만 원과 3조 2천억 원", []string{"money 1억 2천만 원", "money 3조 2천억 원"}},
		{"2026년 9월 말 서울 인구는 9,279,861명(남 4,464,956명)", []string{"date 2026년 9월 말", "count 9,279,861명", "count 4,464,956명"}},
		{"1.5 million people, 50k and $5 million", []string{"count 1.5 million people", "count 50k", "money $5 million"}},
		// Durations, one or several units.
		{"평균 약 8분 20초(약 499초)가 걸립니다", []string{"duration 약 8분 20초", "duration 약 499초"}},
		{"1시간 30분 동안, 2 hours 30 minutes", []string{"duration 1시간 30분", "duration 2 hours 30 minutes"}},
		{"최근 3년간 실제로", []string{"duration 3년"}},
		// Ranges, limits and hedges.
		{"임신·수유 중: 200~300mg/일 이하, 약 1~2잔", []string{"measure 200~300mg/일 이하", "count 약 1~2잔"}},
		{"하루 100mg 안팎 이하 권장(약 1잔 미만)", []string{"measure 100mg 안팎 이하", "count 약 1잔 미만"}},
		{"Viruses are about 20–300 nanometers, 10 to 100 times smaller", []string{"measure about 20–300 nanometers", "ratio 10 to 100 times"}},
		{"thousands of times larger; 수천 배; 수만~수십만 년", []string{"ratio thousands of times", "ratio 수천 배", "duration 수만~수십만 년"}},
		{"between 10 and 20 people", []string{"count 10 and 20 people"}},
		{"at least 5 km, up to 3 hours, 20+ minutes, 3 cups or more", []string{"measure at least 5 km", "duration up to 3 hours", "duration 20+ minutes", "count 3 cups or more"}},
		{"Rate above ~6–7%; under about 4–5%", []string{"percent above ~6–7%", "percent under about 4–5%"}},
		{"10여 명, 20여 개국", []string{"count 10여 명", "count 20여 개국"}},
		{"커피로는 2~4잔 정도지만, 3개면", []string{"count 2~4잔 정도", "count 3개"}},
		{"연 3.75~4.00%, 0.25%p 인상", []string{"percent 3.75~4.00%", "percent 0.25%p"}},
		// Dates and years.
		{"2026년 10월 27~28일 FOMC, 다음 회의는 12월 8~9일", []string{"date 2026년 10월 27~28일", "date 12월 8~9일"}},
		{"근일점(1월경)에는, 1877년경, 1990년대, 2027년 봄", []string{"date 1월경", "year 1877년경", "year 1990년대", "year 2027년 봄"}},
		{"released on October 25, 2026 and Oct. 9; 25 October 2026; in 2010", []string{"date October 25, 2026", "date Oct. 9", "date 25 October 2026", "year 2010"}},
		{"2026-10-16, 2026.10.16 and 2026. 10. 16.", []string{"date 2026-10-16", "date 2026.10.16", "date 2026. 10. 16."}},
		{"2020~2024년, 9~10월", []string{"year 2020~2024년", "date 9~10월"}},
		// Versions.
		{"Python 3.15.2 and v2.1; 3.10 series", []string{"version 3.15.2", "version v2.1"}},
		// Money, measures, ratios.
		{"$0.05 (5 cents), 1,199달러부터, €5", []string{"money $0.05", "money 5 cents", "money 1,199달러", "money €5"}},
		{"(about 16–19°C / 60–67°F), 23.5° and 23.5도", []string{"measure about 16–19°C", "measure 60–67°F", "measure 23.5°", "measure 23.5도"}},
		{"초속 약 30만 km, 섭씨 30도", []string{"measure 초속 약 30만 km", "measure 섭씨 30도"}},
		{"1.5–2× a gasoline car; 2배 이상; 약 3분의 1", []string{"ratio 1.5–2×", "ratio 2배 이상", "ratio 약 3분의 1"}},
		{"3 times a week", []string{"count 3 times"}},
		{"in the 1990s, $1.5B and 1200 calories", []string{"year 1990s", "money $1.5B", "measure 1200 calories"}},
		// One-glyph units of the CJK compatibility block (S6R-1).
		{"서울 면적은 605㎢입니다. 쌀 2㎏, 3㎞ 거리, 500㎖ 두 병", []string{"measure 605㎢", "measure 2㎏", "measure 3㎞", "measure 500㎖"}},
		// Not quantities: model names, words with digits, links, bare
		// small numbers, other words that start like a unit.
		{"iPhone 18 Pro, A20 칩, 18e, 에어 2, CO2, HTTP/2, TLS 1.3, COVID-19, 5G, 1st, x86", nil},
		{"기본 포트 | 80 | 443, 제2형 당뇨병, 4대 임금, 21세기", nil},
		{"see https://example.com/2026/10/30/a-123 and www.x.org/1999", nil},
		{"3원칙, 2분기만, 5분석, 200초과는", nil},
		{"3.15", nil}, // a decimal alone, no version
		// A limit word holds the shorter one of the other side: "no more
		// than" is an upper limit, "not exceeding" too.
		{"Pregnant women should have no more than 200 mg of caffeine a day.", []string{"measure no more than 200 mg"}},
		{"a dose not exceeding 400 mg, not less than 5 km, not fewer than 5 people, no greater than 5 km",
			[]string{"measure not exceeding 400 mg", "measure not less than 5 km", "count not fewer than 5 people", "measure no greater than 5 km"}},
		// English hedges and limits in any case: a unit starts with them.
		{"About 8 minutes 20 seconds.", []string{"duration About 8 minutes 20 seconds"}},
		{"Over 100 countries; Up to 400 mg; Nearly 40%; Between 10 and 20 people",
			[]string{"count Over 100 countries", "measure Up to 400 mg", "percent Nearly 40%", "count 10 and 20 people"}},
		// 천 and 백 with digits after them are one number before 만.
		{"연봉 3천500만 원, 1억 2천500만 원, 1억2천500만원", []string{"money 3천500만 원", "money 1억 2천500만 원", "money 1억2천500만원"}},
		{"3천5백만 원, 2천500원, 5백50만 명", []string{"money 3천5백만 원", "money 2천500원", "count 5백50만 명"}},
		// An ordinal after 제 is an article or an event, no amount.
		{"근로기준법 제60조에 따라, 헌법 제1조, 제3조제1항, 제3회 대회, 제2권", nil},
		// A minus sign that starts a word.
		{"성장률은 -0.2%, 기온은 −20°C, -5~-3°C, 3-5개", []string{"percent -0.2%", "measure −20°C", "measure -5~-3°C", "count 3-5개"}},
		// Powers of ten, products of numbers and tolerances.
		{"The Sun is about 1.496 × 10^8 km away; 1.5 × 10^11 m; 3 × 10⁸ m/s; 1.6 × 10^-19 J",
			[]string{"measure about 1.496 × 10^8 km", "measure 1.5 × 10^11 m", "measure 3 × 10⁸ m/s", "measure 1.6 × 10^-19 J"}},
		{"해상도 1920×1080, 2560 × 1440, 3840x2160 px", nil},
		{"mean height 175 ± 6 cm", []string{"measure 175 ± 6 cm"}},
		// Inline math is drawn by the phone; its "$" is no dollar.
		{"빛의 속도는 $3 \\times 10^8$ m/s, $2^{10} = 1024$, 원리금은 $1000 \\times 1.05^{10} \\approx 1628.89$입니다.", nil},
		// A time of day is no quantity, and its minutes are no duration.
		{"오후 2시 30분에 시작, 회의는 오전 9시 50분, 18시 00분", nil},
		{"약 1시간 30분 걸립니다", []string{"duration 약 1시간 30분"}},
		// Phone and model numbers joined by a hyphen; a season stays a year.
		{"고객센터는 1544-0010입니다, 1588-1234, 1588-3366, 010-1234-5678", nil},
		{"2020-21 시즌", []string{"year 2020"}},
		// Day ranges.
		{"the October 27–28 FOMC meeting; October 27-28, 2026; 9월 18일부터 10월 2일까지; 10월 27일-28일",
			[]string{"date October 27–28", "date October 27-28, 2026", "date 9월 18일부터 10월 2일", "date 10월 27일-28일"}},
		// A list marker at the start of a text or a line is no number (the
		// splitter keeps it in an item's text); the rest is read as any text.
		{"1. 서울", nil},
		{"2) 부산", nil},
		{"3. 인구 10만 명", []string{"count 10만 명"}},
		{"12. 마지막", nil},
		{"1. 2배", []string{"ratio 2배"}},
		{"  7. 약 3개", []string{"count 약 3개"}},
		{"서울 인구\n1. 약 930만 명\n  2) 3천500만 원\n10) 5개, 2026. 10. 30.", []string{"count 약 930만 명", "money 3천500만 원", "count 5개", "date 2026. 10. 30."}},
		{"3.5배 늘었다", []string{"ratio 3.5배"}},                 // no marker: a decimal
		{"2026. 10. 30. 기준", []string{"date 2026. 10. 30."}}, // no marker: four digits
		{"1. 2026년 3월", []string{"date 2026년 3월"}},
		{"1999. 서울", []string{"year 1999"}},
		// Money ranges after a currency sign, 대 ranges, ones after 만.
		{"$5–10M, $50–60K and $1.5–2 billion", []string{"money $5–10M", "money $50–60K", "money $1.5–2 billion"}},
		{"물가는 2%대로, 가격은 100만 원대 초반, 관객 1,000만 명대", []string{"percent 2%대", "money 100만 원대", "count 1,000만 명대"}},
		{"차량 1,000대, 자동차 100만 대, 30대 남성, 1990년대", []string{"count 1,000", "count 100만", "year 1990년대"}},
		{"입장료 1만 5,000원, 1만5000원, 관객 1만 3일 만에", []string{"money 1만 5,000원", "money 1만5000원", "count 1만", "duration 3일"}},
	} {
		t.Run(c.text, func(t *testing.T) {
			qs := Numbers(c.text)
			if got := numKindsTexts(qs); !slices.Equal(got, numOrEmpty(c.want)) {
				t.Errorf("got  %q\nwant %q\n%s", got, c.want, numDump(qs))
			}
		})
	}
}

func TestNumbersShapes(t *testing.T) {
	for _, c := range []struct {
		text  string
		shape uint8
		upper bool
		class string
	}{
		{"200~300mg/일 이하", numRangeLimit, true, numMass},
		{"200mg 미만", numLimit, true, numMass},
		{"400mg/일 이상", numLimit, false, numMass},
		{"20+ minutes", numLimit, false, ""},
		{"at most 3 cups", numLimit, true, "잔"},
		{"above ~6–7%", numRangeLimit, false, numPct},
		{"약 1~2잔", numPoint, false, "잔"},
		{"0.25%p", numPoint, false, numPctPt},
		{"약 930만", numPoint, false, ""},
		{"초속 30만 km", numPoint, false, numSpeed},
		{"섭씨 30도", numPoint, false, numTemp},
		{"30도", numPoint, false, numDegree},
		{"no more than 200 mg", numLimit, true, numMass},
		{"not more than 3 cups", numLimit, true, "잔"},
		{"not exceeding 400 mg", numLimit, true, numMass},
		{"no greater than 5 km", numLimit, true, numLength},
		{"not less than 5 km", numLimit, false, numLength},
		{"not fewer than 5 people", numLimit, false, "명"},
		{"Up to 400 mg", numLimit, true, numMass},
		{"At least 5 km", numLimit, false, numLength},
		{"No more than 3 cups", numLimit, true, "잔"},
	} {
		t.Run(c.text, func(t *testing.T) {
			q := numOne(t, c.text)
			if q.num.shape != c.shape || q.num.shape != numPoint && q.num.upper != c.upper || q.num.class != c.class {
				t.Errorf("got %s; want shape %d, upper %v, class %q", numDump([]Quantity{q}), c.shape, c.upper, c.class)
			}
		})
	}
}

func numOrEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func TestNumbersCompatible(t *testing.T) {
	for _, c := range []struct {
		a, b string
		fit  bool
	}{
		// Plain values: half the last digit; touching ends differ.
		{"200 mg", "300 mg", false},
		{"122일", "123일", false},
		{"약 13%", "13%", true},
		{"3.88%", "약 3.88%", true},
		{"10,320원", "10,030원", false},
		{"828 meters", "2,717 feet", true},
		{"16–19°C", "60–67°F", true},
		{"$0.05", "5 cents", true},
		// Ranges overlap.
		{"60–70%", "60–68%", true},
		{"30–50%", "20–35%", true},
		{"5~9m", "6~7m", true},
		{"3–6 months", "2–3 years", false},
		// Limits.
		{"200mg 미만", "200mg 이하", true},
		{"400mg 이하", "400mg 이상", false},
		{"약 1~2잔", "2잔 이내", true},
		{"200~300mg/일", "200mg 미만", false},
		{"200~300mg/일", "200~300mg/일 이하", true},
		{"300mg", "200~300mg/일 이하", false},
		{"20 minutes", "20+ minutes", true},
		{"10 minutes", "20+ minutes", false},
		{"Rate below ~4%", "Rate below ~4% (fixed)", true},
		{"Rate around 4–5%", "under about 4–5%", true},
		// Classes: never across them.
		{"5 km", "5 kg", false},
		{"30%", "30%p", false},
		{"199만 원", "199만 달러", false},
		{"3잔", "3명", false},
		{"약 930만", "약 930만 명", true},
		{"23.5°", "23.5도", true},
		// One-glyph units read as their letters (S6R-1).
		{"서울 면적은 605㎢입니다", "605 km²", true},
		{"서울 면적은 605㎢입니다", "서울 면적은 700㎢입니다", false},
		{"2㎏", "5㎏", false},
		{"2㎏", "2,000 g", true},
		{"3㎞", "3 km", true},
		{"5 ㎢", "5 ㎏", false},
		// Dates and years.
		{"9월 18일", "2026년 9월 18일", true},
		{"9월", "2026년 9월 18일", true},
		{"2026년 10월 28일", "2026년 10월 27~28일", true},
		{"1월경", "1월 초", true},
		{"2024년 말", "2025년 말", false},
		{"1877년경", "1876년", true},
		{"October 25, 2026", "2026년 10월 25일", true},
		{"2026년", "2026년 9월 18일", false}, // a year and a date are not compared
		{"2월 29일", "3월 1일", false},
		{"2024년 2월 29일", "3월 1일", false},
		{"2025년 3월 1일", "3월 1일", true},
		{"1700년대 후반", "1760년경", true}, // a century
		{"the 1800s", "1850", true},
		{"1990년대", "1995년", true},
		{"2000년대", "2015년", false}, // the decade
		{"the October 27–28 FOMC meeting", "on October 28", true},
		{"Sep 18–25", "9월 25일", true},
		{"October 27-28, 2026", "2026년 10월 28일", true},
		{"9월 18일부터 10월 2일까지", "9월 25일", true},
		{"10월 27일-28일", "10월 28일", true},
		// Versions.
		{"v3.15", "3.15.2", true},
		{"3.15.1", "3.15.2", false},
		// Limits whose word holds the other side's.
		{"Pregnant women should have no more than 200 mg of caffeine", "임신 중에는 하루 카페인 200mg 미만이 권고됩니다", true},
		{"a dose not exceeding 400 mg", "400mg 이하", true},
		{"no more than 200 mg a day", "200 mg or more a day", false},
		{"About 8 minutes 20 seconds", "It takes 8 minutes 19 seconds", true},
		{"Over 100 countries", "about 120 countries", true},
		{"Up to 400 mg", "300 mg", true},
		{"Nearly 40%", "39%", true},
		// One number of 천 and the digits after it; a multiplier written once.
		{"연봉 3천500만 원", "연봉 3,500만 원", true},
		{"1억 2천500만 원", "1억 2,500만 원", true},
		{"2천500원", "2,500원", true},
		{"3천500만 원", "500만 원", false},
		{"3~4만 원", "2만 원", false},
		{"3~4만 원", "3만 5천 원", true},
		{"3~4만 원 이하", "3만~4만 원 이하", true},
		{"1~2억 원", "3천만 원", false},
		{"100~200만 명", "20만 명", false},
		{"5–10 million people", "2 million people", false},
		{"between 5 and 10 million", "7 million", true},
		// A first end with separators or its own 천 takes the 만 too, and
		// only the 만, 억 or 조: "1 thousand" takes nothing.
		{"연봉 3,000~4,000만 원", "연봉 2,000만 원", false},
		{"1,500~2,000만 원", "500만 원", false},
		{"연봉 3천~5천만 원", "연봉 1천만 원", false},
		{"연봉 3천~5천만 원", "연봉 4천만 원", true},
		{"3천~5,000만 원", "2,000만 원", false},
		{"5,000~2만 원", "1만 원", true},
		{"1 thousand to 50 million people", "5 million people", true},
		// Signs.
		{"올해 성장률은 -0.2%입니다", "올해 성장률은 0.2%입니다", false},
		{"−20°C", "20°C", false},
		{"-5~-3°C", "-4°C", true},
		// N여 and 남짓 are the value and some more.
		{"100여 명이 참석", "120명이 참석", true},
		{"20여 개국", "23개국", true},
		{"20여 개국", "35개국", false},
		{"1,000여 명", "1,300명", true},
		{"10여 년간 연구", "12년간 연구", true},
		{"100명 남짓", "120명", true},
		{"20년 남짓", "25년", true},
		{"20년 남짓", "35년", false},
		{"1시간 30분 남짓", "1시간 35분", true}, // up to 1:40, the next step of its 30
		{"8분 20초 남짓", "8분 25초", true},
		{"1시간 30분 남짓", "1시간 45분", false},
		// Powers of ten and tolerances.
		{"The Sun is about 1.496 × 10^8 km away", "평균 거리 약 1억 4,960만 km", true},
		{"mean height 175 ± 6 cm", "175cm", true},
		{"mean height 175 ± 6 cm", "181cm", false},
		// A number with no hedge word is exact to its first two significant
		// digits at least, whatever its spelling (rnum-7): 20,000원 is
		// 19,500–20,500원. So are each end of a range and a limit's value.
		{"입장료 20,000원", "15,000원", false},
		{"10,000원", "12,000원", false},
		{"50만 원", "45만 원", false},
		{"인구 10만 명", "14만 명", false},
		{"50 million", "45 million", false},
		{"100만 원", "120만 원", false},
		{"$50M", "$45M", false}, // a guard: false before too, $50M was 49.5M–50.5M
		{"$50 million", "$45 million", false},
		{"2만 원", "15,000원", false},
		{"$20k", "$15,000", false},
		{"3천만 원", "3,200만 원", false},
		{"1억 원", "1.2억 원", false},
		{"3~4만 원", "4만 2천 원", false},
		{"20,000원 이상", "19,000원", false},
		{"2천 원", "2,400원", false}, // below 10,000 to the one, as 2,000원 is
		{"2 thousand people", "2,400 people", false},
		{"2만 원", "20,000원", true},
		{"$50M", "$50 million", true},
		{"$20K", "$20k", true},
		{"$20K", "$20,000", true},
		{"2만 5천 원", "25,000원", true},
		{"20,000원", "20,400원", true},
		{"1억 원", "1.04억 원", true},
		{"2천 원", "2,000원", true},
		{"20,000원 이상", "19,800원", true},
		{"123,456명", "123,457명", false}, // ±0.5 as before: the ends touch
		// A hedged one keeps the whole unit of its last nonzero digit.
		{"약 20,000원", "21,000원", true},
		{"약 2만 원", "15,000원", true},
		{"50만 원 정도", "45만 원", true},
		{"about $50 million", "$45 million", true},
		{"about $50M", "$45M", true}, // as "about $50 million"; 49M–51M before
		{"100만 원 남짓", "150만 원", true},
		{"약 2천 원", "2,400원", true},
		// Ends reached two ways in floating point still only touch.
		{"1.8%", "1.9%", false},
		{"0.9%", "1.0%", false},
		{"8.5%", "8.6%", false},
		{"1.1배", "1.2배", false},
		{"1.7 m", "1.8 m", false},
		{"1.1억 원", "1.2억 원", false},
		{"$1.7B", "$1.8B", false},
		{"0.07%", "0.08%", false},
		{"1.8%", "1.85%", true},
		// A currency before a range's first end: the multiplier is both ends'.
		{"$5–10M", "$7M", true},
		{"$50–60K", "$55,000", true},
		{"$1.5–2 billion", "$1 billion", false},
		{"€3–4 billion", "€3.5 billion", true},
		// "대" after a unit: from the value up to its next step.
		{"2%대", "2.7%", true},
		{"2%대", "3.2%", false},
		{"100만 원대", "150만 원", true},
		{"100만 원대", "210만 원", false},
		// The ones after 만 written in digits.
		{"1만 5,000원", "15,000원", true},
		{"1만5000원", "15,000원", true},
		{"2만 3,500원", "23,500원", true},
		{"2만 3,500원", "25,000원", false},
	} {
		t.Run(c.a+" vs "+c.b, func(t *testing.T) {
			// Each side reads one quantity: a row passes only on what it means.
			a, b := numOne(t, c.a), numOne(t, c.b)
			qa, qb := []Quantity{a}, []Quantity{b}
			if got := Compatible(a, b); got != c.fit {
				t.Errorf("Compatible = %v, want %v\n a %s\n b %s", got, c.fit, numDump(qa), numDump(qb))
			}
			if Compatible(a, b) != Compatible(b, a) {
				t.Errorf("Compatible is not symmetric")
			}
		})
	}
}

// S6R-1: the one-glyph units of the CJK compatibility block read as the
// units they stand for. Before, only ㎡ did, so "605㎢" against "700㎢" had
// no numbers, and since S6 an unsure same of such a pair passed as 같은 말
// (TestAggregateUnsureRule). Each glyph reads as one measure of its class
// at its scale, fits the unit in letters where the table has one, and
// does not fit another value of itself.
func TestNumbersUnitGlyphs(t *testing.T) {
	for _, c := range []struct {
		glyph, class string
		scale        float64 // to the class's base unit
		letters      string  // the same unit in letters; "" when the table has none
	}{
		{"㎚", numLength, 1e-9, "nm"}, {"㎛", numLength, 1e-6, "µm"}, {"㎜", numLength, 1e-3, "mm"},
		{"㎝", numLength, 1e-2, "cm"}, {"㎞", numLength, 1e3, "km"},
		{"㎟", numArea, 1e-6, ""}, {"㎠", numArea, 1e-4, ""}, {"㎡", numArea, 1, "m²"},
		{"㎢", numArea, 1e6, "km²"}, {"㏊", numArea, 1e4, "ha"},
		{"㎕", numVolume, 1e-6, ""}, {"㎖", numVolume, 1e-3, "mL"}, {"㎗", numVolume, 0.1, "dL"},
		{"㎘", numVolume, 1e3, ""}, {"㏄", numVolume, 1e-3, "cc"}, {"㎣", numVolume, 1e-6, ""},
		{"㎤", numVolume, 1e-3, ""}, {"㎥", numVolume, 1e3, ""}, {"㎦", numVolume, 1e12, ""},
		{"㎍", numMass, 1e-6, "µg"}, {"㎎", numMass, 1e-3, "mg"}, {"㎏", numMass, 1e3, "kg"},
		{"㎧", numSpeed, 1, "m/s"},
		{"㎅", numData, 1e3, "KB"}, {"㎆", numData, 1e6, "MB"}, {"㎇", numData, 1e9, "GB"},
		{"㎉", numEnergy, 4184, "kcal"},
		{"㎾", numPower, 1e3, "kW"}, {"㎿", numPower, 1e6, "MW"},
		{"㎐", numFreq, 1, "Hz"}, {"㎑", numFreq, 1e3, "kHz"}, {"㎒", numFreq, 1e6, "MHz"}, {"㎓", numFreq, 1e9, "GHz"},
		{"㏈", numSound, 1, "dB"},
	} {
		t.Run(c.glyph, func(t *testing.T) {
			q := numOne(t, "값은 7"+c.glyph+"입니다.")
			if q.Kind != NumMeasure || q.Text != "7"+c.glyph || q.num.class != c.class || math.Abs(q.num.nomLo/(7*c.scale)-1) > 1e-9 {
				t.Errorf("7%s = %s, want measure %s at %g", c.glyph, numDump([]Quantity{q}), c.class, 7*c.scale)
			}
			if other := numOne(t, "값은 9"+c.glyph+"입니다."); Compatible(q, other) {
				t.Errorf("7%s fits 9%s", c.glyph, c.glyph)
			}
			if c.letters != "" {
				if w := numOne(t, "값은 7 "+c.letters+"입니다."); !Compatible(q, w) {
					t.Errorf("7%s does not fit 7 %s\n %s\n %s", c.glyph, c.letters, numDump([]Quantity{q}), numDump([]Quantity{w}))
				}
			}
		})
	}
}

func TestNumbersWidth(t *testing.T) {
	// The interval each text reads, in the base unit (rnum-7): a number with
	// no hedge word is exact to its first two significant digits at least,
	// whatever its spelling; a hedged one allows a whole unit of its last
	// digit, a round one of 10,000 or more of its last nonzero digit.
	for _, c := range []struct {
		text   string
		lo, hi float64
	}{
		{"20,000원", 19500, 20500},
		{"2만 원", 19500, 20500},
		{"15,000원", 14500, 15500},
		{"-20,000원", -20500, -19500},
		{"1억", 0.95e8, 1.05e8},
		{"1.5억 원", 1.45e8, 1.55e8},
		{"150,000,000원", 1.45e8, 1.55e8},
		{"1억 5천", 1.45e8, 1.55e8},
		{"2만 5천 원", 24500, 25500},
		{"3천만 원", 2.95e7, 3.05e7},
		{"123,456명", 123455.5, 123456.5},
		{"12,345.6명", 12345.55, 12345.65},
		{"$50M", 49.5e6, 50.5e6},
		{"$50 million", 49.5e6, 50.5e6},
		{"50,000,000달러", 49.5e6, 50.5e6},
		{"$1.5B", 1.45e9, 1.55e9},
		{"1.5 billion dollars", 1.45e9, 1.55e9},
		{"$20K", 19500, 20500},
		{"$20k", 19500, 20500},
		{"$100B", 95e9, 105e9}, // as "100 billion": 99.5B–100.5B before
		{"100 billion dollars", 95e9, 105e9},
		{"3 × 10⁸ m/s", 2.95e8, 3.05e8},
		// Below 10,000: the ones at least, as every zero is a digit there.
		{"2,000원", 1999.5, 2000.5},
		{"2천 원", 1999.5, 2000.5},
		{"2 thousand people", 1999.5, 2000.5},
		{"3.75%", 3.745, 3.755},
		{"8분 19초", 498.5, 499.5},
		// Each end of a range.
		{"3~4만 원", 29500, 40500},
		// Hedged: as before.
		{"약 20,000원", 10000, 30000},
		{"약 2만 원", 10000, 30000},
		{"20,000원 정도", 10000, 30000},
		{"약 930만", 9.2e6, 9.4e6},
		{"약 956만", 9.55e6, 9.57e6},
		{"about $50 million", 40e6, 60e6},
		{"about $50M", 40e6, 60e6},
		{"약 2천 원", 1000, 3000},
		{"약 2,000원", 1999, 2001},
		{"약 8분 3초", 482, 484},
		// N여 and 남짓: the value and up to its next step, as before.
		{"100만 원 남짓", 1e6, 2e6},
		{"1,000여 명", 1000, 2000},
	} {
		t.Run(c.text, func(t *testing.T) {
			q := numOne(t, c.text)
			near := func(a, b float64) bool { return math.Abs(a-b) <= 1e-9*max(1, math.Abs(b)) }
			if !near(q.num.lo, c.lo) || !near(q.num.hi, c.hi) {
				t.Errorf("got %s; want %g..%g", numDump([]Quantity{q}), c.lo, c.hi)
			}
		})
	}
}

func TestNumbersMismatchAndUnmatched(t *testing.T) {
	q := Numbers
	// A side without numbers of a kind never disagrees on it.
	if Mismatch(q("약 8분 20초"), nil) || Mismatch(nil, q("약 8분 20초")) || Mismatch(q("약 8분 20초"), q("3명")) {
		t.Error("a side without numbers of the kind is a mismatch")
	}
	// One fitting pair in a kind is enough: 8분 27초 fits, so the two
	// sentences of 2-claude3-opencode do not Mismatch, though Unmatched
	// finds 8분 10초.
	a := q("근일점(1월경)에는 약 8분 10초, 원일점(7월경)에는 약 8분 27초 정도입니다.")
	b := q("실제로는 약 8분 3초(근일점) ~ 8분 27초(원일점) 사이에서 달라집니다.")
	if Mismatch(a, b) || Mismatch(b, a) {
		t.Errorf("Mismatch with a fitting pair:\n%s\n%s", numDump(a), numDump(b))
	}
	if got := Unmatched(a, b); !slices.Equal(got, []int{1}) {
		t.Errorf("Unmatched(a, b) = %v, want [1] (약 8분 10초)\n%s", got, numDump(a))
	}
	if got := Unmatched(b, a); !slices.Equal(got, []int{0}) {
		t.Errorf("Unmatched(b, a) = %v, want [0] (약 8분 3초)", got)
	}
	// Kinds are judged apart: dates fit, counts do not.
	c := q("2026년 9월 기준 약 970만 명")
	d := q("9월 18일에 약 956만 명")
	if !Mismatch(c, d) {
		t.Errorf("counts that differ next to dates that fit: no Mismatch\n%s\n%s", numDump(c), numDump(d))
	}
	// A count without a counter fits a count with one.
	if Mismatch(q("서울 인구는 약 930만"), q("9,279,861명, 3잔")) {
		t.Error("a bare count does not fit a count with a counter")
	}
	// Counts are judged per counter both sides have: 25개 that fit do not
	// hide people that do not.
	for _, pair := range [][2]string{
		{"서울 인구는 약 970만 명이고 자치구는 25개입니다", "서울 인구는 약 956만 명, 자치구 25개"},
		{"3잔, 5명", "3잔, 6명"},
		{"약 930만, 5잔", "9,279,861명, 3잔"}, // the bare count joins the 잔 group
	} {
		if !Mismatch(q(pair[0]), q(pair[1])) || !Mismatch(q(pair[1]), q(pair[0])) {
			t.Errorf("no Mismatch between counters that differ: %q, %q", pair[0], pair[1])
		}
	}
	if Mismatch(q("서울 인구는 약 956만 명이고 자치구는 25개입니다"), q("약 956만 명, 25개")) {
		t.Error("Mismatch between counters that fit")
	}
	if got := Unmatched(q("1억 4,960만 km, 30만 km"), q("약 1억 4,960만 km")); !slices.Equal(got, []int{1}) {
		t.Errorf("Unmatched = %v, want [1]", got)
	}
}

func TestNumbersSpans(t *testing.T) {
	text := "**약 8분 20초**(약 499초), 9월 18일 — 1억 4,960만 km; 3.75%~4.00%"
	qs := Numbers(text)
	if len(qs) != 5 {
		t.Fatalf("got %s", numDump(qs))
	}
	end := 0
	for _, q := range qs {
		if q.Start < end || q.End <= q.Start || q.End > len(text) || text[q.Start:q.End] != q.Text {
			t.Errorf("span %d..%d %q in %q", q.Start, q.End, q.Text, text)
		}
		if !utf8.RuneStart(text[q.Start]) || q.End < len(text) && !utf8.RuneStart(text[q.End]) {
			t.Errorf("span %d..%d cuts a character", q.Start, q.End)
		}
		end = q.End
	}
}

// numP06File is testdata/numbers-p06.json: P0-6 pairs with the sentence of
// the other answer a judge would cite, and what Mismatch and Unmatched
// gave on them when last measured.
type numP06File struct {
	About string       `json:"about"`
	Pairs []numP06Pair `json:"pairs"`
}

type numP06Pair struct {
	ID        string `json:"id"`
	Label     string `json:"label"`             // the gold label: opposite or same
	Numeric   *bool  `json:"numeric,omitempty"` // opposites: the contradiction is about a number
	Claim     string `json:"claim"`
	Other     string `json:"other"`
	Mismatch  bool   `json:"mismatch"`  // Mismatch(Numbers(claim), Numbers(other))
	Unmatched bool   `json:"unmatched"` // Unmatched found a number on either side
}

const numP06Path = "testdata/numbers-p06.json"

var numLinkRe = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

// TestNumbersP06 measures Mismatch on P0-6: how many of the gold opposites
// about a number it flags, and how many gold same pairs it flags although
// they agree. The expectations are measured results (see numUpdate); a
// change to the rules shows here as a diff to look at.
func TestNumbersP06(t *testing.T) {
	raw, err := os.ReadFile(numP06Path)
	if err != nil {
		t.Fatal(err)
	}
	var f numP06File
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	gold := map[string]p06Pair{}
	for _, p := range p06Pairs(t) {
		gold[p.ID] = p
	}
	claims := map[string]string{}
	for _, c := range p06Claims(t) {
		claims[fmt.Sprintf("%d-%s%d", c.Q, c.AI, c.N)] = c.Text
	}
	type tally struct{ n, mismatch, unmatched int }
	var numeric, other, same tally
	digit := regexp.MustCompile(`[0-9]`)
	for i := range f.Pairs {
		p := &f.Pairs[i]
		g, ok := gold[p.ID]
		if !ok || g.Label != p.Label || claims[fmt.Sprintf("%d-%s%d", g.Q, g.From, g.N)] != p.Claim {
			t.Errorf("%s: not the P0-6 pair (label %q, claim %q)", p.ID, p.Label, p.Claim)
			continue
		}
		answer := p06FindAnswer(t, g.Q, g.Against).Answer
		flat := strings.NewReplacer("**", "", "`", "").Replace(numLinkRe.ReplaceAllString(answer, "$1"))
		if !strings.Contains(flat, p.Other) {
			t.Errorf("%s: other sentence not in the answer of %s: %q", p.ID, g.Against, p.Other)
		}
		a, b := Numbers(p.Claim), Numbers(p.Other)
		mm := Mismatch(a, b)
		um := len(Unmatched(a, b)) > 0 || len(Unmatched(b, a)) > 0
		var tl *tally
		switch {
		case p.Label == LabelOpposite && p.Numeric != nil && *p.Numeric:
			tl = &numeric
		case p.Label == LabelOpposite:
			tl = &other
		case p.Label == LabelSame:
			tl = &same
			if !digit.MatchString(p.Claim) || !digit.MatchString(p.Other) {
				t.Errorf("%s: a same pair needs digits on both sides", p.ID)
			}
		default:
			t.Errorf("%s: label %q", p.ID, p.Label)
			continue
		}
		tl.n++
		if mm {
			tl.mismatch++
		}
		if um {
			tl.unmatched++
		}
		if *numUpdate {
			p.Mismatch, p.Unmatched = mm, um
			continue
		}
		if mm != p.Mismatch || um != p.Unmatched {
			t.Errorf("%s (%s): Mismatch %v, Unmatched %v; measured before %v, %v\n claim %s\n other %s",
				p.ID, p.Label, mm, um, p.Mismatch, p.Unmatched, numDump(a), numDump(b))
		}
	}
	if numeric.n+other.n != 21 || same.n < 40 {
		t.Errorf("%d opposites and %d same pairs; want all 21 and at least 40", numeric.n+other.n, same.n)
	}
	t.Logf("gold opposites about a number: %d; Mismatch flags %d, Unmatched %d", numeric.n, numeric.mismatch, numeric.unmatched)
	t.Logf("other gold opposites: %d; Mismatch flags %d, Unmatched %d", other.n, other.mismatch, other.unmatched)
	t.Logf("gold same pairs with digits on both sides: %d; Mismatch flags %d, Unmatched %d", same.n, same.mismatch, same.unmatched)
	if *numUpdate {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", " ")
		if err := enc.Encode(f); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(numP06Path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", filepath.ToSlash(numP06Path))
	}
}

// TestNumbersP06Answers reads all 90 P0-6 answers: every span is in the
// text and every value in order.
func TestNumbersP06Answers(t *testing.T) {
	total := 0
	for _, a := range p06Answers(t) {
		qs := Numbers(a.Answer)
		total += len(qs)
		numCheck(t, a.Answer, qs)
	}
	t.Logf("%d quantities in the 90 answers", total)
}

// numCheck checks what FuzzNumbers checks: spans in the text, in order and
// apart, on character boundaries; values finite and ordered; each value
// fits itself; Compatible symmetric.
func numCheck(t *testing.T, text string, qs []Quantity) {
	t.Helper()
	// Character boundaries as Go decodes text: a byte that is no valid
	// UTF-8 counts as one character.
	bound := make([]bool, len(text)+1)
	for i := range text {
		bound[i] = true
	}
	bound[len(text)] = true
	math := InlineMath(text)
	end := 0
	for i, q := range qs {
		if q.Start < end || q.End <= q.Start || q.End > len(text) || text[q.Start:q.End] != q.Text {
			t.Fatalf("quantity %d: span %d..%d %q (last end %d) in %q", i, q.Start, q.End, q.Text, end, text)
		}
		if !bound[q.Start] || !bound[q.End] {
			t.Fatalf("quantity %d: span %d..%d cuts a character in %q", i, q.Start, q.End, text)
		}
		for _, m := range math {
			if q.Start < m.End && m.Start < q.End {
				t.Fatalf("quantity %d %q overlaps the math at %d..%d in %q", i, q.Text, m.Start, m.End, text)
			}
		}
		end = q.End
		if q.Kind == "" || !numSane(&q.num) {
			t.Fatalf("quantity %d: %s", i, numDump([]Quantity{q}))
		}
		if !Compatible(q, q) {
			t.Fatalf("quantity %d does not fit itself: %s", i, numDump([]Quantity{q}))
		}
		if i > 0 && Compatible(q, qs[i-1]) != Compatible(qs[i-1], q) {
			t.Fatalf("Compatible not symmetric: %s", numDump(qs[i-1:i+1]))
		}
	}
	if Mismatch(qs, qs) {
		t.Fatalf("a text mismatches itself: %s", numDump(qs))
	}
}

func FuzzNumbers(f *testing.F) {
	for _, s := range []string{
		"약 8분 20초(약 499초)", "약 930만 명, 9,279,861명", "200~300mg/일 이하, 약 1~2잔", "970만 명 안팎",
		"2026년 10월 27~28일", "October 25, 2026", "3.15.2", "v2.1", "thousands of times", "수만~수십만 년",
		"$1.05, 5 cents", "16–19°C / 60–67°F", "above ~6–7%", "between 10 and 20 people", "1억 5천", "3분의 1",
		"2026. 10. 16.", "20+ minutes", "10여 명", "초속 약 30만 km", "https://x.org/2026", "1,2,3,4,5,6",
		"9,999,999,999,999,999", "0.0000000001", "1.2.3.4.5.6.7.8.9", "~~~~1", "약약약 1만", "\xff\xfe1만\xc0",
		"0x\xa7000", // a span may end before a byte that is no UTF-8
		"3천500만 원, 3~4만 원, 1억2천500만원", "-5~-3°C, −20°C, 10월 27일-28일", "1.496 × 10^8 km, 3 × 10⁸, 1920×1080, 175 ± 6 cm",
		"100여 명, 20년 남짓", "오후 2시 30분, 제3조, 1544-0010", "$3 \\times 10^8$ m/s 5", "October 27–28, 2026", "No more than 200 mg",
		"1. 서울\n  2) 3천500만 원\n10) 5개, 2026. 10. 30.", "$x$ 1. 2배\n\t3) 약 1억", "$50M, about $50M, $20k, 2 thousand, -20,000원 이상",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		numCheck(t, text, Numbers(text))
		// Two halves as two texts: Mismatch is symmetric, Unmatched gives
		// indexes of a in order.
		a, b := Numbers(text[:len(text)/2]), Numbers(text[len(text)/2:])
		if Mismatch(a, b) != Mismatch(b, a) {
			t.Fatalf("Mismatch not symmetric:\n%s\n%s", numDump(a), numDump(b))
		}
		last := -1
		for _, i := range Unmatched(a, b) {
			if i <= last || i >= len(a) {
				t.Fatalf("Unmatched index %d after %d of %d", i, last, len(a))
			}
			last = i
		}
		numSameAsScan(t, a, b)
	})
}

// numSameAsScan checks that the index a large text gets compares as the
// pair-by-pair scan does.
func numSameAsScan(t *testing.T, a, b []Quantity) {
	t.Helper()
	scan, index := numMatcher{b: b}, numMatcher{b: b, ix: numIndexOf(b)}
	if len(a) > 0 && len(b) > 0 && scan.mismatch(a) != index.mismatch(a) {
		t.Fatalf("Mismatch: scan %v, index %v\n%s\n%s", scan.mismatch(a), index.mismatch(a), numDump(a), numDump(b))
	}
	if s, x := scan.unmatched(a), index.unmatched(a); !slices.Equal(s, x) {
		t.Fatalf("Unmatched: scan %v, index %v\n%s\n%s", s, x, numDump(a), numDump(b))
	}
}

// The index gives what the scan gives, on texts of every kind, shape and
// class: points, limits, ranges given as limits, dates with and without a
// year, versions, bare counts and counters.
func TestNumbersIndexMatchesScan(t *testing.T) {
	pieces := []string{
		"약 8분 20초", "8분 19초", "200~300mg/일 이하", "200~300mg/일", "200mg 미만", "400mg 이상", "100mg 이하", "약 930만",
		"9,279,861명", "3잔", "5명", "25개", "약 970만 명", "약 956만 명", "2026년 9월 18일", "9월 18일", "10월 23일", "9월",
		"2024년 말", "2026년", "1990년대", "3.15.2", "v3.15", "3.10 이상", "30%", "30%p", "약 1~2잔", "2잔 이내", "100여 명",
		"-5°C", "20+ minutes", "at least 5 km", "5 km", "2020~2024년", "$5", "5달러", "3분의 1", "1.5배", "under about 4–5%",
		"Rate below ~4%", "60–70%", "3~4만 원 이하", "3만~4만 원 이하", "약 1억 4,960만 km", "2026년 10월 27~28일",
	}
	rng := rand.New(rand.NewPCG(9, 4))
	text := func() string {
		var b strings.Builder
		for range 1 + rng.IntN(60) {
			b.WriteString(pieces[rng.IntN(len(pieces))] + ", ")
		}
		return b.String()
	}
	for range 2000 {
		numSameAsScan(t, Numbers(text()), Numbers(text()))
	}
}

// TestNumbersLinear reads 64 KiB of answers made to be hard in under 50 ms
// each: the reading is one pass with bounded look-back.
func TestNumbersLinear(t *testing.T) {
	for name, unit := range map[string]string{
		"numbers":   "약 1억 4,960만 km 안팎 이하, 200~300mg/일, 2026년 10월 27~28일, 3.15.2; ",
		"digits":    "1 2 3 4 5 6 7 8 9 0 ",
		"tildes":    "1~2~3~4~5~",
		"hedges":    "약 약 약 about about ~ ",
		"separator": "1,000,000,000,",
		"korean":    "수천수만수십만 년년년 월월 일일 ",
		"mixed":     "x86 A20 CO2 HTTP/2 v1.2.3 $5 ",
	} {
		t.Run(name, func(t *testing.T) {
			text := strings.Repeat(unit, MaxAnswerBytes/len(unit)+1)[:MaxAnswerBytes]
			best := time.Hour
			for n := 0; n < 3; n++ {
				start := time.Now()
				qs := Numbers(text)
				if d := time.Since(start); d < best {
					best = d
				}
				if n == 0 {
					numCheck(t, text, qs)
				}
			}
			if best > 50*time.Millisecond {
				t.Errorf("64 KiB took %v, want under 50ms", best)
			}
			// numbersAbsent compares a unit with a whole answer.
			unitQs, answerQs := Numbers(unit), Numbers(text)
			start := time.Now()
			Mismatch(unitQs, answerQs)
			Unmatched(unitQs, answerQs)
			Unmatched(answerQs, unitQs)
			t.Logf("%d quantities in %v; a unit against them in %v", len(answerQs), best, time.Since(start))
		})
	}
}
