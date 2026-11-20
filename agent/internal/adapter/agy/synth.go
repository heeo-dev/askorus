package agy

// Combined answers and question refining (종합 답변, 질문 정제) with agy
// (#20 decision 6): the moderator's one-off run. It is an ask's run with
// another agent file, whose only tool is finish: agy's --json-schema hands
// the structured output over through finish, and without it a run went
// round four turns for 100 seconds and gave none (2026-11-18). The schema
// is the constant core.SynthSchemaV1 or core.RefineSchemaV1, inline. The
// deny rules are the ask's (with photos, the photo run's: a moderator
// whose model sees photos gets the question's photos in a folder of their
// own, photo.go). agy keeps the run as a conversation and has no command
// to delete one: Cleanup stays empty (decision 3).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

// JudgeMinVersion is the oldest agy the one-off command was tested with
// (2026-11-18). agy updates itself; the gate compares Info's version with
// it through core.VersionAtLeast.
const JudgeMinVersion = "1.5.10"

const (
	synthAgent      = "askorus-synth"
	synthPhotoAgent = "askorus-synth-photo"
)

const synthInstructions = "You write one answer from the material you are given. " +
	"Use no tool but finish, which carries your answer in the shape the schema asks for.\n"

const synthAgentFile = "---\n" +
	"name: " + synthAgent + "\n" +
	"description: Askorus moderator, one structured answer\n" +
	"tools: [finish]\n" +
	"mainAgent: true\n" +
	"subagent: false\n" +
	"commandExecutionPolicy: off\n" +
	"---\n" +
	synthInstructions

const synthPhotoAgentFile = "---\n" +
	"name: " + synthPhotoAgent + "\n" +
	"description: Askorus moderator with the question's photos, one structured answer\n" +
	"tools: [finish, view_file]\n" +
	"mainAgent: true\n" +
	"subagent: false\n" +
	"commandExecutionPolicy: off\n" +
	"---\n" +
	synthInstructions +
	"The photos of the question are files in the current folder; you may look at them with view_file. Open no other file.\n"

// synthEffort is the level a combined answer runs with when the request
// names none.
const synthEffort = "medium"

var (
	synthTools      = map[string]bool{"finish": true, "manage_task": true}
	synthPhotoTools = map[string]bool{"finish": true, "manage_task": true, "read_file": true, "view_file": true}
)

// synthTimeout is the time limit Synthesize sets itself; a variable so a
// test can shorten it.
var synthTimeout = core.SynthTimeout

var (
	_ core.Synthesizer = (*Adapter)(nil)
	_ core.Judger      = (*Adapter)(nil)
	_ core.Named       = (*Adapter)(nil)
	_ core.ImageSeer   = (*Adapter)(nil)
)

// Family names the company of a model agy runs (core.Judger). The adapter
// only runs Gemini models (decision 4); the others agy offers are named
// too, so a setting that slips one in is still placed. Anything else is "".
func (a *Adapter) Family(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "gemini-"):
		return "google"
	case strings.HasPrefix(m, "claude-"):
		return "anthropic"
	case strings.HasPrefix(m, "gpt-oss-"):
		return "openai"
	}
	return ""
}

// synthArgs builds the one-off run. The prompt goes on stdin.
//
//	--agent A --model M --json-schema S --input-format stream-json --output-format stream-json --disable-slash-commands --settings D
func synthArgs(agent, model, schema, settings string) []string {
	return []string{"--agent", agent, "--model", model, "--json-schema", schema,
		"--input-format", "stream-json", "--output-format", "stream-json", "--disable-slash-commands", "--settings", settings}
}

// checkSynthArgs accepts exactly what synthArgs makes from one of the two
// agents with its own settings, a valid model and one of the two schemas.
func checkSynthArgs(args []string) error {
	if len(args) != 13 {
		return fmt.Errorf("agy synth: %d arguments refused", len(args))
	}
	agent, model, schema := args[1], args[3], args[5]
	settings := map[string]string{synthAgent: askSettings, synthPhotoAgent: photoSettings}[agent]
	switch {
	case settings == "":
		return fmt.Errorf("agy synth: agent %q refused", agent)
	case !modelRe.MatchString(model):
		return fmt.Errorf("agy synth: model %q refused", model)
	case schema != core.SynthSchemaV1 && schema != core.RefineSchemaV1:
		return fmt.Errorf("agy synth: schema refused")
	case !slices.Equal(args, synthArgs(agent, model, schema, settings)):
		return fmt.Errorf("agy synth: arguments %q refused", args)
	}
	return nil
}

// hasLevel reports whether model already names its level
// ("gemini-3.8-flash-high"), as a model of the settings' own may.
func hasLevel(model string) bool {
	for e := range efforts {
		if strings.HasSuffix(model, "-"+e) {
			return true
		}
	}
	return false
}

// Synthesize writes one combined answer, or the moderator's question spec
// (core.Synthesizer). The output is returned unchecked. A newer model of
// a line that agy refuses is asked again with the shipped one (#64).
func (a *Adapter) Synthesize(ctx context.Context, req core.SynthRequest) (core.SynthResult, error) {
	res, err := a.synthesize(ctx, req)
	if core.ErrorCode(err) == core.ErrCodeBadModel && a.follow.Refuse(req.Model) {
		req.Model = a.follow.Base(req.Model)
		res, err = a.synthesize(ctx, req)
	}
	return res, err
}

