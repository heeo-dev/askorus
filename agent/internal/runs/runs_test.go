package runs

import (
	"context"
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
