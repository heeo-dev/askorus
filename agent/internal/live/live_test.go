//go:build live

// Live scenario tests (LT-xx in docs/testing.md):
// the agent's HTTP API with the real Claude adapter, the real claude
// executable and the real ~/.claude, driven over HTTP the way the phone
// does. They only ever create sessions and servers in the test folder
// (.local/rc-test) and clean up after themselves.
//
//	go test -tags live -count=1 -v ./internal/live
//
// They are not part of `go test ./...`: they need this PC's Claude Code
// login and take a few minutes.

package live

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/adapter/claude"
	"github.com/asksemble/asksemble/agent/internal/api"
	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/devices"
	"github.com/asksemble/asksemble/agent/internal/hidden"
	"github.com/asksemble/asksemble/agent/internal/logx"
)

type liveEnv struct {
	t       *testing.T
	srv     *httptest.Server
	token   string
	adapter *claude.Adapter
	folder  string
}

// testFolder is the only folder these tests start anything in.
func testFolder(t *testing.T) string {
	t.Helper()
	if f := os.Getenv("ASKSEMBLE_LIVE_FOLDER"); f != "" {
		return f
	}
	// .local is not in git, so a worktree has none: look upward from
	// agent/internal/live for the checkout that has it.
	dir, _ := os.Getwd()
	for {
		f := filepath.Join(dir, ".local", "rc-test")
		if info, err := os.Stat(f); err == nil && info.IsDir() {
			return f
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip(".local/rc-test not found above the test (set ASKSEMBLE_LIVE_FOLDER)")
		}
		dir = parent
	}
}

func newLive(t *testing.T) *liveEnv {
	t.Helper()
	folder := testFolder(t)
	claudeDir, err := claude.DefaultClaudeDir()
	if err != nil {
		t.Fatal(err)
	}
	a := claude.New(claude.Options{ClaudeDir: claudeDir})
	dir := t.TempDir()
	store, _ := devices.Open(dir)
	token, _, err := store.Add("live test")
	if err != nil {
		t.Fatal(err)
	}
	hid, _ := hidden.Open(dir)
	allowed := []config.Folder{{ID: "rc-test", Path: folder, Server: config.FolderServer{Spawn: config.SpawnSameDir, Capacity: 2}}}
	h := api.New(api.Options{
		Version: "live", Hostname: "PC", Adapters: []core.Adapter{a}, Devices: store,
		Logger: logx.Discard(), Hidden: hid,
		Folders:        func() []config.Folder { return allowed },
		HistoryFolders: func() bool { return false },
	}).Handler()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &liveEnv{t: t, srv: srv, token: token, adapter: a, folder: folder}
}

func (e *liveEnv) call(method, path string, body any) (int, map[string]any) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+e.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

// job sends a slow action and asks for its result like the phone does.
func (e *liveEnv) job(method, path string, body any) map[string]any {
	e.t.Helper()
	st, r := e.call(method, path, body)
	if st != 202 {
		e.t.Fatalf("%s %s = %d %v", method, path, st, r)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		_, j := e.call("GET", "/v1/jobs/"+r["jobId"].(string), nil)
		if j["status"] != "running" {
			return j
		}
		time.Sleep(500 * time.Millisecond)
	}
	e.t.Fatal("job still running after 2 minutes")
	return nil
}

func (e *liveEnv) session(id string) map[string]any {
	e.t.Helper()
	_, r := e.call("GET", "/v1/sessions?folder="+urlQuery(e.folder), nil)
	for _, x := range r["sessions"].([]any) {
		if s := x.(map[string]any); s["id"] == id {
			return s
		}
	}
	return nil
}

func urlQuery(s string) string {
	return strings.NewReplacer("%", "%25", " ", "%20", "&", "%26", "#", "%23", "+", "%2B").Replace(s)
}

func doneResult(t *testing.T, what string, j map[string]any) map[string]any {
	t.Helper()
	if j["status"] != "done" {
		t.Fatalf("%s = %v", what, j)
	}
	return j["result"].(map[string]any)
}

