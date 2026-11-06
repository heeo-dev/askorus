package fakecli

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
)

// The debate (토론, P2-5, #26). The moderator's calls are synth calls whose
// prompt is a debate prompt: the fake plans one debated point between the
// first two AIs of the prompt's answers (mode two adds a taste point for
// the last AI), closes every check with DONE | agreed (mode next asks the
// first AI one more turn at every check that may), and writes
// DebateAfterText after the debate. Mode none plans NONE. A debater's turn
// is an ask whose prompt is a debate turn: DebateTurnText and the role,
// then STANCE: keep; the ask modes (fail, empty) apply to it as to any
// ask, and the answers file does not.

// EnvDebate is the debate mode: "" (one point), "two", "next" or "none".
const EnvDebate = "ASKSEMBLE_FAKE_DEBATE"

// Debate modes of ASKSEMBLE_FAKE_DEBATE.
const (
	DebateOne  = ""
	DebateTwo  = "two"
	DebateNext = "next"
	DebateNone = "none"
)

// Fixed texts of the fake debate, for tests to compare against.
const (
	DebateAfterText = "Fake answer after the debate."
	DebateLine      = "Fake outcome line."
	DebateTurnText  = "Fake turn of "
	DebateNextAsk   = "One more fake question?"
)

var debateAnswerAI = regexp.MustCompile(`(?m)^ANSWER: \{"ai":"([^"]+)"`)

// debateReply is the moderator's reply to a debate prompt, and whether
// prompt was one.
func debateReply(prompt string) (string, bool) {
	var text string
	switch {
	case strings.HasPrefix(prompt, "[Asksemble debate-plan v2]"), strings.HasPrefix(prompt, "[Asksemble debate-plan v3]"):
		text = debatePlan(prompt)
	case strings.HasPrefix(prompt, "[Asksemble debate-check v1]"):
		text = "DONE | agreed | " + DebateLine
		if os.Getenv(EnvDebate) == DebateNext && !strings.Contains(prompt, "This is the last check") {
			names := debateTurnAIs(prompt)
			if len(names) > 0 {
				text = "NEXT | " + names[0] + " | " + DebateNextAsk
			}
		}
	case strings.HasPrefix(prompt, "[Asksemble synthesis debate-v4]"), strings.HasPrefix(prompt, "[Asksemble synthesis debate-v5]"):
		text = DebateAfterText
	default:
		return "", false
	}
	b, _ := json.Marshal(map[string]string{"answer": text})
	return string(b), true
}

// debatePlan plans from the prompt's answers' names.
func debatePlan(prompt string) string {
	if os.Getenv(EnvDebate) == DebateNone {
		return "NONE"
	}
	var names []string
	for _, m := range debateAnswerAI.FindAllStringSubmatch(prompt, -1) {
		names = append(names, m[1])
	}
	if len(names) < 2 {
		return "NONE"
	}
	a, b := names[0], names[1]
	lines := []string{
		"POINT | p1 | fact | yes | Fake point",
		"SIDE | p1 | " + a + " | one",
		"SIDE | p1 | " + strings.Join(names[1:], ", ") + " | two",
		"GOAL | p1 | the fake goal",
		"TURN | p1 | " + a + " | Fake question to " + a + "?",
		"TURN | p1 | " + b + " | Fake question to " + b + "?",
	}
	if os.Getenv(EnvDebate) == DebateTwo {
		last := names[len(names)-1]
		lines = append(lines,
			"POINT | p2 | taste | yes | Fake taste point",
			"SIDE | p2 | "+a+" | A",
			"SIDE | p2 | "+last+" | B",
			"TURN | p2 | "+last+" | Combine the two?")
	}
	return strings.Join(lines, "\n")
}

var debateTurnAI = regexp.MustCompile(`(?m)^MODERATOR TO ([^:]+):`)

// debateTurnAIs are the AIs that spoke in a check prompt, in order.
func debateTurnAIs(prompt string) []string {
	var out []string
	for _, m := range debateTurnAI.FindAllStringSubmatch(prompt, -1) {
		out = append(out, m[1])
	}
	return out
}

// debateTurn is a debater's reply to a debate turn's ask prompt, and
// whether the prompt was one.
func debateTurn(role, prompt string) (string, bool) {
	if !strings.Contains(prompt, "[Asksemble debate-turn v1]") {
		return "", false
	}
	return DebateTurnText + role + ".\nSTANCE: keep", true
}
