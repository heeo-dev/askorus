package runs

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
)

func wait(t *testing.T, m *Manager, owner, id string) Run {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, ok := m.Wait(ctx, owner, id)
	if !ok {
		t.Fatal("run gone")
	}
	return r
}

func answer(id, text string) func(context.Context) (core.AskResult, error) {
	return func(context.Context) (core.AskResult, error) {
		return core.AskResult{SessionID: id, Answer: text, Model: "m-" + id, InputTokens: 3, OutputTokens: 4}, nil
	}
}

func TestPartsFinishOnTheirOwn(t *testing.T) {
	m := NewManager()
	slow := make(chan struct{})
	r, err := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tier: core.TierStandard, Tasks: []Task{
		{Agent: "claude", Ask: answer("c1", "서울입니다.")},
		{Agent: "codex", Ask: func(ctx context.Context) (core.AskResult, error) {
			<-slow
			return core.AskResult{SessionID: "x1", Answer: "Seoul."}, nil
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Running || len(r.Parts) != 2 || r.Parts[0].Status != Running || r.Tier != core.TierStandard {
		t.Fatalf("start = %+v", r)
	}
	// The fast AI's answer shows while the slow one still works.
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _ := m.Get("phone", r.ID)
		if got.Parts[0].Status == Done {
			if got.Status != Running || got.Parts[1].Status != Running {
				t.Fatalf("run = %+v", got)
			}
			p := got.Parts[0]
			if p.Answer != "서울입니다." || p.Session != "claude:c1" || p.Model != "m-c1" || p.InputTokens != 3 || p.OutputTokens != 4 {
				t.Fatalf("part = %+v", p)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first part never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(slow)
	done := wait(t, m, "phone", r.ID)
	if done.Status != Done || done.Parts[1].Session != "codex:x1" || done.FinishedAt.IsZero() {
		t.Fatalf("done = %+v", done)
	}
}

func TestFailuresAreKeptPerPart(t *testing.T) {
	m := NewManager()
	m.PartLimit = 50 * time.Millisecond
	r, _ := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tasks: []Task{
		{Agent: "a", Ask: func(context.Context) (core.AskResult, error) {
			return core.AskResult{SessionID: "s"}, core.Fail(core.ErrCodeAskFailed, "rate limited")
		}},
		{Agent: "b", Ask: func(ctx context.Context) (core.AskResult, error) {
			<-ctx.Done()
			return core.AskResult{}, ctx.Err()
		}},
		{Agent: "c", Ask: func(context.Context) (core.AskResult, error) { return core.AskResult{SessionID: "e"}, nil }},
		{Agent: "d", Ask: func(context.Context) (core.AskResult, error) { panic("boom") }},
	}})
	done := wait(t, m, "phone", r.ID)
	want := []string{core.ErrCodeAskFailed, core.ErrCodeAskTimeout, core.ErrCodeAskEmpty, "internal"}
	for i, p := range done.Parts {
		if p.Status != Failed || p.Error == nil || p.Error.Code != want[i] {
			t.Errorf("part %d = %+v", i, p)
		}
	}
	if done.Parts[0].Error.Message != "rate limited" || done.Parts[0].Session != "a:s" {
		t.Errorf("failed part keeps its message and session: %+v", done.Parts[0])
	}
	if done.Status != Failed {
		t.Errorf("no answer at all: status = %s", done.Status)
	}
}

func TestSameKeyReturnsTheFirstRun(t *testing.T) {
	m := NewManager()
	calls := 0
	task := []Task{{Agent: "a", Ask: func(context.Context) (core.AskResult, error) {
		calls++
		return core.AskResult{SessionID: "1", Answer: "ok"}, nil
	}}}
	r1, _ := m.Start(Spec{Owner: "phone", Key: "k1", Mode: core.ModeAsk, Tasks: task})
	wait(t, m, "phone", r1.ID)
	r2, _ := m.Start(Spec{Owner: "phone", Key: "k1", Mode: core.ModeAsk, Tasks: task})
	if r2.ID != r1.ID || calls != 1 {
		t.Fatalf("retry started a new run: %s %s calls=%d", r1.ID, r2.ID, calls)
	}
	r3, _ := m.Start(Spec{Owner: "other", Key: "k1", Mode: core.ModeAsk, Tasks: task})
	if r3.ID == r1.ID {
		t.Fatal("another device shares the key")
	}
	if _, ok := m.Get("other", r1.ID); ok {
		t.Fatal("another device reads the run")
	}
}

func TestTooManyRunning(t *testing.T) {
	m := NewManager()
	block := make(chan struct{})
	defer close(block)
	task := []Task{{Agent: "a", Ask: func(context.Context) (core.AskResult, error) {
		<-block
		return core.AskResult{Answer: "x"}, nil
	}}}
	for i := 0; i < MaxRunning; i++ {
		if _, err := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tasks: task}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tasks: task}); err != ErrTooMany {
		t.Fatalf("err = %v", err)
	}
}

func TestOneFollowUpPerSession(t *testing.T) {
	m := NewManager()
	release := make(chan struct{})
	slow := Task{Agent: "claude", Session: "claude:X", Ask: func(context.Context) (core.AskResult, error) {
		<-release
		return core.AskResult{SessionID: "X", Answer: "a"}, nil
	}}
	r, err := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tasks: []Task{slow}})
	if err != nil {
		t.Fatal(err)
	}
	other := Task{Agent: "codex", Ask: answer("y", "b")}
	if _, err := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tasks: []Task{other, slow}}); err != ErrSessionBusy {
		t.Fatalf("second follow-up on the same session: %v", err)
	}
	// A new conversation with the same AI is fine meanwhile.
	if _, err := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tasks: []Task{{Agent: "claude", Ask: answer("z", "c")}}}); err != nil {
		t.Fatalf("new conversation: %v", err)
	}
	close(release)
	wait(t, m, "phone", r.ID)
	if _, err := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tasks: []Task{{Agent: "claude", Session: "claude:X", Ask: answer("X", "d")}}}); err != nil {
		t.Fatalf("after the first ended: %v", err)
	}
}

