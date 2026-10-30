package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// askAdapter is an AI that can answer questions, with sessions and a
// folder history for the ask badge and folder checks.
type askAdapter struct {
	name      string
	available bool
	sessions  []core.Session
	recent    []core.RecentFolder

	mu   sync.Mutex
	reqs []core.AskRequest
}

func (a *askAdapter) Agent() string { return a.name }
func (a *askAdapter) Info(context.Context) core.AdapterInfo {
	caps := []core.Capability{core.CapAsk}
	if !a.available {
		caps = []core.Capability{}
	}
	return core.AdapterInfo{Agent: a.name, Version: "1.0.0", Available: a.available, Capabilities: caps}
}
func (a *askAdapter) ListSessions(context.Context) (core.ListResult, error) {
	return core.ListResult{Sessions: append([]core.Session(nil), a.sessions...)}, nil
}
func (a *askAdapter) RecentFolders(context.Context) ([]core.RecentFolder, error) {
	return a.recent, nil
}
func (a *askAdapter) AskTiers() map[string]core.AskModel {
	return map[string]core.AskModel{core.TierStandard: {Model: a.name + "-std", Effort: "medium"}}
}
func (a *askAdapter) Ask(_ context.Context, req core.AskRequest) (core.AskResult, error) {
	a.mu.Lock()
	a.reqs = append(a.reqs, req)
	a.mu.Unlock()
	if strings.Contains(req.Prompt, "FAIL") {
		return core.AskResult{}, core.Fail(core.ErrCodeAskFailed, "usage limit reached")
	}
	return core.AskResult{SessionID: "s-" + a.name, Answer: a.name + " says SECRET-ANSWER", Model: a.name + "-std"}, nil
}
func (a *askAdapter) requests() []core.AskRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]core.AskRequest(nil), a.reqs...)
}

// multiEnv serves several adapters with an ask folder.
func multiEnv(t *testing.T, adapters ...core.Adapter) (env, string) {
	t.Helper()
	ask := filepath.Join(t.TempDir(), "ask")
	e := newEnv(t, adapters[0], func(o *Options) {
		o.Adapters = adapters
		o.AskFolder = ask
		o.HistoryFolders = func() bool { return true }
	})
	return e, ask
}

