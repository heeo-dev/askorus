package compares

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// The debate (P2-5, #26): the moderator's plan, the turns and checks, the
// answer after the debate, stop, limits and what the AIs get.

const (
	debateQuestion = "첫 배는 몇 시에 떠?"
	debateCombined = "첫 배 시간은 AI마다 다릅니다: {{Claude}} 7시, {{Codex}}와 {{OpenCode}} 8시."
	debateAfter    = "첫 배는 8시입니다."
	planHead       = "[Asksemble debate-plan v1]"
	checkHead      = "[Asksemble debate-check v1]"
	afterHead      = "[Asksemble synthesis debate-v4]"
	turnHead       = "[Asksemble debate-turn v1]"
)

// twoPointPlan debates p1 (a fact: Claude, then Codex) and p2 (a taste
// point: OpenCode), and leaves p3 undebated.
const twoPointPlan = `POINT | p1 | fact | yes | 첫 배 시간
SIDE | p1 | Claude | 7시
SIDE | p1 | Codex, OpenCode | 8시
GOAL | p1 | the time checked with a source
TURN | p1 | Claude | 7시라는 근거는?
TURN | p1 | Codex | 8시라는 출처는?
POINT | p2 | taste | yes | 안내 문구
SIDE | p2 | Claude | A안
SIDE | p2 | OpenCode | B안
TURN | p2 | OpenCode | 두 안을 합쳐 주세요
POINT | p3 | judgment | no | 표 사는 곳
SIDE | p3 | Claude | 터미널
SIDE | p3 | Codex | 앱`

func debateSpec(owner, key string) DebateSpec {
	return DebateSpec{Owner: owner, Key: key, Question: debateQuestion, Combined: debateCombined, Answers: []DebateAnswer{
		{Agent: "claude", Via: judge.ViaPC, Model: "claude-m", Text: "첫 배는 7시입니다."},
		{Agent: "codex", Via: judge.ViaPC, Model: "codex-m", Text: "First ferry at 8 a.m."},
		{Agent: "opencode", Via: judge.ViaPC, Model: "oc-m", Text: "첫 배는 8시입니다."},
	}}
}

// moderatorScript answers the moderator's calls by their prompt: plan
// first, then checks in order (DONE | agreed when they run out), then
// debateAfter. Each reply is the synth JSON unless it starts with "!",
// which gives the rest as prose.
type moderatorScript struct {
	mu     sync.Mutex
	plan   []string // one per plan call; the last repeats
	checks []string
	n      int
	calls  []string // the prompts' heads in order
	asked  []string // every prompt
}

func (s *moderatorScript) synth(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, req.Prompt)
	reply := ""
	switch {
	case strings.HasPrefix(req.Prompt, planHead):
		s.calls = append(s.calls, "plan")
		reply = s.plan[min(countOf(s.calls, "plan"), len(s.plan))-1]
	case strings.HasPrefix(req.Prompt, checkHead):
		s.calls = append(s.calls, "check")
		reply = "DONE | agreed | 8시입니다."
		if s.n < len(s.checks) {
			reply = s.checks[s.n]
		}
		s.n++
	case strings.HasPrefix(req.Prompt, afterHead):
		s.calls = append(s.calls, "after")
		reply = debateAfter
	default:
		s.calls = append(s.calls, "other")
	}
	if p, ok := strings.CutPrefix(reply, "!"); ok {
		return core.SynthResult{Output: json.RawMessage(p), Model: req.Model}, nil
	}
	b, _ := json.Marshal(map[string]string{"answer": reply})
	return core.SynthResult{Output: b, Model: req.Model}, nil
}

func countOf(xs []string, x string) int {
	n := 0
	for _, v := range xs {
		if v == x {
			n++
		}
	}
	return n
}

func (s *moderatorScript) heads() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.calls)
}

func (s *moderatorScript) prompts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.asked)
}

// speaker answers turns with "<name> turn <n>" and stance change.
func speaker(name string) func(ctx context.Context, req core.AskRequest, n int) (core.AskResult, error) {
	return func(ctx context.Context, req core.AskRequest, n int) (core.AskResult, error) {
		return core.AskResult{Answer: name + " turn " + string(rune('0'+n)) + "\n\nSTANCE: change", Model: name + "-standard"}, nil
	}
}

