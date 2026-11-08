package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/heeo-dev/askorus/agent/internal/compares"
	"github.com/heeo-dev/askorus/agent/internal/core"
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
// model that answered: without it the answer's company is unknown) or
// "handoff" for one pasted from an official
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
	// Synthesis asks for the combined answer once the answers are taken
	// (S5b); absent is off.
	Synthesis *ComparisonSynthesis `json:"synthesis,omitempty"`
	// Context is the conversation record of the turn's follow-up (#29),
	// for the combined answer.
	Context *RequestContext `json:"context,omitempty"`
}

// ComparisonSynthesis is ComparisonRequest.synthesis: the AI that should
// write ("" lets the agent pick among the answers' AIs and the listed
// judges),
// off, and the ask tier whose model writes ("" is standard).
type ComparisonSynthesis struct {
	Agent string `json:"agent,omitempty"`
	Off   bool   `json:"off,omitempty"`
	Tier  string `json:"tier,omitempty"`
	// Model and effort: the moderator model set for agent (11-06), as RunSynthesis's.
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

// SynthesisRequest is the optional body of POST
// /v1/comparisons/{id}/synthesis: the AI that should write, or none for
// the one asked for before. Unknown fields are ignored.
type SynthesisRequest struct {
	Agent string `json:"agent,omitempty"`
}

// maxSynthesisBody bounds POST /v1/comparisons/{id}/synthesis.
const maxSynthesisBody = 4 << 10

// Fixed messages of the combined answer's 400s.
const (
	msgBadComparisonSynth = "synthesis must be {agent, off, tier, model, effort} with an AI of this PC, a tier, and a model and effort only with their AI"
	msgBadSynthesisBody   = "body must be empty or {agent} with an AI of this PC"
)

const msgComparisonNotFound = "no such comparison (ended more than a day ago, made by another device, or the agent restarted)"

// comparisonStatus adds pollAfterMs while the comparison or its combined
// answer waits or runs.
func comparisonStatus(c compares.Comparison) ComparisonStatus {
	st := ComparisonStatus{Comparison: c}
	busy := c.Synthesis != nil && (c.Synthesis.Status == compares.SynthWaiting || c.Synthesis.Status == compares.SynthRunning)
	if c.Status == compares.Waiting || c.Status == compares.Running || busy {
		st.PollAfterMs = compares.PollMs
	}
	return st
}

// startComparison is POST /v1/comparisons. It answers 202 at once; GET
// /v1/comparisons/{id} then shows the combined answer as it is written. The
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
	conv, ok := conversationOf(req.Context)
	if !ok {
		writeError(w, http.StatusBadRequest, CodeBadRequest, msgBadContext)
		return
	}
	if sy := req.Synthesis; sy != nil && ((sy.Agent != "" && s.adapterFor(sy.Agent) == nil) || (sy.Tier != "" && !core.ValidTier(sy.Tier)) || badSynthModel(sy.Agent, sy.Model, sy.Effort)) {
		writeError(w, http.StatusBadRequest, CodeBadRequest, msgBadComparisonSynth)
		return
	}
	if _, err := s.askFolder(); err != nil {
		// The combined answer runs in the ask folder's judge folder.
		if s.o.Logger != nil {
			s.o.Logger.Error("ask folder", "err", err.Error())
		}
		writeError(w, http.StatusServiceUnavailable, CodeAskFolder, "the ask folder cannot be made on the PC")
		return
	}
	spec := compares.DirectSpec{
		Owner: deviceOf(r), Key: req.ClientComparisonID,
		Question: req.Question, Earlier: req.Earlier, Judges: req.Judges, Conversation: conv,
	}
	for _, a := range req.Answers {
		spec.Answers = append(spec.Answers, compares.DirectAnswer{ID: a.ID, Agent: a.Agent, Via: a.Via, Model: a.Model, Text: a.Text})
	}
	if sy := req.Synthesis; sy != nil {
		spec.Synth = &compares.SynthSpec{Off: sy.Off, Agent: sy.Agent, Tier: sy.Tier, Model: sy.Model, Effort: sy.Effort}
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
		writeError(w, http.StatusServiceUnavailable, core.ErrCodeNoJudge, "no AI on this PC writes combined answers now")
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

// retryComparison is POST /v1/comparisons/{id}/retry: a comparison that
// never took its answers (cancelled while it waited, or its run was gone)
// starts over (202). Any other ended comparison comes back as it is.
func (s *Server) retryComparison(w http.ResponseWriter, r *http.Request) {
	c, err := s.compares.Retry(deviceOf(r), r.PathValue("id"))
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, comparisonStatus(c))
	case errors.Is(err, compares.ErrNotFound):
		writeError(w, http.StatusNotFound, CodeComparisonNotFound, msgComparisonNotFound)
	case errors.Is(err, compares.ErrRunning):
		writeError(w, http.StatusConflict, CodeComparisonRunning, "the comparison or its combined answer is still waiting or running")
	default:
		writeError(w, http.StatusInternalServerError, CodeInternal, "the agent could not retry the comparison")
	}
}

// writeSynthesis is POST /v1/comparisons/{id}/synthesis: the combined
// answer of an ended comparison that failed or was off is written (202),
// with the body's AI or the one asked for before. A done one comes back as
// it is; 409 while the comparison or its combined answer waits or runs.
func (s *Server) writeSynthesis(w http.ResponseWriter, r *http.Request) {
	var req SynthesisRequest
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSynthesisBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, msgBadSynthesisBody)
		return
	}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, CodeBadRequest, msgBadSynthesisBody)
			return
		}
	}
	if req.Agent != "" && s.adapterFor(req.Agent) == nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, msgBadSynthesisBody)
		return
	}
	c, err := s.compares.Synthesize(deviceOf(r), r.PathValue("id"), req.Agent)
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, comparisonStatus(c))
	case errors.Is(err, compares.ErrNotFound):
		writeError(w, http.StatusNotFound, CodeComparisonNotFound, msgComparisonNotFound)
	case errors.Is(err, compares.ErrRunning):
		writeError(w, http.StatusConflict, CodeComparisonRunning, "the comparison or its combined answer is still waiting or running")
	default:
		writeError(w, http.StatusInternalServerError, CodeInternal, "the agent could not write the combined answer")
	}
}

// cancelComparison is POST /v1/comparisons/{id}/cancel: a comparison that
// waits for its run, or takes its answers, ends cancelled. A combined
// answer that waits or runs is cancelled too, also of an ended
// comparison. An ended comparison otherwise stays as it is.
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
