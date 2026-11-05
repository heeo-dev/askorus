package opencode

import (
	"context"
	"slices"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Milestone C: a question spec runs the combined-answer command with the
// refine agent (deny-all, as the writer); the check takes both agents and
// no other.
func TestRefineArgsAndOutput(t *testing.T) {
	a, _, jf := judgeSetup(t)
	task := judge.RefineTask{Question: "HTTP와 HTTPS 차이는?"}
	p := judge.RefinePrompt(task, judge.RefineNonce(task))
	res, err := a.Synthesize(context.Background(), core.SynthRequest{Folder: jf, Prompt: p, Model: testSynthModel, Refine: true})
	if err != nil {
		t.Fatal(err)
	}
	cs := fakecli.Calls(t)
	if len(cs) != 1 || !slices.Equal(cs[0].Args, refineArgs(testSynthModel)) || cs[0].Stdin != p {
		t.Fatalf("calls %+v", cs)
	}
	if spec, err := judge.ValidateRefine(res.Output); err != nil || spec.Kind != "compare" {
		t.Fatalf("spec %+v %v", spec, err)
	}
	if checkSynthArgs(refineArgs(testSynthModel)) != nil || checkSynthArgs(writerArgs("asksemble-judge", testSynthModel)) == nil {
		t.Fatal("the check took the old judge agent or refused the refine agent")
	}
}
