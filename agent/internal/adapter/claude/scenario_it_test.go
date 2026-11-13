package claude

// Scenario integration tests (IT-xx in docs/testing.md): the real HTTP
// server, API, pairing, device store and Claude adapter, with only the
// claude executable faked. Each test walks a flow the phone app does, in
// order, over HTTP.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/api"
	"github.com/heeo-dev/askorus/agent/internal/api/apitest"
	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/devices"
	"github.com/heeo-dev/askorus/agent/internal/hidden"
	"github.com/heeo-dev/askorus/agent/internal/logx"
	"github.com/heeo-dev/askorus/agent/internal/pairing"
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
	login  string // the Tailscale account the node sees the phone's requests come from
	others string // a second existing folder with past sessions
	ask    string // the ask folder (P2-1)
}

// newPhone starts the whole agent around a fake claude and returns a
// phone that talks to it as through the agent's Tailscale node.
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

	ask := filepath.Join(filepath.Dir(env.folder), "Askorus", "ask") // made by the first ask
	allowed := []config.Folder{{ID: "work", Path: env.folder, Server: config.FolderServer{Spawn: config.SpawnSameDir, Capacity: 4}}}
	h := api.New(api.Options{
		Version: "it", Hostname: "PC", Adapters: []core.Adapter{env.a}, Devices: store,
		Pairer:         &pairing.Pairer{Dir: dir, Devices: store, Poll: 10 * time.Millisecond, Timeout: 5 * time.Second},
		Logger:         logx.Discard(),
		Hosts:          func() []string { return []string{itHost} },
		Owner:          func() string { return itOwner },
		Hidden:         hid,
		Folders:        func() []config.Folder { return allowed },
		HistoryFolders: func() bool { return true },
		AddFolder: func(p string) (config.Folder, error) {
			c := config.Config{Folders: allowed}
			f, _ := c.AddFolder(p, "")
			allowed = c.Folders
			return f, nil
		},
		AskFolder: ask,
	}).Handler()
	srv := httptest.NewServer(apitest.AsOwner(h))
	t.Cleanup(srv.Close)
	return &phone{t: t, env: env, srv: srv, dir: dir, login: itOwner, others: others, ask: ask}
}

// run sends a question and asks for the answers like the phone does.
func (p *phone) run(body any) map[string]any {
	p.t.Helper()
	st, r := p.call("POST", "/v1/runs", body)
	if st != 202 || r["status"] != "running" {
		p.t.Fatalf("POST /v1/runs = %d %v", st, r)
	}
	for i := 0; i < 2000; i++ {
		_, run := p.call("GET", "/v1/runs/"+r["runId"].(string), nil)
		if run["status"] != "running" {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.t.Fatal("run still running")
	return nil
}

// call sends one request as the node hands it on from the phone.
func (p *phone) call(method, path string, body any) (int, map[string]any) {
	p.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, p.srv.URL+path, rd)
	req.Host = itHost
	req.Header.Set(apitest.LoginHeader, p.login)
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
	t.Setenv("ASKORUS_FAKE_NOLINK", "1")
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
	t.Setenv("ASKORUS_FAKE_UNTRUSTED", "1")
	if code := failedCode(p.job("POST", "/v1/sessions/claude:"+id6+"/open", nil)); code != core.ErrCodeUntrusted {
		t.Errorf("untrusted = %q", code)
	}
	t.Setenv("ASKORUS_FAKE_UNTRUSTED", "")
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

// IT-10 The phone reads the last question and answer of a session (F8):
// announced as a capability, paired phones only.
func TestIT10Preview(t *testing.T) {
	p := newPhone(t, nil)
	const blog = "/v1/sessions/claude:77777777-7777-4777-8777-777777777777/preview"
	if st, _ := p.call("GET", blog, nil); st != 401 {
		t.Errorf("before pairing = %d, want 401", st)
	}
	p.pair()
	_, info := p.call("GET", "/v1/info", nil)
	if caps, _ := json.Marshal(info["capabilities"]); !strings.Contains(string(caps), `"preview"`) {
		t.Errorf("capabilities = %s, want preview", caps)
	}
	st, pv := p.call("GET", blog, nil)
	turns, _ := pv["turns"].([]any)
	if st != 200 || len(turns) != 1 || turns[0].(map[string]any)["text"] != "x" {
		t.Fatalf("preview = %d %v", st, pv)
	}
	if st, _ := p.call("GET", "/v1/sessions/claude:99999999-9999-4999-8999-999999999999/preview", nil); st != 404 {
		t.Errorf("unknown session = %d, want 404", st)
	}
}

// IT-11 Ask (P2-1): a question to Claude in the ask folder with the prompt
// on stdin, a follow-up on the same session, the ask badge in the session
// list, and the ask folder never offered for new sessions.
func TestIT11Ask(t *testing.T) {
	p := newPhone(t, nil)
	body := map[string]any{"mode": "ask", "prompt": "대한민국의 수도는?", "tier": "quick", "agents": []map[string]string{{"agent": "claude"}}}
	if st, _ := p.call("POST", "/v1/runs", body); st != 401 {
		t.Fatalf("before pairing = %d, want 401", st)
	}
	p.pair()
	_, info := p.call("GET", "/v1/info", nil)
	agents := info["agents"].([]any)
	ask, _ := agents[0].(map[string]any)["ask"].(map[string]any)
	if caps, _ := json.Marshal(info["capabilities"]); !strings.Contains(string(caps), `"ask"`) || ask == nil {
		t.Fatalf("info = %v", info)
	}
	if quick := ask["tiers"].(map[string]any)["quick"].(map[string]any); quick["model"] != "haiku" {
		t.Errorf("quick tier = %v", quick)
	}

	run := p.run(body)
	part := run["parts"].([]any)[0].(map[string]any)
	session, _ := part["session"].(string)
	if run["status"] != "done" || part["answer"] != "fake answer: 대한민국의 수도는?" || !strings.HasPrefix(session, "claude:") || part["model"] != "claude-haiku-5-5" {
		t.Fatalf("run = %v", run)
	}
	calls := askCalls(t, p.env.argsFile)
	last := calls[len(calls)-1]
	if !sameFolder(last.Cwd, p.ask) || last.Stdin != core.AskPrompt("대한민국의 수도는?") || slices.Contains(last.Args, "대한민국의 수도는?") {
		t.Errorf("ask call = %+v", last)
	}

	body["prompt"] = "인구는?"
	body["agents"] = []map[string]string{{"agent": "claude", "session": session}}
	run = p.run(body)
	part = run["parts"].([]any)[0].(map[string]any)
	if part["session"] != session || part["answer"] != "fake answer: 인구는?" {
		t.Fatalf("follow-up = %v", run)
	}
	calls = askCalls(t, p.env.argsFile)
	if args := calls[len(calls)-1].Args; !slices.Contains(args, "--resume") || args[len(args)-1] != strings.TrimPrefix(session, "claude:") {
		t.Errorf("follow-up args = %q", args)
	}

	s, ok := p.sessions("")[session]
	if !ok || !strings.Contains(fmt.Sprint(s["badges"]), "ask") {
		t.Errorf("ask session in the list = %v", s)
	}
	_, folders := p.call("GET", "/v1/folders", nil)
	for _, f := range folders["folders"].([]any) {
		if path := f.(map[string]any)["path"].(string); sameFolder(path, p.ask) {
			t.Errorf("the ask folder is offered for new sessions: %v", f)
		}
	}
	if st, r := p.call("POST", "/v1/runs", map[string]any{"mode": "code", "prompt": "x"}); st != 501 {
		t.Errorf("mode code = %d %v", st, r)
	}
}
