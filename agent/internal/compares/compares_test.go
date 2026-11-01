package compares

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
	"github.com/asksemble/asksemble/agent/internal/logx"
	"github.com/asksemble/asksemble/agent/internal/runs"
)

// ---- fakes ----------------------------------------------------------------

// fakeAI is an AI program that judges in process: by default the rule
// judge of fakecli.Answer, as the fake CLIs answer.
type fakeAI struct {
	name   string
	family string            // the company of every model it runs
	fams   map[string]string // models of another company, or "" for one it cannot tell
	def    core.AskModel

	mu      sync.Mutex
	version string
	avail   bool
	caps    []core.Capability
	// judge replaces the rule judge; n counts this AI's calls from 1.
	judge    func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error)
	calls    []core.JudgeRequest
	running  int
	peak     int
	load     *loadStats // shared by the AIs of a test, for the peak of all
	cleanups [][]string
	// cleanup replaces CleanupJudge's result.
	cleanup func(ctx context.Context, folder string, refs []string) error
}

type loadStats struct {
	mu            sync.Mutex
	running, peak int
}

func newFake(name, family string) *fakeAI {
	return &fakeAI{name: name, family: family, def: core.AskModel{Model: name + "-judge", Effort: "medium"},
		version: "1.0.0", avail: true, caps: []core.Capability{}}
}

func (f *fakeAI) Agent() string { return f.name }

func (f *fakeAI) Info(context.Context) core.AdapterInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return core.AdapterInfo{Agent: f.name, Version: f.version, Available: f.avail, Capabilities: f.caps}
}

func (f *fakeAI) ListSessions(context.Context) (core.ListResult, error) {
	return core.ListResult{}, nil
}
func (f *fakeAI) JudgeDefault() core.AskModel { return f.def }

func (f *fakeAI) Family(model string) string {
	if fam, ok := f.fams[model]; ok {
		return fam
	}
	if model == "" {
		return ""
	}
	return f.family
}

func (f *fakeAI) setAvailable(ok bool) {
	f.mu.Lock()
	f.avail = ok
	f.mu.Unlock()
}

func (f *fakeAI) setJudge(j func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error)) {
	f.mu.Lock()
	f.judge = j
	f.mu.Unlock()
}

func (f *fakeAI) Judge(ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	n, j := len(f.calls), f.judge
	f.running++
	f.peak = max(f.peak, f.running)
	f.mu.Unlock()
	if f.load != nil {
		f.load.mu.Lock()
		f.load.running++
		f.load.peak = max(f.load.peak, f.load.running)
		f.load.mu.Unlock()
	}
	defer func() {
		f.mu.Lock()
		f.running--
		f.mu.Unlock()
		if f.load != nil {
			f.load.mu.Lock()
			f.load.running--
			f.load.mu.Unlock()
		}
	}()
	if j != nil {
		return j(ctx, req, n)
	}
	return ruleJudge(req), nil
}

// ruleJudge is fakecli's rule judge with the model asked for.
func ruleJudge(req core.JudgeRequest) core.JudgeResult {
	return core.JudgeResult{Output: json.RawMessage(fakecli.Answer(req.Prompt, false)), Model: req.Model, InputTokens: 10, OutputTokens: 2}
}

func (f *fakeAI) judgeCalls() []core.JudgeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeAI) peakRunning() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.peak
}

// cleanerAI is a fakeAI whose judging leaves something behind, as
// OpenCode's does.
type cleanerAI struct{ *fakeAI }

func (c cleanerAI) CleanupJudge(ctx context.Context, folder string, refs []string) error {
	c.mu.Lock()
	c.cleanups = append(c.cleanups, slices.Clone(refs))
	hook := c.cleanup
	c.mu.Unlock()
	if hook != nil {
		return hook(ctx, folder, refs)
	}
	return nil
}

func (c cleanerAI) cleanupCalls() [][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.cleanups)
}

// askerAI is a fakeAI that also answers questions.
type askerAI struct {
	*fakeAI
	tiers map[string]core.AskModel
}

func (a askerAI) Ask(context.Context, core.AskRequest) (core.AskResult, error) {
	return core.AskResult{}, nil
}
func (a askerAI) AskTiers() map[string]core.AskModel { return a.tiers }

// plainAI cannot judge.
type plainAI struct{ name, version string }

func (p *plainAI) Agent() string { return p.name }
func (p *plainAI) Info(context.Context) core.AdapterInfo {
	return core.AdapterInfo{Agent: p.name, Version: p.version, Available: true, Capabilities: []core.Capability{}}
}
func (p *plainAI) ListSessions(context.Context) (core.ListResult, error) {
	return core.ListResult{}, nil
}

// fakeRuns is a run source with one run.
type fakeRuns struct {
	mu    sync.Mutex
	run   runs.Run
	ok    bool
	gets  int
	onGet func()
}

func (f *fakeRuns) set(r runs.Run) {
	f.mu.Lock()
	f.run, f.ok = r, true
	f.mu.Unlock()
}

func (f *fakeRuns) Get(owner, id string) (runs.Run, bool) {
	f.mu.Lock()
	f.gets++
	hook := f.onGet
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.run, f.ok && f.run.ID == id
}

// syncBuffer is a log buffer that tests read while the Manager writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// ---- helpers --------------------------------------------------------------

// threeAIs are the three AI programs of the reference PC; opencode leaves
// conversations behind.
func threeAIs() []core.Adapter {
	return []core.Adapter{newFake("claude", "anthropic"), newFake("codex", "openai"), cleanerAI{newFake("opencode", "deepseek")}}
}

func fakeOf(a core.Adapter) *fakeAI {
	switch v := a.(type) {
	case *fakeAI:
		return v
	case cleanerAI:
		return v.fakeAI
	case askerAI:
		return v.fakeAI
	}
	panic("not a fake")
}

func newTestManager(t *testing.T, ais []core.Adapter, opt func(*Options)) (*Manager, *syncBuffer) {
	t.Helper()
	buf := &syncBuffer{}
	o := Options{Adapters: ais, Gate: testGate(t, ais...), Logger: logx.New(buf, slog.LevelInfo)}
	if opt != nil {
		opt(&o)
	}
	m := New(o)
	t.Cleanup(m.Close)
	return m, buf
}

const testQuestion = "What is the number of the day?"

// threeAnswers: claude and codex say 1, opencode 2. With the rule judge
// that is claude:1 and codex:1 against opencode:1 on 다른 말.
func threeAnswers(owner, key string) DirectSpec {
	return DirectSpec{Owner: owner, Key: key, Question: testQuestion, Answers: []DirectAnswer{
		{ID: "claude", Agent: "claude", Via: judge.ViaPC, Model: "claude-m", Text: "The number is NUM 1."},
		{ID: "codex", Agent: "codex", Via: judge.ViaPC, Model: "codex-m", Text: "The number is NUM 1."},
		{ID: "opencode", Agent: "opencode", Via: judge.ViaPC, Model: "oc-m", Text: "The number is NUM 2."},
	}}
}

func startDirect(t *testing.T, m *Manager, s DirectSpec) Comparison {
	t.Helper()
	c, err := m.Start(s)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return c
}

func waitIdle(t *testing.T, m *Manager, owner, id string) Comparison {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, ok := m.WaitIdle(ctx, owner, id)
	if !ok {
		t.Fatalf("WaitIdle: no comparison %s", id)
	}
	if ctx.Err() != nil {
		t.Fatalf("WaitIdle timed out: %s", c.Status)
	}
	return c
}

// waitFor polls cond until it holds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func part(agent, answer, model string) runs.Part {
	return runs.Part{Agent: agent, Status: runs.Done, Answer: answer, Model: model}
}

func endedRun(id string, parts ...runs.Part) runs.Run {
	return runs.Run{ID: id, Mode: core.ModeAsk, Status: runs.Done, Parts: parts}
}

func threeParts() []runs.Part {
	return []runs.Part{
		part("claude", "The number is NUM 1.", "claude-m"),
		part("codex", "The number is NUM 1.", "codex-m"),
		part("opencode", "The number is NUM 2.", "oc-m"),
	}
}

func runSpec(owner, id string) RunSpec {
	return RunSpec{Owner: owner, RunID: id, Question: testQuestion, Judges: []string{"claude", "codex", "opencode"}, Asked: []string{"claude", "codex", "opencode"}}
}

func totalCalls(ais []core.Adapter) int {
	n := 0
	for _, a := range ais {
		if _, ok := a.(*plainAI); !ok {
			n += len(fakeOf(a).judgeCalls())
		}
	}
	return n
}

