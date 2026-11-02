//go:build judgeeval

package main

import (
	"context"
	"encoding/json"
	"fmt"
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

// The parity table (deviation 2): each case's script runs through the
// product's compares.Manager and through attemptLoop, and both must end
// the same way, with the same attempts and judgments. Only a usage limit
// differs on purpose: the product fails the request with judge_failed,
// judge-eval leaves it unrecorded and stops.

// Steps of a scripted judge call.
const (
	stepGood        = "good"  // a1 same (b1), a2 opposite (b2)
	stepGood2       = "good2" // a1 absent, a2 same (b1)
	stepMissing     = "missing"
	stepProse       = "prose"
	stepBadOutput   = "bad_output" // the adapter's judge_bad_output
	stepWrongFamily = "wrong_family"
	stepUsage       = "usage"
	stepFailed      = "failed"
	stepTool        = "tool"
)

// scripted judges direction fa>fb by its steps, one per call; every other
// call is good.
type scripted struct {
	steps []string

	mu      sync.Mutex
	n       int
	prompts []string
}

const scriptModel = "fj-m"

func (s *scripted) JudgeDefault() core.AskModel { return core.AskModel{Model: scriptModel} }

func (s *scripted) Family(model string) string {
	switch model {
	case scriptModel:
		return "fam-j"
	case "fa-m":
		return "fam-a"
	case "fb-m":
		return "fam-b"
	}
	return ""
}

func (s *scripted) Judge(ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
	step := stepGood
	if len(req.Task.Units) > 0 && strings.Contains(req.Task.Units[0].Text, "Alpha") {
		s.mu.Lock()
		if s.n < len(s.steps) {
			step = s.steps[s.n]
		}
		s.n++
		s.prompts = append(s.prompts, req.Prompt)
		s.mu.Unlock()
	}
	res := core.JudgeResult{Model: scriptModel, InputTokens: 7, OutputTokens: 3}
	type v struct {
		ID       string   `json:"id"`
		Core     bool     `json:"core"`
		Evidence []string `json:"evidence"`
		Reason   string   `json:"reason"`
		Label    string   `json:"label"`
		Unsure   bool     `json:"unsure"`
	}
	answer := func(vs ...v) core.JudgeResult {
		b, _ := json.Marshal(map[string][]v{"judgments": vs})
		res.Output = b
		return res
	}
	a1 := v{ID: "a1", Core: true, Evidence: []string{"b1"}, Reason: "r1 " + step, Label: judge.LabelSame}
	a2 := v{ID: "a2", Core: true, Evidence: []string{"b2"}, Reason: "r2 " + step, Label: judge.LabelOpposite, Unsure: true}
	switch step {
	case stepGood2:
		return answer(v{ID: "a1", Evidence: []string{}, Reason: "x", Label: judge.LabelAbsent}, v{ID: "a2", Core: true, Evidence: []string{"b1"}, Reason: "y", Label: judge.LabelSame}), nil
	case stepMissing:
		return answer(a2), nil
	case stepProse:
		res.Output = json.RawMessage(fakecli.ProseText)
		return res, nil
	case stepBadOutput:
		return res, core.Fail(core.ErrCodeJudgeBadOutput, "")
	case stepWrongFamily:
		r := answer(a1, a2)
		r.Model = "zz-1"
		return r, nil
	case stepUsage:
		return res, core.Fail(core.ErrCodeJudgeFailed, fakecli.UsageLimitCodex)
	case stepFailed:
		return res, core.Fail(core.ErrCodeJudgeFailed, fakecli.OverloadedStderr)
	case stepTool:
		return res, core.Fail(core.ErrCodeJudgeUsedTool, "")
	}
	return answer(a1, a2), nil
}

// fakeAdapter is an AI of the parity test's Manager.
type fakeAdapter struct {
	name string
	*scripted
}

func (a fakeAdapter) Agent() string { return a.name }
func (a fakeAdapter) Info(context.Context) core.AdapterInfo {
	return core.AdapterInfo{Agent: a.name, Version: "1.0.0", Available: true, Capabilities: []core.Capability{}}
}
func (a fakeAdapter) ListSessions(context.Context) (core.ListResult, error) {
	return core.ListResult{}, nil
}

const (
	parityQuestion = "parity?"
	parityA        = "Alpha one is right. Alpha two is right."
	parityB        = "Beta one is right. Beta two is right."
)

// productRun runs steps through compares.Manager and returns request
// fa>fb and its judgments.
func productRun(t *testing.T, steps []string) (judge.Request, []judge.Judgment, *scripted) {
	t.Helper()
	s := &scripted{steps: steps}
	other := &scripted{}
	m := compares.New(compares.Options{
		Adapters: []core.Adapter{fakeAdapter{"fa", other}, fakeAdapter{"fb", other}, fakeAdapter{"fj", s}},
		Gate:     compares.Gate{MinVersions: map[string]string{"fa": "1.0.0", "fb": "1.0.0", "fj": "1.0.0"}, Folder: t.TempDir()},
	})
	defer m.Close()
	c, err := m.Start(compares.DirectSpec{Owner: "o", Question: parityQuestion, Judges: []string{"fj"}, Answers: []compares.DirectAnswer{
		{ID: "fa", Agent: "fa", Via: judge.ViaPC, Model: "fa-m", Text: parityA},
		{ID: "fb", Agent: "fb", Via: judge.ViaPC, Model: "fb-m", Text: parityB},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snap, _ := m.WaitIdle(ctx, "o", c.ID)
	for _, r := range snap.Requests {
		if r.From == "fa" {
			var js []judge.Judgment
			for _, j := range snap.Judgments {
				if j.Request == r.ID {
					js = append(js, j)
				}
			}
			return r, js, s
		}
	}
	t.Fatalf("no request fa>fb in %+v", snap.Requests)
	return judge.Request{}, nil, nil
}

// evalRun runs steps through attemptLoop.
func evalRun(t *testing.T, steps []string, prompt func(core.JudgeTask, string) string) (outcome, core.JudgeTask, *scripted) {
	t.Helper()
	spec := judge.Spec{Question: parityQuestion, Answers: []judge.Answer{
		splitAnswer("fa", "fa-m", parityA, judge.MaxUnits), splitAnswer("fb", "fb-m", parityB, judge.MaxUnits),
	}}
	task := judge.BuildTask(spec, judge.Request{From: "fa", Against: "fb"})
	s := &scripted{steps: steps}
	l := attemptLoop{judger: s, family: "fam-j", folder: t.TempDir(), model: scriptModel, prompt: prompt}
	return l.run(context.Background(), task), task, s
}

// brief is a judgment without what Aggregate adds.
func brief(js []judge.Judgment) []string {
	var out []string
	for _, j := range js {
		out = append(out, fmt.Sprintf("%s %s core=%v unsure=%v ev=%v", j.Unit, j.Label, j.Core, j.Unsure, j.Evidence))
	}
	slices.Sort(out)
	return out
}

var nonceRe = regexp.MustCompile(`<data-([0-9a-f]{16})>`)

func TestAttemptParity(t *testing.T) {
	for _, c := range []struct {
		name     string
		steps    []string
		done     bool
		code     string // when not done
		attempts int
		want     []string // the judgments when done
	}{
		{"good", []string{stepGood}, true, "", 1,
			[]string{"fa:1 same core=true unsure=false ev=[fb:1]", "fa:2 opposite core=true unsure=true ev=[fb:2]"}},
		{"bad output, then good", []string{stepBadOutput, stepGood}, true, "", 2,
			[]string{"fa:1 same core=true unsure=false ev=[fb:1]", "fa:2 opposite core=true unsure=true ev=[fb:2]"}},
		{"missing, then filled: attempt 1 stands", []string{stepMissing, stepGood2}, true, "", 2,
			[]string{"fa:1 absent core=false unsure=false ev=[]", "fa:2 opposite core=true unsure=true ev=[fb:2]"}},
		{"missing twice: the id stays unjudged", []string{stepMissing, stepMissing}, true, "", 2,
			[]string{"fa:2 opposite core=true unsure=true ev=[fb:2]"}},
		{"missing, then bad output: attempt 1 stands", []string{stepMissing, stepProse}, true, "", 2,
			[]string{"fa:2 opposite core=true unsure=true ev=[fb:2]"}},
		{"prose, then missing", []string{stepProse, stepMissing}, true, "", 2,
			[]string{"fa:2 opposite core=true unsure=true ev=[fb:2]"}},
		{"family mismatch: bad_model, no retry", []string{stepWrongFamily}, false, core.ErrCodeBadModel, 1, nil},
		{"prose twice", []string{stepProse, stepProse}, false, core.ErrCodeJudgeBadOutput, 2, nil},
		{"adapter bad output twice", []string{stepBadOutput, stepBadOutput}, false, core.ErrCodeJudgeBadOutput, 2, nil},
		{"judge_failed that is no usage limit is final", []string{stepFailed}, false, core.ErrCodeJudgeFailed, 1, nil},
		{"a tool is final", []string{stepTool}, false, core.ErrCodeJudgeUsedTool, 1, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			pr, pjs, ps := productRun(t, c.steps)
			out, _, es := evalRun(t, c.steps, nil)
			if !out.recorded() {
				t.Fatalf("eval left it unrecorded: %s", out.abandon)
			}
			pDone := pr.Status == judge.RequestDone
			pCode := ""
			if pr.Error != nil {
				pCode = pr.Error.Code
			}
			if pDone != c.done || out.done != c.done || (!c.done && (pCode != c.code || out.code != c.code)) {
				t.Errorf("product %s %s, eval done=%v %s; want done=%v %s", pr.Status, pCode, out.done, out.code, c.done, c.code)
			}
			if pr.Attempt != c.attempts || len(out.attempts) != c.attempts || ps.n != c.attempts || es.n != c.attempts {
				t.Errorf("attempts: product %d (%d calls), eval %d (%d calls); want %d", pr.Attempt, ps.n, len(out.attempts), es.n, c.attempts)
			}
			if c.done {
				ejs := judge.Judgments(judge.Request{ID: pr.ID, From: "fa", Against: "fb", Judge: "fj"}, out.v)
				if got, want := brief(ejs), brief(pjs); !slices.Equal(got, want) || !slices.Equal(got, c.want) {
					t.Errorf("judgments:\n eval    %q\n product %q\n want    %q", got, want, c.want)
				}
				for _, v := range out.v.Verdicts {
					if !strings.HasPrefix(v.Reason, "r") && v.Reason != "x" && v.Reason != "y" {
						t.Errorf("reason %q not kept", v.Reason)
					}
				}
			}
			// A new nonce on each attempt, in both.
			for _, s := range []*scripted{ps, es} {
				seen := map[string]bool{}
				for _, p := range s.prompts {
					m := nonceRe.FindStringSubmatch(p)
					if m == nil || seen[m[1]] {
						t.Errorf("a prompt without a new nonce")
						continue
					}
					seen[m[1]] = true
				}
			}
		})
	}
}

func TestAttemptUsageLimit(t *testing.T) {
	// The product fails the request; judge-eval stops and records nothing.
	pr, _, _ := productRun(t, []string{stepUsage})
	if pr.Status != judge.RequestFailed || pr.Error == nil || pr.Error.Code != core.ErrCodeJudgeFailed {
		t.Errorf("product: %+v", pr)
	}
	for _, steps := range [][]string{{stepUsage}, {stepBadOutput, stepUsage}, {stepMissing, stepUsage}} {
		out, _, _ := evalRun(t, steps, nil)
		if out.recorded() || out.abandon != abandonUsage {
			t.Errorf("%v: recorded=%v abandon=%q", steps, out.recorded(), out.abandon)
		}
	}
	for _, msg := range []string{fakecli.UsageLimitClaude, fakecli.UsageLimitCodex, fakecli.UsageLimitOpenCode,
		"Rate limit exceeded", "429 Too Many Requests", "You have reached your weekly limit", "insufficient quota",
		"Your credit balance is too low", "5-hour limit reached", "Please try again later."} {
		if !usageLimit(core.Fail(core.ErrCodeJudgeFailed, msg)) {
			t.Errorf("%q is not read as a usage limit", msg)
		}
	}
	for _, err := range []error{core.Fail(core.ErrCodeJudgeFailed, fakecli.OverloadedStderr), core.Fail(core.ErrCodeJudgeTimeout, "usage limit"), fmt.Errorf("usage limit")} {
		if usageLimit(err) {
			t.Errorf("%v is read as a usage limit", err)
		}
	}
}

func TestAttemptEmptyPrompt(t *testing.T) {
	out, _, s := evalRun(t, nil, func(core.JudgeTask, string) string { return "" })
	if out.done || out.code != core.ErrCodeInternal || s.n != 0 || len(out.attempts) != 0 {
		t.Errorf("empty prompt: %+v, %d calls", out, s.n)
	}
}

func TestAttemptStopBetweenAttempts(t *testing.T) {
	// A stop before attempt 2 leaves the request unrecorded, so a resume
	// asks it again.
	spec := judge.Spec{Question: parityQuestion, Answers: []judge.Answer{
		splitAnswer("fa", "fa-m", parityA, judge.MaxUnits), splitAnswer("fb", "fb-m", parityB, judge.MaxUnits),
	}}
	task := judge.BuildTask(spec, judge.Request{From: "fa", Against: "fb"})
	n := 0
	l := attemptLoop{judger: &scripted{steps: []string{stepBadOutput}}, family: "fam-j", folder: t.TempDir(), model: scriptModel,
		before: func() error {
			n++
			if n == 2 {
				return errStop(abandonBudget)
			}
			return nil
		}}
	if out := l.run(context.Background(), task); out.recorded() || out.abandon != abandonBudget || len(out.attempts) != 1 {
		t.Errorf("%+v", out)
	}
}
