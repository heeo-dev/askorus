//go:build live

package live

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/adapter/claude"
	"github.com/heeo-dev/askorus/agent/internal/adapter/codex"
	"github.com/heeo-dev/askorus/agent/internal/api"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/devices"
	"github.com/heeo-dev/askorus/agent/internal/logx"
)

// LT-12 A follow-up with the conversation record (#29) with the real
// Claude Code and Codex on the quick tier: Claude answers a first
// question, then the follow-up goes to Claude in its session and to Codex
// new to the conversation, with a record of a settled point and the last
// answer, and Claude writes the combined answer (synth-v6). Each answer
// and the combined answer must take the follow-up's new condition (3,000
// m, so below 100 °C). 4 AI calls, 1 of them Codex.
func TestLT12FollowUpWithRecord(t *testing.T) {
	claudeDir, err := claude.DefaultClaudeDir()
	if err != nil {
		t.Fatal(err)
	}
	folder := askFolder(t)
	store, _ := devices.Open(t.TempDir())
	token, _, err := store.Add("live test")
	if err != nil {
		t.Fatal(err)
	}
	adapters := []core.Adapter{claude.New(claude.Options{ClaudeDir: claudeDir}), codex.New(codex.Options{SchemaDir: t.TempDir()})}
	srv := httptest.NewServer(api.New(api.Options{
		Version: "live", Hostname: "PC", Adapters: adapters, Devices: store, Logger: logx.Discard(), AskFolder: folder,
		JudgeMinVersions: map[string]string{claude.Name: claude.JudgeMinVersion, codex.Name: codex.JudgeMinVersion},
	}).Handler())
	t.Cleanup(srv.Close)
	e := &liveEnv{t: t, srv: srv, token: token, folder: folder}

	if _, info := e.call("GET", "/v1/info", nil); info["context"] == nil {
		t.Fatalf("info has no context: %v", info)
	}
	q1 := "해수면(1기압)에서 순수한 물이 끓는 온도는 섭씨 몇 도인가요? 한 문장으로 답하세요."
	parts, took := e.run(map[string]any{"mode": "ask", "tier": "quick", "prompt": q1, "agents": []map[string]string{{"agent": "claude"}}})
	first := parts["claude"]
	if first["status"] != "done" || first["session"] == nil {
		t.Fatalf("turn 1: %v", first)
	}
	t.Logf("LT-12 turn 1: claude %s, %d bytes", took.Round(100*time.Millisecond), len(first["answer"].(string)))

	q2 := "그럼 해발 3000 m 산 위에서는 몇 도쯤에서 끓나요? 한 문장으로 답하세요."
	start := time.Now()
	st, r := e.call("POST", "/v1/runs", map[string]any{
		"mode": "ask", "tier": "quick", "prompt": q2,
		"agents":  []map[string]string{{"agent": "claude", "session": first["session"].(string)}, {"agent": "codex"}},
		"compare": map[string]any{"earlier": []string{q1}, "judges": []string{"claude"}, "synthesis": map[string]string{"agent": "claude"}},
		"context": map[string]any{
			"earlier": []string{q1},
			"settled": []map[string]string{{"topic": "해수면에서 물의 끓는점", "result": "agreed", "line": "해수면(1기압)에서 순수한 물은 섭씨 100도에서 끓습니다."}},
			"last":    "해수면(1기압)에서 순수한 물은 섭씨 100도에서 끓습니다.",
		},
	})
	if st != 202 || r["comparisonId"] == nil {
		t.Fatalf("POST /v1/runs = %d %v", st, r)
	}
	var run, cmp map[string]any
	deadline := time.Now().Add(6 * time.Minute)
	for time.Now().Before(deadline) {
		_, run = e.call("GET", "/v1/runs/"+r["runId"].(string), nil)
		_, cmp = e.call("GET", "/v1/comparisons/"+r["comparisonId"].(string), nil)
		sy, _ := cmp["synthesis"].(map[string]any)
		if run["status"] != "running" && sy != nil && sy["status"] != "waiting" && sy["status"] != "running" {
			break
		}
		time.Sleep(time.Second)
	}
	// Each answer and the combined answer give a temperature below 100 °C
	// (about 90): the new condition wins over the settled sea-level point.
	below := func(s string) bool {
		for _, n := range []string{"89", "90", "91", "88", "87", "92"} {
			if strings.Contains(s, n) {
				return true
			}
		}
		return false
	}
	for _, x := range run["parts"].([]any) {
		p := x.(map[string]any)
		a, _ := p["answer"].(string)
		t.Logf("  %v: %v %v %v ms, %d bytes", p["agent"], p["status"], p["model"], p["ms"], len(a))
		if p["status"] != "done" || !below(a) {
			t.Errorf("%v's answer: %v %q", p["agent"], p["status"], a)
		}
	}
	sy, _ := cmp["synthesis"].(map[string]any)
	text, _ := sy["text"].(string)
	t.Logf("LT-12 turn 2: %s, synthesis %v by %v %v, %d bytes", time.Since(start).Round(100*time.Millisecond), sy["status"], sy["agent"], sy["model"], len(text))
	if sy["status"] != "done" || !below(text) {
		t.Errorf("combined answer: %v %q", sy["status"], text)
	}
	if strings.Contains(text, "토론") {
		t.Errorf("the combined answer writes about an earlier debate: %q", text)
	}
}
