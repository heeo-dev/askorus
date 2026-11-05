package core

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"
)

// What is left of comparison judging (비교 판정, P2-3). Until 2026-11-05
// the AIs judged each other's answers unit by unit; since C6 (#19) the
// moderator alone writes the combined answer, and C6d (#24) removed the
// judging. Its one-off command (no tools, no MCP, no saved conversation,
// the prompt on stdin) stays: the combined answer (Synthesizer) and
// refining run it, in the judge folder, with the judge's error codes as
// wire values. The names are the judging days'.

// Judger is implemented by adapters that can name the company of a model
// they run. The gate lets only such an AI write combined answers, and a
// comparison's answers carry it. The name is the judging days'.
type Judger interface {
	// Family names the company of a model this AI program runs, in lower
	// case: anthropic, openai, google, deepseek, moonshot, zhipu, alibaba.
	// It returns "" when it cannot tell the company, never a provider's or
	// a placeholder name ("openrouter", "unknown"): a writer of family ""
	// does not write (fail closed).
	Family(model string) string
}

// JudgeCleaner is implemented by engines whose one-off calls leave
// something behind that can be removed: OpenCode keeps every run as a
// conversation. The caller collects the Cleanup refs of one AI's calls
// and calls CleanupJudge once, after that AI's last call of a comparison
// ended.
type JudgeCleaner interface {
	// CleanupJudge removes what the one-off calls with these refs left in
	// folder, and nothing it did not make itself.
	CleanupJudge(ctx context.Context, folder string, refs []string) error
}

// JudgeResult is what an adapter's output parser reads from one run of
// the one-off command; Synthesize turns it into a SynthResult. Output is
// untrusted and never logged.
type JudgeResult struct {
	// Output is the JSON as the AI gave it: Claude's structured_output,
	// Codex's last agent_message, or OpenCode's last text, which may sit
	// in a code fence or among other words.
	Output json.RawMessage
	Model  string // the model that answered, as the AI names it
	// Tokens the AI reports, for counting use; zero when it reports none.
	InputTokens  int
	OutputTokens int
	// Cleanup is an opaque reference for JudgeCleaner (OpenCode: the
	// conversation's id); empty when nothing is left behind.
	Cleanup string
}

// Error codes of the one-off calls and of comparisons, on a combined
// answer (synthesis.error), a comparison (error) or an API reply. They
// are wire values from the judging days and keep their names. Adapters
// return judge_failed, judge_timeout, judge_bad_output and judge_used_tool
// as ActionError codes, and ErrCodeBadModel, ErrCodeAgentUnavailable and
// ErrCodeInternal as asks and the API do; internal/judge and the
// comparisons give the rest. An ActionError's Error() carries its Detail,
// and judge_failed's Detail is the AI's own words, which may quote the
// answers: the comparison turns an adapter's error into a judge.Error
// (Code, and Detail as Message) at once, and logs only the code, never the
// error value.
const (
	ErrCodeJudgeFailed        = "judge_failed"         // the AI ran and reported an error; Detail is its message, at most 300 characters
	ErrCodeJudgeTimeout       = "judge_timeout"        // no output within the time limit, or 5 minutes in the queue
	ErrCodeJudgeBadOutput     = "judge_bad_output"     // the output is not the JSON asked for, also when asked again
	ErrCodeJudgeUsedTool      = "judge_used_tool"      // the AI reached for a tool; its output is thrown away
	ErrCodeNoJudge            = "no_judge"             // no AI on this PC may write the combined answer
	ErrCodeNeedTwoAnswers     = "need_two_answers"     // fewer than two answers to compare
	ErrCodeTooManyComparisons = "too_many_comparisons" // 8 comparisons are still unfinished
	// ErrCodeCancelled is a call the comparison's cancel dropped while it
	// waited in the queue, and a waiting comparison that was cancelled
	// before its run ended (POST /v1/comparisons/{id}/cancel). A call that
	// already runs finishes, but is not asked again.
	ErrCodeCancelled = "cancelled"
	// ErrCodeInternal is the agent's own fault, never the AI's: an empty
	// prompt, or a schema file that changed after the agent wrote it. It
	// is api.CodeInternal.
	ErrCodeInternal = "internal"
)

// Limits of the one-off calls and the judge folder.
const (
	// JudgeCleanupTimeout bounds one OpenCode session list or delete.
	JudgeCleanupTimeout = 30 * time.Second
	// JudgeMessageChars bounds judge_failed's Detail, in characters.
	JudgeMessageChars = 300
	// JudgeFolderName is the judge folder inside the ask folder.
	JudgeFolderName = "judge"
)

// JudgeFolder is where the one-off calls run: <ask folder>\judge, an empty
// folder outside every project like the ask folder. Its sessions are
// hidden from GET /v1/sessions and the CLI sessions command, and an ask
// never carries a conversation of it on. It is "" when askFolder is "".
func JudgeFolder(askFolder string) string {
	if askFolder == "" {
		return ""
	}
	return filepath.Join(filepath.Clean(askFolder), JudgeFolderName)
}
