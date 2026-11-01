package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/compares"
	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// ---- fakes ----------------------------------------------------------------

// judgeFamilies are the companies of the fake AIs' models.
var judgeFamilies = map[string]string{"claude": "anthropic", "codex": "openai", "opencode": "deepseek"}

// judgeMins are the oldest versions the fake judges were "tested" with.
var judgeMins = map[string]string{"claude": "1.0.0", "codex": "1.0.0", "opencode": "1.0.0"}

// judgeAdapter is an AI that asks like askAdapter and judges in process
// with fakecli's rule judge (numbers "NUM n" decide same and opposite).
type judgeAdapter struct {
	askAdapter
	family  string
	version string        // "" keeps askAdapter's 1.0.0
	answer  string        // the answer to every ask; "" keeps askAdapter's
	askGate chan struct{} // when set, an ask waits for it to close
	leaves  bool          // Judge returns a cleanup ref (cleaningAdapter)

	jmu     sync.Mutex
	judges  []core.JudgeRequest
	fail    string        // while set, Judge fails with judge_failed and this message
	block   chan struct{} // while set, Judge waits for it to close (or for ctx)
	cleaned []string      // the folders CleanupJudge was called with
	refs    [][]string
}

func newJudge(name string) *judgeAdapter {
	return &judgeAdapter{askAdapter: askAdapter{name: name, available: true}, family: judgeFamilies[name]}
}

func (a *judgeAdapter) Info(ctx context.Context) core.AdapterInfo {
	info := a.askAdapter.Info(ctx)
	if a.version != "" {
		info.Version = a.version
	}
	return info
}

func (a *judgeAdapter) Ask(ctx context.Context, req core.AskRequest) (core.AskResult, error) {
	if a.askGate != nil {
		select {
		case <-a.askGate:
		case <-ctx.Done():
			return core.AskResult{}, ctx.Err()
		}
	}
	res, err := a.askAdapter.Ask(ctx, req)
	if err == nil && a.answer != "" {
		res.Answer = a.answer
	}
	return res, err
}

func (a *judgeAdapter) JudgeDefault() core.AskModel {
	return core.AskModel{Model: a.name + "-judge", Effort: "low"}
}

func (a *judgeAdapter) Family(model string) string {
	if model == "" {
		return ""
	}
	return a.family
}

func (a *judgeAdapter) Judge(ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
	a.jmu.Lock()
	a.judges = append(a.judges, req)
	n, block := len(a.judges), a.block
	a.jmu.Unlock()
	res := core.JudgeResult{Model: req.Model, InputTokens: 7, OutputTokens: 3}
	if a.leaves {
		res.Cleanup = fmt.Sprintf("%s-ref-%d", a.name, n)
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return res, ctx.Err()
		}
	}
	a.jmu.Lock()
	fail := a.fail
	a.jmu.Unlock()
	if fail != "" {
		return res, core.Fail(core.ErrCodeJudgeFailed, fail)
	}
	res.Output = json.RawMessage(fakecli.Answer(req.Prompt, false))
	return res, nil
}

func (a *judgeAdapter) setFail(msg string) {
	a.jmu.Lock()
	a.fail = msg
	a.jmu.Unlock()
}

func (a *judgeAdapter) setBlock(ch chan struct{}) {
	a.jmu.Lock()
	a.block = ch
	a.jmu.Unlock()
}

func (a *judgeAdapter) judgeCalls() []core.JudgeRequest {
	a.jmu.Lock()
	defer a.jmu.Unlock()
	return slices.Clone(a.judges)
}

func (a *judgeAdapter) cleanups() []string {
	a.jmu.Lock()
	defer a.jmu.Unlock()
	return slices.Clone(a.cleaned)
}

// cleaningAdapter is a judgeAdapter whose judging leaves something behind,
// as OpenCode's does.
type cleaningAdapter struct{ *judgeAdapter }

func newCleaning(name string) cleaningAdapter {
	j := newJudge(name)
	j.leaves = true
	return cleaningAdapter{j}
}

func (c cleaningAdapter) CleanupJudge(_ context.Context, folder string, refs []string) error {
	c.jmu.Lock()
	defer c.jmu.Unlock()
	c.cleaned = append(c.cleaned, folder)
	c.refs = append(c.refs, slices.Clone(refs))
	return nil
}

// judgeEnv serves the adapters with an ask folder and the fake judges'
// minimum versions.
func judgeEnv(t *testing.T, mutate func(*Options), adapters ...core.Adapter) (env, string) {
	t.Helper()
	ask := filepath.Join(t.TempDir(), "ask")
	e := newEnv(t, adapters[0], func(o *Options) {
		o.Adapters = adapters
		o.AskFolder = ask
		o.JudgeMinVersions = judgeMins
		if mutate != nil {
			mutate(o)
		}
	})
	return e, ask
}

// threeJudges are claude, codex and opencode (which leaves conversations).
func threeJudges() (*judgeAdapter, *judgeAdapter, cleaningAdapter) {
	return newJudge("claude"), newJudge("codex"), newCleaning("opencode")
}

func decodeBody[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	return v
}

func (e env) comparison(t *testing.T, id string) ComparisonStatus {
	t.Helper()
	w := e.do("GET", "/v1/comparisons/"+id, nil)
	if w.Code != 200 {
		t.Fatalf("GET comparison = %d %s", w.Code, w.Body)
	}
	return decodeBody[ComparisonStatus](t, w)
}

// waitComparison polls the comparison like the phone does until it has
// ended.
func (e env) waitComparison(t *testing.T, id string) ComparisonStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := e.comparison(t, id)
		if st.Status != compares.Waiting && st.Status != compares.Running {
			if st.PollAfterMs != 0 {
				t.Errorf("ended comparison with pollAfterMs %d", st.PollAfterMs)
			}
			return st
		}
		if st.PollAfterMs != compares.PollMs {
			t.Fatalf("%s comparison with pollAfterMs %d", st.Status, st.PollAfterMs)
		}
		if time.Now().After(deadline) {
			t.Fatalf("comparison still %s: %+v", st.Status, st.Requests)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// idle waits until nothing of the comparison runs, its cleanup included.
func (e env) idle(t *testing.T, id string) compares.Comparison {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, ok := e.srv.Compares().WaitIdle(ctx, e.device, id)
	if !ok || ctx.Err() != nil {
		t.Fatalf("comparison %s not idle: %v %v", id, ok, ctx.Err())
	}
	return c
}

var comparisonIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ---- info -------------------------------------------------------------------

func TestInfoAnnouncesJudge(t *testing.T) {
	cl, cx, oc := threeJudges()
	e, _ := judgeEnv(t, func(o *Options) {
		o.Judge = config.JudgeConfig{Models: map[string]core.AskModel{"codex": {Model: "gpt-x", Effort: "high"}}}
	}, cl, cx, oc)
	w := e.do("GET", "/v1/info", nil)
	if w.Code != 200 {
		t.Fatalf("info = %d %s", w.Code, w.Body)
	}
	resp := decodeBody[InfoResponse](t, w)
	want := map[string]core.JudgeInfo{
		"claude":   {Model: "claude-judge", Effort: "low", Family: "anthropic"},
		"codex":    {Model: "gpt-x", Effort: "high", Family: "openai"}, // judge.models of agent.json
		"opencode": {Model: "opencode-judge", Effort: "low", Family: "deepseek"},
	}
	for _, a := range resp.Agents {
		if a.Judge == nil || *a.Judge != want[a.Agent] {
			t.Errorf("%s judge = %+v, want %+v", a.Agent, a.Judge, want[a.Agent])
		}
		if !contains(a.Capabilities, core.CapJudge) || !contains(a.Capabilities, core.CapAsk) || a.Ask == nil {
			t.Errorf("%s = %+v", a.Agent, a)
		}
	}
	if !contains(resp.Capabilities, core.CapJudge) || contains(resp.Reserved, core.CapJudge) {
		t.Errorf("capabilities %v, reserved %v", resp.Capabilities, resp.Reserved)
	}
	if resp.Judging == nil {
		t.Fatal("no judging")
	}
	if j := *resp.Judging; j.Engine != "pc" || j.Shape != judge.ShapeDirected || j.Labels != judge.LabelsV1 || j.Rules != judge.Rules || !slices.Equal(j.Checks, []string{judge.CheckUnsure}) {
		t.Errorf("judging = %+v", j)
	}
	var raw struct {
		Judging      map[string]json.RawMessage `json:"judging"`
		Integrations json.RawMessage            `json:"integrations"`
	}
	json.Unmarshal(w.Body.Bytes(), &raw)
	var keys []string
	for k := range raw.Judging {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"checks", "engine", "labels", "rules", "shape"}) {
		t.Errorf("judging keys = %v (verified and typicalMs come with S6)", keys)
	}
	if string(raw.Integrations) != "[]" {
		t.Errorf("integrations = %s, want [] (N1)", raw.Integrations)
	}
}

