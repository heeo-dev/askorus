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

// LT-13 Asking the moderator (#25) with the real Claude Code on the quick
// tier: the previous turn's two answers differ on a fact (100 °C vs 90 °C)
// and the person asks the moderator what the AIs differed on. Claude
// answers alone, as a one-off in the judge folder (no session in the run,
// none in the session list), and its answer names the difference. 1 AI
// call.
func TestLT13ModeratorAsk(t *testing.T) {
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

	if _, info := e.call("GET", "/v1/info", nil); info["moderator"] == nil {
		t.Fatalf("info has no moderator: %v", info)
	}
	q1 := "해수면(1기압)에서 순수한 물이 끓는 온도는 섭씨 몇 도인가요? 한 문장으로 답하세요."
	parts, took := e.run(map[string]any{
		"mode": "ask", "tier": "quick", "prompt": "AI들이 뭐가 달랐는지 한 문장으로 말해 줘.",
		"moderator": map[string]any{
			"agent": "claude",
			"answers": []map[string]string{
				{"agent": "claude", "text": "해수면에서 순수한 물은 섭씨 100도에서 끓습니다."},
				{"agent": "codex", "text": "해수면에서 순수한 물은 섭씨 90도에서 끓습니다."},
			},
		},
		"context": map[string]any{"earlier": []string{q1}, "last": "끓는점은 AI마다 다릅니다: Claude는 100도, Codex는 90도라고 했습니다."},
	})
	p := parts["claude"]
	answer, _ := p["answer"].(string)
	t.Logf("LT-13: %v %v in %s, %d bytes", p["status"], p["model"], took.Round(100*time.Millisecond), len(answer))
	if len(parts) != 1 || p["status"] != "done" || p["session"] != nil {
		t.Fatalf("run: %v", parts)
	}
	if !strings.Contains(answer, "100") || !strings.Contains(answer, "90") {
		t.Errorf("the moderator did not name the difference: %q", answer)
	}
	// One-off in the judge folder: nothing new in the session list of that folder.
	_, list := e.call("GET", "/v1/sessions?folder="+urlQuery(core.JudgeFolder(folder)), nil)
	if ss, _ := list["sessions"].([]any); len(ss) != 0 {
		t.Errorf("the judge folder's sessions are listed: %d", len(ss))
	}
}
