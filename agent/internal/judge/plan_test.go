package judge

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// planAnswer is a ready answer with n units, all judged.
func planAnswer(id, agent, via, family string, n int) Answer {
	a := Answer{ID: id, Agent: agent, Via: via, Family: family, Status: AnswerReady, Units: []Unit{}}
	for i := 1; i <= n; i++ {
		a.Units = append(a.Units, Unit{ID: fmt.Sprintf("%s:%d", id, i), N: i, Kind: UnitSentence, Text: fmt.Sprintf("sentence %d", i), Judged: true})
	}
	return a
}

// planPC3 are the three PC answers and judges of the default setup.
func planPC3() ([]Answer, []JudgeAI) {
	return []Answer{
		planAnswer("claude", "claude", ViaPC, "anthropic", 3),
		planAnswer("codex", "codex", ViaPC, "openai", 3),
		planAnswer("opencode", "opencode", ViaPC, "deepseek", 3),
	}, []JudgeAI{
		{Agent: "claude", Family: "anthropic", Model: "haiku", Effort: "medium"},
		{Agent: "codex", Family: "openai", Model: "gpt-6-luna", Effort: "medium"},
		{Agent: "opencode", Family: "deepseek", Model: "opencode-go/deepseek-v4.1-flash#high"},
	}
}

// planCheck checks the rules every plan keeps: ids r1, r2, …; a judge is
// in s.Judges, is neither answer's program nor of either's company, and
// takes both directions of its pair; a record without a judge is a failed
// no_judge one.
func planCheck(t *testing.T, s Spec, reqs []Request) {
	t.Helper()
	answers := map[string]Answer{}
	for _, a := range s.Answers {
		answers[a.ID] = a
	}
	judges := map[string]JudgeAI{}
	for _, j := range s.Judges {
		judges[j.Agent] = j
	}
	byPair := map[string]string{}
	for i, r := range reqs {
		if r.ID != fmt.Sprintf("r%d", i+1) || r.Attempt != 0 {
			t.Errorf("request %d = %+v", i, r)
		}
		x, y := answers[r.From], answers[r.Against]
		if x.ID == "" || y.ID == "" || x.ID == y.ID {
			t.Errorf("%s: from %q against %q", r.ID, r.From, r.Against)
		}
		key := min(r.From, r.Against) + "|" + max(r.From, r.Against)
		if prev, ok := byPair[key]; ok && prev != r.Judge {
			t.Errorf("%s: pair %s split between %q and %q", r.ID, key, prev, r.Judge)
		}
		byPair[key] = r.Judge
		if r.Judge == "" {
			if r.Status != RequestFailed || r.Error == nil || r.Error.Code != core.ErrCodeNoJudge || r.JudgeOnly || r.Model != "" {
				t.Errorf("%s: record %+v", r.ID, r)
			}
			continue
		}
		j, ok := judges[r.Judge]
		if !ok || r.Status != RequestQueued || r.Error != nil || r.Model != j.Model {
			t.Errorf("%s: request %+v", r.ID, r)
		}
		for _, a := range []Answer{x, y} {
			if !planTestMay(j, a) {
				t.Errorf("%s: %s (%s) judges %s's answer (%s, %s)", r.ID, j.Agent, j.Family, a.ID, a.Agent, a.Family)
			}
		}
	}
}

// planTestMay is the rule written out again: another program, another
// company; a pasted answer is its app's company too, and an app out of the
// table is of unknown company; unknown is no.
func planTestMay(j JudgeAI, a Answer) bool {
	families := []string{a.Family}
	if a.Via == ViaHandoff {
		families = append(families, map[string]string{"chatgpt": "openai", "claude": "anthropic", "gemini": "google"}[a.Agent])
		if a.Family == "" {
			families = families[1:]
		}
	}
	ok := j.Agent != "" && j.Family != "" && a.Agent != "" && !strings.EqualFold(j.Agent, a.Agent)
	for _, f := range families {
		ok = ok && f != "" && !strings.EqualFold(j.Family, f)
	}
	return ok
}

