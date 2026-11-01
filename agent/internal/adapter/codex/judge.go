package codex

// Comparison judging with codex (P2-3, spec part 8): `codex exec` in the
// judge folder, read-only, with the person's config.toml left out, web
// search off, no session file (--ephemeral) and the judge JSON forced by
// --output-schema. Codex cannot run with zero tools: like an ask, the judge
// turns off every feature that brings a tool (featuresOff, features.go),
// and what keeps the rest (apply_patch, MCP resource reads) harmless is the
// read-only sandbox, the left-out config, web search off and throwing away
// any result in which it reached for a tool.

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

// JudgeMinVersion is the oldest codex the judge command was tested with
// (P2-3 version table). S3's version gate compares Info's version with it
// through core.VersionAtLeast.
const JudgeMinVersion = "0.164.0"

// schemaName is the judge schema file in the agent folder; codex reads
// --output-schema from a file only, and uninstall removes it.
const schemaName = core.JudgeSchemaFile

// judgeDefault is the judge model when the settings name none (spec part
// 8, 기본값). Stage 1 of 판정 검증 may change it.
var judgeDefault = core.AskModel{Model: "gpt-6-luna", Effort: "medium"}

// judgeTimeout is the time limit Judge sets itself (core.Judger); a
// variable so a test can shorten it.
var judgeTimeout = core.JudgeTimeout

var _ core.Judger = (*Adapter)(nil)

// JudgeDefault returns the model and effort codex judges with (core.Judger).
func (a *Adapter) JudgeDefault() core.AskModel { return judgeDefault }

// Family names the company of a model codex runs (core.Judger): gpt-*,
// o<digit>* and codex-* are OpenAI's. Anything else is "", so a model it
// cannot place gets no pair (fail closed).
func (a *Adapter) Family(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "gpt-"), strings.HasPrefix(m, "codex-"),
		len(m) >= 2 && m[0] == 'o' && m[1] >= '0' && m[1] <= '9':
		return "openai"
	}
	return ""
}

// judgeArgs builds one judge: exec with no --search before it, read-only,
// the person's config left out, no session file, web search off, the
// schema from a file and the featuresOff features off (disableArgs). "-"
// reads the prompt from stdin.
func judgeArgs(model, effort, schema string) []string {
	args := []string{"exec", "-s", "read-only", "--ignore-user-config", "--skip-git-repo-check", "--ephemeral", "--json",
		"-m", model, "-c", `model_reasoning_effort="` + effort + `"`, "-c", `web_search="disabled"`,
		"--output-schema", schema}
	args = append(args, disableArgs()...)
	return append(args, "-")
}

// checkJudgeArgs accepts exactly what judgeArgs makes from a valid model,
// a codex effort and this adapter's schema file, so no other option (a
// sandbox or approval change, a profile, a config key, --search, resume,
// --enable, a feature left on or a prompt) reaches codex through a judge.
// Ask's checkArgs refuses this shape and this refuses an ask's: an ask
// starts with --search and has no --ephemeral, web_search="disabled" or
// --output-schema.
func (a *Adapter) checkJudgeArgs(args []string) error {
	if len(args) != 16+2*len(featuresOff) {
		return fmt.Errorf("codex judge: %d arguments refused", len(args))
	}
	model, schema := args[8], args[14]
	effort, ok := strings.CutPrefix(args[10], `model_reasoning_effort="`)
	effort, ok2 := strings.CutSuffix(effort, `"`)
	switch {
	case !config.ValidModel(model):
		return fmt.Errorf("codex judge: model %q refused", model)
	case !ok || !ok2 || !efforts[effort]:
		return fmt.Errorf("codex judge: effort %q refused", args[10])
	case schema == "" || !filepath.IsAbs(schema) || schema != a.schemaPath():
		return fmt.Errorf("codex judge: schema %q refused", schema)
	case !slices.Equal(args, judgeArgs(model, effort, schema)):
		return fmt.Errorf("codex judge: arguments %q refused", args)
	}
	return nil
}

// schemaPath is the judge schema file, or "" without an agent folder.
func (a *Adapter) schemaPath() string {
	if a.schemaDir == "" {
		return ""
	}
	return filepath.Join(a.schemaDir, schemaName)
}

