package compares

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/judge"
	"github.com/heeo-dev/askorus/agent/internal/runs"
)

// Question refining (질문 정제; milestone C, design C-1..C-6 in "마일스톤 C"
// of docs/phase2-plan.md). Before a run's AIs answer, the moderator (the
// AI that writes the combined answer, picked by the same rule and only
// among the AIs asked) fills in a question spec with the writer's locked
// command (core.SynthRequest.Refine) and the quick tier's model. The run
// then asks every AI the question with the spec's answer template under
// it (judge.RefinedQuestion), and its combined answer follows the spec.
// Refining waits for no process slot (it is one short call before the
// run's asks), takes at most refineLimit, and when it fails the
// question goes as written: it never stops a question.

// Refine states.
const (
	RefineRunning = "running"
	RefineDone    = "done"
	RefineFailed  = "failed"
	RefineOff     = "off"
)

// refineLimit bounds refining; a variable so a test can shorten it.
var refineLimit = core.RefineTimeout

// RefineSpec is one question to refine.
type RefineSpec struct {
	Question string   // as the person wrote it
	Earlier  []string // earlier questions of a follow-up, oldest first
	Asked    []string // the run's AIs in turn order: the moderator is one of them
	Agent    string   // the moderator the person picked; "" takes the first that may
}

// Refined is what refining gave: the run's snapshot field (runs.Refine).
type Refined = runs.Refine

// Refine asks the moderator for the question's spec and returns it with
// its answer template, or a failure with its code (no_judge when no asked
// AI may moderate, judge_timeout, judge_bad_output after the second try,
// bad_model, judge_failed…). It asks again once, with a new nonce, when
// the reply is not a spec.
func (m *Manager) Refine(ctx context.Context, s RefineSpec) (out Refined) {
	start := time.Now()
	out = Refined{Status: RefineFailed}
	defer func() {
		if v := recover(); v != nil {
			out = Refined{Status: RefineFailed, Agent: out.Agent, Model: out.Model, Error: synthFailure(core.ErrCodeInternal, "")}
		}
		out.Ms = time.Since(start).Milliseconds()
		m.log.Info("refine ended", "moderator", out.Agent, "status", out.Status, "code", errCode(out.Error), "ms", out.Ms)
	}()
	w, ok := pickWriter(m.Writers(ctx, core.TierQuick), synthInput{spec: SynthSpec{Agent: s.Agent}, allowed: s.Asked})
	if !ok {
		out.Error = synthFailure(core.ErrCodeNoJudge, "")
		return out
	}
	out.Agent, out.Model = w.Agent, w.Model
	if err := makeFolder(m.o.Gate.Folder); err != nil {
		out.Error = synthFailure(core.ErrCodeInternal, "")
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, refineLimit)
	defer cancel()
	task := judge.RefineTask{Question: s.Question, Earlier: s.Earlier}
	var refs []string
	defer func() {
		if len(refs) > 0 && w.Cleaner != nil {
			m.refineCleanup(w, refs)
		}
	}()
	for attempt := 1; attempt <= 2; attempt++ {
		prompt := judge.RefinePrompt(task, judge.RefineNonce(task))
		if prompt == "" {
			out.Error = synthFailure(core.ErrCodeInternal, "")
			return out
		}
		res, err := w.Synth.Synthesize(ctx, core.SynthRequest{Folder: m.o.Gate.Folder, Prompt: prompt, Model: w.Model, Refine: true})
		if res.Cleanup != "" {
			refs = append(refs, res.Cleanup)
		}
		if res.Model != "" {
			out.Model = res.Model
		}
		retry := attempt == 1 && ctx.Err() == nil
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				err = core.Fail(core.ErrCodeJudgeTimeout, "")
			}
			out.Error = synthCallFailure(err)
			if out.Error.Code == core.ErrCodeJudgeBadOutput && retry {
				continue
			}
			return out
		}
		// As for a writer: a model of another company is not this AI.
		if family := w.Judger.Family(res.Model); family == "" || !strings.EqualFold(family, w.Family) {
			out.Error = synthFailure(core.ErrCodeBadModel, msgSynthCompany)
			return out
		}
		spec, verr := judge.ValidateRefine(res.Output)
		if verr != nil {
			out.Error = synthFailure(core.ErrCodeJudgeBadOutput, "")
			if retry {
				continue
			}
			return out
		}
		return Refined{Status: RefineDone, Agent: w.Agent, Model: out.Model, Spec: &spec, Template: judge.AnswerTemplate(spec)}
	}
	return out
}

// refineCleanup removes what the moderator left (OpenCode's conversation),
// in the background as a writer's cleanup, even when the Manager closes.
func (m *Manager) refineCleanup(w Writer, refs []string) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() { _ = recover() }()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(m.base), core.JudgeCleanupTimeout*time.Duration(2+len(refs)))
		defer cancel()
		if err := w.Cleaner.CleanupJudge(ctx, m.o.Gate.Folder, refs); err != nil {
			m.log.Warn("refine cleanup failed", "moderator", w.Agent, "code", core.ErrorCode(err))
		}
	}()
}

func errCode(e *judge.Error) string {
	if e == nil {
		return ""
	}
	return e.Code
}

// RunRefiner refines one run's question once for all of the run's AIs:
// the first Question call starts refining, every call waits for its end
// and gets the question to ask. report gets the refining when it starts
// (running) and when it ends, for the run's snapshot.
type RunRefiner struct {
	m      *Manager
	spec   RefineSpec
	report func(Refined)
	once   sync.Once
	done   chan struct{}
	res    Refined
}

// NewRunRefiner makes the refiner of one run.
func (m *Manager) NewRunRefiner(s RefineSpec, report func(Refined)) *RunRefiner {
	return &RunRefiner{m: m, spec: s, report: report, done: make(chan struct{})}
}

// Question is the question to ask: the person's words with the spec's
// answer template, or as written when refining failed. Refining runs on
// its own (a stopped part does not stop it for the others); ctx ends only
// this wait.
func (r *RunRefiner) Question(ctx context.Context) (string, error) {
	r.once.Do(func() {
		r.report(Refined{Status: RefineRunning})
		r.m.wg.Add(1)
		go func() {
			defer r.m.wg.Done()
			r.res = r.m.Refine(r.m.base, r.spec)
			r.report(r.res)
			close(r.done)
		}()
	})
	select {
	case <-r.done:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if r.res.Status == RefineDone && r.res.Spec != nil {
		return judge.RefinedQuestion(r.spec.Question, *r.res.Spec), nil
	}
	return r.spec.Question, nil
}
