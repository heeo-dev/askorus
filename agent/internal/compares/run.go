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

// Fixed messages of request and comparison errors. A judge_failed
// request keeps the judging AI's own words instead (at most
// core.JudgeMessageChars characters); no other message ever carries an
// adapter's Detail, which may name a model or a path.
var messages = map[string]string{
	core.ErrCodeJudgeFailed:      "the judging AI reported an error",
	core.ErrCodeJudgeTimeout:     "the judge gave no answer within its time limit",
	core.ErrCodeJudgeBadOutput:   "the judge's reply was not the judge JSON, also when asked again",
	core.ErrCodeJudgeUsedTool:    "the judge reached for a tool, so its reply was thrown away",
	core.ErrCodeBadModel:         "the judging AI does not know its judge model",
	core.ErrCodeAgentUnavailable: "the judging AI does not judge on this PC any more",
	core.ErrCodeCancelled:        "the comparison was cancelled before this request ran",
	core.ErrCodeInternal:         "the agent could not run this judge request",
}

const (
	msgOther            = "the judge request failed"
	msgQueue            = "the request waited over 5 minutes for a free judge process"
	msgCompany          = "the model that judged is not of the company the judge was picked for"
	msgFolder           = "the agent could not make the judge folder"
	msgPrompt           = "the agent could not write the judge prompt"
	msgCards            = "the agent could not make the cards"
	msgRunGone          = "the run of this comparison is gone"
	msgClosed           = "the agent is stopping"
	msgCancelledWaiting = "the comparison was cancelled before the answers were in"
)

// failure is a request error with code and message, or the fixed message
// of code when message is "".
func failure(code, message string) *judge.Error {
	if message == "" {
		message = messages[code]
	}
	if message == "" {
		message = msgOther
	}
	return &judge.Error{Code: code, Message: message}
}