// judgeSchema makes sure the schema file holds core.JudgeSchemaV1 and
// returns its path. The first judge of the process writes it, unless it
// already holds those bytes; a write that failed is tried again by the
// next judge. After that every judge only reads and compares: a rename
// while another codex reads the file would fail on Windows, so a file
// that changed is never repaired here but refused as internal.
func (a *Adapter) judgeSchema() (string, error) {
	p := a.schemaPath()
	if p == "" {
		return "", core.Fail(core.ErrCodeInternal, "codex judge: no folder for the judge schema")
	}
	want := []byte(core.JudgeSchemaV1)
	a.schemaMu.Lock()
	defer a.schemaMu.Unlock()
	if !a.schemaReady {
		if b, err := os.ReadFile(p); err != nil || !bytes.Equal(b, want) {
			if err := config.WriteFileAtomic(p, want); err != nil {
				return "", core.Fail(core.ErrCodeInternal, "codex judge: the judge schema file could not be written")
			}
			a.schemaWrites++
		}
		a.schemaReady = true
	}
	if b, err := os.ReadFile(p); err != nil || !bytes.Equal(b, want) {
		return "", core.Fail(core.ErrCodeInternal, "codex judge: the judge schema file changed; restart the agent")
	}
	return p, nil
}

// Judge runs one judge request with codex exec (core.Judger). The prompt
// goes on stdin; the output is returned unchecked.
func (a *Adapter) Judge(ctx context.Context, req core.JudgeRequest) (core.JudgeResult, error) {
	if a.exeErr != nil {
		return core.JudgeResult{}, core.Fail(core.ErrCodeAgentUnavailable, "codex was not found")
	}
	if req.Prompt == "" {
		return core.JudgeResult{}, core.Fail(core.ErrCodeInternal, "codex judge: empty prompt")
	}
	// The judge folder is the caller's to make, as with claude and
	// opencode: a missing one is the agent's fault, not the AI's.
	if info, err := os.Stat(req.Folder); !filepath.IsAbs(req.Folder) || err != nil || !info.IsDir() {
		return core.JudgeResult{}, core.Fail(core.ErrCodeInternal, "codex judge: the judge folder is not an absolute existing folder")
	}
	model, effort := req.Model, req.Effort
	if model == "" {
		model = judgeDefault.Model
	}
	if effort == "" {
		effort = judgeDefault.Effort
	}
	switch {
	case !config.ValidModel(model):
		return core.JudgeResult{}, core.Fail(core.ErrCodeBadModel, "codex judge: model refused")
	case !efforts[effort]:
		return core.JudgeResult{}, core.Fail(core.ErrCodeBadModel, "codex judge: effort refused")
	}
	schema, err := a.judgeSchema()
	if err != nil {
		return core.JudgeResult{}, err
	}
	args := judgeArgs(model, effort, schema)
	if err := a.checkJudgeArgs(args); err != nil {
		return core.JudgeResult{}, core.Fail(core.ErrCodeInternal, "codex judge: arguments refused")
	}
	ctx, cancel := context.WithTimeout(ctx, judgeTimeout)
	defer cancel()
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.exe, Dir: req.Folder, Args: args, Stdin: req.Prompt})
	r := core.JudgeResult{Model: model}
	switch {
	case errors.Is(err, proc.ErrTimeout), err != nil && ctx.Err() != nil:
		// A context that ended before codex even started gives its own
		// error, not ErrTimeout.
		return r, core.Fail(core.ErrCodeJudgeTimeout, "")
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, proc.ErrScript):
		return r, core.Fail(core.ErrCodeAgentUnavailable, "codex was not found")
	case err != nil:
		return r, core.Fail(core.ErrCodeJudgeFailed, "codex could not run")
	}
	return parseJudgeOutput(res, model)
}

// judgeItems are the items a judge may produce. Any other item, such as
// command_execution, file_change, mcp_tool_call or web_search, is a tool.
var judgeItems = map[string]bool{"agent_message": true, "reasoning": true, "error": true, "todo_list": true}

// judgeEvent is one JSONL line of `codex exec --json` as a judge reads it.
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

// parseJudgeOutput reads a judge's events. In this order: a tool item
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
		// judge_failed carries codex's words, never the judge's JSON.
		if strings.TrimSpace(detail) == "" || strings.Contains(detail, `"judgments"`) {
			detail = "codex's turn failed with no message"
			if res.ExitCode != 0 {
				detail = fmt.Sprintf("codex ended with exit code %d and no judgment", res.ExitCode)
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
