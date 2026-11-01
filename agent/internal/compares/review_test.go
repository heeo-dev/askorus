package compares

// Tests added after the S3 review (11-01, R-30): the cleanup after Close,
// a round that ends inside its spawning, and behaviours no test held
// (Prev, cards while requests run, the retry's company gate, the merge of
// two attempts, tokens and the twin Start).

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// endedLogs counts the "comparison ended" lines of a Manager's log.
func endedLogs(buf *syncBuffer) int {
	return strings.Count(buf.String(), `"msg":"comparison ended"`)
}

// Close (agent shutdown, or the CLI's Ctrl+C and 10-minute limit) while
// OpenCode judges still deletes OpenCode's judge conversations, which
// hold the question and the answers, with a context Close has not ended.
func TestCloseStillCleansUp(t *testing.T) {
	ais := threeAIs()
	oc := ais[2].(cleanerAI)
	oc.judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		<-ctx.Done()
		return core.JudgeResult{Model: req.Model, Cleanup: "ses_left" + string(rune('0'+n))}, ctx.Err()
	}
	var live atomic.Int32 // cleanups whose context still ran
	oc.cleanup = func(ctx context.Context, folder string, refs []string) error {
		if ctx.Err() == nil {
			live.Add(1)
		}
		return nil
	}
	m, _ := newTestManager(t, ais, nil)
	c := startDirect(t, m, threeAnswers("phone", ""))
	waitFor(t, "opencode to judge twice", func() bool { return len(oc.judgeCalls()) == 2 })
	m.Close()
	calls := oc.cleanupCalls()
	if len(calls) != 1 {
		t.Fatalf("%d cleanups after Close, want 1: the OpenCode judge conversations stay in the judge folder", len(calls))
	}
	refs := slices.Clone(calls[0])
	sort.Strings(refs)
	if !slices.Equal(refs, []string{"ses_left1", "ses_left2"}) {
		t.Errorf("refs %v", refs)
	}
	if live.Load() != 1 {
		t.Error("the cleanup got a context Close had already ended")
	}
	if got, _ := m.Get("phone", c.ID); got.Status != Done {
		t.Errorf("status %s", got.Status)
	}
}

// planBlock is a fakeAI whose Family of the answer model claude-m waits:
// the round is planning then.
type planBlock struct {
	*fakeAI
	once             sync.Once
	entered, release chan struct{}
}

func (p *planBlock) Family(model string) string {
	if model == "claude-m" {
		p.once.Do(func() { close(p.entered) })
		<-p.release
	}
	return p.fakeAI.Family(model)
}

// A cancel that lands while the round plans (the phone's '새 질문')
// ends every request at once, and the comparison ends once.
func TestCancelWhilePlanningEndsOnce(t *testing.T) {
	ais := threeAIs()
	pb := &planBlock{fakeAI: newFake("claude", "anthropic"), entered: make(chan struct{}), release: make(chan struct{})}
	ais[0] = pb
	m, buf := newTestManager(t, ais, nil)
	c := startDirect(t, m, threeAnswers("phone", ""))
	select {
	case <-pb.entered:
	case <-time.After(10 * time.Second):
		close(pb.release)
		t.Fatal("the round did not plan")
	}
	if got, err := m.Cancel("phone", c.ID); err != nil || got.Status != Running {
		t.Errorf("Cancel = %s %v", got.Status, err)
	}
	close(pb.release)
	c = waitIdle(t, m, "phone", c.ID)
	if c.Status != Failed || c.Error == nil || c.Error.Code != core.ErrCodeCancelled || len(c.Requests) != 6 {
		t.Fatalf("comparison %s %+v, %d requests", c.Status, c.Error, len(c.Requests))
	}
	for _, r := range c.Requests {
		if r.Status != judge.RequestFailed || r.Error.Code != core.ErrCodeCancelled || r.Attempt != 0 {
			t.Errorf("request %+v %+v", r, r.Error)
		}
	}
	if n := len(pb.judgeCalls()) + len(fakeOf(ais[1]).judgeCalls()) + len(fakeOf(ais[2]).judgeCalls()); n != 0 {
		t.Errorf("%d judge calls after cancel", n)
	}
	if n := endedLogs(buf); n != 1 {
		t.Errorf("comparison ended logged %d times, want 1:\n%s", n, buf.String())
	}
}

