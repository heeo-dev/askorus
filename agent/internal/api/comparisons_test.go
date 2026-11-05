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
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// ---- fakes ----------------------------------------------------------------

// judgeFamilies are the companies of the fake AIs' models.
var judgeFamilies = map[string]string{"claude": "anthropic", "codex": "openai", "opencode": "deepseek"}

// judgeMins are the oldest versions the fake AIs' one-off command was
// "tested" with.
var judgeMins = map[string]string{"claude": "1.0.0", "codex": "1.0.0", "opencode": "1.0.0"}

// judgeAdapter is an AI that asks like askAdapter and names its models'
// company (core.Judger), so it passes the gate. It writes no combined
// answers (writerAdapter does). The name is the judging days'.
type judgeAdapter struct {
	askAdapter
	family  string
	version string        // "" keeps askAdapter's 1.0.0
	answer  string        // the answer to every ask; "" keeps askAdapter's
	askGate chan struct{} // when set, an ask waits for it to close

	jmu  sync.Mutex
	hold chan struct{} // while set, Family waits for it to close
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

// Family names the company of a model. A comparison asks it for each PC
// answer while it takes its answers, so a hold keeps the comparison
// unfinished.
func (a *judgeAdapter) Family(model string) string {
	a.jmu.Lock()
	hold := a.hold
	a.jmu.Unlock()
	if hold != nil {
		<-hold
	}
	if model == "" {
		return ""
	}
	return a.family
}

// holdFamily makes a's Family wait until release, which the test's end
// calls too, so that nothing is left waiting.
func holdFamily(t *testing.T, a *judgeAdapter) (release func()) {
	t.Helper()
	ch := make(chan struct{})
	a.jmu.Lock()
	a.hold = ch
	a.jmu.Unlock()
	release = sync.OnceFunc(func() { close(ch) })
	t.Cleanup(release)
	return release
}

// judgeEnv serves the adapters with an ask folder and the fake AIs'
// minimum versions, so they pass the gate.
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

// threeAIs are claude, codex and opencode, which pass the gate and write
// no combined answers.
func threeAIs() (*judgeAdapter, *judgeAdapter, *judgeAdapter) {
	return newJudge("claude"), newJudge("codex"), newJudge("opencode")
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
			t.Fatalf("comparison still %s", st.Status)
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

// infoOf is GET /v1/info and its body.
func infoOf(t *testing.T, e env) (InfoResponse, string) {
	t.Helper()
	w := e.do("GET", "/v1/info", nil)
	if w.Code != 200 {
		t.Fatalf("info = %d %s", w.Code, w.Body)
	}
	return decodeBody[InfoResponse](t, w), w.Body.String()
}

// noJudging fails when an info body has judging, the capability judge or
// an AI's judge (all gone since C6d, #24), or integrations other than []
// (N1).
func noJudging(t *testing.T, body string) {
	t.Helper()
	if strings.Contains(body, `"judging"`) || strings.Contains(body, `"judge"`) {
		t.Errorf("info has judging: %s", body)
	}
	if !strings.Contains(body, `"integrations":[]`) {
		t.Errorf("integrations missing: %s", body)
	}
}

// An AI that passes the gate and writes announces synthesize; one that
// passes it but does not write leaves it reserved. No AI announces judge.
func TestInfoAnnouncesSynthesize(t *testing.T) {
	cl, cx, oc := threeWriterAdapters()
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	resp, body := infoOf(t, e)
	for _, a := range resp.Agents {
		if !contains(a.Capabilities, core.CapSynthesize) || !contains(a.Capabilities, core.CapAsk) || a.Ask == nil {
			t.Errorf("%s = %+v", a.Agent, a)
		}
	}
	if !contains(resp.Capabilities, core.CapSynthesize) || contains(resp.Reserved, core.CapSynthesize) {
		t.Errorf("capabilities %v, reserved %v", resp.Capabilities, resp.Reserved)
	}
	noJudging(t, body)
	// AIs that pass the gate but do not write: reserved.
	jc, jx, jo := threeAIs()
	e2, _ := judgeEnv(t, nil, jc, jx, jo)
	resp, body = infoOf(t, e2)
	if contains(resp.Capabilities, core.CapSynthesize) || !contains(resp.Reserved, core.CapSynthesize) {
		t.Errorf("no writers: capabilities %v, reserved %v", resp.Capabilities, resp.Reserved)
	}
	noJudging(t, body)
}

// familylessWriter writes combined answers and asks, but cannot name its
// models' companies (no core.Judger), so it never passes the gate.
type familylessWriter struct{ *askAdapter }

func (familylessWriter) Synthesize(context.Context, core.SynthRequest) (core.SynthResult, error) {
	return core.SynthResult{}, core.Fail(core.ErrCodeInternal, "")
}

// An AI writes only when the gate lets it: judge.off, an old or unknown
// CLI version, no ask folder, or an adapter that cannot name its models'
// companies leave synthesize reserved.
func TestInfoSynthesizeGated(t *testing.T) {
	none := func(t *testing.T, e env) {
		t.Helper()
		resp, body := infoOf(t, e)
		for _, a := range resp.Agents {
			if contains(a.Capabilities, core.CapSynthesize) {
				t.Errorf("%s writes: %+v", a.Agent, a)
			}
		}
		if !contains(resp.Reserved, core.CapSynthesize) || contains(resp.Capabilities, core.CapSynthesize) {
			t.Errorf("info = %s", body)
		}
		noJudging(t, body)
	}
	t.Run("old version", func(t *testing.T) {
		cl, cx, oc := threeWriterAdapters()
		cl.version = "0.9.9"
		e, _ := judgeEnv(t, nil, cl, cx, oc)
		resp, _ := infoOf(t, e)
		if contains(resp.Agents[0].Capabilities, core.CapSynthesize) || !contains(resp.Agents[1].Capabilities, core.CapSynthesize) ||
			!contains(resp.Capabilities, core.CapSynthesize) {
			t.Errorf("agents = %+v", resp.Agents)
		}
	})
	t.Run("all old", func(t *testing.T) {
		cl, cx, oc := threeWriterAdapters()
		cl.version, cx.version, oc.version = "0.1.0", "0.1.0", "garbled"
		e, _ := judgeEnv(t, nil, cl, cx, oc)
		none(t, e)
	})
	t.Run("off", func(t *testing.T) {
		cl, cx, oc := threeWriterAdapters()
		e, _ := judgeEnv(t, func(o *Options) { o.Judge.Off = true }, cl, cx, oc)
		none(t, e)
	})
	t.Run("no minimum versions", func(t *testing.T) {
		cl, cx, oc := threeWriterAdapters()
		e, _ := judgeEnv(t, func(o *Options) { o.JudgeMinVersions = nil }, cl, cx, oc)
		none(t, e)
	})
	t.Run("no ask folder", func(t *testing.T) {
		cl, cx, oc := threeWriterAdapters()
		e, _ := judgeEnv(t, func(o *Options) { o.AskFolder = "" }, cl, cx, oc)
		none(t, e)
	})
	t.Run("relative ask folder", func(t *testing.T) {
		cl, cx, oc := threeWriterAdapters()
		e, _ := judgeEnv(t, func(o *Options) { o.AskFolder = "ask" }, cl, cx, oc)
		none(t, e)
	})
	t.Run("cannot name companies", func(t *testing.T) {
		e, _ := judgeEnv(t, nil, familylessWriter{&askAdapter{name: "claude", available: true}}, familylessWriter{&askAdapter{name: "codex", available: true}})
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

// A run's comparison waits for the run's last answer, then takes the
// answers and is done at once. Its combined answer is off unless
// compare.synthesis asks for it.
func TestRunWithCompare(t *testing.T) {
	cl, cx, oc := threeAIs()
	cl.answer = "ANSWER-SECRET 값은 NUM 1입니다."
	cx.answer = "ANSWER-SECRET 값은 NUM 2입니다."
	oc.answer = "ANSWER-SECRET 값은 NUM 1입니다."
	cx.askGate = make(chan struct{}) // codex answers after the 202
	e, _ := judgeEnv(t, nil, cl, cx, oc)
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
	// The answers in the turn's order, each with its model's company.
	var got []string
	for _, a := range c.Answers {
		if a.ID != a.Agent || a.Via != judge.ViaPC || a.Status != judge.AnswerReady || a.Model != a.Agent+"-std" {
			t.Errorf("answer = %+v", a)
		}
		got = append(got, a.Agent+" "+a.Family)
	}
	if want := []string{"claude anthropic", "codex openai", "opencode deepseek"}; !slices.Equal(got, want) {
		t.Errorf("answers %v, want %v", got, want)
	}
	// The judging's lists stay, empty (principle 4); no text leaves the
	// agent, and no combined answer was asked for.
	body := e.do("GET", "/v1/comparisons/"+st.ComparisonID, nil).Body.String()
	for _, part := range []string{`"requests":[]`, `"judgments":[]`, `"items":[]`, `"units":[]`, `"synthesis":{"status":"off","judged":false}`} {
		if !strings.Contains(body, part) {
			t.Errorf("comparison lacks %s: %s", part, body)
		}
	}
	if strings.Contains(body, "SECRET") {
		t.Errorf("comparison has the texts: %s", body)
	}
	e.idle(t, st.ComparisonID)
	logs := e.logs.String()
	for _, leak := range []string{"QUESTION-SECRET", "ANSWER-SECRET", "EARLIER-SECRET", "값은"} {
		if strings.Contains(logs, leak) {
			t.Errorf("log contains %q: %s", leak, logs)
		}
	}
	for _, line := range []string{`"comparison started"`, `"comparison ended"`, `"compare":true`} {
		if !strings.Contains(logs, line) {
			t.Errorf("log lacks %s: %s", line, logs)
		}
	}
}

// A compare the agent cannot read gives compareError bad_compare in the
// 202, and the ask runs all the same: never a 400 or 413 for compare.
func TestRunBadCompareStillAsks(t *testing.T) {
	cl, cx, oc := threeAIs()
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
	if c := e.waitComparison(t, st.ComparisonID); c.Status != compares.Done || len(c.Answers) != 3 {
		t.Errorf("at the limits: %+v", c.Comparison)
	}
}

// One asked AI has nothing to compare with: compareError need_two_answers.
// judges, the AIs the earlier questions went to, are cut to the asked AIs,
// and none of them left means every asked AI: a writer gets the earlier
// questions only when it is one of them. An AI that was not asked never
// writes (결정 5).
func TestRunCompareJudgesAndOneAI(t *testing.T) {
	cl, cx, oc := threeWriterAdapters()
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	st := startRun(t, e, map[string]any{"mode": "ask", "prompt": "q", "agents": []map[string]string{{"agent": "claude"}}, "compare": map[string]any{}})
	if st.CompareError == nil || st.CompareError.Code != core.ErrCodeNeedTwoAnswers || st.ComparisonID != "" {
		t.Fatalf("one AI: %+v", st)
	}
	if done := e.waitRun(t, st.ID); done.Status != "done" {
		t.Fatalf("run = %+v", done)
	}
	writers := map[string]*writerAdapter{"claude": cl, "codex": cx, "opencode": oc.writerAdapter}
	two := []map[string]string{{"agent": "claude"}, {"agent": "codex"}}
	for _, c := range []struct {
		name    string
		judges  []string
		agent   string // compare.synthesis.agent
		writer  string
		earlier bool
	}{
		{"no judge asked", []string{"opencode"}, "codex", "codex", true},
		{"writer no judge", []string{"claude"}, "codex", "codex", false},
		{"writer not asked", []string{"opencode", "claude"}, "opencode", "claude", true},
	} {
		st := startRun(t, e, map[string]any{"mode": "ask", "prompt": "q", "agents": two, "compare": map[string]any{
			"earlier": []string{"EARLIER-Q"}, "judges": c.judges, "synthesis": map[string]any{"agent": c.agent},
		}})
		if st.CompareError != nil || st.ComparisonID == "" {
			t.Fatalf("%s: %+v", c.name, st)
		}
		got := e.waitSynthesis(t, st.ComparisonID)
		if got.Status != compares.Done || got.Synthesis.Status != compares.SynthDone || got.Synthesis.Agent != c.writer {
			t.Errorf("%s: %s, synthesis %+v", c.name, got.Status, got.Synthesis)
		}
		calls := writers[c.writer].synthCalls()
		if len(calls) == 0 {
			t.Fatalf("%s: %s never wrote", c.name, c.writer)
		}
		sent := strings.Contains(calls[len(calls)-1].Prompt, `EARLIER QUESTIONS (context only, do not answer them): ["EARLIER-Q"]`)
		if sent != c.earlier {
			t.Errorf("%s: earlier questions sent %v, want %v", c.name, sent, c.earlier)
		}
		e.idle(t, st.ComparisonID)
	}
	if n := len(oc.synthCalls()); n != 0 {
		t.Errorf("opencode, never asked, wrote %d times", n)
	}
}

// The longest body: a 32 KiB question and three 4 KiB earlier questions,
// all quotes, which JSON doubles. It is over the old limit and accepted.
func TestRunCompareLongestBody(t *testing.T) {
	cl, cx, oc := threeAIs()
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
	if c := e.waitComparison(t, st.ComparisonID); c.Status != compares.Done {
		t.Errorf("comparison = %+v", c.Comparison)
	}
}

// At 8 unfinished comparisons the 9th run gets too_many_comparisons, and
// its ask runs all the same. A re-sent run still finds its comparison.
func TestRunCompareTooMany(t *testing.T) {
	cl, cx, oc := threeAIs()
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	release := holdFamily(t, cl) // every comparison stays unfinished, taking its answers
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
	release()
	for _, id := range ids {
		if c := e.waitComparison(t, id); c.Status != compares.Done {
			t.Errorf("%s = %s", id, c.Status)
		}
	}
}

// ---- the comparison routes ---------------------------------------------

func TestComparisonBelongsToItsDevice(t *testing.T) {
	cl, cx, oc := threeAIs()
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
	cl, cx, oc := threeAIs()
	e, _ := judgeEnv(t, nil, cl, cx, oc)
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
	if c.Status != compares.Done || len(c.Answers) != 3 || c.Error != nil {
		t.Fatalf("comparison = %+v", c.Comparison)
	}
	// A PC answer's company is its adapter's, a pasted one's its app's.
	if c.Answers[0].Family != "anthropic" || c.Answers[1].Family != "openai" || c.Answers[2].Family != "openai" ||
		c.Answers[2].ID != "gpt" || c.Answers[2].Via != judge.ViaHandoff {
		t.Errorf("answers = %+v", c.Answers)
	}
	if c.Synthesis == nil || c.Synthesis.Status != compares.SynthOff {
		t.Errorf("synthesis = %+v", c.Synthesis)
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
	cl, cx, oc := threeAIs()
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
	logs := e.logs.String()
	if strings.Contains(logs, `"comparison started"`) {
		t.Errorf("a refused request made a comparison: %s", logs)
	}
	if strings.Contains(logs, "SECRET") {
		t.Errorf("log has content: %s", logs)
	}
}

// No AI may write combined answers on the PC (judge.off, or every AI older
// than its one-off command): 503 no_judge. Without an ask folder there is
// no judge folder to write in: 503 ask_folder_unavailable.
func TestDirectComparisonNeedsWriterAndFolder(t *testing.T) {
	cl, cx, oc := threeWriterAdapters()
	e, _ := judgeEnv(t, func(o *Options) { o.Judge.Off = true }, cl, cx, oc)
	if w := e.post("/v1/comparisons", directBody("")); w.Code != 503 || errCode(t, w) != core.ErrCodeNoJudge {
		t.Errorf("judge.off = %d %s", w.Code, w.Body)
	}
	e, _ = judgeEnv(t, func(o *Options) {
		o.JudgeMinVersions = map[string]string{"claude": "9.0.0", "codex": "9.0.0", "opencode": "9.0.0"}
	}, cl, cx, oc)
	if w := e.post("/v1/comparisons", directBody("")); w.Code != 503 || errCode(t, w) != core.ErrCodeNoJudge {
		t.Errorf("every AI too old = %d %s", w.Code, w.Body)
	}
	e, _ = judgeEnv(t, func(o *Options) { o.AskFolder = "" }, cl, cx, oc)
	if w := e.post("/v1/comparisons", directBody("")); w.Code != 503 || errCode(t, w) != CodeAskFolder {
		t.Errorf("no ask folder = %d %s", w.Code, w.Body)
	}
}

func TestDirectComparisonTooMany(t *testing.T) {
	cl, cx, oc := threeAIs()
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	holdFamily(t, cl) // every comparison stays running, taking its answers
	for i := 0; i < compares.MaxUnfinished; i++ {
		if w := e.post("/v1/comparisons", directBody("")); w.Code != 202 {
			t.Fatalf("%d: %d %s", i, w.Code, w.Body)
		}
	}
	if w := e.post("/v1/comparisons", directBody("")); w.Code != 429 || errCode(t, w) != core.ErrCodeTooManyComparisons {
		t.Errorf("9th = %d %s", w.Code, w.Body)
	}
}

// Retry starts over a comparison that never took its answers, here one
// cancelled while it waited for its run, with its combined answer; not
// while it waits or runs. One that took its answers, done or failed, has
// nothing to retry and comes back as it is.
func TestRetryComparison(t *testing.T) {
	cl, cx, oc := threeWriterAdapters()
	cx.askGate = make(chan struct{})
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	st := startRun(t, e, map[string]any{"mode": "ask", "prompt": "q", "compare": map[string]any{"synthesis": map[string]any{"agent": "codex"}}})
	target := "/v1/comparisons/" + st.ComparisonID
	if w := e.post(target+"/retry", nil); w.Code != 409 || errCode(t, w) != CodeComparisonRunning {
		t.Fatalf("retry while waiting = %d %s", w.Code, w.Body)
	}
	if w := e.post(target+"/cancel", nil); w.Code != 200 || decodeBody[ComparisonStatus](t, w).Status != compares.Failed {
		t.Fatalf("cancel = %d %s", w.Code, w.Body)
	}
	close(cx.askGate)
	e.waitRun(t, st.ID)
	w := e.post(target+"/retry", nil)
	if w.Code != 202 {
		t.Fatalf("retry = %d %s", w.Code, w.Body)
	}
	// It finds its run ended and takes the answers at once.
	if r := decodeBody[ComparisonStatus](t, w); r.Status == compares.Failed || r.Error != nil || r.Synthesis.Error != nil {
		t.Errorf("retry 202 = %+v %+v", r.Comparison, r.Synthesis)
	}
	c := e.waitSynthesis(t, st.ComparisonID)
	if c.Status != compares.Done || len(c.Answers) != 3 || c.Synthesis.Status != compares.SynthDone || c.Synthesis.Agent != "codex" || len(cx.synthCalls()) != 1 {
		t.Fatalf("after retry = %+v %+v", c.Comparison, c.Synthesis)
	}
	e.idle(t, st.ComparisonID)
	// Nothing left to retry: the comparison comes back as it is, and no AI
	// writes again.
	w = e.post(target+"/retry", nil)
	if r := decodeBody[ComparisonStatus](t, w); w.Code != 202 || r.Status != compares.Done || r.PollAfterMs != 0 ||
		r.Synthesis.Text != c.Synthesis.Text || len(cx.synthCalls()) != 1 {
		t.Errorf("retry of a done comparison = %d %s", w.Code, w.Body)
	}
	// Cancel of an ended comparison changes nothing.
	if w := e.post(target+"/cancel", nil); w.Code != 200 || decodeBody[ComparisonStatus](t, w).Status != compares.Done {
		t.Errorf("cancel of a done comparison = %d %s", w.Code, w.Body)
	}
	// Every AI failed: the comparison took its answers, fewer than two, and
	// stays failed.
	st = startRun(t, e, map[string]any{"mode": "ask", "prompt": "FAIL q", "compare": map[string]any{}})
	c = e.waitComparison(t, st.ComparisonID)
	if c.Status != compares.Failed || c.Error == nil || c.Error.Code != core.ErrCodeNeedTwoAnswers || len(c.Answers) != 3 {
		t.Fatalf("no answers = %+v", c.Comparison)
	}
	w = e.post("/v1/comparisons/"+st.ComparisonID+"/retry", nil)
	if r := decodeBody[ComparisonStatus](t, w); w.Code != 202 || r.Status != compares.Failed || r.Error == nil || r.Error.Code != core.ErrCodeNeedTwoAnswers {
		t.Errorf("retry of a failed comparison = %d %s", w.Code, w.Body)
	}
}

// Cancel ends a comparison that waits for its run at once, failed with
// cancelled, and it never starts, also once the run has ended. One that
// takes its answers ends failed with cancelled too. Either way its
// combined answer fails with cancelled and no AI writes.
func TestCancelComparison(t *testing.T) {
	cl, cx, oc := threeWriterAdapters()
	cx.askGate = make(chan struct{})
	e, _ := judgeEnv(t, nil, cl, cx, oc)
	cancelled := func(name string, c ComparisonStatus) {
		t.Helper()
		if c.Status != compares.Failed || c.Error == nil || c.Error.Code != core.ErrCodeCancelled || c.PollAfterMs != 0 ||
			c.Synthesis.Status != compares.SynthFailed || c.Synthesis.Error == nil || c.Synthesis.Error.Code != core.ErrCodeCancelled {
			t.Errorf("%s = %+v %+v", name, c.Comparison, c.Synthesis)
		}
	}
	st := startRun(t, e, map[string]any{"mode": "ask", "prompt": "q", "compare": map[string]any{"synthesis": map[string]any{}}})
	w := e.post("/v1/comparisons/"+st.ComparisonID+"/cancel", nil)
	if w.Code != 200 {
		t.Fatalf("cancel = %d %s", w.Code, w.Body)
	}
	cancelled("cancel while waiting", decodeBody[ComparisonStatus](t, w))
	close(cx.askGate)
	e.waitRun(t, st.ID)
	e.idle(t, st.ComparisonID)
	c := e.comparison(t, st.ComparisonID)
	cancelled("after the run", c)
	if len(c.Answers) != 0 {
		t.Errorf("answers taken after the cancel: %+v", c.Answers)
	}

	// A comparison that takes its answers: the 200 shows it still running.
	release := holdFamily(t, cl.judgeAdapter)
	body := directBody("")
	body["synthesis"] = map[string]any{}
	st2 := decodeBody[ComparisonStatus](t, e.post("/v1/comparisons", body))
	w = e.post("/v1/comparisons/"+st2.ID+"/cancel", nil)
	if got := decodeBody[ComparisonStatus](t, w); w.Code != 200 || got.Status != compares.Running || got.PollAfterMs != compares.PollMs ||
		got.Synthesis.Status != compares.SynthFailed {
		t.Errorf("cancel while running = %d %s", w.Code, w.Body)
	}
	release()
	cancelled("after taking its answers", e.waitComparison(t, st2.ID))
	e.idle(t, st2.ID)
	if n := synthCallsOf(cl, cx, oc.writerAdapter); n != 0 {
		t.Errorf("%d synth calls after cancels", n)
	}
}

// ---- combined answers (S5b) ---------------------------------------------------

// writerAdapter is a judgeAdapter that also writes combined answers, with
// fakecli's ok text, fakecli.SynthLead.
type writerAdapter struct {
	*judgeAdapter
	leaves bool // Synthesize returns a cleanup ref (cleaningWriter)

	smu     sync.Mutex
	synths  []core.SynthRequest
	sfail   string        // while set, Synthesize fails with judge_failed and this message
	sblock  chan struct{} // while set, Synthesize waits for it to close (or for ctx)
	cleaned []string      // the folders CleanupJudge was called with
	refs    [][]string    // and the refs
}

func newWriterAdapter(name string, leaves bool) *writerAdapter {
	return &writerAdapter{judgeAdapter: newJudge(name), leaves: leaves}
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
	res.Output, _ = json.Marshal(map[string]string{"answer": fakecli.SynthLead})
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

// cleanups are the folders and the refs CleanupJudge was called with.
func (w *writerAdapter) cleanups() ([]string, [][]string) {
	w.smu.Lock()
	defer w.smu.Unlock()
	return slices.Clone(w.cleaned), slices.Clone(w.refs)
}

// cleaningWriter leaves conversations behind, as OpenCode does.
type cleaningWriter struct{ *writerAdapter }

func (c cleaningWriter) CleanupJudge(_ context.Context, folder string, refs []string) error {
	c.smu.Lock()
	defer c.smu.Unlock()
	c.cleaned = append(c.cleaned, folder)
	c.refs = append(c.refs, slices.Clone(refs))
	return nil
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

func TestRunWithSynthesis(t *testing.T) {
	cl, cx, oc := threeWriterAdapters()
	cl.answer = "ANSWER-SECRET 값은 NUM 1입니다."
	cx.answer = "ANSWER-SECRET 값은 NUM 2입니다."
	oc.answer = "ANSWER-SECRET 값은 NUM 3입니다."
	block := make(chan struct{})
	cx.set("", block)
	e, ask := judgeEnv(t, nil, cl, cx, oc)
	st := startRun(t, e, map[string]any{
		"mode": "ask", "prompt": "QUESTION-SECRET 값은?", "tier": "deep",
		"compare": map[string]any{
			"earlier": []string{"EARLIER-SECRET 처음 질문"}, "judges": []string{"claude", "codex", "opencode"},
			"synthesis": map[string]any{"agent": "codex", "later": 1},
		},
	})
	if st.CompareError != nil || st.ComparisonID == "" {
		t.Fatalf("202 = %+v", st)
	}
	// The comparison is done while codex writes: status done, pollAfterMs
	// on.
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
	if s := c.Synthesis; s.Status != compares.SynthDone || s.Agent != "codex" || s.Model != "codex-deep" || s.Text != fakecli.SynthLead || s.Judged {
		t.Fatalf("synthesis = %+v", s)
	}
	// Judged is always false; unnamedSides and topics went with the judging.
	if body := e.do("GET", "/v1/comparisons/"+st.ComparisonID, nil).Body.String(); !strings.Contains(body, `"judged":false`) ||
		strings.Contains(body, "unnamedSides") || strings.Contains(body, `"topics"`) {
		t.Errorf("comparison = %s", body)
	}
	calls := cx.synthCalls()
	if len(calls) != 1 || synthCallsOf(cl, oc.writerAdapter) != 0 {
		t.Fatalf("synth calls %d", len(calls))
	}
	req := calls[0]
	if req.Folder != core.JudgeFolder(ask) || req.Model != "codex-deep" || req.Effort != "high" || strings.Contains(req.Prompt, core.AskPreamble) {
		t.Errorf("request = %+v", req)
	}
	// synth-v5: the question as the person wrote it, the earlier questions
	// and each answer with its AI's name, and nothing of the judging.
	if !strings.HasPrefix(req.Prompt, "[Asksemble synthesis v5] ") {
		t.Errorf("prompt is not synth-v5:\n%s", req.Prompt)
	}
	for _, line := range []string{
		`QUESTION: "QUESTION-SECRET 값은?"`,
		`EARLIER QUESTIONS (context only, do not answer them): ["EARLIER-SECRET 처음 질문"]`,
		`ANSWER: {"ai":"Claude","text":"ANSWER-SECRET 값은 NUM 1입니다."}`,
		`ANSWER: {"ai":"Codex","text":"ANSWER-SECRET 값은 NUM 2입니다."}`,
		`ANSWER: {"ai":"Opencode","text":"ANSWER-SECRET 값은 NUM 3입니다."}`,
	} {
		if !strings.Contains(req.Prompt, "\n"+line+"\n") {
			t.Errorf("prompt lacks %s:\n%s", line, req.Prompt)
		}
	}
	for _, gone := range []string{"\nDIFFER: ", "\nCHECK: ", "\nCOMPARED.", "\nNOT COMPARED."} {
		if strings.Contains(req.Prompt, gone) {
			t.Errorf("prompt has %q:\n%s", gone, req.Prompt)
		}
	}
	e.idle(t, st.ComparisonID)
	logs := e.logs.String()
	for _, leak := range []string{"QUESTION-SECRET", "ANSWER-SECRET", "EARLIER-SECRET", "Fake combined", "값은"} {
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
	if s := c.Synthesis; s.Status != compares.SynthDone || s.Agent != "codex" || s.Model != "codex-quick" || s.Text != fakecli.SynthLead {
		t.Errorf("synthesis = %+v", s)
	}
	// The question trimmed, the earlier questions (codex is one of the AIs
	// that may write, as no judges were named) and the pasted answer under
	// its app's name.
	req := cx.synthCalls()[0]
	for _, part := range []string{
		"\n" + `QUESTION: "QUESTION-SECRET 몇 개인가요?"` + "\n",
		"\n" + `EARLIER QUESTIONS (context only, do not answer them): ["EARLIER-SECRET"]` + "\n",
		`"ai":"ChatGPT"`,
	} {
		if !strings.Contains(req.Prompt, part) {
			t.Errorf("prompt lacks %s:\n%s", part, req.Prompt)
		}
	}
	if req.Effort != "" {
		t.Errorf("effort %q", req.Effort)
	}
}

func TestWriteSynthesis(t *testing.T) {
	cl, cx, oc := threeWriterAdapters()
	e, ask := judgeEnv(t, nil, cl, cx, oc)
	if w := e.postRaw(e.token, "/v1/comparisons/0123456789abcdef0123456789abcdef/synthesis", ""); w.Code != 404 || errCode(t, w) != CodeComparisonNotFound {
		t.Errorf("unknown = %d %s", w.Code, w.Body)
	}
	// While the comparison takes its answers: 409.
	release := holdFamily(t, cl.judgeAdapter)
	st := decodeBody[ComparisonStatus](t, e.post("/v1/comparisons", directBody("w-1")))
	if st.Synthesis == nil || st.Synthesis.Status != compares.SynthOff {
		t.Fatalf("absent synthesis = %+v", st.Synthesis)
	}
	target := "/v1/comparisons/" + st.ID + "/synthesis"
	if w := e.postRaw(e.token, target, ""); w.Code != 409 || errCode(t, w) != CodeComparisonRunning {
		t.Errorf("while taking its answers = %d %s", w.Code, w.Body)
	}
	release()
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
	folders, refs := oc.cleanups()
	if !slices.ContainsFunc(refs, func(r []string) bool { return slices.Equal(r, []string{"opencode-synth-1"}) }) || !slices.Contains(folders, core.JudgeFolder(ask)) {
		t.Errorf("cleanups %v in %v", refs, folders)
	}
	// Done: the same snapshot again, no call.
	if w := e.postRaw(e.token, target, ""); w.Code != 202 || decodeBody[ComparisonStatus](t, w).Synthesis.Text != idle.Synthesis.Text || len(oc.synthCalls()) != 1 {
		t.Errorf("again = %d %s", w.Code, w.Body)
	}
	// A failed one is written again, with the AI asked for before. The
	// writer's own words reach the phone, never the log.
	cx.set(fakecli.UsageLimitCodex, nil)
	body := directBody("w-2")
	body["synthesis"] = map[string]any{"agent": "codex"}
	st2 := decodeBody[ComparisonStatus](t, e.post("/v1/comparisons", body))
	c = e.waitSynthesis(t, st2.ID)
	if s := c.Synthesis; s.Status != compares.SynthFailed || s.Error == nil || s.Error.Code != core.ErrCodeJudgeFailed || s.Error.Message != fakecli.UsageLimitCodex {
		t.Fatalf("usage limit = %+v", s)
	}
	if strings.Contains(e.logs.String(), fakecli.UsageLimitCodex) {
		t.Errorf("log has the writer's words: %s", e.logs)
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
