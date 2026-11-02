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

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
	"github.com/asksemble/asksemble/agent/internal/runs"
)

// Limits and timings (spec parts 3 and 6).
const (
	// KeepFor is how long an ended comparison can still be read.
	KeepFor = 30 * time.Minute
	// MaxComparisons bounds memory; the oldest ended comparisons go first.
	MaxComparisons = 100
	// MaxUnfinished bounds the comparisons that are waiting or running.
	MaxUnfinished = 8
	// MaxProcesses bounds the judge processes of every AI together, apart
	// from the asks' runs.MaxRunning. Each AI runs at most judge.perAI.
	MaxProcesses = 6
	// QueueLimit is how long a request may wait for a judge process slot,
	// timed from the start of its round; then it fails with judge_timeout.
	QueueLimit = 5 * time.Minute
	// JudgeLimit bounds one judge call. It is a safety net only: every
	// adapter's Judge applies core.JudgeTimeout itself.
	JudgeLimit = core.JudgeTimeout + 30*time.Second
	// PollMs is the pollAfterMs of a comparison that is waiting or running.
	PollMs = 1000
	// closeWait is how long Close waits for the goroutines.
	closeWait = 5 * time.Second
)

// abandonAfter is how long a judge call may go on after JudgeLimit before
// the request stops waiting for it; a variable so tests can shorten it.
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
	Running = "running"
	Done    = "done"   // every request ended and at least one is done
	Failed  = "failed" // no request is done, or it could not be planned; Error says why
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
	// ErrNoJudge: no AI judges on this PC (core.ErrCodeNoJudge).
	ErrNoJudge = errors.New("no AI judges on this PC")
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
// There is no reason anywhere: a judge's words never leave the agent.
// Fields may be added inside /v1, never removed (principle 4). A snapshot
// shares its units, judgments and items with the Manager: read them only.
type Comparison struct {
	ID     string `json:"comparisonId"`
	Status string `json:"status"`
	Source string `json:"source"`
	RunID  string `json:"runId,omitempty"`
	Engine string `json:"engine"`
	// Answers are in the turn's order, with their units; Text and Tail
	// stay in the agent.
	Answers   []judge.Answer   `json:"answers"`
	Requests  []judge.Request  `json:"requests"`
	Judgments []judge.Judgment `json:"judgments"` // the usable ones, with their flags
	Items     []judge.Item     `json:"items"`
	Counts    judge.Counts     `json:"counts"`
	Error     *judge.Error     `json:"error,omitempty"`
	// Synthesis is the combined answer (종합 답변, S5b): always set, with
	// status off when it was not asked for. Status above stays the
	// judges'.
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
	// QueueLimit, JudgeLimit and SynthLimit replace the constants when
	// not zero, for tests.
	QueueLimit, JudgeLimit, SynthLimit time.Duration
}

// RunSpec attaches a comparison to a run of POST /v1/runs.
type RunSpec struct {
	Owner, RunID string
	Question     string   // as the person wrote it: trimmed, without core.AskPreamble
	Earlier      []string // earlier questions of a follow-up, oldest first
	// Judges are the AIs that may judge, in order of preference, already
	// cut to the asked ones (결정 5); empty means Asked.
	Judges []string
	Asked  []string // the run's AIs in turn order
	// Synth asks for the combined answer once the judges are done (S5b);
	// nil is off. Its Tier is the run's.
	Synth *SynthSpec
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
	Judges     []string // in order of preference; empty means every AI that judges
	// Synth asks for the combined answer once the judges are done (S5b);
	// nil is off.
	Synth *SynthSpec
}

// Manager keeps comparisons in memory. One mutex guards every comparison;
// no adapter, Judger or run source is called while it is held.
type Manager struct {
	o          Options
	log        *slog.Logger
	queueLimit time.Duration
	judgeLimit time.Duration
	synthLimit time.Duration
	perAICap   int
	checks     []string
	maxUnits   int
	base       context.Context
	cancel     context.CancelFunc
	global     chan struct{} // MaxProcesses slots
	wg         sync.WaitGroup
	// aggregate is judge.Aggregate; tests replace it to make it panic.
	aggregate func(judge.AggregateInput) judge.Summary

	mu     sync.Mutex
	closed bool
	byID   map[string]*comparison
	byRun  map[string]*comparison // by run id
	byKey  map[string]*comparison // direct ones by owner and key
	perAI  map[string]chan struct{}
}

