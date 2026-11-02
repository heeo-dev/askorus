//go:build judgeeval

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

func TestMain(m *testing.M) {
	if fakecli.Active() {
		os.Exit(fakecli.Main(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// writeMini writes testdata/mini-set.json's files into a new folder, after
// edit (which may change them), and returns the folder.
func writeMini(t *testing.T, edit func(files map[string]map[string]any)) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "mini-set.json"))
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		Files map[string]map[string]any `json:"files"`
	}
	if err := json.Unmarshal(b, &set); err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(set.Files)
	}
	dir := t.TempDir()
	for name, v := range set.Files {
		out, err := json.MarshalIndent(v, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), out, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// committedData is docs/judge of this repository.
func committedData(t *testing.T) string {
	t.Helper()
	d, err := findData()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// cli runs judge-eval with args and returns its exit code and output.
func cli(t *testing.T, ctx context.Context, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runMain(ctx, args, &out, &errb)
	return code, out.String(), errb.String()
}

// fakeJudger is an in-process core.Judger for runner tests. script gives
// the reply of call n (from 1); nil answers every unit.
type fakeJudger struct {
	family string
	model  string
	script func(n int, ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error)

	mu    sync.Mutex
	calls int
	tasks []core.JudgeTask
}

func (f *fakeJudger) JudgeDefault() core.AskModel { return core.AskModel{Model: f.model} }

func (f *fakeJudger) Family(model string) string {
	if model == f.model {
		return f.family
	}
	return ""
}

func (f *fakeJudger) Judge(ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.tasks = append(f.tasks, req.Task)
	f.mu.Unlock()
	if f.script != nil {
		return f.script(n, ctx, req)
	}
	return f.answer(req.Task, judge.LabelSame, nil), nil
}

func (f *fakeJudger) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// answer is a reply judging every unit of task with label; skip leaves ids
// out.
func (f *fakeJudger) answer(task core.JudgeTask, label string, skip map[string]bool) core.JudgeResult {
	type v struct {
		ID       string   `json:"id"`
		Core     bool     `json:"core"`
		Evidence []string `json:"evidence"`
		Reason   string   `json:"reason"`
		Label    string   `json:"label"`
		Unsure   bool     `json:"unsure"`
	}
	out := struct {
		Judgments []v `json:"judgments"`
	}{Judgments: []v{}}
	for _, u := range task.Units {
		if u.ID == "" || skip[u.ID] {
			continue
		}
		out.Judgments = append(out.Judgments, v{ID: u.ID, Core: true, Evidence: []string{}, Reason: "fake " + u.ID, Label: label})
	}
	b, _ := json.Marshal(out)
	return core.JudgeResult{Output: b, Model: f.model, InputTokens: 10, OutputTokens: 2}
}

// useFakes makes buildEngines give in-process engines named after the
// real AIs, backed by fakes.
func useFakes(t *testing.T, fakes map[string]*fakeJudger) {
	t.Helper()
	old := buildEngines
	t.Cleanup(func() { buildEngines = old })
	buildEngines = func(ctx context.Context, names []string, tier, runDir string) (engineSetup, error) {
		st := engineSetup{evalFolder: t.TempDir()}
		for _, n := range names {
			f := fakes[n]
			if f == nil {
				t.Fatalf("no fake for engine %s", n)
			}
			st.engines = append(st.engines, &engine{name: n, judger: f, model: f.model, effort: "medium", family: f.family, version: "9.9.9"})
		}
		return st, nil
	}
}

func newFake(name string) *fakeJudger {
	return &fakeJudger{family: "fam-" + name, model: name + "-model"}
}

func TestFlagsRefused(t *testing.T) {
	data := writeMini(t, nil)
	out := t.TempDir()
	ctx := context.Background()
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"shape pair", []string{"run", "-shape", "pair", "-engines", "gold"}, notBuilt},
		{"labels v1four", []string{"run", "-labels", "v1four", "-engines", "gold"}, notBuilt},
		{"unknown engine", []string{"run", "-engines", "gemini"}, "unknown engine"},
		{"bad split", []string{"run", "-split", "half"}, "-split"},
		{"parallel", []string{"run", "-parallel", "5"}, "-parallel"},
		{"retry without resume", []string{"run", "-retry-failed"}, "-retry-failed"},
		{"question not in split", []string{"run", "-split", "test", "-questions", "1", "-engines", "gold"}, "not in split"},
		{"expect missing with a real engine", []string{"run", "-engines", "claude", "-run", "x1"}, "-expect"},
		{"expect mismatch", []string{"run", "-engines", "claude", "-expect", "5", "-run", "x2"}, "would start 6"},
		{"resume of no run", []string{"run", "-engines", "gold", "-resume", "-run", "none"}, "no run"},
		{"unknown command", []string{"judge"}, "unknown command"},
		{"report without run", []string{"report"}, "-run"},
		{"report bad check", []string{"report", "-run", "a", "-checks", "loud"}, "unknown check"},
	} {
		t.Run(c.name, func(t *testing.T) {
			args := append(c.args, "-data", data, "-out", out)
			if c.args[0] == "judge" {
				args = c.args
			}
			code, _, stderr := cli(t, ctx, args...)
			if code != exitFlags || !strings.Contains(stderr, c.want) {
				t.Errorf("exit %d, stderr %q; want %d and %q", code, stderr, exitFlags, c.want)
			}
		})
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Errorf("refused runs left %d entries in the out folder", len(entries))
	}
}

func TestExistingRunRefused(t *testing.T) {
	data, out := writeMini(t, nil), t.TempDir()
	if err := os.MkdirAll(filepath.Join(out, "taken"), 0o700); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := cli(t, context.Background(), "run", "-engines", "gold", "-run", "taken", "-data", data, "-out", out)
	if code != exitFlags || !strings.Contains(stderr, "-resume") {
		t.Errorf("exit %d, %q", code, stderr)
	}
}

func TestElevatedRefused(t *testing.T) {
	old := selfElevated
	t.Cleanup(func() { selfElevated = old })
	selfElevated = func() bool { return true }
	for _, sub := range []string{"run", "report", "perturb", "data", "cleanup"} {
		code, _, stderr := cli(t, context.Background(), sub)
		if code != exitError || !strings.Contains(stderr, "administrator") {
			t.Errorf("%s: exit %d, %q", sub, code, stderr)
		}
	}
}

func TestGoldRunNeedsNoExpect(t *testing.T) {
	data, out := writeMini(t, nil), t.TempDir()
	code, stdout, stderr := cli(t, context.Background(), "run", "-engines", "gold", "-run", "g", "-data", data, "-out", out)
	if code != exitOK {
		t.Fatalf("exit %d: %s %s", code, stdout, stderr)
	}
	recs, _, err := loadRecords(filepath.Join(out, "g", fileRequests), false)
	if err != nil || len(recs) != 6 {
		t.Fatalf("%d records, %v", len(recs), err)
	}
	for k := range recs {
		if !strings.HasPrefix(k, "p06|q1|-|gold|") {
			t.Errorf("key %q", k)
		}
	}
	// The console and events.log carry no content.
	events, _ := os.ReadFile(filepath.Join(out, "g", fileEvents))
	for _, s := range []string{"서울", "fake", "gold\"", "judgments"} {
		if strings.Contains(stdout, s) || strings.Contains(string(events), s) {
			t.Errorf("output or events.log holds %q", s)
		}
	}
	if !strings.Contains(stdout, "done: 6 requests recorded, 0 remaining") {
		t.Errorf("stdout %q", stdout)
	}
}
