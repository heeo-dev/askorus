package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// gatedAsker answers only once its gate is opened.
type gatedAsker struct {
	*askAdapter
	gate chan struct{}
}

func (g gatedAsker) Ask(ctx context.Context, req core.AskRequest) (core.AskResult, error) {
	select {
	case <-g.gate:
	case <-ctx.Done():
		return core.AskResult{}, ctx.Err()
	}
	return g.askAdapter.Ask(ctx, req)
}

// #73 (2026-11-25): the phone asked for the whole run every second while
// the AIs answered, every answer again each time. A run has a rev that goes
// up when it changes; GET ?rev=N of a running run still at N is short.
func TestRunRevAnswersShortWhileNothingChanged(t *testing.T) {
	fast := &askAdapter{name: "claude", available: true}
	slow := gatedAsker{askAdapter: &askAdapter{name: "codex", available: true}, gate: make(chan struct{})}
	e, _ := multiEnv(t, fast, slow)
	w := e.post("/v1/runs", map[string]any{"mode": "ask", "prompt": "q", "agents": []map[string]string{{"agent": "claude"}, {"agent": "codex"}}})
	var started RunStatus
	json.Unmarshal(w.Body.Bytes(), &started)
	if w.Code != 202 || started.Rev < 1 {
		t.Fatalf("POST = %d, rev %d", w.Code, started.Rev)
	}
	// Once claude has answered, the rev is past the start and stays while codex works.
	var mid RunStatus
	deadline := time.Now().Add(5 * time.Second)
	for {
		json.Unmarshal(e.do("GET", "/v1/runs/"+started.ID, nil).Body.Bytes(), &mid)
		if mid.Parts[0].Status == "done" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("claude never answered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if mid.Rev <= started.Rev {
		t.Fatalf("rev %d after a part ended, started at %d", mid.Rev, started.Rev)
	}
	short := e.do("GET", "/v1/runs/"+started.ID+"?rev="+itoa(mid.Rev), nil)
	var u map[string]any
	json.Unmarshal(short.Body.Bytes(), &u)
	if short.Code != 200 || u["unchanged"] != true || u["status"] != "running" || u["pollAfterMs"] == nil || u["parts"] != nil ||
		strings.Contains(short.Body.String(), "SECRET-ANSWER") {
		t.Fatalf("unchanged answer = %d %s", short.Code, short.Body)
	}
	// An older rev, or none, gets the whole run.
	if full := e.do("GET", "/v1/runs/"+started.ID+"?rev="+itoa(started.Rev), nil); !strings.Contains(full.Body.String(), "SECRET-ANSWER") {
		t.Errorf("older rev = %s", full.Body)
	}
	close(slow.gate)
	done := e.waitRun(t, started.ID)
	if done.Rev <= mid.Rev {
		t.Errorf("rev %d after the last part, was %d", done.Rev, mid.Rev)
	}
	// A run that ended answers in full even at its own rev.
	var end RunStatus
	json.Unmarshal(e.do("GET", "/v1/runs/"+started.ID+"?rev="+itoa(done.Rev), nil).Body.Bytes(), &end)
	if end.Status != "done" || len(end.Parts) != 2 {
		t.Errorf("ended run at its rev = %+v", end)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