// planLoads counts the requests each judge takes.
func planLoads(reqs []Request) map[string]int {
	out := map[string]int{}
	for _, r := range reqs {
		if r.Judge != "" {
			out[r.Judge]++
		}
	}
	return out
}

func TestPlanThreeAIs(t *testing.T) {
	answers, judges := planPC3()
	s := Spec{Question: "q", Answers: answers, Judges: judges}
	reqs, err := Plan(s)
	if err != nil {
		t.Fatal(err)
	}
	// The design: C judges A→B and B→A, B judges A→C and C→A, A judges
	// B→C and C→B; two each, all at once.
	q := func(id, from, against, judge, model string) Request {
		return Request{ID: id, From: from, Against: against, Judge: judge, Model: model, Status: RequestQueued}
	}
	want := []Request{
		q("r1", "claude", "codex", "opencode", "opencode-go/deepseek-v4.1-flash#high"),
		q("r2", "codex", "claude", "opencode", "opencode-go/deepseek-v4.1-flash#high"),
		q("r3", "claude", "opencode", "codex", "gpt-6-luna"),
		q("r4", "opencode", "claude", "codex", "gpt-6-luna"),
		q("r5", "codex", "opencode", "claude", "haiku"),
		q("r6", "opencode", "codex", "claude", "haiku"),
	}
	if !reflect.DeepEqual(reqs, want) {
		t.Fatalf("requests =\n%+v\nwant\n%+v", reqs, want)
	}
	planCheck(t, s, reqs)
	raw, _ := json.Marshal(reqs[0])
	if string(raw) != `{"id":"r1","from":"claude","against":"codex","judge":"opencode","judgeOnly":false,"model":"opencode-go/deepseek-v4.1-flash#high","status":"queued","attempt":0}` {
		t.Errorf("JSON = %s", raw)
	}
	// The same input gives the same plan.
	again, _ := Plan(s)
	if !reflect.DeepEqual(again, reqs) {
		t.Errorf("second plan = %+v", again)
	}
}

func TestPlanSameCompany(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(answers []Answer, judges []JudgeAI)
		judgeBy map[string]string // "from>against" → judge; "" for a no_judge record
	}{
		{
			// OpenCode answered with a Claude model, so its answer is
			// Anthropic's: Claude Code may not judge it, and nobody is left
			// for codex↔opencode.
			name:   "opencode answered with a claude model",
			change: func(a []Answer, _ []JudgeAI) { a[2].Family = "anthropic" },
			judgeBy: map[string]string{
				"claude>codex": "opencode", "codex>claude": "opencode",
				"claude>opencode": "codex", "opencode>claude": "codex",
				"codex>opencode": "", "opencode>codex": "",
			},
		},
		{
			// OpenCode judges with a GPT model, so it may not judge Codex's
			// answer.
			name:   "opencode judges with a gpt model",
			change: func(_ []Answer, j []JudgeAI) { j[2].Family = "openai" },
			judgeBy: map[string]string{
				"claude>codex": "", "codex>claude": "",
				"claude>opencode": "codex", "opencode>claude": "codex",
				"codex>opencode": "claude", "opencode>codex": "claude",
			},
		},
		{
			// The program rule: on the deep tier OpenCode answers with Kimi
			// and judges with DeepSeek, yet never judges its own answer.
			name:   "opencode answered with kimi",
			change: func(a []Answer, _ []JudgeAI) { a[2].Family = "moonshot" },
			judgeBy: map[string]string{
				"claude>codex": "opencode", "codex>claude": "opencode",
				"claude>opencode": "codex", "opencode>claude": "codex",
				"codex>opencode": "claude", "opencode>codex": "claude",
			},
		},
		{
			name:   "family names differ only in case",
			change: func(a []Answer, _ []JudgeAI) { a[2].Family = "Anthropic" },
			judgeBy: map[string]string{
				"claude>codex": "opencode", "codex>claude": "opencode",
				"claude>opencode": "codex", "opencode>claude": "codex",
				"codex>opencode": "", "opencode>codex": "",
			},
		},
		{
			// An unknown company matches every company.
			name:   "an answer of unknown company",
			change: func(a []Answer, _ []JudgeAI) { a[0].Family = "" },
			judgeBy: map[string]string{
				"claude>codex": "", "codex>claude": "",
				"claude>opencode": "", "opencode>claude": "",
				"codex>opencode": "claude", "opencode>codex": "claude",
			},
		},
		{
			name:   "a judge of unknown company",
			change: func(_ []Answer, j []JudgeAI) { j[0].Family = "" },
			judgeBy: map[string]string{
				"claude>codex": "opencode", "codex>claude": "opencode",
				"claude>opencode": "codex", "opencode>claude": "codex",
				"codex>opencode": "", "opencode>codex": "",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answers, judges := planPC3()
			tc.change(answers, judges)
			s := Spec{Answers: answers, Judges: judges}
			reqs, err := Plan(s)
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for _, r := range reqs {
				got[r.From+">"+r.Against] = r.Judge
			}
			if !reflect.DeepEqual(got, tc.judgeBy) || len(reqs) != 6 {
				t.Errorf("judges = %v, want %v", got, tc.judgeBy)
			}
			planCheck(t, s, reqs)
		})
	}
}

