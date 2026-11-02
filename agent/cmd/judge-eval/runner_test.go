//go:build judgeeval

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// keysOf are the keys of the lines of requests.jsonl, repeats included.
func keysOf(t *testing.T, dir string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, fileRequests))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var r record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("bad line %q", line)
		}
		out = append(out, r.Key)
	}
	return out
}

func readStop(t *testing.T, dir string) stopFile {
	t.Helper()
	var s stopFile
	b, err := os.ReadFile(filepath.Join(dir, fileStop))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestUsageLimitStopAndResume(t *testing.T) {
	data, out := writeMini(t, nil), t.TempDir()
	dir := filepath.Join(out, "u")
	limited := newFake("claude")
	limited.script = func(n int, ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
		if n == 3 {
			return core.JudgeResult{Model: limited.model}, core.Fail(core.ErrCodeJudgeFailed, fakecli.UsageLimitClaude)
		}
		return limited.answer(req.Task, judge.LabelSame, nil), nil
	}
	useFakes(t, map[string]*fakeJudger{"claude": limited})
	args := []string{"run", "-engines", "claude", "-run", "u", "-parallel", "1", "-data", data, "-out", out}
	code, stdout, stderr := cli(t, context.Background(), append(args, "-expect", "6")...)
	if code != exitUsageLimit {
		t.Fatalf("exit %d: %s %s", code, stdout, stderr)
	}
	keys := keysOf(t, dir)
	if len(keys) != 2 || limited.count() != 3 {
		t.Fatalf("%d lines after %d calls; want 2 lines after 3", len(keys), limited.count())
	}
	limitedKey := jobKey(setP06, 1, "", "claude", "claude", "opencode") // r3
	if slices.Contains(keys, limitedKey) {
		t.Error("the limited request was recorded")
	}
	if s := readStop(t, dir); s.Code != stopUsage || s.Engine != "claude" || s.Remaining != 4 {
		t.Errorf("stop.json %+v", s)
	}

	good := newFake("claude")
	useFakes(t, map[string]*fakeJudger{"claude": good})
	if code, _, stderr := cli(t, context.Background(), append(args, "-resume", "-expect", "6")...); code != exitFlags || !strings.Contains(stderr, "would start 4") {
		t.Fatalf("a wrong -expect on resume: exit %d %q", code, stderr)
	}
	code, stdout, stderr = cli(t, context.Background(), append(args, "-resume", "-expect", "4")...)
	if code != exitOK || good.count() != 4 {
		t.Fatalf("resume: exit %d after %d calls: %s %s", code, good.count(), stdout, stderr)
	}
	keys = keysOf(t, dir)
	if len(keys) != 6 || len(slices.Compact(slices.Sorted(slices.Values(keys)))) != 6 {
		t.Errorf("keys after resume %v", keys)
	}
	if _, err := os.Stat(filepath.Join(dir, fileStop)); !os.IsNotExist(err) {
		t.Error("stop.json stayed after the run finished")
	}
	m, err := readManifest(dir)
	if err != nil || len(m.Sessions) != 2 || !m.Sessions[1].Resume {
		t.Errorf("manifest sessions %+v, %v", m.Sessions, err)
	}
}

func TestInterrupt(t *testing.T) {
	data, out := writeMini(t, nil), t.TempDir()
	started := make(chan struct{}, 4)
	f := newFake("codex")
	f.script = func(n int, ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
		started <- struct{}{}
		<-ctx.Done()
		return core.JudgeResult{Model: f.model}, core.Fail(core.ErrCodeJudgeTimeout, "")
	}
	useFakes(t, map[string]*fakeJudger{"codex": f})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		<-started
		cancel()
	}()
	code, stdout, stderr := cli(t, ctx, "run", "-engines", "codex", "-run", "i", "-expect", "6", "-data", data, "-out", out)
	if code != exitInterrupted {
		t.Fatalf("exit %d: %s %s", code, stdout, stderr)
	}
	if keys := keysOf(t, filepath.Join(out, "i")); len(keys) != 0 {
		t.Errorf("in-flight requests were recorded: %v", keys)
	}
	if s := readStop(t, filepath.Join(out, "i")); s.Code != stopInterrupt || s.Remaining != 6 {
		t.Errorf("stop.json %+v", s)
	}
	if f.count() != 2 {
		t.Errorf("%d calls; want the two in flight only", f.count())
	}
}