// judgeFailure turns a Judge error into a request error: the code, and for
// judge_failed the AI's own words cut to core.JudgeMessageChars.
func judgeFailure(err error) *judge.Error {
	var ae *core.ActionError
	if !errors.As(err, &ae) || ae.Code == "" {
		return failure(core.ErrCodeInternal, "")
	}
	if ae.Code == core.ErrCodeJudgeFailed {
		return failure(ae.Code, clipChars(strings.TrimSpace(ae.Detail), core.JudgeMessageChars))
	}
	return failure(ae.Code, "")
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

// round is one round of a comparison's requests: the first, or a retry's.
type round struct {
	start    time.Time       // real time: the queue limit and the logged ms count from it
	cancel   <-chan struct{} // the comparison's cancelCh when the round began
	spec     judge.Spec
	engines  map[string]Engine // judge AI → its engine
	families map[string]string // judge AI → the company it was planned with
	left     int               // requests of the round still running
	perAI    map[string]int    // the same, per judge AI
	called   map[string]bool   // a Judge call of the AI ran
	refs     map[string][]string
}

func (m *Manager) newRoundLocked(c *comparison, engines map[string]Engine) *round {
	return &round{
		start: time.Now(), cancel: c.cancelCh, spec: c.spec, engines: engines, families: c.families,
		perAI: map[string]int{}, called: map[string]bool{}, refs: map[string][]string{},
	}
}

// add counts one more request of judge AI agent in the round.
func (rd *round) add(agent string) {
	rd.left++
	rd.perAI[agent]++
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

// plan is what a round's planning made, outside the lock.
type plan struct {
	spec     judge.Spec
	requests []judge.Request
	err      *judge.Error // need_two_answers, no_judge or internal
	engines  map[string]Engine
	families map[string]string
	mkErr    error
}

// round plans comparison c and starts its requests. It holds c's slot
// (the waiting slot of a run's comparison, or Start's or Retry's) until
// the requests are spawned, so the comparison is never idle in between.
func (m *Manager) round(c *comparison) {
	var p plan
	func() {
		defer func() {
			if v := recover(); v != nil {
				p = plan{err: failure(core.ErrCodeInternal, "")}
			}
		}()
		p = m.makePlan(c)
	}()
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.beginSafeLocked(c, p) {
		c.broken = true
		m.cancelLocked(c)
		if c.active == 1 && (c.status == Waiting || c.status == Running) {
			// Nothing was spawned: no request will end the round.
			for i := range c.requests {
				if r := &c.requests[i]; r.Status != judge.RequestDone && r.Status != judge.RequestFailed {
					r.Status, r.Error = judge.RequestFailed, failure(core.ErrCodeInternal, "")
				}
			}
			m.finishLocked(c, Failed, failure(core.ErrCodeInternal, ""), time.Now())
		}
	}
	c.active--
	m.touchLocked(c)
}

// beginSafeLocked is beginLocked, false when it panicked.
func (m *Manager) beginSafeLocked(c *comparison, p plan) (ok bool) {
	defer func() {
		if v := recover(); v != nil {
			ok = false
		}
	}()
	m.beginLocked(c, p)
	return true
}

// makePlan splits the answers, picks the judges and plans the requests.
// It runs outside the lock: it calls the adapters.
func (m *Manager) makePlan(c *comparison) plan {
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
	var p plan
	var want []string
	askCodes := map[string]string{}
	switch {
	case d != nil:
		p.spec = judge.Spec{Question: d.Question, Earlier: slices.Clone(d.Earlier)}
		for _, a := range d.Answers {
			family := judge.HandoffFamily(a.Agent)
			if a.Via == judge.ViaPC {
				family = m.family(a.Agent, a.Model)
			}
			p.spec.Answers = append(p.spec.Answers, m.answer(a.ID, a.Agent, a.Via, a.Model, family, a.Text, true))
		}
		want = d.Judges
		if len(want) == 0 {
			for _, e := range engines {
				want = append(want, e.Agent)
			}
		}
	case rs != nil && run != nil:
		p.spec = judge.Spec{Question: rs.Question, Earlier: slices.Clone(rs.Earlier)}
		for _, part := range run.Parts {
			ready := part.Status == runs.Done && strings.TrimSpace(part.Answer) != ""
			p.spec.Answers = append(p.spec.Answers, m.answer(part.Agent, part.Agent, judge.ViaPC, part.Model, m.family(part.Agent, part.Model), part.Answer, ready))
			if part.Status == runs.Failed && part.Error != nil {
				switch part.Error.Code {
				case core.ErrCodeAskTimeout, core.ErrCodeAskEmpty, core.ErrCodeAskFailed:
					askCodes[part.Agent] = part.Error.Code
				}
			}
		}
		want = rs.Judges
		if len(want) == 0 {
			want = rs.Asked
		}
	default:
		return plan{err: failure(core.ErrCodeInternal, msgRunGone)}
	}
	p.engines, p.families = map[string]Engine{}, map[string]string{}
	for _, name := range want {
		i := slices.IndexFunc(engines, func(e Engine) bool { return e.Agent == name })
		if i < 0 || p.engines[name].Agent != "" {
			continue // it does not judge now, or is listed twice
		}
		e := engines[i]
		p.engines[name], p.families[name] = e, e.Family
		p.spec.Judges = append(p.spec.Judges, judge.JudgeAI{Agent: e.Agent, Family: e.Family, Model: e.Model, Effort: e.Effort, AskCode: askCodes[name]})
	}
	reqs, err := judge.Plan(p.spec)
	p.requests = reqs
	if err != nil {
		var je *judge.Error
		if !errors.As(err, &je) {
			je = failure(core.ErrCodeInternal, "")
		}
		p.err = &judge.Error{Code: je.Code, Message: je.Message}
		return p
	}
	if slices.ContainsFunc(reqs, func(r judge.Request) bool { return r.Judge != "" }) {
		p.mkErr = makeFolder(m.o.Gate.Folder)
	}
	return p
}

// makeFolder makes the judge folder, as every round does first.
func makeFolder(folder string) error {
	if folder == "" {
		return errors.New("no judge folder")
	}
	return os.MkdirAll(folder, 0o700)
}

// family is the company of the model that wrote a PC answer, as the
// answering AI's adapter names it: "" when the adapter cannot judge, or
// when no model is known (fail closed). The AI need not judge itself.
func (m *Manager) family(agent, model string) string {
	j, ok := m.adapter(agent).(core.Judger)
	if !ok || model == "" {
		return ""
	}
	return j.Family(model)
}

// answer is one answer of a comparison, split into units when it is ready.
func (m *Manager) answer(id, agent, via, model, family, text string, ready bool) judge.Answer {
	a := judge.Answer{ID: id, Agent: agent, Via: via, Family: family, Model: model, Status: judge.AnswerMissing, Units: []judge.Unit{}}
	if !ready {
		return a
	}
	a.Status, a.Text = judge.AnswerReady, text
	units, tail, _ := judge.SplitAnswer(id, text, m.maxUnits)
	if units != nil {
		a.Units = units
	}
	a.Tail = tail
	for _, u := range a.Units {
		if !u.Judged {
			a.NotJudged++
		}
	}
	return a
}

// beginLocked stores the plan and spawns its requests, or ends the
// comparison when it could not be planned. Callers hold mu.
func (m *Manager) beginLocked(c *comparison, p plan) {
	c.spec = p.spec
	c.requests = p.requests
	if c.requests == nil {
		c.requests = []judge.Request{}
	}
	c.judgments, c.lastItems, c.lastEndedItems = nil, nil, nil
	c.families = p.families
	if p.err != nil {
		c.planned = false
		m.aggregateLocked(c, true)
		m.finishLocked(c, Failed, p.err, time.Now())
		return
	}
	c.planned = true
	c.status = Running
	rd := m.newRoundLocked(c, p.engines)
	var spawn []int
	judges, noJudge, queued := map[string]bool{}, 0, 0
	for i := range c.requests {
		r := &c.requests[i]
		if r.Judge == "" {
			noJudge++
			continue
		}
		queued++
		judges[r.Judge] = true
		if p.mkErr != nil {
			r.Status, r.Error = judge.RequestFailed, failure(core.ErrCodeInternal, msgFolder)
			continue
		}
		rd.add(r.Judge)
		spawn = append(spawn, i)
	}
	m.log.Info("comparison started", "comparison", c.id, "source", c.source, "ais", len(c.spec.Answers),
		"requests", queued, "judges", len(judges), "noJudge", noJudge)
	m.aggregateLocked(c, rd.left == 0)
	m.spawnLocked(c, rd, spawn)
	if len(spawn) == 0 {
		// Nothing was spawned, so no request ends the round. A request
		// that was spawned and ended at once (cancelled, or the Manager
		// closed) has ended it already (R-30).
		m.endRoundLocked(c, rd)
	}
}

// spawnLocked starts the requests at idx of round rd. A request of a
// cancelled comparison or a closed Manager ends at once. Callers hold mu.
func (m *Manager) spawnLocked(c *comparison, rd *round, idx []int) {
	for _, i := range idx {
		req, e := c.requests[i], rd.engines[c.requests[i].Judge]
		c.active++
		if c.cancelled || !m.goLocked(func() { m.request(c, rd, i, req, e) }) {
			c.active--
			m.endRequestLocked(c, rd, i, e, outcome{err: failure(core.ErrCodeCancelled, "")})
		}
	}
}

// outcome is how one request ended: done with v, or failed with err.
type outcome struct {
	done bool
	v    judge.Validated
	err  *judge.Error
}

// request runs request i of comparison c to its end, in its own
// goroutine. Whatever happens, a panic too, it ends the request, so the
// round and the cleanup always come.
func (m *Manager) request(c *comparison, rd *round, i int, req judge.Request, e Engine) {
	out := outcome{err: failure(core.ErrCodeInternal, "")}
	defer func() {
		if v := recover(); v != nil {
			out = outcome{err: failure(core.ErrCodeInternal, "")}
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.endRequestLocked(c, rd, i, e, out)
		c.active--
		m.touchLocked(c)
	}()
	out = m.judgeRequest(c, rd, i, req, e)
}

// judgeRequest waits for a slot and asks the judge, once more when the
// reply was not the judge JSON or missed ids. A new nonce is drawn every
// time. After a cancel no judge is asked again.
func (m *Manager) judgeRequest(c *comparison, rd *round, i int, req judge.Request, e Engine) outcome {
	release, failed := m.acquire(rd, e.Agent)
	if failed != nil {
		return outcome{err: failed}
	}
	defer release()
	m.locked(func() {
		c.requests[i].Status = judge.RequestRunning
		m.touchLocked(c)
	})

	task := judge.BuildTask(rd.spec, req)
	var first *judge.Validated // attempt 1's verdicts when it missed ids
	var firstErr *judge.Error  // attempt 1's bad output
	end := func(err *judge.Error) outcome {
		if first != nil {
			return outcome{done: true, v: *first} // attempt 1's verdicts stand
		}
		return outcome{err: err}
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if attempt == 2 && rd.stopped(m.base) {
			return end(firstErr)
		}
		prompt := judge.Prompt(task, judge.Nonce(task))
		if prompt == "" {
			return end(failure(core.ErrCodeInternal, msgPrompt))
		}
		m.locked(func() {
			c.requests[i].Attempt = attempt
			rd.called[e.Agent] = true
			m.touchLocked(c)
		})

		start := time.Now()
		res, err := m.callJudge(e, core.JudgeRequest{Folder: m.o.Gate.Folder, Task: task, Prompt: prompt, Model: e.Model, Effort: e.Effort})
		// The cleanup ref is kept whatever the error (core.Judger).
		m.locked(func() {
			r := &c.requests[i]
			r.Ms += time.Since(start).Milliseconds()
			r.InputTokens += res.InputTokens
			r.OutputTokens += res.OutputTokens
			if res.Model != "" {
				r.Model = res.Model // the model that judged (S3 decision 14)
			}
			if res.Cleanup != "" {
				rd.refs[e.Agent] = append(rd.refs[e.Agent], res.Cleanup)
			}
			m.touchLocked(c)
		})

		if err != nil {
			f := judgeFailure(err)
			if f.Code == core.ErrCodeJudgeBadOutput && attempt == 1 {
				firstErr = f
				continue
			}
			return end(f)
		}
		// S2 decision 3: a judge of another company than planned may be
		// judging its own company's answer. Its reply is thrown away.
		if family := e.Judger.Family(res.Model); family == "" || !strings.EqualFold(family, rd.families[e.Agent]) {
			return end(failure(core.ErrCodeBadModel, msgCompany))
		}
		v, verr := judge.Validate(task, res.Output)
		if verr != nil {
			var je *judge.Error
			if !errors.As(verr, &je) || je.Code == core.ErrCodeInternal {
				return end(failure(core.ErrCodeInternal, ""))
			}
			f := failure(core.ErrCodeJudgeBadOutput, "")
			if attempt == 1 {
				firstErr = f
				continue
			}
			return end(f)
		}
		switch {
		case first != nil:
			return outcome{done: true, v: mergeVerdicts(task, *first, v)}
		case attempt == 1 && len(v.Missing) > 0:
			first = &v
			continue
		}
		return outcome{done: true, v: v}
	}
	return end(firstErr)
}

// mergeVerdicts keeps attempt 1's verdicts and takes attempt 2's only for
// the ids attempt 1 missed (S3 decision 7), in the order the ids were
// sent. Ids both missed stay missing, and so unjudged.
func mergeVerdicts(task core.JudgeTask, first, second judge.Validated) judge.Validated {
	missed := map[string]bool{}
	for _, id := range first.Missing {
		missed[id] = true
	}
	byID := map[string]judge.Verdict{}
	for _, v := range first.Verdicts {
		byID[v.ID] = v
	}
	for _, v := range second.Verdicts {
		if missed[v.ID] {
			byID[v.ID] = v
		}
	}
	out := judge.Validated{Verdicts: []judge.Verdict{}, Missing: []string{}, Dropped: first.Dropped + second.Dropped}
	seen := map[string]bool{}
	for _, u := range task.Units {
		if u.ID == "" || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		if v, ok := byID[u.ID]; ok {
			out.Verdicts = append(out.Verdicts, v)
		} else if missed[u.ID] {
			out.Missing = append(out.Missing, u.ID)
		}
	}
	return out
}

// acquire waits for a judge process slot of agent and then a slot of
// all AIs. The wait ends with judge_timeout QueueLimit after the round
// began, and with cancelled when the comparison is cancelled or the
// Manager closes. release gives both slots back.
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

// callJudge makes one Judge call within JudgeLimit. A Judge that panics
// gives internal; one that ends with the limit gives judge_timeout; one
// that keeps going abandonAfter past it is left behind with
// judge_timeout, so a stuck engine cannot hold the comparison.
func (m *Manager) callJudge(e Engine, req core.JudgeRequest) (core.JudgeResult, error) {
	ctx, cancel := context.WithTimeout(m.base, m.judgeLimit)
	defer cancel()
	type result struct {
		res core.JudgeResult
		err error
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if v := recover(); v != nil {
				ch <- result{err: core.Fail(core.ErrCodeInternal, "")}
			}
		}()
		res, err := e.Judger.Judge(ctx, req)
		ch <- result{res, err}
	}()
	timedOut := func(r result) (core.JudgeResult, error) {
		if r.err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			r.err = core.Fail(core.ErrCodeJudgeTimeout, "")
		}
		return r.res, r.err
	}
	select {
	case r := <-ch:
		return timedOut(r)
	case <-ctx.Done():
	}
	t := time.NewTimer(abandonAfter)
	defer t.Stop()
	select {
	case r := <-ch:
		return timedOut(r)
	case <-t.C:
		return core.JudgeResult{}, core.Fail(core.ErrCodeJudgeTimeout, "")
	}
}

// endRequestLocked records how request i ended, makes the cards again,
// starts the judge AI's cleanup after its last request of the round and
// ends the round after its last request. Callers hold mu.
func (m *Manager) endRequestLocked(c *comparison, rd *round, i int, e Engine, out outcome) {
	r := &c.requests[i]
	code := ""
	if out.done {
		r.Status, r.Error = judge.RequestDone, nil
		c.judgments = append(c.judgments, judge.Judgments(*r, out.v)...)
	} else {
		r.Status, r.Error = judge.RequestFailed, out.err
		code = out.err.Code
	}
	rd.left--
	rd.perAI[r.Judge]--
	m.aggregateLocked(c, allEnded(c.requests))
	m.log.Info("judge request", "comparison", c.id, "judge", r.Judge, "status", r.Status, "code", code,
		"attempt", r.Attempt, "ms", r.Ms)
	if rd.perAI[r.Judge] == 0 && rd.called[r.Judge] && e.Cleaner != nil {
		// The cleanup starts even when the Manager is closed (R-30): the
		// judge's conversations hold the question and the answers. Close
		// waits for it up to closeWait. A judge was called, so a request
		// goroutine of the Manager's WaitGroup runs this: Add is safe.
		refs := slices.Clone(rd.refs[r.Judge])
		c.active++
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.cleanup(c, e, refs)
		}()
	}
	if rd.left == 0 {
		m.endRoundLocked(c, rd)
	}
	m.touchLocked(c)
}

