package api

import (
	"os"
	"strings"
	"unicode/utf8"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/judge"
)

// Asking the moderator (사회자에게 묻기, #25, 11-07): POST /v1/runs with
// moderator sends the question to the moderator AI alone, which answers
// with the ask command (web search on) as a one-off in the judge folder, so
// no session is kept or listed. It gets the run's context (the
// conversation record) and the previous turn's answers as data
// (judge.ModeratorAskPrompt). The run is then like a one-AI ask: no
// comparison and no refining.

// RunModerator is RunRequest.moderator: the moderator AI (its model and
// effort as set for it, the tier's when left out) and the previous turn's
// answers by AI. Unknown fields are ignored (principle 4).
type RunModerator struct {
	Agent   string            `json:"agent"`
	Model   string            `json:"model,omitempty"`
	Effort  string            `json:"effort,omitempty"`
	Answers []ModeratorAnswer `json:"answers,omitempty"`
}

// ModeratorAnswer is one answer a moderator ask carries: the AI program or
// app that wrote it, and its text.
type ModeratorAnswer struct {
	Agent string `json:"agent"`
	Text  string `json:"text"`
}

// ModeratorInfo is /v1/info's moderator: the agent asks the moderator,
// with these limits. The phone offers '사회자' only when it is there.
type ModeratorInfo struct {
	MaxAnswers     int    `json:"maxAnswers"`
	MaxAnswerBytes int    `json:"maxAnswerBytes"`
	Rules          string `json:"rules"`
}

// moderatorInfo is /v1/info's moderator: nil without an ask folder, as a
// debate's.
func (s *Server) moderatorInfo() *ModeratorInfo {
	if s.gate.Folder == "" {
		return nil
	}
	return &ModeratorInfo{MaxAnswers: judge.MaxModeratorAnswers, MaxAnswerBytes: judge.MaxModeratorAnswerBytes, Rules: judge.ModeratorRules}
}

// maxModeratorBody is what a moderator adds to a run's body: its answers,
// twice for JSON escapes, and room for the names.
const maxModeratorBody = 2*judge.MaxModeratorAnswers*judge.MaxModeratorAnswerBytes + 2<<10

const msgBadModerator = "moderator must be {agent, model, effort, answers} with an AI of this PC that can answer, a model and effort only together, and at most 6 answers {agent, text} of 8 KiB, all text"

// checkModerator reports whether m is within its limits.
func checkModerator(m *RunModerator) bool {
	if !agentNameRe.MatchString(m.Agent) || badSynthModel(m.Agent, m.Model, m.Effort) || len(m.Answers) > judge.MaxModeratorAnswers {
		return false
	}
	for _, a := range m.Answers {
		if !agentNameRe.MatchString(a.Agent) || len(a.Text) > judge.MaxModeratorAnswerBytes || !utf8.ValidString(a.Text) || strings.ContainsRune(a.Text, 0) {
			return false
		}
	}
	return true
}

// moderatorAnswers names the answers as the prompt does: an AI of this PC
// by its display name, a pasted app's answer by the app's name, else the
// code.
func (s *Server) moderatorAnswers(m *RunModerator) []judge.ModeratorAnswer {
	out := make([]judge.ModeratorAnswer, 0, len(m.Answers))
	for _, a := range m.Answers {
		if strings.TrimSpace(a.Text) == "" {
			continue
		}
		name := a.Agent
		if n, ok := s.adapterFor(a.Agent).(core.Named); ok && strings.TrimSpace(n.DisplayName()) != "" {
			name = strings.TrimSpace(n.DisplayName())
		} else if h := judge.HandoffName(a.Agent); h != "" {
			name = h
		}
		out = append(out, judge.ModeratorAnswer{AI: name, Text: a.Text})
	}
	return out
}

// moderatorFolder is the folder a moderator ask runs in: the judge folder,
// made if missing.
func (s *Server) moderatorFolder() (string, error) {
	if _, err := s.askFolder(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.gate.Folder, 0o700); err != nil {
		return "", err
	}
	return s.gate.Folder, nil
}