func TestPlanJudgeOnly(t *testing.T) {
	two := func() []Answer {
		return []Answer{
			planAnswer("claude", "claude", ViaPC, "anthropic", 2),
			planAnswer("codex", "codex", ViaPC, "openai", 2),
			{ID: "opencode", Agent: "opencode", Via: ViaPC, Family: "deepseek", Status: AnswerMissing},
		}
	}
	authors := []JudgeAI{{Agent: "claude", Family: "anthropic"}, {Agent: "codex", Family: "openai"}}
	third := func(agent, family, askCode string) JudgeAI {
		return JudgeAI{Agent: agent, Family: family, Model: agent + "-judge", AskCode: askCode}
	}
	for _, tc := range []struct {
		name   string
		judges []JudgeAI
		want   string // the judge of both directions; "" for no_judge
	}{
		{"the third AI timed out", append(slices.Clone(authors), third("opencode", "deepseek", core.ErrCodeAskTimeout)), "opencode"},
		{"empty answer before ask_failed", append(slices.Clone(authors), third("opencode", "deepseek", core.ErrCodeAskFailed), third("other", "zhipu", core.ErrCodeAskEmpty)), "other"},
		{"ask_failed when nobody else may", append(slices.Clone(authors), third("opencode", "deepseek", core.ErrCodeAskFailed)), "opencode"},
		{"ties go to the earlier judge", append(slices.Clone(authors), third("opencode", "deepseek", core.ErrCodeAskEmpty), third("other", "zhipu", core.ErrCodeAskTimeout)), "opencode"},
		{"ties go to the earlier judge, other order", append(slices.Clone(authors), third("other", "zhipu", core.ErrCodeAskTimeout), third("opencode", "deepseek", core.ErrCodeAskEmpty)), "other"},
		{"the third AI turned off", authors, ""},
		{"the third AI of an answer's company", append(slices.Clone(authors), third("opencode", "openai", core.ErrCodeAskTimeout)), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Spec{Answers: two(), Judges: tc.judges}
			reqs, err := Plan(s)
			planCheck(t, s, reqs)
			if len(reqs) != 2 || reqs[0].From != "claude" || reqs[1].From != "codex" {
				t.Fatalf("requests = %+v", reqs)
			}
			for _, r := range reqs {
				if r.Judge != tc.want || r.JudgeOnly != (tc.want != "") {
					t.Errorf("%s: judge %q, judgeOnly %v; want %q", r.ID, r.Judge, r.JudgeOnly, tc.want)
				}
				if tc.want != "" && r.Model != tc.want+"-judge" {
					t.Errorf("%s: model %q", r.ID, r.Model)
				}
			}
			var je *Error
			if tc.want == "" {
				if !errors.As(err, &je) || je.Code != core.ErrCodeNoJudge {
					t.Errorf("error = %v, want no_judge", err)
				}
			} else if err != nil {
				t.Errorf("error = %v", err)
			}
		})
	}
}

