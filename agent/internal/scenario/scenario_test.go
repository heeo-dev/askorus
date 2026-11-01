package scenario

// The rig: the agent's API with the real Claude, Codex and OpenCode
// adapters, each pointed at this test binary as fakecli plays it, over
// HTTP with a device token, the way the phone talks to the agent.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/adapter/claude"
	"github.com/asksemble/asksemble/agent/internal/adapter/codex"
	"github.com/asksemble/asksemble/agent/internal/adapter/opencode"
	"github.com/asksemble/asksemble/agent/internal/api"
	"github.com/asksemble/asksemble/agent/internal/compares"
	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/devices"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
	"github.com/asksemble/asksemble/agent/internal/logx"
)

func TestMain(m *testing.M) {
	if fakecli.Active() {
		os.Exit(fakecli.Main(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// The question and the keyed answers of the scenarios (fakecli rule
// mode): Claude and Codex give different prices, all three the same
// height, and only Claude a colour number. OpenCode's second sentence has
// no number, so no judge calls it core.
const question = "이 상자의 가격과 높이는 얼마인가요?"

var answers = map[string]string{
	fakecli.RoleClaude:   "가격은 NUM p=1입니다. 높이는 NUM h=5입니다. 색 번호는 NUM c=9입니다.",
	fakecli.RoleCodex:    "가격은 NUM p=2입니다. 높이는 NUM h=5입니다.",
	fakecli.RoleOpenCode: "높이는 NUM h=5입니다. 오늘은 맑습니다.",
}

// agents is the turn order of the scenarios.
var agents = []string{fakecli.RoleClaude, fakecli.RoleCodex, fakecli.RoleOpenCode}

// syncBuffer is the agent log, written by many goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type rigOptions struct {
	answers map[string]string // fakecli's answers file
	env     map[string]string // set before the adapters are made
	judge   config.JudgeConfig
}

type rig struct {
	t         *testing.T
	exes      map[string]string
	root      string
	claudeDir string
	askDir    string // the ask folder
	judge     string // its judge folder
	adapters  []core.Adapter
	store     *devices.Store
	o         rigOptions
	server    *api.Server
	srv       *httptest.Server
	logs      *syncBuffer
	token     string
	device    string // the device id: the owner of its runs and comparisons
}

// newRig starts the agent around the three fakes. mutate may change the
// options first.
func newRig(t *testing.T, mutate func(*rigOptions)) *rig {
	t.Helper()
	exes := fakecli.Install(t)
	o := rigOptions{answers: answers}
	if mutate != nil {
		mutate(&o)
	}
	fakecli.SetAnswers(t, o.answers)
	for k, v := range o.env {
		t.Setenv(k, v)
	}
	root := t.TempDir()
	r := &rig{t: t, exes: exes, root: root, o: o, logs: &syncBuffer{},
		claudeDir: filepath.Join(root, "claude"), askDir: filepath.Join(root, "ask")}
	r.judge = core.JudgeFolder(r.askDir)
	for _, d := range []string{filepath.Join(r.claudeDir, "projects"), filepath.Join(root, "agent")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	r.adapters = []core.Adapter{
		claude.New(claude.Options{ClaudeDir: r.claudeDir, ClaudePath: exes[fakecli.RoleClaude]}),
		codex.New(codex.Options{Path: exes[fakecli.RoleCodex], SchemaDir: filepath.Join(root, "agent")}),
		opencode.New(opencode.Options{Path: exes[fakecli.RoleOpenCode]}),
	}
	store, err := devices.Open(filepath.Join(root, "devices"))
	if err != nil {
		t.Fatal(err)
	}
	r.store = store
	r.start()
	t.Cleanup(r.stop)
	return r
}

// start starts a new agent on the same adapters, folders and devices.
func (r *rig) start() {
	r.server = api.New(api.Options{
		Version: "it", Hostname: "PC", Adapters: r.adapters, Devices: r.store,
		Logger:    logx.New(r.logs, slog.LevelDebug),
		AskFolder: r.askDir, Judge: r.o.judge,
		JudgeMinVersions: map[string]string{
			claude.Name: claude.JudgeMinVersion, codex.Name: codex.JudgeMinVersion, opencode.Name: opencode.JudgeMinVersion,
		},
	})
	r.srv = httptest.NewServer(r.server.Handler())
}

func (r *rig) stop() {
	if r.srv != nil {
		r.srv.Close()
		r.server.Close()
		r.srv = nil
	}
}

// restart stops the agent and starts it again: what it kept in memory is
// gone, the devices stay.
func (r *rig) restart() {
	r.stop()
	r.start()
}

// addDevice pairs the phone as the PC's person would.
func (r *rig) addDevice() {
	r.t.Helper()
	token, d, err := r.store.Add("Galaxy")
	if err != nil {
		r.t.Fatal(err)
	}
	r.token, r.device = token, d.ID
}

// call sends one request and returns its status and body.
func (r *rig) call(method, path string, body any) (int, []byte) {
	r.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			r.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, r.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

// callJSON sends one request, wants status want and decodes the body.
func (r *rig) callJSON(method, path string, body any, want int, out any) []byte {
	r.t.Helper()
	st, b := r.call(method, path, body)
	if st != want {
		r.t.Fatalf("%s %s = %d, want %d: %s", method, path, st, want, b)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			r.t.Fatalf("%s %s: %v in %s", method, path, err, b)
		}
	}
	return b
}

func (r *rig) info() api.InfoResponse {
	r.t.Helper()
	var info api.InfoResponse
	r.callJSON("GET", "/v1/info", nil, 200, &info)
	return info
}

// agentInfo is one AI's entry of /v1/info.
func agentInfo(t *testing.T, info api.InfoResponse, agent string) core.AdapterInfo {
	t.Helper()
	i := slices.IndexFunc(info.Agents, func(a core.AdapterInfo) bool { return a.Agent == agent })
	if i < 0 {
		t.Fatalf("%s is not in /v1/info", agent)
	}
	return info.Agents[i]
}

// ask posts a run to all three AIs (or the given targets) with body's
// other fields, and returns the 202.
func (r *rig) ask(targets []map[string]string, extra map[string]any) api.RunStatus {
	r.t.Helper()
	if targets == nil {
		for _, a := range agents {
			targets = append(targets, map[string]string{"agent": a})
		}
	}
	body := map[string]any{"mode": "ask", "tier": "quick", "prompt": question, "agents": targets}
	for k, v := range extra {
		body[k] = v
	}
	var st api.RunStatus
	r.callJSON("POST", "/v1/runs", body, 202, &st)
	return st
}

// waitRun asks for a run until it has ended, as the phone does.
func (r *rig) waitRun(id string) api.RunStatus {
	r.t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		var st api.RunStatus
		r.callJSON("GET", "/v1/runs/"+id, nil, 200, &st)
		if st.Status != "running" {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	r.t.Fatal("run still running after 60 s")
	return api.RunStatus{}
}

// waitComparison asks for a comparison until it has ended and returns it
// with the body of the last GET.
func (r *rig) waitComparison(id string) (api.ComparisonStatus, []byte) {
	r.t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		var c api.ComparisonStatus
		b := r.callJSON("GET", "/v1/comparisons/"+id, nil, 200, &c)
		if c.Status != compares.Waiting && c.Status != compares.Running {
			if c.PollAfterMs != 0 {
				r.t.Errorf("an ended comparison has pollAfterMs %d", c.PollAfterMs)
			}
			return c, b
		}
		if c.PollAfterMs != compares.PollMs {
			r.t.Errorf("a %s comparison has pollAfterMs %d", c.Status, c.PollAfterMs)
		}
		time.Sleep(20 * time.Millisecond)
	}
	r.t.Fatal("comparison still running after 90 s")
	return api.ComparisonStatus{}, nil
}

// waitIdle waits until nothing of the comparison runs, OpenCode's
// cleanup included.
func (r *rig) waitIdle(id string) {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, ok := r.server.Compares().WaitIdle(ctx, r.device, id); !ok {
		r.t.Fatalf("WaitIdle: comparison %s not found", id)
	}
	if ctx.Err() != nil {
		r.t.Fatal("comparison still busy after 60 s")
	}
}

// compareRun asks all three with compare and waits for the run and its
// comparison.
func (r *rig) compareRun(compare any) (api.RunStatus, api.ComparisonStatus, []byte) {
	r.t.Helper()
	st := r.ask(nil, map[string]any{"compare": compare})
	if st.ComparisonID == "" || st.CompareError != nil {
		r.t.Fatalf("POST /v1/runs with compare: comparisonId %q, compareError %+v", st.ComparisonID, st.CompareError)
	}
	run := r.waitRun(st.ID)
	if run.ComparisonID != st.ComparisonID {
		r.t.Errorf("GET run has comparisonId %q, the 202 had %q", run.ComparisonID, st.ComparisonID)
	}
	c, b := r.waitComparison(st.ComparisonID)
	return run, c, b
}

// --- fakecli's record ---

// Kinds of recorded calls.
const (
	kindAsk    = "ask"
	kindJudge  = "judge"
	kindList   = "list"   // OpenCode session list
	kindDelete = "delete" // OpenCode session delete
	kindOther  = "other"  // claude agents
)

func kindOf(c fakecli.Call) string {
	switch c.Role {
	case fakecli.RoleClaude:
		switch {
		case slices.Contains(c.Args, "--json-schema"):
			return kindJudge
		case len(c.Args) > 0 && c.Args[0] == "-p":
			return kindAsk
		}
	case fakecli.RoleCodex:
		switch {
		case slices.Contains(c.Args, "--output-schema"):
			return kindJudge
		case slices.Contains(c.Args, "exec"):
			return kindAsk
		}
	case fakecli.RoleOpenCode:
		switch {
		case len(c.Args) > 0 && c.Args[0] == "run" && flag(c.Args, "--agent") == fakecli.JudgeAgent:
			return kindJudge
		case len(c.Args) > 0 && c.Args[0] == "run":
			return kindAsk
		case len(c.Args) > 1 && c.Args[0] == "session" && c.Args[1] == "list":
			return kindList
		case len(c.Args) > 1 && c.Args[0] == "session" && c.Args[1] == "delete":
			return kindDelete
		}
	}
	return kindOther
}

// calls are the recorded calls of kind (and role, unless it is ""), from
// index from of the record on.
func calls(t *testing.T, from int, kind, role string) []fakecli.Call {
	t.Helper()
	all := fakecli.Calls(t)
	var out []fakecli.Call
	for _, c := range all[min(from, len(all)):] {
		if kindOf(c) == kind && (role == "" || c.Role == role) {
			out = append(out, c)
		}
	}
	return out
}

// flag is the value after name in args, or "".
func flag(args []string, name string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
}

// disabled are the values after each --disable, in order.
func disabled(args []string) []string {
	var out []string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--disable" {
			out = append(out, args[i+1])
		}
	}
	return out
}

// nonceOf is the nonce of a judge prompt: the N of its "<data-N>" line,
// and how many times the open and the close line occur.
func nonceOf(prompt string) (nonce string, opens, closes int) {
	lines := strings.Split(prompt, "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "<data-") && strings.HasSuffix(l, ">") && !strings.HasPrefix(l, "</") {
			nonce = strings.TrimSuffix(strings.TrimPrefix(l, "<data-"), ">")
		}
	}
	for _, l := range lines {
		switch l {
		case "<data-" + nonce + ">":
			opens++
		case "</data-" + nonce + ">":
			closes++
		}
	}
	return nonce, opens, closes
}

// dataLines are the lines of a judge prompt's data block.
func dataLines(prompt, nonce string) []string {
	lines := strings.Split(prompt, "\n")
	open := slices.Index(lines, "<data-"+nonce+">")
	end := slices.Index(lines, "</data-"+nonce+">")
	if open < 0 || end < open {
		return nil
	}
	return lines[open+1 : end]
}

// --- the snapshot ---

// itemsOf are the cards' items of kind.
func itemsOf(c api.ComparisonStatus, kind string) []judge.Item {
	var out []judge.Item
	for _, it := range c.Items {
		if it.Kind == kind {
			out = append(out, it)
		}
	}
	return out
}

// unitsOf are every unit of an item's positions.
func unitsOf(it judge.Item) []string {
	var out []string
	for _, p := range it.Positions {
		out = append(out, p.Units...)
	}
	return out
}

// requestsBy are the comparison's requests judged by agent.
func requestsBy(c api.ComparisonStatus, agent string) []judge.Request {
	var out []judge.Request
	for _, rq := range c.Requests {
		if rq.Judge == agent {
			out = append(out, rq)
		}
	}
	return out
}
