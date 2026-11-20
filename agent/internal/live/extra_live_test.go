//go:build live

package live

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/adapter/claude"
	"github.com/heeo-dev/askorus/agent/internal/adapter/codex"
	"github.com/heeo-dev/askorus/agent/internal/core"
)

// LT-20 Models outside the tiers (#65) with the real CLIs: one short
// one-off ask with Codex's Astra and one with Claude's Fable, each at low
// effort, as an own pick sends them. Both use much more of the plan, so
// only these two short asks. Each is skipped when its AI is not usable
// here.
func TestLT20ExtraModels(t *testing.T) {
	ctx := context.Background()
	claudeDir, err := claude.DefaultClaudeDir()
	if err != nil {
		t.Fatal(err)
	}
	for name, a := range map[string]interface {
		core.Asker
		core.ExtraLister
		Info(context.Context) core.AdapterInfo
	}{
		"codex":  codex.New(codex.Options{SchemaDir: t.TempDir(), Follow: true}),
		"claude": claude.New(claude.Options{ClaudeDir: claudeDir}),
	} {
		t.Run(name, func(t *testing.T) {
			if info := a.Info(ctx); !info.Available {
				t.Skipf("%s is not usable here: %v", name, info.Notices)
			}
			if w, ok := a.(interface{ WaitModels(context.Context) }); ok {
				wctx, cancel := context.WithTimeout(ctx, 60*time.Second)
				w.WaitModels(wctx)
				cancel()
			}
			extra := a.AskExtra()
			if len(extra) != 1 || !extra[0].Heavy {
				t.Fatalf("extra = %+v", extra)
			}
			folder := askFolder(t)
			if err := os.MkdirAll(folder, 0o700); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			res, err := a.Ask(ctx, core.AskRequest{Folder: folder, Prompt: core.AskPrompt("Reply with the single word: ok"), Tier: core.TierQuick, Model: extra[0].Model, Effort: "low", OneOff: true})
			if err != nil || res.Answer == "" {
				t.Fatalf("ask with %s: %+v %v", extra[0].Model, res, err)
			}
			t.Logf("    %s: %s answered as %s in %.1f s", name, extra[0].Model, res.Model, time.Since(start).Seconds())
		})
	}
}
