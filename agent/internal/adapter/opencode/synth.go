package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/proc"
)

// Combined answers (종합 답변, P2-3 S5b, spec part 20). A writer is one
// `opencode run --standalone` with its own agent, asksemble-synth, that may
// use no tool (the one-off command, judge.go). It runs in the judge folder
// and takes the judge title, so CleanupJudge, and uninstall, delete its
// conversation.

const (
	// synthAgent is the agent a writer runs with, handed to OpenCode for
	// one run only in OPENCODE_CONFIG_CONTENT.
	synthAgent = "asksemble-synth"
	// refineAgent fills in a question spec (질문 정제, milestone C): the
	// same locked agent with its own prompt; the message says the JSON.
	refineAgent = "asksemble-refine"
	// synthTitle is the title of every writer conversation: the judge
	// title, so CleanupJudge deletes it.
	synthTitle = judgeTitle
)

// synthTimeout is the time limit Synthesize sets itself
// (core.Synthesizer); a variable so a test can shorten it.
var synthTimeout = core.SynthTimeout

// synthConfig holds the writer agent and the refine agent: apart from
// askConfig, with every tool denied, the web tools too.
var synthConfig = func() string {
	b, _ := json.Marshal(map[string]any{"agent": map[string]any{
		synthAgent: map[string]any{
			"description": "Asksemble synthesis: write one combined answer",
			"mode":        "primary",
			"prompt": "You write the combined answer for the Asksemble app. " +
				"Reply only with the JSON the message asks for, and use no tools.",
			"permission": map[string]string{"*": "deny"},
		},
		refineAgent: map[string]any{
			"description": "Asksemble refine: fill in a question spec",
			"mode":        "primary",
			"prompt": "You fill in a question spec for the Asksemble app; you do not answer the question. " +
				"Reply only with the JSON the message asks for, and use no tools.",
			"permission": map[string]string{"*": "deny"},
		},
	}})
	return string(b)
}()

var (
	_ core.Synthesizer = (*Adapter)(nil)
	_ core.Named       = (*Adapter)(nil)
)

// DisplayName is "OpenCode", as a combined answer names its answers
// (core.Named).
func (a *Adapter) DisplayName() string { return "OpenCode" }

func synthArgs(model string) []string { return writerArgs(synthAgent, model) }

// refineArgs is synthArgs with the refine agent.
func refineArgs(model string) []string { return writerArgs(refineAgent, model) }

func writerArgs(agent, model string) []string {
	return []string{"run", "--standalone", "--format", "json", "--agent", agent, "-m", model, "--title", synthTitle}
}

// checkSynthArgs accepts exactly what synthArgs or refineArgs makes from a
// valid model: never an ask's or another agent. The prompt goes on stdin.
func checkSynthArgs(args []string) error {
	switch {
	case len(args) != len(synthArgs("")):
		return fmt.Errorf("opencode synth: %d arguments refused", len(args))
	case !modelRe.MatchString(args[7]):
		return fmt.Errorf("opencode synth: model %q refused", args[7])
	case !slices.Equal(args, synthArgs(args[7])) && !slices.Equal(args, refineArgs(args[7])):
		return fmt.Errorf("opencode synth: arguments %q refused", args)
	}
	return nil
}

// synthEnv is what a writer run gets: the judge folder as PWD and the
// writer agent.
func synthEnv(folder string) map[string]string {
	return map[string]string{"PWD": folder, "OPENCODE_CONFIG_CONTENT": synthConfig}
}

// Synthesize writes one combined answer with opencode run
// (core.Synthesizer). The model and variant work as in Ask: req.Effort,
// when set, replaces the model's variant; without one OpenCode runs the
// model as named. Cleanup is the conversation's id on every path once
// opencode ran, and the output is dropped with any error; the caller
// deletes the conversation with CleanupJudge.
func (a *Adapter) Synthesize(ctx context.Context, req core.SynthRequest) (core.SynthResult, error) {
	if a.exeErr != nil {
		return core.SynthResult{}, core.Fail(core.ErrCodeAgentUnavailable, "opencode was not found")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return core.SynthResult{}, core.Fail(core.ErrCodeInternal, "the combined-answer prompt is empty")
	}
	if !judgeFolderOK(req.Folder) {
		return core.SynthResult{}, core.Fail(core.ErrCodeInternal, "the judge folder is not an absolute existing folder")
	}
	switch {
	case req.Effort != "" && !variantRe.MatchString(req.Effort):
		return core.SynthResult{}, core.Fail(core.ErrCodeBadModel, "variant "+req.Effort)
	case req.Model == "" || !modelRe.MatchString(modelArg(req.Model, req.Effort)):
		return core.SynthResult{}, core.Fail(core.ErrCodeBadModel, req.Model)
	}
	model := modelArg(req.Model, req.Effort)
	args := synthArgs(model)
	if req.Refine {
		args = refineArgs(model)
	}
	if err := checkSynthArgs(args); err != nil {
		return core.SynthResult{}, core.Fail(core.ErrCodeInternal, "the combined-answer command was refused")
	}
	ctx, cancel := context.WithTimeout(ctx, synthTimeout)
	defer cancel()
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.exe, Dir: req.Folder, Args: args, Env: synthEnv(req.Folder), Stdin: req.Prompt})
	jr, perr := parseJudgeOutput(res, model)
	r := core.SynthResult{Output: jr.Output, Model: jr.Model, InputTokens: jr.InputTokens, OutputTokens: jr.OutputTokens, Cleanup: jr.Cleanup}
	switch {
	case err == nil:
		if perr != nil {
			r.Output = nil
		}
		return r, perr
	case errors.Is(err, proc.ErrTimeout), ctx.Err() != nil:
		err = core.Fail(core.ErrCodeJudgeTimeout, "")
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, proc.ErrScript):
		err = core.Fail(core.ErrCodeAgentUnavailable, "opencode was not found")
	default:
		err = core.Fail(core.ErrCodeJudgeFailed, "opencode could not run")
	}
	// What the run printed before it ended still names its conversation
	// and tokens; its output is never used.
	r.Output = nil
	return r, err
}
