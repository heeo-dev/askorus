package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/compares"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
	"github.com/asksemble/asksemble/agent/internal/runs"
)

// Error codes for runs.
const (
	CodeRunNotFound   = "run_not_found"
	CodeTooManyRuns   = "too_many_runs"
	CodeModeReserved  = "mode_not_available" // work and code come in Phase 4
	CodeAgentNoAsk    = "agent_cannot_ask"
	CodeAskFolder     = "ask_folder_unavailable"
	CodeBadSessionRef = "bad_session"
	// compareError of POST /v1/runs: compare was not {earlier, judges,
	// synthesis} within its limits. The ask runs all the same (P2-3).
	CodeBadCompare = "bad_compare"
)

const (
	// maxPrompt bounds one question. A follow-up that quotes other AIs'
	// answers (P2-3) is the longest case expected.
	maxPrompt = 32 << 10
	// maxRunBody: JSON escapes (\n, \") make the body longer than the
	// prompt; compare's earlier questions (3 of 4 KiB) take up to 32 KiB
	// more, escaped the same way.
	maxRunBody  = 2*maxPrompt + 8<<10 + 32<<10 + maxContextBody
	maxClientID = 64
	// compare's limits: the earlier questions every listed AI gets
	// (internal/judge's limits) and the judges a phone may name.
	maxCompareEarlier      = judge.MaxEarlier
	maxCompareEarlierBytes = judge.MaxEarlierBytes
	maxCompareJudges       = compares.MaxJudges
)

// RunTarget is one AI of a run. Model and effort override the tier for
// this AI; session continues an earlier answer ("<agent>:<id>").
type RunTarget struct {
	Agent   string `json:"agent"`
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort,omitempty"`
	Session string `json:"session,omitempty"`
}

// RunRequest is the body of POST /v1/runs. Mode is ask in Phase 2; work
// and code are reserved. Agents empty means every AI that can ask.
type RunRequest struct {
	Mode   string      `json:"mode"`
	Prompt string      `json:"prompt"`
	Tier   string      `json:"tier,omitempty"` // quick | standard (default) | deep
	Agents []RunTarget `json:"agents,omitempty"`
	// ClientRunID is the phone's own id for this run. Sending the same
	// request again returns the first run instead of asking twice.
	ClientRunID string `json:"clientRunId,omitempty"`
	// Compare asks for the answers to be compared once they are all in
	// (P2-3): {earlier, judges}, see RunCompare. It is read apart from the
	// rest, so a bad one never refuses the ask: the 202 then carries
	// compareError instead of comparisonId.
	Compare json.RawMessage `json:"compare,omitempty"`
	// Refine asks the moderator to refine the question first (질문 정제,
	// milestone C): {"agent": "claude"} names the moderator ("" lets the
	// agent pick among the asked AIs), {"off": true} or absent is off. The
	// run's snapshot then carries refine. Without Options.Refine (the
	// default since 2026-11-04) it is ignored.
	Refine *RunRefine `json:"refine,omitempty"`
	// Context is a follow-up's conversation record (#29): it goes to the
	// answering AIs before the question, and to the combined answer.
	Context *RequestContext `json:"context,omitempty"`
}

// RunRefine is RunRequest.refine.
type RunRefine struct {
	Agent string `json:"agent,omitempty"`
	Off   bool   `json:"off,omitempty"`
}

// RunCompare is RunRequest.compare. Earlier are the earlier questions of a
// follow-up, oldest first, at most 3 of 4 KiB; judges (a name kept from
// the judging days) are the AIs the phone sent the earlier questions to,
// in order of preference, at most 8: the agent keeps those that were asked
// (결정 5), and every asked AI when none of them was; a writer among them
// gets the earlier questions. Synthesis asks for the combined answer once
// the answers are in (S5b); absent is off. Unknown fields are ignored (principle 4).
type RunCompare struct {
	Earlier   []string      `json:"earlier,omitempty"`
	Judges    []string      `json:"judges,omitempty"`
	Synthesis *RunSynthesis `json:"synthesis,omitempty"`
}

