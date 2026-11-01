package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/asksemble/asksemble/agent/internal/compares"
	"github.com/asksemble/asksemble/agent/internal/core"
)

// Error codes for comparisons (P2-3), beside core's judge codes
// (need_two_answers, no_judge, too_many_comparisons).
const (
	CodeComparisonNotFound = "comparison_not_found"
	CodeComparisonRunning  = "comparison_running"
)

// maxComparisonBody bounds POST /v1/comparisons: six answers of 64 KiB,
// the question and the earlier questions, with room for JSON escapes.
const maxComparisonBody = 1 << 20

// ComparisonStatus is the answer to the comparison routes: the comparison
// and, while it is waiting or running, when to ask again.
type ComparisonStatus struct {
	compares.Comparison
	PollAfterMs int `json:"pollAfterMs,omitempty"`
}

// ComparisonAnswer is one answer of POST /v1/comparisons. Via is "pc" for
// an answer of an AI program on this PC (Agent is its name, Model the
// model that answered: without it the answer's company is unknown and no
// judge can be picked for it) or "handoff" for one pasted from an official
// app (Agent is the app: chatgpt, claude, gemini).
type ComparisonAnswer struct {
	ID    string `json:"id"`
	Agent string `json:"agent"`
	Via   string `json:"via"`
	Model string `json:"model,omitempty"`
	Text  string `json:"text"`
}

// ComparisonRequest is the body of POST /v1/comparisons: a comparison made
// from answers the phone has, after its run expired or for pasted answers.
// ClientComparisonID is the phone's own id for it: sending the same
// request again returns the first comparison. Unknown fields are ignored
// (principle 4).
type ComparisonRequest struct {
	ClientComparisonID string             `json:"clientComparisonId,omitempty"`
	Question           string             `json:"question"`
	Earlier            []string           `json:"earlier,omitempty"`
	Answers            []ComparisonAnswer `json:"answers"`
	Judges             []string           `json:"judges,omitempty"`
}

const msgComparisonNotFound = "no such comparison (ended more than 30 minutes ago, made by another device, or the agent restarted)"

func comparisonStatus(c compares.Comparison) ComparisonStatus {
	st := ComparisonStatus{Comparison: c}
	if c.Status == compares.Waiting || c.Status == compares.Running {
		st.PollAfterMs = compares.PollMs
	}
	return st
}

// startComparison is POST /v1/comparisons. It answers 202 at once; GET
// /v1/comparisons/{id} then shows the cards as the judges answer. The
// question and the answers are never logged (the request log has method,
// path and status only), and errors go out through writeError with fixed
// text, never actionError, which logs an error's detail.
func (s *Server) startComparison(w http.ResponseWriter, r *http.Request) {
	var req ComparisonRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxComparisonBody)).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, CodeBadRequest, "body is over 1 MiB")
			return
		}
		writeError(w, http.StatusBadRequest, CodeBadRequest, "body must be {clientComparisonId, question, earlier, answers, judges}")
		return
	}
	if len(req.ClientComparisonID) > maxClientID {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "clientComparisonId is too long")
		return
	}
	if _, err := s.askFolder(); err != nil {
		// Judges run in the ask folder's judge folder.
		if s.o.Logger != nil {
			s.o.Logger.Error("ask folder", "err", err.Error())
		}
		writeError(w, http.StatusServiceUnavailable, CodeAskFolder, "the ask folder cannot be made on the PC")
		return
	}
	spec := compares.DirectSpec{
		Owner: deviceOf(r), Key: req.ClientComparisonID,
		Question: req.Question, Earlier: req.Earlier, Judges: req.Judges,
	}
	for _, a := range req.Answers {
		spec.Answers = append(spec.Answers, compares.DirectAnswer{ID: a.ID, Agent: a.Agent, Via: a.Via, Model: a.Model, Text: a.Text})
	}
	c, err := s.compares.Start(spec)
	var bad *compares.SpecError
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, comparisonStatus(c))
	case errors.As(err, &bad) && bad.TooLarge:
		writeError(w, http.StatusRequestEntityTooLarge, CodeBadRequest, bad.Message)
	case errors.As(err, &bad):
		writeError(w, http.StatusBadRequest, bad.Code, bad.Message)
	case errors.Is(err, compares.ErrTooMany):
		writeError(w, http.StatusTooManyRequests, core.ErrCodeTooManyComparisons, "the PC is still comparing other answers")
	case errors.Is(err, compares.ErrNoJudge):
		writeError(w, http.StatusServiceUnavailable, core.ErrCodeNoJudge, "no AI on this PC judges comparisons now")
	default:
		writeError(w, http.StatusInternalServerError, CodeInternal, "the agent could not make the comparison")
	}
}

// getComparison is GET /v1/comparisons/{id}. A device only sees its own
// comparisons.
func (s *Server) getComparison(w http.ResponseWriter, r *http.Request) {
	c, ok := s.compares.Get(deviceOf(r), r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, CodeComparisonNotFound, msgComparisonNotFound)
		return
	}
	writeJSON(w, http.StatusOK, comparisonStatus(c))
}

// retryComparison is POST /v1/comparisons/{id}/retry: the failed requests
// of an ended comparison run again (202). An ended comparison with none
// comes back as it is.
func (s *Server) retryComparison(w http.ResponseWriter, r *http.Request) {
	c, err := s.compares.Retry(deviceOf(r), r.PathValue("id"))
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, comparisonStatus(c))
	case errors.Is(err, compares.ErrNotFound):
		writeError(w, http.StatusNotFound, CodeComparisonNotFound, msgComparisonNotFound)
	case errors.Is(err, compares.ErrRunning):
		writeError(w, http.StatusConflict, CodeComparisonRunning, "the comparison is still waiting or running")
	default:
		writeError(w, http.StatusInternalServerError, CodeInternal, "the agent could not retry the comparison")
	}
}

// cancelComparison is POST /v1/comparisons/{id}/cancel: queued requests
// are dropped and no judge is asked again; requests that run finish. An
// ended comparison stays as it is.
func (s *Server) cancelComparison(w http.ResponseWriter, r *http.Request) {
	c, err := s.compares.Cancel(deviceOf(r), r.PathValue("id"))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, comparisonStatus(c))
	case errors.Is(err, compares.ErrNotFound):
		writeError(w, http.StatusNotFound, CodeComparisonNotFound, msgComparisonNotFound)
	default:
		writeError(w, http.StatusInternalServerError, CodeInternal, "the agent could not cancel the comparison")
	}
}
