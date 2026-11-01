//go:build live

package live

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
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
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/devices"
	"github.com/asksemble/asksemble/agent/internal/judge"
	"github.com/asksemble/asksemble/agent/internal/logx"
	"github.com/asksemble/asksemble/agent/internal/proc"
)

// LT-06b and LT-07: comparisons through the API with the real claude,
// codex and opencode (P2-3 S3, spec part 11). The ask folder is
// <ask folder>\live, as LT-05's, so the judges run in <ask folder>\live\judge;
// Claude and Codex keep no judge conversation and OpenCode's are deleted.
// They log ids, labels, counts, models and times; never an answer, a
// judge's words or a marker. Codex quota: LT-06b judges twice with Codex,
// LT-07 asks Codex twice and judges twice.

// lockedBuffer is the agent log, written by many goroutines.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type compareEnv struct {
	*liveEnv
	server    *api.Server
	device    string
	claudeDir string
	judge     string // the judge folder
	logs      *lockedBuffer
}

// newLiveCompare is the agent with the real AIs, asking in <ask>\live and
// judging in <ask>\live\judge, with the judge commands' tested versions.
func newLiveCompare(t *testing.T) *compareEnv {
	t.Helper()
	claudeDir, err := claude.DefaultClaudeDir()
	if err != nil {
		t.Fatal(err)
	}
	folder := askFolder(t)
	store, _ := devices.Open(t.TempDir())
	token, d, err := store.Add("live test")
	if err != nil {
		t.Fatal(err)
	}
	logs := &lockedBuffer{}
	// Codex's judge schema goes to a folder of the test, not to the agent
	// folder of the agent the person runs (R-30).
	adapters := []core.Adapter{claude.New(claude.Options{ClaudeDir: claudeDir}), codex.New(codex.Options{SchemaDir: t.TempDir()}), opencode.New(opencode.Options{})}
	server := api.New(api.Options{
		Version: "live", Hostname: "PC", Adapters: adapters, Devices: store, Logger: logx.New(logs, slog.LevelDebug), AskFolder: folder,
		JudgeMinVersions: map[string]string{
			claude.Name: claude.JudgeMinVersion, codex.Name: codex.JudgeMinVersion, opencode.Name: opencode.JudgeMinVersion,
		},
	})
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(func() {
		srv.Close()
		server.Close()
	})
	return &compareEnv{
		liveEnv: &liveEnv{t: t, srv: srv, token: token, folder: folder},
		server:  server, device: d.ID, claudeDir: claudeDir, judge: core.JudgeFolder(folder), logs: logs,
	}
}

// judging checks that every AI on this PC announces judge, and returns
// them in /v1/info's order.
func (e *compareEnv) judging() []string {
	e.t.Helper()
	st, b := e.raw("GET", "/v1/info", nil)
	var info api.InfoResponse
	if st != 200 || json.Unmarshal(b, &info) != nil {
		e.t.Fatalf("info = %d", st)
	}
	var out []string
	for _, a := range info.Agents {
		e.t.Logf("%s %s available=%v judge=%+v", a.Agent, a.Version, a.Available, a.Judge)
		if a.Available && (a.Judge == nil || !slices.Contains(a.Capabilities, core.CapJudge)) {
			e.t.Errorf("%s is available but does not judge", a.Agent)
		}
		if a.Judge != nil {
			out = append(out, a.Agent)
		}
	}
	if info.Judging == nil || len(out) < 3 {
		e.t.Fatalf("judging %+v; %d AIs judge, these tests need all three", info.Judging, len(out))
	}
	return out
}

// raw sends one request and returns the status and body.
func (e *compareEnv) raw(method, path string, body any) (int, []byte) {
	e.t.Helper()
	st, m := e.call(method, path, body)
	b, _ := json.Marshal(m)
	return st, b
}

func (e *compareEnv) comparison(id string) api.ComparisonStatus {
	e.t.Helper()
	st, b := e.raw("GET", "/v1/comparisons/"+id, nil)
	var c api.ComparisonStatus
	if st != 200 || json.Unmarshal(b, &c) != nil {
		e.t.Fatalf("GET comparison = %d", st)
	}
	return c
}

