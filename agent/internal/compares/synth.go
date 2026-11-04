package compares

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Combined answers (종합 답변, P2-3 S5b, spec part 20). Once a comparison's
// judge round has ended, done or failed, one AI writes one combined answer
// from the question, every answer with its AI's name and the 다른 말 and
// 확인 필요 items, with its ask tier model and the judge's locked-down
// command (core.Synthesizer). Only an AI asked for the question writes
// (결정 5's rule): the one the person picked, else the first asked AI that
// may. The writer takes the judges' process slots. The combined answer
// has its own status beside the comparison's, which stays the judges'.
// Logs carry codes and counts only, never the text, the prompt or the
// nonce.

// SynthLimit bounds one combined-answer call. It is a safety net only:
// every adapter's Synthesize applies core.SynthTimeout itself.
const SynthLimit = core.SynthTimeout + 30*time.Second

// Combined-answer statuses (Synthesis.Status).
const (
	SynthWaiting = "waiting" // the judges are not done yet
	SynthRunning = "running" // waiting for a process slot, or writing
	SynthDone    = "done"
	SynthFailed  = "failed" // Error says why
	SynthOff     = "off"    // not asked for: POST …/synthesis writes it
)

// SynthSpec asks for a comparison's combined answer. A nil *SynthSpec is
// off.
type SynthSpec struct {
	Off   bool
	Agent string // the AI the person picked; "" lets the agent pick
	Tier  string // the ask tier whose model writes; "" or not valid: standard
}

// Synthesis is a comparison's combined answer as the snapshot shows it.
// Text is set only when it is done. There is no prompt, nonce or reason
// anywhere. Fields may be added inside /v1, never removed (principle 4).
type Synthesis struct {
	Status string `json:"status"`
	// Agent is the AI the person asked for until one is picked, then the
	// AI that writes.
	Agent        string `json:"agent,omitempty"`
	Model        string `json:"model,omitempty"`   // the model that wrote, as the AI names it
	Attempt      int    `json:"attempt,omitempty"` // 2 after the retry on bad output
	Ms           int64  `json:"ms,omitempty"`      // over the attempts
	InputTokens  int    `json:"inputTokens,omitempty"`
	OutputTokens int    `json:"outputTokens,omitempty"`
	Text         string `json:"text,omitempty"` // Markdown
	// Judged is true when the prompt carried the comparison's results: at
	// least one judge request was done when it was made.
	Judged bool `json:"judged"`
	// UnnamedSides counts the 다른 말 sides given to the writer none of
	// whose AI names the text has: the phone warns when it is over 0.
	UnnamedSides int          `json:"unnamedSides,omitempty"`
	Error        *judge.Error `json:"error,omitempty"`
	// Topics names the 다른 말 items for the phone (C5, topics.go): none
	// when Options.Topics is off, the comparison has no such item or the
	// combined answer has not started.
	Topics *Topics `json:"topics,omitempty"`
}

// synthState is a comparison's combined answer. Manager.mu guards it.
type synthState struct {
	spec      SynthSpec
	status    string
	writer    string // the AI that writes, once picked
	model     string
	attempt   int
	ms        int64
	in, out   int
	text      string
	judged    bool
	unnamed   int
	err       *judge.Error
	cancelled bool
	cancelCh  chan struct{} // closed by Cancel while it runs
	topics    *topicsState  // the 다른 말 items named beside it (topics.go)
}

// newSynth is the combined answer a spec asks for: off without one.
func newSynth(s *SynthSpec) synthState {
	if s == nil || s.Off {
		st := synthState{status: SynthOff, cancelCh: make(chan struct{})}
		if s != nil {
			st.spec = *s
		}
		return st
	}
	return synthState{spec: *s, status: SynthWaiting, cancelCh: make(chan struct{})}
}

// Fixed messages of combined-answer errors. A judge_failed one keeps the
// writing AI's own words instead, unless they hold the synth JSON.
var synthMessages = map[string]string{
	core.ErrCodeJudgeFailed:      "the writing AI reported an error",
	core.ErrCodeJudgeTimeout:     "the writing AI gave no combined answer within its time limit",
	core.ErrCodeJudgeBadOutput:   "the writing AI's reply was not the combined-answer JSON, also when asked again",
	core.ErrCodeJudgeUsedTool:    "the writing AI reached for a tool, so its reply was thrown away",
	core.ErrCodeBadModel:         "the writing AI does not know its model",
	core.ErrCodeAgentUnavailable: "the writing AI does not write on this PC any more",
	core.ErrCodeNoJudge:          "no AI asked for this question may write the combined answer on this PC",
	core.ErrCodeNeedTwoAnswers:   "a combined answer needs at least two answers",
	core.ErrCodeCancelled:        "the combined answer was cancelled before it was written",
	core.ErrCodeInternal:         "the agent could not write the combined answer",
}

