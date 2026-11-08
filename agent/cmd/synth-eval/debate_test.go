//go:build syntheval

package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/judge"
)

// The debate (C7): the moderator's plan and checks, the debaters' stances,
// the answer after the debate, and a whole debate with a fake.

func debateAnswers() []AnswerRec {
	return []AnswerRec{
		{Agent: "claude", Status: "done", Text: "첫 배는 7시입니다."},
		{Agent: "codex", Status: "done", Text: "First ferry at 8 a.m."},
		{Agent: "opencode", Status: "done", Text: "첫 배는 8시입니다."},
	}
}

func TestParsePlan(t *testing.T) {
	names := []string{"Claude", "Codex", "OpenCode"}
	text := strings.Join([]string{
		"POINT | p1 | fact | yes | 첫 배 시간",
		"SIDE | p1 | Claude | 7시",
		"SIDE | p1 | codex, OpenCode | 8시",
		"GOAL | p1 | the checked time with a source",
		"TURN | p1 | Claude | 근거는?",
		"TURN | p1 | Codex | 7시라는데?",
		"TURN | p1 | Gemini | 모르는 AI",
		"POINT | p2 | taste | yes | 문구",
		"SIDE | p2 | Claude | A",
		"SIDE | p2 | Codex | B",
		"TURN | p2 | Codex | 어느 쪽?",
		"POINT | p3 | judgment | yes | 추천",
		"SIDE | p3 | Claude | X",
		"SIDE | p3 | Codex | Y",
		"TURN | p3 | Claude | 조건은?",
		"POINT | p4 | fact | yes | 넷째",
		"POINT | p9 | vibes | yes | 모르는 성격",
		"noise",
	}, "\n")
	ps, err := parsePlan(text, names)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 3 || ps[0].ID != "p1" || ps[1].ID != "p2" || ps[2].ID != "p3" {
		t.Fatalf("points = %+v", ps)
	}
	p1 := ps[0]
	if !p1.Debate || p1.Nature != "fact" || len(p1.Sides) != 2 || p1.Sides[1].AIs[0] != "Codex" || len(p1.Turns) != 2 || p1.Goal == "" {
		t.Errorf("p1 = %+v", p1)
	}
	// At most two points are debated: the third keeps no turns.
	if !ps[1].Debate || ps[2].Debate || ps[2].Turns != nil {
		t.Errorf("p2 %v, p3 %v %+v", ps[1].Debate, ps[2].Debate, ps[2].Turns)
	}
	if _, err := parsePlan("nothing", names); err == nil {
		t.Error("no point, no error")
	}
	// A debated point needs two sides and a turn.
	ps, _ = parsePlan("POINT | p1 | fact | yes | x\nSIDE | p1 | Claude | a\nTURN | p1 | Claude | ?", names)
	if ps[0].Debate {
		t.Errorf("one side debated: %+v", ps[0])
	}
}

func TestParseCheck(t *testing.T) {
	answers := debateAnswers()
	if next, r, l, err := parseCheck("DONE | agreed | 8시입니다.", answers, false); err != nil || next != nil || r != "agreed" || l != "8시입니다." {
		t.Errorf("done: %v %q %q %v", next, r, l, err)
	}
	if next, _, _, err := parseCheck("NEXT | opencode | 출처는?", answers, false); err != nil || next == nil || next.AI != "OpenCode" || next.Ask != "출처는?" {
		t.Errorf("next: %+v %v", next, err)
	}
	if _, _, _, err := parseCheck("NEXT | Codex | ?", answers, true); err == nil {
		t.Error("the last check took a NEXT")
	}
	if _, _, _, err := parseCheck("DONE | maybe | x", answers, false); err == nil {
		t.Error("an unknown outcome")
	}
}

func TestStanceOf(t *testing.T) {
	for in, want := range map[string][2]string{
		"8시가 맞습니다.\nSTANCE: keep":             {"8시가 맞습니다.", "keep"},
		"근거를 보니 7시입니다.\n\n**STANCE: change**": {"근거를 보니 7시입니다.", "change"},
		"그냥 답": {"그냥 답", ""},
	} {
		if text, s := stanceOf(in); text != want[0] || s != want[1] {
			t.Errorf("stanceOf(%q) = %q %q", in, text, s)
		}
	}
}

func TestSplitParts(t *testing.T) {
	ps := []DebatePoint{{ID: "p1", Debate: true}, {ID: "p2", Debate: true}}
	got := splitParts("서론 [p2] 둘째\nSTANCE: keep\n[p1] 첫째\nSTANCE: change", ps, []int{0, 1})
	if !strings.HasPrefix(got["p1"], "첫째") || !strings.HasPrefix(got["p2"], "둘째") {
		t.Errorf("parts = %q", got)
	}
	if got := splitParts("표시 없음", ps, []int{0, 1}); got["p1"] != "표시 없음" || got["p2"] != "표시 없음" {
		t.Errorf("no marks = %q", got)
	}
}

