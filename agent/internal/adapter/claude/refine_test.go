package claude

import (
	"context"
	"slices"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
	"github.com/heeo-dev/askorus/agent/internal/judge"
)

// Milestone C: a question spec runs the combined-answer command with the
// refine schema, effort low unless one is given, the prompt on stdin; the
// check takes both shapes and nothing between them.
func TestRefineArgsAndOutput(t *testing.T) {
	a, folder := newJudgeAdapter(t)
	task := judge.RefineTask{Question: "HTTP와 HTTPS 차이는?"}
	p := judge.RefinePrompt(task, judge.RefineNonce(task))
	res, err := a.Synthesize(context.Background(), core.SynthRequest{Folder: folder, Prompt: p, Model: "haiku", Refine: true})
	if err != nil {
		t.Fatal(err)
	}
	calls := fakecli.Calls(t)
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	want := []string{"-p", "--output-format", "json", "--model", "haiku", "--effort", "low",
		"--tools", "", "--strict-mcp-config", "--settings", noFileReads, "--no-session-persistence", "--safe-mode",
		"--json-schema", core.RefineSchemaV1}
	if !slices.Equal(calls[0].Args, want) || calls[0].Stdin != p {
		t.Fatalf("args %q", calls[0].Args)
	}
	spec, err := judge.ValidateRefine(res.Output)
	if err != nil || spec.Kind != "compare" || spec.Language != "ko" {
		t.Fatalf("spec %+v %v", spec, err)
	}
	if err := checkSynthArgs(refineArgs("haiku", "low")); err != nil {
		t.Fatal(err)
	}
	mixed := refineArgs("haiku", "low")
	mixed[13] = `{"type":"object"}`
	if checkSynthArgs(mixed) == nil {
		t.Fatal("another schema passed as a writer's")
	}
}