const (
	msgSynthOther   = "the combined answer failed"
	msgSynthCompany = "the model that wrote is not of the company the writer was picked for"
	msgSynthQueue   = "the combined answer waited over 5 minutes for a free process"
	msgSynthClosed  = "the agent is stopping"
	// synthKey marks the synth JSON, which never becomes a message.
	synthKey = `"answer"`
)

// synthFailure is a combined-answer error with code and message, or the
// fixed message of code when message is "".
func synthFailure(code, message string) *judge.Error {
	if message == "" {
		message = synthMessages[code]
	}
	if message == "" {
		message = msgSynthOther
	}
	return &judge.Error{Code: code, Message: message}
}

// synthCallFailure turns a Synthesize error into a combined-answer error:
// the code, and for judge_failed the AI's own words cut to
// core.JudgeMessageChars, or the fixed text when they hold the synth JSON.
func synthCallFailure(err error) *judge.Error {
	var ae *core.ActionError
	if !errors.As(err, &ae) || ae.Code == "" {
		return synthFailure(core.ErrCodeInternal, "")
	}
	if ae.Code == core.ErrCodeJudgeFailed {
		msg := clipChars(strings.TrimSpace(ae.Detail), core.JudgeMessageChars)
		if strings.Contains(msg, synthKey) {
			msg = ""
		}
		return synthFailure(ae.Code, msg)
	}
	return synthFailure(ae.Code, "")
}

// snapshotSynthLocked is c's combined answer for a snapshot. Callers hold
// mu.
func snapshotSynthLocked(c *comparison) *Synthesis {
	s := c.synth
	out := &Synthesis{
		Status: s.status, Agent: s.spec.Agent, Model: s.model, Attempt: s.attempt, Ms: s.ms,
		InputTokens: s.in, OutputTokens: s.out, Judged: s.judged, UnnamedSides: s.unnamed,
	}
	if out.Status == "" {
		out.Status = SynthOff
	}
	if s.writer != "" {
		out.Agent = s.writer
	}
	if s.status == SynthDone {
		out.Text = s.text
	}
	if s.err != nil {
		e := *s.err
		out.Error = &e
	}
	out.Topics = snapshotTopics(s.topics)
	return out
}

// synthBusy reports whether c's combined answer waits or is written, or
// its topics are being named.
func synthBusy(c *comparison) bool {
	return c.synth.status == SynthWaiting || c.synth.status == SynthRunning || topicsBusy(c)
}

// synthResetLocked makes c's combined answer wait for the next round's
// end, unless it is off. Callers hold mu.
func (m *Manager) synthResetLocked(c *comparison) {
	if c.synth.status == SynthOff || c.synth.status == "" {
		return
	}
	spec := c.synth.spec
	c.synth = newSynth(&spec)
}

// synthAfterLocked starts c's combined answer when its judge round has
// ended: finishLocked and endUnstartedLocked call it. It acts only on a
// combined answer that waits: a broken comparison fails it with internal,
// a cancelled one or a closed Manager with cancelled, fewer than two
// answers with need_two_answers; else it starts at once, without waiting
// for the judges' cleanup. Callers hold mu.
func (m *Manager) synthAfterLocked(c *comparison) {
	if c.synth.status != SynthWaiting {
		return
	}
	switch {
	case c.broken:
		m.synthEndLocked(c, synthFailure(core.ErrCodeInternal, ""))
	case c.cancelled:
		m.synthEndLocked(c, synthFailure(core.ErrCodeCancelled, ""))
	default:
		m.synthStartLocked(c)
	}
}

// synthStartLocked starts c's combined answer that waits, or ends it at
// once when it cannot be written. Callers hold mu.
func (m *Manager) synthStartLocked(c *comparison) {
	ready := 0
	for _, a := range c.spec.Answers {
		if a.Status == judge.AnswerReady && strings.TrimSpace(a.Text) != "" {
			ready++
		}
	}
	switch {
	case m.closed:
		m.synthEndLocked(c, synthFailure(core.ErrCodeCancelled, msgSynthClosed))
		return
	case c.broken:
		m.synthEndLocked(c, synthFailure(core.ErrCodeInternal, ""))
		return
	case ready < 2:
		// Contract B, also when the comparison itself failed internal (its
		// run is gone): writing again cannot help, and the phone hides it
		// (R-35).
		m.synthEndLocked(c, synthFailure(core.ErrCodeNeedTwoAnswers, ""))
		return
	}
	c.synth.status = SynthRunning
	c.synth.cancelled, c.synth.cancelCh = false, make(chan struct{})
	c.active++
	if !m.goLocked(func() { m.synthesize(c) }) {
		c.active--
		m.synthEndLocked(c, synthFailure(core.ErrCodeCancelled, msgSynthClosed))
	}
}

