package codex

// The one-off command of the judging days (P2-3, spec part 8): `codex exec`
// in the judge folder, read-only, with the person's config.toml left out,
// web search off, no session file (--ephemeral) and the JSON forced by
// --output-schema. Codex cannot run with zero tools: like an ask, the
// command turns off every feature that brings a tool (featuresOff,
// features.go), and what keeps the rest (apply_patch, MCP resource reads)
// harmless is the read-only sandbox, the left-out config, web search off
// and throwing away any result in which it reached for a tool. The judges
// went with C6d (#24); the combined answer and refining (synth.go) run
// this command, and this file keeps what they share: the company of a
// model, the command line and the event parser.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/proc"
)

// JudgeMinVersion is the oldest codex the one-off command was tested with
// (P2-3 version table). The gate compares Info's version with it through
// core.VersionAtLeast.
const JudgeMinVersion = "0.164.0"

var _ core.Judger = (*Adapter)(nil)

// Family names the company of a model codex runs (core.Judger): gpt-*,
// o<digit>* and codex-* are OpenAI's. Anything else is "", so a model it
// cannot place does not write (fail closed).
func (a *Adapter) Family(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "gpt-"), strings.HasPrefix(m, "codex-"),
		len(m) >= 2 && m[0] == 'o' && m[1] >= '0' && m[1] <= '9':
		return "openai"
	}
	return ""
}

// judgeArgs builds the one-off command: exec with no --search before it,
// read-only, the person's config left out, no session file, web search
// off, the schema from a file and the featuresOff features off
// (disableArgs). "-" reads the prompt from stdin.
func judgeArgs(model, effort, schema string) []string {
	args := []string{"exec", "-s", "read-only", "--ignore-user-config", "--skip-git-repo-check", "--ephemeral", "--json",
		"-m", model, "-c", `model_reasoning_effort="` + effort + `"`, "-c", `web_search="disabled"`,
		"--output-schema", schema}
	args = append(args, disableArgs()...)
	return append(args, "-")
}

// judgeItems are the items a one-off run may produce. Any other item, such
// as command_execution, file_change, mcp_tool_call or web_search, is a
// tool.
var judgeItems = map[string]bool{"agent_message": true, "reasoning": true, "error": true, "todo_list": true}

// judgeEvent is one JSONL line of `codex exec --json` as the parser of a
// one-off run reads it.
type judgeEvent struct {
	Type string `json:"type"`
	Item *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Message string `json:"message"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// parseJudgeOutput reads a one-off run's events. In this order: a tool item
// throws the result away (judge_used_tool); a failed turn or exit code is
// bad_model or judge_failed with codex's own message; no agent message is
// judge_bad_output; else Output is the last agent message. Model and the
// tokens come back with any error, Output never does.
func parseJudgeOutput(res proc.Result, model string) (core.JudgeResult, error) {
	r := core.JudgeResult{Model: model}
	msg, haveMsg := "", false
	tool, failed := false, false
	errMsg := ""
	for _, line := range bytes.Split(res.Stdout, []byte("\n")) {
		line = bytes.TrimSpace(line)
		var e judgeEvent
		if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &e) != nil {
			continue
		}
		switch e.Type {
		case "item.started", "item.updated", "item.completed":
			if e.Item == nil {
				continue
			}
			if !judgeItems[e.Item.Type] {
				tool = true
			}
			if e.Type == "item.completed" && e.Item.Type == "agent_message" {
				msg, haveMsg = e.Item.Text, true
			}
		case "turn.completed":
			if e.Usage != nil {
				r.InputTokens, r.OutputTokens = e.Usage.InputTokens, e.Usage.OutputTokens
			}
		case "turn.failed":
			failed = true
			if e.Error != nil && e.Error.Message != "" {
				errMsg = e.Error.Message
			}
		case "error":
			if errMsg == "" {
				errMsg = e.Message
			}
		}
	}
	switch {
	case tool:
		return r, core.Fail(core.ErrCodeJudgeUsedTool, "")
	case failed || res.ExitCode != 0:
		code, detail := core.ErrCodeJudgeFailed, errMsg
		if inner, c := apiError(errMsg); inner != "" || c != "" {
			if inner != "" {
				detail = inner
			}
			if c == "model_not_found" {
				code = core.ErrCodeBadModel
			}
		}
		if strings.TrimSpace(detail) == "" {
			detail = lastLines(strings.TrimSpace(string(res.Stderr)))
		}
		// judge_failed carries codex's words, never the judging days' JSON.
		if strings.TrimSpace(detail) == "" || strings.Contains(detail, `"judgments"`) {
			detail = "codex's turn failed with no message"
			if res.ExitCode != 0 {
				detail = fmt.Sprintf("codex ended with exit code %d and no output", res.ExitCode)
			}
		}
		// clip adds "…" after n characters: at most JudgeMessageChars in all.
		return r, core.Fail(code, clip(detail, core.JudgeMessageChars-1))
	case !haveMsg || strings.TrimSpace(msg) == "":
		return r, core.Fail(core.ErrCodeJudgeBadOutput, "")
	}
	r.Output = json.RawMessage(msg)
	return r, nil
}