func (e env) post(target string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", target, bytes.NewReader(b))
	r.Host = "127.0.0.1:8790"
	r.Header.Set("Authorization", "Bearer "+e.token)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e env) waitRun(t *testing.T, id string) RunStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		w := e.do("GET", "/v1/runs/"+id, nil)
		if w.Code != 200 {
			t.Fatalf("GET run = %d %s", w.Code, w.Body)
		}
		var st RunStatus
		json.Unmarshal(w.Body.Bytes(), &st)
		if st.Status != "running" {
			return st
		}
		if st.PollAfterMs == 0 {
			t.Fatal("running run without pollAfterMs")
		}
		if time.Now().After(deadline) {
			t.Fatal("run still running")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunAsksEveryNamedAI(t *testing.T) {
	cl := &askAdapter{name: "claude", available: true}
	cx := &askAdapter{name: "codex", available: true}
	e, ask := multiEnv(t, cl, cx)
	w := e.post("/v1/runs", map[string]any{
		"mode": "ask", "prompt": "  대한민국의 수도는?  ", "tier": "deep",
		"agents": []map[string]string{{"agent": "claude", "model": "opus"}, {"agent": "codex", "session": "codex:t-1", "effort": "high"}},
	})
	if w.Code != 202 {
		t.Fatalf("POST = %d %s", w.Code, w.Body)
	}
	var started RunStatus
	json.Unmarshal(w.Body.Bytes(), &started)
	if started.ID == "" || started.Mode != "ask" || started.Tier != "deep" || len(started.Parts) != 2 || started.PollAfterMs == 0 {
		t.Fatalf("started = %+v", started)
	}
	st := e.waitRun(t, started.ID)
	if st.Status != "done" || st.Parts[0].Answer != "claude says SECRET-ANSWER" || st.Parts[1].Session != "codex:s-codex" {
		t.Fatalf("run = %+v", st)
	}
	if info, err := os.Stat(ask); err != nil || !info.IsDir() {
		t.Fatalf("ask folder not made: %v", err)
	}
	c, x := cl.requests()[0], cx.requests()[0]
	if c.Folder != ask || c.Tier != "deep" || c.Model != "opus" || c.Resume != "" {
		t.Errorf("claude request = %+v", c)
	}
	if x.Resume != "t-1" || x.Effort != "high" {
		t.Errorf("codex request = %+v", x)
	}
	if c.Prompt != core.AskPrompt("대한민국의 수도는?") {
		t.Errorf("prompt = %q", c.Prompt)
	}
	// Neither the question nor the answers reach the log.
	if logs := e.logs.String(); strings.Contains(logs, "수도") || strings.Contains(logs, "SECRET-ANSWER") {
		t.Errorf("conversation text in the log: %s", logs)
	}
	if !strings.Contains(e.logs.String(), `"run part"`) {
		t.Errorf("parts not logged: %s", e.logs.String())
	}
}

func TestRunDefaultsToEveryAvailableAsker(t *testing.T) {
	cl := &askAdapter{name: "claude", available: true}
	gone := &askAdapter{name: "opencode", available: false}
	e, _ := multiEnv(t, cl, fakeAdapter{}, gone)
	w := e.post("/v1/runs", map[string]any{"mode": "ask", "prompt": "q"})
	if w.Code != 202 {
		t.Fatalf("POST = %d %s", w.Code, w.Body)
	}
	var st RunStatus
	json.Unmarshal(w.Body.Bytes(), &st)
	if len(st.Parts) != 1 || st.Parts[0].Agent != "claude" || st.Tier != "standard" {
		t.Fatalf("parts = %+v", st)
	}
	e.waitRun(t, st.ID)
}

func TestRunFailurePerAI(t *testing.T) {
	cl := &askAdapter{name: "claude", available: true}
	e, _ := multiEnv(t, cl)
	w := e.post("/v1/runs", map[string]any{"mode": "ask", "prompt": "FAIL please"})
	var st RunStatus
	json.Unmarshal(w.Body.Bytes(), &st)
	st = e.waitRun(t, st.ID)
	if st.Status != "failed" || st.Parts[0].Error == nil || st.Parts[0].Error.Code != core.ErrCodeAskFailed || st.Parts[0].Error.Message != "usage limit reached" {
		t.Fatalf("run = %+v", st)
	}
	if strings.Contains(e.logs.String(), "usage limit") {
		t.Errorf("AI message logged: %s", e.logs.String())
	}
}

func TestRunRequestChecks(t *testing.T) {
	cl := &askAdapter{name: "claude", available: true}
	gone := &askAdapter{name: "opencode", available: false}
	e, _ := multiEnv(t, cl, gone)
	cases := []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"work is later", map[string]any{"mode": "work", "prompt": "q"}, 501, CodeModeReserved},
		{"code is later", map[string]any{"mode": "code", "prompt": "q"}, 501, CodeModeReserved},
		{"unknown mode", map[string]any{"mode": "chat", "prompt": "q"}, 400, CodeBadRequest},
		{"empty prompt", map[string]any{"mode": "ask", "prompt": "  \n "}, 400, CodeBadRequest},
		{"long prompt", map[string]any{"mode": "ask", "prompt": strings.Repeat("가", maxPrompt/3+1)}, 413, CodeBadRequest},
		{"bad tier", map[string]any{"mode": "ask", "prompt": "q", "tier": "max"}, 400, CodeBadRequest},
		{"unknown AI", map[string]any{"mode": "ask", "prompt": "q", "agents": []map[string]string{{"agent": "gemini"}}}, 400, CodeAgentNoAsk},
		{"AI not here", map[string]any{"mode": "ask", "prompt": "q", "agents": []map[string]string{{"agent": "opencode"}}}, 503, CodeAgentNoAsk},
		{"twice", map[string]any{"mode": "ask", "prompt": "q", "agents": []map[string]string{{"agent": "claude"}, {"agent": "claude"}}}, 400, CodeBadRequest},
		{"flag as model", map[string]any{"mode": "ask", "prompt": "q", "agents": []map[string]string{{"agent": "claude", "model": "--dangerously-skip-permissions"}}}, 400, CodeBadRequest},
		{"bad effort", map[string]any{"mode": "ask", "prompt": "q", "agents": []map[string]string{{"agent": "claude", "effort": "-x"}}}, 400, CodeBadRequest},
		{"other AI's session", map[string]any{"mode": "ask", "prompt": "q", "agents": []map[string]string{{"agent": "claude", "session": "codex:abc"}}}, 400, CodeBadSessionRef},
		{"session without agent", map[string]any{"mode": "ask", "prompt": "q", "agents": []map[string]string{{"agent": "claude", "session": "abc"}}}, 400, CodeBadSessionRef},
	}
	for _, c := range cases {
		w := e.post("/v1/runs", c.body)
		if w.Code != c.status || errCode(t, w) != c.code {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body)
		}
	}
	if n := len(cl.requests()); n != 0 {
		t.Errorf("a refused request asked the AI %d times", n)
	}
	if w := e.do("GET", "/v1/runs", nil); w.Code != 405 {
		t.Errorf("GET /v1/runs = %d", w.Code)
	}
}

// slowAsker answers only when released, for follow-ups still running.
type slowAsker struct {
	askAdapter
	release chan struct{}
}

func (a *slowAsker) Ask(ctx context.Context, req core.AskRequest) (core.AskResult, error) {
	<-a.release
	return a.askAdapter.Ask(ctx, req)
}