// debateRig is three AIs; claude moderates with script.
func debateRig(t *testing.T, script *moderatorScript, opt func(*Options)) (*Manager, *syncBuffer, *writerAI, *writerAI, *writerAI, []core.Adapter) {
	t.Helper()
	cl, cx, oc, ais := threeWriters()
	cl.setSynth(script.synth)
	cl.ask, cx.ask, oc.ask = speaker("Claude"), speaker("Codex"), speaker("OpenCode")
	m, buf := newTestManager(t, ais, opt)
	return m, buf, cl, cx, oc, ais
}

func startDebate(t *testing.T, m *Manager, s DebateSpec) Debate {
	t.Helper()
	d, err := m.StartDebate(s)
	if err != nil {
		t.Fatalf("StartDebate: %v", err)
	}
	return d
}

func waitDebate(t *testing.T, m *Manager, owner, id string) Debate {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d, ok := m.WaitDebate(ctx, owner, id)
	if !ok {
		t.Fatalf("no debate %s", id)
	}
	if ctx.Err() != nil {
		t.Fatalf("the debate did not end: %+v", d)
	}
	return d
}

func asksOf(w *writerAI) []core.AskRequest {
	w.smu.Lock()
	defer w.smu.Unlock()
	return slices.Clone(w.asks)
}

func TestDebateRuns(t *testing.T) {
	script := &moderatorScript{plan: []string{twoPointPlan}, checks: []string{
		"NEXT | opencode | 운항표 출처를 확인해 주세요", // p1: one more turn
		"DONE | agreed | 첫 배는 8시입니다.",
		"DONE | kept | B안이 낫습니다.", // p2
	}}
	m, buf, cl, cx, oc, _ := debateRig(t, script, nil)
	spec := debateSpec("phone", "k1")
	spec.Earlier = []string{"EARLIER-QUESTION"}
	spec.Judges = []string{"codex"} // the moderator (claude) is not listed: no earlier questions
	d := startDebate(t, m, spec)
	if d.Status != DebatePlanning || d.Tier != core.TierStandard || len(d.Points) != 0 {
		t.Fatalf("at start: %+v", d)
	}
	got := waitDebate(t, m, "phone", d.ID)
	if got.Status != DebateDone || got.Error != nil || got.Moderator != "claude" || got.Model != "claude-standard" {
		t.Fatalf("debate: %+v", got)
	}
	if got.After == nil || got.After.Text != debateAfter || got.After.Agent != "claude" {
		t.Errorf("after: %+v", got.After)
	}
	if len(got.Points) != 3 {
		t.Fatalf("points: %+v", got.Points)
	}
	p1, p2, p3 := got.Points[0], got.Points[1], got.Points[2]
	if !p1.Debate || p1.Nature != judge.NatureFact || p1.Result != judge.OutcomeAgreed || p1.Line != "첫 배는 8시입니다." ||
		len(p1.Sides) != 2 || !slices.Equal(p1.Sides[1].AIs, []string{"codex", "opencode"}) {
		t.Errorf("p1: %+v", p1)
	}
	var who []string
	for _, tn := range p1.Turns {
		who = append(who, tn.AI)
		if tn.Status != TurnDone || tn.Stance != judge.StanceChange || !strings.Contains(tn.Text, " turn ") || strings.Contains(tn.Text, "STANCE") || tn.Model == "" {
			t.Errorf("turn: %+v", tn)
		}
	}
	if !slices.Equal(who, []string{"claude", "codex", "opencode"}) || p1.Turns[2].Ask != "운항표 출처를 확인해 주세요" {
		t.Errorf("p1 turns: %+v", p1.Turns)
	}
	if !p2.Debate || p2.Result != judge.OutcomeKept || len(p2.Turns) != 1 || p2.Turns[0].AI != "opencode" {
		t.Errorf("p2: %+v", p2)
	}
	if p3.Debate || len(p3.Turns) != 0 || p3.Result != "" || p3.Turns == nil {
		t.Errorf("p3: %+v", p3)
	}
	// plan, p1: 2 turns, check (next), turn, check (done); p2: turn, check; after.
	if h := script.heads(); !slices.Equal(h, []string{"plan", "check", "check", "check", "after"}) || got.Calls != 9 {
		t.Errorf("moderator calls %v, calls %d", h, got.Calls)
	}
	// The debaters: the ask command in the judge folder at the tier, the
	// debate prompt inside the ask preamble, the others' words as data.
	clAsks, cxAsks, ocAsks := asksOf(cl), asksOf(cx), asksOf(oc)
	if len(clAsks) != 1 || len(cxAsks) != 1 || len(ocAsks) != 2 {
		t.Fatalf("asks: %d %d %d", len(clAsks), len(cxAsks), len(ocAsks))
	}
	for _, r := range [][]core.AskRequest{clAsks, cxAsks, ocAsks} {
		for _, a := range r {
			if a.Folder != m.o.Gate.Folder || a.Tier != core.TierStandard || a.Resume != "" ||
				!strings.HasPrefix(a.Prompt, core.AskPreamble+"\n\n"+turnHead) {
				t.Errorf("ask: folder %q tier %q prompt %.80q", a.Folder, a.Tier, a.Prompt)
			}
		}
	}
	third := ocAsks[0].Prompt
	data := strings.Index(third, "<data-")
	if !strings.Contains(third, "You are OpenCode") || data < 0 || strings.Index(third, "Claude turn 1") < data || strings.Index(third, "Codex turn 1") < data {
		t.Errorf("the third turn's prompt: %s", third)
	}
	// p2's turn sees none of p1's turns.
	if strings.Contains(ocAsks[1].Prompt, "Claude turn 1") || !strings.Contains(ocAsks[1].Prompt, "taste point") {
		t.Errorf("p2's turn: %s", ocAsks[1].Prompt)
	}
	ps := script.prompts()
	if strings.Contains(ps[0], "EARLIER-QUESTION") || !strings.Contains(ps[0], "COMBINED: ") {
		t.Errorf("the plan prompt: %s", ps[0])
	}
	last := ps[len(ps)-1]
	if !strings.HasPrefix(last, afterHead) || !strings.Contains(last, `POINT: {"debated":true`) || !strings.Contains(last, "OpenCode turn 1") {
		t.Errorf("the after prompt: %s", last)
	}
	// Logs: codes and counts, never a text.
	logs := buf.String()
	for _, secret := range []string{debateQuestion, "7시", "turn 1", debateAfter, "운항표"} {
		if strings.Contains(logs, secret) {
			t.Errorf("logs carry %q:\n%s", secret, logs)
		}
	}
	if !strings.Contains(logs, `"msg":"debate ended"`) || !strings.Contains(logs, `"debated":2,"turns":4,"calls":9`) {
		t.Errorf("logs:\n%s", logs)
	}
	// The same key gives the same debate.
	if again, err := m.StartDebate(spec); err != nil || again.ID != d.ID {
		t.Errorf("again: %v %v", again.ID, err)
	}
}

