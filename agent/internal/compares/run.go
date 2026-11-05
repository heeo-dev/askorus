package compares

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
	"github.com/asksemble/asksemble/agent/internal/runs"
)

// A comparison's round, since the judging went (C6, #19; the code with
// C6d, #24): once its answers are in (its run ended, or Start gave them),
// it takes them and the AIs the earlier questions may go to, and ends at
// once: done, or failed with need_two_answers when fewer than two are
// ready. Its combined answer then starts (synth.go).

// Fixed messages of comparison errors.
var messages = map[string]string{
	core.ErrCodeNeedTwoAnswers: "fewer than two answers to compare",
	core.ErrCodeCancelled:      "the comparison was cancelled",
	core.ErrCodeInternal:       "the agent could not make this comparison",
}

const (
	msgOther            = "the comparison failed"
	msgQueue            = "the writer waited over 5 minutes for a free process"
	msgRunGone          = "the run of this comparison is gone"
	msgClosed           = "the agent is stopping"
	msgCancelledWaiting = "the comparison was cancelled before the answers were in"
)

// failure is an error with code and message, or the fixed message of code
// when message is "".
func failure(code, message string) *judge.Error {
	if message == "" {
		message = messages[code]
	}
	if message == "" {
		message = msgOther
	}
	return &judge.Error{Code: code, Message: message}
}

