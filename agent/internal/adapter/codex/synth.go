package codex

// Combined answers with codex (종합 답변, P2-3 S5b, spec part 20): the
// locked-down one-off `codex exec` of the judging days (judge.go): the
// model is the question's ask tier model, and --output-schema is the
// combined-answer schema file, synth-schema-v1.json.
// Read-only, the person's config left out, web search off, no session
// file, the featuresOff features off, any tool item thrown away, the
// prompt on stdin.

import (
	"bytes"
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

// synthSchemaName is the combined-answer schema file in the agent folder;
// uninstall removes it.
const synthSchemaName = core.SynthSchemaFile

// refineSchemaName is the question spec's schema file (질문 정제, milestone C),
// next to the synth schema; the command is otherwise the same.
const refineSchemaName = core.RefineSchemaFile

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

// DisplayName is "Codex", as a combined answer names its answers
// (core.Named).
func (a *Adapter) DisplayName() string { return "Codex" }

// synthArgs builds one combined answer: judgeArgs with the synth schema
// file. "-" reads the prompt from stdin.
func synthArgs(model, effort, schema string) []string {
	return judgeArgs(model, effort, schema)
}

// checkSynthArgs accepts exactly what synthArgs makes from a valid model,
// a codex effort and this adapter's synth or refine schema file: never
// another schema file, and no other option.
func (a *Adapter) checkSynthArgs(args []string) error {
	if len(args) != 16+2*len(featuresOff) {
		return fmt.Errorf("codex synth: %d arguments refused", len(args))
	}
	model, schema := args[8], args[14]
	effort, ok := strings.CutPrefix(args[10], `model_reasoning_effort="`)
	effort, ok2 := strings.CutSuffix(effort, `"`)
	switch {
	case !config.ValidModel(model):
		return fmt.Errorf("codex synth: model %q refused", model)
	case !ok || !ok2 || !efforts[effort]:
		return fmt.Errorf("codex synth: effort %q refused", args[10])
	case schema == "" || !filepath.IsAbs(schema) || (schema != a.synthSchemaPath() && schema != a.refineSchemaPath()):
		return fmt.Errorf("codex synth: schema %q refused", schema)
	case !slices.Equal(args, synthArgs(model, effort, schema)):
		return fmt.Errorf("codex synth: arguments %q refused", args)
	}
	return nil
}

// synthSchemaPath is the combined-answer schema file, or "" without an
// agent folder.
func (a *Adapter) synthSchemaPath() string {
	if a.schemaDir == "" {
		return ""
	}
	return filepath.Join(a.schemaDir, synthSchemaName)
}

// refineSchemaPath is the refine schema file's path, "" without a folder.
func (a *Adapter) refineSchemaPath() string {
	if a.schemaDir == "" {
		return ""
	}
	return filepath.Join(a.schemaDir, refineSchemaName)
}

// synthSchema makes sure the combined-answer schema file holds
// core.SynthSchemaV1 and gives its path.
func (a *Adapter) synthSchema() (string, error) {
	return a.schemaFile(a.synthSchemaPath(), core.SynthSchemaV1, &a.synthReady, &a.synthWrites)
}

// refineSchema does the same for core.RefineSchemaV1.
func (a *Adapter) refineSchema() (string, error) {
	return a.schemaFile(a.refineSchemaPath(), core.RefineSchemaV1, &a.refineReady, &a.refineWrites)
}

// schemaFile writes schema to p once (when it is missing or differs), then
// checks before every use that it still holds exactly that: a file
// changed under a running agent is internal, never used.
func (a *Adapter) schemaFile(p, schema string, ready *bool, writes *int) (string, error) {
	if p == "" {
		return "", core.Fail(core.ErrCodeInternal, "codex synth: no folder for the schema")
	}
	want := []byte(schema)
	a.synthMu.Lock()
	defer a.synthMu.Unlock()
	if !*ready {
		if b, err := os.ReadFile(p); err != nil || !bytes.Equal(b, want) {
			if err := config.WriteFileAtomic(p, want); err != nil {
				return "", core.Fail(core.ErrCodeInternal, "codex synth: the schema file could not be written")
			}
			*writes++
		}
		*ready = true
	}
	if b, err := os.ReadFile(p); err != nil || !bytes.Equal(b, want) {
		return "", core.Fail(core.ErrCodeInternal, "codex synth: the schema file changed; restart the agent")
	}
	return p, nil
}

// Synthesize writes one combined answer with codex exec
// (core.Synthesizer). The prompt goes on stdin; the output is returned
// unchecked. Cleanup is always empty: --ephemeral keeps no session file.
func (a *Adapter) Synthesize(ctx context.Context, req core.SynthRequest) (core.SynthResult, error) {
	if a.exeErr != nil {
		return core.SynthResult{}, core.Fail(core.ErrCodeAgentUnavailable, "codex was not found")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return core.SynthResult{}, core.Fail(core.ErrCodeInternal, "codex synth: empty prompt")
	}
	if info, err := os.Stat(req.Folder); !filepath.IsAbs(req.Folder) || err != nil || !info.IsDir() {
		return core.SynthResult{}, core.Fail(core.ErrCodeInternal, "codex synth: the judge folder is not an absolute existing folder")
	}
	model, effort := req.Model, req.Effort
	if effort == "" {
		effort = synthEffort
		if req.Refine {
			effort = core.RefineEffort
		}
	}
	switch {
	case !config.ValidModel(model):
		return core.SynthResult{}, core.Fail(core.ErrCodeBadModel, "codex synth: model refused")
	case !efforts[effort]:
		return core.SynthResult{}, core.Fail(core.ErrCodeBadModel, "codex synth: effort refused")
	}
	schemaOf := a.synthSchema
	if req.Refine {
		schemaOf = a.refineSchema
	}
	schema, err := schemaOf()
	if err != nil {
		return core.SynthResult{}, err
	}
	args := synthArgs(model, effort, schema)
	if err := a.checkSynthArgs(args); err != nil {
		return core.SynthResult{}, core.Fail(core.ErrCodeInternal, "codex synth: arguments refused")
	}
	ctx, cancel := context.WithTimeout(ctx, synthTimeout)
	defer cancel()
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.exe, Dir: req.Folder, Args: args, Stdin: req.Prompt})
	r := core.SynthResult{Model: model}
	switch {
	case errors.Is(err, proc.ErrTimeout), err != nil && ctx.Err() != nil:
		return r, core.Fail(core.ErrCodeJudgeTimeout, "")
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, proc.ErrScript):
		return r, core.Fail(core.ErrCodeAgentUnavailable, "codex was not found")
	case err != nil:
		return r, core.Fail(core.ErrCodeJudgeFailed, "codex could not run")
	}
	// The one-off command's parser reads the events: a tool item is
	// judge_used_tool, the last agent message is the output.
	jr, err := parseJudgeOutput(res, model)
	return core.SynthResult{Output: jr.Output, Model: jr.Model, InputTokens: jr.InputTokens, OutputTokens: jr.OutputTokens}, err
}
