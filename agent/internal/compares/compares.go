package compares

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/judge"
	"github.com/heeo-dev/askorus/agent/internal/runs"
)

// Limits and timings (spec parts 3 and 6).
const (
	// KeepFor is how long an ended comparison (and debate) can still be
	// read: a day, as runs.KeepFor (R-80, 11-07); MaxComparisons and
	// MaxDebates still bound memory.
	KeepFor = 24 * time.Hour
	// MaxComparisons bounds memory; the oldest ended comparisons go first.
	MaxComparisons = 100
	// MaxUnfinished bounds the comparisons that are waiting or running.
	MaxUnfinished = 8
	// MaxProcesses bounds the one-off processes (combined answers,
	// refining) of every AI together, apart from the asks'
	// runs.MaxRunning. Each AI runs at most judge.perAI.
	MaxProcesses = 6
	// QueueLimit is how long a one-off call may wait for a process slot;
	// then it fails with judge_timeout.
	QueueLimit = 5 * time.Minute
	// PollMs is the pollAfterMs of a comparison that is waiting or running.
	PollMs = 1000
	// closeWait is how long Close waits for the goroutines.
	closeWait = 5 * time.Second
)

// abandonAfter is how long a one-off call may go on after its limit
// before the comparison stops waiting for it; a variable so tests can
// shorten it.
var abandonAfter = 5 * time.Second

// Limits of a comparison made from answers (POST /v1/comparisons).
const (
	MinAnswers       = 2
	MaxAnswers       = 6
	MaxAnswerBytes   = judge.MaxAnswerBytes // 64 KiB of one answer's text
	MaxQuestionBytes = 32 << 10             // of the question, without white space around it
	MaxJudges        = 8
)

// Comparison statuses.
const (
	Waiting = "waiting" // its run's answers are not all in yet
	Running = "running" // taking its answers, for a moment
	Done    = "done"    // it has its answers; the combined answer goes on in Synthesis
	Failed  = "failed"  // fewer than two answers, cancelled or internal; Error says why
)

// Where the answers of a comparison came from.
const (
	SourceRun    = "run"    // a run of POST /v1/runs with compare
	SourceDirect = "direct" // POST /v1/comparisons, or the CLI's compare
)

// Errors of the Manager. The API turns them into codes.
var (
	ErrNotFound = errors.New("comparison not found")
	// ErrRunning: the comparison is waiting or running (comparison_running).
	ErrRunning = errors.New("comparison is waiting or running")
	// ErrTooMany: MaxUnfinished comparisons are waiting or running
	// (core.ErrCodeTooManyComparisons).
	ErrTooMany = errors.New("too many unfinished comparisons")
	// ErrNoJudge: no AI on this PC may write a combined answer
	// (core.ErrCodeNoJudge, the code of the judging days).
	ErrNoJudge = errors.New("no AI on this PC writes combined answers")
)

// Codes of a SpecError besides core.ErrCodeNeedTwoAnswers.
const CodeBadRequest = "bad_request"

// SpecError is a DirectSpec the Manager refuses. Code is bad_request or
// need_two_answers; TooLarge marks a text over its limit (413). Message is
// a fixed English sentence and never quotes the spec.
type SpecError struct {
	Code     string
	Message  string
	TooLarge bool
}

func (e *SpecError) Error() string { return e.Code + ": " + e.Message }

// Comparison is a snapshot of one comparison, as GET /v1/comparisons/{id}
// shows it; the API adds pollAfterMs. Every slice is [] rather than null.
// Fields may be added inside /v1, never removed (principle 4): requests,
// judgments, items and counts were the judging's and are always empty
// since C6d (#24).
type Comparison struct {
	ID     string `json:"comparisonId"`
	Status string `json:"status"`
	Source string `json:"source"`
	RunID  string `json:"runId,omitempty"`
	Engine string `json:"engine"`
	// Answers are in the turn's order, with their units; Text and Tail
	// stay in the agent.
	Answers   []judge.Answer `json:"answers"`
	Requests  []struct{}     `json:"requests"`
	Judgments []struct{}     `json:"judgments"`
	Items     []struct{}     `json:"items"`
	Counts    judge.Counts   `json:"counts"`
	Error     *judge.Error   `json:"error,omitempty"`
	// Synthesis is the combined answer (종합 답변, S5b): always set, with
	// status off when it was not asked for.
	Synthesis *Synthesis `json:"synthesis,omitempty"`
}