// comparison is one comparison. Every field but the immutable ones (id,
// owner, key, source, runID) is guarded by Manager.mu.
type comparison struct {
	id, owner, key, source, runID string

	status     string
	err        *judge.Error
	finishedAt time.Time
	// active counts what still runs for it: the waiting slot or the
	// round's planning, each request and each cleanup. It is idle at 0.
	active  int
	changed chan struct{} // closed and replaced on every change

	runSpec       *RunSpec
	run           *runs.Run // the ended run, once RunEnded had it
	direct        *DirectSpec
	directEngines []Engine // what Start saw, for its first round only

	started   bool // a round was spawned (a run's: RunEnded took it)
	cancelled bool
	cancelCh  chan struct{}
	broken    bool // the cards could not be made: it ends failed with internal

	planned        bool // Plan made its requests
	spec           judge.Spec
	families       map[string]string // judge AI → the company it was planned with
	requests       []judge.Request
	judgments      []judge.Judgment // every judgment so far, in the order they came
	summary        judge.Summary
	lastItems      []judge.Item
	lastEndedItems []judge.Item

	synth synthState // the combined answer (synth.go)
}

// New returns an empty Manager.
func New(o Options) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		o: o, log: o.Logger, queueLimit: o.QueueLimit, judgeLimit: o.JudgeLimit, synthLimit: o.SynthLimit,
		perAICap: o.Gate.Settings.PerAIOrDefault(), checks: o.Gate.Settings.ChecksOrDefault(),
		maxUnits: o.Gate.Settings.MaxUnitsOrDefault(),
		base:     ctx, cancel: cancel, global: make(chan struct{}, MaxProcesses), aggregate: judge.Aggregate,
		byID: map[string]*comparison{}, byRun: map[string]*comparison{}, byKey: map[string]*comparison{},
		perAI: map[string]chan struct{}{},
	}
	if m.log == nil {
		m.log = slog.New(slog.DiscardHandler)
	}
	if m.queueLimit <= 0 {
		m.queueLimit = QueueLimit
	}
	if m.judgeLimit <= 0 {
		m.judgeLimit = JudgeLimit
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
// returns a *SpecError for a spec it refuses, ErrNoJudge when no AI judges
// on this PC and ErrTooMany when MaxUnfinished comparisons are unfinished.
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
	c.status, c.started, c.active = Running, true, 1 // the round's planning
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

// Retry runs the failed requests of an ended comparison again, with the
// AIs that judge now: a judge that no longer does fails at once with
// agent_unavailable, and one whose model is now of another company with
// bad_model. A comparison that was never planned (need_two_answers,
// no_judge, internal, or cancelled while it waited) starts over from its
// run or its answers. With nothing to retry it returns the comparison
// unchanged. It returns ErrNotFound, or ErrRunning while the comparison
// or its combined answer is waiting or running. A retry that runs a judge
// request again, or starts over, writes the combined answer again when
// its round ends, unless it is off.
func (m *Manager) Retry(owner, id string) (Comparison, error) {
	c, next, snap, err := m.retryBegin(owner, id)
	switch next {
	case retryCheckRun:
		m.checkRun(c)
		return m.mustGet(c), nil
	case retryRequests:
		var engines []Engine
		var mkErr error
		func() {
			defer func() {
				if v := recover(); v != nil {
					engines, mkErr = nil, errors.New("internal")
				}
			}()
			engines, mkErr = m.Engines(m.base), makeFolder(m.o.Gate.Folder)
		}()
		return m.retryRequests(c, engines, mkErr), nil
	}
	return snap, err
}

// What Retry does after retryBegin.
const (
	retryNothing  = iota // return the snapshot or the error
	retryCheckRun        // look the run up again: it may have ended
	retryRequests        // run the failed requests again
)

// retryBegin checks a Retry and does what needs no adapter.
func (m *Manager) retryBegin(owner, id string) (c *comparison, next int, snap Comparison, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c = m.byID[id]
	switch {
	case c == nil || c.owner != owner:
		return nil, retryNothing, Comparison{}, ErrNotFound
	case c.status == Waiting || c.status == Running || synthBusy(c):
		return nil, retryNothing, Comparison{}, ErrRunning
	case !c.planned:
		if m.restartLocked(c) {
			return c, retryCheckRun, Comparison{}, nil
		}
	case !slices.ContainsFunc(c.requests, retryable):
		if c.broken {
			// Every request ended but the cards could not be made: make
			// them again, and the combined answer after them.
			m.reopenLocked(c)
			m.synthResetLocked(c)
			m.endRoundLocked(c, m.newRoundLocked(c, nil))
			m.touchLocked(c)
		}
	default:
		m.reopenLocked(c)
		c.active++ // the retry's own slot until its requests are spawned
		m.touchLocked(c)
		return c, retryRequests, Comparison{}, nil
	}
	return c, retryNothing, m.snapshotLocked(c), nil
}

// retryRequests runs c's failed requests again with the engines that
// judge now.
func (m *Manager) retryRequests(c *comparison, engines []Engine, mkErr error) Comparison {
	m.mu.Lock()
	defer m.mu.Unlock()
	byAgent := map[string]Engine{}
	for _, e := range engines {
		byAgent[e.Agent] = e
	}
	rd := m.newRoundLocked(c, byAgent)
	var spawn []int
	for i := range c.requests {
		r := &c.requests[i]
		if !retryable(*r) {
			continue
		}
		r.Status, r.Error, r.Attempt, r.Ms, r.InputTokens, r.OutputTokens = judge.RequestQueued, nil, 0, 0, 0, 0
		e, ok := byAgent[r.Judge]
		switch {
		case !ok:
			r.Status, r.Error = judge.RequestFailed, failure(core.ErrCodeAgentUnavailable, "")
		case !strings.EqualFold(e.Family, c.families[r.Judge]):
			r.Status, r.Error = judge.RequestFailed, failure(core.ErrCodeBadModel, msgCompany)
		case mkErr != nil:
			r.Status, r.Error = judge.RequestFailed, failure(core.ErrCodeInternal, msgFolder)
		default:
			r.Model = e.Model
			rd.add(e.Agent)
			spawn = append(spawn, i)
		}
	}
	m.aggregateLocked(c, false) // differ only; Prev keeps the other cards' keys
	if len(spawn) > 0 {
		m.synthResetLocked(c) // written again when this round ends
	}
	m.spawnLocked(c, rd, spawn)
	c.active--
	if len(spawn) == 0 { // else the last request ends the round, also at once (R-30)
		m.endRoundLocked(c, rd)
	}
	m.touchLocked(c)
	return m.snapshotLocked(c)
}

// mustGet is Get for a comparison Retry holds, which prune cannot remove
// while it waits.
func (m *Manager) mustGet(c *comparison) Comparison {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked(c)
}

// retryable reports whether Retry runs request r again: it failed, and a
// judge was planned for it (a no_judge record has none).
func retryable(r judge.Request) bool { return r.Status == judge.RequestFailed && r.Judge != "" }

// reopenLocked makes an ended comparison run again.
func (m *Manager) reopenLocked(c *comparison) {
	c.status, c.err, c.finishedAt = Running, nil, time.Time{}
	c.cancelled, c.cancelCh, c.broken = false, make(chan struct{}), false
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
// cancelled and never starts; a running one drops its queued requests
// (failed, cancelled) and asks no judge again, while the requests that run
// finish. Its combined answer fails with cancelled when it waits, and when
// it runs drops its wait for a slot and is not asked again, also when the
// comparison has ended. An ended comparison is otherwise left as it is.
// It returns ErrNotFound.
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

// cancelLocked marks c cancelled and wakes its queued requests.
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

// Engines are the AIs that judge now, in the adapters' order. An adapter
// whose Info or Family panics does not judge.
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

// Close stops every judge (their processes are ended), starts nothing
// more but the cleanups of the judges that ran, and waits up to 5 seconds
// for what still runs; it is for agent shutdown.
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
	m.log.Info("comparison ended", "comparison", c.id, "status", c.status, "code", err.Code,
		"differ", 0, "check", 0, "agree", 0, "onlyOne", 0, "notCompared", 0, "notJudged", 0, "ms", 0)
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

// pruneLocked drops ended comparisons with nothing running 30 minutes
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
		Requests:  make([]judge.Request, 0, len(c.requests)),
		Judgments: slices.Clone(c.summary.Judgments),
		Items:     slices.Clone(c.summary.Items),
		Counts:    c.summary.Counts,
	}
	for _, a := range c.spec.Answers {
		a.Units = slices.Clone(a.Units)
		if a.Units == nil {
			a.Units = []judge.Unit{}
		}
		a.Text, a.Tail = "", nil // the phone has the text; it stays in the agent
		out.Answers = append(out.Answers, a)
	}
	for _, r := range c.requests {
		if r.Error != nil {
			e := *r.Error
			r.Error = &e
		}
		out.Requests = append(out.Requests, r)
	}
	if out.Judgments == nil {
		out.Judgments = []judge.Judgment{}
	}
	if out.Items == nil {
		out.Items = []judge.Item{}
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