func TestPlanNoJudgeRecords(t *testing.T) {
	s := Spec{
		Answers: []Answer{planAnswer("claude", "claude", ViaPC, "anthropic", 1), planAnswer("codex", "codex", ViaPC, "openai", 1)},
		Judges:  []JudgeAI{{Agent: "claude", Family: "anthropic"}, {Agent: "codex", Family: "openai"}},
	}
	reqs, err := Plan(s)
	var je *Error
	if !errors.As(err, &je) || je.Code != core.ErrCodeNoJudge || err.Error() != core.ErrCodeNoJudge {
		t.Fatalf("error = %v", err)
	}
	raw, _ := json.Marshal(reqs)
	want := `[{"id":"r1","from":"claude","against":"codex","judge":"","judgeOnly":false,"status":"failed","attempt":0,"error":{"code":"no_judge"}},` +
		`{"id":"r2","from":"codex","against":"claude","judge":"","judgeOnly":false,"status":"failed","attempt":0,"error":{"code":"no_judge"}}]`
	if string(raw) != want {
		t.Errorf("records = %s", raw)
	}

	// With three answers and one AI unable to judge (an old CLI), only the
	// pair it would have judged gets records, and the plan goes on.
	answers, judges := planPC3()
	s = Spec{Answers: answers, Judges: judges[:2]} // OpenCode does not announce judge
	reqs, err = Plan(s)
	if err != nil {
		t.Fatal(err)
	}
	planCheck(t, s, reqs)
	var records []string
	for _, r := range reqs {
		if r.Judge == "" {
			records = append(records, r.From+">"+r.Against)
		}
	}
	if !slices.Equal(records, []string{"claude>codex", "codex>claude"}) || len(reqs) != 6 {
		t.Errorf("no_judge records %v in %+v", records, reqs)
	}
}

func TestPlanNeedTwoAnswers(t *testing.T) {
	answers, judges := planPC3()
	answers[1].Status = AnswerMissing
	answers[2].Status = ""
	for name, list := range map[string][]Answer{
		"none":           nil,
		"one":            answers[:1],
		"one of three":   answers,
		"all missing":    {answers[1]},
		"missing as two": {answers[1], answers[1]},
	} {
		reqs, err := Plan(Spec{Answers: list, Judges: judges})
		var je *Error
		if reqs != nil || !errors.As(err, &je) || je.Code != core.ErrCodeNeedTwoAnswers {
			t.Errorf("%s: requests %+v, error %v", name, reqs, err)
		}
	}
}