func TestDebateEarlierToAListedModerator(t *testing.T) {
	script := &moderatorScript{plan: []string{"NONE"}}
	m, _, _, _, _, _ := debateRig(t, script, nil)
	spec := debateSpec("phone", "")
	spec.Earlier, spec.Judges = []string{"EARLIER-QUESTION"}, []string{"claude", "codex"}
	waitDebate(t, m, "phone", startDebate(t, m, spec).ID)
	if ps := script.prompts(); len(ps) != 1 {
		t.Fatalf("prompts %d", len(ps))
	}
}

func TestDebateNothingToDebate(t *testing.T) {
	for _, plan := range []string{"NONE", "POINT | p1 | taste | no | 말투\nSIDE | p1 | Claude | a\nSIDE | p1 | Codex | b"} {
		script := &moderatorScript{plan: []string{plan}}
		m, _, cl, _, _, _ := debateRig(t, script, nil)
		got := waitDebate(t, m, "phone", startDebate(t, m, debateSpec("phone", "")).ID)
		if got.Status != DebateDone || got.After != nil || got.Error != nil || got.Calls != 1 || len(asksOf(cl)) != 0 {
			t.Errorf("%q: %+v", plan, got)
		}
		if plan == "NONE" && (got.Points == nil || len(got.Points) != 0) {
			t.Errorf("NONE: points %v", got.Points)
		}
	}
}

