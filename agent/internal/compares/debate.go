package compares

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
	"github.com/asksemble/asksemble/agent/internal/runs"
)

// Debates (토론, P2-5, #26). After a combined answer, the AIs that answered
// on this PC debate the points where their answers differ, one at a time
// in a chat the moderator leads (way 2), and the moderator writes the
// answer after the debate. The moderator plans (the points, their nature
// and sides, the first turns), checks each debated point after its turns
// (one more turn, or closed with an outcome and a line) and writes the
// answer: never who is right. Its calls are the combined answer's
// locked-down one-off command (core.Synthesizer, its {"answer"} schema)
// with the prompts of internal/judge/debate.go. A debater's turn is the
// ask command (core.Asker: web search and reading only, the prompt on
// stdin, a new session every time) run in the judge folder, so the turns
// do not pile up in the session list as ask conversations. What another AI
// wrote reaches an AI only inside a nonce data block.
//
// The debate lives in memory like a comparison: one at a time on a PC,
// read by its device only, 30 minutes after it ended, never past an agent
// restart; the phone keeps it with its turn. Logs carry codes and counts
// only, never a question, an answer, a turn or a nonce.

// Debate statuses.
const (
	DebatePlanning = "planning" // the moderator picks the points
	DebateDebating = "debating" // the AIs speak and the moderator checks
	DebateWriting  = "writing"  // the moderator writes the answer after the debate
	// DebateDone: ended, with the answer after the debate, or without one
	// when no point was worth a debate (the combined answer stands).
	DebateDone = "done"
	// DebateStopped: the person stopped it; what was said stays and no
	// answer after the debate is written.
	DebateStopped = "stopped"
	DebateFailed  = "failed" // Error says why; what was said stays
)

// Turn statuses.
const (
	TurnRunning = "running"
	TurnDone    = "done"
	TurnFailed  = "failed" // Error says why; the debate goes on
)

// Debate limits.
const (
	// MaxDebates bounds the debates kept in memory; the oldest ended ones
	// go first.
	MaxDebates = 20
	// MaxDebateTurns bounds a point's turns: the plan's and one more.
	MaxDebateTurns = judge.MaxPlannedTurns + 1
	// MaxCombinedBytes bounds the combined answer a debate starts from.
	MaxCombinedBytes = judge.MaxSynthTextBytes
	// DebateLimit bounds a whole debate (a safety net: C7's median was
	// 146 s); past it the debate fails with judge_timeout.
	DebateLimit = 15 * time.Minute
)

// ErrDebateBusy: a debate is planning, debating or writing on this PC
// (too_many_debates); there is one at a time.
var ErrDebateBusy = errors.New("a debate is running on this PC")

// ErrDebateNotFound: no such debate of this device.
var ErrDebateNotFound = errors.New("debate not found")

// Debate is a snapshot of one debate, as GET /v1/debates/{id} shows it; the
// API adds pollAfterMs. AIs are named by their agent codes (claude, codex,
// opencode, or an official app's: chatgpt…); the phone shows their names.
// Every slice is [] rather than null. Fields may be added inside /v1, never
// removed (principle 4).
type Debate struct {
	ID     string `json:"debateId"`
	Status string `json:"status"`
	// Moderator is the AI the person asked for until one is picked, then
	// the AI that moderates; Model is the model it moderates with.
	Moderator string        `json:"moderator,omitempty"`
	Model     string        `json:"model,omitempty"`
	Tier      string        `json:"tier"`
	Points    []DebatePoint `json:"points"`
	After     *DebateAfter  `json:"after,omitempty"`
	Calls     int           `json:"calls"` // AI calls so far
	Ms        int64         `json:"ms"`    // since it started, until it ended
	Error     *judge.Error  `json:"error,omitempty"`
}

// DebatePoint is one point where the answers differ. A point that is not
// debated has no turns and no result.
type DebatePoint struct {
	ID     string       `json:"id"`
	Topic  string       `json:"topic"`
	Nature string       `json:"nature"` // fact, reasoning, judgment or taste
	Debate bool         `json:"debate"`
	Goal   string       `json:"goal,omitempty"`
	Sides  []DebateSide `json:"sides"`
	Turns  []DebateTurn `json:"turns"`
	// Result is agreed, conditional, differ or kept once the moderator
	// closed the point, with Line for the person; both are empty when the
	// moderator could not close it.
	Result string `json:"result,omitempty"`
	Line   string `json:"line,omitempty"`
	// Checking: the moderator is checking this point now.
	Checking bool `json:"checking,omitempty"`
}