// A Retry after Close fails its requests at once as cancelled, and its
// round ends once.
func TestRetryAfterCloseEndsOnce(t *testing.T) {
	ais := threeAIs()
	codex := fakeOf(ais[1])
	codex.judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		return core.JudgeResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeFailed, "limit")
	}
	m, buf := newTestManager(t, ais, nil)
	c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
	m.Close()
	if n := endedLogs(buf); n != 1 {
		t.Fatalf("comparison ended logged %d times before the retry", n)
	}
	if _, err := m.Retry("phone", c.ID); err != nil {
		t.Fatal(err)
	}
	c = waitIdle(t, m, "phone", c.ID)
	if n := endedLogs(buf) - 1; n != 1 {
		t.Errorf("retry's round ended %d times, want 1:\n%s", n, buf.String())
	}
	for _, r := range requestsOf(c, "codex") {
		if r.Status != judge.RequestFailed || r.Error.Code != core.ErrCodeCancelled || r.Attempt != 0 {
			t.Errorf("request %+v %+v", r, r.Error)
		}
	}
	if n := len(codex.judgeCalls()); n != 2 {
		t.Errorf("%d codex calls", n)
	}
	if c.Status != Done {
		t.Errorf("status %s", c.Status)
	}
}

// A retry keeps the key of a card other than 다른 말 (Prev holds the
// items of the last ended call), even when its representative changes.
func TestRetryKeepsAgreeKey(t *testing.T) {
	ais := threeAIs()
	var failClaude atomic.Bool
	failClaude.Store(true)
	for _, a := range ais[1:] {
		fakeOf(a).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
			if failClaude.Load() && strings.Contains(req.Task.Units[0].Text, "Claude says") {
				return core.JudgeResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeFailed, "limit")
			}
			return ruleJudge(req), nil
		}
	}
	m, _ := newTestManager(t, ais, nil)
	s := threeAnswers("phone", "")
	s.Answers[0].Text = "Claude says the number is NUM 1."
	s.Answers[2].Text = "The number is NUM 1."
	c := waitIdle(t, m, "phone", startDirect(t, m, s).ID)
	agree := func(c Comparison) []judge.Item {
		var out []judge.Item
		for _, it := range c.Items {
			if it.Kind == judge.CardAgree {
				out = append(out, it)
			}
		}
		return out
	}
	before := agree(c)
	if c.Status != Done || len(before) != 1 || before[0].Positions[0].Units[0] == "claude:1" {
		t.Fatalf("round 1: %s, agree items %+v", c.Status, before)
	}
	failClaude.Store(false)
	if _, err := m.Retry("phone", c.ID); err != nil {
		t.Fatal(err)
	}
	c = waitIdle(t, m, "phone", c.ID)
	after := agree(c)
	if c.Status != Done || len(after) != 1 || after[0].Positions[0].Units[0] != "claude:1" {
		t.Fatalf("after retry: %s, agree items %+v", c.Status, after)
	}
	if after[0].Key != before[0].Key {
		t.Errorf("agree key %q before the retry, %q after", before[0].Key, after[0].Key)
	}
}

// Answers of two units: opencode differs on the number, and all three
// agree on the size.
func twoUnitAnswers() DirectSpec {
	s := threeAnswers("phone", "")
	s.Answers[0].Text = "The number is NUM num=1. The size is NUM size=3."
	s.Answers[1].Text = "The number is NUM num=1. The size is NUM size=3."
	s.Answers[2].Text = "The number is NUM num=2. The size is NUM size=3."
	return s
}

