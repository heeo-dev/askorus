package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/api"
	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/devices"
	"github.com/heeo-dev/askorus/agent/internal/logx"
	"github.com/heeo-dev/askorus/agent/internal/tsnode"
)

// The agent's own Tailscale node (#57).

// fakeNode is a node whose state the test sets.
type fakeNode struct {
	mu      sync.Mutex
	st      tsnode.Status
	err     error // what Status fails with, when set
	logins  int
	logouts int
}

func (f *fakeNode) set(st tsnode.Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.st = st
}

func (f *fakeNode) Status(context.Context) (tsnode.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return tsnode.Status{}, f.err
	}
	return f.st, nil
}

func (f *fakeNode) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeNode) Login(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logins++
	return nil
}

func (f *fakeNode) Logout(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logouts++
	f.st = tsnode.Status{State: tsnode.StateNeedsLogin}
	return nil
}

// agentRuns makes the commands believe an agent runs (or not) for a test.
func agentRuns(t *testing.T, on bool) {
	t.Helper()
	old := agentRunning
	agentRunning = func(int) bool { return on }
	t.Cleanup(func() { agentRunning = old })
}

func fastLoginPoll(t *testing.T) {
	t.Helper()
	old := loginPoll
	loginPoll = 5 * time.Millisecond
	t.Cleanup(func() { loginPoll = old })
}

// The watch gives the API the owner and the node's names only once phones
// can reach the node, writes the state for the other commands, and tells
// the person at the console once per state.
func TestNodeWatch(t *testing.T) {
	agentRuns(t, true)
	dir := t.TempDir()
	node := &fakeNode{}
	var console bytes.Buffer
	w := &nodeWatch{node: node, dir: dir, port: 8790, logger: logx.Discard(), out: &console}
	ctx := context.Background()

	// Not signed in: no owner, no names, and the link is shown.
	node.set(needsLoginNode().Status)
	w.once(ctx)
	if w.owner() != "" || w.hosts() != nil {
		t.Errorf("before sign-in: owner %q, hosts %q", w.owner(), w.hosts())
	}
	if n, ok := readNodeInfo(dir); !ok || n.signedIn() || n.LoginURL == "" || n.Port != 8790 || n.PID != os.Getpid() {
		t.Errorf("node file before sign-in = %+v %v", n, ok)
	}
	if !strings.Contains(console.String(), "https://login.tailscale.com/a/abc123") || !strings.Contains(console.String(), "askorus-agent login") {
		t.Errorf("console before sign-in:\n%s", console.String())
	}

	// Signing in but not through yet (the state says Starting with the
	// owner already known): still nobody is the owner for the API.
	starting := signedInNode().Status
	starting.State = "Starting"
	node.set(starting)
	w.once(ctx)
	if w.owner() != "" {
		t.Errorf("owner while starting = %q", w.owner())
	}

	node.set(signedInNode().Status)
	console.Reset()
	w.once(ctx)
	w.once(ctx) // the same state again says nothing more
	if w.owner() != "me@example.com" {
		t.Errorf("owner = %q", w.owner())
	}
	if want := []string{"askorus-pc.example.ts.net", "askorus-pc", "100.64.0.1"}; !slices.Equal(w.hosts(), want) {
		t.Errorf("hosts = %q, want %q", w.hosts(), want)
	}
	if n, ok := readNodeInfo(dir); !ok || !n.signedIn() || n.Owner != "me@example.com" {
		t.Errorf("node file = %+v %v", n, ok)
	}
	if got := strings.Count(console.String(), "signed in to Tailscale as me@example.com"); got != 1 {
		t.Errorf("signed-in line printed %d times:\n%s", got, console.String())
	}

	// A file left by an agent that no longer runs says nothing.
	agentRunning = func(int) bool { return false }
	if _, ok := readNodeInfo(dir); ok {
		t.Error("a node file was believed although no agent runs")
	}
}

