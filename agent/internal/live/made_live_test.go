//go:build live

package live

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/adapter/agy"
	"github.com/heeo-dev/askorus/agent/internal/adapter/codex"
	"github.com/heeo-dev/askorus/agent/internal/core"
)

// LT-21 Made pictures (#14) with the real CLIs, on the quick tier, as a
// question's own ask with pictures sends them (the make guide after the
// preamble, the image tool on): Codex is asked for a small photo-like
// picture and must bring at least one checked picture from its own folder,
// with no path of the PC in its answer. A question that asks for no picture
// makes none. Antigravity's picture tool has a weekly limit of a few
// pictures, so it is asked only with ASKORUS_LIVE_AGY_PICTURES=1; a limit
// reached is then recorded, not a failure. Codex: 2 asks.
func TestLT21MadePictures(t *testing.T) {
	ctx := context.Background()
	picture := core.AskPromptGuided("English", "Make a small photo-like picture of one red apple on a white table.", core.GuideMake)
	text := core.AskPromptGuided("English", "Reply with the single word: ok", core.GuideMake)
	check := func(t *testing.T, name string, res core.AskResult) {
		t.Helper()
		home, _ := os.UserHomeDir()
		if home != "" && strings.Contains(strings.ToLower(res.Answer), strings.ToLower(home)) {
			t.Errorf("%s's answer names the user folder: %q", name, res.Answer)
		}
		for _, f := range res.Made {
			info, err := os.Lstat(f.Path)
			if err != nil || !info.Mode().IsRegular() || info.Size() > core.MaxMadeBytes || f.Type == "" {
				t.Errorf("%s: %+v (%v)", name, f, err)
			}
		}
	}

	t.Run("codex", func(t *testing.T) {
		a := codex.New(codex.Options{SchemaDir: t.TempDir(), Follow: true})
		if info := a.Info(ctx); !info.Available || a.Makes() == nil {
			t.Skipf("codex cannot make pictures here: %v", info.Notices)
		}
		folder := askFolder(t)
		os.MkdirAll(folder, 0o700)
		start := time.Now()
		res, err := a.Ask(ctx, core.AskRequest{Folder: folder, Prompt: picture, Tier: core.TierQuick, Pictures: true})
		if err != nil {
			t.Fatalf("ask: %v", err)
		}
		if len(res.Made) == 0 {
			t.Fatalf("codex made no picture: %q", res.Answer)
		}
		check(t, "codex", res)
		t.Logf("    codex: %d picture(s) (%s, %s) in %.1f s", len(res.Made), res.Made[0].Type, filepath.Base(res.Made[0].Path), time.Since(start).Seconds())
		res, err = a.Ask(ctx, core.AskRequest{Folder: folder, Prompt: text, Tier: core.TierQuick, Pictures: true})
		if err != nil || len(res.Made) != 0 {
			t.Errorf("a question without a picture: %d made, %v", len(res.Made), err)
		}
	})

	t.Run("agy", func(t *testing.T) {
		if os.Getenv("ASKORUS_LIVE_AGY_PICTURES") != "1" {
			t.Skip("Antigravity's pictures have a weekly limit: set ASKORUS_LIVE_AGY_PICTURES=1 to ask")
		}
		a := agy.New(agy.Options{})
		if info := a.Info(ctx); !info.Available || a.Makes() == nil {
			t.Skipf("agy cannot make pictures here: %v", info.Notices)
		}
		folder := askFolder(t)
		os.MkdirAll(folder, 0o700)
		res, err := a.Ask(ctx, core.AskRequest{Folder: folder, Prompt: picture, Tier: core.TierQuick, Pictures: true})
		if err != nil {
			t.Fatalf("ask: %v", err)
		}
		if res.PictureLimit != nil {
			t.Logf("    agy: picture limit reached, until %q", res.PictureLimit.Until)
			return
		}
		if len(res.Made) == 0 {
			t.Fatalf("agy made no picture: %q", res.Answer)
		}
		check(t, "agy", res)
		if !a.MakesIn(res.SessionID) {
			t.Error("agy does not remember its draw conversation")
		}
		t.Logf("    agy: %d picture(s) (%s)", len(res.Made), res.Made[0].Type)
	})
}