func TestDebateModeratorRetriesOnce(t *testing.T) {
	// A bad plan, then a good one: the debate goes on.
	script := &moderatorScript{plan: []string{"!not json", twoPointPlan}}
	m, _, _, _, _, _ := debateRig(t, script, nil)
	got := waitDebate(t, m, "phone", startDebate(t, m, debateSpec("phone", "")).ID)
	if got.Status != DebateDone || countOf(script.heads(), "plan") != 2 {
		t.Errorf("one bad plan: %s %v", got.Status, script.heads())
	}
	// Two bad plans: failed with judge_bad_output.
	script = &moderatorScript{plan: []string{"no point here"}}
	m, _, _, _, _, _ = debateRig(t, script, nil)
	got = waitDebate(t, m, "phone", startDebate(t, m, debateSpec("phone", "")).ID)
	if got.Status != DebateFailed || got.Error == nil || got.Error.Code != core.ErrCodeJudgeBadOutput || got.Calls != 2 || got.After != nil {
		t.Errorf("two bad plans: %+v", got)
	}
	// A check that never reads leaves its point open; the debate goes on.
	script = &moderatorScript{plan: []string{twoPointPlan}, checks: []string{"what?", "still nothing", "DONE | kept | B안"}}
	m, _, _, _, _, _ = debateRig(t, script, nil)
	got = waitDebate(t, m, "phone", startDebate(t, m, debateSpec("phone", "")).ID)
	if got.Status != DebateDone || got.Points[0].Result != "" || got.Points[1].Result != judge.OutcomeKept || got.After == nil {
		t.Errorf("a bad check: %+v", got)
	}
}

func TestDebateTurnFailsAndGoesOn(t *testing.T) {
	script := &moderatorScript{plan: []string{twoPointPlan}}
	m, _, cl, cx, _, _ := debateRig(t, script, func(o *Options) { o.TurnLimit = 50 * time.Millisecond })
	cl.ask = func(ctx context.Context, req core.AskRequest, n int) (core.AskResult, error) {
		<-ctx.Done() // past the turn's limit
		return core.AskResult{}, ctx.Err()
	}
	cx.ask = func(ctx context.Context, req core.AskRequest, n int) (core.AskResult, error) {
		return core.AskResult{}, core.Fail(core.ErrCodeAskFailed, "PRIVATE words of the AI")
	}
	got := waitDebate(t, m, "phone", startDebate(t, m, debateSpec("phone", "")).ID)
	if got.Status != DebateDone || got.After == nil {
		t.Fatalf("debate: %+v", got)
	}
	t1, t2 := got.Points[0].Turns[0], got.Points[0].Turns[1]
	if t1.Status != TurnFailed || t1.Error.Code != core.ErrCodeAskTimeout || t1.Text != "" {
		t.Errorf("turn 1: %+v", t1)
	}
	if t2.Status != TurnFailed || t2.Error.Code != core.ErrCodeAskFailed || strings.Contains(t2.Error.Message, "PRIVATE") {
		t.Errorf("turn 2: %+v", t2)
	}
	// A failed turn is no part of what the moderator reads.
	for _, p := range script.prompts() {
		if strings.HasPrefix(p, checkHead) && strings.Contains(p, "MODERATOR TO Claude") {
			t.Errorf("a failed turn went to the check: %s", p)
		}
	}
}

func TestDebateStop(t *testing.T) {
	script := &moderatorScript{plan: []string{twoPointPlan}}
	m, _, cl, cx, _, _ := debateRig(t, script, nil)
	entered := make(chan struct{})
	var killed bool
	var mu sync.Mutex
	cl.ask = func(ctx context.Context, req core.AskRequest, n int) (core.AskResult, error) {
		close(entered)
		<-ctx.Done()
		mu.Lock()
		killed = true
		mu.Unlock()
		return core.AskResult{}, ctx.Err()
	}
	d := startDebate(t, m, debateSpec("phone", ""))
	<-entered
	if _, err := m.StopDebate("tablet", d.ID); !errors.Is(err, ErrDebateNotFound) {
		t.Errorf("another device stopped it: %v", err)
	}
	stopped, err := m.StopDebate("phone", d.ID)
	if err != nil || stopped.Status != DebateStopped || stopped.Error != nil {
		t.Fatalf("stop: %+v %v", stopped, err)
	}
	if tn := stopped.Points[0].Turns[0]; tn.Status != TurnFailed || tn.Error.Code != core.ErrCodeCancelled {
		t.Errorf("the running turn: %+v", tn)
	}
	got := waitDebate(t, m, "phone", d.ID)
	mu.Lock()
	k := killed
	mu.Unlock()
	if got.Status != DebateStopped || got.After != nil || !k || len(asksOf(cx)) != 0 || countOf(script.heads(), "after") != 0 {
		t.Errorf("after stop: %+v killed %v", got, k)
	}
	// Stopping again changes nothing.
	if again, _ := m.StopDebate("phone", d.ID); again.Status != DebateStopped {
		t.Errorf("again: %s", again.Status)
	}
}

