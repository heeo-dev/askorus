package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/core"
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
)

const (
	// maxPrompt bounds one question. A follow-up that quotes other AIs'
	// answers (P2-3) is the longest case expected.
	maxPrompt   = 32 << 10
	maxRunBody  = 2*maxPrompt + 8<<10 // JSON escapes (\n, \") make the body longer than the prompt
	maxClientID = 64
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
}

// RunStatus is the answer to POST /v1/runs (202) and GET /v1/runs/{runId}.
type RunStatus struct {
	runs.Run
	PollAfterMs int `json:"pollAfterMs,omitempty"` // set while running
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
	targets, askers, ok := s.askersFor(w, r.Context(), req.Agents)
	if !ok {
		return
	}
	folder, err := s.askFolder()
	if err != nil {
		if s.o.Logger != nil {
			s.o.Logger.Error("ask folder", "err", err.Error())
		}
		writeError(w, http.StatusServiceUnavailable, CodeAskFolder, "the ask folder cannot be made on the PC")
		return
	}
	full := core.AskPrompt(prompt)
	tasks := make([]runs.Task, len(targets))
	for i, t := range targets {
		areq := core.AskRequest{Folder: folder, Prompt: full, Tier: tier, Model: t.Model, Effort: t.Effort}
		if t.Session != "" {
			_, areq.Resume, _ = core.ParseID(t.Session)
		}
		asker := askers[i]
		tasks[i] = runs.Task{Agent: t.Agent, Session: t.Session, Ask: func(ctx context.Context) (core.AskResult, error) {
			return asker.Ask(ctx, areq)
		}}
	}
	run, err := s.runs.Start(runs.Spec{Owner: deviceOf(r), Key: req.ClientRunID, Mode: req.Mode, Tier: tier, Tasks: tasks})
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
	if s.o.Logger != nil {
		names := make([]string, len(targets))
		followUp := false
		for i, t := range targets {
			names[i] = t.Agent
			followUp = followUp || t.Session != ""
		}
		s.o.Logger.Info("run started", "mode", req.Mode, "tier", tier, "agents", strings.Join(names, ","), "followUp", followUp)
	}
	writeJSON(w, http.StatusAccepted, s.runStatus(run))
}

// getRun is GET /v1/runs/{runId}. A device only sees its own runs.
func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	run, ok := s.runs.Get(deviceOf(r), r.PathValue("runId"))
	if !ok {
		writeError(w, http.StatusNotFound, CodeRunNotFound, "no such run (finished more than 30 minutes ago, or the agent restarted)")
		return
	}
	writeJSON(w, http.StatusOK, s.runStatus(run))
}

func (s *Server) runStatus(run runs.Run) RunStatus {
	st := RunStatus{Run: run}
	if run.Status == runs.Running {
		st.PollAfterMs = runPollMs
	}
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
