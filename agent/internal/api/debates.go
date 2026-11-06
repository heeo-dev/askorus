package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/asksemble/asksemble/agent/internal/compares"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Debates (토론, P2-5, #26): POST /v1/debates starts one from the answers
// and the combined answer the phone has, GET /v1/debates/{id} reads it
// (the phone every pollAfterMs) and POST /v1/debates/{id}/stop stops it.
// Nothing of a debate is ever logged but codes and counts.

// Error codes of debates, beside the comparisons' (need_two_answers,
// no_judge, judge_bad_output…).
const (
	CodeDebateNotFound = "debate_not_found"
	CodeTooManyDebates = "too_many_debates"
)

// maxDebateBody bounds POST /v1/debates: six answers of 64 KiB, the
// combined answer, the question and the earlier questions, with room for
// JSON escapes.
const maxDebateBody = 2 << 20

const msgDebateNotFound = "no such debate (ended more than 30 minutes ago, made by another device, or the agent restarted)"

// DebateInfo is /v1/info's debate: the agent runs debates, with these
// limits. The phone shows '토론하기' only when it is there.
type DebateInfo struct {
	MaxPoints int    `json:"maxPoints"` // points debated
	MaxTurns  int    `json:"maxTurns"`  // turns of one point
	Rules     string `json:"rules"`     // the prompts' version
}

// debateInfo is /v1/info's debate: nil without an ask folder, where no AI
// moderates.
func (s *Server) debateInfo() *DebateInfo {
	if s.gate.Folder == "" {
		return nil
	}
	return &DebateInfo{MaxPoints: judge.MaxDebated, MaxTurns: compares.MaxDebateTurns, Rules: judge.DebateRules}
}

// DebateAnswer is one answer of POST /v1/debates, as a comparison's: via
// "pc" (Agent is an AI program on this PC, which speaks) or "handoff"
// (Agent is an official app, whose answer has a side and never speaks).
type DebateAnswer struct {
	Agent string `json:"agent"`
	Via   string `json:"via"`
	Model string `json:"model,omitempty"`
	Text  string `json:"text"`
}

// DebateRequest is the body of POST /v1/debates. ClientDebateID is the
// phone's own id for it: the same request again returns the first debate.
// Synthesis is the combined answer's text the debate starts from. Judges
// are the AIs Earlier may go to, as a comparison's. ModeratorModel and
// ModeratorEffort (11-06) are the moderator model set for Moderator, as
// RunSynthesis's model and effort. Unknown fields are ignored (principle 4).
type DebateRequest struct {
	ClientDebateID  string         `json:"clientDebateId,omitempty"`
	Question        string         `json:"question"`
	Earlier         []string       `json:"earlier,omitempty"`
	Judges          []string       `json:"judges,omitempty"`
	Answers         []DebateAnswer `json:"answers"`
	Synthesis       string         `json:"synthesis"`
	Moderator       string         `json:"moderator,omitempty"`
	Tier            string         `json:"tier,omitempty"`
	ModeratorModel  string         `json:"moderatorModel,omitempty"`
	ModeratorEffort string         `json:"moderatorEffort,omitempty"`
}

// DebateStatus is the answer to the debate routes: the debate and, while
// it runs, when to ask again.
type DebateStatus struct {
	compares.Debate
	PollAfterMs int `json:"pollAfterMs,omitempty"`
}

func debateStatus(d compares.Debate) DebateStatus {
	st := DebateStatus{Debate: d}
	switch d.Status {
	case compares.DebatePlanning, compares.DebateDebating, compares.DebateWriting:
		st.PollAfterMs = compares.PollMs
	}
	return st
}

// startDebate is POST /v1/debates. It answers 202 at once.
func (s *Server) startDebate(w http.ResponseWriter, r *http.Request) {
	var req DebateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxDebateBody)).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, CodeBadRequest, "body is over 2 MiB")
			return
		}
		writeError(w, http.StatusBadRequest, CodeBadRequest, "body must be {clientDebateId, question, earlier, judges, answers, synthesis, moderator, tier}")
		return
	}
	if len(req.ClientDebateID) > maxClientID {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "clientDebateId is too long")
		return
	}
	if badSynthModel(req.Moderator, req.ModeratorModel, req.ModeratorEffort) {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "moderatorModel and moderatorEffort must be a model name and an effort level, with the moderator they were set for")
		return
	}
	if _, err := s.askFolder(); err != nil {
		// The moderator and the debaters run in the ask folder's judge
		// folder.
		if s.o.Logger != nil {
			s.o.Logger.Error("ask folder", "err", err.Error())
		}
		writeError(w, http.StatusServiceUnavailable, CodeAskFolder, "the ask folder cannot be made on the PC")
		return
	}
	spec := compares.DebateSpec{
		Owner: deviceOf(r), Key: req.ClientDebateID, Question: req.Question, Earlier: req.Earlier, Judges: req.Judges,
		Combined: req.Synthesis, Moderator: req.Moderator, Tier: req.Tier,
		ModeratorModel: req.ModeratorModel, ModeratorEffort: req.ModeratorEffort,
	}
	for _, a := range req.Answers {
		spec.Answers = append(spec.Answers, compares.DebateAnswer{Agent: a.Agent, Via: a.Via, Model: a.Model, Text: a.Text})
	}
	d, err := s.compares.StartDebate(spec)
	var bad *compares.SpecError
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, debateStatus(d))
	case errors.As(err, &bad) && bad.TooLarge:
		writeError(w, http.StatusRequestEntityTooLarge, CodeBadRequest, bad.Message)
	case errors.As(err, &bad):
		writeError(w, http.StatusBadRequest, bad.Code, bad.Message)
	case errors.Is(err, compares.ErrDebateBusy):
		writeError(w, http.StatusTooManyRequests, CodeTooManyDebates, "the PC is running another debate")
	default:
		writeError(w, http.StatusInternalServerError, CodeInternal, "the agent could not start the debate")
	}
}

// getDebate is GET /v1/debates/{id}. A device only sees its own debates.
func (s *Server) getDebate(w http.ResponseWriter, r *http.Request) {
	d, ok := s.compares.GetDebate(deviceOf(r), r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, CodeDebateNotFound, msgDebateNotFound)
		return
	}
	writeJSON(w, http.StatusOK, debateStatus(d))
}

// stopDebate is POST /v1/debates/{id}/stop: a running debate ends stopped
// at once (its turn or moderator call is ended); an ended one comes back
// as it is.
func (s *Server) stopDebate(w http.ResponseWriter, r *http.Request) {
	d, err := s.compares.StopDebate(deviceOf(r), r.PathValue("id"))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, debateStatus(d))
	case errors.Is(err, compares.ErrDebateNotFound):
		writeError(w, http.StatusNotFound, CodeDebateNotFound, msgDebateNotFound)
	default:
		writeError(w, http.StatusInternalServerError, CodeInternal, "the agent could not stop the debate")
	}
}