func TestDebateOneAtATime(t *testing.T) {
	script := &moderatorScript{plan: []string{twoPointPlan}}
	m, _, cl, _, _, _ := debateRig(t, script, nil)
	release := make(chan struct{})
	cl.ask = func(ctx context.Context, req core.AskRequest, n int) (core.AskResult, error) {
		<-release
		return core.AskResult{Answer: "ok\nSTANCE: keep"}, nil
	}
	d := startDebate(t, m, debateSpec("phone", "a"))
	if _, err := m.StartDebate(debateSpec("tablet", "b")); !errors.Is(err, ErrDebateBusy) {
		t.Errorf("a second debate: %v", err)
	}
	if again, err := m.StartDebate(debateSpec("phone", "a")); err != nil || again.ID != d.ID {
		t.Errorf("the same key: %v %v", again.ID, err)
	}
	if _, ok := m.GetDebate("tablet", d.ID); ok {
		t.Error("another device read it")
	}
	close(release)
	waitDebate(t, m, "phone", d.ID)
	if _, err := m.StartDebate(debateSpec("tablet", "b")); err != nil {
		t.Errorf("after it ended: %v", err)
	}
}

func TestDebateSpecRefused(t *testing.T) {
	m, _, _, _, _, _ := debateRig(t, &moderatorScript{plan: []string{"NONE"}}, nil)
	cases := map[string]func(*DebateSpec){
		"need two":         func(s *DebateSpec) { s.Answers = s.Answers[:1] },
		"unknown pc":       func(s *DebateSpec) { s.Answers[0].Agent = "gemini-cli" },
		"unknown app":      func(s *DebateSpec) { s.Answers[0] = DebateAnswer{Agent: "copilot", Via: judge.ViaHandoff, Text: "x"} },
		"one AI twice":     func(s *DebateSpec) { s.Answers[2] = DebateAnswer{Agent: "claude", Via: judge.ViaHandoff, Text: "x"} },
		"no combined":      func(s *DebateSpec) { s.Combined = "  " },
		"big combined":     func(s *DebateSpec) { s.Combined = strings.Repeat("가", MaxCombinedBytes) },
		"no question":      func(s *DebateSpec) { s.Question = "" },
		"bad moderator":    func(s *DebateSpec) { s.Moderator = "gemini-cli" },
		"bad tier":         func(s *DebateSpec) { s.Tier = "max" },
		"empty answer":     func(s *DebateSpec) { s.Answers[1].Text = " " },
		"too many earlier": func(s *DebateSpec) { s.Earlier = []string{"a", "b", "c", "d"} },
	}
	for name, f := range cases {
		s := debateSpec("phone", "")
		f(&s)
		_, err := m.StartDebate(s)
		var se *SpecError
		if !errors.As(err, &se) {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if name == "need two" && se.Code != core.ErrCodeNeedTwoAnswers || name == "big combined" && !se.TooLarge {
			t.Errorf("%s: %+v", name, se)
		}
	}
}

func TestDebateHandoffAnswerHasASideOnly(t *testing.T) {
	plan := "POINT | p1 | fact | yes | 시간\nSIDE | p1 | Claude | 7시\nSIDE | p1 | ChatGPT | 8시\nTURN | p1 | ChatGPT | 근거는?\nTURN | p1 | Claude | 근거는?"
	script := &moderatorScript{plan: []string{plan}}
	m, _, cl, _, _, _ := debateRig(t, script, nil)
	spec := debateSpec("phone", "")
	spec.Answers = []DebateAnswer{spec.Answers[0], {Agent: "chatgpt", Via: judge.ViaHandoff, Text: "8 a.m."}}
	spec.Moderator = "codex" // did not answer: claude moderates
	got := waitDebate(t, m, "phone", startDebate(t, m, spec).ID)
	if got.Status != DebateDone || got.Moderator != "claude" {
		t.Fatalf("debate: %+v", got)
	}
	p := got.Points[0]
	if len(p.Turns) != 1 || p.Turns[0].AI != "claude" || !slices.Equal(p.Sides[1].AIs, []string{"chatgpt"}) || len(asksOf(cl)) != 1 {
		t.Errorf("point: %+v", p)
	}
}

func TestDebateModerator(t *testing.T) {
	// No AI of the answers may moderate.
	script := &moderatorScript{plan: []string{"NONE"}}
	m, _, cl, cx, oc, _ := debateRig(t, script, nil)
	for _, w := range []*writerAI{cl, cx, oc} {
		w.setAvailable(false)
	}
	got := waitDebate(t, m, "phone", startDebate(t, m, debateSpec("phone", "")).ID)
	if got.Status != DebateFailed || got.Error.Code != core.ErrCodeNoJudge || got.Calls != 0 {
		t.Errorf("no moderator: %+v", got)
	}
	// The person's pick moderates; a model of another company is refused.
	script = &moderatorScript{plan: []string{"NONE"}}
	m, _, _, cx, _, _ = debateRig(t, script, nil)
	cx.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		res, err := script.synth(ctx, req, n)
		res.Model = "claude-sonnet"
		return res, err
	})
	cx.fams = map[string]string{"claude-sonnet": "anthropic"}
	spec := debateSpec("phone", "")
	spec.Moderator, spec.Tier = "codex", core.TierDeep
	got = waitDebate(t, m, "phone", startDebate(t, m, spec).ID)
	if got.Status != DebateFailed || got.Moderator != "codex" || got.Error.Code != core.ErrCodeBadModel || got.Tier != core.TierDeep {
		t.Errorf("another company: %+v", got)
	}
	// 11-06: the moderator model set for the moderator writes the plan; the
	// debaters keep the tier's.
	script = &moderatorScript{plan: []string{"NONE"}}
	m, _, cl, _, _, _ = debateRig(t, script, nil)
	spec = debateSpec("phone", "")
	spec.Moderator, spec.Tier, spec.ModeratorModel, spec.ModeratorEffort = "claude", core.TierQuick, "claude-deep", ""
	got = waitDebate(t, m, "phone", startDebate(t, m, spec).ID)
	if calls := cl.synthCalls(); got.Moderator != "claude" || got.Model != "claude-deep" || len(calls) == 0 || calls[0].Model != "claude-deep" {
		t.Errorf("moderator model: %+v %+v", got, calls)
	}
}