// RunSynthesis is RunCompare.synthesis: {"agent": "claude"}, the AI that
// should write the combined answer ("" lets the agent pick among the
// asked AIs), or {"off": true}. The tier is the run's. Model and effort
// (11-06) are the moderator model set for agent: used when that AI writes
// and the model is one of its tier models, else the tier's.
type RunSynthesis struct {
	Agent  string `json:"agent,omitempty"`
	Off    bool   `json:"off,omitempty"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

// badSynthModel: a moderator model or effort that is not a model name or
// an effort level, or one without the AI it was set for.
func badSynthModel(agent, model, effort string) bool {
	return (model != "" && (agent == "" || !core.ValidAskModel(model))) || (effort != "" && (model == "" || !core.ValidAskEffort(effort)))
}

// RunStatus is the answer to POST /v1/runs (202) and GET /v1/runs/{runId}.
type RunStatus struct {
	runs.Run
	PollAfterMs int `json:"pollAfterMs,omitempty"` // set while running
	// ComparisonID is the run's comparison (GET /v1/comparisons/{id}).
	ComparisonID string `json:"comparisonId,omitempty"`
	// CompareError is why the POST that asked for a comparison got none:
	// bad_compare, need_two_answers, too_many_comparisons or internal. Only
	// that POST's 202 carries it.
	CompareError *judge.Error `json:"compareError,omitempty"`
}

// Fixed messages of compareError.
const (
	msgBadCompare  = "compare must be {earlier, judges, synthesis}: at most 3 earlier questions of 4 KiB each, at most 8 AI programs of this PC, each named once, and a synthesis {agent, off} with an AI of this PC"
	msgNeedTwo     = "a comparison needs answers from at least two AIs"
	msgTooManyCmp  = "the PC is still comparing other answers"
	msgCompareFail = "the agent could not make the comparison"
)

// agentNameRe is what an AI program's name looks like (an adapter's
// Agent()); compare's judges must match it.
var agentNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// parseCompare reads RunRequest.compare. It is nil, nil when there is none
// (absent or null), and a bad_compare error when it is not a RunCompare
// within its limits.
func (s *Server) parseCompare(raw json.RawMessage) (*RunCompare, *judge.Error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	bad := &judge.Error{Code: CodeBadCompare, Message: msgBadCompare}
	var c RunCompare
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, bad
	}
	if len(c.Earlier) > maxCompareEarlier || len(c.Judges) > maxCompareJudges {
		return nil, bad
	}
	for _, e := range c.Earlier {
		if len(e) > maxCompareEarlierBytes || !utf8.ValidString(e) || strings.ContainsRune(e, 0) {
			return nil, bad
		}
	}
	seen := map[string]bool{}
	for _, j := range c.Judges {
		if !agentNameRe.MatchString(j) || seen[j] || s.adapterFor(j) == nil {
			return nil, bad
		}
		seen[j] = true
	}
	if sy := c.Synthesis; sy != nil && sy.Agent != "" && (!agentNameRe.MatchString(sy.Agent) || s.adapterFor(sy.Agent) == nil) {
		return nil, bad
	}
	if sy := c.Synthesis; sy != nil && badSynthModel(sy.Agent, sy.Model, sy.Effort) {
		return nil, bad
	}
	return &c, nil
}

// attachCompare makes the comparison of a run that has just started. It
// returns the compareError for the 202, or nil when the comparison was
// made (or found again, for a re-sent run).
func (s *Server) attachCompare(owner, question string, run runs.Run, targets []RunTarget, c *RunCompare, conv *judge.Conversation) *judge.Error {
	if len(targets) < 2 {
		return &judge.Error{Code: core.ErrCodeNeedTwoAnswers, Message: msgNeedTwo}
	}
	asked := make([]string, len(targets))
	for i, t := range targets {
		asked[i] = t.Agent
	}
	var judges []string
	for _, j := range c.Judges {
		if slices.Contains(asked, j) {
			judges = append(judges, j) // only AIs that were asked count (결정 5)
		}
	}
	if len(judges) == 0 {
		judges = asked
	}
	spec := compares.RunSpec{
		Owner: owner, RunID: run.ID, Question: question,
		Earlier: c.Earlier, Judges: judges, Asked: asked, Conversation: conv,
	}
	if sy := c.Synthesis; sy != nil {
		spec.Synth = &compares.SynthSpec{Off: sy.Off, Agent: sy.Agent, Tier: run.Tier, Model: sy.Model, Effort: sy.Effort}
	}
	_, err := s.compares.Attach(spec)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, compares.ErrTooMany):
		return &judge.Error{Code: core.ErrCodeTooManyComparisons, Message: msgTooManyCmp}
	default:
		return &judge.Error{Code: core.ErrCodeInternal, Message: msgCompareFail}
	}
}

const runPollMs = 1000

// askersFor checks the targets against the adapters that can ask now.
// It answers the HTTP error itself and returns ok=false.
func (s *Server) askersFor(w http.ResponseWriter, ctx context.Context, targets []RunTarget) ([]RunTarget, []core.Asker, bool) {
	can := map[string]core.Asker{}
	var all []RunTarget
	for _, a := range s.o.Adapters {
		asker, ok := a.(core.Asker)
		if !ok {
			continue
		}
		info := a.Info(ctx)
		if info.Available && slices.Contains(info.Capabilities, core.CapAsk) {
			can[a.Agent()] = asker
			all = append(all, RunTarget{Agent: a.Agent()})
		}
	}
	if len(targets) == 0 {
		targets = all
	}
	if len(targets) == 0 {
		writeError(w, http.StatusServiceUnavailable, core.ErrCodeAgentUnavailable, "no AI on this PC can answer")
		return nil, nil, false
	}
	seen := map[string]bool{}
	askers := make([]core.Asker, len(targets))
	for i, t := range targets {
		switch {
		case seen[t.Agent]:
			writeError(w, http.StatusBadRequest, CodeBadRequest, "each AI may be named once")
			return nil, nil, false
		case t.Model != "" && !core.ValidAskModel(t.Model):
			writeError(w, http.StatusBadRequest, CodeBadRequest, "model is not a model name")
			return nil, nil, false
		case t.Effort != "" && !core.ValidAskEffort(t.Effort):
			writeError(w, http.StatusBadRequest, CodeBadRequest, "effort is not an effort level")
			return nil, nil, false
		}
		seen[t.Agent] = true
		if t.Session != "" {
			agent, _, err := core.ParseID(t.Session)
			if err != nil || agent != t.Agent {
				writeError(w, http.StatusBadRequest, CodeBadSessionRef, "session must be <agent>:<id> of the same AI")
				return nil, nil, false
			}
		}
		asker, ok := can[t.Agent]
		if !ok {
			if s.adapterFor(t.Agent) == nil {
				writeError(w, http.StatusBadRequest, CodeAgentNoAsk, "unknown AI")
			} else {
				writeError(w, http.StatusServiceUnavailable, CodeAgentNoAsk, "this AI cannot answer on this PC now")
			}
			return nil, nil, false
		}
		askers[i] = asker
	}
	return targets, askers, true
}

// startRun is POST /v1/runs. It answers 202 at once; GET /v1/runs/{runId}
// then shows each AI's answer as it comes. The prompt and answers are
// never logged (the request log has method, path and status only).
func (s *Server) startRun(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRunBody)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "body must be {mode, prompt, tier, agents}")
		return
	}
	switch req.Mode {
	case core.ModeAsk:
	case core.ModeWork, core.ModeCode:
		writeError(w, http.StatusNotImplemented, CodeModeReserved, "this mode is not available yet")
		return
	default:
		writeError(w, http.StatusBadRequest, CodeBadRequest, "mode must be ask")
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	switch {
	case prompt == "":
		writeError(w, http.StatusBadRequest, CodeBadRequest, "prompt is empty")
		return
	case len(prompt) > maxPrompt:
		writeError(w, http.StatusRequestEntityTooLarge, CodeBadRequest, "prompt is too long")
		return
	case !utf8.ValidString(prompt) || strings.ContainsRune(prompt, 0):
		writeError(w, http.StatusBadRequest, CodeBadRequest, "prompt must be text")
		return
	}
	tier := req.Tier
	if tier == "" {
		tier = core.TierStandard
	}
	if !core.ValidTier(tier) {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "tier must be quick, standard or deep")
		return
	}
	if len(req.ClientRunID) > maxClientID {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "clientRunId is too long")
		return
	}
	conv, ok := conversationOf(req.Context)
	if !ok {
		writeError(w, http.StatusBadRequest, CodeBadRequest, msgBadContext)
		return
	}
	targets, askers, ok := s.askersFor(w, r.Context(), req.Agents)
	if !ok {
		return
	}
	// compare is checked before the ask starts but never stops it.
	cmp, cmpErr := s.parseCompare(req.Compare)
	folder, err := s.askFolder()
	if err != nil {
		if s.o.Logger != nil {
			s.o.Logger.Error("ask folder", "err", err.Error())
		}
		writeError(w, http.StatusServiceUnavailable, CodeAskFolder, "the ask folder cannot be made on the PC")
		return
	}
	if rf := req.Refine; s.o.Refine && rf != nil && rf.Agent != "" && !agentNameRe.MatchString(rf.Agent) {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "refine must be {agent, off} with an AI of this PC")
		return
	}
	owner := deviceOf(r)
	// Refining (milestone C): the first AI to start asks the moderator,
	// every AI waits for the spec and asks the question with its template.
	// A retried request returns the first run, whose tasks ran already, so
	// it is never refined twice.
	var refiner *compares.RunRefiner
	runID := make(chan string, 1)
	if rf := req.Refine; s.o.Refine && rf != nil && !rf.Off && s.compares != nil {
		var asked []string
		for _, t := range targets {
			asked = append(asked, t.Agent)
		}
		var earlier []string
		if cmp != nil {
			earlier = cmp.Earlier
		}
		var id string
		var idOnce sync.Once
		refiner = s.compares.NewRunRefiner(compares.RefineSpec{Question: prompt, Earlier: earlier, Asked: asked, Agent: rf.Agent},
			func(x compares.Refined) {
				idOnce.Do(func() { id = <-runID })
				s.runs.SetRefine(owner, id, x)
			})
	}
	tasks := make([]runs.Task, len(targets))
	for i, t := range targets {
		// A follow-up's record goes before the question (#29), with the
		// earlier questions for an AI new to the conversation.
		fresh := t.Session == ""
		areq := core.AskRequest{Folder: folder, Prompt: judge.AskWithConversation(prompt, conv, fresh), Tier: tier, Model: t.Model, Effort: t.Effort}
		if t.Session != "" {
			_, areq.Resume, _ = core.ParseID(t.Session)
		}
		asker := askers[i]
		tasks[i] = runs.Task{Agent: t.Agent, Session: t.Session, Ask: func(ctx context.Context) (core.AskResult, error) {
			if refiner != nil {
				q, err := refiner.Question(ctx)
				if err != nil {
					return core.AskResult{}, err
				}
				areq.Prompt = judge.AskWithConversation(q, conv, fresh)
			}
			return asker.Ask(ctx, areq)
		}}
	}
	run, err := s.runs.Start(runs.Spec{Owner: owner, Key: req.ClientRunID, Mode: req.Mode, Tier: tier, Tasks: tasks})
	runID <- run.ID
	switch {
	case errors.Is(err, runs.ErrTooMany):
		writeError(w, http.StatusTooManyRequests, CodeTooManyRuns, "the PC is still answering other questions")
		return
	case errors.Is(err, runs.ErrSessionBusy):
		writeError(w, http.StatusConflict, core.ErrCodeBusy, "that conversation is still answering")
		return
	}
	if err != nil {
		s.actionError(w, err)
		return
	}
	if cmp != nil {
		cmpErr = s.attachCompare(owner, prompt, run, targets, cmp, conv)
	}
	if s.o.Logger != nil {
		names := make([]string, len(targets))
		followUp := false
		for i, t := range targets {
			names[i] = t.Agent
			followUp = followUp || t.Session != ""
		}
		compareCode := ""
		if cmpErr != nil {
			compareCode = cmpErr.Code
		}
		synth := cmp != nil && cmpErr == nil && cmp.Synthesis != nil && !cmp.Synthesis.Off
		s.o.Logger.Info("run started", append([]any{"mode", req.Mode, "tier", tier, "agents", strings.Join(names, ","), "followUp", followUp,
			"compare", cmp != nil || cmpErr != nil, "compareCode", compareCode, "synthesis", synth, "refine", refiner != nil}, conv.Log()...)...)
	}
	st := s.runStatus(owner, run)
	st.CompareError = cmpErr
	writeJSON(w, http.StatusAccepted, st)
}

// getRun is GET /v1/runs/{runId}. A device only sees its own runs.
func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	owner := deviceOf(r)
	run, ok := s.runs.Get(owner, r.PathValue("runId"))
	if !ok {
		writeError(w, http.StatusNotFound, CodeRunNotFound, "no such run (finished more than 30 minutes ago, or the agent restarted)")
		return
	}
	writeJSON(w, http.StatusOK, s.runStatus(owner, run))
}

// runStatus is run as the phone gets it, with its comparison's id.
func (s *Server) runStatus(owner string, run runs.Run) RunStatus {
	st := RunStatus{Run: run}
	if run.Status == runs.Running {
		st.PollAfterMs = runPollMs
	}
	st.ComparisonID, _ = s.compares.ForRun(owner, run.ID)
	return st
}

// askFolder returns the ask folder, made if missing. Asks run there so
// no project's CLAUDE.md or AGENTS.md reaches them (P0-9).
func (s *Server) askFolder() (string, error) {
	dir := s.o.AskFolder
	if dir == "" || !filepath.IsAbs(dir) {
		return "", errors.New("no absolute ask folder set")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// markAskSessions badges sessions that ran in the ask folder (P0-8).
func (s *Server) markAskSessions(ss []core.Session) {
	if s.o.AskFolder == "" {
		return
	}
	for i := range ss {
		if core.InFolder(ss[i].Folder, s.o.AskFolder) && !slices.Contains(ss[i].Badges, core.BadgeAsk) {
			ss[i].Badges = append(ss[i].Badges, core.BadgeAsk)
		}
	}
}
