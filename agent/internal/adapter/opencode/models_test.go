package opencode

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

func TestListedModels(t *testing.T) {
	out := "opencode-go/deepseek-v4.2-flash\r\nopencode-go/deepseek-v4-pro\r\nnot a model\r\nopencode/big-pickle\r\nx#high\r\n-m\r\n"
	var names []string
	for _, m := range listedModels([]byte(out)) {
		names = append(names, m.Name)
		if m.Efforts != nil {
			t.Errorf("%s has efforts %v: OpenCode does not list them", m.Name, m.Efforts)
		}
	}
	if !slices.Equal(names, []string{"opencode-go/deepseek-v4.2-flash", "opencode-go/deepseek-v4-pro", "opencode/big-pickle"}) {
		t.Errorf("names = %q", names)
	}
}

// followSetup is an OpenCode on the shared fake with its model list read.
func followSetup(t *testing.T, newer string, o Options) (*Adapter, string, string) {
	t.Helper()
	o.Path = fakecli.Use(t, fakecli.RoleOpenCode)
	t.Setenv(fakecli.EnvNewer, newer)
	ask := filepath.Join(t.TempDir(), "ask")
	jf := core.JudgeFolder(ask)
	if err := os.MkdirAll(jf, 0o700); err != nil {
		t.Fatal(err)
	}
	a := New(o)
	ctx := context.Background()
	a.Info(ctx)
	a.WaitModels(ctx)
	a.usage.Wait(ctx)
	return a, ask, jf
}

// runModels are the -m of the recorded runs, in order.
func runModels(t *testing.T) []string {
	t.Helper()
	var ms []string
	for _, c := range fakecli.Calls(t) {
		if i := slices.Index(c.Args, "-m"); len(c.Args) > 0 && c.Args[0] == "run" && i >= 0 {
			ms = append(ms, c.Args[i+1])
		}
	}
	return ms
}

// TestFollowNewest (#64): quick and standard move to the newest Flash with
// their variants; deep stays on the newest Pro; a question with photos
// keeps the Flash known to see them.
func TestFollowNewest(t *testing.T) {
	a, ask, _ := followSetup(t, fakecli.NewerList, Options{Follow: true})
	tiers := a.AskTiers()
	if tiers[core.TierQuick] != (core.AskModel{Model: fakecli.NewerOpenCode, Effort: "low"}) ||
		tiers[core.TierStandard] != (core.AskModel{Model: fakecli.NewerOpenCode, Effort: "high"}) ||
		tiers[core.TierDeep] != (core.AskModel{Model: "opencode-go/deepseek-v4-pro", Effort: "high"}) {
		t.Fatalf("tiers = %+v", tiers)
	}
	if n := a.Newer(); len(n) != 1 || n["opencode-go/deepseek-v4.1-flash"] != fakecli.NewerOpenCode {
		t.Errorf("Newer = %v", n)
	}
	// Variants of a newer model are not known: the ones its tiers use.
	if es := a.AskEfforts()[fakecli.NewerOpenCode]; !slices.Equal(es, []string{"low", "high"}) {
		t.Errorf("efforts = %v", es)
	}
	if im := a.ImageModels().Models; !slices.Equal(im, []string{"opencode-go/deepseek-v4.1-flash", fakecli.NewerOpenCode}) || !a.SeesImages(fakecli.NewerOpenCode+"#high") {
		t.Errorf("image models = %v", im)
	}
	ctx := context.Background()
	if _, err := a.Ask(ctx, core.AskRequest{Folder: ask, Prompt: "hi", Tier: core.TierStandard}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Ask(ctx, core.AskRequest{Folder: ask, Prompt: "photo?", Tier: core.TierStandard, Images: keptPhotos(t)}); err != nil {
		t.Fatal(err)
	}
	if ms := runModels(t); !slices.Equal(ms, []string{fakecli.NewerOpenCode + "#high", "opencode-go/deepseek-v4.1-flash#high"}) {
		t.Errorf("ran %q", ms)
	}

	pinned, _, _ := followSetup(t, fakecli.NewerList, Options{Follow: true, Tiers: map[string]core.AskModel{core.TierStandard: {Model: "opencode-go/deepseek-v4.1-flash", Effort: "max"}}})
	if m := pinned.AskTiers()[core.TierStandard]; m.Model != "opencode-go/deepseek-v4.1-flash" {
		t.Errorf("agent.json's standard moved: %+v", m)
	}
}

// TestFollowRefused (#64): a newer model whose variant OpenCode refuses is
// asked again with the shipped one, ask and combined answer alike, and
// left out until the next list.
func TestFollowRefused(t *testing.T) {
	a, ask, jf := followSetup(t, fakecli.NewerRefuse, Options{Follow: true})
	res, err := a.Ask(context.Background(), core.AskRequest{Folder: ask, Prompt: "hi", Tier: core.TierQuick})
	if err != nil || !strings.HasPrefix(res.Answer, "fake: ") {
		t.Fatalf("%+v %v", res, err)
	}
	if ms := runModels(t); !slices.Equal(ms, []string{fakecli.NewerOpenCode + "#low", "opencode-go/deepseek-v4.1-flash#low"}) {
		t.Errorf("ran %q", ms)
	}
	if m := a.AskTiers()[core.TierQuick].Model; m != "opencode-go/deepseek-v4.1-flash" {
		t.Errorf("quick after the refusal = %q", m)
	}

	b, _, _ := followSetup(t, fakecli.NewerRefuse, Options{Follow: true})
	req := synthRequest(t, jf)
	req.Model = fakecli.NewerOpenCode
	sr, err := b.Synthesize(context.Background(), req)
	if err != nil || sr.Model != "opencode-go/deepseek-v4.1-flash#max" {
		t.Errorf("synth: %+v %v", sr, err)
	}
}
