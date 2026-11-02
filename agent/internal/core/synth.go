package core

import (
	"context"
	"encoding/json"
	"time"
)

// Combined answers (종합 답변, P2-3 S5b, decision 7): once a comparison's
// judges are done, one AI reads the question, every answer with its AI's
// name and the comparison's results, and writes one combined answer in
// Markdown that names the AIs wherever they differ. The design is part 20
// of "P2-3 비교 판정 설계" in docs/phase2-plan.md. internal/judge renders
// the prompt and checks the output; each adapter owns its own command
// (principle 2), the same locked-down command as its judge with another
// model and schema.

// Synthesizer is implemented by engines that can write a combined answer:
// adapters, through their AI's CLI. It is apart from Judger and Asker. Like
// a judge, the writer gets no tools and no web, keeps no conversation
// (OpenCode's is deleted with JudgeCleaner) and never carries one on.
type Synthesizer interface {
	// Synthesize runs one combined-answer request to its end and returns
	// the writer's output unchecked; internal/judge's ValidateSynth checks
	// it. CLI engines send req.Prompt on stdin, never as an argument.
	//
	// Synthesize applies SynthTimeout itself (context.WithTimeout on ctx),
	// timed from the call, which the caller makes once the request has its
	// slot; a shorter deadline of ctx still wins. It returns the
	// SynthResult it has, Model, tokens and Cleanup, together with any
	// error, and the caller collects Cleanup whatever the error. An error
	// is an *ActionError with the judge codes: judge_failed (Detail is the
	// AI's own words, at most JudgeMessageChars characters), judge_timeout,
	// judge_bad_output, judge_used_tool (Output is then empty), bad_model,
	// agent_unavailable or internal. Adapters never log: the caller logs
	// the code only.
	Synthesize(ctx context.Context, req SynthRequest) (SynthResult, error)
}

// SynthRequest is one combined-answer call.
type SynthRequest struct {
	Folder string // the judge folder: absolute, exists, outside any project
	Prompt string // the synth-v1 prompt; sent on stdin, never as an argument
	Model  string // the writer's ask tier model; never empty
	Effort string // its effort; "" lets the adapter pick its command's default
}

// SynthResult is one writer's output. Output is untrusted and never logged.
type SynthResult struct {
	// Output is the writer's JSON as the AI gave it: Claude's
	// structured_output, Codex's last agent_message, or OpenCode's last
	// text, which may sit in a code fence or among other words.
	Output json.RawMessage
	Model  string // the model that wrote it, as the AI names it
	// Tokens the AI reports, for counting use; zero when it reports none.
	InputTokens  int
	OutputTokens int
	// Cleanup is an opaque reference for JudgeCleaner (OpenCode: the
	// conversation's id); empty when nothing is left behind.
	Cleanup string
}

// Combined-answer limits.
const (
	// SynthTimeout bounds one combined-answer run, timed from when it got
	// its slot. Every adapter's Synthesize applies it itself.
	SynthTimeout = 3 * time.Minute
	// SynthSchemaFile names the combined-answer schema file (SynthSchemaV1)
	// in the agent folder, next to JudgeSchemaFile: codex reads
	// --output-schema from a file only, and uninstall removes it.
	SynthSchemaFile = "synth-schema-v1.json"
)

// SynthSchemaV1 is the JSON schema of a writer's output: one object with
// the combined answer as a string, nothing else. It is written by hand,
// as JudgeSchemaV1 is; Claude gets it with --json-schema and Codex reads
// the same bytes from SynthSchemaFile. Its object requires its property
// and allows no other, as Codex's strict mode wants.
const SynthSchemaV1 = `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`

// Named is implemented by adapters that have a name for people: the name
// a combined answer gives the AI's answer ("Claude", "Codex",
// "OpenCode"). Without it the agent code is used.
type Named interface {
	DisplayName() string
}