func TestPlanHandoff(t *testing.T) {
	answers, judges := planPC3()
	gemini := planAnswer("p1", "gemini", ViaHandoff, HandoffFamily("gemini"), 2)

	t.Run("three PC answers and Gemini make 12, 4 per AI", func(t *testing.T) {
		s := Spec{Answers: append(slices.Clone(answers), gemini), Judges: judges}
		reqs, err := Plan(s)
		if err != nil {
			t.Fatal(err)
		}
		planCheck(t, s, reqs)
		if len(reqs) != 12 || !reflect.DeepEqual(planLoads(reqs), map[string]int{"claude": 4, "codex": 4, "opencode": 4}) {
			t.Errorf("loads %v over %d requests", planLoads(reqs), len(reqs))
		}
		for _, r := range reqs {
			if r.JudgeOnly {
				t.Errorf("%s: judgeOnly although %s answered", r.ID, r.Judge)
			}
		}
		// Whatever the order of the judges, the loads stay even.
		s.Judges = []JudgeAI{judges[2], judges[0], judges[1]}
		reqs, _ = Plan(s)
		planCheck(t, s, reqs)
		if !reflect.DeepEqual(planLoads(reqs), map[string]int{"claude": 4, "codex": 4, "opencode": 4}) {
			t.Errorf("other order: loads %v", planLoads(reqs))
		}
	})

	t.Run("Codex never judges a ChatGPT answer", func(t *testing.T) {
		// Its company comes from the handoff table, even when the caller
		// gave none or another.
		for _, family := range []string{HandoffFamily("chatgpt"), "", "google"} {
			chatgpt := planAnswer("p2", "chatgpt", ViaHandoff, family, 2)
			s := Spec{Answers: []Answer{answers[0], chatgpt}, Judges: judges}
			reqs, err := Plan(s)
			if err != nil || len(reqs) != 2 || reqs[0].Judge != "opencode" || !reqs[0].JudgeOnly {
				t.Errorf("family %q: requests %+v, error %v", family, reqs, err)
			}
			planCheck(t, s, reqs)
			s.Judges = []JudgeAI{judges[1]}
			if reqs, err := Plan(s); err == nil || reqs[0].Judge != "" {
				t.Errorf("family %q, Codex alone: %+v, %v", family, reqs, err)
			}
		}
		// A PC answer has no app: its Family is all there is.
		pc := planAnswer("codex", "codex", ViaPC, "", 1)
		if reqs, err := Plan(Spec{Answers: []Answer{answers[0], pc}, Judges: judges}); err == nil || reqs[0].Judge != "" {
			t.Errorf("PC answer of no family: %+v, %v", reqs, err)
		}
	})

	t.Run("Claude Code never judges a Claude app answer", func(t *testing.T) {
		app := planAnswer("p3", "claude", ViaHandoff, HandoffFamily("claude"), 2)
		s := Spec{Answers: []Answer{answers[1], app}, Judges: judges}
		reqs, err := Plan(s)
		if err != nil || len(reqs) != 2 || reqs[0].Judge != "opencode" || !reqs[0].JudgeOnly {
			t.Errorf("requests %+v, error %v", reqs, err)
		}
		planCheck(t, s, reqs)
		// Claude Code wrote no answer here, so with OpenCode turned off it
		// is still no judge: the app's answer is its company's.
		s.Judges = judges[:2]
		if reqs, err := Plan(s); err == nil || reqs[0].Judge != "" {
			t.Errorf("without OpenCode: %+v, %v", reqs, err)
		}
	})

	t.Run("answers without units still spread the requests evenly", func(t *testing.T) {
		// Five answers, two of them without a unit: 12 requests, and pairs
		// that make one request count as one.
		pc, _ := planPC3()
		pc[0].Units, pc[1].Units, pc[2].Units = pc[0].Units[:1], []Unit{}, pc[2].Units[:1]
		s := Spec{Answers: append(pc, planAnswer("p3", "gemini", ViaHandoff, "google", 1), planAnswer("p4", "chatgpt", ViaHandoff, "openai", 0)), Judges: judges}
		reqs, err := Plan(s)
		if err != nil {
			t.Fatal(err)
		}
		planCheck(t, s, reqs)
		if len(reqs) != 12 || !reflect.DeepEqual(planLoads(reqs), map[string]int{"claude": 4, "codex": 4, "opencode": 4}) {
			t.Errorf("loads %v over %d requests", planLoads(reqs), len(reqs))
		}
	})

	t.Run("two pasted answers go to the earlier of two free judges", func(t *testing.T) {
		chatgpt := planAnswer("p2", "chatgpt", ViaHandoff, "openai", 1)
		s := Spec{Answers: []Answer{chatgpt, gemini}, Judges: judges}
		reqs, _ := Plan(s)
		planCheck(t, s, reqs)
		if reqs[0].Judge != "claude" || !reqs[0].JudgeOnly {
			t.Errorf("requests %+v", reqs)
		}
		s.Judges = []JudgeAI{judges[2], judges[1], judges[0]}
		if reqs, _ := Plan(s); reqs[0].Judge != "opencode" {
			t.Errorf("other order: %+v", reqs)
		}
	})
}

