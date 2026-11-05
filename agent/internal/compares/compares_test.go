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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
	"github.com/asksemble/asksemble/agent/internal/logx"
	"github.com/asksemble/asksemble/agent/internal/runs"
)

// ---- fakes ----------------------------------------------------------------

// fakeAI is an AI program in process that names its models' companies
// (core.Judger), so it passes the gate. It neither asks nor writes:
// writerAI (synth_test.go) does.
type fakeAI struct {
	name   string
	family string            // the company of every model it runs
	fams   map[string]string // models of another company, or "" for one it cannot tell

	mu       sync.Mutex
	version  string
	avail    bool
	caps     []core.Capability
	cleanups [][]string
	// cleanup replaces CleanupJudge's result.
	cleanup func(ctx context.Context, folder string, refs []string) error
}

func newFake(name, family string) *fakeAI {
	return &fakeAI{name: name, family: family, version: "1.0.0", avail: true, caps: []core.Capability{}}
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

// cleanerAI is a fakeAI whose one-off calls leave something behind, as
// OpenCode's do.
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

// plainAI is no core.Judger: it cannot name its models' companies, so it
// never passes the gate.
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

// threeAnswers: claude and codex say 1, opencode 2.
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

// listedOf is comparison id's AIs the earlier questions may go to.
func listedOf(m *Manager, id string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.byID[id].spec.Listed)
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
	m, buf := newTestManager(t, threeAIs(), nil)
	c := startDirect(t, m, threeAnswers("phone", ""))
	if c.Status != Running || c.Source != SourceDirect || c.Engine != "pc" || c.RunID != "" || len(c.ID) != 32 {
		t.Errorf("first snapshot = %+v", c)
	}
	c = waitIdle(t, m, "phone", c.ID)
	if c.Status != Done || c.Error != nil {
		t.Fatalf("status %s %+v", c.Status, c.Error)
	}
	// Nothing is judged since C6d (#24): the judging's lists stay empty.
	if len(c.Requests) != 0 || len(c.Judgments) != 0 || len(c.Items) != 0 || c.Counts != (judge.Counts{}) {
		t.Errorf("requests %d, judgments %d, items %d, counts %+v", len(c.Requests), len(c.Judgments), len(c.Items), c.Counts)
	}
	families := []string{"anthropic", "openai", "deepseek"}
	if len(c.Answers) != 3 {
		t.Fatalf("%d answers", len(c.Answers))
	}
	for i, a := range c.Answers {
		if a.Text != "" || a.Status != judge.AnswerReady || a.Units == nil || len(a.Units) != 0 || a.NotJudged != 0 || a.Family != families[i] || a.Via != judge.ViaPC {
			t.Errorf("answer %+v", a)
		}
	}
	if s := synthOf(t, c); s.Status != SynthOff {
		t.Errorf("synthesis %+v, want off", s)
	}
	// With no AIs named, every AI that passes the gate is listed.
	if listed := listedOf(m, c.ID); !slices.Equal(listed, []string{"claude", "codex", "opencode"}) {
		t.Errorf("listed %v", listed)
	}
	logs := buf.String()
	for _, want := range []string{`"msg":"comparison started"`, `"source":"direct"`, `"ais":3`, `"msg":"comparison ended"`, `"status":"done"`} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %s:\n%s", want, logs)
		}
	}
	if strings.Contains(logs, "judge request") {
		t.Errorf("logs hold a judge request:\n%s", logs)
	}
}

func TestSnapshotJSON(t *testing.T) {
	m, _ := newTestManager(t, threeAIs(), nil)
	c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`"comparisonId":"`, `"status":"done"`, `"source":"direct"`, `"engine":"pc"`, `"answers":[`, `"units":[]`, `"notJudged":0`,
		`"requests":[]`, `"judgments":[]`, `"items":[]`, `"counts":{"differ":0,"check":0,"agree":0,"onlyOne":0,"notCompared":0,"notJudged":0}`,
		`"synthesis":{"status":"off","judged":false}`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON lacks %s: %s", want, s)
		}
	}
	// The answers' text stays in the agent; a done comparison has no error.
	for _, leak := range []string{"The number is", `"reason"`, `"runId"`, `"error"`, `"Text"`, `"Tail"`, `"Before"`} {
		if strings.Contains(s, leak) {
			t.Errorf("JSON holds %s: %s", leak, s)
		}
	}
	// An empty comparison's slices are [] too, and its error has the fixed
	// message.
	empty := (&Manager{}).snapshotLocked(&comparison{id: "x", status: Failed, err: failure(core.ErrCodeNeedTwoAnswers, "")})
	b, _ = json.Marshal(empty)
	for _, want := range []string{
		`"answers":[]`, `"requests":[]`, `"judgments":[]`, `"items":[]`,
		`"error":{"code":"need_two_answers","message":"` + messages[core.ErrCodeNeedTwoAnswers] + `"}`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("empty JSON lacks %s: %s", want, b)
		}
	}
}