func TestCallBudget(t *testing.T) {
	data, out := writeMini(t, nil), t.TempDir()
	f := newFake("opencode")
	useFakes(t, map[string]*fakeJudger{"opencode": f})
	code, stdout, stderr := cli(t, context.Background(), "run", "-engines", "opencode", "-run", "b", "-expect", "6", "-max-calls", "3", "-data", data, "-out", out)
	if code != exitBudget || f.count() != 3 {
		t.Fatalf("exit %d after %d calls: %s %s", code, f.count(), stdout, stderr)
	}
	if keys := keysOf(t, filepath.Join(out, "b")); len(keys) != 3 {
		t.Errorf("%d lines", len(keys))
	}
	if s := readStop(t, filepath.Join(out, "b")); s.Code != stopBudget || s.Remaining != 3 {
		t.Errorf("stop.json %+v", s)
	}
}

func TestTornLineAndRetryFailed(t *testing.T) {
	data, out := writeMini(t, nil), t.TempDir()
	dir := filepath.Join(out, "t")
	f := newFake("claude")
	f.script = func(n int, ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
		if n == 2 {
			return core.JudgeResult{Model: f.model}, core.Fail(core.ErrCodeJudgeFailed, "API Error: 529 overloaded")
		}
		return f.answer(req.Task, judge.LabelSame, nil), nil
	}
	useFakes(t, map[string]*fakeJudger{"claude": f})
	args := []string{"run", "-engines", "claude", "-run", "t", "-parallel", "1", "-data", data, "-out", out}
	if code, _, stderr := cli(t, context.Background(), append(args, "-expect", "6")...); code != exitOK {
		t.Fatalf("exit %d %s", code, stderr)
	}
	recs, _, _ := loadRecords(filepath.Join(dir, fileRequests), false)
	failedKey := jobKey(setP06, 1, "", "claude", "codex", "claude") // r2
	if r := recs[failedKey]; r.Status != statusFailed || r.Code != core.ErrCodeJudgeFailed {
		t.Fatalf("r2 = %+v", r)
	}
	// A crash tore the last line: it is dropped and cut away, and its
	// request runs again.
	path := filepath.Join(dir, fileRequests)
	b, _ := os.ReadFile(path)
	lines := strings.SplitAfter(strings.TrimSuffix(string(b), "\n"), "\n")
	last := lines[len(lines)-1]
	torn := strings.Join(lines[:len(lines)-1], "") + last[:len(last)/2]
	if err := os.WriteFile(path, []byte(torn), 0o600); err != nil {
		t.Fatal(err)
	}
	recs, _, err := loadRecords(path, false)
	if err != nil || len(recs) != 5 {
		t.Fatalf("torn file: %d records, %v", len(recs), err)
	}
	good := newFake("claude")
	useFakes(t, map[string]*fakeJudger{"claude": good})
	if code, _, stderr := cli(t, context.Background(), append(args, "-resume", "-expect", "1")...); code != exitOK || good.count() != 1 {
		t.Fatalf("resume after the torn line: exit %d, %d calls, %s", code, good.count(), stderr)
	}
	if keys := keysOf(t, dir); len(keys) != 6 {
		t.Errorf("%d lines after the resume", len(keys))
	}
	// The failure was final: a resume skips it, -retry-failed runs it again.
	if code, _, stderr := cli(t, context.Background(), append(args, "-resume", "-expect", "0")...); code != exitOK || good.count() != 1 {
		t.Fatalf("resume with nothing left: exit %d, %d calls, %s", code, good.count(), stderr)
	}
	if code, _, stderr := cli(t, context.Background(), append(args, "-resume", "-retry-failed", "-expect", "1")...); code != exitOK || good.count() != 2 {
		t.Fatalf("retry-failed: exit %d, %d calls, %s", code, good.count(), stderr)
	}
	recs, _, _ = loadRecords(path, false)
	if r := recs[failedKey]; r.Status != statusDone {
		t.Errorf("r2 after -retry-failed = %s %s", r.Status, r.Code)
	}
	if len(good.tasks) != 2 || good.tasks[1].Units[0].Text != recsTask(t, data, "codex").Units[0].Text {
		t.Error("-retry-failed ran another request than the failed one")
	}
}