func TestPlanNothingToJudge(t *testing.T) {
	answers, judges := planPC3()
	// Codex's answer was only code: no unit. Its units are judged nowhere,
	// and the others are still judged against it, with its code.
	units, tail, _ := SplitAnswer("codex", "```python\nprint(2**10)\n```", 0)
	answers[1].Units, answers[1].Tail = units, tail
	reqs, err := Plan(Spec{Answers: answers, Judges: judges})
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, r := range reqs {
		dirs = append(dirs, r.ID+":"+r.From+">"+r.Against+"/"+r.Judge)
	}
	want := []string{"r1:claude>codex/opencode", "r2:claude>opencode/codex", "r3:opencode>claude/codex", "r4:opencode>codex/claude"}
	if !slices.Equal(dirs, want) {
		t.Errorf("requests %v, want %v", dirs, want)
	}
	if task := BuildTask(Spec{Answers: answers}, reqs[0]); !reflect.DeepEqual(task.Other, []core.JudgeUnit{{Context: "(code, not judged) print(2**10)"}}) {
		t.Errorf("ANSWER B of a code-only answer = %+v", task.Other)
	}

	// Units past the limit are evidence only, so they make no request.
	answers[0].Units = []Unit{{ID: "claude:41", N: 41, Kind: UnitSentence, Text: "x", Judged: false}}
	answers[2].Units = nil
	reqs, err = Plan(Spec{Answers: answers, Judges: judges})
	if err != nil || reqs == nil || len(reqs) != 0 {
		t.Errorf("requests %+v, error %v; want none", reqs, err)
	}
}

func TestPlanRefuses(t *testing.T) {
	answers, judges := planPC3()
	for name, s := range map[string]Spec{
		"pair shape":       {Answers: answers, Judges: judges, Shape: ShapePair},
		"four labels":      {Answers: answers, Judges: judges, Labels: LabelsV1Four},
		"unknown shape":    {Answers: answers, Judges: judges, Shape: "triple"},
		"repeated ids":     {Answers: []Answer{answers[0], answers[1], answers[0]}, Judges: judges},
		"an answer, no id": {Answers: []Answer{answers[0], {Agent: "codex", Family: "openai", Status: AnswerReady}}, Judges: judges},
		// A missing answer with a ready one's id would be the one BuildTask
		// or Aggregate found.
		"a repeated id, one of them missing": {Answers: []Answer{answers[0], {ID: "codex", Agent: "codex", Status: AnswerMissing},
			planAnswer("codex", "gemini", ViaHandoff, "google", 2)}, Judges: judges},
		"a missing answer, no id": {Answers: []Answer{answers[0], answers[1], {Agent: "opencode", Status: AnswerMissing}}, Judges: judges},
	} {
		reqs, err := Plan(s)
		var je *Error
		if reqs != nil || !errors.As(err, &je) || je.Code != planCodeInternal {
			t.Errorf("%s: requests %+v, error %v", name, reqs, err)
		}
	}
	// The product's own values are fine.
	if _, err := Plan(Spec{Answers: answers, Judges: judges, Shape: ShapeDirected, Labels: LabelsV1}); err != nil {
		t.Errorf("directed v1: %v", err)
	}
}

