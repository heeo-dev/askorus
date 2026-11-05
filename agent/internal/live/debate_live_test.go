//go:build live

package live

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/adapter/claude"
	"github.com/asksemble/asksemble/agent/internal/adapter/codex"
	"github.com/asksemble/asksemble/agent/internal/api"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/devices"
	"github.com/asksemble/asksemble/agent/internal/logx"
)

// LT-11 A debate (P2-5 토론, #26) with the real Claude Code and Codex on
// the quick tier: two answers that differ on a fact, Claude moderates.
// The debate must end done with a debated point, a turn that has a
// reply, and the answer after the debate. The turns run in the judge
// folder, so none of them shows in the session list. About 6 to 8 AI
// calls, 1 or 2 of them Codex.
func TestLT11Debate(t *testing.T) {
	claudeDir, err := claude.DefaultClaudeDir()
	if err != nil {
		t.Fatal(err)
	}
	folder := askFolder(t)
	dir := t.TempDir()
	store, _ := devices.Open(dir)
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

	_, info := e.call("GET", "/v1/info", nil)
	if info["debate"] == nil {
		t.Fatalf("info has no debate: %v", info)
	}
	start := time.Now()
	st, d := e.call("POST", "/v1/debates", map[string]any{
		"clientDebateId": "lt11", "tier": "quick", "moderator": "claude",
		"question":  "해수면(1기압)에서 순수한 물이 끓는 온도는 섭씨 몇 도인가요? 한 문장으로 답하세요.",
		"synthesis": "끓는점은 AI마다 다릅니다: {{Claude}}는 섭씨 100도, {{Codex}}는 섭씨 90도라고 했습니다.",
		"answers": []map[string]string{
			{"agent": "claude", "via": "pc", "text": "해수면에서 순수한 물은 섭씨 100도에서 끓습니다."},
			{"agent": "codex", "via": "pc", "text": "해수면에서 순수한 물은 섭씨 90도에서 끓습니다."},
		},
	})
	if st != 202 {
		t.Fatalf("POST /v1/debates = %d %v", st, d)
	}
	id := d["debateId"].(string)
	deadline := time.Now().Add(12 * time.Minute)
	for time.Now().Before(deadline) {
		_, d = e.call("GET", "/v1/debates/"+id, nil)
		if s := d["status"]; s == "done" || s == "stopped" || s == "failed" {
			break
		}
		time.Sleep(time.Second)
	}
	t.Logf("LT-11: %v in %s, %v calls, moderator %v %v", d["status"], time.Since(start).Round(100*time.Millisecond), d["calls"], d["moderator"], d["model"])
	if d["status"] != "done" {
		t.Fatalf("debate: %v", d)
	}
	points, _ := d["points"].([]any)
	debated, replies := 0, 0
	for _, x := range points {
		p := x.(map[string]any)
		t.Logf("  point %v (%v) debated=%v result=%v", p["id"], p["nature"], p["debate"], p["result"])
		if p["debate"] == true {
			debated++
		}
		for _, y := range p["turns"].([]any) {
			tn := y.(map[string]any)
			t.Logf("    %v: %v %v stance=%v %v ms", tn["ai"], tn["status"], tn["model"], tn["stance"], tn["ms"])
			if tn["status"] == "done" && tn["text"] != "" {
				replies++
			}
		}
	}
	after, _ := d["after"].(map[string]any)
	if debated == 0 || replies == 0 || after == nil || strings.TrimSpace(after["text"].(string)) == "" {
		t.Fatalf("debated %d, replies %d, after %v", debated, replies, after)
	}
	if !strings.Contains(after["text"].(string), "100") {
		t.Errorf("the answer after the debate lost the boiling point: %v", after["text"])
	}
	// The turns ran in the judge folder, which the list leaves out.
	_, list := e.call("GET", "/v1/sessions?folder="+urlQuery(core.JudgeFolder(folder)), nil)
	if ss, _ := list["sessions"].([]any); len(ss) != 0 {
		t.Errorf("the judge folder's sessions are listed: %d", len(ss))
	}
}
