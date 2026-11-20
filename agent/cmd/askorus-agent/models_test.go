package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// followingAI is an ask-only AI whose tiers follow newer models.
type followingAI struct {
	name   string
	newer  map[string]string
	waited *int
}

func (f followingAI) Agent() string { return f.name }
func (f followingAI) Info(context.Context) core.AdapterInfo {
	return core.AdapterInfo{Agent: f.name, Available: true}
}
func (f followingAI) ListSessions(context.Context) (core.ListResult, error) {
	return core.ListResult{}, nil
}
func (f followingAI) Follow(m string) string     { return m }
func (f followingAI) Newer() map[string]string   { return f.newer }
func (f followingAI) WaitModels(context.Context) { *f.waited++ }

// slowAI is an AI whose Info takes long, as a CLI that hangs.
type slowAI struct{ followingAI }

func (s slowAI) Info(context.Context) core.AdapterInfo {
	time.Sleep(2 * time.Second)
	return core.AdapterInfo{Agent: s.name, Available: true}
}

// R-152 (2ho's 0.5.1 review): waitModels returns at its deadline even when
// an AI's Info outlasts it, and asks the AIs side by side.
func TestWaitModelsDeadline(t *testing.T) {
	waited := 0
	ais := []core.Adapter{
		slowAI{followingAI{name: "codex", waited: new(int)}},
		followingAI{name: "opencode", waited: &waited},
	}
	start := time.Now()
	waitModels(context.Background(), ais, 300*time.Millisecond)
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Errorf("waitModels took %v with a 300 ms deadline", d)
	}
	if waited != 1 {
		t.Errorf("the quick AI was not waited for beside the slow one: %d", waited)
	}
}

// TestModelLines (#64): check waits for the lists and names each AI that
// uses newer models; agent.json ask.noFollow says the models stay.
func TestModelLines(t *testing.T) {
	waited := 0
	ais := []core.Adapter{
		followingAI{name: "codex", newer: map[string]string{"gpt-6.1-sol": "gpt-6.2-sol", "gpt-6-luna": "gpt-6.1-luna"}, waited: &waited},
		followingAI{name: "opencode", newer: map[string]string{}, waited: &waited},
	}
	lines := modelLines(context.Background(), ais, false)
	if waited != 2 || len(lines) != 1 || lines[0].level != "info" {
		t.Fatalf("waited %d, lines %+v", waited, lines)
	}
	if want := "Codex asks use newer models: gpt-6-luna → gpt-6.1-luna, gpt-6.1-sol → gpt-6.2-sol"; lines[0].text.en != want {
		t.Errorf("en = %q", lines[0].text.en)
	}
	if !strings.Contains(lines[0].text.ko, "Codex 묻기가 새 모델을 씁니다") {
		t.Errorf("ko = %q", lines[0].text.ko)
	}

	waited = 0
	off := modelLines(context.Background(), ais, true)
	if waited != 0 || len(off) != 1 || !strings.Contains(off[0].text.en, "ask.noFollow") {
		t.Errorf("noFollow: waited %d, %+v", waited, off)
	}
}