func requestsOf(c Comparison, judgeAI string) []judge.Request {
	var out []judge.Request
	for _, r := range c.Requests {
		if r.Judge == judgeAI {
			out = append(out, r)
		}
	}
	return out
}

func internalOf(m *Manager, id string) *comparison {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byID[id]
}

var nonceRe = regexp.MustCompile(`<data-([0-9a-f]{16})>`)

func nonceOf(prompt string) string {
	if m := nonceRe.FindStringSubmatch(prompt); m != nil {
		return m[1]
	}
	return ""
}

// ---- tests ----------------------------------------------------------------

func TestDirectComparison(t *testing.T) {
	ais := threeAIs()
	m, buf := newTestManager(t, ais, nil)
	c := startDirect(t, m, threeAnswers("phone", ""))
	if c.Status != Running || c.Source != SourceDirect || c.Engine != "pc" || c.RunID != "" || len(c.ID) != 32 {
		t.Errorf("first snapshot = %+v", c)
	}
	c = waitIdle(t, m, "phone", c.ID)
	if c.Status != Done || c.Error != nil {
		t.Fatalf("status %s %+v", c.Status, c.Error)
	}
	if len(c.Requests) != 6 {
		t.Fatalf("%d requests", len(c.Requests))
	}
	for _, r := range c.Requests {
		if r.Status != judge.RequestDone || r.Attempt != 1 || r.Judge == r.From || r.Judge == r.Against || r.Error != nil || r.InputTokens != 10 || r.OutputTokens != 2 {
			t.Errorf("request %+v", r)
		}
	}
	for _, a := range ais {
		if n := len(fakeOf(a).judgeCalls()); n != 2 {
			t.Errorf("%s judged %d times, want 2", a.Agent(), n)
		}
	}
	if c.Counts.Differ != 1 || len(c.Items) == 0 || c.Items[0].Kind != judge.CardDiffer {
		t.Errorf("counts %+v items %+v", c.Counts, c.Items)
	}
	for _, a := range c.Answers {
		if a.Text != "" || a.Tail != nil || a.Status != judge.AnswerReady || len(a.Units) != 1 || a.Family == "" {
			t.Errorf("answer %+v", a)
		}
	}
	// Every judge call got the prompt, the folder and its own nonce.
	nonces := map[string]bool{}
	for _, a := range ais {
		for _, r := range fakeOf(a).judgeCalls() {
			n := nonceOf(r.Prompt)
			if n == "" || nonces[n] || !strings.Contains(r.Prompt, testQuestion) || r.Folder != m.o.Gate.Folder || len(r.Task.Units) == 0 {
				t.Errorf("judge call: nonce %q, folder %q", n, r.Folder)
			}
			nonces[n] = true
		}
	}
	logs := buf.String()
	for _, want := range []string{`"msg":"comparison started"`, `"source":"direct"`, `"ais":3`, `"requests":6`, `"judges":3`, `"noJudge":0`, `"msg":"judge request"`, `"msg":"comparison ended"`, `"differ":1`} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %s:\n%s", want, logs)
		}
	}
}

func TestSnapshotJSON(t *testing.T) {
	ais := threeAIs()
	fakeOf(ais[0]).setJudge(func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		return core.JudgeResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeTimeout, `C:\Users\someone\secret-path`)
	})
	m, _ := newTestManager(t, ais, nil)
	c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"comparisonId":"`, `"status":"done"`, `"source":"direct"`, `"engine":"pc"`, `"answers":[`, `"requests":[`, `"judgments":[`, `"items":[`, `"counts":{`, `"units":[`} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON lacks %s: %s", want, s)
		}
	}
	for _, leak := range []string{`"reason"`, "fake", "secret-path", `"runId"`, `"Text"`, `"Tail"`, `"Before"`} {
		if strings.Contains(s, leak) {
			t.Errorf("JSON holds %s: %s", leak, s)
		}
	}
	if want := `"error":{"code":"judge_timeout","message":"` + messages[core.ErrCodeJudgeTimeout] + `"}`; !strings.Contains(s, want) {
		t.Errorf("JSON lacks the fixed message %s: %s", want, s)
	}
	for _, a := range ais {
		for _, r := range fakeOf(a).judgeCalls() {
			if n := nonceOf(r.Prompt); strings.Contains(s, n) {
				t.Errorf("JSON holds a nonce")
			}
		}
	}
	// An empty comparison's slices are [] too.
	empty := (&Manager{}).snapshotLocked(&comparison{id: "x", status: Failed})
	b, _ = json.Marshal(empty)
	for _, want := range []string{`"answers":[]`, `"requests":[]`, `"judgments":[]`, `"items":[]`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("empty JSON lacks %s: %s", want, b)
		}
	}
}

func TestAttachExactlyOnce(t *testing.T) {
	for _, when := range []string{"before", "during", "after"} {
		t.Run(when, func(t *testing.T) {
			ais := threeAIs()
			fr := &fakeRuns{}
			m, buf := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
			run := endedRun("run-1", threeParts()...)
			running := run
			running.Status = runs.Running
			switch when {
			case "before": // OnEnd fired before Attach registered: the post-check starts it
				fr.set(run)
				m.RunEnded(run)
			case "during": // OnEnd fires while Attach looks the run up
				fr.set(running)
				fr.onGet = func() {
					fr.set(run)
					m.RunEnded(run)
				}
			case "after":
				fr.set(running)
			}
			var wg sync.WaitGroup
			ids := make([]string, 50)
			errs := make([]error, 50)
			start := make(chan struct{})
			for i := range 50 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					if i%2 == 0 {
						ids[i], errs[i] = m.Attach(runSpec("phone", run.ID))
					} else {
						m.RunEnded(run)
					}
				}()
			}
			close(start)
			wg.Wait()
			m.RunEnded(run) // the run's OnEnd, after everything
			id := ids[0]
			for i := 0; i < 50; i += 2 {
				if errs[i] != nil || ids[i] != id {
					t.Fatalf("Attach %d = %q %v, want %q", i, ids[i], errs[i], id)
				}
			}
			c := waitIdle(t, m, "phone", id)
			if c.Status != Done || c.Source != SourceRun || c.RunID != run.ID {
				t.Fatalf("comparison %+v", c)
			}
			if n := totalCalls(ais); n != 6 {
				t.Errorf("%d judge calls, want 6 (the plan once)", n)
			}
			if n := strings.Count(buf.String(), `"msg":"comparison started"`); n != 1 {
				t.Errorf("planned %d times", n)
			}
			if again, err := m.Attach(runSpec("phone", run.ID)); again != id || err != nil {
				t.Errorf("Attach again = %q %v", again, err)
			}
			if got, ok := m.ForRun("phone", run.ID); !ok || got != id {
				t.Errorf("ForRun = %q %v", got, ok)
			}
			if _, ok := m.ForRun("tablet", run.ID); ok {
				t.Error("ForRun of another device")
			}
			if _, err := m.Attach(runSpec("tablet", run.ID)); !errors.Is(err, ErrNotFound) {
				t.Errorf("Attach of another device's run: %v", err)
			}
		})
	}
}

func TestDirectKey(t *testing.T) {
	ais := threeAIs()
	m, _ := newTestManager(t, ais, nil)
	a := startDirect(t, m, threeAnswers("phone", "k1"))
	b := startDirect(t, m, threeAnswers("phone", "k1"))
	if a.ID != b.ID {
		t.Errorf("same key: %s and %s", a.ID, b.ID)
	}
	other := startDirect(t, m, threeAnswers("tablet", "k1"))
	noKey1 := startDirect(t, m, threeAnswers("phone", ""))
	noKey2 := startDirect(t, m, threeAnswers("phone", ""))
	if other.ID == a.ID || noKey1.ID == noKey2.ID {
		t.Errorf("another device or no key must make a new comparison")
	}
	for _, c := range []struct{ owner, id string }{{"phone", a.ID}, {"tablet", other.ID}, {"phone", noKey1.ID}, {"phone", noKey2.ID}} {
		waitIdle(t, m, c.owner, c.id)
	}
	if n := totalCalls(ais); n != 4*6 {
		t.Errorf("%d judge calls, want 24", n)
	}
	// The same key after the first ended still gives it, even with a
	// different body.
	s := threeAnswers("phone", "k1")
	s.Answers = s.Answers[:1]
	if c, err := m.Start(s); err != nil || c.ID != a.ID {
		t.Errorf("Start again = %s %v", c.ID, err)
	}
}