// RunSource is where Attach finds a run: runs.Manager.
type RunSource interface {
	Get(owner, id string) (runs.Run, bool)
}

// Options configures a Manager.
type Options struct {
	Adapters []core.Adapter
	Gate     Gate
	Runs     RunSource // nil in the CLI, which only starts direct comparisons
	Logger   *slog.Logger
	Now      func() time.Time // time.Now when nil; tests move it for expiry
	// QueueLimit and SynthLimit replace the constants when not zero, for
	// tests; so do TurnLimit and DebateLimit (debate.go).
	QueueLimit, SynthLimit time.Duration
	TurnLimit, DebateLimit time.Duration
}

// RunSpec attaches a comparison to a run of POST /v1/runs.
type RunSpec struct {
	Owner, RunID string
	Question     string   // as the person wrote it: trimmed, without core.AskPreamble
	Earlier      []string // earlier questions of a follow-up, oldest first
	// Judges are the AIs the phone listed with Earlier (the request's
	// judges, a name kept from the judging days): each got every earlier
	// question. A writer among them gets them; empty means Asked.
	Judges []string
	Asked  []string // the run's AIs in turn order
	// Synth asks for the combined answer once the answers are in (S5b);
	// nil is off. Its Tier is the run's.
	Synth *SynthSpec
	// Conversation is the conversation record of a follow-up (#29), for
	// its combined answer; nil is none. It is never changed after.
	Conversation *judge.Conversation
}

// DirectAnswer is one answer of a DirectSpec. Via is judge.ViaPC (Agent is
// an adapter, Model the model that answered) or judge.ViaHandoff (Agent is
// an official app of judge.HandoffFamily's table).
type DirectAnswer struct {
	ID, Agent, Via, Model, Text string
}

// DirectSpec is a comparison made from answers the caller gives: POST
// /v1/comparisons after the run expired, P2-4's pasted answers, or the
// CLI. Key is the caller's own id for it: the same Owner and Key give the
// first comparison again; "" never matches.
type DirectSpec struct {
	Owner, Key string
	Question   string
	Earlier    []string
	Answers    []DirectAnswer
	Judges     []string // the AIs Earlier may go to, as RunSpec's; empty means every AI that writes
	// Synth asks for the combined answer at once (S5b); nil is off.
	Synth *SynthSpec
	// Conversation is the conversation record (#29), as RunSpec's.
	Conversation *judge.Conversation
}

// Manager keeps comparisons in memory. One mutex guards every comparison;
// no adapter or run source is called while it is held.
type Manager struct {
	o          Options
	log        *slog.Logger
	queueLimit time.Duration
	synthLimit time.Duration
	perAICap   int
	base       context.Context
	cancel     context.CancelFunc
	global     chan struct{} // MaxProcesses slots
	wg         sync.WaitGroup

	mu     sync.Mutex
	closed bool
	byID   map[string]*comparison
	byRun  map[string]*comparison // by run id
	byKey  map[string]*comparison // direct ones by owner and key
	perAI  map[string]chan struct{}

	debates    map[string]*debate // by id (debate.go)
	debateKeys map[string]*debate // by owner and key
}

