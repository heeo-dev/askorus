// Package runs carries out POST /v1/runs: one prompt sent to several AIs
// at once (mode ask, Phase 2). Like jobs, the phone gets a run id at once
// and then asks for it; each AI's answer shows as soon as that AI is done,
// so a slow AI does not hold back the others. Runs live in memory only:
// the phone keeps the history (P2-6).
package runs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// State is where a run or one of its parts is.
type State string

const (
	Running State = "running"
	Done    State = "done"
	Failed  State = "failed"
)

const (
	// PartLimit caps one AI's answer. Asks with web search took up to 45 s
	// in P0-7; deep tiers on long questions take longer.
	PartLimit = 3 * time.Minute
	// KeepFor is how long a finished run can still be read. Longer than
	// jobs: a person may switch apps or turn the screen off while the AIs
	// answer, and the phone asks nothing until it is back (R-80, 11-07:
	// 30 minutes lost a run the phone came back to after 65; the user
	// chose a day). maxRuns still bounds memory.
	KeepFor = 24 * time.Hour
	// MaxRunning bounds the AI processes the PC runs for the phone.
	MaxRunning = 4
	// maxRuns bounds memory; the oldest finished runs go first.
	maxRuns = 100
)

// ErrTooMany is returned when MaxRunning runs are still running.
var ErrTooMany = errors.New("too many runs at once")

// ErrSessionBusy is returned when a task carries on a session that a part
// of another run still answers in: two follow-ups at once would split the
// conversation (Phase 1 rule: never resume a session that still runs).
var ErrSessionBusy = errors.New("that conversation is still answering")

// Error is a failed part's code and message. The message is the AI's own
// error text, for the person; it is never logged.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

// Part is one AI's share of a run. Fields may be added inside /v1, never
// removed (principle 4).
type Part struct {
	Agent  string `json:"agent"`
	Status State  `json:"status"`
	// Session is "<agent>:<id>"; a follow-up names it to carry the
	// conversation on (follow-up method A).
	Session      string `json:"session,omitempty"`
	Model        string `json:"model,omitempty"`
	Answer       string `json:"answer,omitempty"`
	Ms           int64  `json:"ms,omitempty"`
	InputTokens  int    `json:"inputTokens,omitempty"`
	OutputTokens int    `json:"outputTokens,omitempty"`
	Error        *Error `json:"error,omitempty"`
}