func TestRunSameSessionTwiceIsBusy(t *testing.T) {
	cl := &slowAsker{askAdapter: askAdapter{name: "claude", available: true}, release: make(chan struct{})}
	e, _ := multiEnv(t, cl)
	body := map[string]any{"mode": "ask", "prompt": "q", "agents": []map[string]string{{"agent": "claude", "session": "claude:s-1"}}}
	w := e.post("/v1/runs", body)
	if w.Code != 202 {
		t.Fatalf("first = %d %s", w.Code, w.Body)
	}
	if w2 := e.post("/v1/runs", body); w2.Code != 409 || errCode(t, w2) != core.ErrCodeBusy {
		t.Errorf("second = %d %s", w2.Code, w2.Body)
	}
	close(cl.release)
	var st RunStatus
	json.Unmarshal(w.Body.Bytes(), &st)
	e.waitRun(t, st.ID)
}

func TestRunNoAskFolderNoAsk(t *testing.T) {
	cl := &askAdapter{name: "claude", available: true}
	e := newEnv(t, cl, nil) // no AskFolder
	if w := e.post("/v1/runs", map[string]any{"mode": "ask", "prompt": "q"}); w.Code != 503 || errCode(t, w) != CodeAskFolder {
		t.Fatalf("POST = %d %s", w.Code, w.Body)
	}
}

func TestRunBelongsToItsDevice(t *testing.T) {
	cl := &askAdapter{name: "claude", available: true}
	e, _ := multiEnv(t, cl)
	w := e.post("/v1/runs", map[string]any{"mode": "ask", "prompt": "q", "clientRunId": "phone-1"})
	var st RunStatus
	json.Unmarshal(w.Body.Bytes(), &st)
	e.waitRun(t, st.ID)
	other := e.addDevice(t, "tablet")
	if w := e.do("GET", "/v1/runs/"+st.ID, map[string]string{"Authorization": "Bearer " + other}); w.Code != 404 || errCode(t, w) != CodeRunNotFound {
		t.Fatalf("other device = %d %s", w.Code, w.Body)
	}
	// A retried request (same clientRunId) does not ask again.
	w = e.post("/v1/runs", map[string]any{"mode": "ask", "prompt": "q", "clientRunId": "phone-1"})
	var again RunStatus
	json.Unmarshal(w.Body.Bytes(), &again)
	if again.ID != st.ID || len(cl.requests()) != 1 {
		t.Fatalf("retry asked again: %s vs %s, %d requests", again.ID, st.ID, len(cl.requests()))
	}
}

func TestInfoShowsAskTiers(t *testing.T) {
	e, _ := multiEnv(t, &askAdapter{name: "claude", available: true}, &askAdapter{name: "opencode", available: false})
	var resp InfoResponse
	json.Unmarshal(e.do("GET", "/v1/info", nil).Body.Bytes(), &resp)
	if len(resp.Agents) != 2 || resp.Agents[0].Ask == nil || resp.Agents[0].Ask.Tiers["standard"].Model != "claude-std" {
		t.Fatalf("agents = %+v", resp.Agents)
	}
	if resp.Agents[1].Ask != nil {
		t.Errorf("an AI that cannot ask shows tiers: %+v", resp.Agents[1])
	}
	if !contains(resp.Capabilities, core.CapAsk) {
		t.Errorf("capabilities = %v", resp.Capabilities)
	}
}

func contains(list []core.Capability, c core.Capability) bool {
	for _, x := range list {
		if x == c {
			return true
		}
	}
	return false
}

func TestAskFolderSessionsAndFolders(t *testing.T) {
	cl := &askAdapter{name: "claude", available: true}
	e, ask := multiEnv(t, cl)
	work := filepath.Join(filepath.Dir(ask), "work")
	cl.sessions = []core.Session{
		{ID: "claude:1", Agent: "claude", Folder: ask, State: core.StateHistory, LastActive: time.Now()},
		{ID: "claude:2", Agent: "claude", Folder: work, State: core.StateHistory, LastActive: time.Now().Add(-time.Hour)},
		{ID: "claude:3", Agent: "claude", Folder: filepath.Join(ask, "sub"), State: core.StateHistory, Badges: []string{core.BadgeAsk}},
	}
	cl.recent = []core.RecentFolder{{Path: strings.ToUpper(ask)}, {Path: work}}
	var list SessionsResponse
	json.Unmarshal(e.do("GET", "/v1/sessions", nil).Body.Bytes(), &list)
	badges := map[string][]string{}
	for _, s := range list.Sessions {
		badges[s.ID] = s.Badges
	}
	if got := badges["claude:1"]; len(got) != 1 || got[0] != core.BadgeAsk {
		t.Errorf("ask session badges = %v", got)
	}
	if got := badges["claude:2"]; len(got) != 0 {
		t.Errorf("coding session badges = %v", got)
	}
	if got := badges["claude:3"]; len(got) != 1 {
		t.Errorf("badge added twice: %v", got)
	}
	var folders struct {
		Folders []FolderInfo `json:"folders"`
	}
	json.Unmarshal(e.do("GET", "/v1/folders", nil).Body.Bytes(), &folders)
	if len(folders.Folders) != 1 || folders.Folders[0].Path != work {
		t.Errorf("history folders = %+v (the ask folder must not be offered)", folders.Folders)
	}
}