// An AI judges only when the gate lets it: judge.off, an old or unknown
// CLI version, no ask folder, or an adapter that cannot judge leave judge
// reserved and info without judging.
func TestInfoJudgeGated(t *testing.T) {
	info := func(t *testing.T, e env) (InfoResponse, string) {
		t.Helper()
		w := e.do("GET", "/v1/info", nil)
		if w.Code != 200 {
			t.Fatalf("info = %d %s", w.Code, w.Body)
		}
		return decodeBody[InfoResponse](t, w), w.Body.String()
	}
	none := func(t *testing.T, e env) {
		t.Helper()
		resp, body := info(t, e)
		for _, a := range resp.Agents {
			if a.Judge != nil || contains(a.Capabilities, core.CapJudge) {
				t.Errorf("%s judges: %+v", a.Agent, a)
			}
		}
		if resp.Judging != nil || strings.Contains(body, `"judging"`) || !contains(resp.Reserved, core.CapJudge) || contains(resp.Capabilities, core.CapJudge) {
			t.Errorf("info = %s", body)
		}
		if !strings.Contains(body, `"integrations":[]`) {
			t.Errorf("integrations missing: %s", body)
		}
	}
	t.Run("old version", func(t *testing.T) {
		cl, cx, oc := threeJudges()
		cl.version = "0.9.9"
		e, _ := judgeEnv(t, nil, cl, cx, oc)
		resp, _ := info(t, e)
		if resp.Agents[0].Judge != nil || contains(resp.Agents[0].Capabilities, core.CapJudge) || resp.Agents[1].Judge == nil || resp.Judging == nil {
			t.Errorf("agents = %+v", resp.Agents)
		}
	})
	t.Run("all old", func(t *testing.T) {
		cl, cx, oc := threeJudges()
		cl.version, cx.version, oc.version = "0.1.0", "0.1.0", "garbled"
		e, _ := judgeEnv(t, nil, cl, cx, oc)
		none(t, e)
	})
	t.Run("off", func(t *testing.T) {
		cl, cx, oc := threeJudges()
		e, _ := judgeEnv(t, func(o *Options) { o.Judge.Off = true }, cl, cx, oc)
		none(t, e)
	})
	t.Run("no minimum versions", func(t *testing.T) {
		cl, cx, oc := threeJudges()
		e, _ := judgeEnv(t, func(o *Options) { o.JudgeMinVersions = nil }, cl, cx, oc)
		none(t, e)
	})
	t.Run("no ask folder", func(t *testing.T) {
		cl, cx, oc := threeJudges()
		e, _ := judgeEnv(t, func(o *Options) { o.AskFolder = "" }, cl, cx, oc)
		none(t, e)
	})
	t.Run("relative ask folder", func(t *testing.T) {
		cl, cx, oc := threeJudges()
		e, _ := judgeEnv(t, func(o *Options) { o.AskFolder = "ask" }, cl, cx, oc)
		none(t, e)
	})
	t.Run("cannot judge", func(t *testing.T) {
		e, _ := judgeEnv(t, nil, &askAdapter{name: "claude", available: true}, &askAdapter{name: "codex", available: true})
		none(t, e)
	})
}

// ---- POST /v1/runs with compare -----------------------------------------

func startRun(t *testing.T, e env, body map[string]any) RunStatus {
	t.Helper()
	w := e.post("/v1/runs", body)
	if w.Code != 202 {
		t.Fatalf("POST run = %d %s", w.Code, w.Body)
	}
	return decodeBody[RunStatus](t, w)
}

func TestRunWithCompare(t *testing.T) {
	cl, cx, oc := threeJudges()
	cl.answer = "ANSWER-SECRET 값은 NUM 1입니다."
	cx.answer = "ANSWER-SECRET 값은 NUM 2입니다."
	oc.answer = "ANSWER-SECRET 값은 NUM 1입니다."
	cx.askGate = make(chan struct{}) // codex answers after the 202
	e, ask := judgeEnv(t, nil, cl, cx, oc)
	st := startRun(t, e, map[string]any{
		"mode": "ask", "prompt": "  QUESTION-SECRET 값은?  ",
		"compare": map[string]any{"earlier": []string{"EARLIER-SECRET 처음 질문"}, "judges": []string{"opencode", "claude", "codex"}, "later": true},
	})
	if !comparisonIDRe.MatchString(st.ComparisonID) || st.CompareError != nil {
		t.Fatalf("202 = %+v", st)
	}
	// The comparison waits for the last answer; the run's end starts it
	// (runs.Manager.OnEnd).
	if c := e.comparison(t, st.ComparisonID); c.Status != compares.Waiting || c.PollAfterMs != compares.PollMs || c.RunID != st.ID {
		t.Fatalf("before the last answer = %+v", c)
	}
	close(cx.askGate)
	// Every later GET of the run names the same comparison.
	for i := 0; i < 3; i++ {
		got := e.waitRun(t, st.ID)
		if got.ComparisonID != st.ComparisonID || got.CompareError != nil {
			t.Fatalf("GET %d = %+v", i, got)
		}
	}
	c := e.waitComparison(t, st.ComparisonID)
	if c.Status != compares.Done || c.Source != compares.SourceRun || c.RunID != st.ID || c.Engine != "pc" || c.Error != nil {
		t.Fatalf("comparison = %+v", c.Comparison)
	}
	if len(c.Answers) != 3 || len(c.Requests) != 6 {
		t.Fatalf("answers %d, requests %+v", len(c.Answers), c.Requests)
	}
	for _, r := range c.Requests {
		if r.Status != judge.RequestDone || r.Judge == r.From || r.Judge == r.Against || r.Judge == "" {
			t.Errorf("request = %+v", r)
		}
	}
	if c.Counts.Differ < 1 || !slices.ContainsFunc(c.Items, func(it judge.Item) bool { return it.Kind == judge.CardDiffer }) {
		t.Errorf("no differ card: %+v", c.Items)
	}
	// The judges get the question as the person wrote it, without the ask
	// preamble, and the earlier questions; they run in the judge folder.
	for _, j := range []*judgeAdapter{cl, cx, oc.judgeAdapter} {
		calls := j.judgeCalls()
		if len(calls) != 2 {
			t.Errorf("%s judged %d times", j.name, len(calls))
		}
		for _, call := range calls {
			if call.Task.Question != "QUESTION-SECRET 값은?" || !slices.Equal(call.Task.Earlier, []string{"EARLIER-SECRET 처음 질문"}) ||
				call.Folder != core.JudgeFolder(ask) || call.Model != j.name+"-judge" || strings.Contains(call.Prompt, core.AskPreamble) {
				t.Errorf("%s judge request = %+v", j.name, call.Task)
			}
		}
	}
	e.idle(t, st.ComparisonID)
	if got := oc.cleanups(); !slices.Equal(got, []string{core.JudgeFolder(ask)}) {
		t.Errorf("opencode cleanups = %v", got)
	}
	logs := e.logs.String()
	for _, leak := range []string{"QUESTION-SECRET", "ANSWER-SECRET", "EARLIER-SECRET", "값은"} {
		if strings.Contains(logs, leak) {
			t.Errorf("log contains %q: %s", leak, logs)
		}
	}
	for _, line := range []string{`"comparison started"`, `"comparison ended"`, `"judge request"`, `"compare":true`} {
		if !strings.Contains(logs, line) {
			t.Errorf("log lacks %s: %s", line, logs)
		}
	}
}