// clipChars cuts s to n characters, the last one "…" when it is cut, and
// drops bytes that are not UTF-8.
func clipChars(s string, n int) string {
	s = strings.ToValidUTF8(s, "")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// round is what a one-off call (the combined answer, a refining) waits in:
// its queue limit counts from start, and a cancel of its comparison (or
// the Manager's Close) stops the wait.
type round struct {
	start  time.Time       // real time: the queue limit and the logged ms count from it
	cancel <-chan struct{} // the comparison's cancelCh when the call began
}

// stopped reports whether the round's comparison was cancelled or the
// Manager closed.
func (rd *round) stopped(base context.Context) bool {
	select {
	case <-rd.cancel:
		return true
	case <-base.Done():
		return true
	default:
		return false
	}
}

// round takes comparison c's answers and ends it. It holds c's slot (the
// waiting slot of a run's comparison, or Start's or Retry's) until then,
// so the comparison is never idle in between.
func (m *Manager) round(c *comparison) {
	var spec judge.Spec
	var err *judge.Error
	func() {
		defer func() {
			if v := recover(); v != nil {
				err = failure(core.ErrCodeInternal, "")
			}
		}()
		spec, err = m.answers(c)
	}()
	m.mu.Lock()
	defer m.mu.Unlock()
	c.spec, c.planned = spec, err == nil || err.Code != core.ErrCodeInternal
	m.log.Info("comparison started", "comparison", c.id, "source", c.source, "ais", len(spec.Answers))
	switch {
	case err != nil:
		m.finishLocked(c, Failed, err, time.Now())
	case c.cancelled:
		m.finishLocked(c, Failed, failure(core.ErrCodeCancelled, ""), time.Now())
	default:
		m.finishLocked(c, Done, nil, time.Now())
	}
	c.active--
	m.touchLocked(c)
}

// answers reads comparison c's answers, from its ended run or from Start's
// spec, and the AIs the earlier questions may go to: the listed ones (the
// request's judges, or else the asked AIs) that may write. It runs outside
// the lock: it calls the adapters. The error is need_two_answers when
// fewer than two answers are ready, or internal when the run is gone.
func (m *Manager) answers(c *comparison) (judge.Spec, *judge.Error) {
	var run *runs.Run
	var rs *RunSpec
	var d *DirectSpec
	var engines []Engine
	m.locked(func() {
		run, rs, d, engines = c.run, c.runSpec, c.direct, c.directEngines
		c.directEngines = nil
	})
	if engines == nil {
		engines = m.Engines(m.base)
	}
	var spec judge.Spec
	var want []string
	switch {
	case d != nil:
		spec = judge.Spec{Question: d.Question, Earlier: slices.Clone(d.Earlier)}
		for _, a := range d.Answers {
			family := judge.HandoffFamily(a.Agent)
			if a.Via == judge.ViaPC {
				family = m.family(a.Agent, a.Model)
			}
			spec.Answers = append(spec.Answers, answer(a.ID, a.Agent, a.Via, a.Model, family, a.Text, true))
		}
		want = d.Judges
		if len(want) == 0 {
			for _, e := range engines {
				want = append(want, e.Agent)
			}
		}
	case rs != nil && run != nil:
		spec = judge.Spec{Question: rs.Question, Earlier: slices.Clone(rs.Earlier)}
		for _, part := range run.Parts {
			ready := part.Status == runs.Done && strings.TrimSpace(part.Answer) != ""
			spec.Answers = append(spec.Answers, answer(part.Agent, part.Agent, judge.ViaPC, part.Model, m.family(part.Agent, part.Model), part.Answer, ready))
		}
		want = rs.Judges
		if len(want) == 0 {
			want = rs.Asked
		}
	default:
		return spec, failure(core.ErrCodeInternal, msgRunGone)
	}
	for _, name := range want {
		if slices.ContainsFunc(engines, func(e Engine) bool { return e.Agent == name }) && !slices.Contains(spec.Listed, name) {
			spec.Listed = append(spec.Listed, name)
		}
	}
	if !twoReady(spec.Answers) {
		return spec, failure(core.ErrCodeNeedTwoAnswers, "")
	}
	return spec, nil
}

// twoReady reports whether two or more answers are ready.
func twoReady(answers []judge.Answer) bool {
	n := 0
	for _, a := range answers {
		if a.Status == judge.AnswerReady {
			n++
		}
	}
	return n >= 2
}

// makeFolder makes the judge folder, where the one-off calls run.
func makeFolder(folder string) error {
	if folder == "" {
		return errors.New("no judge folder")
	}
	return os.MkdirAll(folder, 0o700)
}

// family is the company of the model that wrote a PC answer, as the
// answering AI's adapter names it: "" when the adapter cannot tell, or
// when no model is known.
func (m *Manager) family(agent, model string) string {
	j, ok := m.adapter(agent).(core.Judger)
	if !ok || model == "" {
		return ""
	}
	return j.Family(model)
}

// answer is one answer of a comparison.
func answer(id, agent, via, model, family, text string, ready bool) judge.Answer {
	a := judge.Answer{ID: id, Agent: agent, Via: via, Family: family, Model: model, Status: judge.AnswerMissing, Units: []struct{}{}}
	if ready {
		a.Status, a.Text = judge.AnswerReady, text
	}
	return a
}

// acquire waits for a process slot of agent and then a slot of all AIs.
// The wait ends with judge_timeout QueueLimit after the round began, and
// with cancelled when the comparison is cancelled or the Manager closes.
// release gives both slots back.
func (m *Manager) acquire(rd *round, agent string) (release func(), failed *judge.Error) {
	cancelled := failure(core.ErrCodeCancelled, "")
	if rd.stopped(m.base) {
		return nil, cancelled
	}
	var sem chan struct{}
	m.locked(func() {
		if sem = m.perAI[agent]; sem == nil {
			sem = make(chan struct{}, m.perAICap)
			m.perAI[agent] = sem
		}
	})
	timer := time.NewTimer(time.Until(rd.start.Add(m.queueLimit)))
	defer timer.Stop()
	take := func(slots chan struct{}) *judge.Error {
		select {
		case slots <- struct{}{}: // a free slot wins over a limit that has just passed
			return nil
		default:
		}
		select {
		case slots <- struct{}{}:
			return nil
		case <-timer.C:
			return failure(core.ErrCodeJudgeTimeout, msgQueue)
		case <-rd.cancel:
			return cancelled
		case <-m.base.Done():
			return cancelled
		}
	}
	if f := take(sem); f != nil {
		return nil, f
	}
	if f := take(m.global); f != nil {
		<-sem
		return nil, f
	}
	if rd.stopped(m.base) {
		<-m.global
		<-sem
		return nil, cancelled
	}
	return func() { <-m.global; <-sem }, nil
}

// cleanup removes what writer e's calls left behind. It runs whatever
// happened to the calls, after Close too: its context outlives the
// Manager's and ends only with its own time limit. It logs a failure's
// code only.
func (m *Manager) cleanup(c *comparison, e Engine, refs []string) {
	m.cleanupRefs(e, refs)
	m.locked(func() {
		c.active--
		m.touchLocked(c)
	})
}

// cleanupRefs removes what writer e's calls left behind (refs) and logs a
// failure's code only.
func (m *Manager) cleanupRefs(e Engine, refs []string) {
	code := ""
	func() {
		defer func() {
			if v := recover(); v != nil {
				code = core.ErrCodeInternal
			}
		}()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(m.base), core.JudgeCleanupTimeout*time.Duration(2+len(refs)))
		defer cancel()
		if err := e.Cleaner.CleanupJudge(ctx, m.o.Gate.Folder, refs); err != nil {
			if code = core.ErrorCode(err); code == "" {
				code = core.ErrCodeInternal
			}
		}
	}()
	if code != "" {
		m.log.Warn("writer cleanup failed", "writer", e.Agent, "code", code)
	}
}

// locked runs f holding mu, and lets mu go even when f panics.
func (m *Manager) locked(f func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f()
}

// finishLocked ends comparison c, logs it and starts its combined answer
// when one waits. Callers hold mu.
func (m *Manager) finishLocked(c *comparison, status string, err *judge.Error, start time.Time) {
	if status == Done {
		err = nil
	}
	c.status, c.err, c.finishedAt = status, err, m.now()
	code := ""
	if err != nil {
		code = err.Code
	}
	m.log.Info("comparison ended", "comparison", c.id, "status", status, "code", code, "ms", time.Since(start).Milliseconds())
	m.synthAfterLocked(c)
}