// Run is a snapshot of one run.
type Run struct {
	ID   string `json:"runId"`
	Mode string `json:"mode"`
	Tier string `json:"tier,omitempty"` // ask tier
	// Status is running until every part has ended, then done, or failed
	// when no part has an answer.
	Status     State     `json:"status"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
	Parts      []Part    `json:"parts"`
	// Refine is the question's refining (질문 정제, milestone C): set when
	// the run asked for it, running until the moderator's spec is in.
	Refine *Refine `json:"refine,omitempty"`

	owner, key string
	left       int // parts still running
}

// Spec describes a new run. Key is the phone's own id for the run: the
// same owner and key again returns the first run instead of asking the
// AIs twice (a retried request).
type Spec struct {
	Owner, Key string
	Mode, Tier string
	Tasks      []Task
}

// Task is the work of one part. Session is the "<agent>:<id>" it carries
// on, or "" for a new conversation.
type Task struct {
	Agent   string
	Session string
	Ask     func(ctx context.Context) (core.AskResult, error)
}

// Manager keeps runs in memory. Runs do not survive an agent restart.
type Manager struct {
	Now       func() time.Time
	PartLimit time.Duration // PartLimit when zero; tests shorten it
	// OnPart is told when a part ends, for the agent log. It gets no
	// prompt or answer text.
	OnPart func(mode string, p Part)
	// OnEnd is told once when a run has ended, with its final snapshot:
	// after the last part's OnPart, from that part's goroutine, without
	// the lock held (a run without parts gets it from a goroutine of
	// Start). The comparisons start there (P2-3). A panic in it is
	// recovered. Set it before the first Start.
	OnEnd func(r Run)

	base   context.Context
	cancel context.CancelFunc

	mu    sync.Mutex
	runs  map[string]*Run
	inUse map[string]bool // sessions a running part carries on
}

// NewManager returns an empty manager.
func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{base: ctx, cancel: cancel, runs: map[string]*Run{}, inUse: map[string]bool{}}
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Start runs every task at the same time and returns the run.
func (m *Manager) Start(sp Spec) (Run, error) {
	run, ended, err := m.start(sp)
	if ended {
		// A run without parts has ended already; OnEnd still comes, as
		// for any other run, once Start has let the lock go.
		go m.ended(run)
	}
	return run, err
}

// start is Start under the lock. ended reports a new run that has ended
// at once, because it has no parts.
func (m *Manager) start(sp Spec) (run Run, ended bool, err error) {
	owner, key, tasks := sp.Owner, sp.Key, sp.Tasks
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	if key != "" {
		for _, r := range m.runs {
			if r.owner == owner && r.key == key {
				return r.snapshot(), false, nil
			}
		}
	}
	running := 0
	for _, r := range m.runs {
		if r.Status == Running {
			running++
		}
	}
	if running >= MaxRunning {
		return Run{}, false, ErrTooMany
	}
	for _, t := range tasks {
		if t.Session != "" && m.inUse[t.Session] {
			return Run{}, false, ErrSessionBusy
		}
	}
	for _, t := range tasks {
		if t.Session != "" {
			m.inUse[t.Session] = true
		}
	}
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	r := &Run{
		ID: hex.EncodeToString(raw), Mode: sp.Mode, Tier: sp.Tier, Status: Running, StartedAt: m.now().UTC(),
		Parts: make([]Part, len(tasks)), owner: owner, key: key, left: len(tasks),
	}
	for i, t := range tasks {
		r.Parts[i] = Part{Agent: t.Agent, Status: Running}
	}
	if len(tasks) == 0 {
		r.finish(m.now())
	}
	m.runs[r.ID] = r
	for i, t := range tasks {
		go m.runPart(r, i, t)
	}
	return r.snapshot(), len(tasks) == 0, nil
}

func (m *Manager) runPart(r *Run, i int, t Task) {
	limit := m.PartLimit
	if limit == 0 {
		limit = PartLimit
	}
	ctx, cancel := context.WithTimeout(m.base, limit)
	defer cancel()
	start := m.now()
	res, err := safeAsk(ctx, t.Ask)
	p := Part{Agent: t.Agent, Ms: m.now().Sub(start).Milliseconds()}
	switch {
	case err == nil && res.Answer == "":
		p.Status, p.Error = Failed, &Error{Code: core.ErrCodeAskEmpty}
	case err == nil:
		p.Status, p.Answer, p.Model = Done, res.Answer, res.Model
		p.InputTokens, p.OutputTokens = res.InputTokens, res.OutputTokens
	case ctx.Err() == context.DeadlineExceeded:
		p.Status, p.Error = Failed, &Error{Code: core.ErrCodeAskTimeout}
	default:
		p.Status, p.Error = Failed, partError(err)
	}
	if res.SessionID != "" {
		// Kept on failure too: the conversation may hold a partial answer.
		p.Session = core.MakeID(t.Agent, res.SessionID)
	}
	if p.Model == "" {
		p.Model = res.Model
	}
	m.mu.Lock()
	if t.Session != "" {
		delete(m.inUse, t.Session)
	}
	r.Parts[i] = p
	r.left--
	last := r.left == 0
	if last {
		r.finish(m.now())
	}
	final := r.snapshot()
	m.mu.Unlock()
	if m.OnPart != nil {
		func() {
			defer func() { _ = recover() }() // a log hook must not cost the run its OnEnd
			m.OnPart(r.Mode, p)
		}()
	}
	if last {
		// Only the part that ended the run gets here: left reached 0 once,
		// under the lock.
		m.ended(final)
	}
}

// ended tells OnEnd that run r has ended. A panicking OnEnd cannot take
// the agent down or keep the next run from being told.
func (m *Manager) ended(r Run) {
	if m.OnEnd == nil {
		return
	}
	defer func() { _ = recover() }()
	m.OnEnd(r)
}

// safeAsk keeps a panicking adapter from taking the agent down.
func safeAsk(ctx context.Context, ask func(context.Context) (core.AskResult, error)) (res core.AskResult, err error) {
	defer func() {
		if v := recover(); v != nil {
			res, err = core.AskResult{}, errors.New("internal error")
		}
	}()
	return ask(ctx)
}

func partError(err error) *Error {
	var ae *core.ActionError
	if errors.As(err, &ae) {
		return &Error{Code: ae.Code, Message: ae.Detail}
	}
	return &Error{Code: "internal"}
}

// finish marks the run ended. Callers hold mu.
func (r *Run) finish(now time.Time) {
	r.FinishedAt = now.UTC()
	r.Status = Failed
	for _, p := range r.Parts {
		if p.Status == Done {
			r.Status = Done
		}
	}
}

func (r *Run) snapshot() Run {
	c := *r
	c.Parts = append([]Part(nil), r.Parts...)
	return c
}

// Refine is a run's question refining as its snapshot shows it
// (compares.Refined; /v1 fields may be added, never removed). Spec and
// Template are set when done; Template is "" for a kind without one.
type Refine struct {
	Status   string              `json:"status"`
	Agent    string              `json:"agent,omitempty"`
	Model    string              `json:"model,omitempty"`
	Ms       int64               `json:"ms,omitempty"`
	Spec     *judge.QuestionSpec `json:"spec,omitempty"`
	Template string              `json:"template,omitempty"`
	Error    *judge.Error        `json:"error,omitempty"`
}

// SetRefine sets the run's refining. A run that is gone or not owner's is
// left alone. Each call keeps its own copy, so snapshots never share it.
func (m *Manager) SetRefine(owner, id string, rf Refine) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.runs[id]; ok && r.owner == owner {
		c := rf
		if rf.Spec != nil {
			spec := *rf.Spec
			c.Spec = &spec
		}
		r.Refine = &c
	}
}

// Get returns the run if it exists and belongs to owner.
func (m *Manager) Get(owner, id string) (Run, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	r, ok := m.runs[id]
	if !ok || r.owner != owner {
		return Run{}, false
	}
	return r.snapshot(), true
}

// Wait blocks until the run has ended or ctx ends, for tests and the CLI.
func (m *Manager) Wait(ctx context.Context, owner, id string) (Run, bool) {
	for {
		r, ok := m.Get(owner, id)
		if !ok || r.Status != Running {
			return r, ok
		}
		select {
		case <-ctx.Done():
			return r, true
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// Close stops every running part (their AI processes are ended) and
// refuses nothing afterwards; it is for agent shutdown.
func (m *Manager) Close() { m.cancel() }

// prune drops old finished runs. Callers hold mu.
func (m *Manager) prune() {
	now := m.now()
	for id, r := range m.runs {
		if r.Status != Running && now.Sub(r.FinishedAt) > KeepFor {
			delete(m.runs, id)
		}
	}
	for len(m.runs) > maxRuns {
		var oldest *Run
		for _, r := range m.runs {
			if r.Status != Running && (oldest == nil || r.FinishedAt.Before(oldest.FinishedAt)) {
				oldest = r
			}
		}
		if oldest == nil {
			return
		}
		delete(m.runs, oldest.ID)
	}
}