func allEnded(reqs []judge.Request) bool {
	for _, r := range reqs {
		if r.Status != judge.RequestDone && r.Status != judge.RequestFailed {
			return false
		}
	}
	return true
}

// cleanup removes what judge AI e's calls of one round left behind. It
// runs whatever happened to the requests, after Close too: its context
// outlives the Manager's and ends only with its own time limit. It logs a
// failure's code only.
func (m *Manager) cleanup(c *comparison, e Engine, refs []string) {
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
		m.log.Warn("judge cleanup failed", "judge", e.Agent, "code", code)
	}
	m.locked(func() {
		c.active--
		m.touchLocked(c)
	})
}

// locked runs f holding mu, and lets mu go even when f panics.
func (m *Manager) locked(f func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f()
}

// endRoundLocked ends the comparison after the last request of a round:
// done when any request is done, else failed with the first failed
// request's error in plan order (S3 decision 8). Its cleanup may still
// run. Callers hold mu.
func (m *Manager) endRoundLocked(c *comparison, rd *round) {
	m.aggregateLocked(c, true)
	status, err := Failed, (*judge.Error)(nil)
	if len(c.requests) == 0 || slices.ContainsFunc(c.requests, func(r judge.Request) bool { return r.Status == judge.RequestDone }) {
		status = Done
	} else {
		// A judge's failure first: a no_judge record is not what Retry can
		// fix, so it names the comparison's error only when nothing else
		// failed.
		for _, judged := range []bool{true, false} {
			for _, r := range c.requests {
				if r.Status == judge.RequestFailed && r.Error != nil && (r.Judge != "") == judged {
					err = &judge.Error{Code: r.Error.Code, Message: r.Error.Message}
					break
				}
			}
			if err != nil {
				break
			}
		}
		if err == nil {
			err = failure(core.ErrCodeInternal, "")
		}
	}
	if c.broken {
		status, err = Failed, failure(core.ErrCodeInternal, msgCards)
	}
	m.finishLocked(c, status, err, rd.start)
}