// COR-10: a failure whose code is not final (agent_unavailable here, or
// not_found) is asked again by a plain -resume, without -retry-failed.
func TestResumeRerunsNonFinalFailure(t *testing.T) {
	data, out := writeMini(t, nil), t.TempDir()
	dir := filepath.Join(out, "n")
	f := newFake("claude")
	f.script = func(n int, ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
		if n == 2 {
			return core.JudgeResult{}, core.Fail(core.ErrCodeAgentUnavailable, "")
		}
		return f.answer(req.Task, judge.LabelSame, nil), nil
	}
	useFakes(t, map[string]*fakeJudger{"claude": f})
	args := []string{"run", "-engines", "claude", "-run", "n", "-parallel", "1", "-data", data, "-out", out}
	if code, _, stderr := cli(t, context.Background(), append(args, "-expect", "6")...); code != exitOK {
		t.Fatalf("exit %d %s", code, stderr)
	}
	path := filepath.Join(dir, fileRequests)
	recs, _, _ := loadRecords(path, false)
	failedKey := jobKey(setP06, 1, "", "claude", "codex", "claude") // r2
	if r := recs[failedKey]; r.Status != statusFailed || r.Code != core.ErrCodeAgentUnavailable || r.finished(false) {
		t.Fatalf("r2 = %s %s", r.Status, r.Code)
	}
	good := newFake("claude")
	useFakes(t, map[string]*fakeJudger{"claude": good})
	if code, _, stderr := cli(t, context.Background(), append(args, "-resume", "-expect", "1")...); code != exitOK || good.count() != 1 {
		t.Fatalf("resume: exit %d, %d calls, %s", code, good.count(), stderr)
	}
	recs, _, _ = loadRecords(path, false)
	if r := recs[failedKey]; r.Status != statusDone {
		t.Errorf("r2 after the resume = %s %s", r.Status, r.Code)
	}
	if len(good.tasks) != 1 || good.tasks[0].Units[0].Text != recsTask(t, data, "codex").Units[0].Text {
		t.Error("the resume ran another request than the failed one")
	}
	// Every code is final or not on purpose.
	for _, c := range []string{core.ErrCodeAgentUnavailable, core.ErrCodeNotFound} {
		if (record{Status: statusFailed, Code: c}).finished(false) {
			t.Errorf("%s is final", c)
		}
	}
}

// recsTask is the task of the mini set's direction from → claude.
func recsTask(t *testing.T, data, from string) core.JudgeTask {
	t.Helper()
	d, err := loadData(data)
	if err != nil {
		t.Fatal(err)
	}
	p := prepare(d, judge.MaxUnits)
	return judge.BuildTask(p.spec(1), judge.Request{From: from, Against: "claude"})
}

func TestResumeSubsetOfEngines(t *testing.T) {
	data, out := writeMini(t, nil), t.TempDir()
	dir := filepath.Join(out, "s")
	cl, cx := newFake("claude"), newFake("codex")
	gate := make(chan struct{})
	cl.script = func(n int, ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
		if n > 2 {
			<-gate // the codex limit comes first
			time.Sleep(50 * time.Millisecond)
		}
		return cl.answer(req.Task, judge.LabelSame, nil), nil
	}
	cx.script = func(n int, ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
		for cl.count() < 2 {
			time.Sleep(time.Millisecond)
		}
		defer close(gate)
		return core.JudgeResult{Model: cx.model}, core.Fail(core.ErrCodeJudgeFailed, fakecli.UsageLimitCodex)
	}
	useFakes(t, map[string]*fakeJudger{"claude": cl, "codex": cx})
	args := []string{"run", "-run", "s", "-parallel", "1", "-data", data, "-out", out}
	code, stdout, stderr := cli(t, context.Background(), append(args, "-engines", "claude,codex", "-expect", "12")...)
	if code != exitUsageLimit {
		t.Fatalf("exit %d: %s %s", code, stdout, stderr)
	}
	if s := readStop(t, dir); s.Engine != "codex" {
		t.Errorf("stop.json %+v", s)
	}
	recs, _, _ := loadRecords(filepath.Join(dir, fileRequests), false)
	doneClaude := 0
	for _, r := range recs {
		if r.Engine == "codex" {
			t.Errorf("codex recorded %s", r.Key)
		}
		if r.Engine == "claude" && r.Status == statusDone {
			doneClaude++
		}
	}
	if doneClaude < 2 || doneClaude > 3 {
		t.Fatalf("claude finished %d", doneClaude)
	}
	// Claude goes on alone; the manifest still names both.
	cl2 := newFake("claude")
	useFakes(t, map[string]*fakeJudger{"claude": cl2})
	rest := strconv.Itoa(6 - doneClaude)
	if code, _, stderr := cli(t, context.Background(), append(args, "-engines", "claude", "-resume", "-expect", rest)...); code != exitOK {
		t.Fatalf("claude alone: exit %d %s", code, stderr)
	}
	cx2 := newFake("codex")
	useFakes(t, map[string]*fakeJudger{"codex": cx2})
	if code, _, stderr := cli(t, context.Background(), append(args, "-engines", "codex", "-resume", "-expect", "6")...); code != exitOK {
		t.Fatalf("codex alone: exit %d %s", code, stderr)
	}
	if keys := keysOf(t, dir); len(keys) != 12 {
		t.Errorf("%d lines", len(keys))
	}
	m, _ := readManifest(dir)
	if !slices.Equal(m.Engines, []string{"claude", "codex"}) || len(m.Sessions) != 3 {
		t.Errorf("manifest engines %v, %d sessions", m.Engines, len(m.Sessions))
	}
	// An engine the run never had is refused.
	if code, _, stderr := cli(t, context.Background(), append(args, "-engines", "opencode", "-resume", "-expect", "6")...); code != exitFlags || !strings.Contains(stderr, "not one of the run's") {
		t.Errorf("a new engine on resume: exit %d %q", code, stderr)
	}
}