// The answer after a debate is synth-v5 with the points in the data block
// and rule 5 for debated points; rules 1-4 and 6-8 are v5's.
func TestAfterDebatePrompt(t *testing.T) {
	task := judge.SynthTask{Question: "첫 배는?", Answers: []judge.SynthAnswer{{AI: "Claude", Text: "7시"}, {AI: "Codex", Text: "8시"}}}
	const n = "0123456789abcdef"
	points := []DebatePoint{{ID: "p1", Topic: "시간", Nature: "fact", Debate: true, Sides: []DebateSide{{AIs: []string{"Claude"}, Says: "7"}},
		Turns: []DebateTurn{{AI: "Claude", Ask: "근거?", Text: "8시가 맞네요", Stance: "change"}}, Result: "agreed", Line: "8시"}}
	p := afterDebatePrompt(task, points, n)
	v5 := judge.SynthPrompt(task, n)
	if !strings.HasPrefix(p, "[Askorus synthesis debate-v1] ") || !strings.Contains(p, `POINT: {"debated":true,`) ||
		!strings.Contains(p, `"outcome":"agreed: 8시"`) || !strings.Contains(p, debateRule5) || strings.Contains(p, "5. For every place") {
		t.Fatalf("prompt:\n%s", p)
	}
	for _, rule := range []string{"\n1. ", "\n4. ", "\n6. ", "\n8. "} {
		i, j := strings.Index(v5, rule), strings.Index(p, rule)
		if i < 0 || j < 0 || strings.SplitN(v5[i:], "\n", 3)[1] != strings.SplitN(p[j:], "\n", 3)[1] {
			t.Errorf("rule %q differs", rule)
		}
	}
	if strings.Count(p, "</data-"+n+">") != 2 {
		t.Error("end markers")
	}
}

// fakeDebater answers the moderator's calls by their head and the
// debaters' asks with a stance; it counts both.
type fakeDebater struct {
	mu          sync.Mutex
	writes, ask int
	asked       []string
}

func (f *fakeDebater) Write(_ context.Context, r writeReq) (writeRes, error) {
	f.mu.Lock()
	f.writes++
	f.mu.Unlock()
	switch {
	case strings.HasPrefix(r.Prompt, "[Askorus debate plan v1]"):
		return writeRes{Agent: r.Agent, Output: synthJSON("POINT | p1 | fact | yes | 첫 배 시간\nSIDE | p1 | Claude | 7시\nSIDE | p1 | Codex, OpenCode | 8시\nGOAL | p1 | checked\nTURN | p1 | Claude | 근거는?\nTURN | p1 | Codex | 7시라는데?\nPOINT | p2 | taste | no | 말투\nSIDE | p2 | Claude | 존댓말\nSIDE | p2 | Codex | 영어")}, nil
	case strings.HasPrefix(r.Prompt, "[Askorus debate check v1]"):
		if strings.Contains(r.Prompt, "This is the last check") {
			return writeRes{Agent: r.Agent, Output: synthJSON("DONE | agreed | 첫 배는 8시입니다.")}, nil
		}
		return writeRes{Agent: r.Agent, Output: synthJSON("NEXT | OpenCode | 출처는?")}, nil
	case strings.HasPrefix(r.Prompt, "[Askorus synthesis debate-v1]"):
		return writeRes{Agent: r.Agent, Output: synthJSON("첫 배는 8시입니다.")}, nil
	}
	return writeRes{}, nil
}

func (f *fakeDebater) AskOne(_ context.Context, agent, _, prompt string) (string, int64, error) {
	f.mu.Lock()
	f.ask++
	f.asked = append(f.asked, agent+": "+prompt)
	f.mu.Unlock()
	if strings.Contains(prompt, "[p1]") {
		return "[p1] 8시가 맞습니다.\nSTANCE: keep", 10, nil
	}
	return "출처를 보니 8시입니다.\nSTANCE: change", 10, nil
}

