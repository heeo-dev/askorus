package claude

// Scenario integration tests (IT-xx in docs/testing.md): the real HTTP
// server, API, pairing, device store and Claude adapter, with only the
// claude executable faked. Each test walks a flow the phone app does, in
// order, over HTTP.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/api"
	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/devices"
	"github.com/asksemble/asksemble/agent/internal/hidden"
	"github.com/asksemble/asksemble/agent/internal/logx"
	"github.com/asksemble/asksemble/agent/internal/pairing"
)

const (
	itOwner = "owner@example.com"
	itHost  = "pc.tail.ts.net"
)

type phone struct {
	t      *testing.T
	env    launchEnv
	srv    *httptest.Server
	dir    string // agent settings folder (pairing files, devices)
	token  string
	login  string // Tailscale-User-Login the phone's requests carry
	others string // a second existing folder with past sessions
}

// newPhone starts the whole agent around a fake claude and returns a
// phone that talks to it the way tailscale serve forwards requests.
func newPhone(t *testing.T, entries []agentEntry) *phone {
	t.Helper()
	env := newLaunchEnv(t, entries)
	dir := t.TempDir()
	store, err := devices.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	hid, err := hidden.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A second folder that only has history (not allowed in the settings).
	others := filepath.Join(filepath.Dir(env.folder), "blog")
	os.MkdirAll(others, 0o700)
	os.MkdirAll(filepath.Join(env.projects, "blog"), 0o700)
	os.WriteFile(filepath.Join(env.projects, "blog", "77777777-7777-4777-8777-777777777777.jsonl"),
		[]byte(`{"type":"user","message":{"content":"x"},"timestamp":"2026-10-21T00:00:00.000Z","cwd":`+jsonString(others)+`,"sessionId":"77777777-7777-4777-8777-777777777777"}`+"\n"), 0o600)

	allowed := []config.Folder{{ID: "work", Path: env.folder, Server: config.FolderServer{Spawn: config.SpawnSameDir, Capacity: 4}}}
	h := api.New(api.Options{
		Version: "it", Hostname: "PC", Adapters: []core.Adapter{env.a}, Devices: store,
		Pairer:         &pairing.Pairer{Dir: dir, Devices: store, Poll: 10 * time.Millisecond, Timeout: 5 * time.Second},
		Logger:         logx.Discard(),
		ServeHosts:     []string{itHost},
		TailscaleOwner: itOwner,
		Hidden:         hid,
		Folders:        func() []config.Folder { return allowed },
		HistoryFolders: func() bool { return true },
		AddFolder: func(p string) (config.Folder, error) {
			c := config.Config{Folders: allowed}
			f, _ := c.AddFolder(p, "")
			allowed = c.Folders
			return f, nil
		},
	}).Handler()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &phone{t: t, env: env, srv: srv, dir: dir, login: itOwner, others: others}
}

// call sends one request as tailscale serve forwards it from the phone.
func (p *phone) call(method, path string, body any) (int, map[string]any) {
	p.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, p.srv.URL+path, rd)
	req.Host = itHost
	req.Header.Set("Tailscale-User-Login", p.login)
	req.Header.Set("X-Forwarded-For", "100.64.0.9")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

