package claude

import (
	"context"
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

// Combined answers (종합 답변, P2-3 S5b, spec part 20) run the judge's
// locked-down `claude -p` (judge.go) with two changes: the model is the
// question's ask tier model, and --json-schema is core.SynthSchemaV1. No
// tool, no MCP server, no saved conversation, --safe-mode, the prompt on
// stdin, proc.Run with Runner.Path(); checkSynthArgs checks the exact
// shape first, as checkJudgeArgs does for a judge, and each refuses the
// other's. A question spec (질문 정제, milestone C) is the same command with
// --json-schema core.RefineSchemaV1 and, by default, effort low.

// synthTimeout is the time limit Synthesize sets itself
// (core.Synthesizer); a variable so a test can shorten it.
var synthTimeout = core.SynthTimeout

// synthEffort is the effort a writer runs with when its tier names none:
// the command needs one.
const synthEffort = "medium"

var (
	_ core.Synthesizer = (*Adapter)(nil)
	_ core.Named       = (*Adapter)(nil)
)

// DisplayName is "Claude", as a combined answer names its answers
// (core.Named).
func (a *Adapter) DisplayName() string { return "Claude" }

// synthArgs builds the command line of one combined answer: judgeArgs with
// the synth schema. The prompt is not in it.
func synthArgs(model, effort string) []string { return writerArgs(model, effort, core.SynthSchemaV1) }

// refineArgs builds the command line of one question spec: synthArgs with
// the refine schema.
func refineArgs(model, effort string) []string { return writerArgs(model, effort, core.RefineSchemaV1) }

func writerArgs(model, effort, schema string) []string {
	return []string{"-p", "--output-format", "json", "--model", model, "--effort", effort,
		"--tools", "", "--strict-mcp-config", "--no-session-persistence", "--safe-mode",
		"--json-schema", schema}
}

// synthArgCount is len(synthArgs(…)).
const synthArgCount = 14

// checkSynthArgs accepts exactly what synthArgs or refineArgs makes for a
// valid model and effort, the synth or refine schema byte for byte: never
// the judge schema, a permission flag, a tool, an MCP config, a resume or a
// prompt argument.
func checkSynthArgs(args []string) error {
	if len(args) != synthArgCount {
		return fmt.Errorf("%w: a combined answer has %d arguments, not %d", ErrArgRejected, synthArgCount, len(args))
	}
	if !config.ValidModel(args[4]) || !config.ValidEffort(args[6]) {
		return fmt.Errorf("%w: a combined answer needs a valid model and effort", ErrArgRejected)
	}
	if !slices.Equal(args, synthArgs(args[4], args[6])) && !slices.Equal(args, refineArgs(args[4], args[6])) {
		return fmt.Errorf("%w: not the combined-answer command", ErrArgRejected)
	}
	return nil
}

// Synthesize writes one combined answer with claude -p
// (core.Synthesizer). The result carries Model and the tokens with any
// error once claude ran; Output is set only without an error, and Cleanup
// is always empty (claude keeps no conversation).
func (a *Adapter) Synthesize(ctx context.Context, req core.SynthRequest) (core.SynthResult, error) {
	if a.runner == nil {
		return core.SynthResult{}, core.Fail(core.ErrCodeAgentUnavailable, "claude was not found")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return core.SynthResult{}, core.Fail(core.ErrCodeInternal, "the combined-answer prompt is empty")
	}
	if info, err := os.Stat(req.Folder); !filepath.IsAbs(req.Folder) || err != nil || !info.IsDir() {
		return core.SynthResult{}, core.Fail(core.ErrCodeInternal, "the judge folder is not an absolute existing folder")
	}
	defEffort, build := synthEffort, synthArgs
	if req.Refine {
		defEffort, build = core.RefineEffort, refineArgs
	}
	model, effort := req.Model, firstNonEmpty(req.Effort, defEffort)
	if !config.ValidModel(model) {
		return core.SynthResult{}, core.Fail(core.ErrCodeBadModel, model)
	}
	if !config.ValidEffort(effort) {
		return core.SynthResult{}, core.Fail(core.ErrCodeBadModel, "effort "+effort)
	}
	args := build(model, effort)
	if err := checkSynthArgs(args); err != nil {
		return core.SynthResult{}, core.Fail(core.ErrCodeInternal, err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, synthTimeout)
	defer cancel()
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.runner.Path(), Dir: req.Folder, Args: args, Stdin: req.Prompt})
	r := core.SynthResult{Model: model}
	switch {
	case errors.Is(err, proc.ErrTimeout), err != nil && ctx.Err() != nil:
		// A context that ended before claude even started gives its own
		// error, not ErrTimeout.
		return r, core.Fail(core.ErrCodeJudgeTimeout, "")
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, proc.ErrScript):
		return core.SynthResult{}, core.Fail(core.ErrCodeAgentUnavailable, "claude was not found")
	case err != nil:
		return r, core.Fail(core.ErrCodeJudgeFailed, "claude could not run")
	}
	// The judge's parser reads the same result object: structured_output,
	// permission denials, the model that answered and the tokens.
	jr, err := parseJudgeOutput(res, model)
	return core.SynthResult{Output: jr.Output, Model: jr.Model, InputTokens: jr.InputTokens, OutputTokens: jr.OutputTokens}, err
}
