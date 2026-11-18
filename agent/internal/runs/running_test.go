package runs

import (
	"context"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// Running counts the runs still asking an AI, which an update waits for
// (#35); a run counts until its last part has answered.
func TestRunningCountsRunsStillAsking(t *testing.T) {
	m := NewManager()
	if m.Running() != 0 {
		t.Fatal("a new manager runs something")
	}
	slow := make(chan struct{})
	r, err := m.Start(Spec{Owner: "phone", Mode: core.ModeAsk, Tier: core.TierStandard, Tasks: []Task{
		{Agent: "claude", Ask: answer("c1", "done")},
		{Agent: "codex", Ask: func(context.Context) (core.AskResult, error) {
			<-slow
			return core.AskResult{SessionID: "x1", Answer: "late"}, nil
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if n := m.Running(); n != 1 {
		t.Fatalf("Running = %d while a part asks", n)
	}
	close(slow)
	wait(t, m, "phone", r.ID)
	if n := m.Running(); n != 0 {
		t.Fatalf("Running = %d after the run ended", n)
	}
}