// comparison is one comparison. Every field but the immutable ones (id,
// owner, key, source, runID) is guarded by Manager.mu.
type comparison struct {
	id, owner, key, source, runID string

	status     string
	err        *judge.Error
	finishedAt time.Time
	// active counts what still runs for it: the waiting slot or the
	// round, the combined answer and each cleanup. It is idle at 0.
	active  int
	changed chan struct{} // closed and replaced on every change

	runSpec       *RunSpec
	run           *runs.Run // the ended run, once RunEnded had it
	direct        *DirectSpec
	directEngines []Engine // what Start saw, for its first round only

	started   bool // a round was spawned (a run's: RunEnded took it)
	cancelled bool
	cancelCh  chan struct{}

	planned bool // the round took its answers (it may still have failed with need_two_answers)
	spec    judge.Spec

	synth synthState // the combined answer (synth.go)
}

// New returns an empty Manager.
func New(o Options) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		o: o, log: o.Logger, queueLimit: o.QueueLimit, synthLimit: o.SynthLimit,
		perAICap: o.Gate.Settings.PerAIOrDefault(),
		base:     ctx, cancel: cancel, global: make(chan struct{}, MaxProcesses),
		byID: map[string]*comparison{}, byRun: map[string]*comparison{}, byKey: map[string]*comparison{},
		perAI:   map[string]chan struct{}{},
		debates: map[string]*debate{}, debateKeys: map[string]*debate{},
	}
	if m.log == nil {
		m.log = slog.New(slog.DiscardHandler)
	}
	if m.queueLimit <= 0 {
		m.queueLimit = QueueLimit
	}
	if m.synthLimit <= 0 {
		m.synthLimit = SynthLimit
	}
	return m
}

func (m *Manager) now() time.Time {
	if m.o.Now != nil {
		return m.o.Now()
	}
	return time.Now()
}

// Attach makes the comparison of a run that has just started and returns
// its id. It waits for the run to end and then starts, exactly once,
// whether runs.Manager.OnEnd (RunEnded) comes before, during or after
// Attach looks the run up. The same run again gives the same id. It
// returns ErrTooMany when MaxUnfinished comparisons are unfinished.
func (m *Manager) Attach(s RunSpec) (string, error) {
	if s.RunID == "" {
		return "", ErrNotFound
	}
	c, isNew, err := m.attachLocked(s)
	if err != nil {
		return "", err
	}
	if isNew {
		m.checkRun(c)
	}
	return c.id, nil
}

// attachLocked registers the comparison of run s.RunID, or finds it.
func (m *Manager) attachLocked(s RunSpec) (c *comparison, isNew bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	if c := m.byRun[s.RunID]; c != nil {
		if c.owner != s.Owner {
			return nil, false, ErrNotFound
		}
		return c, false, nil // a re-sent run: idempotent, even at MaxUnfinished
	}
	if m.unfinishedLocked() >= MaxUnfinished {
		return nil, false, ErrTooMany
	}
	c = m.newLocked(s.Owner, "", SourceRun, s.RunID)
	spec := s
	spec.Earlier, spec.Judges, spec.Asked = slices.Clone(s.Earlier), slices.Clone(s.Judges), slices.Clone(s.Asked)
	if s.Synth != nil {
		synth := *s.Synth
		spec.Synth = &synth
	}
	c.runSpec = &spec
	c.synth = newSynth(spec.Synth)
	c.status, c.active = Waiting, 1 // the waiting slot; the round takes it over
	m.byRun[s.RunID] = c
	return c, true, nil
}

// checkRun looks c's run up after c was registered: a run that already
// ended starts c, as RunEnded would.
func (m *Manager) checkRun(c *comparison) {
	if m.o.Runs == nil {
		return
	}
	r, ok := m.o.Runs.Get(c.owner, c.runID)
	switch {
	case !ok:
		// Nothing would ever end it: fail it, and free its slot.
		m.locked(func() {
			if c.status == Waiting && !c.started {
				m.endUnstartedLocked(c, &judge.Error{Code: core.ErrCodeInternal, Message: msgRunGone})
			}
		})
	case r.Status != runs.Running:
		m.RunEnded(r)
	}
}