func TestCloseEndsRunningParts(t *testing.T) {
	m := NewManager()
	r, _ := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tasks: []Task{{Agent: "a", Ask: func(ctx context.Context) (core.AskResult, error) {
		<-ctx.Done()
		return core.AskResult{}, ctx.Err()
	}}}})
	m.Close()
	if done := wait(t, m, "phone", r.ID); done.Status != Failed {
		t.Fatalf("after close = %+v", done)
	}
}

func TestFinishedRunsExpire(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	m := NewManager()
	m.Now = func() time.Time { return now }
	r, _ := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tasks: []Task{{Agent: "a", Ask: answer("1", "ok")}}})
	wait(t, m, "phone", r.ID)
	now = now.Add(KeepFor + time.Second)
	if _, ok := m.Get("phone", r.ID); ok {
		t.Fatal("finished run kept past KeepFor")
	}
}

// endLog records OnPart and OnEnd calls, for the OnEnd tests.
type endLog struct {
	mu    sync.Mutex
	parts int   // OnPart calls so far
	ends  []Run // OnEnd snapshots
	seen  []int // OnPart calls when each OnEnd came
	got   chan struct{}
}

func newEndLog(m *Manager) *endLog {
	l := &endLog{got: make(chan struct{}, 16)}
	m.OnPart = func(string, Part) {
		l.mu.Lock()
		l.parts++
		l.mu.Unlock()
	}
	m.OnEnd = func(r Run) {
		l.mu.Lock()
		l.ends = append(l.ends, r)
		l.seen = append(l.seen, l.parts)
		l.mu.Unlock()
		l.got <- struct{}{}
	}
	return l
}

func (l *endLog) wait(t *testing.T) {
	t.Helper()
	select {
	case <-l.got:
	case <-time.After(5 * time.Second):
		t.Fatal("OnEnd never came")
	}
}

func (l *endLog) snapshot() (ends []Run, seen []int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Run(nil), l.ends...), append([]int(nil), l.seen...)
}