func TestPlanInputs(t *testing.T) {
	t.Run("missing answer and judge only", func(t *testing.T) {
		ais := threeAIs()
		fr := &fakeRuns{}
		m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
		parts := threeParts()
		parts[2] = runs.Part{Agent: "opencode", Status: runs.Failed, Error: &runs.Error{Code: core.ErrCodeAskFailed, Message: "usage limit"}}
		parts[1].Answer = "The number is NUM 2."
		fr.set(endedRun("run-2", parts...))
		id, err := m.Attach(runSpec("phone", "run-2"))
		if err != nil {
			t.Fatal(err)
		}
		c := waitIdle(t, m, "phone", id)
		if c.Status != Done || len(c.Requests) != 2 {
			t.Fatalf("status %s, %d requests", c.Status, len(c.Requests))
		}
		for _, r := range c.Requests {
			if r.Judge != "opencode" || !r.JudgeOnly {
				t.Errorf("request %+v, want opencode judge only", r)
			}
		}
		// A failed part names no model, so its company is unknown.
		if a := c.Answers[2]; a.Status != judge.AnswerMissing || a.Units == nil || len(a.Units) != 0 || a.Family != "" {
			t.Errorf("missing answer %+v", a)
		}
		in := internalOf(m, id)
		m.mu.Lock()
		judges := slices.Clone(in.spec.Judges)
		m.mu.Unlock()
		if len(judges) != 3 || judges[2].AskCode != core.ErrCodeAskFailed || judges[0].AskCode != "" || judges[2].Family != "deepseek" {
			t.Errorf("judges %+v", judges)
		}
		if c.Counts.Differ != 1 {
			t.Errorf("counts %+v", c.Counts)
		}
	})
	t.Run("judges cut and families of AIs that do not judge", func(t *testing.T) {
		ais := threeAIs()
		ais = append(ais, newFake("gemini", "google"))
		fakeOf(ais[3]).setAvailable(false) // listed as a judge, but does not judge now
		fakeOf(ais[1]).version = "0.9.0"   // codex answered but does not judge
		fr := &fakeRuns{}
		m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
		fr.set(endedRun("run-3", threeParts()...))
		s := runSpec("phone", "run-3")
		s.Judges = []string{"gemini", "opencode", "codex"} // claude was not chosen
		id, _ := m.Attach(s)
		c := waitIdle(t, m, "phone", id)
		if c.Status != Done {
			t.Fatalf("status %s %+v", c.Status, c.Error)
		}
		if c.Answers[1].Family != "openai" {
			t.Errorf("codex answer family %q: it comes from its adapter even when codex does not judge", c.Answers[1].Family)
		}
		// claude↔codex: opencode. claude↔opencode: only codex, which does
		// not judge. codex↔opencode: only claude, not chosen.
		judged, noJudge := 0, 0
		for _, r := range c.Requests {
			switch {
			case r.Judge == "opencode" && r.Status == judge.RequestDone:
				judged++
			case r.Judge == "" && r.Error != nil && r.Error.Code == core.ErrCodeNoJudge && r.Status == judge.RequestFailed:
				noJudge++
			default:
				t.Errorf("request %+v", r)
			}
		}
		if judged != 2 || noJudge != 4 {
			t.Errorf("%d judged, %d no_judge records", judged, noJudge)
		}
		if n := len(fakeOf(ais[0]).judgeCalls()) + len(fakeOf(ais[1]).judgeCalls()) + len(fakeOf(ais[3]).judgeCalls()); n != 0 {
			t.Errorf("%d calls of AIs that may not judge", n)
		}
	})
	t.Run("no judge at all", func(t *testing.T) {
		ais := threeAIs()
		fr := &fakeRuns{}
		m, buf := newTestManager(t, ais, func(o *Options) { o.Runs = fr; o.Gate.Settings.Off = true })
		fr.set(endedRun("run-4", threeParts()...))
		id, err := m.Attach(runSpec("phone", "run-4"))
		if err != nil {
			t.Fatal(err)
		}
		c := waitIdle(t, m, "phone", id)
		if c.Status != Failed || c.Error == nil || c.Error.Code != core.ErrCodeNoJudge || len(c.Requests) != 6 {
			t.Fatalf("comparison %+v", c)
		}
		if c.Counts.NotCompared != 3 {
			t.Errorf("counts %+v", c.Counts)
		}
		if !strings.Contains(buf.String(), `"code":"no_judge"`) {
			t.Errorf("logs: %s", buf.String())
		}
		// Direct comparisons refuse at once.
		if _, err := m.Start(threeAnswers("phone", "")); !errors.Is(err, ErrNoJudge) {
			t.Errorf("Start = %v", err)
		}
	})
	t.Run("two answers missing", func(t *testing.T) {
		ais := threeAIs()
		fr := &fakeRuns{}
		m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
		parts := threeParts()
		parts[0] = runs.Part{Agent: "claude", Status: runs.Failed, Error: &runs.Error{Code: core.ErrCodeAskTimeout}}
		parts[1] = runs.Part{Agent: "codex", Status: runs.Done, Answer: "  \n "}
		fr.set(endedRun("run-5", parts...))
		id, _ := m.Attach(runSpec("phone", "run-5"))
		c := waitIdle(t, m, "phone", id)
		if c.Status != Failed || c.Error == nil || c.Error.Code != core.ErrCodeNeedTwoAnswers || len(c.Requests) != 0 || c.Requests == nil {
			t.Fatalf("comparison %+v", c)
		}
		if c.Answers[1].Status != judge.AnswerMissing {
			t.Errorf("a blank answer is missing: %+v", c.Answers[1])
		}
	})
}

func TestMaxUnits(t *testing.T) {
	ais := threeAIs()
	m, _ := newTestManager(t, ais, func(o *Options) { o.Gate.Settings.MaxUnits = 1 })
	s := threeAnswers("phone", "")
	// The units after the first have no number, so no rule judge cites
	// them as evidence, which would show them.
	s.Answers[0].Text = "The number is NUM 1. It is a fine number. It is the one."
	c := waitIdle(t, m, "phone", startDirect(t, m, s).ID)
	if a := c.Answers[0]; a.NotJudged != 2 || len(a.Units) != 3 || !a.Units[0].Judged || a.Units[1].Judged {
		t.Errorf("answer %+v", a)
	}
	if c.Counts.NotJudged != 2 {
		t.Errorf("counts %+v", c.Counts)
	}
}

func TestSemaphores(t *testing.T) {
	for _, perAI := range []int{2, 1} {
		t.Run(fmt.Sprint("perAI ", perAI), func(t *testing.T) {
			load := &loadStats{}
			var ais []core.Adapter
			for i, fam := range []string{"anthropic", "openai", "deepseek", "google"} {
				f := newFake(fmt.Sprintf("ai%d", i+1), fam)
				f.load = load
				f.judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
					// Hold the slot until every slot that can be taken is.
					deadline := time.Now().Add(300 * time.Millisecond)
					for time.Now().Before(deadline) {
						load.mu.Lock()
						full := load.running >= min(MaxProcesses, 4*perAI)
						load.mu.Unlock()
						if full {
							break
						}
						time.Sleep(time.Millisecond)
					}
					time.Sleep(5 * time.Millisecond)
					return ruleJudge(req), nil
				}
				ais = append(ais, f)
			}
			m, _ := newTestManager(t, ais, func(o *Options) { o.Gate.Settings.PerAI = perAI })
			var ids []string
			for k := range 2 {
				s := DirectSpec{Owner: "phone", Question: testQuestion}
				for i, a := range ais {
					s.Answers = append(s.Answers, DirectAnswer{ID: a.Agent(), Agent: a.Agent(), Via: judge.ViaPC, Model: "m", Text: fmt.Sprintf("Value NUM %d.", i+k)})
				}
				ids = append(ids, startDirect(t, m, s).ID)
			}
			for _, id := range ids {
				if c := waitIdle(t, m, "phone", id); c.Status != Done || len(c.Requests) != 12 {
					t.Fatalf("comparison %s, %d requests", c.Status, len(c.Requests))
				}
			}
			for _, a := range ais {
				f := fakeOf(a)
				if p := f.peakRunning(); p > perAI || p < 1 {
					t.Errorf("%s ran %d at once, want at most %d", f.name, p, perAI)
				}
			}
			if n := totalCalls(ais); n != 24 {
				t.Errorf("%d judge calls, want 24", n)
			}
			if want := min(MaxProcesses, 4*perAI); load.peak != want {
				t.Errorf("%d judges ran at once, want %d", load.peak, want)
			}
		})
	}
}