func TestJobOrder(t *testing.T) {
	d, err := loadData(writeMini(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	p := prepare(d, judge.MaxUnits)
	jobs := p.makeJobs(setP06, []int{1}, nil, []string{"codex", "claude"})
	var got []string
	for _, j := range jobs {
		got = append(got, j.req.ID+" "+j.key)
	}
	want := []string{
		"r1 p06|q1|-|codex|claude>codex", "r2 p06|q1|-|codex|codex>claude", "r3 p06|q1|-|codex|claude>opencode",
		"r4 p06|q1|-|codex|opencode>claude", "r5 p06|q1|-|codex|codex>opencode", "r6 p06|q1|-|codex|opencode>codex",
	}
	if !slices.Equal(got[:6], want) || len(got) != 12 || jobs[6].engine != "claude" {
		t.Errorf("jobs %v", got)
	}
	// The plan of the product makes the same requests in the same order.
	spec := p.spec(1)
	spec.Judges = []judge.JudgeAI{{Agent: "claude", Family: "anthropic"}, {Agent: "codex", Family: "openai"}, {Agent: "opencode", Family: "deepseek"}}
	plan, err := judge.Plan(spec)
	if err != nil || len(plan) != 6 {
		t.Fatalf("plan %v %v", plan, err)
	}
	for i, r := range plan {
		if r.ID != jobs[i].req.ID || r.From != jobs[i].req.From || r.Against != jobs[i].req.Against {
			t.Errorf("plan %d = %s %s>%s, job %s", i, r.ID, r.From, r.Against, jobs[i].key)
		}
	}
}

// A judge that reached for a tool or answered with another model stops the
// run (exit 6, stop.json "suspect"): its request is recorded as failed, no
// new attempt starts, and -resume goes on after a look.
func TestSuspectStops(t *testing.T) {
	for _, code := range []string{core.ErrCodeJudgeUsedTool, core.ErrCodeBadModel} {
		t.Run(code, func(t *testing.T) {
			data, out := writeMini(t, nil), t.TempDir()
			dir := filepath.Join(out, "s")
			bad := newFake("codex")
			bad.script = func(n int, ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
				if n == 2 {
					return core.JudgeResult{Model: bad.model}, core.Fail(code, "")
				}
				return bad.answer(req.Task, judge.LabelSame, nil), nil
			}
			useFakes(t, map[string]*fakeJudger{"codex": bad})
			args := []string{"run", "-engines", "codex", "-run", "s", "-parallel", "1", "-data", data, "-out", out}
			got, stdout, stderr := cli(t, context.Background(), append(args, "-expect", "6")...)
			if got != exitSuspect {
				t.Fatalf("exit %d: %s %s", got, stdout, stderr)
			}
			if keys := keysOf(t, dir); len(keys) != 2 || bad.count() != 2 {
				t.Fatalf("%d lines after %d calls; want 2 after 2", len(keys), bad.count())
			}
			if s := readStop(t, dir); s.Code != stopSuspect || s.Engine != "codex" || s.Remaining != 4 {
				t.Errorf("stop.json %+v", s)
			}
			good := newFake("codex")
			useFakes(t, map[string]*fakeJudger{"codex": good})
			if got, stdout, stderr := cli(t, context.Background(), append(args, "-resume", "-expect", "4")...); got != exitOK || good.count() != 4 {
				t.Fatalf("resume: exit %d after %d calls: %s %s", got, good.count(), stdout, stderr)
			}
		})
	}
}