// OnEnd comes once per run, after the last part's OnPart, with every part
// filled in, also when the parts end at the same moment (P2-3: the
// comparison starts there).
func TestOnEndOnceAfterTheLastPart(t *testing.T) {
	for _, failLast := range []bool{false, true} {
		m := NewManager()
		l := newEndLog(m)
		var ready sync.WaitGroup
		ready.Add(3)
		gate := make(chan struct{})
		task := func(agent, text string) Task {
			return Task{Agent: agent, Ask: func(context.Context) (core.AskResult, error) {
				ready.Done()
				<-gate // the three end together
				if text == "" {
					return core.AskResult{SessionID: agent}, core.Fail(core.ErrCodeAskFailed, "limit")
				}
				return core.AskResult{SessionID: agent, Answer: text, Model: "m"}, nil
			}}
		}
		third := "c"
		if failLast {
			third = ""
		}
		r, err := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tasks: []Task{task("a", "x"), task("b", "y"), task("c", third)}})
		if err != nil {
			t.Fatal(err)
		}
		ready.Wait()
		ends, _ := l.snapshot()
		if len(ends) != 0 {
			t.Fatalf("OnEnd before the parts ended: %+v", ends)
		}
		close(gate)
		l.wait(t)
		wait(t, m, "phone", r.ID)
		time.Sleep(50 * time.Millisecond) // a second OnEnd would come now
		ends, seen := l.snapshot()
		if len(ends) != 1 || seen[0] != 3 {
			t.Fatalf("OnEnd calls = %d, OnPart calls before it = %v", len(ends), seen)
		}
		e := ends[0]
		if e.ID != r.ID || e.Status != Done || e.FinishedAt.IsZero() || len(e.Parts) != 3 {
			t.Fatalf("OnEnd run = %+v", e)
		}
		for _, p := range e.Parts {
			if p.Status == Running || p.Session == "" {
				t.Errorf("part not filled in: %+v", p)
			}
		}
		if failLast && (e.Parts[2].Status != Failed || e.Parts[2].Error == nil || e.Parts[2].Error.Code != core.ErrCodeAskFailed) {
			t.Errorf("failed part = %+v", e.Parts[2])
		}
	}
}

// A run in which every part failed ends failed, and OnEnd says so.
func TestOnEndFailedRun(t *testing.T) {
	m := NewManager()
	l := newEndLog(m)
	fail := func(context.Context) (core.AskResult, error) {
		return core.AskResult{}, core.Fail(core.ErrCodeAskFailed, "")
	}
	m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tasks: []Task{{Agent: "a", Ask: fail}, {Agent: "b", Ask: fail}}})
	l.wait(t)
	ends, _ := l.snapshot()
	if len(ends) != 1 || ends[0].Status != Failed || ends[0].Parts[0].Status != Failed || ends[0].Parts[1].Status != Failed {
		t.Fatalf("OnEnd = %+v", ends)
	}
}

// A run without parts ends at once, and still gets its OnEnd, from a
// goroutine once Start has let the lock go: OnEnd may call the Manager.
func TestOnEndZeroTasks(t *testing.T) {
	m := NewManager()
	got := make(chan Run, 2)
	m.OnEnd = func(r Run) {
		if _, ok := m.Get("phone", r.ID); !ok { // takes the lock: Start must not hold it
			t.Error("the ended run is not readable from OnEnd")
		}
		got <- r
	}
	r, err := m.Start(Spec{Owner: "phone", Key: "k", Mode: core.ModeAsk})
	if err != nil || r.Status != Failed {
		t.Fatalf("start = %+v, %v", r, err)
	}
	select {
	case e := <-got:
		if e.ID != r.ID || e.Status != Failed || len(e.Parts) != 0 {
			t.Fatalf("OnEnd = %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no OnEnd for a run without parts")
	}
	// The same key again is the same run: no second OnEnd.
	if again, _ := m.Start(Spec{Owner: "phone", Key: "k", Mode: core.ModeAsk}); again.ID != r.ID {
		t.Fatalf("same key gave a new run")
	}
	select {
	case e := <-got:
		t.Fatalf("second OnEnd: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

// A panicking OnEnd or OnPart is recovered: the next run still ends and
// is told, and the Manager's lock is free.
func TestOnEndPanicIsRecovered(t *testing.T) {
	m := NewManager()
	var calls atomic.Int32
	m.OnPart = func(string, Part) { panic("log broke") }
	m.OnEnd = func(Run) {
		calls.Add(1)
		panic("hook broke")
	}
	r1, _ := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tasks: []Task{{Agent: "a", Ask: answer("1", "ok")}}})
	wait(t, m, "phone", r1.ID)
	m.Start(Spec{Owner: "phone", Mode: core.ModeAsk}) // zero tasks: OnEnd from Start's goroutine
	r2, err := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tasks: []Task{{Agent: "a", Ask: answer("2", "ok")}}})
	if err != nil {
		t.Fatal(err)
	}
	if done := wait(t, m, "phone", r2.ID); done.Status != Done {
		t.Fatalf("next run = %+v", done)
	}
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("OnEnd calls = %d, want 3", calls.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
}