// waitComparison asks for the comparison every half second until it has
// ended or limit is over. It also reports whether two requests of the
// same judging AI were seen running at the same time.
func (e *compareEnv) waitComparison(id string, limit time.Duration) (api.ComparisonStatus, map[string]bool) {
	e.t.Helper()
	together := map[string]bool{}
	deadline := time.Now().Add(limit)
	for {
		c := e.comparison(id)
		running := map[string]int{}
		for _, r := range c.Requests {
			if r.Status == judge.RequestRunning {
				running[r.Judge]++
			}
		}
		for ai, n := range running {
			if n >= 2 {
				together[ai] = true
			}
		}
		if c.Status != compares.Waiting && c.Status != compares.Running {
			return c, together
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("comparison still %s after %s", c.Status, limit)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (e *compareEnv) waitIdle(id string) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, ok := e.server.Compares().WaitIdle(ctx, e.device, id); !ok || ctx.Err() != nil {
		e.t.Errorf("WaitIdle: found %v, %v", ok, ctx.Err())
	}
}

// logComparison logs a comparison by ids, labels and counts only.
func logComparison(t *testing.T, c api.ComparisonStatus) {
	t.Helper()
	t.Logf("comparison %s: counts %+v", c.Status, c.Counts)
	for _, r := range c.Requests {
		code := ""
		if r.Error != nil {
			code = r.Error.Code
		}
		t.Logf("  %s %s→%s by %s (%s, judgeOnly %v): %s attempt %d, %d ms, %d in / %d out tokens %s",
			r.ID, r.From, r.Against, r.Judge, r.Model, r.JudgeOnly, r.Status, r.Attempt, r.Ms, r.InputTokens, r.OutputTokens, code)
	}
	for _, it := range c.Items {
		var sides []string
		for _, p := range it.Positions {
			sides = append(sides, "["+strings.Join(p.Units, " ")+"]")
		}
		t.Logf("  %s %s check=%v missing=%v numbersDiffer=%v", it.Kind, strings.Join(sides, " vs "), it.Check, it.Missing, it.NumbersDiffer)
	}
	labels := map[string]int{}
	for _, j := range c.Judgments {
		k := j.Label
		if j.Unsure {
			k += "(unsure)"
		}
		labels[k]++
	}
	t.Logf("  judgments by label: %v", labels)
}

func noSelfJudge(t *testing.T, c api.ComparisonStatus) {
	t.Helper()
	for _, r := range c.Requests {
		if r.Judge == r.From || r.Judge == r.Against {
			t.Errorf("%s %s→%s is judged by %q", r.ID, r.From, r.Against, r.Judge)
		}
	}
}

// openCodeExe finds the real opencode.exe as the adapter does: on PATH, or
// next to the npm opencode.cmd, which is never run.
func openCodeExe(t *testing.T) string {
	t.Helper()
	exe, shim, err := proc.LookExe("opencode")
	if err == nil {
		return exe
	}
	if shim != "" {
		for _, rel := range []string{
			filepath.Join("node_modules", "@opencode", "cli", "bin", "opencode.exe"),
			filepath.Join("node_modules", "opencode-ai", "node_modules", "opencode-windows-x64", "bin", "opencode.exe"),
			filepath.Join("node_modules", "opencode-windows-x64", "bin", "opencode.exe"),
		} {
			if p := filepath.Join(filepath.Dir(shim), rel); proc.CheckExe(p) == nil {
				return p
			}
		}
	}
	t.Fatalf("opencode.exe not found: %v", err)
	return ""
}

// openCodeJudgeSessions counts the OpenCode conversations left in the
// judge folder, with the real opencode's session list (read only, no
// model call).
func (e *compareEnv) openCodeJudgeSessions() int {
	e.t.Helper()
	return openCodeJudgeSessionsIn(e.t, e.judge)
}

// openCodeJudgeSessionsIn counts the OpenCode conversations with the
// judge title left in folder (writers take it too), with the real
// opencode's session list (read only, no model call).
func openCodeJudgeSessionsIn(t *testing.T, folder string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	res, err := proc.Run(ctx, proc.Cmd{Exe: openCodeExe(t), Dir: folder, Env: map[string]string{"PWD": folder},
		Args: []string{"session", "list", "--format", "json", "--standalone"}})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("opencode session list: %v, exit %d", err, res.ExitCode)
	}
	var list []struct {
		ID, Title, Directory string
	}
	if err := json.Unmarshal(bytes.TrimSpace(res.Stdout), &list); err != nil {
		t.Fatalf("opencode session list is not JSON: %v", err)
	}
	n := 0
	for _, s := range list {
		if s.Title == "asksemble-judge" && core.InFolder(s.Directory, folder) {
			n++
		}
	}
	t.Logf("opencode session list: %d sessions, %d judge-titled conversations in the folder", len(list), n)
	return n
}

// noJudgeSessionsListed checks that GET /v1/sessions has nothing in the
// judge folder.
func (e *compareEnv) noJudgeSessionsListed() {
	e.t.Helper()
	st, b := e.raw("GET", "/v1/sessions", nil)
	var res struct {
		Sessions []core.Session `json:"sessions"`
	}
	if st != 200 || json.Unmarshal(b, &res) != nil {
		e.t.Fatalf("sessions = %d", st)
	}
	for _, s := range res.Sessions {
		if core.InFolder(s.Folder, e.judge) {
			e.t.Errorf("session %s in the judge folder is listed", s.ID)
		}
	}
}

// noContentInLog checks the agent log for the markers and the answers.
func (e *compareEnv) noContentInLog(texts ...string) {
	e.t.Helper()
	logs := e.logs.String()
	for _, s := range texts {
		if s != "" && strings.Contains(logs, s) {
			e.t.Error("the agent log holds content (a marker, question or answer)")
			return
		}
	}
	if !strings.Contains(logs, "comparison ended") {
		e.t.Error("the agent log has no 'comparison ended' line")
	}
}

// LT-06b: one comparison through POST /v1/comparisons with the cut-down
// Q12 answers of LT-06a (no asks, to save quota). Six requests, all done,
// none judged by its own AI; OpenCode's two at the same time; then no
// judge conversation is kept: none in Claude transcripts, Codex rollouts
// or thread history (the content marker of the earlier question), none
// left in OpenCode's list, and none in GET /v1/sessions.
func TestLT06bCompare(t *testing.T) {
	start := time.Now()
	e := newLiveCompare(t)
	e.judging()
	m1 := []byte("LT06BMARK" + randomHex())   // in every judge prompt
	m2 := []byte("LT06BSECRET" + randomHex()) // nowhere: the counting is LT-06a's
	var list []map[string]string
	var texts []string
	for _, a := range lt06aAnswers {
		list = append(list, map[string]string{"id": a.agent, "agent": a.agent, "via": judge.ViaPC, "model": a.model, "text": a.text})
		texts = append(texts, a.text)
	}
	body := map[string]any{
		"clientComparisonId": "lt06b-" + randomHex(), "question": lt06aQuestion,
		"earlier": []string{"시험 표식 " + string(m1) + "입니다."}, "answers": list,
	}
	st, b := e.raw("POST", "/v1/comparisons", body)
	var c api.ComparisonStatus
	if st != 202 || json.Unmarshal(b, &c) != nil || c.ID == "" {
		t.Fatalf("POST /v1/comparisons = %d", st)
	}
	posted := time.Now()
	done, together := e.waitComparison(c.ID, 6*time.Minute)
	wall := time.Since(posted)
	logComparison(t, done)
	t.Logf("wall time %s; two requests of one AI seen running at once: %v", wall.Round(100*time.Millisecond), together)
	if done.Status != compares.Done || len(done.Requests) != 6 {
		t.Fatalf("comparison %s with %d requests", done.Status, len(done.Requests))
	}
	ocDone := 0
	for _, r := range done.Requests {
		if r.Status != judge.RequestDone {
			t.Errorf("%s %s→%s by %s is %s", r.ID, r.From, r.Against, r.Judge, r.Status)
		}
		if r.Judge == opencode.Name && r.Status == judge.RequestDone {
			ocDone++
		}
	}
	if ocDone != 2 {
		t.Errorf("%d OpenCode requests done, want 2", ocDone)
	}
	if !together[opencode.Name] {
		t.Error("OpenCode's two judges were never seen running at the same time")
	}
	noSelfJudge(t, done)

	e.waitIdle(c.ID)
	if n := e.openCodeJudgeSessions(); n != 0 {
		t.Errorf("%d OpenCode judge conversations are left in the judge folder", n)
	}
	e.noJudgeSessionsListed()
	e.noContentInLog(append(texts, string(m1), lt06aQuestion)...)
	lt06aMarkers(t, e.claudeDir, start.Add(-2*time.Second), m1, m2)
	t.Logf("LT-06b took %s", time.Since(start).Round(100*time.Millisecond))
}

// LT-07: a follow-up question while the answers are judged. An ask with
// compare to all three on the quick tier; once its answers are in and the
// comparison runs, a follow-up on each AI's own session (without compare)
// starts at once (202, not 409), so the same CLI asks and judges at the
// same time. The follow-up keeps its sessions and the comparison ends done
// with six requests.
func TestLT07FollowUpWhileJudging(t *testing.T) {
	start := time.Now()
	e := newLiveCompare(t)
	names := e.judging()
	var targets []map[string]string
	for _, n := range names {
		targets = append(targets, map[string]string{"agent": n})
	}
	const q = "대한민국의 수도는 어디인가요? 한 문장으로 답하세요."
	st, b := e.raw("POST", "/v1/runs", map[string]any{"mode": "ask", "tier": "quick", "agents": targets, "prompt": q, "compare": map[string]any{}})
	var run api.RunStatus
	if st != 202 || json.Unmarshal(b, &run) != nil || run.ComparisonID == "" || run.CompareError != nil {
		t.Fatalf("POST /v1/runs = %d, comparisonId %q, compareError %+v", st, run.ComparisonID, run.CompareError)
	}
	deadline := time.Now().Add(4 * time.Minute)
	for run.Status == "running" {
		if time.Now().After(deadline) {
			t.Fatal("run still running after 4 minutes")
		}
		time.Sleep(300 * time.Millisecond)
		_, b = e.raw("GET", "/v1/runs/"+run.ID, nil)
		run = api.RunStatus{}
		json.Unmarshal(b, &run)
	}
	t.Logf("asks done after %s", time.Since(start).Round(100*time.Millisecond))
	var follow []map[string]string
	sessions := map[string]string{}
	for _, p := range run.Parts {
		t.Logf("  %s %s: %s, %d ms", p.Agent, p.Model, p.Status, p.Ms)
		if p.Status != "done" || p.Session == "" {
			t.Fatalf("%s: %s", p.Agent, p.Status)
		}
		follow = append(follow, map[string]string{"agent": p.Agent, "session": p.Session})
		sessions[p.Agent] = p.Session
	}

	// The comparison starts as the last answer comes in.
	before := e.comparison(run.ComparisonID)
	for wait := time.Now().Add(10 * time.Second); before.Status == compares.Waiting && time.Now().Before(wait); {
		time.Sleep(100 * time.Millisecond)
		before = e.comparison(run.ComparisonID)
	}
	st, b = e.raw("POST", "/v1/runs", map[string]any{"mode": "ask", "tier": "quick", "agents": follow, "prompt": "그 도시의 인구를 대략 한 문장으로 알려 주세요."})
	after := e.comparison(run.ComparisonID)
	t.Logf("follow-up POST = %d with the comparison %s before and %s after", st, before.Status, after.Status)
	if st != 202 {
		t.Fatalf("follow-up while judging = %d, want 202", st)
	}
	if before.Status != compares.Running || after.Status != compares.Running {
		t.Errorf("the comparison was %s and %s around the follow-up, want running", before.Status, after.Status)
	}
	var follow2 api.RunStatus
	json.Unmarshal(b, &follow2)
	if follow2.ComparisonID != "" {
		t.Errorf("a follow-up without compare has comparison %s", follow2.ComparisonID)
	}
	deadline = time.Now().Add(4 * time.Minute)
	for follow2.Status == "running" {
		if time.Now().After(deadline) {
			t.Fatal("follow-up still running after 4 minutes")
		}
		time.Sleep(300 * time.Millisecond)
		_, b = e.raw("GET", "/v1/runs/"+follow2.ID, nil)
		follow2 = api.RunStatus{}
		json.Unmarshal(b, &follow2)
	}
	for _, p := range follow2.Parts {
		t.Logf("  follow-up %s: %s, %d ms", p.Agent, p.Status, p.Ms)
		if p.Status != "done" || p.Session != sessions[p.Agent] {
			t.Errorf("follow-up %s: %s, session kept %v", p.Agent, p.Status, p.Session == sessions[p.Agent])
		}
	}

	done, together := e.waitComparison(run.ComparisonID, 6*time.Minute)
	logComparison(t, done)
	t.Logf("two requests of one AI seen running at once: %v", together)
	if done.Status != compares.Done || len(done.Requests) != 6 {
		t.Fatalf("comparison %s with %d requests", done.Status, len(done.Requests))
	}
	noSelfJudge(t, done)
	e.waitIdle(run.ComparisonID)
	e.noJudgeSessionsListed()
	var answers []string
	for _, p := range append(run.Parts, follow2.Parts...) {
		answers = append(answers, p.Answer)
	}
	e.noContentInLog(append(answers, q)...)
	if n := e.openCodeJudgeSessions(); n != 0 {
		t.Errorf("%d OpenCode judge conversations are left in the judge folder", n)
	}
	t.Logf("LT-07 took %s", time.Since(start).Round(100*time.Millisecond))
}