// A compare the agent cannot read gives compareError bad_compare in the
// 202, and the ask runs all the same: never a 400 or 413 for compare.
func TestRunBadCompareStillAsks(t *testing.T) {
	cl, cx, oc := threeJudges()
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	cases := map[string]any{
		"number":         5,
		"string":         "yes",
		"array":          []string{"claude"},
		"four earlier":   map[string]any{"earlier": []string{"a", "b", "c", "d"}},
		"long earlier":   map[string]any{"earlier": []string{strings.Repeat("x", 4097)}},
		"NUL in earlier": map[string]any{"earlier": []string{"a\x00b"}},
		"earlier number": map[string]any{"earlier": []int{1}},
		"unknown judge":  map[string]any{"judges": []string{"gemini"}},
		"twice":          map[string]any{"judges": []string{"claude", "claude"}},
		"not a name":     map[string]any{"judges": []string{"Claude"}},
		"flag as judge":  map[string]any{"judges": []string{"--dangerously-skip-permissions"}},
		"nine judges":    map[string]any{"judges": []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}},
	}
	asked := 0
	for name, compare := range cases {
		st := startRun(t, e, map[string]any{"mode": "ask", "prompt": "q", "compare": compare})
		asked++
		if st.CompareError == nil || st.CompareError.Code != CodeBadCompare || st.CompareError.Message == "" || st.ComparisonID != "" {
			t.Errorf("%s: 202 = %+v", name, st)
		}
		if done := e.waitRun(t, st.ID); done.Status != "done" || done.ComparisonID != "" || done.CompareError != nil {
			t.Errorf("%s: run = %+v", name, done)
		}
	}
	if n := len(cl.requests()); n != asked {
		t.Errorf("asked %d times, want %d", n, asked)
	}
	// No compare, or null, is no comparison and no error.
	for _, body := range []map[string]any{{"mode": "ask", "prompt": "q"}, {"mode": "ask", "prompt": "q", "compare": nil}} {
		st := startRun(t, e, body)
		if st.CompareError != nil || st.ComparisonID != "" {
			t.Errorf("no compare: %+v", st)
		}
		e.waitRun(t, st.ID)
	}
	// The limits themselves are fine.
	st := startRun(t, e, map[string]any{"mode": "ask", "prompt": "q", "compare": map[string]any{
		"earlier": []string{strings.Repeat("x", 4096), "", "c"}, "judges": []string{"claude", "codex", "opencode"},
	}})
	if st.CompareError != nil || st.ComparisonID == "" {
		t.Errorf("at the limits: %+v", st)
	}
	e.waitRun(t, st.ID)
	e.waitComparison(t, st.ComparisonID)
	for _, j := range []*judgeAdapter{cl, cx, oc.judgeAdapter} {
		if n := len(j.judgeCalls()); n != 2 {
			t.Errorf("%s judged %d times: only the last run is compared", j.name, n)
		}
	}
}

// One asked AI has nothing to compare with: compareError need_two_answers.
// Judges are cut to the asked AIs; none of them left means every asked AI.
func TestRunCompareJudgesAndOneAI(t *testing.T) {
	cl, cx, oc := threeJudges()
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	st := startRun(t, e, map[string]any{"mode": "ask", "prompt": "q", "agents": []map[string]string{{"agent": "claude"}}, "compare": map[string]any{}})
	if st.CompareError == nil || st.CompareError.Code != core.ErrCodeNeedTwoAnswers || st.ComparisonID != "" {
		t.Fatalf("one AI: %+v", st)
	}
	if done := e.waitRun(t, st.ID); done.Status != "done" {
		t.Fatalf("run = %+v", done)
	}
	// opencode was not asked, so it does not judge (결정 5); claude and
	// codex cannot judge each other, so every pair has no judge.
	st = startRun(t, e, map[string]any{"mode": "ask", "prompt": "q",
		"agents": []map[string]string{{"agent": "claude"}, {"agent": "codex"}}, "compare": map[string]any{"judges": []string{"opencode"}}})
	if st.CompareError != nil || st.ComparisonID == "" {
		t.Fatalf("two AIs: %+v", st)
	}
	c := e.waitComparison(t, st.ComparisonID)
	if c.Status != compares.Failed || c.Error == nil || c.Error.Code != core.ErrCodeNoJudge {
		t.Errorf("comparison = %+v", c.Comparison)
	}
	if n := len(oc.judgeCalls()) + len(cl.judgeCalls()) + len(cx.judgeCalls()); n != 0 {
		t.Errorf("%d judge calls", n)
	}
}

// The longest body: a 32 KiB question and three 4 KiB earlier questions,
// all quotes, which JSON doubles. It is over the old limit and accepted.
func TestRunCompareLongestBody(t *testing.T) {
	cl, cx, oc := threeJudges()
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	q := strings.Repeat(`"`, maxPrompt)
	earlier := strings.Repeat(`"`, maxCompareEarlierBytes)
	body := map[string]any{"mode": "ask", "prompt": q, "compare": map[string]any{"earlier": []string{earlier, earlier, earlier}}}
	raw, _ := json.Marshal(body)
	if len(raw) <= 2*maxPrompt+8<<10 || len(raw) > maxRunBody {
		t.Fatalf("body is %d bytes: the test must need the room compare adds", len(raw))
	}
	st := startRun(t, e, body)
	if st.ComparisonID == "" || st.CompareError != nil {
		t.Fatalf("202 = %+v", st)
	}
	e.waitRun(t, st.ID)
	e.waitComparison(t, st.ComparisonID)
}