func (a *Adapter) synthesize(ctx context.Context, req core.SynthRequest) (core.SynthResult, error) {
	if a.exeErr != nil {
		return core.SynthResult{}, core.Fail(core.ErrCodeAgentUnavailable, "agy was not found")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return core.SynthResult{}, core.Fail(core.ErrCodeInternal, "agy synth: empty prompt")
	}
	if info, err := os.Stat(req.Folder); !filepath.IsAbs(req.Folder) || err != nil || !info.IsDir() {
		return core.SynthResult{}, core.Fail(core.ErrCodeInternal, "agy synth: the judge folder is not an absolute existing folder")
	}
	model, effort := req.Model, req.Effort
	if effort == "" && !hasLevel(model) {
		effort = synthEffort
		if req.Refine {
			effort = core.RefineEffort
		}
	}
	switch {
	case !modelRe.MatchString(model):
		return core.SynthResult{}, core.Fail(core.ErrCodeBadModel, "agy synth: model refused")
	case effort != "" && !efforts[effort]:
		return core.SynthResult{}, core.Fail(core.ErrCodeBadModel, "agy synth: effort refused")
	case req.Refine && len(req.Images) > 0:
		return core.SynthResult{}, core.Fail(core.ErrCodeInternal, "agy synth: a photo was refused")
	}
	m := cliModel(model, effort)
	schema := core.SynthSchemaV1
	if req.Refine {
		schema = core.RefineSchemaV1
	}
	agent, settings, tools, prompt := synthAgent, askSettings, synthTools, req.Prompt
	var dir string
	if len(req.Images) > 0 {
		if a.shadowed(synthPhotoAgent) || !a.photosSafe() {
			return core.SynthResult{}, core.Fail(core.ErrCodeInternal, "agy synth: photos are not safe to give agy here")
		}
		d, names, cleanup, err := photoFolder(req.Folder, synthPhotoAgent, synthPhotoAgentFile, req.Images)
		if err != nil {
			return core.SynthResult{}, err
		}
		defer cleanup()
		dir, agent, settings, tools, prompt = d, synthPhotoAgent, photoSettings, synthPhotoTools, photoLine(names)+req.Prompt
	} else {
		d, err := a.agentFile(filepath.Join(req.Folder, FolderName), synthAgent, synthAgentFile)
		if err != nil {
			return core.SynthResult{}, err
		}
		dir = d
	}
	args := synthArgs(agent, m, schema, settings)
	if err := checkSynthArgs(args); err != nil {
		return core.SynthResult{}, core.Fail(core.ErrCodeInternal, "agy synth: arguments refused")
	}
	ctx, cancel := context.WithTimeout(ctx, synthTimeout)
	defer cancel()
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.exe, Dir: dir, Args: args, Stdin: stdinMessage(prompt)})
	r := core.SynthResult{Model: m}
	switch {
	case errors.Is(err, proc.ErrTimeout), err != nil && ctx.Err() != nil:
		return r, core.Fail(core.ErrCodeJudgeTimeout, "")
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, proc.ErrScript):
		return r, core.Fail(core.ErrCodeAgentUnavailable, "agy was not found")
	case err != nil:
		return r, core.Fail(core.ErrCodeJudgeFailed, "agy could not run")
	}
	return parseSynth(res, agent, tools, m)
}

// parseSynth reads a one-off run: a start event that is not the agent and
// mode the adapter set, or a tool outside tools, throws it away; a
// finished run's structured output is the output; anything else is the
// error agy gave.
func parseSynth(res proc.Result, agent string, tools map[string]bool, model string) (core.SynthResult, error) {
	run := readRun(res.Stdout, tools)
	ae, _, _, rest := stderrInfo(res.Stderr)
	out := core.SynthResult{Model: model}
	if run.result != nil && run.result.Usage != nil {
		out.InputTokens, out.OutputTokens = run.result.Usage.InputTokens, run.result.Usage.OutputTokens
	}
	structured := run.result != nil && len(run.result.StructuredOutput) > 0 && string(run.result.StructuredOutput) != "null"
	switch {
	case run.sawInit && (run.agent != agent || run.mode != permissionMode):
		return out, core.Fail(core.ErrCodeInternal, "agy did not run with Askorus's moderator agent and settings")
	case run.badTool != "":
		return out, core.Fail(core.ErrCodeJudgeUsedTool, "")
	case run.result != nil && (run.result.Status == "SUCCESS" || run.retried(structured)):
		if !run.sawInit {
			return out, core.Fail(core.ErrCodeInternal, "agy did not report its agent")
		}
		if !structured || !json.Valid(run.result.StructuredOutput) {
			return out, core.Fail(core.ErrCodeJudgeBadOutput, "")
		}
		out.Output = stripOutput(run.result.StructuredOutput)
		return out, nil
	}
	msg := ae.ShortError
	if run.result != nil && run.result.Error != "" {
		msg = run.result.Error
	}
	if msg == "" {
		msg = rest
	}
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "invalid model selection") || strings.Contains(low, "not recognized as a known model"):
		return out, core.Fail(core.ErrCodeBadModel, clip(msg, core.JudgeMessageChars-1))
	case strings.TrimSpace(msg) == "":
		return out, core.Fail(core.ErrCodeJudgeFailed, fmt.Sprintf("agy ended with exit code %d and no output", res.ExitCode))
	}
	return out, core.Fail(core.ErrCodeJudgeFailed, clip(msg, core.JudgeMessageChars-1))
}