func TestQueueLimit(t *testing.T) {
	ais := threeAIs()
	release := make(chan struct{})
	for _, a := range ais {
		fakeOf(a).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
			select {
			case <-release:
			case <-time.After(5 * time.Second):
			}
			return ruleJudge(req), nil
		}
	}
	m, _ := newTestManager(t, ais, func(o *Options) { o.Gate.Settings.PerAI = 1; o.QueueLimit = 100 * time.Millisecond })
	c := startDirect(t, m, threeAnswers("phone", ""))
	waitFor(t, "the second requests to time out", func() bool {
		got, _ := m.Get("phone", c.ID)
		n := 0
		for _, r := range got.Requests {
			if r.Status == judge.RequestFailed {
				n++
			}
		}
		return n == 3
	})
	close(release)
	c = waitIdle(t, m, "phone", c.ID)
	for _, a := range ais {
		rs := requestsOf(c, a.Agent())
		done, timedOut := 0, 0
		for _, r := range rs {
			switch {
			case r.Status == judge.RequestDone:
				done++
			case r.Error != nil && r.Error.Code == core.ErrCodeJudgeTimeout && r.Error.Message == msgQueue && r.Attempt == 0:
				timedOut++
			default:
				t.Errorf("request %+v", r)
			}
		}
		if done != 1 || timedOut != 1 {
			t.Errorf("%s: %d done, %d timed out in the queue", a.Agent(), done, timedOut)
		}
	}
	if c.Status != Done {
		t.Errorf("status %s", c.Status)
	}
	for _, it := range c.Items {
		if it.Missing == nil || it.Check == nil {
			t.Errorf("item %+v", it)
		}
	}
}

func TestJudgeLimit(t *testing.T) {
	old := abandonAfter
	abandonAfter = 50 * time.Millisecond
	t.Cleanup(func() { abandonAfter = old })
	ais := threeAIs()
	stuck := make(chan struct{})
	defer close(stuck)
	// claude ends with the context; codex never looks at it.
	fakeOf(ais[0]).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		<-ctx.Done()
		return core.JudgeResult{Cleanup: ""}, ctx.Err()
	}
	fakeOf(ais[1]).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		<-stuck
		return ruleJudge(req), nil
	}
	m, _ := newTestManager(t, ais, func(o *Options) { o.JudgeLimit = 80 * time.Millisecond })
	c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
	for _, ai := range []string{"claude", "codex"} {
		for _, r := range requestsOf(c, ai) {
			if r.Status != judge.RequestFailed || r.Error.Code != core.ErrCodeJudgeTimeout || r.Attempt != 1 || r.Error.Message != messages[core.ErrCodeJudgeTimeout] {
				t.Errorf("%s request %+v %+v", ai, r, r.Error)
			}
		}
	}
	if c.Status != Done {
		t.Errorf("status %s", c.Status)
	}
}

func TestBadOutput(t *testing.T) {
	prose := func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		return core.JudgeResult{Output: json.RawMessage(fakecli.ProseText), Model: req.Model}, nil
	}
	t.Run("prose then rule", func(t *testing.T) {
		ais := threeAIs()
		fakeOf(ais[0]).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
			if n%2 == 1 {
				return prose(ctx, req, n)
			}
			return ruleJudge(req), nil
		}
		m, _ := newTestManager(t, ais, func(o *Options) { o.Gate.Settings.PerAI = 1 })
		c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
		for _, r := range requestsOf(c, "claude") {
			if r.Status != judge.RequestDone || r.Attempt != 2 || r.InputTokens != 10 {
				t.Errorf("request %+v", r)
			}
		}
		calls := fakeOf(ais[0]).judgeCalls()
		if len(calls) != 4 {
			t.Fatalf("%d claude calls", len(calls))
		}
		for i := 0; i < 4; i += 2 {
			a, b := nonceOf(calls[i].Prompt), nonceOf(calls[i+1].Prompt)
			if a == "" || a == b || calls[i].Task.Units[0].Text != calls[i+1].Task.Units[0].Text {
				t.Errorf("attempts %d and %d: nonces %q %q", i+1, i+2, a, b)
			}
		}
	})
	t.Run("prose twice", func(t *testing.T) {
		ais := threeAIs()
		fakeOf(ais[0]).judge = prose
		m, _ := newTestManager(t, ais, nil)
		c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
		for _, r := range requestsOf(c, "claude") {
			if r.Status != judge.RequestFailed || r.Attempt != 2 || r.Error.Code != core.ErrCodeJudgeBadOutput || r.Error.Message != messages[core.ErrCodeJudgeBadOutput] {
				t.Errorf("request %+v %+v", r, r.Error)
			}
		}
		if n := len(fakeOf(ais[0]).judgeCalls()); n != 4 {
			t.Errorf("%d calls", n)
		}
	})
	t.Run("adapter bad output twice", func(t *testing.T) {
		ais := threeAIs()
		fakeOf(ais[0]).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
			return core.JudgeResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeBadOutput, "")
		}
		m, _ := newTestManager(t, ais, nil)
		c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
		for _, r := range requestsOf(c, "claude") {
			if r.Status != judge.RequestFailed || r.Attempt != 2 || r.Error.Code != core.ErrCodeJudgeBadOutput {
				t.Errorf("request %+v", r)
			}
		}
	})
	// Answers of two units, so a judge can miss one.
	twoUnits := func() DirectSpec {
		s := threeAnswers("phone", "")
		s.Answers[1].Text = "The number is NUM 1. The size is NUM 3."
		s.Answers[2].Text = "The number is NUM 2. The size is NUM 3."
		return s
	}
	drop := func(id string) func(ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
		return func(ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
			var out struct {
				Judgments []map[string]any `json:"judgments"`
			}
			json.Unmarshal([]byte(fakecli.Answer(req.Prompt, false)), &out)
			out.Judgments = slices.DeleteFunc(out.Judgments, func(j map[string]any) bool { return j["id"] == id })
			b, _ := json.Marshal(out)
			return core.JudgeResult{Output: b, Model: req.Model}, nil
		}
	}
	t.Run("missing ids merge", func(t *testing.T) {
		ais := threeAIs()
		fakeOf(ais[0]).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
			if n%2 == 1 {
				return drop("a1")(ctx, req)
			}
			return drop("a2")(ctx, req)
		}
		m, _ := newTestManager(t, ais, func(o *Options) { o.Gate.Settings.PerAI = 1 })
		c := waitIdle(t, m, "phone", startDirect(t, m, twoUnits()).ID)
		for _, r := range requestsOf(c, "claude") {
			if r.Status != judge.RequestDone || r.Attempt != 2 {
				t.Errorf("request %+v", r)
			}
			var units []string
			for _, j := range c.Judgments {
				if j.Request == r.ID {
					units = append(units, j.Unit)
				}
			}
			sort.Strings(units)
			if want := []string{r.From + ":1", r.From + ":2"}; !slices.Equal(units, want) {
				t.Errorf("%s judged %v, want %v", r.ID, units, want)
			}
		}
	})
	t.Run("the same id missing twice", func(t *testing.T) {
		ais := threeAIs()
		fakeOf(ais[0]).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
			return drop("a1")(ctx, req)
		}
		m, _ := newTestManager(t, ais, nil)
		c := waitIdle(t, m, "phone", startDirect(t, m, twoUnits()).ID)
		for _, r := range requestsOf(c, "claude") {
			var units []string
			for _, j := range c.Judgments {
				if j.Request == r.ID {
					units = append(units, j.Unit)
				}
			}
			if r.Status != judge.RequestDone || r.Attempt != 2 || !slices.Equal(units, []string{r.From + ":2"}) {
				t.Errorf("request %+v judged %v", r, units)
			}
		}
	})
	t.Run("missing ids, then the retry fails", func(t *testing.T) {
		ais := threeAIs()
		fakeOf(ais[0]).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
			if n%2 == 1 {
				return drop("a1")(ctx, req)
			}
			return core.JudgeResult{}, core.Fail(core.ErrCodeJudgeFailed, "overloaded")
		}
		m, _ := newTestManager(t, ais, func(o *Options) { o.Gate.Settings.PerAI = 1 })
		c := waitIdle(t, m, "phone", startDirect(t, m, twoUnits()).ID)
		for _, r := range requestsOf(c, "claude") {
			if r.Status != judge.RequestDone || r.Attempt != 2 || r.Error != nil {
				t.Errorf("attempt 1's verdicts stand: %+v", r)
			}
		}
	})
}

