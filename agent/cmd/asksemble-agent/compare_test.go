package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/compares"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// ---- fakes ----------------------------------------------------------------

// fakeJudgeAI is an AI program that judges in process, by default with
// fakecli's rule judge, as the fake CLIs do. It never runs a program.
type fakeJudgeAI struct {
	name, family, version string
	avail                 bool
	def                   core.AskModel
	tiers                 map[string]core.AskModel
	// judge replaces the rule judge.
	judge func(ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error)

	mu    sync.Mutex
	calls []core.JudgeRequest
}

func newJudgeAI(name, family, model string) *fakeJudgeAI {
	return &fakeJudgeAI{
		name: name, family: family, version: "1.2.3", avail: true,
		def:   core.AskModel{Model: model, Effort: "medium"},
		tiers: map[string]core.AskModel{core.TierStandard: {Model: name + "-standard"}},
	}
}

// threeAIs are fakes of the three AI programs, judging with their real
// default models.
func threeAIs() []*fakeJudgeAI {
	return []*fakeJudgeAI{
		newJudgeAI("claude", "anthropic", "haiku"),
		newJudgeAI("codex", "openai", "gpt-6-luna"),
		newJudgeAI("opencode", "deepseek", "opencode-go/deepseek-v4.1-flash"),
	}
}

func adaptersOf(ais []*fakeJudgeAI) []core.Adapter {
	out := make([]core.Adapter, len(ais))
	for i, a := range ais {
		out[i] = a
	}
	return out
}

func (f *fakeJudgeAI) Agent() string { return f.name }
func (f *fakeJudgeAI) Info(context.Context) core.AdapterInfo {
	return core.AdapterInfo{Agent: f.name, Version: f.version, Available: f.avail, Capabilities: []core.Capability{core.CapAsk}}
}
func (f *fakeJudgeAI) ListSessions(context.Context) (core.ListResult, error) {
	return core.ListResult{}, nil
}
func (f *fakeJudgeAI) Ask(context.Context, core.AskRequest) (core.AskResult, error) {
	return core.AskResult{}, errors.New("the fake does not answer")
}
func (f *fakeJudgeAI) AskTiers() map[string]core.AskModel { return f.tiers }
func (f *fakeJudgeAI) JudgeDefault() core.AskModel        { return f.def }
func (f *fakeJudgeAI) Family(model string) string {
	if model == "" {
		return ""
	}
	return f.family
}

func (f *fakeJudgeAI) Judge(ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	j := f.judge
	f.mu.Unlock()
	if j != nil {
		return j(ctx, req)
	}
	return core.JudgeResult{Output: json.RawMessage(fakecli.Answer(req.Prompt, false)), Model: req.Model}, nil
}

func (f *fakeJudgeAI) judgeCalls() []core.JudgeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// keyRe finds "key=n" in a unit's text.
var keyRe = regexp.MustCompile(`([a-z])=(-?[0-9]+)`)

// keyedJudge judges from the task: an a-unit with "k=n" is the opposite of
// a b-unit with "k=m", m ≠ n, the same as one with "k=n", and absent when
// no b-unit has k; such a unit is core. A unit without a key is absent and
// not core.
func keyedJudge(_ context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
	type entry struct {
		ID       string   `json:"id"`
		Core     bool     `json:"core"`
		Evidence []string `json:"evidence"`
		Reason   string   `json:"reason"`
		Label    string   `json:"label"`
		Unsure   bool     `json:"unsure"`
	}
	out := struct {
		Judgments []entry `json:"judgments"`
	}{Judgments: []entry{}}
	for _, u := range req.Task.Units {
		if u.ID == "" {
			continue
		}
		e := entry{ID: u.ID, Evidence: []string{}, Reason: "fake", Label: judge.LabelAbsent}
		if k := keyRe.FindStringSubmatch(u.Text); k != nil {
			e.Core = true
			for _, o := range req.Task.Other {
				if ok := keyRe.FindStringSubmatch(o.Text); o.ID != "" && ok != nil && ok[1] == k[1] {
					e.Label, e.Evidence = judge.LabelSame, []string{o.ID}
					if ok[2] != k[2] {
						e.Label = judge.LabelOpposite
					}
					break
				}
			}
		}
		out.Judgments = append(out.Judgments, e)
	}
	b, _ := json.Marshal(out)
	return core.JudgeResult{Output: b, Model: req.Model}, nil
}

func testGate(t *testing.T) compares.Gate {
	return compares.Gate{
		MinVersions: map[string]string{"claude": "1.0.0", "codex": "1.0.0", "opencode": "1.0.0"},
		Folder:      filepath.Join(t.TempDir(), "ask", core.JudgeFolderName),
	}
}

func spec(answers ...compares.DirectAnswer) compares.DirectSpec {
	return compares.DirectSpec{Owner: compareOwner, Question: "질문은 무엇입니까?", Answers: answers}
}

func pcAnswer(agent, model, text string) compares.DirectAnswer {
	return compares.DirectAnswer{ID: agent, Agent: agent, Via: judge.ViaPC, Model: model, Text: text}
}