// RunEnded starts the comparison of run r, which has ended; it is
// runs.Manager.OnEnd. A run still running, a run without a comparison, or
// one whose comparison already started or was cancelled, is passed over,
// so the comparison starts exactly once. It never calls the run source.
func (m *Manager) RunEnded(r runs.Run) {
	if r.Status == runs.Running {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.byRun[r.ID]
	if c == nil || c.started || c.cancelled || c.status != Waiting {
		return
	}
	run := r
	run.Parts = slices.Clone(r.Parts)
	c.run = &run
	c.started = true
	if !m.goLocked(func() { m.round(c) }) {
		m.endUnstartedLocked(c, &judge.Error{Code: core.ErrCodeCancelled, Message: msgClosed})
	}
}

// ForRun returns the id of run runID's comparison.
func (m *Manager) ForRun(owner, runID string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.byRun[runID]
	if c == nil || c.owner != owner {
		return "", false
	}
	return c.id, true
}

// Start makes a comparison from the answers in s and starts it. It
// returns a *SpecError for a spec it refuses, ErrNoJudge when no AI writes
// combined answers on this PC and ErrTooMany when MaxUnfinished
// comparisons are unfinished.
// The same Owner and Key again give the first comparison.
func (m *Manager) Start(s DirectSpec) (Comparison, error) {
	if c, ok := m.byKeySnapshot(s.Owner, s.Key); ok {
		return c, nil
	}
	if err := m.validate(s); err != nil {
		return Comparison{}, err
	}
	engines := m.Engines(m.base)
	if len(engines) == 0 {
		return Comparison{}, ErrNoJudge
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	key := directKey(s.Owner, s.Key)
	if c := m.byKey[key]; s.Key != "" && c != nil {
		return m.snapshotLocked(c), nil // a twin Start came first
	}
	if m.unfinishedLocked() >= MaxUnfinished {
		return Comparison{}, ErrTooMany
	}
	c := m.newLocked(s.Owner, s.Key, SourceDirect, "")
	d := s
	d.Question = strings.TrimSpace(s.Question)
	d.Earlier, d.Answers, d.Judges = slices.Clone(s.Earlier), slices.Clone(s.Answers), slices.Clone(s.Judges)
	if s.Synth != nil {
		synth := *s.Synth
		d.Synth = &synth
	}
	c.direct, c.directEngines = &d, engines
	c.synth = newSynth(d.Synth)
	c.status, c.started, c.active = Running, true, 1 // the round takes the answers
	if s.Key != "" {
		m.byKey[key] = c
	}
	if !m.goLocked(func() { m.round(c) }) {
		m.endUnstartedLocked(c, &judge.Error{Code: core.ErrCodeCancelled, Message: msgClosed})
	}
	return m.snapshotLocked(c), nil
}

func directKey(owner, key string) string { return owner + "\x00" + key }

func (m *Manager) byKeySnapshot(owner, key string) (Comparison, bool) {
	if key == "" {
		return Comparison{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	if c := m.byKey[directKey(owner, key)]; c != nil {
		return m.snapshotLocked(c), true
	}
	return Comparison{}, false
}

// Get returns the comparison if it exists and belongs to owner.
func (m *Manager) Get(owner, id string) (Comparison, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	c := m.byID[id]
	if c == nil || c.owner != owner {
		return Comparison{}, false
	}
	return m.snapshotLocked(c), true
}

// Retry starts over a comparison that never took its answers (cancelled
// while it waited, or its run was gone), from its run or its answers.
// Anything else has nothing to retry since the judging went (C6d): it
// returns the comparison unchanged. It returns ErrNotFound, or ErrRunning
// while the comparison or its combined answer is waiting or running.
func (m *Manager) Retry(owner, id string) (Comparison, error) {
	m.mu.Lock()
	c := m.byID[id]
	switch {
	case c == nil || c.owner != owner:
		m.mu.Unlock()
		return Comparison{}, ErrNotFound
	case c.status == Waiting || c.status == Running || synthBusy(c):
		m.mu.Unlock()
		return Comparison{}, ErrRunning
	case !c.planned && m.restartLocked(c):
		m.mu.Unlock()
		m.checkRun(c)
		return m.mustGet(c), nil
	}
	defer m.mu.Unlock()
	return m.snapshotLocked(c), nil
}

func (m *Manager) mustGet(c *comparison) Comparison {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked(c)
}

// reopenLocked makes an ended comparison run again.
func (m *Manager) reopenLocked(c *comparison) {
	c.status, c.err, c.finishedAt = Running, nil, time.Time{}
	c.cancelled, c.cancelCh = false, make(chan struct{})
}

// restartLocked starts a comparison that was never planned over again. It
// reports whether the caller must look its run up (checkRun), for a run's
// comparison that was cancelled while it waited.
func (m *Manager) restartLocked(c *comparison) (recheck bool) {
	m.reopenLocked(c)
	m.synthResetLocked(c)
	c.active++
	if c.source == SourceRun && c.run == nil {
		c.status, c.started = Waiting, false
		if m.o.Runs == nil {
			m.endUnstartedLocked(c, &judge.Error{Code: core.ErrCodeInternal, Message: msgRunGone})
			return false
		}
		m.touchLocked(c)
		return true
	}
	c.started = true
	if !m.goLocked(func() { m.round(c) }) {
		m.endUnstartedLocked(c, &judge.Error{Code: core.ErrCodeCancelled, Message: msgClosed})
	}
	m.touchLocked(c)
	return false
}

// Cancel stops a comparison: one that waits for its run ends failed with
// cancelled and never starts; one that is taking its answers ends failed
// with cancelled. Its combined answer fails with cancelled when it waits,
// and when it runs drops its wait for a slot and is not asked again, also
// when the comparison has ended. An ended comparison is otherwise left as
// it is. It returns ErrNotFound.
func (m *Manager) Cancel(owner, id string) (Comparison, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.byID[id]
	if c == nil || c.owner != owner {
		return Comparison{}, ErrNotFound
	}
	switch {
	case c.status == Waiting && !c.started:
		c.cancelled = true
		m.endUnstartedLocked(c, &judge.Error{Code: core.ErrCodeCancelled, Message: msgCancelledWaiting})
	case c.status == Waiting || c.status == Running:
		m.cancelLocked(c)
		m.cancelSynthLocked(c)
		m.touchLocked(c)
	case synthBusy(c):
		m.cancelSynthLocked(c)
		m.touchLocked(c)
	}
	return m.snapshotLocked(c), nil
}

// cancelLocked marks c cancelled and wakes what waits on its cancel.
func (m *Manager) cancelLocked(c *comparison) {
	if !c.cancelled {
		c.cancelled = true
		close(c.cancelCh)
	}
}

// WaitIdle blocks until nothing of the comparison runs any more, its
// cleanup included, or ctx ends, and returns it then. It is false when
// there is no such comparison of owner.
func (m *Manager) WaitIdle(ctx context.Context, owner, id string) (Comparison, bool) {
	for {
		var snap Comparison
		var found, idle bool
		var ch chan struct{}
		m.locked(func() {
			c := m.byID[id]
			if found = c != nil && c.owner == owner; found {
				snap, idle, ch = m.snapshotLocked(c), c.active == 0, c.changed
			}
		})
		if !found {
			return Comparison{}, false
		}
		if idle {
			return snap, true
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return snap, true
		}
	}
}

// Engines are the AIs that pass the gate now (Gate.Engine), in the
// adapters' order. An adapter whose Info panics does not pass.
func (m *Manager) Engines(ctx context.Context) []Engine {
	var out []Engine
	for _, a := range m.o.Adapters {
		func() {
			defer func() { _ = recover() }()
			if e, why := m.o.Gate.Engine(a, a.Info(ctx)); why == "" {
				out = append(out, e)
			}
		}()
	}
	return out
}

// Close stops every one-off call (their processes are ended), starts
// nothing more but the cleanups of the calls that ran, and waits up to 5
// seconds for what still runs; it is for agent shutdown.
func (m *Manager) Close() {
	m.locked(func() { m.closed = true })
	m.cancel()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	t := time.NewTimer(closeWait)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
	}
}

// goLocked runs f in a goroutine Close waits for, unless the Manager is
// closed. Callers hold mu.
func (m *Manager) goLocked(f func()) bool {
	if m.closed {
		return false
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		f()
	}()
	return true
}

// newLocked registers a new comparison. Callers hold mu.
func (m *Manager) newLocked(owner, key, source, runID string) *comparison {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	c := &comparison{
		id: hex.EncodeToString(raw), owner: owner, key: key, source: source, runID: runID,
		changed: make(chan struct{}), cancelCh: make(chan struct{}),
	}
	m.byID[c.id] = c
	return c
}

// endUnstartedLocked ends a comparison whose round never ran, freeing the
// slot it held. Callers hold mu.
func (m *Manager) endUnstartedLocked(c *comparison, err *judge.Error) {
	c.status, c.err, c.finishedAt = Failed, err, m.now()
	c.active--
	m.log.Info("comparison ended", "comparison", c.id, "status", c.status, "code", err.Code, "ms", 0)
	m.synthAfterLocked(c)
	m.touchLocked(c)
}

// touchLocked wakes everyone waiting on c. Callers hold mu.
func (m *Manager) touchLocked(c *comparison) {
	close(c.changed)
	c.changed = make(chan struct{})
}

func (m *Manager) unfinishedLocked() int {
	n := 0
	for _, c := range m.byID {
		if c.status == Waiting || c.status == Running {
			n++
		}
	}
	return n
}

// pruneLocked drops ended comparisons with nothing running KeepFor
// after they ended, then the oldest of them while more than
// MaxComparisons remain. Waiting and running ones always stay. Callers
// hold mu.
func (m *Manager) pruneLocked() {
	now := m.now()
	gone := func(c *comparison) bool { return (c.status == Done || c.status == Failed) && c.active == 0 }
	for _, c := range m.byID {
		if gone(c) && now.Sub(c.finishedAt) > KeepFor {
			m.removeLocked(c)
		}
	}
	for len(m.byID) > MaxComparisons {
		var oldest *comparison
		for _, c := range m.byID {
			if gone(c) && (oldest == nil || c.finishedAt.Before(oldest.finishedAt)) {
				oldest = c
			}
		}
		if oldest == nil {
			return
		}
		m.removeLocked(oldest)
	}
}

func (m *Manager) removeLocked(c *comparison) {
	delete(m.byID, c.id)
	if m.byRun[c.runID] == c {
		delete(m.byRun, c.runID)
	}
	if k := directKey(c.owner, c.key); c.key != "" && m.byKey[k] == c {
		delete(m.byKey, k)
	}
}

// snapshotLocked copies c for a caller. Callers hold mu.
func (m *Manager) snapshotLocked(c *comparison) Comparison {
	out := Comparison{
		ID: c.id, Status: c.status, Source: c.source, RunID: c.runID, Engine: EngineName,
		Answers:   make([]judge.Answer, 0, len(c.spec.Answers)),
		Requests:  []struct{}{},
		Judgments: []struct{}{},
		Items:     []struct{}{},
	}
	for _, a := range c.spec.Answers {
		a.Units, a.Text = []struct{}{}, "" // the phone has the text; it stays in the agent
		out.Answers = append(out.Answers, a)
	}
	if c.err != nil {
		e := *c.err
		out.Error = &e
	}
	out.Synthesis = snapshotSynthLocked(c)
	return out
}

var answerIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Fixed messages of SpecError.
const (
	specQuestion     = "the question is empty or not UTF-8 text"
	specQuestionSize = "the question is over 32 KiB"
	specEarlier      = "at most 3 earlier questions, each UTF-8 text"
	specEarlierSize  = "an earlier question is over 4 KiB"
	specTwo          = "a comparison needs at least two answers"
	specMany         = "a comparison takes at most 6 answers"
	specID           = "an answer id is not 1-32 lowercase letters, digits or dashes, or it repeats"
	specVia          = `an answer's via must be "pc" or "handoff"`
	specPCAgent      = "a pc answer must name an AI program on this PC, each once"
	specHandoff      = "a handoff answer must name an official app the agent knows (chatgpt, claude, gemini)"
	specModel        = "an answer's model is not a model name"
	specText         = "an answer's text is empty or not UTF-8 text"
	specTextSize     = "an answer's text is over 64 KiB"
	specJudges       = "judges must name AI programs on this PC, each once, at most 8"
	specSynth        = "synthesis must be {agent, off, tier} with an AI of this PC and a tier"
)

// validate checks a DirectSpec.
func (m *Manager) validate(s DirectSpec) error {
	bad := func(msg string) error { return &SpecError{Code: CodeBadRequest, Message: msg} }
	big := func(msg string) error { return &SpecError{Code: CodeBadRequest, Message: msg, TooLarge: true} }
	q := strings.TrimSpace(s.Question)
	switch {
	case len(q) > MaxQuestionBytes:
		return big(specQuestionSize)
	case q == "" || !plainText(q):
		return bad(specQuestion)
	case len(s.Earlier) > judge.MaxEarlier:
		return bad(specEarlier)
	}
	for _, e := range s.Earlier {
		if len(e) > judge.MaxEarlierBytes {
			return big(specEarlierSize)
		}
		if !plainText(e) {
			return bad(specEarlier)
		}
	}
	switch {
	case len(s.Answers) < MinAnswers:
		return &SpecError{Code: core.ErrCodeNeedTwoAnswers, Message: specTwo}
	case len(s.Answers) > MaxAnswers:
		return bad(specMany)
	}
	ids, pcs := map[string]bool{}, map[string]bool{}
	for _, a := range s.Answers {
		if !answerIDRe.MatchString(a.ID) || ids[a.ID] {
			return bad(specID)
		}
		ids[a.ID] = true
		switch a.Via {
		case judge.ViaPC:
			if m.adapter(a.Agent) == nil || pcs[a.Agent] {
				return bad(specPCAgent)
			}
			pcs[a.Agent] = true
		case judge.ViaHandoff:
			if !answerIDRe.MatchString(a.Agent) || judge.HandoffFamily(a.Agent) == "" {
				return bad(specHandoff)
			}
		default:
			return bad(specVia)
		}
		if a.Model != "" && !core.ValidAskModel(a.Model) {
			return bad(specModel)
		}
		if len(a.Text) > MaxAnswerBytes {
			return big(specTextSize)
		}
		if strings.TrimSpace(a.Text) == "" || !plainText(a.Text) {
			return bad(specText)
		}
	}
	if len(s.Judges) > MaxJudges {
		return bad(specJudges)
	}
	seen := map[string]bool{}
	for _, j := range s.Judges {
		if m.adapter(j) == nil || seen[j] {
			return bad(specJudges)
		}
		seen[j] = true
	}
	if sy := s.Synth; sy != nil && ((sy.Agent != "" && m.adapter(sy.Agent) == nil) || (sy.Tier != "" && !core.ValidTier(sy.Tier))) {
		return bad(specSynth)
	}
	return nil
}

// plainText reports whether s is UTF-8 without NUL.
func plainText(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }

// adapter is the adapter named agent, or nil.
func (m *Manager) adapter(agent string) core.Adapter {
	for _, a := range m.o.Adapters {
		if a.Agent() == agent {
			return a
		}
	}
	return nil
}