// While requests still run only 다른 말 items show (part 5); the other
// cards come once every request has ended.
func TestOnlyDifferWhileRunning(t *testing.T) {
	ais := threeAIs()
	release := make(chan struct{})
	fakeOf(ais[0]).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		<-release
		return ruleJudge(req), nil
	}
	m, _ := newTestManager(t, ais, nil)
	c := startDirect(t, m, twoUnitAnswers())
	var got Comparison
	waitFor(t, "the four other requests to be done", func() bool {
		got, _ = m.Get("phone", c.ID)
		n := 0
		for _, r := range got.Requests {
			if r.Status == judge.RequestDone {
				n++
			}
		}
		return n == 4
	})
	close(release)
	if got.Status != Running || got.Counts.Differ != 1 || got.Counts.Agree != 0 || got.Counts.Check != 0 || got.Counts.OnlyOne != 0 {
		t.Errorf("while running: %s, counts %+v", got.Status, got.Counts)
	}
	for _, it := range got.Items {
		if it.Kind != judge.CardDiffer {
			t.Errorf("a %s item while requests run: %+v", it.Kind, it)
		}
	}
	c = waitIdle(t, m, "phone", c.ID)
	if c.Status != Done || c.Counts.Differ != 1 || c.Counts.Agree != 1 {
		t.Errorf("at the end: %s, counts %+v", c.Status, c.Counts)
	}
}

// A retry gates the company again: a judge whose model is now of another
// company than planned fails with bad_model and is not called.
func TestRetryRegatesTheCompany(t *testing.T) {
	ais := threeAIs()
	codex := fakeOf(ais[1])
	codex.judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		return core.JudgeResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeFailed, "limit")
	}
	m, _ := newTestManager(t, ais, nil)
	c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
	codex.mu.Lock()
	codex.fams = map[string]string{"codex-judge": "anthropic"} // its judge model is now another company's
	codex.judge = nil
	codex.mu.Unlock()
	if _, err := m.Retry("phone", c.ID); err != nil {
		t.Fatal(err)
	}
	c = waitIdle(t, m, "phone", c.ID)
	for _, r := range requestsOf(c, "codex") {
		if r.Status != judge.RequestFailed || r.Error.Code != core.ErrCodeBadModel || r.Error.Message != msgCompany || r.Attempt != 0 {
			t.Errorf("request %+v %+v", r, r.Error)
		}
	}
	if n := len(codex.judgeCalls()); n != 2 {
		t.Errorf("%d codex calls, want 2: the retry must not call a judge of another company", n)
	}
}

// relabel is the rule judge's reply with id dropped (when drop) and the
// label of id set to label (when label is not "").
func relabel(req core.JudgeRequest, drop string, id, label string) core.JudgeResult {
	var out struct {
		Judgments []map[string]any `json:"judgments"`
	}
	_ = json.Unmarshal([]byte(fakecli.Answer(req.Prompt, false)), &out)
	out.Judgments = slices.DeleteFunc(out.Judgments, func(j map[string]any) bool { return j["id"] == drop })
	for _, j := range out.Judgments {
		if j["id"] == id && label != "" {
			j["label"] = label
		}
	}
	b, _ := json.Marshal(out)
	return core.JudgeResult{Output: b, Model: req.Model}
}

// When attempt 1 missed ids, attempt 2 adds only those: attempt 1's
// verdicts stand even when attempt 2 judged them otherwise (S3 decision 7).
func TestMergeKeepsAttemptOne(t *testing.T) {
	ais := threeAIs()
	fakeOf(ais[0]).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		if n%2 == 1 {
			return relabel(req, "a2", "", ""), nil // a1 opposite, a2 missing
		}
		return relabel(req, "", "a1", judge.LabelAbsent), nil // a1 judged otherwise, a2 same
	}
	m, _ := newTestManager(t, ais, func(o *Options) { o.Gate.Settings.PerAI = 1 })
	c := waitIdle(t, m, "phone", startDirect(t, m, twoUnitAnswers()).ID)
	rs := requestsOf(c, "claude")
	if len(rs) != 2 {
		t.Fatalf("%d claude requests", len(rs))
	}
	for _, r := range rs {
		if r.Status != judge.RequestDone || r.Attempt != 2 {
			t.Errorf("request %+v", r)
		}
		labels := map[string]string{}
		for _, j := range c.Judgments {
			if j.Request == r.ID {
				labels[j.Unit] = j.Label
			}
		}
		if labels[r.From+":1"] != judge.LabelOpposite || labels[r.From+":2"] != judge.LabelSame {
			t.Errorf("%s judged %v, want %s:1 opposite (attempt 1) and %s:2 same (attempt 2)", r.ID, labels, r.From, r.From)
		}
	}
}