func TestAttachExactlyOnce(t *testing.T) {
	for _, when := range []string{"before", "during", "after"} {
		t.Run(when, func(t *testing.T) {
			_, _, _, ais := threeWriters()
			fr := &fakeRuns{}
			m, buf := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
			run := endedRun("run-1", threeParts()...)
			running := run
			running.Status = runs.Running
			spec := runSpec("phone", run.ID)
			spec.Synth = &SynthSpec{Agent: "claude"}
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
						ids[i], errs[i] = m.Attach(spec)
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
			if s := synthOf(t, c); s.Status != SynthDone || synthTotal(ais) != 1 {
				t.Errorf("synthesis %+v, %d writer calls, want done and 1", s, synthTotal(ais))
			}
			logs := buf.String()
			if strings.Count(logs, `"msg":"comparison started"`) != 1 || strings.Count(logs, `"msg":"comparison ended"`) != 1 {
				t.Errorf("started or ended more than once:\n%s", logs)
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
	m, buf := newTestManager(t, threeAIs(), nil)
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
	if n := strings.Count(buf.String(), `"msg":"comparison started"`); n != 4 {
		t.Errorf("%d comparisons started, want 4", n)
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
	t.Run("missing answer", func(t *testing.T) {
		fr := &fakeRuns{}
		m, _ := newTestManager(t, threeAIs(), func(o *Options) { o.Runs = fr })
		parts := threeParts()
		parts[2] = runs.Part{Agent: "opencode", Status: runs.Failed, Error: &runs.Error{Code: core.ErrCodeAskFailed, Message: "usage limit"}}
		fr.set(endedRun("run-2", parts...))
		id, err := m.Attach(runSpec("phone", "run-2"))
		if err != nil {
			t.Fatal(err)
		}
		c := waitIdle(t, m, "phone", id)
		if c.Status != Done || c.Error != nil || len(c.Answers) != 3 {
			t.Fatalf("comparison %s %+v, %d answers", c.Status, c.Error, len(c.Answers))
		}
		// A failed part names no model, so its company is unknown.
		if a := c.Answers[2]; a.ID != "opencode" || a.Status != judge.AnswerMissing || a.Units == nil || len(a.Units) != 0 || a.Family != "" {
			t.Errorf("missing answer %+v", a)
		}
		if a := c.Answers[0]; a.ID != "claude" || a.Status != judge.AnswerReady || a.Family != "anthropic" || a.Model != "claude-m" {
			t.Errorf("ready answer %+v", a)
		}
		// An AI whose answer is missing is still listed.
		if listed := listedOf(m, id); !slices.Equal(listed, []string{"claude", "codex", "opencode"}) {
			t.Errorf("listed %v", listed)
		}
	})
	t.Run("listed and families of AIs that fail the gate", func(t *testing.T) {
		ais := threeAIs()
		ais = append(ais, newFake("gemini", "google"))
		fakeOf(ais[3]).setAvailable(false) // listed, but fails the gate now
		fakeOf(ais[1]).version = "0.9.0"   // codex answered but fails the gate
		fr := &fakeRuns{}
		m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
		fr.set(endedRun("run-3", threeParts()...))
		s := runSpec("phone", "run-3")
		s.Judges = []string{"gemini", "opencode", "codex"} // claude was not listed
		id, _ := m.Attach(s)
		c := waitIdle(t, m, "phone", id)
		if c.Status != Done {
			t.Fatalf("status %s %+v", c.Status, c.Error)
		}
		if c.Answers[1].Family != "openai" {
			t.Errorf("codex answer family %q: it comes from its adapter even when codex fails the gate", c.Answers[1].Family)
		}
		if listed := listedOf(m, id); !slices.Equal(listed, []string{"opencode"}) {
			t.Errorf("listed %v, want only opencode: no AI that fails the gate or was not listed", listed)
		}
		// Without AIs listed, the asked AIs that pass the gate are.
		fr.set(endedRun("run-3b", threeParts()...))
		s = runSpec("phone", "run-3b")
		s.Judges = nil
		id, _ = m.Attach(s)
		waitIdle(t, m, "phone", id)
		if listed := listedOf(m, id); !slices.Equal(listed, []string{"claude", "opencode"}) {
			t.Errorf("listed %v, want the asked AIs that pass the gate", listed)
		}
	})
	t.Run("off", func(t *testing.T) {
		_, _, _, ais := threeWriters()
		fr := &fakeRuns{}
		m, buf := newTestManager(t, ais, func(o *Options) { o.Runs = fr; o.Gate.Settings.Off = true })
		fr.set(endedRun("run-4", threeParts()...))
		rs := runSpec("phone", "run-4")
		rs.Synth = &SynthSpec{Agent: "claude"}
		id, err := m.Attach(rs)
		if err != nil {
			t.Fatal(err)
		}
		c := waitIdle(t, m, "phone", id)
		// The comparison has its answers; no AI may write the combined answer.
		if s := synthOf(t, c); c.Status != Done || c.Error != nil || s.Status != SynthFailed || s.Error == nil || s.Error.Code != core.ErrCodeNoJudge || synthTotal(ais) != 0 {
			t.Fatalf("comparison %s %+v, synthesis %+v", c.Status, c.Error, s)
		}
		if listed := listedOf(m, id); len(listed) != 0 {
			t.Errorf("listed %v", listed)
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
		fr := &fakeRuns{}
		m, _ := newTestManager(t, threeAIs(), func(o *Options) { o.Runs = fr })
		parts := threeParts()
		parts[0] = runs.Part{Agent: "claude", Status: runs.Failed, Error: &runs.Error{Code: core.ErrCodeAskTimeout}}
		parts[1] = runs.Part{Agent: "codex", Status: runs.Done, Answer: "  \n "}
		fr.set(endedRun("run-5", parts...))
		id, _ := m.Attach(runSpec("phone", "run-5"))
		c := waitIdle(t, m, "phone", id)
		if c.Status != Failed || c.Error == nil || c.Error.Code != core.ErrCodeNeedTwoAnswers || c.Error.Message != messages[core.ErrCodeNeedTwoAnswers] || len(c.Answers) != 3 {
			t.Fatalf("comparison %+v", c)
		}
		if c.Answers[1].Status != judge.AnswerMissing {
			t.Errorf("a blank answer is missing: %+v", c.Answers[1])
		}
	})
}

func TestPanics(t *testing.T) {
	t.Run("family", func(t *testing.T) {
		// A Family that panics while the answers are taken fails the
		// comparison internal; the panic value reaches no log.
		ais := threeAIs()
		ais[0] = &panicFamily{fakeAI: newFake("claude", "anthropic")}
		m, buf := newTestManager(t, ais, nil)
		c := waitIdle(t, m, "phone", startDirect(t, m, threeAnswers("phone", "")).ID)
		if c.Status != Failed || c.Error == nil || c.Error.Code != core.ErrCodeInternal || c.Error.Message != messages[core.ErrCodeInternal] {
			t.Errorf("comparison %+v", c)
		}
		if strings.Contains(buf.String(), "family bug") {
			t.Error("a panic value reached the log")
		}
	})
	t.Run("info", func(t *testing.T) {
		// An adapter whose Info panics does not pass the gate; the others do.
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
		// A writer's cleanup that panics is logged with its code only.
		_, _, oc, ais := threeWriters()
		oc.cleanup = func(ctx context.Context, folder string, refs []string) error { panic("cleanup bug") }
		m, buf := newTestManager(t, ais, nil)
		c := waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "opencode"})).ID)
		if s := synthOf(t, c); c.Status != Done || s.Status != SynthDone || len(cleanerAI{oc.fakeAI}.cleanupCalls()) != 1 {
			t.Errorf("status %s, synthesis %+v, %d cleanups", c.Status, s, len(cleanerAI{oc.fakeAI}.cleanupCalls()))
		}
		if logs := buf.String(); !strings.Contains(logs, `"msg":"writer cleanup failed","writer":"opencode","code":"internal"`) || strings.Contains(logs, "cleanup bug") {
			t.Errorf("logs: %s", logs)
		}
	})
}

// panicInfo is an AI whose Info panics.
type panicInfo struct{ name string }

func (p *panicInfo) Agent() string                         { return p.name }
func (p *panicInfo) Info(context.Context) core.AdapterInfo { panic("info bug") }
func (p *panicInfo) ListSessions(context.Context) (core.ListResult, error) {
	return core.ListResult{}, nil
}
func (p *panicInfo) Family(string) string { return "google" }

// panicFamily panics when asked for the company of the answer model
// claude-m, until it is fixed.
type panicFamily struct {
	*fakeAI
	fixed atomic.Bool
}

func (p *panicFamily) Family(model string) string {
	if model == "claude-m" && !p.fixed.Load() {
		panic("family bug")
	}
	return p.fakeAI.Family(model)
}

// Retry has nothing to retry in a comparison that took its answers: done,
// failed with need_two_answers, or with its combined answer failed, it
// comes back as it is and no AI is called. One whose run was gone looks
// the run up again.
func TestRetry(t *testing.T) {
	cl, _, _, ais := threeWriters()
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		return core.SynthResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeFailed, fakecli.UsageLimitClaude)
	})
	fr := &fakeRuns{}
	m, buf := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
	started := func() int { return strings.Count(buf.String(), `"msg":"comparison started"`) }

	c := waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID)
	if s := synthOf(t, c); c.Status != Done || s.Status != SynthFailed || len(cl.synthCalls()) != 1 {
		t.Fatalf("first: %s %+v", c.Status, s)
	}
	r, err := m.Retry("phone", c.ID)
	if err != nil || r.Status != Done || r.Error != nil || synthOf(t, r).Status != SynthFailed || synthOf(t, r).Error.Message != fakecli.UsageLimitClaude {
		t.Errorf("Retry of a done comparison = %s %v %+v", r.Status, err, r.Synthesis)
	}

	parts := threeParts()
	parts[0] = runs.Part{Agent: "claude", Status: runs.Failed, Error: &runs.Error{Code: core.ErrCodeAskTimeout}}
	parts[1] = runs.Part{Agent: "codex", Status: runs.Failed, Error: &runs.Error{Code: core.ErrCodeAskFailed}}
	fr.set(endedRun("run-1", parts...))
	one, _ := m.Attach(runSpec("phone", "run-1"))
	if got := waitIdle(t, m, "phone", one); got.Status != Failed || got.Error == nil || got.Error.Code != core.ErrCodeNeedTwoAnswers {
		t.Fatalf("one answer: %s %+v", got.Status, got.Error)
	}
	if r, err := m.Retry("phone", one); err != nil || r.Status != Failed || r.Error == nil || r.Error.Code != core.ErrCodeNeedTwoAnswers {
		t.Errorf("Retry of need_two_answers = %+v %v", r, err)
	}
	waitIdle(t, m, "phone", c.ID)
	waitIdle(t, m, "phone", one)
	if n := started(); n != 2 || len(cl.synthCalls()) != 1 {
		t.Errorf("%d rounds, %d writer calls: a Retry with nothing to retry started something", n, len(cl.synthCalls()))
	}

	// The run is gone (fr holds run-1 only), then it is found.
	gone, _ := m.Attach(runSpec("phone", "run-2"))
	if got := waitIdle(t, m, "phone", gone); got.Status != Failed || got.Error == nil || got.Error.Code != core.ErrCodeInternal || got.Error.Message != msgRunGone {
		t.Fatalf("run gone: %s %+v", got.Status, got.Error)
	}
	fr.set(endedRun("run-2", threeParts()...))
	if _, err := m.Retry("phone", gone); err != nil {
		t.Fatal(err)
	}
	if got := waitIdle(t, m, "phone", gone); got.Status != Done || got.Error != nil || started() != 3 {
		t.Errorf("after the run was found: %s %+v", got.Status, got.Error)
	}

	if _, err := m.Retry("tablet", c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another device: %v", err)
	}
	if _, err := m.Retry("phone", "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
}

