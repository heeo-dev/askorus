package agy

import (
	"context"
	"slices"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

func TestListedModels(t *testing.T) {
	out := "Fetching available models...\n" +
		"gemini-3.9-flash-high\tGemini 3.9 Flash (High)\n" +
		"gemini-3.9-flash-low\tGemini 3.9 Flash (Low)\r\n" +
		"gemini-4-hydrogen\tGemini 4 Hydrogen\n" +
		"claude-sonnet-4-6\tClaude Sonnet 4.6 (Thinking)\n"
	got := listedModels([]byte(out))
	if len(got) != 2 || got[0].Name != "gemini-3.9-flash" || !slices.Equal(got[0].Efforts, []string{"low", "high"}) ||
		got[1].Name != "gemini-4-hydrogen" || got[1].Efforts == nil || len(got[1].Efforts) != 0 {
		t.Errorf("got %+v", got)
	}
}

// followAgy is an agy on the shared fake with its model list read.
func followAgy(t *testing.T, newer string, o Options) (*Adapter, string) {
	t.Helper()
	o.Path = fakecli.Use(t, fakecli.RoleAgy)
	t.Setenv(fakecli.EnvNewer, newer)
	o.ConfigDir = t.TempDir()
	a := New(o)
	a.Info(context.Background())
	a.WaitModels(context.Background())
	return a, t.TempDir()
}

func runModels(t *testing.T) []string {
	t.Helper()
	var ms []string
	for _, c := range fakecli.Calls(t) {
		if i := slices.Index(c.Args, "--model"); i >= 0 {
			ms = append(ms, c.Args[i+1])
		}
	}
	return ms
}

// TestFollowNewest (#64): every tier moves to the newest Gemini Flash with
// its level; agent.json's model stays.
func TestFollowNewest(t *testing.T) {
	a, folder := followAgy(t, fakecli.NewerList, Options{Follow: true})
	for tier, level := range map[string]string{core.TierQuick: "low", core.TierStandard: "medium", core.TierDeep: "high"} {
		if m := a.AskTiers()[tier]; m.Model != fakecli.NewerAgy || m.Effort != level {
			t.Errorf("%s = %+v", tier, m)
		}
	}
	if n := a.Newer(); len(n) != 1 || n["gemini-3.8-flash"] != fakecli.NewerAgy {
		t.Errorf("Newer = %v", n)
	}
	if es := a.AskEfforts()[fakecli.NewerAgy]; !slices.Equal(es, []string{"low", "medium", "high"}) {
		t.Errorf("efforts = %v", es)
	}
	if r, err := ask(t, a, core.AskRequest{Folder: folder}); err != nil || r.Model != fakecli.NewerAgy+"-medium" {
		t.Fatalf("%+v %v", r, err)
	}
	if ms := runModels(t); !slices.Equal(ms, []string{fakecli.NewerAgy + "-medium"}) {
		t.Errorf("ran %q", ms)
	}

	pinned, _ := followAgy(t, fakecli.NewerList, Options{Follow: true, Tiers: map[string]core.AskModel{core.TierDeep: {Model: "gemini-3.8-flash", Effort: "high"}}})
	if m := pinned.AskTiers()[core.TierDeep]; m.Model != "gemini-3.8-flash" {
		t.Errorf("agent.json's deep moved: %+v", m)
	}
}

// R-148 (2ho's 0.5.1 review): a request the agent itself refuses runs
// nothing and leaves no model out.
func TestOwnCheckKeepsTheNewerModel(t *testing.T) {
	a, folder := followAgy(t, fakecli.NewerRefuse, Options{Follow: true})
	if _, err := ask(t, a, core.AskRequest{Folder: folder, Tier: core.TierQuick, Effort: "turbo"}); core.ErrorCode(err) != core.ErrCodeBadModel || len(runModels(t)) != 0 {
		t.Fatalf("err %v, ran %q", err, runModels(t))
	}
	if m := a.AskTiers()[core.TierQuick].Model; m != fakecli.NewerAgy {
		t.Errorf("quick = %q: the newer model was left out", m)
	}
}

// TestFollowRefused (#64): a newer model agy does not take is asked again
// with the shipped one, ask and combined answer alike.
func TestFollowRefused(t *testing.T) {
	a, folder := followAgy(t, fakecli.NewerRefuse, Options{Follow: true})
	if r, err := ask(t, a, core.AskRequest{Folder: folder, Tier: core.TierQuick}); err != nil || r.Model != "gemini-3.8-flash-low" {
		t.Fatalf("%+v %v", r, err)
	}
	if ms := runModels(t); !slices.Equal(ms, []string{fakecli.NewerAgy + "-low", "gemini-3.8-flash-low"}) {
		t.Errorf("ran %q", ms)
	}
	if m := a.AskTiers()[core.TierQuick].Model; m != "gemini-3.8-flash" {
		t.Errorf("quick after the refusal = %q", m)
	}

	b, folder := followAgy(t, fakecli.NewerRefuse, Options{Follow: true})
	if r, err := synthesize(t, b, core.SynthRequest{Folder: folder, Model: fakecli.NewerAgy}); err != nil || r.Model != "gemini-3.8-flash-medium" {
		t.Errorf("synth: %+v %v", r, err)
	}
}