// At 8 unfinished comparisons the 9th run gets too_many_comparisons, and
// its ask runs all the same. A re-sent run still finds its comparison.
func TestRunCompareTooMany(t *testing.T) {
	cl, cx, oc := threeJudges()
	block := make(chan struct{})
	for _, j := range []*judgeAdapter{cl, cx, oc.judgeAdapter} {
		j.setBlock(block)
	}
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	var ids []string
	first := ""
	for i := 0; i < compares.MaxUnfinished; i++ {
		body := map[string]any{"mode": "ask", "prompt": "q", "compare": map[string]any{}}
		if i == 0 {
			body["clientRunId"] = "first"
		}
		st := startRun(t, e, body)
		if st.ComparisonID == "" || st.CompareError != nil {
			t.Fatalf("run %d: %+v", i, st)
		}
		if i == 0 {
			first = st.ComparisonID
		}
		ids = append(ids, st.ComparisonID)
		e.waitRun(t, st.ID) // runs.MaxRunning bounds the asks, not the comparisons
	}
	st := startRun(t, e, map[string]any{"mode": "ask", "prompt": "q", "compare": map[string]any{}})
	if st.CompareError == nil || st.CompareError.Code != core.ErrCodeTooManyComparisons || st.ComparisonID != "" {
		t.Fatalf("9th = %+v", st)
	}
	if done := e.waitRun(t, st.ID); done.Status != "done" || len(cl.requests()) != compares.MaxUnfinished+1 {
		t.Fatalf("9th ask = %+v, %d asks", done, len(cl.requests()))
	}
	again := startRun(t, e, map[string]any{"mode": "ask", "prompt": "q", "clientRunId": "first", "compare": map[string]any{}})
	if again.ComparisonID != first || again.CompareError != nil {
		t.Fatalf("re-sent run = %+v, want comparison %s", again, first)
	}
	close(block)
	for _, id := range ids {
		if c := e.waitComparison(t, id); c.Status != compares.Done {
			t.Errorf("%s = %s", id, c.Status)
		}
	}
}

// ---- the comparison routes ---------------------------------------------

func TestComparisonBelongsToItsDevice(t *testing.T) {
	cl, cx, oc := threeJudges()
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	st := startRun(t, e, map[string]any{"mode": "ask", "prompt": "q", "compare": map[string]any{}})
	e.waitComparison(t, st.ComparisonID)
	other := map[string]string{"Authorization": "Bearer " + e.addDevice(t, "tablet")}
	for _, req := range [][2]string{
		{"GET", "/v1/comparisons/" + st.ComparisonID},
		{"POST", "/v1/comparisons/" + st.ComparisonID + "/retry"},
		{"POST", "/v1/comparisons/" + st.ComparisonID + "/cancel"},
	} {
		if w := e.do(req[0], req[1], other); w.Code != 404 || errCode(t, w) != CodeComparisonNotFound {
			t.Errorf("other device %s %s = %d %s", req[0], req[1], w.Code, w.Body)
		}
	}
	// The other device's GET of the run does not show it either.
	if w := e.do("GET", "/v1/runs/"+st.ID, other); w.Code != 404 {
		t.Errorf("other device's run = %d", w.Code)
	}
	unknown := strings.Repeat("0", 32)
	for _, req := range [][2]string{
		{"GET", "/v1/comparisons/" + unknown},
		{"POST", "/v1/comparisons/" + unknown + "/retry"},
		{"POST", "/v1/comparisons/" + unknown + "/cancel"},
	} {
		if w := e.do(req[0], req[1], nil); w.Code != 404 || errCode(t, w) != CodeComparisonNotFound {
			t.Errorf("unknown %s %s = %d %s", req[0], req[1], w.Code, w.Body)
		}
	}
	for _, req := range [][2]string{
		{"GET", "/v1/comparisons"},
		{"DELETE", "/v1/comparisons/" + st.ComparisonID},
		{"GET", "/v1/comparisons/" + st.ComparisonID + "/retry"},
		{"GET", "/v1/comparisons/" + st.ComparisonID + "/cancel"},
	} {
		if w := e.do(req[0], req[1], nil); w.Code != 405 {
			t.Errorf("%s %s = %d", req[0], req[1], w.Code)
		}
	}
	if w := e.do("GET", "/v1/comparisons/"+st.ComparisonID, map[string]string{"Authorization": ""}); w.Code != 401 {
		t.Errorf("no token = %d", w.Code)
	}
}

// directBody is a POST /v1/comparisons body with three answers: two of AI
// programs on the PC and one pasted from ChatGPT.
func directBody(key string) map[string]any {
	return map[string]any{
		"clientComparisonId": key,
		"question":           "  QUESTION-SECRET 몇 개인가요?  ",
		"earlier":            []string{"EARLIER-SECRET"},
		"answers": []map[string]any{
			{"id": "claude", "agent": "claude", "via": "pc", "model": "claude-std", "text": "ANSWER-SECRET 모두 NUM 1개입니다."},
			{"id": "codex", "agent": "codex", "via": "pc", "model": "codex-std", "text": "ANSWER-SECRET 모두 NUM 2개입니다."},
			{"id": "gpt", "agent": "chatgpt", "via": "handoff", "text": "ANSWER-SECRET 모두 NUM 1개입니다.", "pasted": true},
		},
		"later": "ignored",
	}
}

func TestDirectComparison(t *testing.T) {
	cl, cx, oc := threeJudges()
	e, ask := judgeEnv(t, nil, cl, cx, oc)
	w := e.post("/v1/comparisons", directBody("c-1"))
	if w.Code != 202 {
		t.Fatalf("POST = %d %s", w.Code, w.Body)
	}
	st := decodeBody[ComparisonStatus](t, w)
	if !comparisonIDRe.MatchString(st.ID) || st.Status != compares.Running || st.Source != compares.SourceDirect || st.Engine != "pc" || st.PollAfterMs != compares.PollMs {
		t.Fatalf("202 = %+v", st)
	}
	if again := decodeBody[ComparisonStatus](t, e.post("/v1/comparisons", directBody("c-1"))); again.ID != st.ID {
		t.Fatalf("same clientComparisonId gave %s, want %s", again.ID, st.ID)
	}
	c := e.waitComparison(t, st.ID)
	if c.Status != compares.Done || len(c.Answers) != 3 || len(c.Requests) == 0 {
		t.Fatalf("comparison = %+v", c.Comparison)
	}
	if c.Answers[2].Family != "openai" || c.Answers[2].Via != judge.ViaHandoff || c.Answers[0].Family != "anthropic" {
		t.Errorf("answers = %+v", c.Answers)
	}
	for _, r := range c.Requests {
		if r.Status != judge.RequestDone {
			t.Errorf("request = %+v", r)
		}
	}
	if c.Counts.Differ < 1 {
		t.Errorf("counts = %+v", c.Counts)
	}
	calls := slices.Concat(cl.judgeCalls(), cx.judgeCalls(), oc.judgeCalls())
	if len(calls) != len(c.Requests) {
		t.Errorf("%d judge calls for %d requests: the second POST asked again", len(calls), len(c.Requests))
	}
	for _, call := range calls {
		if call.Task.Question != "QUESTION-SECRET 몇 개인가요?" || call.Folder != core.JudgeFolder(ask) {
			t.Errorf("judge request = %+v in %s", call.Task, call.Folder)
		}
	}
	// A new key is a new comparison.
	if other := decodeBody[ComparisonStatus](t, e.post("/v1/comparisons", directBody("c-2"))); other.ID == st.ID {
		t.Error("another clientComparisonId gave the same comparison")
	} else {
		e.waitComparison(t, other.ID)
	}
	logs := e.logs.String()
	for _, leak := range []string{"QUESTION-SECRET", "ANSWER-SECRET", "EARLIER-SECRET", "몇 개"} {
		if strings.Contains(logs, leak) {
			t.Errorf("log contains %q", leak)
		}
	}
}