// synthEndLocked ends c's combined answer with err, or done when err is
// nil, and logs it. An ended comparison then lives KeepFor from now, so
// the phone can still read the combined answer. Callers hold mu.
func (m *Manager) synthEndLocked(c *comparison, err *judge.Error) {
	s := &c.synth
	code := ""
	if err != nil {
		s.status, s.err, s.text, s.unnamed = SynthFailed, err, "", 0
		code = err.Code
	} else {
		s.status, s.err = SynthDone, nil
	}
	if c.status == Done || c.status == Failed {
		c.finishedAt = m.now()
	}
	m.log.Info("synthesis ended", "comparison", c.id, "writer", s.writer, "status", s.status, "code", code,
		"attempt", s.attempt, "ms", s.ms, "unnamedSides", s.unnamed)
}

// cancelSynthLocked cancels c's combined answer: one that waits fails with
// cancelled at once, one that runs drops its wait for a slot and is not
// asked again, though a call that runs finishes. Callers hold mu.
func (m *Manager) cancelSynthLocked(c *comparison) {
	switch c.synth.status {
	case SynthWaiting:
		m.synthEndLocked(c, synthFailure(core.ErrCodeCancelled, ""))
	case SynthRunning:
		if !c.synth.cancelled {
			c.synth.cancelled = true
			close(c.synth.cancelCh)
		}
	default: // the topics may still wait for a slot
		if topicsBusy(c) && !c.synth.cancelled {
			c.synth.cancelled = true
			close(c.synth.cancelCh)
		}
	}
}

// Synthesize writes the combined answer of an ended comparison whose
// combined answer failed or was off, with agent when it is not "" (an AI
// of this PC; the caller checks the name), else with the AI asked for
// before. A done combined answer comes back unchanged. It returns
// ErrNotFound, or ErrRunning while the comparison or its combined answer
// waits or runs.
func (m *Manager) Synthesize(owner, id, agent string) (Comparison, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	c := m.byID[id]
	switch {
	case c == nil || c.owner != owner:
		return Comparison{}, ErrNotFound
	case c.status == Waiting || c.status == Running || synthBusy(c):
		return Comparison{}, ErrRunning
	case c.synth.status == SynthDone:
		return m.snapshotLocked(c), nil
	}
	spec := c.synth.spec
	spec.Off = false
	if agent != "" {
		spec.Agent = agent
	}
	c.synth = newSynth(&spec)
	// An explicit write: a cancel of the judges does not stop it.
	m.synthStartLocked(c)
	m.touchLocked(c)
	return m.snapshotLocked(c), nil
}

// Writers are the AIs that write combined answers now at tier, in the
// adapters' order. An adapter whose Info, Family or AskTiers panics does
// not write.
func (m *Manager) Writers(ctx context.Context, tier string) []Writer {
	var out []Writer
	for _, a := range m.o.Adapters {
		func() {
			defer func() { _ = recover() }()
			if w, why := m.o.Gate.Writer(a, a.Info(ctx), tier); why == "" {
				out = append(out, w)
			}
		}()
	}
	return out
}

// synthInput is what the goroutine reads of a comparison under the lock.
type synthInput struct {
	spec    SynthSpec
	tier    string
	allowed []string // the AIs that may write, in order of preference after spec.Agent
	anyAI   bool     // a direct comparison without judges: every AI that writes may
	task    judge.Spec
	items   []judge.Item
	judged  bool
	cancel  <-chan struct{}
	// qspec is the run's question spec when it was refined (milestone C,
	// off since 2026-11-04). It rides in the task; synth-v4 does not use it.
	qspec *judge.QuestionSpec
}

