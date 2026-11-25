package runs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// R-186 (2026-11-25): the wait for the refined question ran inside the
// AI's own time, so refining shortened the three minutes. Prepare runs
// before the part's limit starts; its error fails the part without asking.
func TestPrepareIsOutsideThePartLimit(t *testing.T) {
	m := NewManager()
	m.PartLimit = 300 * time.Millisecond
	asked := make(chan struct{}, 1)
	r, _ := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tasks: []Task{
		{Agent: "a", Prepare: func(context.Context) error { time.Sleep(500 * time.Millisecond); return nil },
			Ask: func(ctx context.Context) (core.AskResult, error) {
				time.Sleep(100 * time.Millisecond)
				return core.AskResult{SessionID: "s", Answer: "yes"}, ctx.Err()
			}},
		{Agent: "b", Prepare: func(context.Context) error { return core.Fail(core.ErrCodeNoImages, "") },
			Ask: func(context.Context) (core.AskResult, error) {
				asked <- struct{}{}
				return core.AskResult{Answer: "no"}, nil
			}},
		{Agent: "c", Prepare: func(context.Context) error { panic("boom") },
			Ask: func(context.Context) (core.AskResult, error) {
				asked <- struct{}{}
				return core.AskResult{Answer: "no"}, nil
			}},
	}})
	done := wait(t, m, "phone", r.ID)
	if p := done.Parts[0]; p.Status != Done || p.Answer != "yes" || p.Ms < 500 {
		t.Errorf("a slow prepare cost the AI its time: %+v", p)
	}
	if p := done.Parts[1]; p.Status != Failed || p.Error == nil || p.Error.Code != core.ErrCodeNoImages {
		t.Errorf("failed prepare = %+v", p)
	}
	if p := done.Parts[2]; p.Status != Failed || p.Error == nil || p.Error.Code != "internal" {
		t.Errorf("panicking prepare = %+v", p)
	}
	select {
	case <-asked:
		t.Error("a part whose prepare failed asked anyway")
	default:
	}
}

// #73 (2호's review): refining that moves on, and a part that ends, each
// raise the run's rev, so the phone's short "unchanged" never hides them.
func TestRefineAndPartsRaiseTheRev(t *testing.T) {
	m := NewManager()
	gate := make(chan struct{})
	r, _ := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tasks: []Task{
		{Agent: "a", Ask: func(context.Context) (core.AskResult, error) { <-gate; return core.AskResult{Answer: "x"}, nil }},
	}})
	if r.Rev != 1 {
		t.Fatalf("a new run's rev = %d, want 1", r.Rev)
	}
	m.SetRefine("phone", r.ID, Refine{Status: "running"})
	m.SetRefine("other", r.ID, Refine{Status: "done"}) // not the owner's: nothing changes
	refined, _ := m.Get("phone", r.ID)
	if refined.Rev != 2 {
		t.Errorf("rev after refining moved on = %d, want 2", refined.Rev)
	}
	close(gate)
	if done := wait(t, m, "phone", r.ID); done.Rev != 3 {
		t.Errorf("rev after the part ended = %d, want 3", done.Rev)
	}
}

// R-186: a part that ran out of time keeps the conversation the adapter
// named (Claude carries on from it) and what the adapter saw of the time
// running out (agy: the server was busy).
func TestTimedOutPartKeepsSessionAndHint(t *testing.T) {
	m := NewManager()
	m.PartLimit = 50 * time.Millisecond
	r, _ := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tasks: []Task{
		{Agent: "claude", Ask: func(ctx context.Context) (core.AskResult, error) {
			<-ctx.Done()
			return core.AskResult{SessionID: "c1"}, core.Fail(core.ErrCodeAskTimeout, "")
		}},
		{Agent: "agy", Ask: func(ctx context.Context) (core.AskResult, error) {
			<-ctx.Done()
			return core.AskResult{}, &core.ActionError{Code: core.ErrCodeAskTimeout, Hint: core.HintServerBusy}
		}},
		{Agent: "codex", Ask: func(ctx context.Context) (core.AskResult, error) {
			<-ctx.Done()
			return core.AskResult{}, errors.New("killed")
		}},
	}})
	done := wait(t, m, "phone", r.ID)
	if p := done.Parts[0]; p.Error == nil || p.Error.Code != core.ErrCodeAskTimeout || p.Session != "claude:c1" {
		t.Errorf("claude part = %+v", p)
	}
	if p := done.Parts[1]; p.Error == nil || p.Error.Code != core.ErrCodeAskTimeout || p.Error.Hint != core.HintServerBusy {
		t.Errorf("agy part = %+v", p.Error)
	}
	if p := done.Parts[2]; p.Error == nil || p.Error.Code != core.ErrCodeAskTimeout || p.Error.Hint != "" || p.Session != "" {
		t.Errorf("codex part = %+v %+v", p, p.Error)
	}
}