func TestDirectComparisonRefused(t *testing.T) {
	cl, cx, oc := threeJudges()
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	with := func(f func(b map[string]any)) map[string]any {
		b := directBody("")
		f(b)
		return b
	}
	answers := func(b map[string]any) []map[string]any { return b["answers"].([]map[string]any) }
	pasted := func(n int) []map[string]any {
		var out []map[string]any
		for i := 0; i < n; i++ {
			out = append(out, map[string]any{"id": fmt.Sprintf("a%d", i), "agent": "chatgpt", "via": "handoff", "text": "t"})
		}
		return out
	}
	cases := []struct {
		name   string
		body   any
		status int
		code   string
	}{
		{"one answer", with(func(b map[string]any) { b["answers"] = answers(b)[:1] }), 400, core.ErrCodeNeedTwoAnswers},
		{"seven answers", with(func(b map[string]any) { b["answers"] = pasted(7) }), 400, CodeBadRequest},
		{"long text", with(func(b map[string]any) { answers(b)[0]["text"] = strings.Repeat("x", 65<<10) }), 413, CodeBadRequest},
		{"long question", with(func(b map[string]any) { b["question"] = strings.Repeat("x", 33<<10) }), 413, CodeBadRequest},
		{"body over 1 MiB", with(func(b map[string]any) { b["question"] = strings.Repeat("x", 1<<20) }), 413, CodeBadRequest},
		{"bad via", with(func(b map[string]any) { answers(b)[0]["via"] = "web" }), 400, CodeBadRequest},
		{"unknown app", with(func(b map[string]any) { answers(b)[2]["agent"] = "bard" }), 400, CodeBadRequest},
		{"unknown AI", with(func(b map[string]any) { answers(b)[0]["agent"] = "gemini" }), 400, CodeBadRequest},
		{"same id", with(func(b map[string]any) { answers(b)[1]["id"] = "claude" }), 400, CodeBadRequest},
		{"flag as model", with(func(b map[string]any) { answers(b)[0]["model"] = "--dangerously-skip-permissions" }), 400, CodeBadRequest},
		{"empty question", with(func(b map[string]any) { b["question"] = " \n " }), 400, CodeBadRequest},
		{"four earlier", with(func(b map[string]any) { b["earlier"] = []string{"a", "b", "c", "d"} }), 400, CodeBadRequest},
		{"unknown judge", with(func(b map[string]any) { b["judges"] = []string{"gemini"} }), 400, CodeBadRequest},
		{"long client id", with(func(b map[string]any) { b["clientComparisonId"] = strings.Repeat("k", 65) }), 400, CodeBadRequest},
		{"not an object", []int{1}, 400, CodeBadRequest},
	}
	for _, c := range cases {
		w := e.post("/v1/comparisons", c.body)
		if w.Code != c.status || errCode(t, w) != c.code {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body)
		}
	}
	if w := e.doBody("POST", "/v1/comparisons", "{"); w.Code != 400 || errCode(t, w) != CodeBadRequest {
		t.Errorf("bad JSON = %d %s", w.Code, w.Body)
	}
	if n := len(cl.judgeCalls()) + len(cx.judgeCalls()) + len(oc.judgeCalls()); n != 0 {
		t.Errorf("refused requests made %d judge calls", n)
	}
	if strings.Contains(e.logs.String(), "SECRET") {
		t.Errorf("log has content: %s", e.logs)
	}
}

func TestDirectComparisonNeedsJudgeAndFolder(t *testing.T) {
	cl, cx, oc := threeJudges()
	e, _ := judgeEnv(t, func(o *Options) { o.Judge.Off = true }, cl, cx, oc)
	if w := e.post("/v1/comparisons", directBody("")); w.Code != 503 || errCode(t, w) != core.ErrCodeNoJudge {
		t.Errorf("judging off = %d %s", w.Code, w.Body)
	}
	e, _ = judgeEnv(t, func(o *Options) { o.AskFolder = "" }, cl, cx, oc)
	if w := e.post("/v1/comparisons", directBody("")); w.Code != 503 || errCode(t, w) != CodeAskFolder {
		t.Errorf("no ask folder = %d %s", w.Code, w.Body)
	}
}

func TestDirectComparisonTooMany(t *testing.T) {
	cl, cx, oc := threeJudges()
	block := make(chan struct{})
	defer close(block)
	for _, j := range []*judgeAdapter{cl, cx, oc.judgeAdapter} {
		j.setBlock(block)
	}
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	for i := 0; i < compares.MaxUnfinished; i++ {
		if w := e.post("/v1/comparisons", directBody("")); w.Code != 202 {
			t.Fatalf("%d: %d %s", i, w.Code, w.Body)
		}
	}
	if w := e.post("/v1/comparisons", directBody("")); w.Code != 429 || errCode(t, w) != core.ErrCodeTooManyComparisons {
		t.Errorf("9th = %d %s", w.Code, w.Body)
	}
}

// Retry runs only the failed requests again, and not while the comparison
// runs. The judge's own message reaches the phone, never the log.
func TestRetryComparison(t *testing.T) {
	cl, cx, oc := threeJudges()
	block := make(chan struct{})
	for _, j := range []*judgeAdapter{cl, cx, oc.judgeAdapter} {
		j.setBlock(block)
	}
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	st := startRun(t, e, map[string]any{"mode": "ask", "prompt": "q", "compare": map[string]any{}})
	e.waitRun(t, st.ID)
	if w := e.post("/v1/comparisons/"+st.ComparisonID+"/retry", nil); w.Code != 409 || errCode(t, w) != CodeComparisonRunning {
		t.Fatalf("retry while running = %d %s", w.Code, w.Body)
	}
	oc.setFail("usage limit AI-MESSAGE-SECRET")
	close(block)
	c := e.waitComparison(t, st.ComparisonID)
	if c.Status != compares.Done {
		t.Fatalf("partly failed comparison = %+v", c.Comparison)
	}
	failed := 0
	for _, r := range c.Requests {
		if r.Judge != "opencode" {
			continue
		}
		failed++
		if r.Status != judge.RequestFailed || r.Error == nil || r.Error.Code != core.ErrCodeJudgeFailed || r.Error.Message != "usage limit AI-MESSAGE-SECRET" {
			t.Errorf("opencode request = %+v", r)
		}
	}
	if failed != 2 {
		t.Fatalf("opencode requests = %d", failed)
	}
	oc.setFail("")
	w := e.post("/v1/comparisons/"+st.ComparisonID+"/retry", nil)
	if w.Code != 202 {
		t.Fatalf("retry = %d %s", w.Code, w.Body)
	}
	if r := decodeBody[ComparisonStatus](t, w); r.Status != compares.Running || r.PollAfterMs != compares.PollMs {
		t.Errorf("retry 202 = %+v", r)
	}
	c = e.waitComparison(t, st.ComparisonID)
	if c.Status != compares.Done {
		t.Fatalf("after retry = %+v", c.Comparison)
	}
	for _, r := range c.Requests {
		if r.Status != judge.RequestDone {
			t.Errorf("request after retry = %+v", r)
		}
	}
	if a, b, o := len(cl.judgeCalls()), len(cx.judgeCalls()), len(oc.judgeCalls()); a != 2 || b != 2 || o != 4 {
		t.Errorf("judge calls claude %d, codex %d, opencode %d: only the failed requests run again", a, b, o)
	}
	// Nothing left to retry: the comparison comes back as it is.
	if w := e.post("/v1/comparisons/"+st.ComparisonID+"/retry", nil); w.Code != 202 || decodeBody[ComparisonStatus](t, w).Status != compares.Done {
		t.Errorf("retry of a done comparison = %d %s", w.Code, w.Body)
	}
	// Cancel of an ended comparison changes nothing.
	if w := e.post("/v1/comparisons/"+st.ComparisonID+"/cancel", nil); w.Code != 200 || decodeBody[ComparisonStatus](t, w).Status != compares.Done {
		t.Errorf("cancel of a done comparison = %d %s", w.Code, w.Body)
	}
	if logs := e.logs.String(); strings.Contains(logs, "AI-MESSAGE-SECRET") || strings.Contains(logs, "SECRET-ANSWER") {
		t.Errorf("log has the AI's words: %s", logs)
	}
}