// A background run's console is a file (background.out): the login link, the
// account and the device name stay out of it, since a link in a log someone
// shares would let its reader put this PC on their own Tailscale network
// (#57 review). And a node that cannot be read for a while vouches for
// nobody.
func TestNodeWatchQuietAndUnreadable(t *testing.T) {
	agentRuns(t, true)
	node := &fakeNode{}
	var console bytes.Buffer
	w := &nodeWatch{node: node, dir: t.TempDir(), port: 8790, logger: logx.Discard(), out: &console, quiet: true}
	ctx := context.Background()

	node.set(needsLoginNode().Status)
	w.once(ctx)
	node.set(signedInNode().Status)
	w.once(ctx)
	got := console.String()
	for _, private := range []string{"login.tailscale.com", "me@example.com", "askorus-pc", "100.64."} {
		if strings.Contains(got, private) {
			t.Errorf("the background console holds %q:\n%s", private, got)
		}
	}
	if !strings.Contains(got, "run askorus-agent login") || !strings.Contains(got, "signed in to Tailscale;") {
		t.Errorf("the background console does not say the state:\n%s", got)
	}
	// The other commands still get the link and the account from the file.
	if w.owner() != "me@example.com" {
		t.Errorf("owner = %q", w.owner())
	}

	node.fail(context.DeadlineExceeded)
	for i := 0; i < statusFailsMax-1; i++ {
		w.once(ctx)
	}
	if w.owner() == "" {
		t.Error("a short hiccup already forgot the owner")
	}
	w.once(ctx)
	if w.owner() != "" || w.hosts() != nil {
		t.Errorf("a node that cannot be read still vouches: owner %q, hosts %q", w.owner(), w.hosts())
	}
	node.fail(nil)
	w.once(ctx)
	if w.owner() != "me@example.com" {
		t.Errorf("owner after the node answers again = %q", w.owner())
	}
}

// The other commands ask the running agent for a login link or a logout by
// leaving a word in a file; the watch takes it once.
func TestNodeRequests(t *testing.T) {
	agentRuns(t, true)
	dir := t.TempDir()
	node := &fakeNode{}
	node.set(signedInNode().Status)
	w := &nodeWatch{node: node, dir: dir, port: 8790, logger: logx.Discard(), out: &bytes.Buffer{}}
	ctx := context.Background()

	if err := requestNode(dir, "login"); err != nil {
		t.Fatal(err)
	}
	w.once(ctx)
	w.once(ctx)
	if node.logins != 1 {
		t.Errorf("logins = %d, want 1", node.logins)
	}
	if err := requestNode(dir, "logout"); err != nil {
		t.Fatal(err)
	}
	w.once(ctx)
	w.once(ctx)
	if node.logouts != 1 || w.owner() != "" || w.hosts() != nil {
		t.Errorf("after logout: logouts %d, owner %q, hosts %q", node.logouts, w.owner(), w.hosts())
	}
	if _, err := os.Stat(filepath.Join(dir, nodeRequestFile)); !os.IsNotExist(err) {
		t.Errorf("the request file stayed: %v", err)
	}
	// Anything else in the file is dropped.
	_ = requestNode(dir, "delete everything")
	w.once(ctx)
	if node.logins != 1 || node.logouts != 1 {
		t.Errorf("an unknown request did something: %d %d", node.logins, node.logouts)
	}
}

// fakeCallers says who each connection is from.
type fakeCallers struct {
	login, ip string
	ok        bool
}

func (f fakeCallers) Caller(context.Context, string) (string, string, bool) {
	return f.login, f.ip, f.ok
}