func TestErrorCodes(t *testing.T) {
	long := strings.Repeat("가", 400)
	cases := []struct {
		name     string
		err      error
		code     string
		message  string
		attempts int
	}{
		{"judge_failed keeps the AI's words", core.Fail(core.ErrCodeJudgeFailed, "  "+fakecli.UsageLimitCodex+"\n"), core.ErrCodeJudgeFailed, fakecli.UsageLimitCodex, 1},
		{"judge_failed is cut", core.Fail(core.ErrCodeJudgeFailed, long), core.ErrCodeJudgeFailed, strings.Repeat("가", 299) + "…", 1},
		{"judge_failed without words", core.Fail(core.ErrCodeJudgeFailed, ""), core.ErrCodeJudgeFailed, messages[core.ErrCodeJudgeFailed], 1},
		{"judge_used_tool", core.Fail(core.ErrCodeJudgeUsedTool, fakecli.ToolCommand), core.ErrCodeJudgeUsedTool, messages[core.ErrCodeJudgeUsedTool], 1},
		{"bad_model hides the model", core.Fail(core.ErrCodeBadModel, "claude-secret-model"), core.ErrCodeBadModel, messages[core.ErrCodeBadModel], 1},
		{"agent_unavailable", core.Fail(core.ErrCodeAgentUnavailable, "claude was not found"), core.ErrCodeAgentUnavailable, messages[core.ErrCodeAgentUnavailable], 1},
		{"not an ActionError", errors.New("boom"), core.ErrCodeInternal, messages[core.ErrCodeInternal], 1},
		{"an unknown code", core.Fail("rate_limited", "detail"), "rate_limited", msgOther, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ais := threeAIs()
			fakeOf(ais[1]).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
				return core.JudgeResult{Model: "codex-x", InputTokens: 5}, tc.err
			}
			m, buf := newTestManager(t, ais, nil)
			c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
			rs := requestsOf(c, "codex")
			for _, r := range rs {
				if r.Status != judge.RequestFailed || r.Error.Code != tc.code || r.Error.Message != tc.message || r.Attempt != tc.attempts || r.Model != "codex-x" || r.InputTokens != 5 {
					t.Errorf("request %+v %+v", r, r.Error)
				}
				if utf8.RuneCountInString(r.Error.Message) > core.JudgeMessageChars {
					t.Errorf("message of %d characters", utf8.RuneCountInString(r.Error.Message))
				}
			}
			if n := len(fakeOf(ais[1]).judgeCalls()); n != 2*tc.attempts {
				t.Errorf("%d calls", n)
			}
			if c.Status != Done {
				t.Errorf("a partial failure is done: %s", c.Status)
			}
			if logs := buf.String(); strings.Contains(logs, "usage limit") || strings.Contains(logs, "가가") || strings.Contains(logs, "detail") || strings.Contains(logs, "secret") || strings.Contains(logs, "curl") {
				t.Errorf("logs carry the AI's words: %s", logs)
			}
		})
	}
}

func TestWrongCompany(t *testing.T) {
	for _, model := range []string{"gpt-x", ""} {
		t.Run(fmt.Sprintf("model %q", model), func(t *testing.T) {
			ais := threeAIs()
			claude := fakeOf(ais[0])
			claude.fams = map[string]string{"gpt-x": "openai"}
			claude.judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
				res := ruleJudge(req)
				res.Model = model
				return res, nil
			}
			m, _ := newTestManager(t, ais, nil)
			c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
			for _, r := range requestsOf(c, "claude") {
				if r.Status != judge.RequestFailed || r.Error.Code != core.ErrCodeBadModel || r.Error.Message != msgCompany || r.Attempt != 1 {
					t.Errorf("request %+v %+v", r, r.Error)
				}
				for _, j := range c.Judgments {
					if j.Request == r.ID {
						t.Errorf("a judgment of %s stayed: %+v", r.ID, j)
					}
				}
			}
			if n := len(claude.judgeCalls()); n != 2 {
				t.Errorf("%d calls: bad_model is not asked again", n)
			}
		})
	}
}

func TestPanics(t *testing.T) {
	t.Run("judge", func(t *testing.T) {
		ais := threeAIs()
		fakeOf(ais[0]).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
			panic("judge bug")
		}
		m, _ := newTestManager(t, ais, nil)
		c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
		for _, r := range requestsOf(c, "claude") {
			if r.Status != judge.RequestFailed || r.Error.Code != core.ErrCodeInternal || r.Attempt != 1 {
				t.Errorf("request %+v", r)
			}
		}
		if c.Status != Done {
			t.Errorf("status %s", c.Status)
		}
	})
	t.Run("family", func(t *testing.T) {
		// A Family that panics while the answers are split fails the
		// comparison; the gate asks it only of the judge model.
		ais := threeAIs()
		ais[0] = &panicFamily{fakeAI: newFake("claude", "anthropic")}
		m, _ := newTestManager(t, ais, nil)
		c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
		if c.Status != Failed || c.Error == nil || c.Error.Code != core.ErrCodeInternal {
			t.Errorf("comparison %+v", c)
		}
	})
	t.Run("info", func(t *testing.T) {
		// An adapter whose Info panics does not judge; the others do.
		ais := threeAIs()
		ais = append(ais, &panicInfo{name: "gemini"})
		m, _ := newTestManager(t, ais, nil)
		if n := len(m.Engines(context.Background())); n != 3 {
			t.Errorf("%d engines", n)
		}
		if c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID); c.Status != Done {
			t.Errorf("status %s", c.Status)
		}
	})
	t.Run("cleanup", func(t *testing.T) {
		ais := threeAIs()
		oc := ais[2].(cleanerAI)
		oc.cleanup = func(ctx context.Context, folder string, refs []string) error { panic("cleanup bug") }
		m, buf := newTestManager(t, ais, nil)
		c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
		if c.Status != Done || len(oc.cleanupCalls()) != 1 {
			t.Errorf("status %s, %d cleanups", c.Status, len(oc.cleanupCalls()))
		}
		if !strings.Contains(buf.String(), `"msg":"judge cleanup failed","judge":"opencode","code":"internal"`) {
			t.Errorf("logs: %s", buf.String())
		}
	})
	t.Run("aggregate", func(t *testing.T) {
		ais := threeAIs()
		m, buf := newTestManager(t, ais, nil)
		m.mu.Lock()
		m.aggregate = func(in judge.AggregateInput) judge.Summary {
			if len(in.Judgments) > 0 {
				panic("aggregate bug")
			}
			return judge.Aggregate(in)
		}
		m.mu.Unlock()
		c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
		if c.Status != Failed || c.Error == nil || c.Error.Code != core.ErrCodeInternal || c.Error.Message != msgCards {
			t.Errorf("comparison %+v %+v", c, c.Error)
		}
		if strings.Contains(buf.String(), "aggregate bug") {
			t.Error("a panic value reached the log")
		}
		// The Manager keeps working.
		m.mu.Lock()
		m.aggregate = judge.Aggregate
		m.mu.Unlock()
		if c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID); c.Status != Done {
			t.Errorf("next comparison %s", c.Status)
		}
		// And Retry makes the cards of the broken one again.
		if _, err := m.Retry("phone", c.ID); err != nil {
			t.Fatal(err)
		}
		if c = waitIdle(t, m, "phone", c.ID); c.Status != Done || c.Error != nil || c.Counts.Differ != 1 {
			t.Errorf("after retry: %s %+v %+v", c.Status, c.Error, c.Counts)
		}
	})
}

// panicInfo is a judging AI whose Info panics.
type panicInfo struct{ name string }

func (p *panicInfo) Agent() string                         { return p.name }
func (p *panicInfo) Info(context.Context) core.AdapterInfo { panic("info bug") }
func (p *panicInfo) ListSessions(context.Context) (core.ListResult, error) {
	return core.ListResult{}, nil
}
func (p *panicInfo) Judge(context.Context, core.JudgeRequest) (core.JudgeResult, error) {
	return core.JudgeResult{}, nil
}
func (p *panicInfo) JudgeDefault() core.AskModel { return core.AskModel{Model: "g"} }
func (p *panicInfo) Family(string) string        { return "google" }

// panicFamily panics when asked for the company of an answer's model.
type panicFamily struct{ *fakeAI }

func (p *panicFamily) Family(model string) string {
	if model == "claude-m" {
		panic("family bug")
	}
	return p.fakeAI.Family(model)
}