// Cancel drops the queued requests (failed, cancelled) and asks no judge
// again; the requests that run finish.
func TestCancelComparison(t *testing.T) {
	cl, cx, oc := threeJudges()
	block := make(chan struct{})
	judges := []*judgeAdapter{cl, cx, oc.judgeAdapter}
	for _, j := range judges {
		j.setBlock(block)
	}
	e, _ := judgeEnv(t, func(o *Options) { o.Judge.PerAI = 1 }, cl, cx, oc)
	st := startRun(t, e, map[string]any{"mode": "ask", "prompt": "q", "compare": map[string]any{}})
	e.waitRun(t, st.ID)
	calls := func() int {
		n := 0
		for _, j := range judges {
			n += len(j.judgeCalls())
		}
		return n
	}
	deadline := time.Now().Add(5 * time.Second)
	for calls() < 3 { // one judge process per AI: three run, three wait
		if time.Now().After(deadline) {
			t.Fatalf("judge calls = %d", calls())
		}
		time.Sleep(5 * time.Millisecond)
	}
	w := e.post("/v1/comparisons/"+st.ComparisonID+"/cancel", nil)
	if w.Code != 200 {
		t.Fatalf("cancel = %d %s", w.Code, w.Body)
	}
	close(block)
	c := e.waitComparison(t, st.ComparisonID)
	done, cancelled := 0, 0
	for _, r := range c.Requests {
		switch {
		case r.Status == judge.RequestDone:
			done++
		case r.Error != nil && r.Error.Code == core.ErrCodeCancelled:
			cancelled++
		default:
			t.Errorf("request = %+v", r)
		}
	}
	if done != 3 || cancelled != 3 || calls() != 3 || c.Status != compares.Done {
		t.Errorf("done %d, cancelled %d, judge calls %d, status %s", done, cancelled, calls(), c.Status)
	}
}

// ---- combined answers (S5b) ---------------------------------------------------

// writerAdapter is a judgeAdapter that also writes combined answers, with
// fakecli's ok text that names every DIFFER side.
type writerAdapter struct {
	*judgeAdapter
	smu    sync.Mutex
	synths []core.SynthRequest
	sfail  string        // while set, Synthesize fails with judge_failed and this message
	sblock chan struct{} // while set, Synthesize waits for it to close (or for ctx)
}

func newWriterAdapter(name string, leaves bool) *writerAdapter {
	j := newJudge(name)
	j.leaves = leaves
	return &writerAdapter{judgeAdapter: j}
}

func (w *writerAdapter) AskTiers() map[string]core.AskModel {
	return map[string]core.AskModel{
		core.TierQuick:    {Model: w.name + "-quick"},
		core.TierStandard: {Model: w.name + "-std", Effort: "medium"},
		core.TierDeep:     {Model: w.name + "-deep", Effort: "high"},
	}
}

func (w *writerAdapter) DisplayName() string { return strings.ToUpper(w.name[:1]) + w.name[1:] }

func (w *writerAdapter) Synthesize(ctx context.Context, req core.SynthRequest) (core.SynthResult, error) {
	w.smu.Lock()
	w.synths = append(w.synths, req)
	n, block, fail := len(w.synths), w.sblock, w.sfail
	w.smu.Unlock()
	res := core.SynthResult{Model: req.Model, InputTokens: 40, OutputTokens: 9}
	if w.leaves {
		res.Cleanup = fmt.Sprintf("%s-synth-%d", w.name, n)
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return res, core.Fail(core.ErrCodeJudgeTimeout, "")
		}
	}
	w.smu.Lock()
	fail = w.sfail
	w.smu.Unlock()
	if fail != "" {
		return res, core.Fail(core.ErrCodeJudgeFailed, fail)
	}
	res.Output, _ = json.Marshal(map[string]string{"answer": fakecli.SynthText(req.Prompt)})
	return res, nil
}

func (w *writerAdapter) synthCalls() []core.SynthRequest {
	w.smu.Lock()
	defer w.smu.Unlock()
	return slices.Clone(w.synths)
}

func (w *writerAdapter) set(fail string, block chan struct{}) {
	w.smu.Lock()
	w.sfail, w.sblock = fail, block
	w.smu.Unlock()
}

// cleaningWriter leaves conversations behind, as OpenCode does.
type cleaningWriter struct{ *writerAdapter }

func (c cleaningWriter) CleanupJudge(ctx context.Context, folder string, refs []string) error {
	return cleaningAdapter{c.judgeAdapter}.CleanupJudge(ctx, folder, refs)
}

func threeWriterAdapters() (*writerAdapter, *writerAdapter, cleaningWriter) {
	return newWriterAdapter("claude", false), newWriterAdapter("codex", false), cleaningWriter{newWriterAdapter("opencode", true)}
}

func synthCallsOf(ws ...*writerAdapter) int {
	n := 0
	for _, w := range ws {
		n += len(w.synthCalls())
	}
	return n
}