// LT-01 Claude Code is there and usable, with nothing on the PC that
// would block remote control.
func TestLT01Info(t *testing.T) {
	e := newLive(t)
	st, info := e.call("GET", "/v1/info", nil)
	if st != 200 {
		t.Fatalf("info = %d %v", st, info)
	}
	ag := info["agents"].([]any)[0].(map[string]any)
	t.Logf("claude %v, notices %v", ag["version"], ag["notices"])
	if ag["available"] != true {
		t.Fatalf("claude not available: %v", ag)
	}
	if n, _ := ag["notices"].([]any); len(n) > 0 {
		t.Errorf("the PC reports problems: %v", n)
	}
}

// LT-02 The whole life of a session with the real claude: start, stop,
// wake (the link handed out must be the reconnected one, R-08), remove.
func TestLT02SessionLife(t *testing.T) {
	e := newLive(t)
	name := fmt.Sprintf("live-%d", time.Now().Unix())

	start := time.Now()
	r := doneResult(t, "start", e.job("POST", "/v1/sessions", map[string]string{"folder": "rc-test", "name": name}))
	id := r["session"].(map[string]any)["id"].(string)
	link := r["remoteUrl"].(string)
	t.Logf("started %s in %s, link …%s", id, time.Since(start).Round(time.Second), link[len(link)-8:])
	t.Cleanup(func() { e.call("DELETE", "/v1/sessions/"+id, nil) })
	if !strings.HasPrefix(link, "https://claude.ai/code/session_") {
		t.Fatalf("link = %q", link)
	}
	if s := e.session(id); s == nil || s["state"] != "running" {
		t.Fatalf("after start: %v", s)
	}

	if st, r := e.call("POST", "/v1/sessions/"+id+"/stop", nil); st != 200 {
		t.Fatalf("stop = %d %v", st, r)
	}
	if s := e.session(id); s == nil || s["state"] != "stopped" {
		t.Fatalf("after stop: %v", s)
	}

	wake := time.Now()
	r = doneResult(t, "wake", e.job("POST", "/v1/sessions/"+id+"/open", nil))
	t.Logf("woke in %s (action %v)", time.Since(wake).Round(time.Second), r["action"])
	if r["action"] != "resumed" || r["remoteUrl"] == "" {
		t.Fatalf("wake result = %v", r)
	}
	// The link must come from after the wake: the transcript's newest link
	// line is written once Claude has reconnected.
	if s := e.session(id); s == nil || s["state"] != "running" {
		t.Fatalf("after wake: %v", s)
	}

	// Opening a running session hands out the link without running anything.
	again := time.Now()
	r = doneResult(t, "open running", e.job("POST", "/v1/sessions/"+id+"/open", nil))
	if r["action"] != "none" || time.Since(again) > 10*time.Second {
		t.Errorf("open running = %v in %s", r, time.Since(again))
	}

	if st, _ := e.call("DELETE", "/v1/sessions/"+id, nil); st != 200 {
		t.Fatalf("remove = %d", st)
	}
	if s := e.session(id); s != nil {
		t.Errorf("still listed after remove: %v", s)
	}
}

// LT-03 Keeping the test folder open starts a real `claude remote-control`
// server, and closing it stops the server cleanly.
func TestLT03FolderServer(t *testing.T) {
	e := newLive(t)
	srv := core.Server(e.adapter)
	t.Cleanup(func() { srv.StopServer(e.folder) })
	if st := srv.ServerStatus(e.folder); st.State != core.ServeOff {
		t.Skipf("a server already runs in %s (%s); not touching it", e.folder, st.State)
	}
	if err := srv.StartServer(e.folder, core.ServeOptions{Name: "asksemble live", Spawn: config.SpawnSameDir, Capacity: 2}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(45 * time.Second)
	for srv.ServerStatus(e.folder).State != core.ServeRunning && time.Now().Before(deadline) {
		time.Sleep(time.Second)
	}
	if st := srv.ServerStatus(e.folder); st.State != core.ServeRunning {
		t.Fatalf("server state = %+v", st)
	}
	stop := time.Now()
	if err := srv.StopServer(e.folder); err != nil {
		t.Fatal(err)
	}
	t.Logf("server stopped in %s", time.Since(stop).Round(100*time.Millisecond))
	if st := srv.ServerStatus(e.folder); st.State != core.ServeOff {
		t.Errorf("after stop: %+v", st)
	}
}