func TestCleanup(t *testing.T) {
	ais := threeAIs()
	oc := ais[2].(cleanerAI)
	m, buf := newTestManager(t, ais, func(o *Options) { o.Gate.Settings.PerAI = 1 })
	var id atomic.Value
	ready := make(chan struct{}) // the test has the id
	oc.judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		<-ready
		if n == 1 {
			res := ruleJudge(req)
			res.Cleanup = "ses_1"
			return res, nil
		}
		return core.JudgeResult{Model: req.Model, Cleanup: "ses_2"}, core.Fail(core.ErrCodeJudgeTimeout, "")
	}
	var deadline time.Duration
	oc.cleanup = func(ctx context.Context, folder string, refs []string) error {
		// Both of opencode's requests have ended by now.
		c, _ := m.Get("phone", id.Load().(string))
		for _, r := range requestsOf(c, "opencode") {
			if r.Status != judge.RequestDone && r.Status != judge.RequestFailed {
				t.Errorf("cleanup before request %s ended: %s", r.ID, r.Status)
			}
		}
		if folder != m.o.Gate.Folder {
			t.Errorf("cleanup folder %q", folder)
		}
		d, _ := ctx.Deadline()
		deadline = time.Until(d)
		return core.Fail(core.ErrCodeJudgeFailed, "opencode could not delete ses_1")
	}
	c := startDirect(t, m, threeAnswers("phone", ""))
	id.Store(c.ID)
	close(ready)
	c = waitIdle(t, m, "phone", c.ID)
	calls := oc.cleanupCalls()
	if len(calls) != 1 {
		t.Fatalf("%d cleanups, want 1", len(calls))
	}
	refs := slices.Clone(calls[0])
	sort.Strings(refs)
	if !slices.Equal(refs, []string{"ses_1", "ses_2"}) {
		t.Errorf("refs %v", refs)
	}
	if deadline <= 3*core.JudgeCleanupTimeout || deadline > 4*core.JudgeCleanupTimeout {
		t.Errorf("cleanup deadline %v, want (2+2)×%v", deadline, core.JudgeCleanupTimeout)
	}
	logs := buf.String()
	if !strings.Contains(logs, `"msg":"judge cleanup failed","judge":"opencode","code":"judge_failed"`) || strings.Contains(logs, "ses_1") {
		t.Errorf("logs: %s", logs)
	}
	if c.Status != Done {
		t.Errorf("status %s", c.Status)
	}
	// A round in which opencode never ran a judge cleans nothing.
	m2, _ := newTestManager(t, threeAIs(), func(o *Options) { o.Gate.Settings.Models = map[string]core.AskModel{"opencode": {Model: "unknown-x"}} })
	oc2 := m2.o.Adapters[2].(cleanerAI)
	oc2.fams = map[string]string{"unknown-x": ""}
	waitIdle(t, m2, "phone", startDirect(t, m2, threeAnswers("phone", "")).ID)
	if n := len(oc2.cleanupCalls()); n != 0 {
		t.Errorf("%d cleanups of an AI that never judged", n)
	}
}

func TestRetry(t *testing.T) {
	ais := threeAIs()
	codex := fakeOf(ais[1])
	limited := atomic.Bool{}
	limited.Store(true)
	codex.judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		if limited.Load() {
			return core.JudgeResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeFailed, fakecli.UsageLimitCodex)
		}
		return ruleJudge(req), nil
	}
	m, _ := newTestManager(t, ais, nil)
	c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
	if c.Status != Done || len(requestsOf(c, "codex")) != 2 {
		t.Fatalf("status %s", c.Status)
	}
	for _, r := range requestsOf(c, "codex") {
		if r.Status != judge.RequestFailed || r.Error.Message != fakecli.UsageLimitCodex {
			t.Fatalf("request %+v", r)
		}
	}
	keys := map[string]string{}
	for _, it := range c.Items {
		keys[it.Kind] = it.Key
	}
	before := map[string]int{}
	for _, a := range ais {
		before[a.Agent()] = len(fakeOf(a).judgeCalls())
	}

	limited.Store(false)
	r, err := m.Retry("phone", c.ID)
	if err != nil || r.Status != Running {
		t.Fatalf("Retry = %s %v", r.Status, err)
	}
	c = waitIdle(t, m, "phone", c.ID)
	if c.Status != Done || c.Error != nil {
		t.Fatalf("after retry: %s %+v", c.Status, c.Error)
	}
	for _, a := range ais {
		want := before[a.Agent()]
		if a.Agent() == "codex" {
			want += 2
		}
		if n := len(fakeOf(a).judgeCalls()); n != want {
			t.Errorf("%s: %d calls, want %d (only the failed requests run again)", a.Agent(), n, want)
		}
	}
	for _, rq := range c.Requests {
		if rq.Status != judge.RequestDone || rq.Attempt != 1 || rq.Error != nil {
			t.Errorf("request %+v", rq)
		}
	}
	if k := keys[judge.CardDiffer]; k == "" || c.Items[0].Key != k {
		t.Errorf("differ key %q before, %q after", k, c.Items[0].Key)
	}
	// Nothing left to retry: the same snapshot, no call.
	again, err := m.Retry("phone", c.ID)
	if err != nil || again.Status != Done || totalCalls(ais) != before["claude"]+before["codex"]+before["opencode"]+2 {
		t.Errorf("Retry of a done comparison: %s %v", again.Status, err)
	}
	if _, err := m.Retry("tablet", c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another device: %v", err)
	}
	if _, err := m.Retry("phone", "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
}

func TestRetryWhileRunning(t *testing.T) {
	ais := threeAIs()
	release := make(chan struct{})
	fakeOf(ais[0]).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		<-release
		return ruleJudge(req), nil
	}
	m, _ := newTestManager(t, ais, nil)
	c := startDirect(t, m, threeAnswers("phone", ""))
	if _, err := m.Retry("phone", c.ID); !errors.Is(err, ErrRunning) {
		t.Errorf("Retry while running: %v", err)
	}
	close(release)
	waitIdle(t, m, "phone", c.ID)
}

func TestRetryRegates(t *testing.T) {
	ais := threeAIs()
	codex := fakeOf(ais[1])
	codex.judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		return core.JudgeResult{}, core.Fail(core.ErrCodeJudgeFailed, "limit")
	}
	m, _ := newTestManager(t, ais, nil)
	c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
	codex.setAvailable(false)
	if _, err := m.Retry("phone", c.ID); err != nil {
		t.Fatal(err)
	}
	c = waitIdle(t, m, "phone", c.ID)
	for _, r := range requestsOf(c, "codex") {
		if r.Status != judge.RequestFailed || r.Error.Code != core.ErrCodeAgentUnavailable || r.Attempt != 0 {
			t.Errorf("request %+v", r)
		}
	}
	if n := len(codex.judgeCalls()); n != 2 {
		t.Errorf("%d codex calls", n)
	}
	if c.Status != Done {
		t.Errorf("status %s", c.Status)
	}
}

func TestRetryReplans(t *testing.T) {
	ais := threeAIs()
	for _, a := range ais {
		fakeOf(a).setAvailable(false)
	}
	fr := &fakeRuns{}
	m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
	fr.set(endedRun("run-r", threeParts()...))
	id, _ := m.Attach(runSpec("phone", "run-r"))
	c := waitIdle(t, m, "phone", id)
	if c.Status != Failed || c.Error.Code != core.ErrCodeNoJudge {
		t.Fatalf("comparison %+v", c)
	}
	for _, a := range ais {
		fakeOf(a).setAvailable(true)
	}
	if _, err := m.Retry("phone", id); err != nil {
		t.Fatal(err)
	}
	c = waitIdle(t, m, "phone", id)
	if c.Status != Done || len(c.Requests) != 6 || totalCalls(ais) != 6 {
		t.Errorf("after retry: %s, %d requests, %d calls", c.Status, len(c.Requests), totalCalls(ais))
	}
}

