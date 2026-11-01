package live

// This file has no build tag: its test runs with every go test (test-all),
// and the live tests (LT-06a, LT-06b, LT-08) share its answers and
// helpers.

import (
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

const lt06aQuestion = "가장 최신 아이폰 모델은 무엇이고 언제 나왔나요?"

// lt06aAnswers are P0-6 Q12's answers (docs/judge, 2026-10-30) cut down:
// Claude's first sentence and its 발표, 출시 and 칩 items and the line on
// the plain 18; Codex's two sentences without the link; OpenCode's first
// paragraph and its 정리 sentence. The models are those of the P0-6 run.
var lt06aAnswers = []struct{ agent, model, text string }{
	{"claude", "claude-sonnet-5-5", "2026년 10월 30일 기준으로 가장 최신 아이폰은 **iPhone 18 Pro**와 **iPhone 18 Pro Max**입니다.\n\n" +
		"- **발표:** 2026년 9월 9일\n- **출시:** 2026년 9월 18일. 다른 20개 국가와 지역에는 9월 25일부터 출시됐습니다.\n- **칩:** A20 Pro\n\n" +
		"일반 iPhone 18은 이번 가을에 나오지 않았습니다. MacRumors에 따르면 2027년 봄에 출시될 예정입니다."},
	{"codex", "gpt-6.1-terra", "2026년 10월 30일 기준 가장 최근에 출시된 아이폰은 **아이폰 듀오(iPhone Duo)**입니다. 2026년 9월 9일 공개됐고, **10월 23일** 출시됐습니다."},
	{"opencode", "opencode-go/deepseek-v4.1-flash#max", "**가장 최신 아이폰은 '아이폰 듀오(iPhone Duo)'** 입니다. 애플의 첫 폴더블 아이폰으로, 2026년 9월 9일 공개됐고 **2026년 10월 23일 출시**됐습니다(한국도 같은 날 1차 출시국으로 판매 시작).\n\n" +
		"정리하면 \"현재 시점의 최신 모델\"은 10월 23일 나온 아이폰 듀오이고, 가장 최근에 대중적으로 풀린 주력 모델은 9월 18일 출시된 아이폰 18 프로 시리즈입니다."},
}

func answer(id string, j core.Judger, model, text string) judge.Answer {
	units, tail, _ := judge.SplitAnswer(id, text, judge.MaxUnits)
	notJudged := 0
	for _, u := range units {
		if !u.Judged {
			notJudged++
		}
	}
	fam := ""
	if j != nil {
		fam = j.Family(model)
	}
	return judge.Answer{ID: id, Agent: id, Via: judge.ViaPC, Family: fam, Model: model, Status: judge.AnswerReady,
		NotJudged: notJudged, Units: units, Tail: tail, Text: text}
}

// lt06aState is where unit ends up on the phone when request r, whose
// validated output is v, is all that was judged: judge.Aggregate with the
// default checks once every request has ended, as the agent makes the
// cards (S6R-2). shown is true on 다른 말 (differ) or 확인 필요 (check),
// where the phone shows a disagreement; 같은 말, 한 AI만 or a hidden unit
// would hide it. answers are r.From's and r.Against's.
func lt06aState(answers []judge.Answer, r judge.Request, v judge.Validated, unit string) (state string, shown bool) {
	r.Status = judge.RequestDone
	s := judge.Aggregate(judge.AggregateInput{Answers: answers, Requests: []judge.Request{r}, Judgments: judge.Judgments(r, v),
		Checks: judge.DefaultChecks, Ended: true})
	for _, u := range s.Units {
		if u.Unit == unit {
			return u.State, u.State == judge.CardDiffer || u.State == judge.CardCheck
		}
	}
	return "", false
}

// S6R-2: LT-06a passes the newest-model opposite of Q12 only where the
// phone shows it as a disagreement. Since S6 an unsure same counts as same
// and an unsure absent as absent unless a number check fires, so 'opposite
// or unsure' passed verdicts the phone puts on 같은 말 or 한 AI만.
func TestLT06aStateIsTheProducts(t *testing.T) {
	byID := map[string]judge.Answer{}
	for _, a := range lt06aAnswers {
		byID[a.agent] = answer(a.agent, nil, a.model, a.text)
	}
	pair := []judge.Answer{byID["claude"], byID["codex"]}
	r := judge.Request{ID: "r1", From: "claude", Against: "codex", Judge: "opencode"}
	// Claude's 출시 unit: its dates (9월 18일, 9월 25일) fit none of Codex's
	// second sentence (9월 9일, 10월 23일).
	release := ""
	for _, u := range byID["claude"].Units {
		if strings.Contains(u.Text, "9월 18일") && u.Judged {
			release = u.ID
			break
		}
	}
	if release == "" || release == "claude:1" {
		t.Fatalf("no 출시 unit in %+v", byID["claude"].Units)
	}
	n := strings.TrimPrefix(release, "claude:")
	vd := func(id, label string, unsure bool, evidence ...string) judge.Validated {
		return judge.Validated{Verdicts: []judge.Verdict{{ID: id, Core: true, Label: label, Unsure: unsure, Evidence: append([]string{}, evidence...)}}}
	}
	for _, c := range []struct {
		name      string
		v         judge.Validated
		unit      string
		wantState string
		wantShown bool
	}{
		{"a sure opposite", vd("a1", judge.LabelOpposite, false, "b1"), "claude:1", judge.CardDiffer, true},
		{"an unsure opposite", vd("a1", judge.LabelOpposite, true), "claude:1", judge.CardDiffer, true},
		// The review's case: both units hold 2026년 10월 30일, so no
		// number check fires and the phone shows 같은 말.
		{"an unsure same with evidence", vd("a1", judge.LabelSame, true, "b1"), "claude:1", judge.CardAgree, false},
		{"an unsure same without evidence", vd("a1", judge.LabelSame, true), "claude:1", judge.CardAgree, false},
		{"a sure same", vd("a1", judge.LabelSame, false, "b1"), "claude:1", judge.CardAgree, false},
		{"an unsure absent", vd("a1", judge.LabelAbsent, true), "claude:1", judge.CardOnlyOne, false},
		// An unsure same whose numbers clash is on 확인 필요: shown.
		{"an unsure same whose dates clash", vd("a"+n, judge.LabelSame, true, "b2"), release, judge.CardCheck, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			state, shown := lt06aState(pair, r, c.v, c.unit)
			if state != c.wantState || shown != c.wantShown {
				t.Errorf("%s: state %q shown %v, want %q %v", c.unit, state, shown, c.wantState, c.wantShown)
			}
		})
	}
	if _, shown := lt06aState(pair, r, judge.Validated{}, "claude:1"); shown {
		t.Error("a unit no verdict judged is shown")
	}
}
