package opencode

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// Photos with a question (#14): opencode gets "--file" and the path of
// each kept photo, at the end, and only with a model known to see photos.

var (
	photoJPEG = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{5}, 90)...)
	photoPNG  = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{6}, 90)...)
)

// keptPhotos writes two photos as internal/uploads keeps them.
func keptPhotos(t *testing.T) []core.Image {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "uploads")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ims := []core.Image{
		{Path: filepath.Join(dir, "up_"+strings.Repeat("a", 32)+".jpg"), Type: core.ImageJPEG},
		{Path: filepath.Join(dir, "up_"+strings.Repeat("b", 32)+".png"), Type: core.ImagePNG},
	}
	for i, data := range [][]byte{photoJPEG, photoPNG} {
		if err := os.WriteFile(ims[i].Path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return ims
}

func TestAskNamesPhotosWithFile(t *testing.T) {
	a, ask, _ := judgeSetup(t)
	ims := keptPhotos(t)
	res, err := a.Ask(context.Background(), core.AskRequest{Folder: ask, Prompt: "이 사진은 뭐야?", Tier: core.TierStandard, Images: ims})
	if err != nil {
		t.Fatal(err)
	}
	if res.Answer != "fake: 이 사진은 뭐야? [photos: 2]" {
		t.Errorf("answer = %q", res.Answer)
	}
	cs := fakecli.Calls(t)
	want := []string{"run", "--standalone", "--format", "json", "--agent", "askorus-ask", "-m", "opencode-go/deepseek-v4.1-flash#high",
		"--file", ims[0].Path, "--file", ims[1].Path}
	if len(cs) != 1 || !slices.Equal(cs[0].Args, want) || cs[0].Stdin != "이 사진은 뭐야?" || cs[0].Env["OPENCODE_CONFIG_CONTENT"] != askConfig {
		t.Fatalf("call = %+v\nwant args %q", cs, want)
	}
	follow := askImageArgs(testDefaultModel, "ses_e773ef84effedagCyC2bkvl1W9", []string{ims[0].Path})
	if err := checkArgs(follow); err != nil || !slices.Equal(follow[len(follow)-4:], []string{"--session", "ses_e773ef84effedagCyC2bkvl1W9", "--file", ims[0].Path}) {
		t.Errorf("follow-up = %q, %v", follow, err)
	}
	if !slices.Equal(askImageArgs(testDefaultModel, "", nil), askArgs(testDefaultModel, "")) {
		t.Error("an ask without photos changed")
	}
}

func TestAModelThatDoesNotSeeIsNotAsked(t *testing.T) {
	a, ask, _ := judgeSetup(t)
	ims := keptPhotos(t)
	// The deep tier is DeepSeek V4 Pro, which answers without seeing them.
	_, err := a.Ask(context.Background(), core.AskRequest{Folder: ask, Prompt: "q", Tier: core.TierDeep, Images: ims})
	if core.ErrorCode(err) != core.ErrCodeNoImages {
		t.Fatalf("deep tier with photos = %v", err)
	}
	_, err = a.Ask(context.Background(), core.AskRequest{Folder: ask, Prompt: "q", Tier: core.TierQuick, Model: "opencode-go/some-new-model", Images: ims})
	if core.ErrorCode(err) != core.ErrCodeNoImages {
		t.Fatalf("an unknown model with photos = %v", err)
	}
	if n := len(fakecli.Calls(t)); n != 0 {
		t.Errorf("opencode ran %d times", n)
	}
	// The same models answer a question without photos.
	if _, err := a.Ask(context.Background(), core.AskRequest{Folder: ask, Prompt: "q", Tier: core.TierDeep}); err != nil {
		t.Errorf("deep tier without photos: %v", err)
	}
	var seer core.ImageSeer = a
	for model, want := range map[string]bool{
		"opencode-go/deepseek-v4.1-flash":      true,
		"opencode-go/deepseek-v4.1-flash#low":  true,
		"opencode-go/deepseek-v4.1-flash#high": true,
		"opencode-go/deepseek-v4-pro#high":     false,
		"opencode-go/deepseek-v4.1-flash-x":    false,
		"":                                     false,
	} {
		if seer.SeesImages(model) != want {
			t.Errorf("SeesImages(%q) = %v", model, !want)
		}
	}
	if im := seer.ImageModels(); im.All || !slices.Equal(im.Models, []string{"opencode-go/deepseek-v4.1-flash"}) {
		t.Errorf("ImageModels = %+v", im)
	}
}

func TestCheckArgsTakesOnlyKeptPhotos(t *testing.T) {
	ims := keptPhotos(t)
	dir := filepath.Dir(ims[0].Path)
	with := func(paths ...string) []string { return askImageArgs(testDefaultModel, "", paths) }
	if err := checkArgs(with(ims[0].Path, ims[1].Path)); err != nil {
		t.Fatalf("two kept photos: %v", err)
	}
	for name, args := range map[string][]string{
		"another file":    with(filepath.Join(filepath.Dir(dir), "auth.json")),
		"a relative path": with(filepath.Base(ims[0].Path)),
		"a flag":          with("--auto"),
		"a longer name":   with(ims[0].Path + ".txt"),
		"the same twice":  with(ims[0].Path, ims[0].Path),
		"photos before the model": func() []string {
			args := askArgs(testDefaultModel, "")
			return slices.Insert(args, 1, "--file", ims[0].Path)
		}(),
	} {
		if err := checkArgs(args); err == nil {
			t.Errorf("%s: checkArgs(%q) took it", name, args)
		}
	}
}

func TestSynthesizeNamesPhotosWithFile(t *testing.T) {
	a, _, jf := judgeSetup(t)
	ims := keptPhotos(t)
	req := synthRequest(t, jf)
	req.Images = ims
	res, err := a.Synthesize(context.Background(), req)
	if err != nil || len(res.Output) == 0 {
		t.Fatalf("Synthesize = %+v, %v", res, err)
	}
	cs := fakecli.Calls(t)
	want := append(synthArgs(modelArg(testSynthModel, "max")), "--file", ims[0].Path, "--file", ims[1].Path)
	if len(cs) != 1 || !slices.Equal(cs[0].Args, want) || cs[0].Photos != 2 || cs[0].Stdin != req.Prompt || cs[0].Env["OPENCODE_CONFIG_CONTENT"] != synthConfig {
		t.Fatalf("call = %+v\nwant args %q", cs, want)
	}
	if err := checkSynthArgs(want); err != nil {
		t.Errorf("checkSynthArgs: %v", err)
	}
	if err := checkArgs(want); err == nil {
		t.Error("checkArgs took the writer's command")
	}
	model := modelArg(testSynthModel, "max")
	for name, args := range map[string][]string{
		"another file":       append(synthArgs(model), "--file", filepath.Join(filepath.Dir(ims[0].Path), "notes.txt")),
		"refine with photos": append(refineArgs(model), "--file", ims[0].Path),
		"a stray argument":   append(synthArgs(model), "--file", ims[0].Path, "--auto"),
		"the same twice":     append(synthArgs(model), "--file", ims[0].Path, "--file", ims[0].Path),
	} {
		if err := checkSynthArgs(args); err == nil {
			t.Errorf("%s: checkSynthArgs took %q", name, args)
		}
	}
	// A writer whose model does not see photos, and a question spec, get none.
	blind := req
	blind.Model, blind.Effort = "opencode-go/deepseek-v4-pro", "high"
	if _, err := a.Synthesize(context.Background(), blind); core.ErrorCode(err) != core.ErrCodeInternal {
		t.Errorf("a blind writer with photos = %v", err)
	}
	req.Refine = true
	if _, err := a.Synthesize(context.Background(), req); core.ErrorCode(err) != core.ErrCodeInternal {
		t.Errorf("refine with photos = %v", err)
	}
	if n := len(fakecli.Calls(t)); n != 1 {
		t.Errorf("opencode ran %d times", n)
	}
}