func TestDebateOne(t *testing.T) {
	f := &fakeDebater{}
	r := Record{Question: Question{ID: "general-1", Text: "첫 배는?"}, Answers: debateAnswers(), Synth: &SynthRec{Status: "done", Text: "AI마다 다릅니다."}}
	recs := debateOne(context.Background(), f, r, "claude", "standard")
	m2, m1 := recs["m2"], recs["m1"]
	if m2.Synth.Status != "done" || m2.Synth.Text != "첫 배는 8시입니다." || m2.Debate.Method != "m2" || m2.Debate.Error != "" {
		t.Fatalf("m2 = %+v %+v", m2.Synth, m2.Debate)
	}
	p := m2.Debate.Points[0]
	// Two planned turns, the check's one more, then the last check closes it.
	if len(p.Turns) != 3 || p.Turns[2].AI != "OpenCode" || p.Turns[0].Stance != "change" || p.Result != "agreed" || p.Line != "첫 배는 8시입니다." {
		t.Errorf("m2 point = %+v", p)
	}
	if m2.Debate.Points[1].Debate || m2.Debate.Points[1].Turns != nil {
		t.Errorf("an undebated point got turns: %+v", m2.Debate.Points[1])
	}
	// m1: each AI once, every point at once.
	q := m1.Debate.Points[0]
	if m1.Synth.Text != "첫 배는 8시입니다." || len(q.Turns) != 3 || q.Turns[0].Text != "8시가 맞습니다." || q.Turns[0].Stance != "keep" || q.Result != "" {
		t.Errorf("m1 point = %+v", q)
	}
	// The second turn of m2 sees the first.
	var second string
	for _, a := range f.asked {
		if strings.HasPrefix(a, "codex: ") && strings.Contains(a, "THE MODERATOR ASKS YOU: 7시라는데?") {
			second = a
		}
	}
	if !strings.Contains(second, "[Claude] 출처를 보니 8시입니다.") {
		t.Errorf("the second turn did not see the first:\n%s", second)
	}
	// Moderator: plan 1, checks 2 and finals 2; debaters: m2 3, m1 3.
	if f.writes != 5 || f.ask != 6 {
		t.Errorf("writes %d, asks %d", f.writes, f.ask)
	}
}

// The command needs -expect and refuses an existing run.
func TestDebateCmdFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runMain(context.Background(), []string{"debate", "-from", "x", "-run", "y", "-tier", "ultra"}, &out, &errOut); code != exitFlags {
		t.Errorf("bad tier: %d", code)
	}
	for _, ways := range []string{"m3", "m2,m2", ""} {
		errOut.Reset()
		if code := runMain(context.Background(), []string{"debate", "-from", "x", "-run", "y", "-ways", ways}, &out, &errOut); code != exitFlags || !strings.Contains(errOut.String(), "-ways") {
			t.Errorf("-ways %q: %d %s", ways, code, errOut.String())
		}
	}
}

// tasteDebater plans one taste point; it keeps every prompt.
type tasteDebater struct {
	mu            sync.Mutex
	writes, asked []string
}

func (f *tasteDebater) Write(_ context.Context, r writeReq) (writeRes, error) {
	f.mu.Lock()
	f.writes = append(f.writes, r.Prompt)
	f.mu.Unlock()
	switch {
	case strings.HasPrefix(r.Prompt, "[Askorus debate plan v1]"):
		return writeRes{Agent: r.Agent, Output: synthJSON("POINT | p1 | taste | yes | 이름\nSIDE | p1 | Claude | Panelist\nSIDE | p1 | Codex | Askorus\nGOAL | p1 | the strongest options\nTURN | p1 | Claude | Askorus는?\nTURN | p1 | Codex | Panelist는?")}, nil
	case strings.HasPrefix(r.Prompt, "[Askorus debate check v1]"):
		if strings.Contains(r.Prompt, "This is the last check") {
			return writeRes{Agent: r.Agent, Output: synthJSON("DONE | kept | Moderai, Panelist, Askorus")}, nil
		}
		return writeRes{Agent: r.Agent, Output: synthJSON("NEXT | Claude | 둘을 합쳐 주세요")}, nil
	case strings.HasPrefix(r.Prompt, "[Askorus synthesis debate-v"):
		return writeRes{Agent: r.Agent, Output: synthJSON(r.Prompt[len("[Askorus synthesis "):len("[Askorus synthesis debate-v1")])}, nil
	}
	return writeRes{}, nil
}

func (f *tasteDebater) AskOne(_ context.Context, agent, _, prompt string) (string, int64, error) {
	f.mu.Lock()
	f.asked = append(f.asked, prompt)
	f.mu.Unlock()
	return "새 안: Moderai\nSTANCE: partly", 10, nil
}