// DebateSide is one side of a point: its AIs and what it says.
type DebateSide struct {
	AIs  []string `json:"ais"`
	Says string   `json:"says"`
}

// DebateTurn is one debater's turn: the moderator's question and the
// reply. Ms runs on while the turn runs.
type DebateTurn struct {
	AI     string       `json:"ai"`
	Ask    string       `json:"ask"`
	Status string       `json:"status"` // running, done or failed
	Text   string       `json:"text,omitempty"`
	Stance string       `json:"stance,omitempty"` // keep, change or partly; "" when it gave none
	Model  string       `json:"model,omitempty"`
	Ms     int64        `json:"ms"`
	Error  *judge.Error `json:"error,omitempty"`

	started time.Time
}

// DebateAfter is the answer after the debate (토론 뒤 정리), in Markdown.
type DebateAfter struct {
	Text  string `json:"text"`
	Agent string `json:"agent"`
	Model string `json:"model,omitempty"`
	Ms    int64  `json:"ms"`
}

// DebateAnswer is one answer a debate starts from. Via is judge.ViaPC
// (Agent is an adapter, which speaks in the debate) or judge.ViaHandoff
// (Agent is an official app: its answer has a side but never speaks).
type DebateAnswer struct {
	Agent, Via, Model, Text string
}

// DebateSpec starts a debate (POST /v1/debates): the answers and the
// combined answer come in the request, as the phone keeps them. Key is
// the caller's own id for it: the same Owner and Key give the first
// debate again; "" never matches.
type DebateSpec struct {
	Owner, Key string
	Question   string
	Earlier    []string
	// Judges are the AIs Earlier may go to, as a comparison's: the
	// moderator gets Earlier only when it is listed.
	Judges   []string
	Answers  []DebateAnswer
	Combined string // the combined answer before the debate
	// Moderator is the AI the person picked; "" lets the agent pick. Only
	// an AI that answered on this PC moderates.
	Moderator string
	Tier      string // the ask tier of the moderator and the debaters; "" is standard
}

// debate is one debate. Every field but the immutable ones (id, owner,
// key, spec, ctx, cancel) is guarded by Manager.mu.
type debate struct {
	id, owner, key string
	spec           DebateSpec
	ctx            context.Context
	cancel         context.CancelFunc
	stopCh         chan struct{} // closed by Stop; a slot wait ends on it

	status     string
	err        *judge.Error
	moderator  string
	model      string
	points     []DebatePoint
	after      *DebateAfter
	calls      int
	start      time.Time
	ms         int64
	finishedAt time.Time
	active     int // the goroutine; idle at 0
	changed    chan struct{}
}

// ended reports whether d has ended. Callers hold mu.
func (d *debate) ended() bool {
	return d.status == DebateDone || d.status == DebateStopped || d.status == DebateFailed
}

// Fixed messages of debate errors.
var debateMessages = map[string]string{
	core.ErrCodeJudgeFailed:      "the moderator reported an error",
	core.ErrCodeJudgeTimeout:     "the moderator gave no reply within its time limit",
	core.ErrCodeJudgeBadOutput:   "the moderator's reply was not in the debate's format, also when asked again",
	core.ErrCodeJudgeUsedTool:    "the moderator reached for a tool, so its reply was thrown away",
	core.ErrCodeBadModel:         "the moderator does not know its model",
	core.ErrCodeAgentUnavailable: "the moderator does not run on this PC any more",
	core.ErrCodeNoJudge:          "no AI that answered on this PC may moderate the debate",
	core.ErrCodeCancelled:        "the debate was stopped",
	core.ErrCodeInternal:         "the agent could not run the debate",
	core.ErrCodeAskTimeout:       "the AI gave no reply within its time limit",
	core.ErrCodeAskFailed:        "the AI reported an error",
	core.ErrCodeAskEmpty:         "the AI ended without a reply",
}

const (
	msgDebateOther   = "the debate failed"
	msgDebateQueue   = "the debate waited over 5 minutes for a free process"
	msgDebateClosed  = "the agent is stopping"
	msgDebateLimit   = "the debate went over its time limit"
	msgDebateCompany = "the model that moderated is not of the company the moderator was picked for"
)