// Retry is ErrRunning while the comparison waits for its run or takes its
// answers.
func TestRetryWhileRunning(t *testing.T) {
	ais := threeAIs()
	pb := &planBlock{fakeAI: newFake("claude", "anthropic"), entered: make(chan struct{}), release: make(chan struct{})}
	ais[0] = pb
	fr := &fakeRuns{}
	m, _ := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
	fr.set(runs.Run{ID: "run-w", Status: runs.Running})
	waiting, _ := m.Attach(runSpec("phone", "run-w"))
	if _, err := m.Retry("phone", waiting); !errors.Is(err, ErrRunning) {
		t.Errorf("Retry while waiting: %v", err)
	}
	c := startDirect(t, m, threeAnswers("phone", ""))
	select {
	case <-pb.entered:
	case <-time.After(10 * time.Second):
		close(pb.release)
		t.Fatal("the round did not take the answers")
	}
	if _, err := m.Retry("phone", c.ID); !errors.Is(err, ErrRunning) {
		t.Errorf("Retry while running: %v", err)
	}
	close(pb.release)
	waitIdle(t, m, "phone", c.ID)
}

func TestCancel(t *testing.T) {
	t.Run("waiting never starts", func(t *testing.T) {
		fr := &fakeRuns{}
		m, buf := newTestManager(t, threeAIs(), func(o *Options) { o.Runs = fr })
		started := func() int { return strings.Count(buf.String(), `"msg":"comparison started"`) }
		run := endedRun("run-c", threeParts()...)
		running := run
		running.Status = runs.Running
		fr.set(running)
		id, _ := m.Attach(runSpec("phone", run.ID))
		m.RunEnded(running) // a run that has not ended starts nothing
		if c, _ := m.Get("phone", id); c.Status != Waiting || started() != 0 {
			t.Fatalf("status %s", c.Status)
		}
		c, err := m.Cancel("phone", id)
		if err != nil || c.Status != Failed || c.Error.Code != core.ErrCodeCancelled || c.Error.Message != msgCancelledWaiting {
			t.Fatalf("Cancel = %+v %v", c, err)
		}
		fr.set(run)
		m.RunEnded(run)
		c = waitIdle(t, m, "phone", id)
		if c.Status != Failed || started() != 0 {
			t.Errorf("a cancelled comparison started: %s", c.Status)
		}
		// Retry waits for the run again, and starts as it has ended.
		if _, err := m.Retry("phone", id); err != nil {
			t.Fatal(err)
		}
		if c = waitIdle(t, m, "phone", id); c.Status != Done || c.Error != nil || len(c.Answers) != 3 || started() != 1 {
			t.Errorf("after retry: %s %+v, %d answers", c.Status, c.Error, len(c.Answers))
		}
		// Cancel of an ended comparison changes nothing.
		if again, err := m.Cancel("phone", id); err != nil || again.Status != Done || again.Error != nil {
			t.Errorf("Cancel again = %s %v", again.Status, err)
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
	if a := c.Answers[3]; a.Family != "google" || a.Via != judge.ViaHandoff || a.Status != judge.AnswerReady || c.Status != Done {
		t.Errorf("pasted answer %+v, status %s", a, c.Status)
	}
	if listed := listedOf(m, c.ID); !slices.Equal(listed, []string{"opencode", "claude"}) {
		t.Errorf("listed %v, want the AIs as listed", listed)
	}
	// A pc answer without a model is of no known company (fail closed).
	s = threeAnswers("phone", "")
	s.Answers[2].Model = ""
	c = waitIdle(t, m, "phone", startDirect(t, m, s).ID)
	if c.Answers[2].Family != "" || c.Status != Done {
		t.Errorf("answer %+v, status %s", c.Answers[2], c.Status)
	}
}

// A judge folder that cannot be made fails what runs in it, the combined
// answer and refining, internal and without a call; the comparison itself
// has its answers and is done.
func TestFolderFails(t *testing.T) {
	cl, _, _, ais := threeWriters()
	m, _ := newTestManager(t, ais, nil)
	// A file where the judge folder must be.
	if err := makeFolder(m.o.Gate.Folder[:len(m.o.Gate.Folder)-len(core.JudgeFolderName)-1]); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.o.Gate.Folder, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c := waitIdle(t, m, "phone", startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"})).ID)
	if s := synthOf(t, c); c.Status != Done || s.Status != SynthFailed || s.Error.Code != core.ErrCodeInternal || synthTotal(ais) != 0 {
		t.Errorf("comparison %s, synthesis %+v, %d calls", c.Status, s, synthTotal(ais))
	}
	if r := m.Refine(context.Background(), RefineSpec{Question: "q", Asked: []string{"claude"}}); r.Status != RefineFailed || r.Error == nil || r.Error.Code != core.ErrCodeInternal || len(cl.synthCalls()) != 0 {
		t.Errorf("refine %+v, %d calls", r, len(cl.synthCalls()))
	}
}

// Close ends a writer that runs and waits for it a short while; a Start
// after it ends at once, cancelled.
func TestClose(t *testing.T) {
	cl, _, _, ais := threeWriters()
	entered := make(chan struct{})
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		close(entered)
		<-ctx.Done()
		return core.SynthResult{}, core.Fail(core.ErrCodeJudgeTimeout, "")
	})
	m, _ := newTestManager(t, ais, nil)
	c := startDirect(t, m, withSynth(threeAnswers("phone", ""), &SynthSpec{Agent: "claude"}))
	<-entered
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
	if after.Status != Failed || after.Error.Code != core.ErrCodeCancelled || after.Error.Message != msgClosed {
		t.Errorf("Start after Close: %+v", after)
	}
}

func TestLogsCarryNoContent(t *testing.T) {
	cl, _, _, ais := threeWriters()
	secret := "SECRET-ANSWER-7781"
	cl.setSynth(func(ctx context.Context, req core.SynthRequest, n int) (core.SynthResult, error) {
		return core.SynthResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeFailed, "AI-MESSAGE-4410 quoting "+secret)
	})
	fr := &fakeRuns{}
	m, buf := newTestManager(t, ais, func(o *Options) { o.Runs = fr })
	s := threeAnswers("phone", "k")
	s.Question = "QUESTION-MARKER-5512 what is it?"
	s.Earlier = []string{"EARLIER-MARKER-9921"}
	s.Answers[0].Text = "It is NUM 1 says " + secret + "."
	s.Synth = &SynthSpec{Agent: "claude"}
	c := waitIdle(t, m, "phone", startDirect(t, m, s).ID)
	parts := threeParts()
	parts[0].Answer = "Run answer RUN-MARKER-3141 NUM 1."
	fr.set(endedRun("run-l", parts...))
	rs := runSpec("phone", "run-l")
	rs.Question = "QUESTION-MARKER-5512"
	rs.Earlier = []string{"EARLIER-MARKER-9921"}
	rs.Synth = &SynthSpec{Agent: "claude"}
	id, _ := m.Attach(rs)
	waitIdle(t, m, "phone", id)
	if s := synthOf(t, c); c.Status != Done || s.Error == nil || s.Error.Code != core.ErrCodeJudgeFailed || len(cl.synthCalls()) != 2 {
		t.Fatalf("status %s, synthesis %+v, %d writer calls", c.Status, s, len(cl.synthCalls()))
	}
	logs := buf.String()
	leaks := []string{secret, "AI-MESSAGE-4410", "QUESTION-MARKER-5512", "EARLIER-MARKER-9921", "RUN-MARKER-3141", "NUM 1", `"fake"`, "reason", logx.Redacted}
	for _, call := range cl.synthCalls() {
		leaks = append(leaks, nonceOf(call.Prompt))
	}
	for _, leak := range leaks {
		if strings.Contains(logs, leak) {
			t.Errorf("logs hold %q:\n%s", leak, logs)
		}
	}
	if strings.Count(logs, `"msg":"comparison started"`) != 2 || strings.Count(logs, `"msg":"comparison ended"`) != 2 ||
		strings.Count(logs, `"msg":"synthesis ended"`) != 2 || strings.Contains(logs, "judge request") {
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