// r2 (C8) refines a taste point together: the debaters build on each
// other's options, the check asks for a combined option and writes 다르다,
// and the answer after the debate is debate-v2; m2 on the same plan stays
// C7's.
func TestDebateRefine(t *testing.T) {
	f := &tasteDebater{}
	r := Record{Question: Question{ID: "general-8", Text: "앱 이름은?"}, Answers: debateAnswers(), Synth: &SynthRec{Status: "done", Text: "후보"}}
	recs := debateOne(context.Background(), f, r, "claude", "standard", "m2", "r2")
	if len(recs) != 2 || recs["m1"].Debate != nil {
		t.Fatalf("ways: %v", recs)
	}
	m2, r2 := recs["m2"], recs["r2"]
	if m2.Synth.Text != "debate-v1" || r2.Synth.Text != "debate-v2" || r2.Debate.Method != "r2" || r2.Debate.Points[0].Result != "kept" {
		t.Fatalf("m2 %q, r2 %q %+v", m2.Synth.Text, r2.Synth.Text, r2.Debate)
	}
	refined, plain := 0, 0
	for _, p := range f.asked {
		if strings.Contains(p, debateRefineRule) {
			refined++
		} else if strings.Contains(p, debateTurnRule) {
			plain++
		}
	}
	// Each way: two planned turns and the check's one more.
	if refined != 3 || plain != 3 {
		t.Errorf("turns: %d refined, %d plain", refined, plain)
	}
	checks := 0
	for _, p := range f.writes {
		if strings.HasPrefix(p, "[Askorus debate check v1]") && strings.Contains(p, "never 갈리다") {
			checks++
			if !strings.Contains(p, "This is the last check") && !strings.Contains(p, "combine the strongest options") {
				t.Errorf("an r2 check without the refine ask:\n%s", p)
			}
		}
	}
	if checks != 2 {
		t.Errorf("r2 checks = %d", checks)
	}
	task := judge.SynthTask{Question: "q", Answers: []judge.SynthAnswer{{AI: "Claude", Text: "a"}, {AI: "Codex", Text: "b"}}}
	if p := afterDebatePromptOf(task, r2.Debate.Points, "0123456789abcdef", true); !strings.HasPrefix(p, "[Askorus synthesis debate-v2] ") || !strings.Contains(p, debateRefineRule5) || strings.Contains(p, debateRule5) {
		t.Errorf("debate-v2:\n%s", p)
	}
}

// NONE is a plan without points: nothing is debated and the combined
// answer stays as it was.
func TestDebateNone(t *testing.T) {
	if ps, err := parsePlan("NONE", []string{"Claude"}); err != nil || len(ps) != 0 {
		t.Fatalf("NONE: %v %v", ps, err)
	}
	f := &noneDebater{}
	r := Record{Question: Question{ID: "general-1", Text: "q"}, Answers: debateAnswers(), Synth: &SynthRec{Status: "done", Text: "그대로"}}
	recs := debateOne(context.Background(), f, r, "claude", "standard")
	if m2 := recs["m2"]; m2.Synth.Text != "그대로" || len(m2.Debate.Points) != 0 || m2.Debate.Error != "" || f.writes != 1 {
		t.Errorf("m2 = %+v %+v, writes %d", m2.Synth, m2.Debate, f.writes)
	}
}

type noneDebater struct{ writes int }

func (f *noneDebater) Write(_ context.Context, r writeReq) (writeRes, error) {
	f.writes++
	return writeRes{Agent: r.Agent, Output: synthJSON("NONE")}, nil
}

func (f *noneDebater) AskOne(context.Context, string, string, string) (string, int64, error) {
	return "", 0, nil
}

// A combined answer written after a debate brings the debate to the judge
// as a source; without a debate the prompt is score-v0 as it was.
func TestScorePromptGivesTheDebate(t *testing.T) {
	a, b := taskRecord("g-1", 1), taskRecord("g-1", 1)
	a.Synth.Text, b.Synth.Text = "A-TEXT", "B-TEXT"
	plain := scorePrompt(a, b)
	if strings.Contains(plain, "DEBATE") || !strings.Contains(plain, "no claim that no answer makes") {
		t.Fatalf("prompt without a debate:\n%s", plain)
	}
	b.Debate = &DebateRec{Method: "m2", Points: []DebatePoint{{ID: "p1", Topic: "가격", Debate: true, Turns: []DebateTurn{
		{AI: "Claude", Text: "3원이 맞습니다. 출처: 가격표"},
		{AI: "Codex", Error: "ask_timeout"},
	}}}}
	p := scorePrompt(a, b)
	for _, want := range []string{
		"the AIs may also have debated",
		"DEBATE BEFORE COMBINED 2 BY Claude: \"Point: 가격\\n3원이 맞습니다. 출처: 가격표\"\nCOMBINED 1: \"A-TEXT\"",
		"no claim that no answer or debate line makes",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("missing %q in:\n%s", want, p)
		}
	}
	if strings.Contains(p, "BY Codex") {
		t.Errorf("a failed turn was given:\n%s", p)
	}
	if p = scorePrompt(b, a); !strings.Contains(p, "DEBATE BEFORE COMBINED 1 BY Claude") {
		t.Fatalf("swapped:\n%s", p)
	}
}
