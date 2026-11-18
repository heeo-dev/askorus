//go:build live

package live

import (
	"context"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/adapter/codex"
	"github.com/heeo-dev/askorus/agent/internal/adapter/opencode"
	"github.com/heeo-dev/askorus/agent/internal/core"
)

// LT-18 What is left of the plans (#20), read the way the agent reads it:
// Codex through `codex app-server` (account/rateLimits/read, stdin kept
// open until the answer) and OpenCode Go through `opencode api
// --standalone experimental.usage.limits`. Neither asks a model, so
// nothing is spent. Both are experimental in their CLIs: this test is
// what notices when one of them changes. Each is skipped when its AI is
// not usable here; OpenCode without a Go plan (or not signed in to the
// Console) has no usage, which is logged, not failed.
func TestLT18Usage(t *testing.T) {
	ctx := context.Background()
	t.Run("codex", func(t *testing.T) {
		a := codex.New(codex.Options{SchemaDir: t.TempDir()})
		if info := a.Info(ctx); !info.Available || len(info.Notices) != 0 {
			t.Skipf("codex is not usable here: %v", info.Notices)
		}
		start := time.Now()
		g := a.Usage(ctx)
		took := time.Since(start)
		if len(g) != 2 || g[0].Window != core.Window5h || g[1].Window != core.WindowWeek {
			t.Fatalf("usage = %+v (%v)", g, took)
		}
		checkGroups(t, g)
		t.Logf("    codex: 5 hours %.0f%% left, week %.0f%% left, %.1f s", g[0].Left*100, g[1].Left*100, took.Seconds())
	})
	t.Run("opencode", func(t *testing.T) {
		a := opencode.New(opencode.Options{})
		if info := a.Info(ctx); !info.Available {
			t.Skipf("opencode is not usable here: %v", info.Notices)
		}
		start := time.Now()
		g := a.Usage(ctx)
		took := time.Since(start)
		if g == nil {
			t.Logf("    opencode: no Go usage (no Go plan, or not signed in to the Console), %.1f s", took.Seconds())
			return
		}
		if len(g) != 3 || g[0].Window != core.Window5h || g[1].Window != core.WindowWeek || g[2].Window != core.WindowMonth {
			t.Fatalf("usage = %+v (%v)", g, took)
		}
		checkGroups(t, g)
		t.Logf("    opencode go: 5 hours %.0f%%, week %.0f%%, month %.0f%% left, %.1f s", g[0].Left*100, g[1].Left*100, g[2].Left*100, took.Seconds())
	})
}

func checkGroups(t *testing.T, gs []core.UsageGroup) {
	t.Helper()
	for _, g := range gs {
		if g.Left < 0 || g.Left > 1 || g.Name == "" {
			t.Errorf("group %+v", g)
		}
		if g.ResetAt != "" {
			if r, err := time.Parse(time.RFC3339, g.ResetAt); err != nil || r.Before(time.Now().Add(-time.Hour)) || r.After(time.Now().Add(32*24*time.Hour)) {
				t.Errorf("reset of %s: %q", g.Window, g.ResetAt)
			}
		}
	}
}