// waitSynthesis polls like the phone until the comparison and its
// combined answer have both ended; pollAfterMs is set exactly while
// either waits or runs.
func (e env) waitSynthesis(t *testing.T, id string) ComparisonStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := e.comparison(t, id)
		if st.Synthesis == nil {
			t.Fatalf("no synthesis: %+v", st.Comparison)
		}
		busy := st.Status == compares.Waiting || st.Status == compares.Running ||
			st.Synthesis.Status == compares.SynthWaiting || st.Synthesis.Status == compares.SynthRunning
		if !busy {
			if st.PollAfterMs != 0 {
				t.Errorf("ended comparison with pollAfterMs %d", st.PollAfterMs)
			}
			return st
		}
		if st.PollAfterMs != compares.PollMs {
			t.Fatalf("%s comparison, %s synthesis with pollAfterMs %d", st.Status, st.Synthesis.Status, st.PollAfterMs)
		}
		if time.Now().After(deadline) {
			t.Fatalf("still %s / %s", st.Status, st.Synthesis.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (e env) postRaw(token, target, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", target, strings.NewReader(body))
	r.Host = "127.0.0.1:8790"
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func TestInfoAnnouncesSynthesize(t *testing.T) {
	cl, cx, oc := threeWriterAdapters()
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	resp := decodeBody[InfoResponse](t, e.do("GET", "/v1/info", nil))
	for _, a := range resp.Agents {
		if !contains(a.Capabilities, core.CapSynthesize) || !contains(a.Capabilities, core.CapJudge) {
			t.Errorf("%s = %v", a.Agent, a.Capabilities)
		}
	}
	if !contains(resp.Capabilities, core.CapSynthesize) || contains(resp.Reserved, core.CapSynthesize) {
		t.Errorf("capabilities %v, reserved %v", resp.Capabilities, resp.Reserved)
	}
	// AIs that judge but do not write, and judging off: reserved.
	jc, jx, jo := threeJudges()
	e2, _ := judgeEnv(t, nil, jc, jx, jo)
	resp = decodeBody[InfoResponse](t, e2.do("GET", "/v1/info", nil))
	if contains(resp.Capabilities, core.CapSynthesize) || !contains(resp.Reserved, core.CapSynthesize) {
		t.Errorf("judges only: capabilities %v, reserved %v", resp.Capabilities, resp.Reserved)
	}
	cl2, cx2, oc2 := threeWriterAdapters()
	e3, _ := judgeEnv(t, func(o *Options) { o.Judge = config.JudgeConfig{Off: true} }, cl2, cx2, oc2)
	resp = decodeBody[InfoResponse](t, e3.do("GET", "/v1/info", nil))
	if contains(resp.Capabilities, core.CapSynthesize) || !contains(resp.Reserved, core.CapSynthesize) {
		t.Errorf("off: capabilities %v, reserved %v", resp.Capabilities, resp.Reserved)
	}
}

func TestRunWithSynthesis(t *testing.T) {
	cl, cx, oc := threeWriterAdapters()
	cl.answer = "ANSWER-SECRET 값은 NUM 1입니다."
	cx.answer = "ANSWER-SECRET 값은 NUM 2입니다."
	oc.answer = "ANSWER-SECRET 값은 NUM 1입니다."
	block := make(chan struct{})
	cx.set("", block)
	e, ask := judgeEnv(t, nil, cl, cx, oc)
	st := startRun(t, e, map[string]any{
		"mode": "ask", "prompt": "QUESTION-SECRET 값은?", "tier": "deep",
		"compare": map[string]any{"judges": []string{"claude", "codex", "opencode"}, "synthesis": map[string]any{"agent": "codex", "later": 1}},
	})
	if st.CompareError != nil || st.ComparisonID == "" {
		t.Fatalf("202 = %+v", st)
	}
	// The judges are done while codex writes: status done, pollAfterMs on.
	deadline := time.Now().Add(10 * time.Second)
	for {
		c := e.comparison(t, st.ComparisonID)
		if c.Status == compares.Done && c.Synthesis != nil && c.Synthesis.Status == compares.SynthRunning && len(cx.synthCalls()) == 1 {
			if c.PollAfterMs != compares.PollMs || c.Synthesis.Agent != "codex" {
				t.Errorf("while writing: %d %+v", c.PollAfterMs, c.Synthesis)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never writing: %+v", c.Synthesis)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if w := e.post("/v1/comparisons/"+st.ComparisonID+"/retry", nil); w.Code != 409 || errCode(t, w) != CodeComparisonRunning {
		t.Errorf("retry while writing = %d %s", w.Code, w.Body)
	}
	close(block)
	c := e.waitSynthesis(t, st.ComparisonID)
	s := c.Synthesis
	named := strings.Contains(s.Text, "Claude, Opencode vs Codex") || strings.Contains(s.Text, "Codex vs Claude, Opencode")
	if s.Status != compares.SynthDone || s.Agent != "codex" || s.Model != "codex-deep" || !s.Judged || s.UnnamedSides != 0 || !named {
		t.Fatalf("synthesis = %+v", s)
	}
	calls := cx.synthCalls()
	if len(calls) != 1 || synthCallsOf(cl, oc.writerAdapter) != 0 {
		t.Fatalf("synth calls %d", len(calls))
	}
	if req := calls[0]; req.Folder != core.JudgeFolder(ask) || req.Model != "codex-deep" || req.Effort != "high" ||
		strings.Contains(req.Prompt, core.AskPreamble) || !strings.Contains(req.Prompt, `QUESTION: "QUESTION-SECRET 값은?"`) {
		t.Errorf("request = %+v", req)
	}
	e.idle(t, st.ComparisonID)
	logs := e.logs.String()
	for _, leak := range []string{"QUESTION-SECRET", "ANSWER-SECRET", "Fake combined", "값은"} {
		if strings.Contains(logs, leak) {
			t.Errorf("log contains %q", leak)
		}
	}
	for _, line := range []string{`"synthesis started"`, `"synthesis ended"`, `"synthesis":true`, `"writer":"codex"`} {
		if !strings.Contains(logs, line) {
			t.Errorf("log lacks %s", line)
		}
	}
}

// compare.synthesis: absent and off write nothing; a name of no AI here or
// a wrong type is bad_compare, and the ask runs all the same.
func TestRunSynthesisParse(t *testing.T) {
	cl, cx, oc := threeWriterAdapters()
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	for name, synth := range map[string]any{
		"string":        "claude",
		"unknown AI":    map[string]any{"agent": "gemini"},
		"agent number":  map[string]any{"agent": 5},
		"off string":    map[string]any{"off": "yes"},
		"agent as flag": map[string]any{"agent": "--claude"},
	} {
		st := startRun(t, e, map[string]any{"mode": "ask", "prompt": "q " + name, "compare": map[string]any{"synthesis": synth}})
		if st.CompareError == nil || st.CompareError.Code != CodeBadCompare || st.ComparisonID != "" || st.CompareError.Message != msgBadCompare {
			t.Errorf("%s: %+v", name, st)
		}
		e.waitRun(t, st.ID)
	}
	for name, c := range map[string]struct {
		compare map[string]any
		status  string
		writer  string
	}{
		"absent":  {map[string]any{}, compares.SynthOff, ""},
		"null":    {map[string]any{"synthesis": nil}, compares.SynthOff, ""},
		"off":     {map[string]any{"synthesis": map[string]any{"off": true, "agent": "codex"}}, compares.SynthOff, ""},
		"no AI":   {map[string]any{"synthesis": map[string]any{}}, compares.SynthDone, "claude"},
		"one AI":  {map[string]any{"synthesis": map[string]any{"agent": "opencode"}}, compares.SynthDone, "opencode"},
		"unknown": {map[string]any{"synthesis": map[string]any{"agent": "claude", "style": "short"}}, compares.SynthDone, "claude"},
	} {
		before := synthCallsOf(cl, cx, oc.writerAdapter)
		st := startRun(t, e, map[string]any{"mode": "ask", "prompt": "q " + name, "compare": c.compare})
		if st.CompareError != nil || st.ComparisonID == "" {
			t.Fatalf("%s: %+v", name, st)
		}
		got := e.waitSynthesis(t, st.ComparisonID)
		calls := synthCallsOf(cl, cx, oc.writerAdapter) - before
		if got.Synthesis.Status != c.status || (c.writer != "" && got.Synthesis.Agent != c.writer) || (c.status == compares.SynthOff) != (calls == 0) {
			t.Errorf("%s: %+v (%d calls)", name, got.Synthesis, calls)
		}
		e.idle(t, st.ComparisonID)
	}
}

func TestDirectComparisonSynthesis(t *testing.T) {
	cl, cx, oc := threeWriterAdapters()
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	for name, synth := range map[string]any{
		"unknown AI": map[string]any{"agent": "gemini"},
		"bad tier":   map[string]any{"tier": "fast"},
	} {
		body := directBody("")
		body["synthesis"] = synth
		w := e.post("/v1/comparisons", body)
		if w.Code != 400 || errCode(t, w) != CodeBadRequest || !strings.Contains(w.Body.String(), msgBadComparisonSynth) {
			t.Errorf("%s = %d %s", name, w.Code, w.Body)
		}
	}
	body := directBody("")
	body["synthesis"] = map[string]any{"agent": 5}
	if w := e.post("/v1/comparisons", body); w.Code != 400 || errCode(t, w) != CodeBadRequest {
		t.Errorf("wrong type = %d %s", w.Code, w.Body)
	}
	body = directBody("s-1")
	body["synthesis"] = map[string]any{"agent": "codex", "tier": "quick"}
	w := e.post("/v1/comparisons", body)
	if w.Code != 202 {
		t.Fatalf("POST = %d %s", w.Code, w.Body)
	}
	st := decodeBody[ComparisonStatus](t, w)
	if st.Synthesis == nil || st.Synthesis.Status != compares.SynthWaiting || st.PollAfterMs != compares.PollMs {
		t.Errorf("202 synthesis = %+v", st.Synthesis)
	}
	c := e.waitSynthesis(t, st.ID)
	if s := c.Synthesis; s.Status != compares.SynthDone || s.Agent != "codex" || s.Model != "codex-quick" || !strings.Contains(s.Text, "ChatGPT") {
		t.Errorf("synthesis = %+v", s)
	}
	if req := cx.synthCalls()[0]; !strings.Contains(req.Prompt, `"ai":"ChatGPT"`) || req.Effort != "" {
		t.Errorf("effort %q, prompt:\n%s", req.Effort, req.Prompt)
	}
}

func TestWriteSynthesis(t *testing.T) {
	cl, cx, oc := threeWriterAdapters()
	e, ask := judgeEnv(t, nil, cl, cx, oc)
	if w := e.postRaw(e.token, "/v1/comparisons/0123456789abcdef0123456789abcdef/synthesis", ""); w.Code != 404 || errCode(t, w) != CodeComparisonNotFound {
		t.Errorf("unknown = %d %s", w.Code, w.Body)
	}
	// While the judges run: 409.
	block := make(chan struct{})
	cl.setBlock(block)
	st := decodeBody[ComparisonStatus](t, e.post("/v1/comparisons", directBody("w-1")))
	if st.Synthesis == nil || st.Synthesis.Status != compares.SynthOff {
		t.Fatalf("absent synthesis = %+v", st.Synthesis)
	}
	target := "/v1/comparisons/" + st.ID + "/synthesis"
	if w := e.postRaw(e.token, target, ""); w.Code != 409 || errCode(t, w) != CodeComparisonRunning {
		t.Errorf("while judging = %d %s", w.Code, w.Body)
	}
	cl.setBlock(nil)
	close(block)
	c := e.waitSynthesis(t, st.ID)
	if c.Synthesis.Status != compares.SynthOff || synthCallsOf(cl, cx, oc.writerAdapter) != 0 {
		t.Fatalf("off: %+v", c.Synthesis)
	}
	for name, body := range map[string]string{
		"bad JSON":   "{",
		"unknown AI": `{"agent":"gemini"}`,
		"number":     `{"agent":7}`,
		"too big":    `{"agent":"claude","pad":"` + strings.Repeat("x", 5<<10) + `"}`,
	} {
		if w := e.postRaw(e.token, target, body); w.Code != 400 || errCode(t, w) != CodeBadRequest {
			t.Errorf("%s = %d %s", name, w.Code, w.Body)
		}
	}
	// Another device does not see it.
	if w := e.postRaw(e.addDevice(t, "tablet"), target, ""); w.Code != 404 {
		t.Errorf("another device = %d", w.Code)
	}
	// Write it with opencode, which leaves a conversation; hold it to see
	// 409 and pollAfterMs.
	hold := make(chan struct{})
	oc.set("", hold)
	w := e.postRaw(e.token, target, `{"agent":"opencode","later":true}`)
	if w.Code != 202 {
		t.Fatalf("write = %d %s", w.Code, w.Body)
	}
	if got := decodeBody[ComparisonStatus](t, w); got.Synthesis.Status != compares.SynthRunning || got.PollAfterMs != compares.PollMs || got.Status != compares.Done {
		t.Errorf("202 = %+v %d", got.Synthesis, got.PollAfterMs)
	}
	if w := e.postRaw(e.token, target, "{}"); w.Code != 409 {
		t.Errorf("while writing = %d %s", w.Code, w.Body)
	}
	close(hold)
	c = e.waitSynthesis(t, st.ID)
	if c.Synthesis.Status != compares.SynthDone || c.Synthesis.Agent != "opencode" || len(oc.synthCalls()) != 1 {
		t.Fatalf("written = %+v", c.Synthesis)
	}
	idle := e.idle(t, st.ID)
	oc.jmu.Lock()
	refs := slices.Clone(oc.refs)
	oc.jmu.Unlock()
	if !slices.ContainsFunc(refs, func(r []string) bool { return slices.Equal(r, []string{"opencode-synth-1"}) }) || !slices.Contains(oc.cleanups(), core.JudgeFolder(ask)) {
		t.Errorf("cleanups %v", refs)
	}
	// Done: the same snapshot again, no call.
	if w := e.postRaw(e.token, target, ""); w.Code != 202 || decodeBody[ComparisonStatus](t, w).Synthesis.Text != idle.Synthesis.Text || len(oc.synthCalls()) != 1 {
		t.Errorf("again = %d %s", w.Code, w.Body)
	}
	// A failed one is written again, with the AI asked for before.
	cx.set(fakecli.UsageLimitCodex, nil)
	body := directBody("w-2")
	body["synthesis"] = map[string]any{"agent": "codex"}
	st2 := decodeBody[ComparisonStatus](t, e.post("/v1/comparisons", body))
	c = e.waitSynthesis(t, st2.ID)
	if s := c.Synthesis; s.Status != compares.SynthFailed || s.Error == nil || s.Error.Code != core.ErrCodeJudgeFailed || s.Error.Message != fakecli.UsageLimitCodex {
		t.Fatalf("usage limit = %+v", s)
	}
	cx.set("", nil)
	if w := e.postRaw(e.token, "/v1/comparisons/"+st2.ID+"/synthesis", ""); w.Code != 202 {
		t.Fatalf("again = %d %s", w.Code, w.Body)
	}
	if s := e.waitSynthesis(t, st2.ID).Synthesis; s.Status != compares.SynthDone || s.Agent != "codex" || len(cx.synthCalls()) != 2 {
		t.Errorf("rewritten = %+v", s)
	}
}

// Cancel of an ended comparison whose combined answer runs: 200, and the
// writer is not asked again.
func TestCancelSynthesis(t *testing.T) {
	cl, cx, oc := threeWriterAdapters()
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	hold := make(chan struct{})
	cl.set("", hold)
	body := directBody("c-s")
	body["synthesis"] = map[string]any{"agent": "claude"}
	st := decodeBody[ComparisonStatus](t, e.post("/v1/comparisons", body))
	deadline := time.Now().Add(10 * time.Second)
	for len(cl.synthCalls()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("never writing")
		}
		time.Sleep(5 * time.Millisecond)
	}
	w := e.post("/v1/comparisons/"+st.ID+"/cancel", nil)
	if w.Code != 200 || decodeBody[ComparisonStatus](t, w).PollAfterMs != compares.PollMs {
		t.Errorf("cancel = %d %s", w.Code, w.Body)
	}
	cl.set(fakecli.UsageLimitClaude, hold)
	close(hold)
	if s := e.waitSynthesis(t, st.ID).Synthesis; s.Status != compares.SynthFailed || len(cl.synthCalls()) != 1 {
		t.Errorf("after cancel = %+v", s)
	}
}