// The tokens of both attempts add up.
func TestTokensAddUp(t *testing.T) {
	ais := threeAIs()
	fakeOf(ais[0]).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		if n%2 == 1 {
			return core.JudgeResult{Output: json.RawMessage(fakecli.ProseText), Model: req.Model, InputTokens: 7, OutputTokens: 3}, nil
		}
		return ruleJudge(req), nil // 10 and 2
	}
	m, _ := newTestManager(t, ais, func(o *Options) { o.Gate.Settings.PerAI = 1 })
	c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
	for _, r := range requestsOf(c, "claude") {
		if r.Status != judge.RequestDone || r.Attempt != 2 || r.InputTokens != 17 || r.OutputTokens != 5 {
			t.Errorf("request %+v, want 17 input and 5 output tokens", r)
		}
	}
}

// barrierInfo is a fakeAI whose Info waits until want callers are in it,
// so twin Starts both pass the key lookup before either registers.
type barrierInfo struct {
	*fakeAI
	mu   sync.Mutex
	n    int
	want int
	all  chan struct{}
}

func (b *barrierInfo) Info(ctx context.Context) core.AdapterInfo {
	b.mu.Lock()
	if b.n++; b.n == b.want {
		close(b.all)
	}
	b.mu.Unlock()
	select {
	case <-b.all:
	case <-time.After(5 * time.Second):
	}
	return b.fakeAI.Info(ctx)
}

// Two Starts with the same key at once make one comparison.
func TestTwinStart(t *testing.T) {
	ais := threeAIs()
	bi := &barrierInfo{fakeAI: newFake("claude", "anthropic"), want: 2, all: make(chan struct{})}
	ais[0] = bi
	m, _ := newTestManager(t, ais, nil)
	var wg sync.WaitGroup
	ids := make([]string, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := m.Start(threeAnswers("phone", "twin"))
			ids[i], errs[i] = c.ID, err
		}()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || ids[0] == "" || ids[0] != ids[1] {
		t.Fatalf("twin Starts = %q %v, %q %v", ids[0], errs[0], ids[1], errs[1])
	}
	waitIdle(t, m, "phone", ids[0])
	m.mu.Lock()
	n := len(m.byID)
	m.mu.Unlock()
	calls := len(bi.judgeCalls()) + len(fakeOf(ais[1]).judgeCalls()) + len(fakeOf(ais[2]).judgeCalls())
	if n != 1 || calls != 6 {
		t.Errorf("%d comparisons, %d judge calls; want 1 and 6", n, calls)
	}
}

// A failed comparison names a judge's failure (judge_failed, which Retry
// can fix) before a no_judge record: opencode does not judge, so the
// claude↔codex pair has no judge, and claude and codex hit their limits.
func TestFailedErrorPrefersAJudgeFailure(t *testing.T) {
	ais := threeAIs()
	fakeOf(ais[2]).setAvailable(false)
	for _, a := range ais[:2] {
		fakeOf(a).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
			return core.JudgeResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeFailed, "usage limit")
		}
	}
	m, _ := newTestManager(t, ais, nil)
	c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
	noJudge := 0
	for _, r := range c.Requests {
		if r.Judge == "" && r.Error != nil && r.Error.Code == core.ErrCodeNoJudge {
			noJudge++
		}
	}
	if noJudge == 0 {
		t.Fatalf("no no_judge record; the test no longer holds the case: %+v", c.Requests)
	}
	if c.Status != Failed || c.Error == nil || c.Error.Code != core.ErrCodeJudgeFailed {
		t.Errorf("comparison %s, error %+v: want failed with judge_failed", c.Status, c.Error)
	}
}
