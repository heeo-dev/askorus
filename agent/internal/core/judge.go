package core

import (
	"context"
	"encoding/json"
)

// Comparison judging (비교 판정, P2-3): when two or more AIs answer one
// question, an AI that wrote neither of two answers, and does not work for
// either answer's company, judges unit by unit whether the other answer
// says the same, the opposite or nothing about it. The design is "P2-3 비교
// 판정 설계" in docs/phase2-plan.md. internal/judge holds the logic that
// knows no AI; each adapter owns its own judge command (principle 2).

// Judger is implemented by engines that can judge: adapters, through
// their AI's CLI, and later API engines. It is apart from Asker, whose
// checkers require the web tools: a judge gets no tools, keeps no
// conversation and never carries one on.
type Judger interface {
	// Judge runs one judge request to its end and returns the judge's
	// output unchecked; internal/judge validates it. CLI engines send
	// req.Prompt on stdin, API engines build their call from req.Task.
	Judge(ctx context.Context, req JudgeRequest) (JudgeResult, error)
	// JudgeDefault is the model and effort the AI judges with when the
	// settings name none. Stage 1 of 판정 검증 sets it.
	JudgeDefault() AskModel
	// Family names the company of a model this AI program runs, in lower
	// case: anthropic, openai, google, deepseek, moonshot, zhipu, alibaba.
	// A judge never judges an answer of its own company. It returns ""
	// when it cannot tell the company, never a provider's or a placeholder
	// name ("openrouter", "unknown"): internal/judge trusts any name it
	// gets as a company other than the rest, and gives a judge or an
	// answer of family "" no pair (fail closed).
	Family(model string) string
}

// JudgeCleaner is implemented by engines whose judging leaves something
// behind that can be removed: OpenCode keeps every run as a conversation.
// The caller collects JudgeResult.Cleanup of one AI's requests and calls
// CleanupJudge once, after that AI's last request of a comparison ended.
type JudgeCleaner interface {
	// CleanupJudge removes what the judge calls with these refs left in
	// folder, and nothing it did not make itself.
	CleanupJudge(ctx context.Context, folder string, refs []string) error
}

// JudgeTask is one judge request as data: the question, the units of the
// answer to judge and the other answer, whose units the judge cites as
// evidence. internal/judge renders it as the prompt and checks the output
// against it, so the product, the verification tool and API engines all
// work from the same task.
type JudgeTask struct {
	Question string      // the question as the person wrote it
	Earlier  []string    // earlier questions of a follow-up, oldest first; context only
	Units    []JudgeUnit // ANSWER A, the units to judge: a1, a2, …
	Other    []JudgeUnit // ANSWER B, the other answer: b1, b2, …
	Shape    string      // "directed"; "pair" (both directions in one request) only in judge-eval
	Labels   string      // "v1": same, opposite or absent, plus unsure (JudgeSchemaV1); "v1four" only in judge-eval
}

// JudgeUnit is one line of an answer as a judge sees it. A unit has an id
// and its text, and Context holds its table header (Kind "row") or the
// start of its parent list item (Kind "item"). A context line, such as a
// heading or code that is not judged, has no id and only Context.
type JudgeUnit struct {
	ID      string // "a3" or "b3", for unit 3 of its answer; empty for a context line
	Kind    string // sentence, item, row or quote; empty for a context line
	Text    string
	Context string
}

// JudgeRequest is one judge call.
type JudgeRequest struct {
	Folder string    // the judge folder, <ask folder>\judge: absolute, exists, outside any project
	Task   JudgeTask // the task as data, for API engines
	Prompt string    // the task as the judge-v1 prompt, for CLI engines; sent on stdin, never as an argument
	Model  string    // empty: JudgeDefault's
	Effort string    // empty: JudgeDefault's
}

// JudgeResult is one judge's output. Output is untrusted and never logged.
type JudgeResult struct {
	// Output is the judge's JSON as the AI gave it: Claude's
	// structured_output, Codex's last agent_message, or OpenCode's last
	// text, which may sit in a code fence or among other words.
	Output json.RawMessage
	Model  string // the model that judged, as the AI names it
	// Tokens the AI reports, for counting use; zero when it reports none.
	InputTokens  int
	OutputTokens int
	// Cleanup is an opaque reference for JudgeCleaner (OpenCode: the
	// conversation's id); empty when nothing is left behind.
	Cleanup string
}

// Judge error codes, on one request (requests[].error) or on a whole
// comparison (error). Adapters return judge_failed, judge_timeout,
// judge_bad_output and judge_used_tool as ActionError codes, and
// ErrCodeBadModel and ErrCodeAgentUnavailable as asks do; internal/judge
// and the comparisons give the rest. An ActionError's Error() carries its
// Detail, and judge_failed's Detail is the judging AI's own words, which
// may quote the answers: the comparison turns an adapter's Judge error
// into a judge.Error (Code, and Detail as Message) at once, and logs only
// the code, never the error value (spec part 6, 로그).
const (
	ErrCodeJudgeFailed        = "judge_failed"         // the AI ran and reported an error; Detail is its message, at most 300 characters
	ErrCodeJudgeTimeout       = "judge_timeout"        // no output within 2 minutes, or 5 minutes in the queue
	ErrCodeJudgeBadOutput     = "judge_bad_output"     // the output is not the judge JSON, also when asked again
	ErrCodeJudgeUsedTool      = "judge_used_tool"      // the judge reached for a tool; its output is thrown away
	ErrCodeNoJudge            = "no_judge"             // no AI may judge the pair: the others wrote an answer, work for an answer's company or cannot judge
	ErrCodeNeedTwoAnswers     = "need_two_answers"     // fewer than two answers to compare
	ErrCodeTooManyComparisons = "too_many_comparisons" // 8 comparisons are still unfinished
)

// JudgeSchemaV1 is the JSON schema of a judge's output with labels v1.
// It is written by hand: Go sorts map keys, which would put label before
// reason, and the order id, core, evidence, reason, label, unsure makes
// the judge pick and explain before it concludes. Claude gets it with
// --json-schema and Codex reads the same bytes from a file. Every object
// requires all its properties and allows no others, as Codex's strict
// mode wants.
const JudgeSchemaV1 = `{"type":"object","properties":{"judgments":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"core":{"type":"boolean"},"evidence":{"type":"array","items":{"type":"string"}},"reason":{"type":"string"},"label":{"type":"string","enum":["same","opposite","absent"]},"unsure":{"type":"boolean"}},"required":["id","core","evidence","reason","label","unsure"],"additionalProperties":false}}},"required":["judgments"],"additionalProperties":false}`
