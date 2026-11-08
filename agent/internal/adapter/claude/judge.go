package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

// The one-off command of the judging days (P2-3 비교 판정, spec part 8):
// `claude -p` in the judge folder with the prompt on stdin. Unlike an ask,
// it gets no tool at all (--tools ""), loads no MCP server
// (--strict-mcp-config), keeps no conversation (--no-session-persistence)
// and runs without the user's hooks, plugins, skills and CLAUDE.md
// (--safe-mode; login, model choice and permissions stay as they are).
// --json-schema makes claude hand the JSON back as structured_output. No
// allow rule, no permission flag and no resume is ever passed, and the ask
// command is not touched. The judges went with C6d (#24); the combined
// answer and refining (synth.go) run this command, and this file keeps
// what they share: the company of a model and the output parser.
//
// The command goes through proc.Run with Runner.Path(), not Runner.Run:
// the Runner's checker refuses --strict-mcp-config, and proc.Run adds the
// stdin and the job object. checkSynthArgs checks the exact shape first.

// JudgeMinVersion is the oldest Claude Code the one-off command was tested
// with (P2-3 version table). The gate compares Info's version with it
// (core.VersionAtLeast).
const JudgeMinVersion = "2.1.314"

var _ core.Judger = (*Adapter)(nil)

// familyAliases are Claude Code's own model aliases; each picks an
// Anthropic model.
var familyAliases = map[string]bool{
	"haiku": true, "sonnet": true, "opus": true, "default": true, "best": true, "opusplan": true,
}

// Family returns "anthropic" for Claude Code's model aliases and for
// claude- model names, with or without the [1m] suffix, in any case. It
// returns "" for anything else, such as another provider's model a Claude
// Code was pointed at: a writer of family "" does not write (fail closed,
// core.Judger).
func (a *Adapter) Family(model string) string {
	m := strings.TrimSuffix(strings.ToLower(model), "[1m]")
	if familyAliases[m] || (strings.HasPrefix(m, "claude-") && len(m) > len("claude-")) {
		return "anthropic"
	}
	return ""
}

// judgeOutput is the one result object of the one-off command: an ask's,
// plus what --json-schema and the permission check add.
type judgeOutput struct {
	askOutput
	// Subtype is "success" even on most errors; claude says
	// error_max_structured_output_retries when it gave up on the schema.
	Subtype           string            `json:"subtype"`
	StructuredOutput  json.RawMessage   `json:"structured_output"`
	PermissionDenials []json.RawMessage `json:"permission_denials"`
}

const (
	unrecognizedModel = "[claude-code:unrecognized_model]"
	schemaGaveUp      = "error_max_structured_output_retries"
	// judgeKey marks the judging days' JSON, which must never become a
	// message.
	judgeKey = `"judgments"`
)

// parseJudgeOutput reads the last result object of a one-off run (a
// combined answer or a question spec). In order: a tool the AI reached
// for (permission_denials) throws the output away; an unknown model is
// bad_model; claude giving up on the schema is judge_bad_output; any other
// error is judge_failed with the AI's own words, taken from result only
// when there is no structured_output (so the JSON never shows as a
// message), else from stderr; no structured_output is judge_bad_output.
func parseJudgeOutput(res proc.Result, asked string) (core.JudgeResult, error) {
	r := core.JudgeResult{Model: asked}
	stderr := strings.TrimSpace(string(res.Stderr))
	out, found := lastJudgeResult(res.Stdout)
	if !found {
		if strings.Contains(stderr, unrecognizedModel) {
			return r, core.Fail(core.ErrCodeBadModel, asked)
		}
		if msg := aiMessage(stderr); msg != "" {
			return r, core.Fail(core.ErrCodeJudgeFailed, msg)
		}
		return r, core.Fail(core.ErrCodeJudgeFailed, fmt.Sprintf("claude ended with exit code %d and no result", res.ExitCode))
	}
	r.Model = answeringModel(out.ModelUsage, asked)
	r.InputTokens = out.Usage.InputTokens + out.Usage.CacheCreationTokens + out.Usage.CacheReadTokens
	r.OutputTokens = out.Usage.OutputTokens
	structured, hasStructured := structuredOutput(out.StructuredOutput)
	failed := out.IsError || res.ExitCode != 0
	switch {
	case len(out.PermissionDenials) > 0:
		return r, core.Fail(core.ErrCodeJudgeUsedTool, "")
	case failed && (strings.Contains(stderr, unrecognizedModel) || (out.APIErrorStatus != nil && *out.APIErrorStatus == 404)):
		return r, core.Fail(core.ErrCodeBadModel, asked)
	case failed && out.Subtype == schemaGaveUp:
		return r, core.Fail(core.ErrCodeJudgeBadOutput, "claude gave up on the JSON format")
	case failed:
		msg := ""
		if !hasStructured {
			msg = aiMessage(out.Result)
		}
		if msg == "" {
			msg = aiMessage(stderr)
		}
		if msg == "" {
			msg = fmt.Sprintf("claude reported an error (exit code %d)", res.ExitCode)
		}
		return r, core.Fail(core.ErrCodeJudgeFailed, msg)
	case !hasStructured:
		return r, core.Fail(core.ErrCodeJudgeBadOutput, "claude gave no structured output")
	}
	r.Output = structured
	return r, nil
}

// lastJudgeResult finds the last {"type":"result"} line of stdout.
func lastJudgeResult(stdout []byte) (judgeOutput, bool) {
	lines := bytes.Split(bytes.TrimSpace(stdout), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		var out judgeOutput
		if len(line) > 0 && line[0] == '{' && json.Unmarshal(line, &out) == nil && out.Type == "result" {
			return out, true
		}
	}
	return judgeOutput{}, false
}

// structuredOutput is the JSON of structured_output: an object as
// it is, a JSON string as its text. Absent, null or an empty string is
// none.
func structuredOutput(raw json.RawMessage) (json.RawMessage, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil || strings.TrimSpace(s) == "" {
			return nil, false
		}
		return json.RawMessage(s), true
	}
	return slices.Clone(raw), true
}

// aiMessage is s as judge_failed's Detail: on one line and at most
// JudgeMessageChars characters with the ellipsis. Text that holds the
// judging days' JSON is never a message.
func aiMessage(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.Contains(s, judgeKey) {
		return ""
	}
	return clip(s, core.JudgeMessageChars-1)
}
