package core

import (
	"context"
	"regexp"
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
// plain answers.
const AskPreamble = "[Asksemble] 이 질문은 다른 앱이 여러 AI에게 같이 보낸 일반 질문입니다. 코딩 작업이 아닙니다. " +
	"질문에만 답하고, 다음에 할 일 제안·추천 문장·'다음에 쓸 문장' 같은 덧붙임 절은 쓰지 마세요."

// AskPrompt is what an AI gets for one question or follow-up. The phone
// keeps the question itself in its own history, without the preamble.
func AskPrompt(question string) string { return AskPreamble + "\n\n" + question }

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

// AskRequest is one question to one AI.
type AskRequest struct {
	Folder string // the ask folder: absolute, exists, outside any project
	Prompt string // AskPrompt(question); sent on stdin, never as an argument
	Tier   string // a valid tier
	Model  string // overrides the tier's model when not empty
	Effort string // overrides the tier's effort when not empty
	Resume string // native session id to continue, or "" for a new one
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