// debateFailure is a debate error with code and message, or the fixed
// message of code when message is "".
func debateFailure(code, message string) *judge.Error {
	if message == "" {
		message = debateMessages[code]
	}
	if message == "" {
		message = msgDebateOther
	}
	return &judge.Error{Code: code, Message: message}
}

// Fixed messages of a DebateSpec's SpecError, beside the comparison's.
const (
	specDebateAnswer    = "an answer must be an AI program on this PC (via pc) or an official app the agent knows (via handoff), each AI once"
	specDebateCombined  = "synthesis (the combined answer) is empty or not UTF-8 text"
	specDebateCombSize  = "synthesis (the combined answer) is over 64 KiB"
	specDebateModerator = "moderator must name an AI program on this PC"
	specDebateTier      = "tier must be quick, standard or deep"
)

// validateDebate checks a DebateSpec.
func (m *Manager) validateDebate(s DebateSpec) error {
	bad := func(msg string) error { return &SpecError{Code: CodeBadRequest, Message: msg} }
	big := func(msg string) error { return &SpecError{Code: CodeBadRequest, Message: msg, TooLarge: true} }
	// The question, the earlier questions and the judges as a comparison's.
	answers := make([]DirectAnswer, len(s.Answers))
	for i, a := range s.Answers {
		answers[i] = DirectAnswer{ID: debateAnswerID(i), Agent: a.Agent, Via: a.Via, Model: a.Model, Text: a.Text}
	}
	if err := m.validate(DirectSpec{Question: s.Question, Earlier: s.Earlier, Answers: answers, Judges: s.Judges}); err != nil {
		var se *SpecError
		if errors.As(err, &se) && (se.Message == specPCAgent || se.Message == specHandoff) {
			return bad(specDebateAnswer)
		}
		return err
	}
	seen := map[string]bool{}
	for _, a := range s.Answers {
		code := strings.ToLower(a.Agent)
		if seen[code] {
			return bad(specDebateAnswer)
		}
		seen[code] = true
	}
	c := strings.TrimSpace(s.Combined)
	switch {
	case len(c) > MaxCombinedBytes:
		return big(specDebateCombSize)
	case c == "" || !plainText(c):
		return bad(specDebateCombined)
	case s.Moderator != "" && m.adapter(s.Moderator) == nil:
		return bad(specDebateModerator)
	case s.Tier != "" && !core.ValidTier(s.Tier):
		return bad(specDebateTier)
	}
	return nil
}