// finishLocked ends comparison c and logs it. Callers hold mu.
func (m *Manager) finishLocked(c *comparison, status string, err *judge.Error, start time.Time) {
	if status == Done {
		err = nil
	}
	c.status, c.err, c.finishedAt = status, err, m.now()
	code := ""
	if err != nil {
		code = err.Code
	}
	n := c.summary.Counts
	m.log.Info("comparison ended", "comparison", c.id, "status", status, "code", code,
		"differ", n.Differ, "check", n.Check, "agree", n.Agree, "onlyOne", n.OnlyOne,
		"notCompared", n.NotCompared, "notJudged", n.NotJudged, "ms", time.Since(start).Milliseconds())
}

// aggregateLocked makes the cards again from every judgment so far. When
// it panics, the comparison is broken: it asks no judge again and ends
// failed with internal. Callers hold mu.
func (m *Manager) aggregateLocked(c *comparison, ended bool) {
	in := judge.AggregateInput{
		Answers: c.spec.Answers, Requests: c.requests, Judgments: c.judgments, Checks: m.checks, Ended: ended,
		Prev: append(slices.Clone(c.lastItems), c.lastEndedItems...),
	}
	s, ok := m.aggregateSafe(in)
	if !ok {
		c.broken = true
		m.cancelLocked(c)
		return
	}
	c.summary = s
	c.lastItems = s.Items
	if ended {
		c.lastEndedItems = s.Items
	}
}

// aggregateSafe is judge.Aggregate that cannot take the agent down.
func (m *Manager) aggregateSafe(in judge.AggregateInput) (s judge.Summary, ok bool) {
	defer func() {
		if v := recover(); v != nil {
			s, ok = judge.Summary{}, false
		}
	}()
	return m.aggregate(in), true
}
