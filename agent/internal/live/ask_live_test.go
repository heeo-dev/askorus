//go:build live

package live

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/adapter/claude"
	"github.com/asksemble/asksemble/agent/internal/adapter/codex"
	"github.com/asksemble/asksemble/agent/internal/adapter/opencode"
	"github.com/asksemble/asksemble/agent/internal/api"
	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/devices"
	"github.com/asksemble/asksemble/agent/internal/logx"
)

// askFolder is where the live asks run: a folder inside the real ask
// folder, so test asks stay apart but the real agent still badges them and
// never offers the folder for new coding sessions. The asks stay there as
// history, like any ask.
func askFolder(t *testing.T) string {
	t.Helper()
	cfg := config.Default()
	dir, err := cfg.AskFolder()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "live")
}

func newLiveAsk(t *testing.T) (*liveEnv, string) {
	t.Helper()
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
	adapters := []core.Adapter{claude.New(claude.Options{ClaudeDir: claudeDir}), codex.New(codex.Options{SchemaDir: t.TempDir()}), opencode.New(opencode.Options{})}
	srv := httptest.NewServer(api.New(api.Options{
		Version: "live", Hostname: "PC", Adapters: adapters, Devices: store, Logger: logx.Discard(), AskFolder: folder,
	}).Handler())
	t.Cleanup(srv.Close)
	return &liveEnv{t: t, srv: srv, token: token, folder: folder}, folder
}

// run asks and waits for every answer like the phone does.
func (e *liveEnv) run(body map[string]any) (map[string]map[string]any, time.Duration) {
	e.t.Helper()
	start := time.Now()
	st, r := e.call("POST", "/v1/runs", body)
	if st != 202 {
		e.t.Fatalf("POST /v1/runs = %d %v", st, r)
	}
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		_, run := e.call("GET", "/v1/runs/"+r["runId"].(string), nil)
		if run["status"] != "running" {
			parts := map[string]map[string]any{}
			for _, x := range run["parts"].([]any) {
				p := x.(map[string]any)
				parts[p["agent"].(string)] = p
			}
			return parts, time.Since(start)
		}
		time.Sleep(500 * time.Millisecond)
	}
	e.t.Fatal("run still running after 4 minutes")
	return nil, 0
}

// LT-05 Ask (P2-1) with the real Claude Code, Codex and OpenCode: one
// question to all three at once, a follow-up on each AI's own session, no
// file written by any of them, and the ask badge on the Claude session.
// Only short answers are checked; their text is not kept.
func TestLT05Ask(t *testing.T) {
	e, folder := newLiveAsk(t)
	_, info := e.call("GET", "/v1/info", nil)
	var agents []map[string]string
	for _, x := range info["agents"].([]any) {
		a := x.(map[string]any)
		t.Logf("%s %v available=%v notices=%v", a["agent"], a["version"], a["available"], a["notices"])
		if a["available"] != true || a["ask"] == nil {
			t.Errorf("%s cannot ask on this PC", a["agent"])
			continue
		}
		agents = append(agents, map[string]string{"agent": a["agent"].(string)})
	}
	if len(agents) == 0 {
		t.Fatal("no AI can ask")
	}

	parts, took := e.run(map[string]any{"mode": "ask", "tier": "quick", "agents": agents,
		"prompt": "대한민국의 수도는 어디인가요? 한 문장으로 답하세요."})
	t.Logf("first ask: %s in all", took.Round(100*time.Millisecond))
	sessions := map[string]string{}
	for name, p := range parts {
		t.Logf("  %s %v: %v ms, %v in / %v out tokens", name, p["model"], p["ms"], p["inputTokens"], p["outputTokens"])
		if p["status"] != "done" || !strings.Contains(p["answer"].(string), "서울") || p["session"] == nil {
			t.Fatalf("%s: %v", name, p)
		}
		sessions[name] = p["session"].(string)
	}

	var follow []map[string]string
	for name, s := range sessions {
		follow = append(follow, map[string]string{"agent": name, "session": s})
	}
	parts, took = e.run(map[string]any{"mode": "ask", "tier": "quick", "agents": follow,
		"prompt": "방금 답한 도시의 인구를 대략 한 문장으로 알려 주세요."})
	t.Logf("follow-up: %s in all", took.Round(100*time.Millisecond))
	for name, p := range parts {
		t.Logf("  %s: %v ms", name, p["ms"])
		if p["status"] != "done" || p["answer"] == "" || p["session"] != sessions[name] {
			t.Errorf("%s follow-up: %v (session was %s)", name, p, sessions[name])
		}
	}

	file := filepath.Join(folder, "ask-write-test.txt")
	parts, _ = e.run(map[string]any{"mode": "ask", "tier": "quick", "agents": agents,
		"prompt": "도구를 써서 지금 폴더에 ask-write-test.txt 파일을 만들고 hi라고 적어 주세요."})
	if _, err := os.Stat(file); err == nil {
		os.Remove(file)
		t.Errorf("an ask wrote a file (removed): %v", parts)
	}
	for name, p := range parts {
		t.Logf("  write test %s: %v", name, p["status"])
	}

	if s := e.sessionIn(folder, sessions["claude"]); s == nil || !strings.Contains(strings.Join(toStrings(s["badges"]), ","), core.BadgeAsk) {
		t.Errorf("claude ask session in the list: %v", s)
	}
}

func (e *liveEnv) sessionIn(folder, id string) map[string]any {
	e.t.Helper()
	_, r := e.call("GET", "/v1/sessions?folder="+urlQuery(folder), nil)
	for _, x := range r["sessions"].([]any) {
		if s := x.(map[string]any); s["id"] == id {
			return s
		}
	}
	return nil
}

func toStrings(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, x := range list {
		out = append(out, x.(string))
	}
	return out
}
