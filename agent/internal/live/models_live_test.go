//go:build live

package live

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/adapter/agy"
	"github.com/heeo-dev/askorus/agent/internal/adapter/codex"
	"github.com/heeo-dev/askorus/agent/internal/adapter/opencode"
	"github.com/heeo-dev/askorus/agent/internal/core"
)

// followingAI is an ask-only adapter whose tiers follow the newest model
// of their lines (#64).
type followingAI interface {
	core.Asker
	core.Following
	Info(context.Context) core.AdapterInfo
	WaitModels(context.Context)
}

// LT-19 The newest model of each tier's line (#64) with the real CLIs:
// each list is read the way the agent reads it (`codex debug models`,
// `opencode models --standalone`, `agy models`), and one short one-off
// ask at the tier whose model moved (quick when none did) answers. A
// newer model the AI refuses falls back to the shipped one, which is
// logged: that is the design, and the next list tries it again. Each AI
// is skipped when it is not usable here. Three short asks at most.
func TestLT19NewestModels(t *testing.T) {
	ctx := context.Background()
	for name, a := range map[string]followingAI{
		"codex":    codex.New(codex.Options{SchemaDir: t.TempDir(), Follow: true}),
		"opencode": opencode.New(opencode.Options{Follow: true}),
		"agy":      agy.New(agy.Options{Follow: true}),
	} {
		t.Run(name, func(t *testing.T) {
			if info := a.Info(ctx); !info.Available || len(info.Notices) != 0 {
				t.Skipf("%s is not usable here: %v", name, info.Notices)
			}
			start := time.Now()
			wctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			a.WaitModels(wctx)
			cancel()
			took := time.Since(start)
			tiers, newer := a.AskTiers(), a.Newer()
			t.Logf("    %s: list %.1f s, tiers %v, newer %v", name, took.Seconds(), tiers, newer)
			tier := core.TierQuick
			for _, x := range core.Tiers {
				for _, n := range newer {
					if tiers[x].Model == n {
						tier = x
					}
				}
			}
			folder := askFolder(t)
			if err := os.MkdirAll(folder, 0o700); err != nil {
				t.Fatal(err)
			}
			res, err := a.Ask(ctx, core.AskRequest{Folder: folder, Prompt: core.AskPrompt("Reply with the single word: ok"), Tier: tier, OneOff: true})
			if err != nil || res.Answer == "" {
				t.Fatalf("ask at %s (%s): %+v %v", tier, tiers[tier].Model, res, err)
			}
			if !strings.HasPrefix(res.Model, tiers[tier].Model) {
				t.Logf("    %s: %s was refused, answered with %s (the shipped one)", name, tiers[tier].Model, res.Model)
			} else {
				t.Logf("    %s: %s answered with %s", name, tier, res.Model)
			}
		})
	}
}
