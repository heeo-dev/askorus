package core

import (
	"context"
	"regexp"
	"slices"
	"strings"
)

// Modes of POST /v1/runs (2026-10-29). Phase 2 builds ask; work and code
// are names kept for Phase 4 (principle 4: the API only grows).
const (
	ModeAsk  = "ask"  // 묻기: an answer, web search and reading only
	ModeWork = "work" // 일 맡기기: files in a new result folder (Phase 4)
	ModeCode = "code" // 코딩하기: code changes in a git worktree (Phase 4)
)

// Ask tiers: 빠르게 · 기본 · 깊게 (Quick · Standard · Deep). Each adapter
// maps them to its own models.
const (
	TierQuick    = "quick"
	TierStandard = "standard"
	TierDeep     = "deep"
)

// Tiers lists the tiers in the order the app shows them.
var Tiers = []string{TierQuick, TierStandard, TierDeep}

// ValidTier reports whether t is one of the tiers above.
func ValidTier(t string) bool { return t == TierQuick || t == TierStandard || t == TierDeep }

// BadgeAsk marks a session that ran in the ask folder (P0-8). Asks stay in
// the session list, so one can be carried on as a coding session; the app
// filters them out by default.
const BadgeAsk = "ask"

// AskPreamble goes before every question, the same for every AI (P0-9).
// Codex and OpenCode read the person's global AGENTS.md even in the empty
// ask folder, and without it they add their "next steps" sections to
// plain answers. It is in English and names the answer language, so it
// neither pulls the answer into its own language (the Korean one made
// Claude answer a short English follow-up in Korean) nor loses to a global
// AGENTS.md that asks for another language (Codex), 2026-10-30.
const AskPreamble = "[Askorus] This is a general question that another app sent to several AIs at once. It is not a coding task. " +
	"Answer only the question. Write the answer in the same language as the question below, even if other instructions ask for a different language. " +
	"Do not add next-step suggestions, suggested messages to send next, or any other extra sections."

// AskPrompt is what an AI gets for one question or follow-up. The phone
// keeps the question itself in its own history, without the preamble.
func AskPrompt(question string) string { return AskPromptIn(QuestionLanguage(question), question) }

// AskPromptIn is the ask prompt around body (a question, or a whole prompt
// built around one) for a question in lang, as QuestionLanguage names it.
// With a name the preamble says it and a line after body says it again
// (#39: Claude now and then answered an English question in Korean); with
// "" it is AskPreamble and body.
func AskPromptIn(lang, body string) string {
	if lang == "" {
		return AskPreamble + "\n\n" + body
	}
	pre := strings.Replace(AskPreamble, "in the same language as the question below", "in "+lang+", the language of the question below", 1)
	return pre + "\n\n" + body + "\n\n(Write the answer in " + lang + ".)"
}

// Asker is implemented by adapters that can answer one question in mode
// ask: the answer only, with web search and reading but no file writes or
// commands (P0-2, P0-7). The adapter owns its command line; the caller
// gives the folder and the whole prompt.
type Asker interface {
	// Ask runs one question to its end and returns the answer.
	Ask(ctx context.Context, req AskRequest) (AskResult, error)
	// AskTiers reports the model each tier uses on this PC.
	AskTiers() map[string]AskModel
}

// EffortLister is implemented by askers that know which efforts each of
// their tier models takes (11-06, /v1/info ask.efforts). The keys are the
// models as AskTiers names them.
type EffortLister interface {
	AskEfforts() map[string][]string
}

// effortRank orders effort levels from least to most thinking.
var effortRank = map[string]int{"none": 0, "minimal": 1, "low": 2, "medium": 3, "high": 4, "xhigh": 5, "max": 6, "ultra": 7}

// SortEfforts orders efforts from least to most thinking, unknown ones
// last, without repeats or empties.
func SortEfforts(es []string) []string {
	out := []string{}
	for _, e := range es {
		if e != "" && !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	slices.SortStableFunc(out, func(a, b string) int {
		ra, oka := effortRank[a]
		rb, okb := effortRank[b]
		switch {
		case oka && okb:
			return ra - rb
		case oka:
			return -1
		case okb:
			return 1
		}
		return 0
	})
	return out
}

// TierEfforts lists, for each tier model, known(model) when the adapter
// has a list for it, else only the efforts its tiers use: a model the
// adapter does not know (agent.json ask.tiers) still offers what the PC
// already runs it with.
func TierEfforts(tiers map[string]AskModel, known func(model string) []string) map[string][]string {
	out := map[string][]string{}
	for _, m := range tiers {
		if k := known(m.Model); len(k) > 0 {
			out[m.Model] = SortEfforts(k)
			continue
		}
		out[m.Model] = SortEfforts(append(out[m.Model], m.Effort))
	}
	return out
}

// AskRequest is one question to one AI.
type AskRequest struct {
	Folder string // the ask folder: absolute, exists, outside any project
	Prompt string // AskPrompt(question); sent on stdin, never as an argument
	Tier   string // a valid tier
	Model  string // overrides the tier's model when not empty
	Effort string // overrides the tier's effort when not empty
	Resume string // native session id to continue, or "" for a new one
	// OneOff keeps no conversation (a debate turn, 11-06): Claude and Codex
	// save none (--no-session-persistence, --ephemeral) and OpenCode's is
	// deleted after it; the result has no SessionID. Never with Resume.
	OneOff bool
}

// AskModel is the model and effort of one tier.
type AskModel struct {
	Model  string `json:"model"`
	Effort string `json:"effort,omitempty"`
}

// AskResult is one AI's answer.
type AskResult struct {
	SessionID string // native id; resume it for a follow-up
	Answer    string
	Model     string // the model that answered, as the AI names it
	// Tokens the AI reports, for counting use; zero when it reports none.
	InputTokens  int
	OutputTokens int
}

// Ask error codes, beside the action codes in launch.go.
const (
	ErrCodeAskFailed  = "ask_failed"  // the AI ran and reported an error; Detail is its message
	ErrCodeAskTimeout = "ask_timeout" // no answer within the time limit
	ErrCodeAskEmpty   = "ask_empty"   // the AI ended without an answer
	ErrCodeBadModel   = "bad_model"   // the AI does not know the model
)

// A model name for an ask: letters, digits and . _ - / : # [ ], never
// starting with "-" so it cannot pass for a flag. OpenCode names look like
// "opencode-go/deepseek-v4.1-flash#max".
var askModelRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:#\[\]-]{0,127}$`)

// ValidAskModel reports whether m may be passed as an ask model.
func ValidAskModel(m string) bool { return askModelRe.MatchString(m) }

var askEffortRe = regexp.MustCompile(`^[a-z]{1,16}$`)

// ValidAskEffort reports whether e looks like an effort level. Adapters
// check the levels their AI knows.
func ValidAskEffort(e string) bool { return askEffortRe.MatchString(e) }