// StartDebate starts a debate from the answers in s. It returns a
// *SpecError for a spec it refuses and ErrDebateBusy while another debate
// of any device runs. The same Owner and Key again give the first debate.
func (m *Manager) StartDebate(s DebateSpec) (Debate, error) {
	if err := m.validateDebate(s); err != nil {
		return Debate{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneDebatesLocked()
	if d := m.debateKeys[directKey(s.Owner, s.Key)]; s.Key != "" && d != nil {
		return m.debateSnapshotLocked(d), nil
	}
	for _, d := range m.debates {
		if !d.ended() {
			return Debate{}, ErrDebateBusy
		}
	}
	spec := s
	spec.Earlier, spec.Judges, spec.Answers = slices.Clone(s.Earlier), slices.Clone(s.Judges), slices.Clone(s.Answers)
	spec.Question, spec.Combined = strings.TrimSpace(s.Question), strings.TrimSpace(s.Combined)
	if !core.ValidTier(spec.Tier) {
		spec.Tier = core.TierStandard
	}
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	limit := DebateLimit
	if m.o.DebateLimit > 0 {
		limit = m.o.DebateLimit
	}
	ctx, cancel := context.WithTimeout(m.base, limit)
	d := &debate{
		id: hex.EncodeToString(raw), owner: s.Owner, key: s.Key, spec: spec, ctx: ctx, cancel: cancel,
		stopCh: make(chan struct{}), status: DebatePlanning, moderator: spec.Moderator,
		points: []DebatePoint{}, start: time.Now(), active: 1, changed: make(chan struct{}),
	}
	m.debates[d.id] = d
	if s.Key != "" {
		m.debateKeys[directKey(s.Owner, s.Key)] = d
	}
	if !m.goLocked(func() { m.debateRun(d) }) {
		d.active = 0
		m.debateEndLocked(d, DebateFailed, debateFailure(core.ErrCodeCancelled, msgDebateClosed))
		cancel()
	}
	return m.debateSnapshotLocked(d), nil
}

// GetDebate returns the debate if it exists and belongs to owner.
func (m *Manager) GetDebate(owner, id string) (Debate, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneDebatesLocked()
	d := m.debates[id]
	if d == nil || d.owner != owner {
		return Debate{}, false
	}
	return m.debateSnapshotLocked(d), true
}

// StopDebate stops a debate that runs: it ends stopped at once, a turn or
// moderator call that runs is ended (its process too) and fails with
// cancelled, and no answer after the debate is written. An ended debate
// comes back as it is. It returns ErrDebateNotFound.
func (m *Manager) StopDebate(owner, id string) (Debate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.debates[id]
	if d == nil || d.owner != owner {
		return Debate{}, ErrDebateNotFound
	}
	if !d.ended() {
		m.debateEndLocked(d, DebateStopped, nil)
	}
	return m.debateSnapshotLocked(d), nil
}

// WaitDebate blocks until nothing of the debate runs any more, or ctx
// ends, and returns it then; it is for tests. It is false when there is no
// such debate of owner.
func (m *Manager) WaitDebate(ctx context.Context, owner, id string) (Debate, bool) {
	for {
		var snap Debate
		var found, idle bool
		var ch chan struct{}
		m.locked(func() {
			d := m.debates[id]
			if found = d != nil && d.owner == owner; found {
				snap, idle, ch = m.debateSnapshotLocked(d), d.active == 0, d.changed
			}
		})
		if !found {
			return Debate{}, false
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

// debateEndLocked ends d with status and err (nil for done and stopped),
// ends what still runs of it and logs it. Callers hold mu.
func (m *Manager) debateEndLocked(d *debate, status string, err *judge.Error) {
	d.status, d.err, d.finishedAt = status, err, m.now()
	d.ms = time.Since(d.start).Milliseconds()
	for i := range d.points {
		d.points[i].Checking = false
		for j := range d.points[i].Turns {
			t := &d.points[i].Turns[j]
			if t.Status == TurnRunning {
				t.Status, t.Error, t.Ms = TurnFailed, debateFailure(core.ErrCodeCancelled, ""), time.Since(t.started).Milliseconds()
			}
		}
	}
	select {
	case <-d.stopCh:
	default:
		close(d.stopCh)
	}
	d.cancel()
	code := ""
	if err != nil {
		code = err.Code
	}
	debated, turns := 0, 0
	for _, p := range d.points {
		if p.Debate {
			debated++
		}
		turns += len(p.Turns)
	}
	m.log.Info("debate ended", "debate", d.id, "status", status, "code", code, "points", len(d.points),
		"debated", debated, "turns", turns, "calls", d.calls, "ms", d.ms)
	m.touchDebateLocked(d)
}

// touchDebateLocked wakes everyone waiting on d. Callers hold mu.
func (m *Manager) touchDebateLocked(d *debate) {
	close(d.changed)
	d.changed = make(chan struct{})
}

// update runs f on d under the lock while d has not ended, and reports
// whether it did: a debate that ended (stopped, the agent closing, its
// limit) is never changed by its goroutine again.
func (m *Manager) update(d *debate, f func()) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d.ended() {
		return false
	}
	f()
	m.touchDebateLocked(d)
	return true
}

// pruneDebatesLocked drops ended debates 30 minutes after they ended, then
// the oldest of them while more than MaxDebates remain. Callers hold mu.
func (m *Manager) pruneDebatesLocked() {
	now := m.now()
	gone := func(d *debate) bool { return d.ended() && d.active == 0 }
	drop := func(d *debate) {
		delete(m.debates, d.id)
		if k := directKey(d.owner, d.key); d.key != "" && m.debateKeys[k] == d {
			delete(m.debateKeys, k)
		}
	}
	for _, d := range m.debates {
		if gone(d) && now.Sub(d.finishedAt) > KeepFor {
			drop(d)
		}
	}
	for len(m.debates) > MaxDebates {
		var oldest *debate
		for _, d := range m.debates {
			if gone(d) && (oldest == nil || d.finishedAt.Before(oldest.finishedAt)) {
				oldest = d
			}
		}
		if oldest == nil {
			return
		}
		drop(oldest)
	}
}

// debateSnapshotLocked copies d for a caller. Callers hold mu.
func (m *Manager) debateSnapshotLocked(d *debate) Debate {
	out := Debate{
		ID: d.id, Status: d.status, Moderator: d.moderator, Model: d.model, Tier: d.spec.Tier,
		Points: make([]DebatePoint, len(d.points)), Calls: d.calls, Ms: d.ms,
	}
	if !d.ended() {
		out.Ms = time.Since(d.start).Milliseconds()
	}
	for i, p := range d.points {
		p.Sides = slices.Clone(p.Sides)
		for j := range p.Sides {
			p.Sides[j].AIs = slices.Clone(p.Sides[j].AIs)
		}
		p.Turns = slices.Clone(p.Turns)
		for j := range p.Turns {
			if p.Turns[j].Status == TurnRunning {
				p.Turns[j].Ms = time.Since(p.Turns[j].started).Milliseconds()
			}
			if e := p.Turns[j].Error; e != nil {
				c := *e
				p.Turns[j].Error = &c
			}
		}
		out.Points[i] = p
	}
	if d.after != nil {
		a := *d.after
		out.After = &a
	}
	if d.err != nil {
		e := *d.err
		out.Error = &e
	}
	return out
}

// ---- The run ----

// debateAI is one AI of a debate: its agent code, the name the prompts
// give it, its answer as the prompts carry it, and whether it speaks.
type debateAI struct {
	agent, name, own string
	speaks           bool
}

// debateCast is a debate's AIs and the maps between their names and codes.
type debateCast struct {
	ais    []debateAI
	byName map[string]debateAI // by the name the prompts give
}

func (c debateCast) names(speakersOnly bool) []string {
	var out []string
	for _, a := range c.ais {
		if a.speaks || !speakersOnly {
			out = append(out, a.name)
		}
	}
	return out
}

// codes turns the prompts' names into agent codes.
func (c debateCast) codes(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if a, ok := c.byName[n]; ok {
			out = append(out, a.agent)
		}
	}
	return out
}

// cast is a debate's AIs, its task (the prompts' names, answers cut as a
// combined answer's) and the AIs that may moderate, in the answers'
// order: the PC answers' AIs.
func (m *Manager) cast(s DebateSpec) (debateCast, judge.DebateTask, []string) {
	spec := judge.Spec{Question: s.Question, Earlier: slices.Clone(s.Earlier)}
	for i, a := range s.Answers {
		spec.Answers = append(spec.Answers, judge.Answer{ID: debateAnswerID(i), Agent: a.Agent, Via: a.Via, Status: judge.AnswerReady, Text: a.Text})
	}
	names := m.answerNames(spec.Answers)
	used := map[string]bool{}
	for _, a := range spec.Answers {
		n := names[a.ID]
		if used[strings.ToLower(n)] {
			n = a.Agent // two AIs with one name: the later one goes by its code
		}
		used[strings.ToLower(n)] = true
		names[a.ID] = n
	}
	task := judge.BuildSynthTask(spec, func(id string) string { return names[id] })
	cast := debateCast{byName: map[string]debateAI{}}
	var pcs []string
	for i, a := range s.Answers {
		ai := debateAI{agent: a.Agent, name: names[debateAnswerID(i)], speaks: a.Via == judge.ViaPC}
		for _, t := range task.Answers {
			if t.AI == ai.name {
				ai.own = t.Text
			}
		}
		if ai.speaks {
			if _, ok := m.adapter(a.Agent).(core.Asker); !ok {
				ai.speaks = false
			}
			pcs = append(pcs, a.Agent)
		}
		cast.ais = append(cast.ais, ai)
		cast.byName[ai.name] = ai
	}
	return cast, judge.DebateTask{SynthTask: task, Combined: s.Combined}, pcs
}

func debateAnswerID(i int) string { return "a" + strings.Repeat("x", i) }

// debateRun runs debate d to its end, in its own goroutine: the plan, each
// debated point's turns and checks, and the answer after the debate.
// Whatever happens, a panic too, the debate ends and the moderator's
// leftovers are cleaned up.
func (m *Manager) debateRun(d *debate) {
	var w Writer
	var refs []string
	defer func() {
		if v := recover(); v != nil {
			m.update(d, func() { m.debateEndLocked(d, DebateFailed, debateFailure(core.ErrCodeInternal, "")) })
		}
		if w.Cleaner != nil && len(refs) > 0 {
			func() {
				defer func() { _ = recover() }()
				m.cleanupRefs(Engine{Agent: w.Agent, Cleaner: w.Cleaner}, refs)
			}()
		}
		m.locked(func() {
			d.active--
			d.cancel()
			m.touchDebateLocked(d)
		})
	}()
	fail := func(f *judge.Error) {
		if d.ctx.Err() != nil {
			m.ctxEnd(d) // stopped, closing or over its limit: f is only its echo
			return
		}
		m.update(d, func() { m.debateEndLocked(d, DebateFailed, f) })
	}
	spec := d.spec
	cast, task, pcs := m.cast(spec)
	var ok bool
	w, ok = pickWriter(m.Writers(d.ctx, spec.Tier), synthInput{spec: SynthSpec{Agent: spec.Moderator}, allowed: pcs})
	if !ok {
		fail(debateFailure(core.ErrCodeNoJudge, ""))
		return
	}
	if !slices.Contains(spec.Judges, w.Agent) {
		task.Earlier = nil // part 20's rule: only an AI that got them
	}
	if !m.update(d, func() { d.moderator, d.model = w.Agent, w.Model }) {
		return
	}
	m.log.Info("debate started", "debate", d.id, "moderator", w.Agent, "rules", judge.DebateRules, "ais", len(cast.ais))
	if err := makeFolder(m.o.Gate.Folder); err != nil {
		fail(debateFailure(core.ErrCodeInternal, ""))
		return
	}
	call := func(prompt func() string, read func(text string) error) *judge.Error {
		return m.moderate(d, w, &refs, prompt, read)
	}

	// The plan.
	var plan []judge.DebatePoint
	if f := call(func() string {
		return judge.DebatePlanPrompt(task, judge.DebateNonce(task.TaskContent()...))
	}, func(text string) error {
		p, err := judge.ParsePlan(text, cast.names(false))
		plan = p
		return err
	}); f != nil {
		fail(f)
		return
	}
	speakers := cast.names(true)
	planned := make([][]judge.DebateTurn, len(plan))
	points := make([]DebatePoint, len(plan))
	for i, p := range plan {
		for _, t := range p.Turns {
			if slices.Contains(speakers, t.AI) {
				planned[i] = append(planned[i], t)
			}
		}
		if len(planned[i]) == 0 {
			p.Debate = false // only an official app's answer was to speak
		}
		points[i] = DebatePoint{ID: p.ID, Topic: p.Topic, Nature: p.Nature, Debate: p.Debate, Goal: p.Goal, Turns: []DebateTurn{}}
		for _, s := range p.Sides {
			points[i].Sides = append(points[i].Sides, DebateSide{AIs: cast.codes(s.AIs), Says: s.Says})
		}
		if points[i].Sides == nil {
			points[i].Sides = []DebateSide{}
		}
	}
	debated := slices.ContainsFunc(points, func(p DebatePoint) bool { return p.Debate })
	if !m.update(d, func() {
		d.points = points
		if debated {
			d.status = DebateDebating
		}
	}) {
		return
	}
	if !debated {
		// Nothing worth a debate: not an error, and the combined answer
		// stands.
		m.update(d, func() { m.debateEndLocked(d, DebateDone, nil) })
		return
	}

	// The debated points, one after the other.
	for i := range points {
		if !points[i].Debate {
			continue
		}
		for _, t := range planned[i] {
			if !m.turn(d, cast, task.Question, i, t.AI, t.Ask) {
				return
			}
		}
		for check := 1; check <= judge.MaxDebateChecks; check++ {
			last := check == judge.MaxDebateChecks || m.turnCount(d, i) >= MaxDebateTurns
			if !m.update(d, func() { d.points[i].Checking = true }) {
				return
			}
			jp := m.judgePoint(d, cast, i)
			var next *judge.DebateTurn
			var result, line string
			f := call(func() string {
				return judge.DebateCheckPrompt(task.Question, jp, last, judge.DebateNonce(append([]string{task.Question}, judge.PointContent(jp)...)...))
			}, func(text string) error {
				var err error
				next, result, line, err = judge.ParseCheck(text, speakers, last)
				return err
			})
			if !m.update(d, func() { d.points[i].Checking = false }) {
				return
			}
			if f != nil {
				if f.Code == core.ErrCodeCancelled || d.ctx.Err() != nil {
					fail(f)
					return
				}
				// The point stays without an outcome; the debate goes on.
				m.log.Warn("debate check failed", "debate", d.id, "code", f.Code)
				break
			}
			if next == nil {
				if !m.update(d, func() { d.points[i].Result, d.points[i].Line = result, line }) {
					return
				}
				break
			}
			if !m.turn(d, cast, task.Question, i, next.AI, next.Ask) {
				return
			}
		}
	}

	// The answer after the debate.
	if !m.update(d, func() { d.status = DebateWriting }) {
		return
	}
	var jps []judge.DebatePoint
	for i := range points {
		jps = append(jps, m.judgePoint(d, cast, i))
	}
	start := time.Now()
	var after string
	if f := call(func() string {
		return judge.AfterDebatePrompt(task.SynthTask, jps, judge.DebateNonce(judge.AfterDebateContent(task.SynthTask, jps)...))
	}, func(text string) error {
		after = text
		return nil
	}); f != nil {
		fail(f)
		return
	}
	m.update(d, func() {
		d.after = &DebateAfter{Text: after, Agent: w.Agent, Model: d.model, Ms: time.Since(start).Milliseconds()}
		m.debateEndLocked(d, DebateDone, nil)
	})
}

// ctxEnd ends d after its context ended: the agent is closing or the
// debate went over its limit (a stop has ended it already).
func (m *Manager) ctxEnd(d *debate) {
	f := debateFailure(core.ErrCodeCancelled, msgDebateClosed)
	if errors.Is(d.ctx.Err(), context.DeadlineExceeded) {
		f = debateFailure(core.ErrCodeJudgeTimeout, msgDebateLimit)
	}
	m.update(d, func() { m.debateEndLocked(d, DebateFailed, f) })
}

// turnCount is the number of point i's turns.
func (m *Manager) turnCount(d *debate, i int) int {
	n := 0
	m.locked(func() { n = len(d.points[i].Turns) })
	return n
}

// judgePoint is point i as the prompts read it: names for codes, and only
// the turns that have a reply.
func (m *Manager) judgePoint(d *debate, cast debateCast, i int) judge.DebatePoint {
	var p DebatePoint
	m.locked(func() {
		p = d.points[i]
		p.Turns = slices.Clone(p.Turns)
		p.Sides = slices.Clone(p.Sides)
	})
	name := func(code string) string {
		for _, a := range cast.ais {
			if a.agent == code {
				return a.name
			}
		}
		return code
	}
	out := judge.DebatePoint{ID: p.ID, Topic: p.Topic, Nature: p.Nature, Debate: p.Debate, Goal: p.Goal, Result: p.Result, Line: p.Line}
	for _, s := range p.Sides {
		js := judge.DebateSide{Says: s.Says}
		for _, c := range s.AIs {
			js.AIs = append(js.AIs, name(c))
		}
		out.Sides = append(out.Sides, js)
	}
	for _, t := range p.Turns {
		if t.Status == TurnDone && t.Text != "" {
			out.Turns = append(out.Turns, judge.DebateTurn{AI: name(t.AI), Ask: t.Ask, Text: t.Text, Stance: t.Stance})
		}
	}
	return out
}

// turn runs one turn of point i: the AI named name answers ask. A failed
// turn stays failed and the debate goes on. It reports false when the
// debate ended meanwhile, and ends it when its context did.
func (m *Manager) turn(d *debate, cast debateCast, question string, i int, name, ask string) bool {
	ai, ok := cast.byName[name]
	if !ok || !ai.speaks {
		return true
	}
	jp := m.judgePoint(d, cast, i)
	j := -1
	if !m.update(d, func() {
		d.points[i].Turns = append(d.points[i].Turns, DebateTurn{AI: ai.agent, Ask: ask, Status: TurnRunning, started: time.Now()})
		j = len(d.points[i].Turns) - 1
	}) {
		return false
	}
	prompt := judge.DebateTurnPrompt(question, ai.name, ai.own, jp, ask,
		judge.DebateNonce(append([]string{question, ai.name, ai.own, ask}, judge.PointContent(jp)...)...))
	var t DebateTurn
	if prompt == "" {
		t.Status, t.Error = TurnFailed, debateFailure(core.ErrCodeInternal, "")
	} else {
		t = m.speak(d, ai.agent, prompt)
	}
	if d.ctx.Err() != nil {
		m.ctxEnd(d)
		return false
	}
	return m.update(d, func() {
		u := &d.points[i].Turns[j]
		u.Status, u.Text, u.Stance, u.Model, u.Error = t.Status, t.Text, t.Stance, t.Model, t.Error
		u.Ms = time.Since(u.started).Milliseconds()
		d.calls++
	})
}

// speak asks agent one turn with the ask command in the judge folder, at
// the debate's tier, within the ask's time limit and a process slot.
func (m *Manager) speak(d *debate, agent, prompt string) DebateTurn {
	failed := func(code string) DebateTurn { return DebateTurn{Status: TurnFailed, Error: debateFailure(code, "")} }
	asker, ok := m.adapter(agent).(core.Asker)
	if !ok {
		return failed(core.ErrCodeAgentUnavailable)
	}
	rd := &round{start: time.Now(), cancel: d.stopCh}
	release, f := m.acquire(rd, agent)
	if f != nil {
		if f.Code == core.ErrCodeJudgeTimeout {
			return DebateTurn{Status: TurnFailed, Error: debateFailure(core.ErrCodeAskTimeout, msgDebateQueue)}
		}
		return failed(core.ErrCodeCancelled)
	}
	defer release()
	limit := runs.PartLimit
	if m.o.TurnLimit > 0 {
		limit = m.o.TurnLimit
	}
	ctx, cancel := context.WithTimeout(d.ctx, limit)
	defer cancel()
	res, err := func() (res core.AskResult, err error) {
		defer func() {
			if v := recover(); v != nil {
				res, err = core.AskResult{}, core.Fail(core.ErrCodeInternal, "")
			}
		}()
		return asker.Ask(ctx, core.AskRequest{Folder: m.o.Gate.Folder, Prompt: core.AskPrompt(prompt), Tier: d.spec.Tier})
	}()
	switch {
	case err == nil && strings.TrimSpace(res.Answer) == "":
		return failed(core.ErrCodeAskEmpty)
	case err == nil:
		text, stance := judge.StanceOf(res.Answer)
		return DebateTurn{Status: TurnDone, Text: text, Stance: stance, Model: res.Model}
	case d.ctx.Err() != nil:
		return failed(core.ErrCodeCancelled)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return failed(core.ErrCodeAskTimeout)
	}
	code := core.ErrorCode(err)
	if debateMessages[code] == "" {
		code = core.ErrCodeAskFailed // the AI's own words are never shown or logged
	}
	return failed(code)
}

// moderate makes one moderator call within a process slot: the prompt
// (it draws its own nonce), the model's company checked, the {"answer"}
// read and then read by read; once more with a new prompt when the reply
// is not usable. It returns the error the debate ends with, or nil.
func (m *Manager) moderate(d *debate, w Writer, refs *[]string, prompt func() string, read func(text string) error) *judge.Error {
	var first *judge.Error
	for attempt := 1; attempt <= 2; attempt++ {
		if d.ctx.Err() != nil {
			return debateFailure(core.ErrCodeCancelled, "")
		}
		p := prompt()
		if p == "" {
			return debateFailure(core.ErrCodeInternal, "")
		}
		rd := &round{start: time.Now(), cancel: d.stopCh}
		release, f := m.acquire(rd, w.Agent)
		if f != nil {
			if f.Code == core.ErrCodeJudgeTimeout {
				return debateFailure(core.ErrCodeJudgeTimeout, msgDebateQueue)
			}
			return debateFailure(core.ErrCodeCancelled, "")
		}
		if !m.update(d, func() { d.calls++ }) {
			release()
			return debateFailure(core.ErrCodeCancelled, "")
		}
		res, err := m.callSynthIn(d.ctx, w, core.SynthRequest{Folder: m.o.Gate.Folder, Prompt: p, Model: w.Model, Effort: w.Effort})
		release()
		if res.Cleanup != "" {
			*refs = append(*refs, res.Cleanup)
		}
		if d.ctx.Err() != nil {
			return debateFailure(core.ErrCodeCancelled, "")
		}
		if err != nil {
			sf := synthCallFailure(err)
			if sf.Code != core.ErrCodeJudgeFailed {
				sf = debateFailure(sf.Code, "")
			}
			if sf.Code == core.ErrCodeJudgeBadOutput && attempt == 1 {
				first = sf
				continue
			}
			return sf
		}
		if family := w.Judger.Family(res.Model); family == "" || !strings.EqualFold(family, w.Family) {
			return debateFailure(core.ErrCodeBadModel, msgDebateCompany)
		}
		text, verr := judge.ValidateSynth(res.Output)
		if verr == nil {
			verr = read(text)
		}
		if verr != nil {
			first = debateFailure(core.ErrCodeJudgeBadOutput, "")
			continue
		}
		if res.Model != "" {
			m.update(d, func() { d.model = res.Model })
		}
		return nil
	}
	return first
}
