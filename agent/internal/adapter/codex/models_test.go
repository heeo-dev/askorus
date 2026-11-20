package codex

import (
	"context"
	"slices"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// TestParseModels: the listed models with their efforts; hidden ones,
// unknown efforts, names that could pass for a flag and anything that is
// not the catalog are left out.
func TestParseModels(t *testing.T) {
	out := `{"models":[
		{"slug":"gpt-6.2-sol","visibility":"list","supported_reasoning_levels":[{"effort":"high"},{"effort":"low","description":"x"},{"effort":"turbo"}]},
		{"slug":"codex-auto-review","visibility":"hide","supported_reasoning_levels":[{"effort":"low"}]},
		{"slug":"gpt-6-luna","visibility":"list","supported_reasoning_levels":["medium","max"]},
		{"slug":"-m","visibility":"list"}]}`
	got := parseModels([]byte(out))
	want := []core.ListedModel{{Name: "gpt-6.2-sol", Efforts: []string{"low", "high"}}, {Name: "gpt-6-luna", Efforts: []string{"medium", "max"}}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i].Name != want[i].Name || !slices.Equal(got[i].Efforts, want[i].Efforts) {
			t.Errorf("%d: %+v, want %+v", i, got[i], want[i])
		}
	}
	for _, junk := range []string{"", "Error: not signed in", `{"models":"x"}`} {
		if l := parseModels([]byte(junk)); len(l) != 0 {
			t.Errorf("%q listed %+v", junk, l)
		}
	}
}

// execModels are the models of the recorded codex exec runs, in order.
func execModels(t *testing.T) []string {
	t.Helper()
	var ms []string
	for _, c := range fakecli.Calls(t) {
		if i := slices.Index(c.Args, "-m"); slices.Contains(c.Args, "exec") && i >= 0 {
			ms = append(ms, c.Args[i+1])
		}
	}
	return ms
}

// followAdapter is a codex on the shared fake with its model list read.
func followAdapter(t *testing.T, newer string, o Options) *Adapter {
	t.Helper()
	o.Path = fakecli.Use(t, fakecli.RoleCodex)
	t.Setenv(fakecli.EnvNewer, newer)
	o.SchemaDir = t.TempDir()
	a := New(o)
	ctx := context.Background()
	a.Info(ctx)
	a.WaitModels(ctx)
	a.usage.Wait(ctx)
	return a
}

// TestFollowNewest (#64): deep moves to the newest Sol; the others stay
// on the newest of theirs; agent.json's model stays; off, nothing moves.
func TestFollowNewest(t *testing.T) {
	a := followAdapter(t, fakecli.NewerList, Options{Follow: true})
	tiers := a.AskTiers()
	if tiers[core.TierDeep].Model != fakecli.NewerCodex || tiers[core.TierStandard].Model != "gpt-6.1-terra" || tiers[core.TierQuick].Model != "gpt-6-luna" {
		t.Fatalf("tiers = %+v", tiers)
	}
	if n := a.Newer(); len(n) != 1 || n["gpt-6.1-sol"] != fakecli.NewerCodex {
		t.Errorf("Newer = %v", n)
	}
	if es := a.AskEfforts()[fakecli.NewerCodex]; !slices.Equal(es, []string{"low", "medium", "high", "xhigh", "max", "ultra"}) {
		t.Errorf("efforts = %v", es)
	}
	if a.Follow("gpt-6.1-sol") != fakecli.NewerCodex || a.Follow("gpt-6-sol") != fakecli.NewerCodex || a.Follow("gpt-6-luna") != "gpt-6-luna" {
		t.Error("Follow")
	}
	// #65: Astra outside the tiers, heavy, with its six efforts; an old
	// Astra pick moves to it.
	if x := a.AskExtra(); len(x) != 1 || x[0] != (core.ExtraModel{Model: "gpt-6.2-astra", Heavy: true}) {
		t.Errorf("AskExtra = %+v", x)
	}
	if es := a.AskEfforts()["gpt-6.2-astra"]; len(es) != 6 || a.Follow("gpt-6-astra") != "gpt-6.2-astra" {
		t.Errorf("astra efforts %v, follow %q", es, a.Follow("gpt-6-astra"))
	}
	// An ask at deep, and one with an own pick saved before (the old Sol).
	folder := t.TempDir()
	for _, req := range []core.AskRequest{{Tier: core.TierDeep}, {Tier: core.TierQuick, Model: "gpt-6.1-sol"}} {
		req.Folder, req.Prompt = folder, core.AskPrompt("hi")
		if res, err := a.Ask(context.Background(), req); err != nil || res.Model != fakecli.NewerCodex {
			t.Fatalf("ask %+v: %+v %v", req, res, err)
		}
	}
	if ms := execModels(t); !slices.Equal(ms, []string{fakecli.NewerCodex, fakecli.NewerCodex}) {
		t.Errorf("ran %v", ms)
	}

	pinned := followAdapter(t, fakecli.NewerList, Options{Follow: true, Tiers: map[string]core.AskModel{core.TierDeep: {Model: "gpt-6.1-sol"}}})
	if m := pinned.AskTiers()[core.TierDeep]; m.Model != "gpt-6.1-sol" || len(pinned.Newer()) != 0 || pinned.Follow("gpt-6.1-sol") != "gpt-6.1-sol" {
		t.Errorf("agent.json's deep moved: %+v", m)
	}

	off := followAdapter(t, fakecli.NewerList, Options{})
	if m := off.AskTiers()[core.TierDeep]; m.Model != "gpt-6.1-sol" || off.follow.Listed() {
		t.Errorf("off: %+v", m)
	}
}

