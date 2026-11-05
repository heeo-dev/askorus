package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/devices"
	"github.com/asksemble/asksemble/agent/internal/logx"
)

type fakeAdapter struct {
	sessions []core.Session
	err      error
}

func (f fakeAdapter) Agent() string { return "claude" }
func (f fakeAdapter) Info(context.Context) core.AdapterInfo {
	return core.AdapterInfo{Agent: "claude", Version: "2.1.310", Available: true, Capabilities: []core.Capability{core.CapList}}
}
func (f fakeAdapter) ListSessions(context.Context) (core.ListResult, error) {
	return core.ListResult{Sessions: f.sessions, Warnings: []string{"w1"}}, f.err
}

type env struct {
	h      http.Handler
	srv    *Server
	token  string
	device string // the device id of token, the owner of its runs and comparisons
	logs   *syncBuffer
	store  *devices.Store
}

// addDevice pairs another device and returns its token.
func (e env) addDevice(t *testing.T, name string) string {
	t.Helper()
	tok, _, err := e.store.Add(name)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func newEnv(t *testing.T, a core.Adapter, mutate func(*Options)) env {
	t.Helper()
	store, err := devices.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tok, dev, err := store.Add("test phone")
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	o := Options{
		Version: "0.1.0", Hostname: "pc", Adapters: []core.Adapter{a}, Devices: store,
		Logger: logx.New(logs, slog.LevelInfo), ServeHosts: []string{"pc.tail.ts.net"},
	}
	if mutate != nil {
		mutate(&o)
	}
	srv := New(o)
	// Asks and combined answers still running end with the test, before
	// its folders are removed (cleanups run last in, first out).
	t.Cleanup(srv.Close)
	return env{h: srv.Handler(), srv: srv, token: tok, device: dev.ID, logs: logs, store: store}
}

// syncBuffer is a log buffer that the request goroutine and background
// work (run parts, comparisons) may write at the same time.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func (e env) do(method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.Host = "127.0.0.1:8790"
	r.Header.Set("Authorization", "Bearer "+e.token)
	for k, v := range hdr {
		if v == "" {
			r.Header.Del(k)
		} else if k == "Host" {
			r.Host = v
		} else {
			r.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var b errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("error body %q: %v", w.Body.String(), err)
	}
	return b.Error.Code
}

func TestInfo(t *testing.T) {
	e := newEnv(t, fakeAdapter{}, nil)
	w := e.do("GET", "/v1/info", nil)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var resp InfoResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.APIVersion != 1 || resp.Version != "0.1.0" || len(resp.Agents) != 1 || resp.Agents[0].Version != "2.1.310" {
		t.Errorf("info = %+v", resp)
	}
	if len(resp.Capabilities) != 1 || resp.Capabilities[0] != core.CapList {
		t.Errorf("capabilities = %v", resp.Capabilities)
	}
	if len(resp.Reserved) != len(core.AllCapabilities)-1 {
		t.Errorf("reserved = %v", resp.Reserved)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("CORS header set: %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
}

func TestStreamReserved(t *testing.T) {
	e := newEnv(t, fakeAdapter{}, nil)
	for _, m := range []string{"GET", "POST"} {
		w := e.do(m, "/v1/stream", nil)
		if w.Code != 404 || errCode(t, w) != CodeNotImplemented {
			t.Errorf("%s /v1/stream = %d %s", m, w.Code, w.Body)
		}
	}
	if w := e.do("GET", "/v1/nope", nil); w.Code != 404 || errCode(t, w) != CodeNotFound {
		t.Errorf("unknown path = %d", w.Code)
	}
	if w := e.do("POST", "/v1/info", nil); w.Code != 405 || errCode(t, w) != CodeMethod {
		t.Errorf("POST /v1/info = %d", w.Code)
	}
}

func TestHostCheck(t *testing.T) {
	e := newEnv(t, fakeAdapter{}, nil)
	for _, host := range []string{"127.0.0.1", "localhost:8790", "[::1]:8790", "PC.tail.ts.net", "pc.tail.ts.net."} {
		if w := e.do("GET", "/v1/info", map[string]string{"Host": host}); w.Code != 200 {
			t.Errorf("host %q refused: %d", host, w.Code)
		}
	}
	for _, host := range []string{"evil.example", "evil.example:8790", "127.0.0.1.evil.example", "100.64.0.1"} {
		w := e.do("GET", "/v1/info", map[string]string{"Host": host})
		if w.Code != 403 || errCode(t, w) != CodeBadHost {
			t.Errorf("host %q allowed: %d", host, w.Code)
		}
	}
}

func TestAuth(t *testing.T) {
	e := newEnv(t, fakeAdapter{}, func(o *Options) { o.TailscaleOwner = "owner@example.com" })
	cases := []struct {
		name   string
		hdr    map[string]string
		status int
		code   string
	}{
		{"no token", map[string]string{"Authorization": ""}, 401, CodeUnauthorized},
		{"wrong token", map[string]string{"Authorization": "Bearer ask_wrong"}, 401, CodeUnauthorized},
		{"basic auth", map[string]string{"Authorization": "Basic Zm9vOmJhcg=="}, 401, CodeUnauthorized},
		{"other account", map[string]string{"Tailscale-User-Login": "kid@example.com"}, 403, CodeWrongAccount},
		{"browser origin", map[string]string{"Origin": "https://evil.example"}, 403, CodeCrossOrigin},
	}
	for _, c := range cases {
		w := e.do("GET", "/v1/info", c.hdr)
		if w.Code != c.status || errCode(t, w) != c.code {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body)
		}
	}
	if w := e.do("GET", "/v1/info", map[string]string{"Tailscale-User-Login": "Owner@Example.com"}); w.Code != 200 {
		t.Errorf("owner refused: %d", w.Code)
	}
	if w := e.do("OPTIONS", "/v1/info", nil); w.Code != 403 {
		t.Errorf("preflight = %d", w.Code)
	}
}

func TestSessions(t *testing.T) {
	now := time.Now().UTC()
	a := fakeAdapter{sessions: []core.Session{
		{ID: "claude:a", Agent: "claude", Title: "Old", Folder: `C:\w\a`, State: core.StateHistory, LastActive: now.Add(-time.Hour), SearchText: "secret words", OpenModes: []string{"official"}},
		{ID: "claude:b", Agent: "claude", Title: "New", Folder: `C:\w\b`, State: core.StateRunning, LastActive: now, OpenModes: []string{"official"}},
	}}
	e := newEnv(t, a, nil)

	w := e.do("GET", "/v1/sessions", nil)
	var resp SessionsResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != 200 || len(resp.Sessions) != 2 || resp.Sessions[0].ID != "claude:b" || len(resp.Warnings) != 1 {
		t.Fatalf("sessions = %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "secret words") {
		t.Error("search text sent to the app")
	}

	w = e.do("GET", "/v1/sessions?state=history&q=SECRET", nil)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Sessions) != 1 || resp.Sessions[0].ID != "claude:a" {
		t.Errorf("filtered = %s", w.Body)
	}
	if strings.Contains(e.logs.String(), "SECRET") {
		t.Error("search words written to the log")
	}
	if w := e.do("GET", "/v1/sessions?state=bogus", nil); w.Code != 400 {
		t.Errorf("bad state = %d", w.Code)
	}
	if w := e.do("GET", "/v1/sessions?q="+strings.Repeat("x", 201), nil); w.Code != 400 {
		t.Errorf("long query = %d", w.Code)
	}
}

func TestSessionsAdapterError(t *testing.T) {
	e := newEnv(t, fakeAdapter{err: errors.New("disk")}, nil)
	w := e.do("GET", "/v1/sessions", nil)
	var resp SessionsResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != 200 || resp.Sessions == nil || len(resp.Sessions) != 0 || len(resp.Warnings) != 1 || resp.Warnings[0] != "claude_list_failed" {
		t.Errorf("adapter error = %d %s", w.Code, w.Body)
	}
}

func TestLogHasNoToken(t *testing.T) {
	e := newEnv(t, fakeAdapter{}, nil)
	e.do("GET", "/v1/info?q=private", nil)
	e.do("GET", "/v1/info", map[string]string{"Authorization": "Bearer ask_guess"})
	logs := e.logs.String()
	for _, leak := range []string{e.token, "ask_guess", "private"} {
		if strings.Contains(logs, leak) {
			t.Errorf("log contains %q", leak)
		}
	}
	if !strings.Contains(logs, `"status":401`) || !strings.Contains(logs, `"path":"/v1/info"`) {
		t.Errorf("log missing request lines: %s", logs)
	}
}

func TestListenLoopbackOnly(t *testing.T) {
	ln, err := Listen(0)
	if err == nil {
		defer ln.Close()
	}
	// Port 0 is refused by config validation, but Listen itself must bind
	// to 127.0.0.1 whatever port it gets.
	if err == nil && !strings.HasPrefix(ln.Addr().String(), "127.0.0.1:") {
		t.Errorf("listening on %s", ln.Addr())
	}
}