func TestDebateCloseAndLimit(t *testing.T) {
	script := &moderatorScript{plan: []string{twoPointPlan}}
	m, _, cl, _, _, _ := debateRig(t, script, nil)
	entered := make(chan struct{})
	cl.ask = func(ctx context.Context, req core.AskRequest, n int) (core.AskResult, error) {
		close(entered)
		<-ctx.Done()
		return core.AskResult{}, ctx.Err()
	}
	d := startDebate(t, m, debateSpec("phone", ""))
	<-entered
	m.Close()
	got, _ := m.GetDebate("phone", d.ID)
	if got.Status != DebateFailed || got.Error.Code != core.ErrCodeCancelled || got.Error.Message != msgDebateClosed {
		t.Errorf("after Close: %+v", got)
	}
	// Over the debate's limit.
	script = &moderatorScript{plan: []string{twoPointPlan}}
	m, _, cl, _, _, _ = debateRig(t, script, func(o *Options) { o.DebateLimit = 100 * time.Millisecond })
	cl.ask = func(ctx context.Context, req core.AskRequest, n int) (core.AskResult, error) {
		<-ctx.Done()
		return core.AskResult{}, ctx.Err()
	}
	got = waitDebate(t, m, "phone", startDebate(t, m, debateSpec("phone", "")).ID)
	if got.Status != DebateFailed || got.Error.Code != core.ErrCodeJudgeTimeout || got.Error.Message != msgDebateLimit {
		t.Errorf("over the limit: %+v", got)
	}
}

func TestDebateCleanupAndExpiry(t *testing.T) {
	script := &moderatorScript{plan: []string{twoPointPlan}}
	now := time.Now()
	var nmu sync.Mutex
	m, _, _, _, oc, _ := debateRig(t, script, func(o *Options) {
		o.Now = func() time.Time { nmu.Lock(); defer nmu.Unlock(); return now }
	})
	oc.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		res, err := script.synth(ctx, req, n)
		res.Cleanup = "oc-" + string(rune('0'+n))
		return res, err
	})
	spec := debateSpec("phone", "")
	spec.Moderator = "opencode"
	d := startDebate(t, m, spec)
	got := waitDebate(t, m, "phone", d.ID)
	if got.Status != DebateDone || got.Moderator != "opencode" {
		t.Fatalf("debate: %+v", got)
	}
	calls := (cleanerAI{oc.fakeAI}).cleanupCalls()
	if len(calls) != 1 || len(calls[0]) != len(script.heads()) {
		t.Errorf("cleanup %v for %d calls", calls, len(script.heads()))
	}
	nmu.Lock()
	now = now.Add(KeepFor + time.Minute)
	nmu.Unlock()
	if _, ok := m.GetDebate("phone", d.ID); ok {
		t.Error("an ended debate outlived KeepFor")
	}
}