func TestPlanJudgesOnce(t *testing.T) {
	// A judge listed twice, or without a name, counts once or not at all.
	answers, judges := planPC3()
	s := Spec{Answers: answers, Judges: []JudgeAI{{Family: "zhipu"}, judges[0], judges[1], {Agent: "Claude", Family: "anthropic"}, judges[2], judges[2]}}
	reqs, err := Plan(s)
	if err != nil {
		t.Fatal(err)
	}
	planCheck(t, s, reqs)
	if !reflect.DeepEqual(planLoads(reqs), map[string]int{"claude": 2, "codex": 2, "opencode": 2}) {
		t.Errorf("loads %v", planLoads(reqs))
	}
}

// An app not in the handoff table is of unknown company ("") and fails
// closed: no judge takes its answer, not even one of another company,
// since "ChatGPT app", "Claude Code" or an aggregator such as Copilot may
// run a judge's own models (11-01 user decision).
func TestHandoffFamily(t *testing.T) {
	for app, want := range map[string]string{
		"chatgpt":      "openai",
		"ChatGPT":      "openai",
		"claude":       "anthropic",
		" gemini ":     "google",
		"perplexity":   "",
		"DeepSeek":     "",
		"ChatGPT app":  "",
		"Claude Code":  "",
		"claude.ai":    "",
		"":             "",
		"   ":          "",
		"claude-code?": "",
	} {
		if got := HandoffFamily(app); got != want {
			t.Errorf("HandoffFamily(%q) = %q, want %q", app, got, want)
		}
	}
	answers, judges := planPC3()
	for _, app := range []string{"ChatGPT app", "Claude Code", "copilot"} {
		for _, family := range []string{"", "google"} {
			s := Spec{Answers: []Answer{answers[0], planAnswer("p9", app, ViaHandoff, family, 2)}, Judges: judges}
			reqs, err := Plan(s)
			planCheck(t, s, reqs)
			var je *Error
			if !errors.As(err, &je) || je.Code != core.ErrCodeNoJudge || len(reqs) != 2 || reqs[0].Judge != "" || reqs[1].Judge != "" {
				t.Errorf("%s (family %q): requests %+v, error %v; want no_judge", app, family, reqs, err)
			}
		}
	}
}

