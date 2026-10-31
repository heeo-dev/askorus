package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/proc"
)

// Judges (P2-3 비교 판정, spec part 8) run `claude -p` in the judge folder
// with the judge prompt on stdin. Unlike an ask, a judge gets no tool at
// all (--tools ""), loads no MCP server (--strict-mcp-config), keeps no
// conversation (--no-session-persistence) and runs without the user's
// hooks, plugins, skills and CLAUDE.md (--safe-mode; login, model choice
// and permissions stay as they are). --json-schema makes claude hand the
// judge JSON back as structured_output. No allow rule, no permission flag
// and no resume is ever passed, and the ask command is not touched.
//
// The command goes through proc.Run with Runner.Path(), not Runner.Run:
// the Runner's checker refuses --strict-mcp-config, and proc.Run adds the
// stdin and the job object. checkJudgeArgs checks the exact shape first.

// JudgeMinVersion is the oldest Claude Code the judge command was tested
// with (P2-3 version table). S3's version gate compares Info's version
// with it (core.VersionAtLeast).
const JudgeMinVersion = "2.1.314"

// judgeDefault is the judge model when the settings name none (spec part
// 8, until stage 1 of 판정 검증 sets it).
var judgeDefault = core.AskModel{Model: "haiku", Effort: "medium"}

// judgeTimeout is the time limit Judge sets itself (core.Judger); a
// variable so a test can shorten it.
var judgeTimeout = core.JudgeTimeout

var _ core.Judger = (*Adapter)(nil)

// JudgeDefault returns haiku at medium effort (core.Judger).
func (a *Adapter) JudgeDefault() core.AskModel { return judgeDefault }

// familyAliases are Claude Code's own model aliases; each picks an
// Anthropic model.
var familyAliases = map[string]bool{
	"haiku": true, "sonnet": true, "opus": true, "default": true, "best": true, "opusplan": true,
}

// Family returns "anthropic" for Claude Code's model aliases and for
// claude- model names, with or without the [1m] suffix, in any case. It
// returns "" for anything else, such as another provider's model a Claude
// Code was pointed at: internal/judge gives a family of "" no pair (fail
// closed, core.Judger).
func (a *Adapter) Family(model string) string {
	m := strings.TrimSuffix(strings.ToLower(model), "[1m]")
	if familyAliases[m] || (strings.HasPrefix(m, "claude-") && len(m) > len("claude-")) {
		return "anthropic"
	}
	return ""
}

// judgeArgs builds the command line of one judge. The prompt is not in it;
// on Windows Go passes "" as an empty argument ("").
func judgeArgs(model, effort string) []string {
	return []string{"-p", "--output-format", "json", "--model", model, "--effort", effort,
		"--tools", "", "--strict-mcp-config", "--no-session-persistence", "--safe-mode",
		"--json-schema", core.JudgeSchemaV1}
}

// judgeArgCount is len(judgeArgs(…)).
const judgeArgCount = 14

// checkJudgeArgs accepts exactly what judgeArgs makes for a valid model
// and effort: the schema byte for byte, and nothing added, missing or
// moved, so no permission flag, tool, MCP config, resume or prompt
// argument can reach claude through a judge.
func checkJudgeArgs(args []string) error {
	if len(args) != judgeArgCount {
		return fmt.Errorf("%w: a judge has %d arguments, not %d", ErrArgRejected, judgeArgCount, len(args))
	}
	if !config.ValidModel(args[4]) || !config.ValidEffort(args[6]) {
		return fmt.Errorf("%w: a judge needs a valid model and effort", ErrArgRejected)
	}
	if !slices.Equal(args, judgeArgs(args[4], args[6])) {
		return fmt.Errorf("%w: not the judge command", ErrArgRejected)
	}
	return nil
}

// Judge runs one judge request with claude -p (core.Judger). The result
// carries Model and the tokens with any error once claude ran; Output is
// set only without an error, and Cleanup is always empty (claude keeps
// no conversation).
func (a *Adapter) Judge(ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
	if a.runner == nil {
		return core.JudgeResult{}, core.Fail(core.ErrCodeAgentUnavailable, "claude was not found")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return core.JudgeResult{}, core.Fail(core.ErrCodeInternal, "the judge prompt is empty")
	}
	if info, err := os.Stat(req.Folder); !filepath.IsAbs(req.Folder) || err != nil || !info.IsDir() {
		return core.JudgeResult{}, core.Fail(core.ErrCodeInternal, "the judge folder is not an absolute existing folder")
	}
	def := a.JudgeDefault()
	model, effort := firstNonEmpty(req.Model, def.Model), firstNonEmpty(req.Effort, def.Effort)
	if !config.ValidModel(model) {
		return core.JudgeResult{}, core.Fail(core.ErrCodeBadModel, model)
	}
	if !config.ValidEffort(effort) {
		return core.JudgeResult{}, core.Fail(core.ErrCodeBadModel, "effort "+effort)
	}
	args := judgeArgs(model, effort)
	if err := checkJudgeArgs(args); err != nil {
		return core.JudgeResult{}, core.Fail(core.ErrCodeInternal, err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, judgeTimeout)
	defer cancel()
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.runner.Path(), Dir: req.Folder, Args: args, Stdin: req.Prompt})
	r := core.JudgeResult{Model: model}
	switch {
	case errors.Is(err, proc.ErrTimeout), err != nil && ctx.Err() != nil:
		// A context that ended before claude even started gives its own
		// error, not ErrTimeout.
		return r, core.Fail(core.ErrCodeJudgeTimeout, "")
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, proc.ErrScript):
		return core.JudgeResult{}, core.Fail(core.ErrCodeAgentUnavailable, "claude was not found")
	case err != nil:
		return r, core.Fail(core.ErrCodeJudgeFailed, "claude could not run")
	}
	return parseJudgeOutput(res, model)
}

// judgeOutput is the one result object of a judge: an ask's, plus what
// --json-schema and the permission check add.
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
	// judgeKey marks the judge JSON, which must never become a message.
	judgeKey = `"judgments"`
)

// parseJudgeOutput reads the last result object of a judge run. In order:
// a tool the judge reached for (permission_denials) throws the output
// away; an unknown model is bad_model; claude giving up on the schema is
// judge_bad_output; any other error is judge_failed with the AI's own
// words, taken from result only when there is no structured_output (so
// the judge JSON never shows as a message), else from stderr; no
// structured_output is judge_bad_output.
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
		return r, core.Fail(core.ErrCodeJudgeBadOutput, "claude gave up on the judge format")
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

// structuredOutput is the judge JSON of structured_output: an object as
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
// judge JSON is never a message.
func aiMessage(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.Contains(s, judgeKey) {
		return ""
	}
	return clip(s, core.JudgeMessageChars-1)
}