// ---- compareWith ----------------------------------------------------------

// Two answers, judged by the third AI only: the differ card, the empty
// cards, the request lines and the counts, in both languages.
func TestCompareWithRule(t *testing.T) {
	ais := threeAIs()
	var out bytes.Buffer
	s := spec(pcAnswer("claude", "sonnet", "가격은 NUM 1입니다."), pcAnswer("codex", "gpt-6.1-terra", "가격은 NUM 2입니다.\x1b[2J"))
	gate := testGate(t)
	if err := compareWith(context.Background(), adaptersOf(ais), gate, s, &out, false); err != nil {
		t.Fatalf("compareWith: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"comparing 2 answers; judges: Claude Code haiku · Codex gpt-6-luna · OpenCode opencode-go/deepseek-v4.1-flash",
		"답 2개를 비교합니다. 판정: Claude Code haiku",
		"== Differ / 다른 말 (1)",
		"  - [claude] 가격은 NUM 1입니다.",
		"    ↔ [codex] 가격은 NUM 2입니다.",
		"== Check this / 확인 필요 (0)",
		"== Agree / 같은 말 (0)",
		"== Only one / 한 AI만 (0)",
		"(none / 없음)",
		"== Judged by / 판정한 AI (2)",
		"r1 opencode (judge only / 판정만): claude → codex · done",
		"r2 opencode (judge only / 판정만): codex → claude · done",
		"opencode-go/deepseek-v4.1-flash",
		"differ 1 · check this 0 · agree 0 · only one 0 · not compared 0 · not judged 0",
		"다른 말 1 · 확인 필요 0 · 같은 말 0 · 한 AI만 0",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("an AI's control character reached the terminal: %q", got)
	}
	if n := len(ais[0].judgeCalls()) + len(ais[1].judgeCalls()); n != 0 {
		t.Errorf("an answer's own AI judged %d times", n)
	}
	if n := len(ais[2].judgeCalls()); n != 2 {
		t.Errorf("opencode judged %d times, want 2", n)
	}
	for _, c := range ais[2].judgeCalls() {
		if c.Folder != gate.Folder || c.Model != "opencode-go/deepseek-v4.1-flash" || c.Effort != "medium" ||
			!strings.Contains(c.Prompt, "가격은 NUM 1입니다.") || !strings.Contains(c.Prompt, "질문은 무엇입니까?") {
			t.Errorf("judge request: folder %q, model %q %q", c.Folder, c.Model, c.Effort)
		}
	}

	// -judges: only those, in its order of preference.
	s.Judges = []string{"opencode", "claude"}
	out.Reset()
	if err := compareWith(context.Background(), adaptersOf(threeAIs()), gate, s, &out, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "judges: OpenCode opencode-go/deepseek-v4.1-flash · Claude Code haiku\n") {
		t.Errorf("output:\n%s", out.String())
	}
}

// Three answers: every card gets its item (IT-12's answers, keyed), and
// no request is judged by the AI of its from or against answer.
func TestCompareWithEveryCard(t *testing.T) {
	ais := threeAIs()
	for _, a := range ais {
		a.judge = keyedJudge
	}
	var out bytes.Buffer
	s := spec(
		pcAnswer("claude", "sonnet", "가격은 p=1입니다. 높이는 h=5입니다. 색 번호는 c=9입니다."),
		pcAnswer("codex", "gpt-6.1-terra", "가격은 p=2입니다. 높이는 h=5입니다."),
		pcAnswer("opencode", "opencode-go/kimi-k3", "높이는 h=5입니다. 오늘은 맑습니다."),
	)
	if err := compareWith(context.Background(), adaptersOf(ais), testGate(t), s, &out, false); err != nil {
		t.Fatalf("compareWith: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"comparing 3 answers",
		"== Differ / 다른 말 (1)",
		"  - [claude] 가격은 p=1입니다.",
		"    ↔ [codex] 가격은 p=2입니다.",
		"== Agree / 같은 말 (1)",
		"[claude, codex, opencode] 높이는 h=5입니다. (+2 more / 2개 더)",
		// OpenCode's "오늘은 맑습니다." has no number, so no judge calls it
		// core; since S5c it is shown after the core claims.
		"== Only one / 한 AI만 (2)",
		"  - [claude] 색 번호는 c=9입니다.\n  - [opencode] 오늘은 맑습니다.",
		"== Judged by / 판정한 AI (6)",
		"differ 1 · check this 0 · agree 1 · only one 2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "judge only") {
		t.Errorf("an AI that answered is shown as judge only:\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		f := regexp.MustCompile(`^  r\d+ (\S+): (\S+) → (\S+) · done`).FindStringSubmatch(line)
		if strings.HasPrefix(line, "  r") && f == nil {
			t.Errorf("request line %q", line)
		}
		if f != nil && (f[1] == f[2] || f[1] == f[3]) {
			t.Errorf("self-judged: %q", line)
		}
	}
}

// -json prints the comparison as GET /v1/comparisons/{id} does, and
// nothing else.
func TestCompareWithJSON(t *testing.T) {
	var out bytes.Buffer
	s := spec(pcAnswer("claude", "sonnet", "가격은 NUM 1입니다."), pcAnswer("codex", "gpt-6.1-terra", "가격은 NUM 2입니다."))
	if err := compareWith(context.Background(), adaptersOf(threeAIs()), testGate(t), s, &out, true); err != nil {
		t.Fatal(err)
	}
	var c compares.Comparison
	if err := json.Unmarshal(out.Bytes(), &c); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if c.Status != compares.Done || c.Source != compares.SourceDirect || c.Engine != compares.EngineName ||
		len(c.Requests) != 2 || c.Counts.Differ != 1 || len(c.Answers) != 2 || len(c.Answers[0].Units) != 1 {
		t.Errorf("comparison = %+v", c)
	}
	if strings.Contains(out.String(), `"reason"`) || strings.Contains(out.String(), "comparing") {
		t.Errorf("JSON has more than the comparison:\n%s", out.String())
	}
}

// A comparison with no request done is an error with its code; the
// requests are printed with the AI's message, the error has the code.
func TestCompareWithFailed(t *testing.T) {
	ais := threeAIs()
	ais[2].judge = func(context.Context, core.JudgeRequest) (core.JudgeResult, error) {
		return core.JudgeResult{}, core.Fail(core.ErrCodeJudgeFailed, "Usage limit reached for opencode-go. Try again later.")
	}
	var out bytes.Buffer
	s := spec(pcAnswer("claude", "sonnet", "가격은 NUM 1입니다."), pcAnswer("codex", "gpt-6.1-terra", "가격은 NUM 2입니다."))
	err := compareWith(context.Background(), adaptersOf(ais), testGate(t), s, &out, false)
	if err == nil || !strings.Contains(err.Error(), "the comparison failed (judge_failed)") || !strings.Contains(err.Error(), "비교에 실패했습니다 (judge_failed)") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out.String(), "r1 opencode (judge only / 판정만): claude → codex · failed: judge_failed Usage limit reached") {
		t.Errorf("output:\n%s", out.String())
	}
}

// No AI that judges, or none among -judges: an error, not a comparison.
func TestCompareWithNoJudge(t *testing.T) {
	s := spec(pcAnswer("claude", "sonnet", "가격은 NUM 1입니다."), pcAnswer("codex", "gpt-6.1-terra", "가격은 NUM 2입니다."))
	g := testGate(t)
	g.Folder = ""
	var out bytes.Buffer
	err := compareWith(context.Background(), adaptersOf(threeAIs()), g, s, &out, false)
	if err == nil || !strings.Contains(err.Error(), "no AI judges on this PC") || !strings.Contains(err.Error(), "판정하는 AI가 없어") {
		t.Errorf("err = %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("printed %q", out.String())
	}

	// Judging turned off in the settings says so.
	g = testGate(t)
	g.Settings.Off = true
	err = compareWith(context.Background(), adaptersOf(threeAIs()), g, s, &out, false)
	if err == nil || !strings.Contains(err.Error(), "judge.off") || !strings.Contains(err.Error(), "꺼져 있습니다") || out.Len() != 0 {
		t.Errorf("off: err = %v, out %q", err, out.String())
	}

	// Only the AIs of the answers may judge: no pair has a judge.
	s.Judges = []string{"claude", "codex"}
	out.Reset()
	err = compareWith(context.Background(), adaptersOf(threeAIs()), testGate(t), s, &out, false)
	if err == nil || !strings.Contains(err.Error(), "no_judge") {
		t.Errorf("err = %v", err)
	}
	if !strings.Contains(out.String(), "judges: Claude Code haiku · Codex gpt-6-luna\n") || !strings.Contains(out.String(), "(no judge / 판정할 AI 없음): claude → codex · failed: no_judge") {
		t.Errorf("output:\n%s", out.String())
	}
}

// The 10-minute limit and Ctrl+C end the comparison and its judges.
func TestCompareWithLimitAndCancel(t *testing.T) {
	old := compareLimit
	t.Cleanup(func() { compareLimit = old })
	compareLimit = 200 * time.Millisecond
	block := func(ctx context.Context, _ core.JudgeRequest) (core.JudgeResult, error) {
		<-ctx.Done()
		return core.JudgeResult{}, core.Fail(core.ErrCodeJudgeTimeout, "")
	}
	s := spec(pcAnswer("claude", "sonnet", "가격은 NUM 1입니다."), pcAnswer("codex", "gpt-6.1-terra", "가격은 NUM 2입니다."))

	ais := threeAIs()
	ais[2].judge = block
	start := time.Now()
	err := compareWith(context.Background(), adaptersOf(ais), testGate(t), s, &bytes.Buffer{}, false)
	if err == nil || !strings.Contains(err.Error(), "did not end within") || !strings.Contains(err.Error(), "안에 끝나지 않았습니다") {
		t.Errorf("err = %v", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("took %v", d)
	}

	compareLimit = time.Minute
	ais = threeAIs()
	ais[2].judge = block
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	err = compareWith(ctx, adaptersOf(ais), testGate(t), s, &bytes.Buffer{}, false)
	if err == nil || !strings.Contains(err.Error(), "cancelled") || !strings.Contains(err.Error(), "취소했습니다") {
		t.Errorf("err = %v", err)
	}
}

// ---- flags and files ------------------------------------------------------

// compareFiles writes the files of a compare call and returns their paths.
func compareFiles(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	paths := map[string]string{}
	for name, text := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		paths[name] = p
	}
	return paths
}

func notElevated(t *testing.T) {
	old := selfElevated
	t.Cleanup(func() { selfElevated = old })
	selfElevated = func() bool { return false }
}

// Each wrong call is refused, in both languages, before any AI program
// is looked for.
func TestCompareRefuses(t *testing.T) {
	notElevated(t)
	p := compareFiles(t, map[string]string{
		"q.txt": "질문입니다?", "a.txt": "답 하나입니다.", "b.txt": "답 둘입니다.",
		"big-answer.txt": strings.Repeat("가", 65<<10/3+1), "big-q.txt": strings.Repeat("q", 33<<10),
		"e.txt": "앞 질문", "big-e.txt": strings.Repeat("e", 4097), "blank.txt": " \n\t",
		"latin1.txt": "caf\xe9", "huge.txt": strings.Repeat("x", compareFileMax+1),
	})
	ok := []string{"-q", p["q.txt"], "-answer", "claude=" + p["a.txt"], "-answer", "codex=" + p["b.txt"]}
	with := func(extra ...string) []string { return append(slices.Clone(ok), extra...) }
	for _, c := range []struct {
		name string
		args []string
		en   string
		ko   string
	}{
		{"no -q", []string{"-answer", "claude=" + p["a.txt"], "-answer", "codex=" + p["b.txt"]}, "usage: asksemble-agent compare", "사용법"},
		{"stray argument", with("extra"), "usage: asksemble-agent compare", "사용법"},
		{"one answer", []string{"-q", p["q.txt"], "-answer", "claude=" + p["a.txt"]}, "give 2 to 6 answers", "2~6개"},
		{"seven answers", with("-answer", "chatgpt="+p["a.txt"], "-answer", "gemini="+p["a.txt"], "-answer", "opencode="+p["a.txt"],
			"-answer", "x="+p["a.txt"], "-answer", "y="+p["a.txt"]), "give 2 to 6 answers", "2~6개"},
		{"unknown AI", []string{"-q", p["q.txt"], "-answer", "claude=" + p["a.txt"], "-answer", "copilot=" + p["b.txt"]}, `unknown AI "copilot"`, "모르는 AI"},
		{"upper case app", []string{"-q", p["q.txt"], "-answer", "claude=" + p["a.txt"], "-answer", "ChatGPT=" + p["b.txt"]}, `unknown AI "ChatGPT"`, "모르는 AI"},
		{"no file", []string{"-q", p["q.txt"], "-answer", "claude=" + p["a.txt"], "-answer", "codex"}, "is not AI=FILE", "AI=파일 꼴이"},
		{"same AI twice", []string{"-q", p["q.txt"], "-answer", "claude=" + p["a.txt"], "-answer", "claude=" + p["b.txt"]}, "names claude twice", "두 번"},
		{"bad model", with("-model", "claude=bad model"), "is not AI=MODEL", "AI=모델 꼴이"},
		{"model without answer", with("-model", "opencode=x"), "has no -answer", "-answer가 없습니다"},
		{"model twice", with("-model", "claude=a", "-model", "claude=b"), "-model names claude twice", "두 번"},
		{"four earlier", with("-earlier", p["e.txt"], "-earlier", p["e.txt"], "-earlier", p["e.txt"], "-earlier", p["e.txt"]), "at most 3 -earlier", "3개까지"},
		{"unknown judge", with("-judges", "claude,gemini"), "-judges must name", "-judges에는"},
		{"judge twice", with("-judges", "codex,codex"), "-judges must name", "-judges에는"},
		{"two stdin", []string{"-q", "-", "-answer", "claude=-", "-answer", "codex=" + p["b.txt"]}, "only one file can be -", "표준 입력"},
		{"65 KiB answer", []string{"-q", p["q.txt"], "-answer", "claude=" + p["a.txt"], "-answer", "codex=" + p["big-answer.txt"]}, "is over 64 KiB", "64KiB를 넘습니다"},
		{"huge answer", []string{"-q", p["q.txt"], "-answer", "claude=" + p["a.txt"], "-answer", "codex=" + p["huge.txt"]}, "is over 64 KiB", "64KiB를 넘습니다"},
		{"33 KiB question", []string{"-q", p["big-q.txt"], "-answer", "claude=" + p["a.txt"], "-answer", "codex=" + p["b.txt"]}, "is over 32 KiB", "32KiB를 넘습니다"},
		{"blank question", []string{"-q", p["blank.txt"], "-answer", "claude=" + p["a.txt"], "-answer", "codex=" + p["b.txt"]}, "question in", "비어 있습니다"},
		{"blank answer", []string{"-q", p["q.txt"], "-answer", "claude=" + p["a.txt"], "-answer", "codex=" + p["blank.txt"]}, "answer in", "비어 있습니다"},
		{"4097-byte earlier", with("-earlier", p["big-e.txt"]), "is over 4 KiB", "4KiB를 넘습니다"},
		{"blank earlier", with("-earlier", p["blank.txt"]), "earlier question in", "비어 있습니다"},
		// This PC's sandbox writes a file with NUL as empty: NUL comes on
		// standard input instead.
		{"NUL", []string{"-q", p["q.txt"], "-answer", "claude=" + p["a.txt"], "-answer", "codex=-"}, "- is not UTF-8 text", "UTF-8 글이 아닙니다"},
		{"not UTF-8", []string{"-q", p["latin1.txt"], "-answer", "claude=" + p["a.txt"], "-answer", "codex=" + p["b.txt"]}, "is not UTF-8 text", "UTF-8 글이 아닙니다"},
		{"missing file", []string{"-q", p["q.txt"] + ".gone", "-answer", "claude=" + p["a.txt"], "-answer", "codex=" + p["b.txt"]}, "cannot read", "읽을 수 없습니다"},
	} {
		var out bytes.Buffer
		err := compareCmd(t.TempDir(), c.args, strings.NewReader("a\x00b"), &out)
		if err == nil || !strings.Contains(err.Error(), c.en) || !strings.Contains(err.Error(), c.ko) {
			t.Errorf("%s: err = %v, want %q and %q", c.name, err, c.en, c.ko)
		}
		if out.Len() != 0 {
			t.Errorf("%s: printed %q", c.name, out.String())
		}
	}
}

// An administrator terminal is refused before any file is read.
func TestCompareRefusesAdministrator(t *testing.T) {
	old := selfElevated
	t.Cleanup(func() { selfElevated = old })
	selfElevated = func() bool { return true }
	p := compareFiles(t, map[string]string{"a.txt": "답"})
	err := compareCmd(t.TempDir(), []string{"-q", p["a.txt"] + ".gone", "-answer", "claude=" + p["a.txt"], "-answer", "codex=" + p["a.txt"]}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || err.Error() != errElevated().Error() {
		t.Errorf("err = %v", err)
	}
}

// Files are read as UTF-8 without a byte order mark; the question and the
// earlier ones lose the white space around them, the answers keep theirs;
// one file may be standard input; apps' answers are handoff ones.
func TestReadCompareFiles(t *testing.T) {
	p := compareFiles(t, map[string]string{
		"q.txt": "\uFEFF  질문입니다?\r\n", "a.txt": "\uFEFF답 하나입니다.\n", "e1.txt": " 첫 질문 \n", "e2.txt": "둘째 질문",
		"exact-q.txt": strings.Repeat("q", 32<<10) + "\n\n", "exact-a.txt": strings.Repeat("a", 64<<10), "exact-e.txt": strings.Repeat("e", 4096),
	})
	f, err := parseCompareFlags([]string{"-q", p["q.txt"], "-answer", "claude=" + p["a.txt"], "-answer", "chatgpt=-",
		"-model", "chatgpt=gpt-6", "-earlier", p["e1.txt"], "-earlier", p["e2.txt"], "-judges", "opencode, codex", "-json"})
	if err != nil {
		t.Fatal(err)
	}
	ci, err := readCompareFiles(f, strings.NewReader("붙여 넣은 답입니다."))
	if err != nil {
		t.Fatal(err)
	}
	want := []compares.DirectAnswer{
		{ID: "claude", Agent: "claude", Via: judge.ViaPC, Text: "답 하나입니다.\n"},
		{ID: "chatgpt", Agent: "chatgpt", Via: judge.ViaHandoff, Model: "gpt-6", Text: "붙여 넣은 답입니다."},
	}
	if ci.question != "질문입니다?" || !slices.Equal(ci.earlier, []string{"첫 질문", "둘째 질문"}) || !slices.Equal(ci.answers, want) ||
		!slices.Equal(ci.judges, []string{"opencode", "codex"}) || !ci.asJSON {
		t.Errorf("input = %+v", ci)
	}

	// The limits themselves pass.
	f, err = parseCompareFlags([]string{"-q", p["exact-q.txt"], "-answer", "claude=" + p["exact-a.txt"], "-answer", "codex=" + p["a.txt"], "-earlier", p["exact-e.txt"]})
	if err != nil {
		t.Fatal(err)
	}
	if ci, err := readCompareFiles(f, strings.NewReader("")); err != nil || len(ci.question) != 32<<10 || len(ci.answers[0].Text) != 64<<10 {
		t.Errorf("at the limits: %v", err)
	}
}

// A PC answer without -model is taken as its AI's standard tier model,
// and the note says so; -model and handoff answers are left as they are.
func TestAnswerModelsAssumeTheStandardTier(t *testing.T) {
	ais := threeAIs()
	ais[1].tiers = map[string]core.AskModel{core.TierQuick: {Model: "gpt-6-luna"}, core.TierStandard: {Model: "gpt-6.1-terra", Effort: "high"}}
	in := []compares.DirectAnswer{
		{ID: "claude", Agent: "claude", Via: judge.ViaPC, Model: "opus"},
		{ID: "codex", Agent: "codex", Via: judge.ViaPC},
		{ID: "gemini", Agent: "gemini", Via: judge.ViaHandoff},
	}
	got, notes, err := answerModels(adaptersOf(ais), in)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Model != "opus" || got[1].Model != "gpt-6.1-terra" || got[2].Model != "" || in[1].Model != "" {
		t.Errorf("answers = %+v (input %+v)", got, in)
	}
	if len(notes) != 1 || !strings.Contains(notes[0].en, "model assumed: codex gpt-6.1-terra, its standard tier") ||
		!strings.Contains(notes[0].en, "-model codex=MODEL") || !strings.Contains(notes[0].ko, "codex gpt-6.1-terra(표준 단계)") {
		t.Errorf("notes = %+v", notes)
	}

	// An AI with no standard tier model: an error, not a guess.
	ais[1].tiers = map[string]core.AskModel{}
	if _, _, err := answerModels(adaptersOf(ais), in); err == nil || !strings.Contains(err.Error(), "-model codex=MODEL") || !strings.Contains(err.Error(), "-model codex=모델") {
		t.Errorf("err = %v", err)
	}
}

func TestTerminalText(t *testing.T) {
	if got := terminalText("a\x1b[31mb\nc\td\u0085e\u2028f 가"); got != "a [31mb c d e f 가" {
		t.Errorf("terminalText = %q", got)
	}
}

// The request line of each kind of request.
func TestRequestLine(t *testing.T) {
	for _, c := range []struct {
		r    judge.Request
		want string
	}{
		{judge.Request{ID: "r1", From: "claude", Against: "codex", Judge: "opencode", JudgeOnly: true, Status: judge.RequestDone, Ms: 9840, Model: "opencode-go/deepseek-v4.1-flash", Attempt: 1},
			"r1 opencode (judge only / 판정만): claude → codex · done · 9.8 s · opencode-go/deepseek-v4.1-flash"},
		{judge.Request{ID: "r2", From: "codex", Against: "claude", Judge: "opencode", Status: judge.RequestFailed, Attempt: 2, Ms: 1200,
			Error: &judge.Error{Code: core.ErrCodeJudgeBadOutput, Message: "the judge did not give its JSON"}},
			"r2 opencode: codex → claude · failed: judge_bad_output the judge did not give its JSON · 1.2 s · attempt 2 / 2번째"},
		{judge.Request{ID: "r3", From: "claude", Against: "gemini", Status: judge.RequestFailed, Error: &judge.Error{Code: core.ErrCodeNoJudge}},
			"r3 (no judge / 판정할 AI 없음): claude → gemini · failed: no_judge"},
		{judge.Request{ID: "r4", From: "claude", Against: "codex", Judge: "codex", Status: judge.RequestQueued, Model: "gpt-6-luna"},
			"r4 codex: claude → codex · queued · gpt-6-luna"},
	} {
		if got := requestLine(c.r); got != c.want {
			t.Errorf("requestLine = %q\nwant          %q", got, c.want)
		}
	}
}

// An item shows its sides, numbers, the checks that put it on the card
// and the answers it was not compared with.
func TestPrintItem(t *testing.T) {
	units := map[string]judge.Unit{"claude:2": {ID: "claude:2", Text: "높이는 5입니다."}}
	it := judge.Item{Kind: judge.CardCheck, Positions: []judge.Position{{Agents: []string{"claude"}, Units: []string{"claude:2", "claude:3"}}},
		NumbersDiffer: true, Check: []string{judge.CheckUnsure, judge.CheckCrossDirection, "newCheck"}, Missing: []string{"codex", "gemini"}}
	var out bytes.Buffer
	printItem(&out, it, units)
	want := "  - [claude] 높이는 5입니다. (+1 more / 1개 더)\n" +
		"    numbers differ / 숫자가 다릅니다\n" +
		"    why: the judge was not sure; the two directions disagree; newCheck / 까닭: 확신 못 함; 두 방향이 안 맞음; newCheck\n" +
		"    not compared with codex, gemini / codex, gemini 답과는 비교하지 못했습니다\n"
	if out.String() != want {
		t.Errorf("printItem =\n%s\nwant\n%s", out.String(), want)
	}
}

// ---- combined answers (-synth, S5b) -------------------------------------------

// fakeWriterAI is a fakeJudgeAI that also writes combined answers: by
// default fakecli's ok text, which names every DIFFER side.
type fakeWriterAI struct {
	*fakeJudgeAI
	write func(ctx context.Context, req core.SynthRequest) (core.SynthResult, error)
	smu   sync.Mutex
	reqs  []core.SynthRequest
}

func (f *fakeWriterAI) DisplayName() string { return strings.ToUpper(f.name[:1]) + f.name[1:] }

func (f *fakeWriterAI) Synthesize(ctx context.Context, req core.SynthRequest) (core.SynthResult, error) {
	f.smu.Lock()
	f.reqs = append(f.reqs, req)
	f.smu.Unlock()
	if f.write != nil {
		return f.write(ctx, req)
	}
	b, _ := json.Marshal(map[string]string{"answer": fakecli.SynthText(req.Prompt)})
	return core.SynthResult{Output: b, Model: req.Model}, nil
}

func (f *fakeWriterAI) synthCalls() []core.SynthRequest {
	f.smu.Lock()
	defer f.smu.Unlock()
	return slices.Clone(f.reqs)
}

func threeWriterAIs() []*fakeWriterAI {
	var out []*fakeWriterAI
	for _, a := range threeAIs() {
		a.tiers[core.TierDeep] = core.AskModel{Model: a.name + "-deep", Effort: "high"}
		out = append(out, &fakeWriterAI{fakeJudgeAI: a})
	}
	return out
}

func writersOf(ais []*fakeWriterAI) []core.Adapter {
	out := make([]core.Adapter, len(ais))
	for i, a := range ais {
		out[i] = a
	}
	return out
}

func TestCompareWithSynthesis(t *testing.T) {
	ais := threeWriterAIs()
	ais[1].write = func(_ context.Context, req core.SynthRequest) (core.SynthResult, error) {
		text := "# 가격\n\r\nClaude는 NUM 1, Codex는 NUM 2라고 합니다.\x1b[2J\r끝\u2028."
		b, _ := json.Marshal(map[string]string{"answer": text})
		return core.SynthResult{Output: b, Model: req.Model}, nil
	}
	var out bytes.Buffer
	s := spec(pcAnswer("claude", "sonnet", "가격은 NUM 1입니다."), pcAnswer("codex", "gpt-6.1-terra", "가격은 NUM 2입니다."))
	s.Synth = &compares.SynthSpec{Agent: "codex", Tier: core.TierDeep}
	if err := compareWith(context.Background(), writersOf(ais), testGate(t), s, &out, false); err != nil {
		t.Fatalf("compareWith: %v\n%s", err, out.String())
	}
	got := out.String()
	head := "\n== Combined answer / 종합 답변 (Codex codex-deep"
	i := strings.Index(got, head)
	if i < 0 || i < strings.Index(got, "differ 1 · check this 0") {
		t.Fatalf("no combined answer after the cards:\n%s", got)
	}
	want := ")\n# 가격\n\nClaude는 NUM 1, Codex는 NUM 2라고 합니다. [2J\n끝 .\n"
	if !strings.Contains(got[i:], want) {
		t.Errorf("combined answer section:\n%q\nwant %q", got[i:], want)
	}
	if strings.Contains(got, "\x1b") || strings.Contains(got, "\r") || strings.Contains(got, "\u2028") {
		t.Errorf("a control character reached the terminal: %q", got)
	}
	if strings.Contains(got, "; not compared / 비교 결과 없이") || strings.Contains(got, "빠졌을 수") {
		t.Errorf("notes that do not belong:\n%s", got)
	}
	if calls := ais[1].synthCalls(); len(calls) != 1 || calls[0].Model != "codex-deep" || calls[0].Effort != "high" {
		t.Errorf("synth calls = %+v", calls)
	}
	// Without -synth: no section and no call.
	out.Reset()
	s.Synth = nil
	if err := compareWith(context.Background(), writersOf(ais), testGate(t), s, &out, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Combined answer") || len(ais[1].synthCalls()) != 1 {
		t.Errorf("off:\n%s", out.String())
	}
}

// Notes: written without the comparison's results, and names missing.
func TestCompareWithSynthesisNotes(t *testing.T) {
	ais := threeWriterAIs()
	ais[2].judge = func(context.Context, core.JudgeRequest) (core.JudgeResult, error) {
		return core.JudgeResult{}, core.Fail(core.ErrCodeJudgeFailed, "limit")
	}
	var out bytes.Buffer
	s := spec(pcAnswer("claude", "sonnet", "가격은 NUM 1입니다."), pcAnswer("codex", "gpt-6.1-terra", "가격은 NUM 2입니다."))
	s.Synth = &compares.SynthSpec{Agent: "claude"}
	err := compareWith(context.Background(), writersOf(ais), testGate(t), s, &out, false)
	if err == nil || !strings.Contains(err.Error(), "the comparison failed (judge_failed)") {
		t.Fatalf("err = %v", err)
	}
	if got := out.String(); !strings.Contains(got, "== Combined answer / 종합 답변 (Claude Code claude-standard") ||
		!strings.Contains(got, "; not compared / 비교 결과 없이)\n"+fakecli.SynthLead+"\n") {
		t.Errorf("uncompared:\n%s", out.String())
	}

	ais = threeWriterAIs()
	ais[0].write = func(_ context.Context, req core.SynthRequest) (core.SynthResult, error) {
		return core.SynthResult{Output: []byte(`{"answer":"The prices differ."}`), Model: req.Model}, nil
	}
	out.Reset()
	if err := compareWith(context.Background(), writersOf(ais), testGate(t), s, &out, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"(some places where the AIs differ may be missing from the combined answer: check the Differ card above)",
		"(갈린 곳 일부가 종합 답변에 빠졌을 수 있습니다. 위의 다른 말 카드에서 확인하세요)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

// A combined answer that failed prints its code and message and is an
// error; -json carries the synthesis.
func TestCompareWithSynthesisFailedAndJSON(t *testing.T) {
	ais := threeWriterAIs()
	ais[0].write = func(_ context.Context, req core.SynthRequest) (core.SynthResult, error) {
		return core.SynthResult{Model: req.Model}, core.Fail(core.ErrCodeJudgeFailed, fakecli.UsageLimitClaude+"\x1b[31m")
	}
	var out bytes.Buffer
	s := spec(pcAnswer("claude", "sonnet", "가격은 NUM 1입니다."), pcAnswer("codex", "gpt-6.1-terra", "가격은 NUM 2입니다."))
	s.Synth = &compares.SynthSpec{Agent: "claude"}
	err := compareWith(context.Background(), writersOf(ais), testGate(t), s, &out, false)
	if err == nil || !strings.Contains(err.Error(), "the combined answer failed (judge_failed): "+fakecli.UsageLimitClaude) ||
		!strings.Contains(err.Error(), "종합 답변을 쓰지 못했습니다 (judge_failed)") || strings.Contains(err.Error(), "\x1b") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out.String(), "  failed / 실패: judge_failed "+fakecli.UsageLimitClaude) {
		t.Errorf("output:\n%s", out.String())
	}

	ais = threeWriterAIs()
	out.Reset()
	if err := compareWith(context.Background(), writersOf(ais), testGate(t), s, &out, true); err != nil {
		t.Fatal(err)
	}
	var c compares.Comparison
	if err := json.Unmarshal(out.Bytes(), &c); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if c.Synthesis == nil || c.Synthesis.Status != compares.SynthDone || c.Synthesis.Agent != "claude" || !strings.HasPrefix(c.Synthesis.Text, fakecli.SynthLead) {
		t.Errorf("synthesis = %+v", c.Synthesis)
	}
	if strings.Contains(out.String(), "Combined answer") {
		t.Error("the JSON has the text section")
	}
}

// The compare limit covers the combined answer too.
func TestCompareWithSynthesisLimit(t *testing.T) {
	old := compareLimit
	t.Cleanup(func() { compareLimit = old })
	compareLimit = 200 * time.Millisecond
	ais := threeWriterAIs()
	ais[0].write = func(ctx context.Context, req core.SynthRequest) (core.SynthResult, error) {
		<-ctx.Done()
		return core.SynthResult{}, core.Fail(core.ErrCodeJudgeTimeout, "")
	}
	s := spec(pcAnswer("claude", "sonnet", "가격은 NUM 1입니다."), pcAnswer("codex", "gpt-6.1-terra", "가격은 NUM 2입니다."))
	s.Synth = &compares.SynthSpec{Agent: "claude"}
	err := compareWith(context.Background(), writersOf(ais), testGate(t), s, &bytes.Buffer{}, false)
	if err == nil || !strings.Contains(err.Error(), "did not end within") {
		t.Errorf("err = %v", err)
	}
}

func TestCompareSynthFlags(t *testing.T) {
	base := []string{"-q", "q.txt", "-answer", "claude=a.txt", "-answer", "codex=b.txt"}
	f, err := parseCompareFlags(base)
	if err != nil || f.synth != nil {
		t.Fatalf("no -synth: %+v %v", f.synth, err)
	}
	f, err = parseCompareFlags(append(slices.Clone(base), "-synth"))
	if err != nil || f.synth == nil || *f.synth != (compares.SynthSpec{Agent: "claude", Tier: core.TierStandard}) {
		t.Fatalf("-synth: %+v %v", f.synth, err)
	}
	f, err = parseCompareFlags(append(slices.Clone(base), "-synth", "-writer", "opencode", "-tier", "deep"))
	if err != nil || f.synth == nil || *f.synth != (compares.SynthSpec{Agent: "opencode", Tier: core.TierDeep}) {
		t.Fatalf("-writer -tier: %+v %v", f.synth, err)
	}
	for name, args := range map[string][]string{
		"writer without synth": {"-writer", "codex"},
		"tier without synth":   {"-tier", "quick"},
		"unknown writer":       {"-synth", "-writer", "gemini"},
		"bad tier":             {"-synth", "-tier", "fast"},
	} {
		if _, err := parseCompareFlags(append(slices.Clone(base), args...)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	for _, want := range []string{"[-synth [-writer AI] [-tier quick|standard|deep]]"} {
		if !strings.Contains(compareUsageEn, want) || !strings.Contains(compareUsageKo, want) {
			t.Errorf("usage lacks %s", want)
		}
	}
}

func TestTerminalLines(t *testing.T) {
	if got := terminalLines("a\r\nb\rc\n\td\x1b[0m\u2029e\u0085"); got != "a\nb\nc\n d [0m e " {
		t.Errorf("terminalLines = %q", got)
	}
}