// Only the node says who sent a request (#57). What the sender writes into
// the request, such as the headers `tailscale serve` used to add, counts
// for nothing, and a paired phone's token alone does not get in.
func TestOnlyTheNodeSaysWhoCalls(t *testing.T) {
	dir := t.TempDir()
	store, err := devices.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := store.Add("phone")
	if err != nil {
		t.Fatal(err)
	}
	srv := api.New(api.Options{
		Version: "t", Hostname: "PC", Devices: store, Logger: logx.Discard(), Adapters: []core.Adapter{},
		Hosts: func() []string { return []string{"askorus-pc.example.ts.net"} },
		Owner: func() string { return "me@example.com" },
	})
	t.Cleanup(srv.Close)
	get := func(who callerSource, hdr map[string]string) int {
		r := httptest.NewRequest("GET", "/v1/info", nil)
		r.Host = "askorus-pc.example.ts.net:8790"
		r.RemoteAddr = "100.64.0.9:40000"
		r.Header.Set("Authorization", "Bearer "+token)
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		viaNode(who, srv.Handler()).ServeHTTP(w, r)
		return w.Code
	}
	forged := map[string]string{"Tailscale-User-Login": "me@example.com", "X-Forwarded-For": "100.64.0.9"}
	for name, c := range map[string]struct {
		who  fakeCallers
		hdr  map[string]string
		want int
	}{
		"the owner's phone":                    {fakeCallers{"me@example.com", "100.64.0.9", true}, nil, 200},
		"the owner's phone, other letter case": {fakeCallers{"Me@Example.com", "100.64.0.9", true}, nil, 200},
		"a device the node cannot place":       {fakeCallers{}, nil, http.StatusForbidden},
		"the same with forged headers":         {fakeCallers{}, forged, http.StatusForbidden},
		"another account's device":             {fakeCallers{"kid@example.com", "100.64.0.7", true}, nil, http.StatusForbidden},
		"another account with forged headers":  {fakeCallers{"kid@example.com", "100.64.0.7", true}, forged, http.StatusForbidden},
		"an address outside the tailnet":       {fakeCallers{"me@example.com", "192.168.0.5", true}, nil, http.StatusForbidden},
	} {
		if got := get(c.who, c.hdr); got != c.want {
			t.Errorf("%s: %d, want %d", name, got, c.want)
		}
	}
	// The API handler served without the node (on any other socket) lets
	// nobody in, whatever the request says.
	r := httptest.NewRequest("GET", "/v1/info", nil)
	r.Host = "askorus-pc.example.ts.net:8790"
	r.Header.Set("Authorization", "Bearer "+token)
	for k, v := range forged {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("without the node: %d, want 403", w.Code)
	}
}

// login shows the link and its QR code and returns once the person has
// signed in.
func TestWaitLogin(t *testing.T) {
	agentRuns(t, true)
	fastLoginPoll(t)
	dir := t.TempDir()
	if err := writeNodeInfo(dir, needsLoginNode()); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(60 * time.Millisecond)
		_ = writeNodeInfo(dir, signedInNode())
	}()
	var out bytes.Buffer
	if err := waitLogin(context.Background(), dir, &out, 5*time.Second); err != nil {
		t.Fatalf("waitLogin: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, s := range []string{"https://login.tailscale.com/a/abc123", "Nothing of Tailscale needs installing", "Tailscale을 따로 설치할 필요는 없습니다",
		"signed in to Tailscale as me@example.com", "askorus-pc.example.ts.net", "█"} {
		if !strings.Contains(got, s) {
			t.Errorf("output lacks %q:\n%s", s, got)
		}
	}
	if strings.Count(got, "https://login.tailscale.com/a/abc123") != 1 {
		t.Errorf("the link was shown more than once:\n%s", got)
	}
}

