package codex

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Milestone C: a question spec runs the combined-answer command with the
// refine schema file (written once, apart from the synth one) and effort
// low unless one is given.
func TestRefineArgsSchemaFileAndOutput(t *testing.T) {
	a, schemaDir, folder := judgeAdapter(t)
	task := judge.RefineTask{Question: "HTTP와 HTTPS 차이는?"}
	p := judge.RefinePrompt(task, judge.RefineNonce(task))
	for range 2 {
		res, err := a.Synthesize(context.Background(), core.SynthRequest{Folder: folder, Prompt: p, Model: "gpt-6-luna", Refine: true})
		if err != nil {
			t.Fatal(err)
		}
		if spec, err := judge.ValidateRefine(res.Output); err != nil || spec.Kind != "compare" {
			t.Fatalf("spec %+v %v", spec, err)
		}
	}
	schema := filepath.Join(schemaDir, core.RefineSchemaFile)
	calls := fakecli.Calls(t)
	if len(calls) != 2 || !slices.Equal(calls[0].Args, wantJudgeArgs("gpt-6-luna", "low", schema)) || calls[0].Stdin != p || calls[0].Schema != core.RefineSchemaV1 {
		t.Fatalf("calls %+v", calls)
	}
	if b, err := os.ReadFile(schema); err != nil || string(b) != core.RefineSchemaV1 || a.refineWrites != 1 || a.synthWrites != 0 {
		t.Fatalf("schema file %q %v, writes %d/%d", b, err, a.refineWrites, a.synthWrites)
	}
	// A schema file changed under the running agent is never used.
	if err := os.WriteFile(schema, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Synthesize(context.Background(), core.SynthRequest{Folder: folder, Prompt: p, Model: "gpt-6-luna", Refine: true}); err == nil {
		t.Fatal("a changed schema file was used")
	}
}