func TestCancel(t *testing.T) {
	t.Run("waiting never starts", func(t *testing.T) {
		ais := threeAIs()
		fr := &fakeRuns{}
		m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
		run := endedRun("run-c", threeParts()...)
		running := run
		running.Status = runs.Running
		fr.set(running)
		id, _ := m.Attach(runSpec("phone", run.ID))
		m.RunEnded(running) // a run that has not ended starts nothing
		if c, _ := m.Get("phone", id); c.Status != Waiting || totalCalls(ais) != 0 {
			t.Fatalf("status %s", c.Status)
		}
		c, err := m.Cancel("phone", id)
		if err != nil || c.Status != Failed || c.Error.Code != core.ErrCodeCancelled {
			t.Fatalf("Cancel = %+v %v", c, err)
		}
		fr.set(run)
		m.RunEnded(run)
		c = waitIdle(t, m, "phone", id)
		if c.Status != Failed || totalCalls(ais) != 0 {
			t.Errorf("a cancelled comparison started: %s, %d calls", c.Status, totalCalls(ais))
		}
		// Retry waits for the run again, and starts as it has ended.
		if _, err := m.Retry("phone", id); err != nil {
			t.Fatal(err)
		}
		if c = waitIdle(t, m, "phone", id); c.Status != Done || totalCalls(ais) != 6 {
			t.Errorf("after retry: %s, %d calls", c.Status, totalCalls(ais))
		}
	})
	t.Run("queued are dropped, running finish", func(t *testing.T) {
		ais := threeAIs()
		release := make(chan struct{})
		var running atomic.Int32
		for _, a := range ais {
			fakeOf(a).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
				running.Add(1)
				<-release
				return ruleJudge(req), nil
			}
		}
		m, _ := newTestManager(t, ais, func(o *Options) { o.Gate.Settings.PerAI = 1 })
		c := startDirect(t, m, threeAnswers("phone", ""))
		waitFor(t, "one judge per AI", func() bool { return running.Load() == 3 })
		if got, err := m.Cancel("phone", c.ID); err != nil || got.Status != Running {
			t.Fatalf("Cancel = %s %v", got.Status, err)
		}
		waitFor(t, "the queued requests to drop", func() bool {
			got, _ := m.Get("phone", c.ID)
			n := 0
			for _, r := range got.Requests {
				if r.Error != nil && r.Error.Code == core.ErrCodeCancelled {
					n++
				}
			}
			return n == 3
		})
		close(release)
		c = waitIdle(t, m, "phone", c.ID)
		done, cancelled := 0, 0
		for _, r := range c.Requests {
			switch {
			case r.Status == judge.RequestDone:
				done++
			case r.Error != nil && r.Error.Code == core.ErrCodeCancelled && r.Attempt == 0:
				cancelled++
			}
		}
		if done != 3 || cancelled != 3 || totalCalls(ais) != 3 || c.Status != Done {
			t.Errorf("%d done, %d cancelled, %d calls, status %s", done, cancelled, totalCalls(ais), c.Status)
		}
		// Cancel of an ended comparison changes nothing.
		if again, err := m.Cancel("phone", c.ID); err != nil || again.Status != Done {
			t.Errorf("Cancel again = %s %v", again.Status, err)
		}
	})
	t.Run("no retry after cancel", func(t *testing.T) {
		ais := threeAIs()
		release := make(chan struct{})
		started := make(chan struct{}, 2)
		fakeOf(ais[0]).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
			started <- struct{}{}
			<-release
			return core.JudgeResult{Output: json.RawMessage(fakecli.ProseText), Model: req.Model}, nil
		}
		m, _ := newTestManager(t, ais, nil)
		c := startDirect(t, m, threeAnswers("phone", ""))
		<-started
		<-started
		m.Cancel("phone", c.ID)
		close(release)
		c = waitIdle(t, m, "phone", c.ID)
		for _, r := range requestsOf(c, "claude") {
			if r.Status != judge.RequestFailed || r.Error.Code != core.ErrCodeJudgeBadOutput || r.Attempt != 1 {
				t.Errorf("request %+v", r)
			}
		}
		if n := len(fakeOf(ais[0]).judgeCalls()); n != 2 {
			t.Errorf("%d claude calls after cancel", n)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		m, _ := newTestManager(t, threeAIs(), nil)
		if _, err := m.Cancel("phone", "nope"); !errors.Is(err, ErrNotFound) {
			t.Errorf("Cancel = %v", err)
		}
	})
}

func TestLimits(t *testing.T) {
	ais := threeAIs()
	fr := &fakeRuns{}
	now := atomic.Int64{}
	now.Store(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC).UnixNano())
	m, _ := newTestManager(t, ais, func(o *Options) {
		o.Runs = fr
		o.Now = func() time.Time { return time.Unix(0, now.Load()) }
	})
	running := runs.Run{ID: "none", Status: runs.Running}
	fr.set(running) // no run the comparisons ask for exists: each fails at once
	if id, err := m.Attach(runSpec("phone", "gone")); err != nil {
		t.Fatal(err)
	} else if c := waitIdle(t, m, "phone", id); c.Status != Failed || c.Error.Code != core.ErrCodeInternal {
		t.Errorf("a run that is gone: %+v", c)
	}
	var first string
	for i := range MaxUnfinished {
		id := fmt.Sprintf("run-%d", i)
		fr.set(runs.Run{ID: id, Status: runs.Running})
		cid, err := m.Attach(runSpec("phone", id))
		if err != nil {
			t.Fatalf("Attach %d: %v", i, err)
		}
		if i == 0 {
			first = cid
		}
	}
	if _, err := m.Attach(runSpec("phone", "run-9")); !errors.Is(err, ErrTooMany) {
		t.Errorf("9th Attach = %v", err)
	}
	if _, err := m.Start(threeAnswers("phone", "")); !errors.Is(err, ErrTooMany) {
		t.Errorf("Start at the limit = %v", err)
	}
	if id, err := m.Attach(runSpec("phone", "run-0")); err != nil || id != first {
		t.Errorf("Attach of a run already attached at the limit = %q %v", id, err)
	}
	for i := range MaxUnfinished {
		m.Cancel("phone", internalIDOfRun(m, fmt.Sprintf("run-%d", i)))
	}
	c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)

	// 30 minutes after the end it is gone, and so are the cancelled ones.
	now.Add(int64(29 * time.Minute))
	if _, ok := m.Get("phone", c.ID); !ok {
		t.Fatal("gone before 30 minutes")
	}
	now.Add(int64(2 * time.Minute))
	if _, ok := m.Get("phone", c.ID); ok {
		t.Error("kept after 30 minutes")
	}
	if _, ok := m.ForRun("phone", "run-0"); ok {
		t.Error("ForRun after expiry")
	}
	m.mu.Lock()
	n := len(m.byID)
	m.mu.Unlock()
	if n != 0 {
		t.Errorf("%d comparisons left", n)
	}

	// At most 100: the oldest ended go first; waiting ones always stay.
	m.mu.Lock()
	base := time.Unix(0, now.Load())
	waiting := m.newLocked("phone", "", SourceRun, "w")
	waiting.status, waiting.active = Waiting, 1
	for i := range MaxComparisons + 5 {
		c := m.newLocked("phone", fmt.Sprint("k", i), SourceDirect, "")
		c.status, c.finishedAt = Done, base.Add(time.Duration(i)*time.Second)
		m.byKey[directKey("phone", c.key)] = c
	}
	busy := m.newLocked("phone", "", SourceDirect, "")
	busy.status, busy.finishedAt, busy.active = Done, base.Add(-time.Hour), 1 // its cleanup still runs
	m.pruneLocked()
	left := len(m.byID)
	_, keptWaiting := m.byID[waiting.id]
	_, keptBusy := m.byID[busy.id]
	oldest := m.byKey[directKey("phone", "k0")]
	newest := m.byKey[directKey("phone", fmt.Sprint("k", MaxComparisons+4))]
	m.mu.Unlock()
	if left != MaxComparisons || !keptWaiting || !keptBusy || oldest != nil || newest == nil {
		t.Errorf("%d left, waiting kept %v, busy kept %v, oldest %v", left, keptWaiting, keptBusy, oldest != nil)
	}
}

func internalIDOfRun(m *Manager, runID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byRun[runID].id
}