// pair runs the pairing flow, with the person at the PC allowing it.
func (p *phone) pair() {
	p.t.Helper()
	code, win, err := pairing.Open(p.dir, time.Now())
	if err != nil {
		p.t.Fatal(err)
	}
	go func() {
		for i := 0; i < 1000; i++ {
			if _, ok := pairing.PendingRequest(p.dir, win.ID); ok {
				pairing.Decide(p.dir, win.ID, true)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	st, r := p.call("POST", "/v1/pair", map[string]string{"code": code, "deviceName": "Galaxy"})
	if st != 202 {
		p.t.Fatalf("pair = %d %v", st, r)
	}
	for i := 0; i < 500; i++ {
		st, s := p.call("POST", "/v1/pair/status", map[string]any{"requestId": r["requestId"]})
		if st != 200 {
			p.t.Fatalf("pair status = %d %v", st, s)
		}
		if s["status"] == "approved" {
			p.token = s["token"].(string)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.t.Fatal("pairing never approved")
}

// job sends a slow action and asks for its result like the phone does.
func (p *phone) job(method, path string, body any) map[string]any {
	p.t.Helper()
	st, r := p.call(method, path, body)
	if st != 202 {
		p.t.Fatalf("%s %s = %d %v", method, path, st, r)
	}
	for i := 0; i < 2000; i++ {
		_, j := p.call("GET", "/v1/jobs/"+r["jobId"].(string), nil)
		if j["status"] != "running" {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.t.Fatal("job still running")
	return nil
}

func (p *phone) sessions(query string) map[string]map[string]any {
	p.t.Helper()
	st, r := p.call("GET", "/v1/sessions"+query, nil)
	if st != 200 {
		p.t.Fatalf("sessions = %d %v", st, r)
	}
	out := map[string]map[string]any{}
	for _, x := range r["sessions"].([]any) {
		s := x.(map[string]any)
		out[s["id"].(string)] = s
	}
	return out
}

func failedCode(j map[string]any) string {
	if j["status"] != "failed" {
		return ""
	}
	return j["error"].(map[string]any)["code"].(string)
}

// IT-01 First use: pair, see the PC, see sessions by state.
func TestIT01PairAndList(t *testing.T) {
	p := newPhone(t, []agentEntry{runningEntry, stoppedEntry, termEntry})
	if st, _ := p.call("GET", "/v1/info", nil); st != 401 {
		t.Fatalf("info before pairing = %d, want 401", st)
	}
	p.pair()
	st, info := p.call("GET", "/v1/info", nil)
	if st != 200 || info["hostname"] != "PC" {
		t.Fatalf("info = %d %v", st, info)
	}
	list := p.sessions("")
	want := map[string]string{"claude:" + id1: "running", "claude:" + id2: "stopped", "claude:" + id5: "terminal", "claude:" + id6: "history"}
	for id, state := range want {
		if list[id]["state"] != state {
			t.Errorf("%s state = %v, want %s", id, list[id]["state"], state)
		}
	}
	if got := p.sessions("?state=running"); len(got) != 1 {
		t.Errorf("running filter = %d sessions", len(got))
	}
}

// IT-02 Wake a stopped session, use it, stop it, wake it again.
func TestIT02WakeStopWake(t *testing.T) {
	p := newPhone(t, []agentEntry{stoppedEntry})
	p.pair()
	id := "claude:" + id2
	j := p.job("POST", "/v1/sessions/"+id+"/open", nil)
	r := j["result"].(map[string]any)
	if j["status"] != "done" || r["action"] != "resumed" || !strings.HasPrefix(r["remoteUrl"].(string), "https://claude.ai/code/session_") {
		t.Fatalf("open = %v", j)
	}
	if p.sessions("")[id]["state"] != "running" {
		t.Fatal("not running after open")
	}
	if st, _ := p.call("POST", "/v1/sessions/"+id+"/stop", nil); st != 200 {
		t.Fatalf("stop = %d", st)
	}
	if p.sessions("")[id]["state"] != "stopped" {
		t.Fatal("not stopped after stop")
	}
	if j := p.job("POST", "/v1/sessions/"+id+"/open", nil); j["status"] != "done" {
		t.Fatalf("second open = %v", j)
	}
	// Stopped sessions resume without flags; flags would fork a copy.
	for _, c := range p.env.launches(t) {
		if c.Args[0] == "--bg" && len(c.Args) != 3 {
			t.Errorf("resume ran with flags: %v", c.Args)
		}
	}
}

// IT-03 Open a session that only has history: resumed with remote control
// and its title.
func TestIT03OpenHistory(t *testing.T) {
	p := newPhone(t, nil)
	p.pair()
	j := p.job("POST", "/v1/sessions/claude:"+id6+"/open", nil)
	if j["status"] != "done" {
		t.Fatalf("open = %v", j)
	}
	got := p.env.launches(t)
	if len(got) != 1 || strings.Join(got[0].Args, " ") != "--bg --resume "+id6+" --remote-control -n Title 6666" {
		t.Errorf("launch = %+v", got)
	}
}

// IT-04 After a reboot the woken session takes time to reconnect; the
// phone must never get its old (archived) link (2026-10-27 regression).
func TestIT04RebootNoOldLink(t *testing.T) {
	gone := agentEntry{ID: "11111111", Kind: kindBackground, SessionID: id1, Name: "job one", State: "done"}
	p := newPhone(t, []agentEntry{gone})
	p.pair()
	t.Setenv("ASKSEMBLE_FAKE_NOLINK", "1")
	j := p.job("POST", "/v1/sessions/claude:"+id1+"/open", nil)
	if code := failedCode(j); code != core.ErrCodeLinkTimeout {
		t.Fatalf("open = %v, want remote_link_timeout instead of the old link", j)
	}
}

// IT-05 Terminal session: nothing runs, the phone is told to use /rc.
func TestIT05TerminalHint(t *testing.T) {
	p := newPhone(t, []agentEntry{termEntry})
	p.pair()
	j := p.job("POST", "/v1/sessions/claude:"+id5+"/open", nil)
	r := j["result"].(map[string]any)
	if r["hint"] != core.HintUseRCInTerminal {
		t.Fatalf("open = %v", j)
	}
	if got := p.env.launches(t); len(got) != 0 {
		t.Errorf("ran claude for a terminal session: %+v", got)
	}
}

// IT-06 New session in an allowed folder and in a folder with history;
// a raw path is refused.
func TestIT06NewSessionFolders(t *testing.T) {
	p := newPhone(t, nil)
	p.pair()
	_, fr := p.call("GET", "/v1/folders", nil)
	var historyID string
	for _, x := range fr["folders"].([]any) {
		f := x.(map[string]any)
		if f["source"] == "history" && f["path"] == p.others {
			historyID = f["id"].(string)
		}
	}
	if historyID == "" {
		t.Fatalf("history folder missing: %v", fr)
	}
	for _, folder := range []string{"work", historyID} {
		j := p.job("POST", "/v1/sessions", map[string]string{"folder": folder, "name": "it " + folder})
		if j["status"] != "done" || j["result"].(map[string]any)["action"] != "started" {
			t.Fatalf("new in %s = %v", folder, j)
		}
	}
	if st, _ := p.call("POST", "/v1/sessions", map[string]string{"folder": p.others, "name": "x"}); st != 404 {
		t.Errorf("raw path = %d, want 404", st)
	}
	got := p.env.launches(t)
	if len(got) != 2 || got[1].Cwd != p.others {
		t.Errorf("launches = %+v", got)
	}
}

// IT-07 Remove hides a session from the list.
func TestIT07Remove(t *testing.T) {
	p := newPhone(t, []agentEntry{stoppedEntry})
	p.pair()
	id := "claude:" + id2
	if st, _ := p.call("DELETE", "/v1/sessions/"+id, nil); st != 200 {
		t.Fatalf("delete = %d", st)
	}
	if _, ok := p.sessions("")[id]; ok {
		t.Error("removed session still listed")
	}
}

// IT-08 Failures the phone explains: untrusted folder, folder gone.
func TestIT08Failures(t *testing.T) {
	p := newPhone(t, nil)
	p.pair()
	t.Setenv("ASKSEMBLE_FAKE_UNTRUSTED", "1")
	if code := failedCode(p.job("POST", "/v1/sessions/claude:"+id6+"/open", nil)); code != core.ErrCodeUntrusted {
		t.Errorf("untrusted = %q", code)
	}
	t.Setenv("ASKSEMBLE_FAKE_UNTRUSTED", "")
	os.RemoveAll(p.env.folder)
	s := p.sessions("")["claude:"+id6]
	if modes := s["openModes"].([]any); len(modes) != 0 {
		t.Errorf("open modes for a gone folder = %v", modes)
	}
	if code := failedCode(p.job("POST", "/v1/sessions/claude:"+id6+"/open", nil)); code != core.ErrCodeFolderMissing {
		t.Errorf("folder gone = %q", code)
	}
}

// IT-09 Security: another Tailscale account and an unpaired phone are
// refused; unpairing drops the token on the PC.
func TestIT09Access(t *testing.T) {
	p := newPhone(t, nil)
	p.pair()
	p.login = "someone@else.com"
	if st, _ := p.call("GET", "/v1/sessions", nil); st != 403 {
		t.Errorf("other account = %d, want 403", st)
	}
	p.login = itOwner
	if st, _ := p.call("DELETE", "/v1/devices/self", nil); st != 200 {
		t.Fatalf("unpair = %d", st)
	}
	if st, _ := p.call("GET", "/v1/sessions", nil); st != 401 {
		t.Errorf("after unpair = %d, want 401", st)
	}
}