// With no link yet (signed out, or the key ran out), login asks the running
// agent for one. Ctrl+C only stops the waiting.
func TestWaitLoginAsksForALink(t *testing.T) {
	agentRuns(t, true)
	fastLoginPoll(t)
	dir := t.TempDir()
	_ = writeNodeInfo(dir, nodeInfo{Port: 8790, Status: tsnode.Status{State: tsnode.StateNeedsLogin}})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	if err := waitLogin(ctx, dir, &out, 5*time.Second); err != nil {
		t.Fatalf("waitLogin: %v", err)
	}
	if got := takeNodeRequest(dir); got != "login" {
		t.Errorf("request = %q, want login", got)
	}
	if !strings.Contains(out.String(), "stopped waiting") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestWaitLoginWithoutAgent(t *testing.T) {
	agentRuns(t, false)
	var out bytes.Buffer
	err := waitLogin(context.Background(), t.TempDir(), &out, time.Second)
	if err == nil || !strings.Contains(err.Error(), "askorus-agent start") {
		t.Errorf("err = %v", err)
	}
}

// pair needs a running agent that is signed in to Tailscale: without that
// no phone could reach the PC, so it says what to do instead of showing a
// QR code that cannot work.
func TestPairNeedsASignedInAgent(t *testing.T) {
	dir := t.TempDir()
	if err := config.Save(dir, config.Default()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	agentRuns(t, false)
	if err := pair(dir, nil, strings.NewReader(""), &out); err == nil || !strings.Contains(err.Error(), "askorus-agent start") {
		t.Errorf("no agent: %v", err)
	}
	agentRunning = func(int) bool { return true }
	_ = writeNodeInfo(dir, needsLoginNode())
	if err := pair(dir, nil, strings.NewReader(""), &out); err == nil || !strings.Contains(err.Error(), "askorus-agent login") {
		t.Errorf("not signed in: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("pair printed something before refusing:\n%s", out.String())
	}
}

// The run without a Tailscale node (ASKORUS_TEST_NO_TAILNET=1, for the
// tests of installing and starting): it opens nothing and names nobody as
// a caller, so the API behind it lets nobody in.
func TestNoNodeRun(t *testing.T) {
	t.Setenv(noNodeEnv, "1")
	node, err := startNode(t.TempDir(), "PC")
	if err != nil {
		t.Fatal(err)
	}
	st, _ := node.Status(context.Background())
	if st.State != stateNoNode || st.Name != "" || st.Owner != "" {
		t.Errorf("status = %+v", st)
	}
	if _, _, ok := node.Caller(context.Background(), "100.64.0.9:1"); ok {
		t.Error("a run without a node named a caller")
	}
	ln, err := node.Listen(8790)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan error, 1)
	go func() {
		_, err := ln.Accept()
		got <- err
	}()
	select {
	case err := <-got:
		t.Fatalf("Accept returned before Close: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	node.Close()
	node.Close() // closing twice is fine
	select {
	case err := <-got:
		if err == nil {
			t.Error("Accept gave a connection")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Accept did not end on Close")
	}

	// The watch reports it, and the status line says it is a test run.
	agentRuns(t, true)
	dir := t.TempDir()
	w := &nodeWatch{node: node, dir: dir, port: 8790, logger: logx.Discard(), out: &bytes.Buffer{}}
	w.once(context.Background())
	if w.owner() != "" || w.hosts() != nil {
		t.Errorf("owner %q, hosts %q", w.owner(), w.hosts())
	}
	if level, line := nodeLine(dir, true, time.Now()); level != "warn" || !strings.Contains(line.en, "without Tailscale (a test run") {
		t.Errorf("node line: %s %q", level, line.en)
	}
}

func TestNodeLine(t *testing.T) {
	agentRuns(t, true)
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	soon := signedInNode()
	soon.KeyExpiry = now.Add(5 * 24 * time.Hour)
	later := signedInNode()
	later.KeyExpiry = now.Add(100 * 24 * time.Hour)
	approval := nodeInfo{Port: 8790, Status: tsnode.Status{State: tsnode.StateNeedsApproval}}
	for name, c := range map[string]struct {
		node  *nodeInfo
		level string
		says  string
	}{
		"signed in":      {ptr(signedInNode()), "ok", "askorus-pc.example.ts.net:8790"},
		"key far off":    {&later, "ok", "me@example.com"},
		"key ends soon":  {&soon, "warn", "2026-11-18"},
		"needs login":    {ptr(needsLoginNode()), "bad", "askorus-agent login"},
		"needs approval": {&approval, "bad", "관리 화면"},
		"no file yet":    {nil, "warn", "알 수 없습니다"},
	} {
		dir := t.TempDir()
		if c.node != nil {
			_ = writeNodeInfo(dir, *c.node)
		}
		level, line := nodeLine(dir, true, now)
		if level != c.level || !strings.Contains(line.en+line.ko, c.says) {
			t.Errorf("%s: %s %q / %q", name, level, line.en, line.ko)
		}
	}
}

func ptr[T any](v T) *T { return &v }