// TestPlanSpreadsEvenly checks Plan on random comparisons against every
// possible assignment of judges to pairs: no pair goes to a judge that
// may not take it, no pair that some judge may take is left out, and the
// loads, in requests, are as even as they can be (the fewest requests on
// the busiest judge, and then the smallest sum of squared loads). From
// seed 400 on some answers have no unit, so some pairs make one request.
func TestPlanSpreadsEvenly(t *testing.T) {
	agents := []string{"claude", "codex", "opencode", "other"}
	families := []string{"anthropic", "openai", "deepseek", "google", "zhipu"}
	codes := []string{"", "", "", core.ErrCodeAskTimeout, core.ErrCodeAskFailed}
	checked, mixed := 0, 0
	for seed := uint64(0); seed < 800; seed++ {
		rng := rand.New(rand.NewPCG(seed, 7))
		var s Spec
		for i := range 2 + rng.IntN(5) {
			via, agent := ViaPC, agents[rng.IntN(len(agents))]
			if rng.IntN(3) == 0 {
				via, agent = ViaHandoff, []string{"chatgpt", "gemini", "claude", "kimi"}[rng.IntN(4)]
			}
			units := 1 + rng.IntN(2)
			if seed >= 400 {
				units = rng.IntN(3)
			}
			s.Answers = append(s.Answers, planAnswer(fmt.Sprintf("x%d", i), agent, via, families[rng.IntN(len(families))], units))
		}
		for _, k := range rng.Perm(len(agents))[:1+rng.IntN(len(agents))] {
			s.Judges = append(s.Judges, JudgeAI{Agent: agents[k], Family: families[rng.IntN(len(families))], AskCode: codes[rng.IntN(len(codes))]})
		}

		reqs, err := Plan(s)
		if err != nil && !strings.Contains(err.Error(), core.ErrCodeNoJudge) {
			t.Fatalf("seed %d: %v", seed, err)
		}
		planCheck(t, s, reqs)
		n, want := len(s.Answers), 0
		for _, x := range s.Answers {
			if len(x.Units) > 0 {
				want += n - 1
			}
		}
		if len(reqs) != want {
			t.Fatalf("seed %d: %d requests, want %d", seed, len(reqs), want)
		}

		// Brute force: who may take each pair, by the rules written out
		// again here, ask_failed judges only when nobody else may; how many
		// requests each pair makes.
		type pair struct{ x, y string }
		var pairs []pair
		var may [][]string
		var weight []int
		for i := range s.Answers {
			for j := i + 1; j < n; j++ {
				x, y := s.Answers[i], s.Answers[j]
				w := min(len(x.Units), 1) + min(len(y.Units), 1)
				if w == 0 {
					continue
				}
				var good, failed []string
				for _, jd := range s.Judges {
					ok := planTestMay(jd, x) && planTestMay(jd, y)
					switch {
					case !ok:
					case jd.AskCode == core.ErrCodeAskFailed:
						failed = append(failed, jd.Agent)
					default:
						good = append(good, jd.Agent)
					}
				}
				if len(good) == 0 {
					good = failed
				}
				pairs = append(pairs, pair{x.ID, y.ID})
				may = append(may, good)
				weight = append(weight, w)
			}
		}
		got := map[pair]string{}
		for _, r := range reqs {
			got[pair{min(r.From, r.Against), max(r.From, r.Against)}] = r.Judge
		}
		size := 1
		for i, p := range pairs {
			if j := got[p]; (j == "") != (len(may[i]) == 0) || (j != "" && !slices.Contains(may[i], j)) {
				t.Fatalf("seed %d: pair %v went to %q; may: %v", seed, p, j, may[i])
			}
			size *= max(1, len(may[i]))
		}
		if slices.Contains(weight, 1) && slices.Contains(weight, 2) {
			mixed++
		}
		if size > 1<<16 {
			continue
		}
		checked++
		gotMax, gotSq := planSpread(func(yield func(string, int) bool) {
			for i, p := range pairs {
				if j := got[p]; j != "" && !yield(j, weight[i]) {
					return
				}
			}
		})
		bestMax, bestSq := 1<<30, 1<<30
		choice := make([]int, len(pairs))
		for {
			m, sq := planSpread(func(yield func(string, int) bool) {
				for i := range pairs {
					if len(may[i]) > 0 && !yield(may[i][choice[i]], weight[i]) {
						return
					}
				}
			})
			if m < bestMax || m == bestMax && sq < bestSq {
				bestMax, bestSq = m, sq
			}
			i := 0
			for ; i < len(pairs); i++ {
				if choice[i]+1 < len(may[i]) {
					choice[i]++
					break
				}
				choice[i] = 0
			}
			if i == len(pairs) {
				break
			}
		}
		if gotMax != bestMax || gotSq != bestSq {
			t.Errorf("seed %d: busiest judge %d requests, squares %d; best %d and %d (%v)", seed, gotMax, gotSq, bestMax, bestSq, got)
		}
	}
	t.Logf("%d comparisons checked against every assignment, %d with pairs of one request and of two", checked, mixed)
	if checked < 600 || mixed < 100 {
		t.Errorf("only %d comparisons checked against every assignment, %d with pairs of one request", checked, mixed)
	}
}

// planSpread is the largest load and the sum of squared loads of an
// assignment, given as the judge of each pair and its requests.
func planSpread(judges func(yield func(string, int) bool)) (most, squares int) {
	load := map[string]int{}
	for j, w := range judges {
		load[j] += w
	}
	for _, n := range load {
		most = max(most, n)
		squares += n * n
	}
	return most, squares
}