func TestOwnerIsolation(t *testing.T) {
	m, _ := newTestManager(t, threeAIs(), nil)
	c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
	if _, ok := m.Get("tablet", c.ID); ok {
		t.Error("Get by another device")
	}
	if _, err := m.Retry("tablet", c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Retry: %v", err)
	}
	if _, err := m.Cancel("tablet", c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Cancel: %v", err)
	}
	if _, ok := m.WaitIdle(context.Background(), "tablet", c.ID); ok {
		t.Error("WaitIdle by another device")
	}
	if _, ok := m.Get("phone", "0123"); ok {
		t.Error("unknown id")
	}
	// WaitIdle on a waiting comparison returns with the context.
	fr := &fakeRuns{}
	m2, _ := newTestManager(t, threeAIs(), func(o *Options) { o.Runs = fr })
	fr.set(runs.Run{ID: "r", Status: runs.Running})
	id, _ := m2.Attach(runSpec("phone", "r"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if got, ok := m2.WaitIdle(ctx, "phone", id); !ok || got.Status != Waiting {
		t.Errorf("WaitIdle = %s %v", got.Status, ok)
	}
}

func TestValidateDirect(t *testing.T) {
	ais := threeAIs()
	m, _ := newTestManager(t, ais, nil)
	cases := []struct {
		name     string
		mutate   func(*DirectSpec)
		code     string
		tooLarge bool
	}{
		{"one answer", func(s *DirectSpec) { s.Answers = s.Answers[:1] }, core.ErrCodeNeedTwoAnswers, false},
		{"seven answers", func(s *DirectSpec) {
			for i := range 4 {
				s.Answers = append(s.Answers, DirectAnswer{ID: fmt.Sprint("p", i), Agent: "chatgpt", Via: judge.ViaHandoff, Text: "x"})
			}
		}, CodeBadRequest, false},
		{"bad id", func(s *DirectSpec) { s.Answers[0].ID = "Claude" }, CodeBadRequest, false},
		{"repeated id", func(s *DirectSpec) { s.Answers[1].ID = "claude" }, CodeBadRequest, false},
		{"bad via", func(s *DirectSpec) { s.Answers[0].Via = "paste" }, CodeBadRequest, false},
		{"unknown pc agent", func(s *DirectSpec) { s.Answers[0].Agent = "gemini" }, CodeBadRequest, false},
		{"pc agent twice", func(s *DirectSpec) { s.Answers[1].Agent = "claude" }, CodeBadRequest, false},
		{"unknown handoff app", func(s *DirectSpec) { s.Answers[0].Via, s.Answers[0].Agent = judge.ViaHandoff, "copilot" }, CodeBadRequest, false},
		{"handoff app not as listed", func(s *DirectSpec) { s.Answers[0].Via, s.Answers[0].Agent = judge.ViaHandoff, "ChatGPT" }, CodeBadRequest, false},
		{"flag model", func(s *DirectSpec) { s.Answers[0].Model = "--yolo" }, CodeBadRequest, false},
		{"blank text", func(s *DirectSpec) { s.Answers[0].Text = " \n\t" }, CodeBadRequest, false},
		{"NUL", func(s *DirectSpec) { s.Answers[0].Text = "a\x00b" }, CodeBadRequest, false},
		{"not UTF-8", func(s *DirectSpec) { s.Answers[0].Text = "a\xffb" }, CodeBadRequest, false},
		{"64 KiB text", func(s *DirectSpec) { s.Answers[0].Text = strings.Repeat("a", 64<<10+1) }, CodeBadRequest, true},
		{"blank question", func(s *DirectSpec) { s.Question = "  " }, CodeBadRequest, false},
		{"32 KiB question", func(s *DirectSpec) { s.Question = strings.Repeat("q", 32<<10+1) }, CodeBadRequest, true},
		{"4 earlier", func(s *DirectSpec) { s.Earlier = []string{"a", "b", "c", "d"} }, CodeBadRequest, false},
		{"4 KiB earlier", func(s *DirectSpec) { s.Earlier = []string{strings.Repeat("e", 4<<10+1)} }, CodeBadRequest, true},
		{"unknown judge", func(s *DirectSpec) { s.Judges = []string{"claude", "gemini"} }, CodeBadRequest, false},
		{"judge twice", func(s *DirectSpec) { s.Judges = []string{"claude", "claude"} }, CodeBadRequest, false},
	}
	for _, tc := range cases {
		s := threeAnswers("phone", "")
		tc.mutate(&s)
		_, err := m.Start(s)
		var se *SpecError
		if !errors.As(err, &se) || se.Code != tc.code || se.TooLarge != tc.tooLarge || se.Message == "" {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	// The limits themselves pass, and a pasted answer of a known app does.
	s := threeAnswers("phone", "")
	s.Question = strings.Repeat("q", 32<<10)
	s.Earlier = []string{strings.Repeat("e", 4<<10), "b", "c"}
	s.Answers[0].Text = strings.Repeat("a", 64<<10)
	s.Answers = append(s.Answers, DirectAnswer{ID: "pasted-1", Agent: "gemini", Via: judge.ViaHandoff, Text: "The number is NUM 3."})
	s.Judges = []string{"opencode", "claude"}
	c, err := m.Start(s)
	if err != nil {
		t.Fatalf("limits refused: %v", err)
	}
	c = waitIdle(t, m, "phone", c.ID)
	if c.Answers[3].Family != "google" || c.Status != Done {
		t.Errorf("pasted answer %+v, status %s", c.Answers[3], c.Status)
	}
	for _, r := range c.Requests {
		if r.Judge == "codex" {
			t.Errorf("codex was not among the judges: %+v", r)
		}
	}
	// A pc answer without a model is of no known company (fail closed).
	s = threeAnswers("phone", "")
	s.Answers[2].Model = ""
	c = waitIdle(t, m, "phone", startDirect(t, m, s).ID)
	if c.Answers[2].Family != "" {
		t.Errorf("family %q", c.Answers[2].Family)
	}
	for _, r := range c.Requests {
		if (r.From == "opencode" || r.Against == "opencode") && (r.Judge != "" || r.Error.Code != core.ErrCodeNoJudge) {
			t.Errorf("request with the unknown answer: %+v", r)
		}
	}
}

func TestFolderFails(t *testing.T) {
	ais := threeAIs()
	m, _ := newTestManager(t, ais, nil)
	// A file where the judge folder must be.
	if err := makeFolder(m.o.Gate.Folder[:len(m.o.Gate.Folder)-len(core.JudgeFolderName)-1]); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.o.Gate.Folder, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
	if c.Status != Failed || c.Error.Code != core.ErrCodeInternal || totalCalls(ais) != 0 {
		t.Errorf("comparison %+v, %d calls", c, totalCalls(ais))
	}
}

func TestClose(t *testing.T) {
	ais := threeAIs()
	fakeOf(ais[0]).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		<-ctx.Done()
		return core.JudgeResult{}, core.Fail(core.ErrCodeJudgeTimeout, "")
	}
	m, _ := newTestManager(t, ais, nil)
	c := startDirect(t, m, threeAnswers("phone", ""))
	waitFor(t, "claude to judge", func() bool { return len(fakeOf(ais[0]).judgeCalls()) == 2 })
	start := time.Now()
	m.Close()
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Close took %v", d)
	}
	if got, _ := m.Get("phone", c.ID); got.Status != Done {
		t.Errorf("after Close: %s", got.Status)
	}
	after, err := m.Start(threeAnswers("phone", ""))
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != Failed || after.Error.Code != core.ErrCodeCancelled {
		t.Errorf("Start after Close: %+v", after)
	}
}

func TestLogsCarryNoContent(t *testing.T) {
	ais := threeAIs()
	secret := "SECRET-ANSWER-7781"
	fakeOf(ais[1]).judge = func(ctx context.Context, req core.JudgeRequest, n int) (core.JudgeResult, error) {
		return core.JudgeResult{}, core.Fail(core.ErrCodeJudgeFailed, "AI-MESSAGE-4410 quoting "+secret)
	}
	fr := &fakeRuns{}
	m, buf := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
	s := threeAnswers("phone", "k")
	s.Question = "QUESTION-MARKER-5512 what is it?"
	s.Earlier = []string{"EARLIER-MARKER-9921"}
	s.Answers[0].Text = "It is NUM 1 says " + secret + "."
	c := waitIdle(t, m, "phone", startDirect(t, m, s).ID)
	parts := threeParts()
	parts[0].Answer = "Run answer RUN-MARKER-3141 NUM 1."
	fr.set(endedRun("run-l", parts...))
	rs := runSpec("phone", "run-l")
	rs.Question = "QUESTION-MARKER-5512"
	id, _ := m.Attach(rs)
	waitIdle(t, m, "phone", id)
	if c.Status != Done {
		t.Fatalf("status %s", c.Status)
	}
	logs := buf.String()
	leaks := []string{secret, "AI-MESSAGE-4410", "QUESTION-MARKER-5512", "EARLIER-MARKER-9921", "RUN-MARKER-3141", "NUM 1", `"fake"`, "reason", logx.Redacted}
	for _, a := range ais {
		for _, r := range fakeOf(a).judgeCalls() {
			leaks = append(leaks, nonceOf(r.Prompt))
		}
	}
	for _, leak := range leaks {
		if strings.Contains(logs, leak) {
			t.Errorf("logs hold %q:\n%s", leak, logs)
		}
	}
	if strings.Count(logs, `"msg":"comparison started"`) != 2 || strings.Count(logs, `"msg":"comparison ended"`) != 2 || strings.Count(logs, `"msg":"judge request"`) != 12 {
		t.Errorf("logs:\n%s", logs)
	}
}

func TestClipChars(t *testing.T) {
	if got := clipChars("abc", 3); got != "abc" {
		t.Errorf("%q", got)
	}
	if got := clipChars("abcd", 3); got != "ab…" {
		t.Errorf("%q", got)
	}
	if got := clipChars("a\xffb", 3); got != "ab" {
		t.Errorf("%q", got)
	}
}