// R-148 (2ho's 0.5.1 review): a request the agent itself refuses (an
// effort codex does not have, from a paired phone) runs nothing and leaves
// no model out for the other phones.
func TestOwnCheckKeepsTheNewerModel(t *testing.T) {
	a := followAdapter(t, fakecli.NewerRefuse, Options{Follow: true})
	_, err := a.Ask(context.Background(), core.AskRequest{Folder: t.TempDir(), Prompt: core.AskPrompt("hi"), Tier: core.TierDeep, Effort: "turbo"})
	if core.ErrorCode(err) != core.ErrCodeBadModel || len(execModels(t)) != 0 {
		t.Fatalf("err %v, ran %v", err, execModels(t))
	}
	if m := a.AskTiers()[core.TierDeep].Model; m != fakecli.NewerCodex {
		t.Errorf("deep = %q: the newer model was left out", m)
	}
	if _, err := a.Synthesize(context.Background(), core.SynthRequest{Folder: t.TempDir(), Prompt: synthPrompt(t), Model: fakecli.NewerCodex, Effort: "turbo"}); core.ErrorCode(err) != core.ErrCodeBadModel {
		t.Errorf("synth err %v", err)
	}
	if m := a.AskTiers()[core.TierDeep].Model; m != fakecli.NewerCodex {
		t.Errorf("deep after the synth = %q", m)
	}
}

// TestFollowRefused (#64): a newer model codex refuses is asked again with
// the shipped one, and left out until the next list.
func TestFollowRefused(t *testing.T) {
	a := followAdapter(t, fakecli.NewerRefuse, Options{Follow: true})
	if a.AskTiers()[core.TierDeep].Model != fakecli.NewerCodex {
		t.Fatal("not followed")
	}
	res, err := a.Ask(context.Background(), core.AskRequest{Folder: t.TempDir(), Prompt: core.AskPrompt("hi"), Tier: core.TierDeep})
	if err != nil || res.Model != "gpt-6.1-sol" || res.Answer == "" {
		t.Fatalf("%+v %v", res, err)
	}
	if ms := execModels(t); !slices.Equal(ms, []string{fakecli.NewerCodex, "gpt-6.1-sol"}) {
		t.Errorf("ran %v", ms)
	}
	if m := a.AskTiers()[core.TierDeep].Model; m != "gpt-6.1-sol" || len(a.Newer()) != 0 {
		t.Errorf("deep after the refusal = %q", m)
	}

	// R-149 (2ho's 0.5.1 review): the refused model named again (a pick
	// saved while it was deep's) goes to the shipped one without running it.
	from := len(execModels(t))
	if res, err := a.Ask(context.Background(), core.AskRequest{Folder: t.TempDir(), Prompt: core.AskPrompt("hi"), Tier: core.TierQuick, Model: fakecli.NewerCodex}); err != nil || res.Model != "gpt-6.1-sol" {
		t.Fatalf("pick of the refused model: %+v %v", res, err)
	}
	if ms := execModels(t)[from:]; !slices.Equal(ms, []string{"gpt-6.1-sol"}) {
		t.Errorf("pick of the refused model ran %v", ms)
	}

	// The combined answer too.
	b := followAdapter(t, fakecli.NewerRefuse, Options{Follow: true})
	sr, err := b.Synthesize(context.Background(), core.SynthRequest{Folder: t.TempDir(), Prompt: synthPrompt(t), Model: fakecli.NewerCodex})
	if err != nil || sr.Model != "gpt-6.1-sol" {
		t.Errorf("synth: %+v %v", sr, err)
	}
}