// synthesize writes c's combined answer, in its own goroutine. Whatever
// happens, a panic too, it ends the combined answer and gives back its
// slot, so WaitIdle and the cleanup always come.
func (m *Manager) synthesize(c *comparison) {
	err := synthFailure(core.ErrCodeInternal, "")
	var text string
	var unnamed int
	defer func() {
		if v := recover(); v != nil {
			err = synthFailure(core.ErrCodeInternal, "")
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if err == nil {
			c.synth.text, c.synth.unnamed = text, unnamed
		}
		m.synthEndLocked(c, err)
		c.active--
		m.touchLocked(c)
	}()
	text, unnamed, err = m.synthRun(c)
}

// synthRun picks the writer, makes the task, waits for a slot and asks the
// writer, once more when the reply was not the combined answer. A new
// nonce is drawn every time. After a cancel no writer is asked again.
func (m *Manager) synthRun(c *comparison) (string, int, *judge.Error) {
	var in synthInput
	m.locked(func() { in = m.synthInputLocked(c) })

	w, ok := pickWriter(m.Writers(m.base, in.tier), in)
	if !ok {
		return "", 0, synthFailure(core.ErrCodeNoJudge, "")
	}
	// The earlier questions are the ones every judge already got (part 20:
	// earlier as for judging, R-35): a writer that is not one of the
	// planned judges gets none.
	if !slices.ContainsFunc(in.task.Judges, func(j judge.JudgeAI) bool { return j.Agent == w.Agent }) {
		in.task.Earlier = nil
	}
	names := m.answerNames(in.task.Answers)
	task := judge.BuildSynthTask(in.task, in.items, in.judged, func(id string) string { return names[id] })
	task.Spec = in.qspec
	m.locked(func() {
		c.synth.writer, c.synth.model, c.synth.judged = w.Agent, w.Model, in.judged
		m.touchLocked(c)
	})
	m.log.Info("synthesis started", "comparison", c.id, "writer", w.Agent, "judged", in.judged, "rules", judge.SynthRulesOf(task),
		"ais", len(task.Answers), "items", len(task.Differ)+len(task.Check), "leftOut", task.LeftOut)
	m.topicsStart(c, w, in, names)
	if err := makeFolder(m.o.Gate.Folder); err != nil {
		return "", 0, synthFailure(core.ErrCodeInternal, "")
	}

	rd := &round{start: time.Now(), cancel: in.cancel}
	release, failed := m.acquire(rd, w.Agent)
	if failed != nil {
		switch {
		case failed.Code == core.ErrCodeJudgeTimeout:
			return "", 0, synthFailure(core.ErrCodeJudgeTimeout, msgSynthQueue)
		case m.base.Err() != nil:
			return "", 0, synthFailure(core.ErrCodeCancelled, msgSynthClosed)
		}
		return "", 0, synthFailure(core.ErrCodeCancelled, "")
	}
	called := false
	var refs []string
	defer func() {
		release()
		if called && w.Cleaner != nil {
			m.synthCleanup(c, w, refs)
		}
	}()

	var firstErr *judge.Error // attempt 1's bad output
	for attempt := 1; attempt <= 2; attempt++ {
		if attempt == 2 && rd.stopped(m.base) {
			return "", 0, firstErr
		}
		prompt := judge.SynthPrompt(task, judge.SynthNonce(task))
		if prompt == "" {
			return "", 0, synthFailure(core.ErrCodeInternal, "")
		}
		m.locked(func() {
			c.synth.attempt = attempt
			m.touchLocked(c)
		})
		called = true
		start := time.Now()
		res, err := m.callSynth(w, core.SynthRequest{Folder: m.o.Gate.Folder, Prompt: prompt, Model: w.Model, Effort: w.Effort})
		// The cleanup ref is kept whatever the error (core.Synthesizer).
		if res.Cleanup != "" {
			refs = append(refs, res.Cleanup)
		}
		m.locked(func() {
			s := &c.synth
			s.ms += time.Since(start).Milliseconds()
			s.in += res.InputTokens
			s.out += res.OutputTokens
			if res.Model != "" {
				s.model = res.Model
			}
			m.touchLocked(c)
		})
		if m.base.Err() != nil {
			return "", 0, synthFailure(core.ErrCodeCancelled, msgSynthClosed) // Close ended the writer
		}
		if err != nil {
			f := synthCallFailure(err)
			if f.Code == core.ErrCodeJudgeBadOutput && attempt == 1 {
				firstErr = f
				continue
			}
			return "", 0, f
		}
		// As for judges (S2 decision 3): a model of another company than
		// planned may be writing about its own company's answer.
		if family := w.Judger.Family(res.Model); family == "" || !strings.EqualFold(family, w.Family) {
			return "", 0, synthFailure(core.ErrCodeBadModel, msgSynthCompany)
		}
		text, verr := judge.ValidateSynth(res.Output)
		if verr != nil {
			f := synthFailure(core.ErrCodeJudgeBadOutput, "")
			if attempt == 1 {
				firstErr = f
				continue
			}
			return "", 0, f
		}
		return text, judge.UnnamedSides(task, text), nil
	}
	return "", 0, firstErr
}

// synthInputLocked reads what the combined answer of c needs. Callers
// hold mu.
func (m *Manager) synthInputLocked(c *comparison) synthInput {
	in := synthInput{
		spec: c.synth.spec, tier: c.synth.spec.Tier, task: c.spec,
		items: slices.Clone(c.summary.Items), cancel: c.synth.cancelCh,
		judged: slices.ContainsFunc(c.requests, func(r judge.Request) bool { return r.Status == judge.RequestDone }),
	}
	in.task.Answers = slices.Clone(c.spec.Answers)
	if c.run != nil && c.run.Refine != nil && c.run.Refine.Status == RefineDone && c.run.Refine.Spec != nil {
		sp := *c.run.Refine.Spec
		in.qspec = &sp
	}
	switch {
	case c.runSpec != nil:
		in.allowed = slices.Clone(c.runSpec.Asked)
		if in.tier == "" && c.run != nil {
			in.tier = c.run.Tier
		}
	case c.direct != nil:
		for _, a := range c.direct.Answers {
			if a.Via == judge.ViaPC && !slices.Contains(in.allowed, a.Agent) {
				in.allowed = append(in.allowed, a.Agent)
			}
		}
		for _, j := range c.direct.Judges {
			if !slices.Contains(in.allowed, j) {
				in.allowed = append(in.allowed, j)
			}
		}
		in.anyAI = len(c.direct.Judges) == 0
	}
	return in
}

// pickWriter is the AI that writes: in.spec.Agent when it may, else the
// first AI of in.allowed that writes, else, for a direct comparison
// without judges, the first AI that writes. An AI that was not asked
// never writes.
func pickWriter(writers []Writer, in synthInput) (Writer, bool) {
	find := func(agent string) (Writer, bool) {
		i := slices.IndexFunc(writers, func(w Writer) bool { return w.Agent == agent })
		if i < 0 {
			return Writer{}, false
		}
		return writers[i], true
	}
	may := func(agent string) bool { return in.anyAI || slices.Contains(in.allowed, agent) }
	if in.spec.Agent != "" && may(in.spec.Agent) {
		if w, ok := find(in.spec.Agent); ok {
			return w, true
		}
	}
	for _, a := range in.allowed {
		if w, ok := find(a); ok {
			return w, true
		}
	}
	if in.anyAI && len(writers) > 0 {
		return writers[0], true
	}
	return Writer{}, false
}

// answerNames are the names the combined answer gives the answers: a PC
// answer's adapter's DisplayName (core.Named), a pasted one's app name
// (judge.HandoffName), else the AI's code.
func (m *Manager) answerNames(answers []judge.Answer) map[string]string {
	out := map[string]string{}
	for _, a := range answers {
		name := ""
		switch a.Via {
		case judge.ViaPC:
			if n, ok := m.adapter(a.Agent).(core.Named); ok {
				name = strings.TrimSpace(n.DisplayName())
			}
		case judge.ViaHandoff:
			name = judge.HandoffName(a.Agent)
		}
		if name == "" {
			name = a.Agent
		}
		out[a.ID] = name
	}
	return out
}

// callSynth makes one Synthesize call within SynthLimit, as callJudge
// does for a judge: a panic gives internal, the limit judge_timeout, and a
// call that keeps going abandonAfter past it is left behind.
func (m *Manager) callSynth(w Writer, req core.SynthRequest) (core.SynthResult, error) {
	ctx, cancel := context.WithTimeout(m.base, m.synthLimit)
	defer cancel()
	type result struct {
		res core.SynthResult
		err error
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if v := recover(); v != nil {
				ch <- result{err: core.Fail(core.ErrCodeInternal, "")}
			}
		}()
		res, err := w.Synth.Synthesize(ctx, req)
		ch <- result{res, err}
	}()
	timedOut := func(r result) (core.SynthResult, error) {
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
		return core.SynthResult{}, core.Fail(core.ErrCodeJudgeTimeout, "")
	}
}

// synthCleanup starts the cleanup of what the writer left behind, as a
// judge AI's (endRequestLocked): it runs even when the Manager is closed
// (R-30), and the comparison is not idle until it has ended. The synth
// goroutine, one of the Manager's WaitGroup, calls it, so Add is safe.
func (m *Manager) synthCleanup(c *comparison, w Writer, refs []string) {
	e := Engine{Agent: w.Agent, Cleaner: w.Cleaner}
	refs = slices.Clone(refs)
	m.locked(func() { c.active++ })
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.cleanup(c, e, refs)
	}()
}
