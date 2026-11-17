package codex

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

// Photos with a question (#14): codex gets "-i" and the path of each kept
// photo, before --json. The question still goes on stdin.

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

func TestAskNamesPhotosWithDashI(t *testing.T) {
	a, _, folder := judgeAdapter(t)
	ims := keptPhotos(t)
	res, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "이 사진은 뭐야?", Tier: core.TierQuick, Images: ims})
	if err != nil {
		t.Fatal(err)
	}
	if res.Answer != "fake: 이 사진은 뭐야? [photos: 2]" {
		t.Errorf("answer = %q", res.Answer)
	}
	cs := fakecli.Calls(t)
	want := slices.Concat([]string{"--search", "exec", "-s", "read-only", "--ignore-user-config", "--skip-git-repo-check",
		"-i", ims[0].Path, "-i", ims[1].Path, "--json", "-m", "gpt-6-luna", "-c", `model_reasoning_effort="medium"`},
		wantDisables(), []string{"-"})
	if len(cs) != 1 || !slices.Equal(cs[0].Args, want) || cs[0].Stdin != "이 사진은 뭐야?" {
		t.Fatalf("call = %+v\nwant args %q", cs, want)
	}
	// A follow-up and a one-off take them in the same place.
	follow := askImageArgs("gpt-6-luna", "medium", testThread, false, []string{ims[0].Path})
	oneOff := askImageArgs("gpt-6-luna", "medium", "", true, []string{ims[0].Path})
	if i := slices.Index(follow, "-i"); follow[i-1] != "--skip-git-repo-check" || follow[i+2] != "--json" || follow[4] != "resume" {
		t.Errorf("follow-up = %q", follow)
	}
	if i := slices.Index(oneOff, "-i"); oneOff[i-1] != "--ephemeral" || oneOff[i+2] != "--json" {
		t.Errorf("one-off = %q", oneOff)
	}
	for _, args := range [][]string{follow, oneOff} {
		if err := checkArgs(args); err != nil {
			t.Errorf("checkArgs(%q) = %v", args, err)
		}
	}
	// Without photos the command is the one it always was.
	if !slices.Equal(askImageArgs("gpt-6-luna", "medium", "", false, nil), askArgs("gpt-6-luna", "medium", "", false)) {
		t.Error("an ask without photos changed")
	}
}

func TestCheckArgsTakesOnlyKeptPhotos(t *testing.T) {
	ims := keptPhotos(t)
	dir := filepath.Dir(ims[0].Path)
	with := func(paths ...string) []string { return askImageArgs("gpt-6-luna", "medium", "", false, paths) }
	if err := checkArgs(with(ims[0].Path, ims[1].Path)); err != nil {
		t.Fatalf("two kept photos: %v", err)
	}
	outside := filepath.Join(filepath.Dir(dir), "win.ini")
	bad := map[string][]string{
		"another file":       with(outside),
		"a relative path":    with(filepath.Base(ims[0].Path)),
		"a path with ..":     with(dir + string(filepath.Separator) + ".." + string(filepath.Separator) + "uploads" + string(filepath.Separator) + filepath.Base(ims[0].Path)),
		"a flag":             with("--dangerously-bypass-approvals-and-sandbox"),
		"a longer name":      with(filepath.Join(dir, "up_"+strings.Repeat("a", 32)+".jpg.exe")),
		"an upper-case name": with(filepath.Join(dir, "UP_"+strings.Repeat("a", 32)+".jpg")),
		"the same twice":     with(ims[0].Path, ims[0].Path),
		"five":               with(ims[0].Path, ims[1].Path, filepath.Join(dir, "up_"+strings.Repeat("c", 32)+".webp"), filepath.Join(dir, "up_"+strings.Repeat("d", 32)+".jpg"), filepath.Join(dir, "up_"+strings.Repeat("e", 32)+".jpg")),
		"photos after --json": func() []string {
			args := askArgs("gpt-6-luna", "medium", "", false)
			return slices.Insert(args, len(args)-1, "-i", ims[0].Path)
		}(),
	}
	for name, args := range bad {
		if err := checkArgs(args); err == nil {
			t.Errorf("%s: checkArgs(%q) took it", name, args)
		}
	}
}

func TestAskRefusesPhotosItCannotUse(t *testing.T) {
	a, _, folder := judgeAdapter(t)
	ims := keptPhotos(t)
	outside := filepath.Join(t.TempDir(), "notes.jpg")
	os.WriteFile(outside, photoJPEG, 0o600)
	for name, bad := range map[string][]core.Image{
		"not a kept photo": {{Path: outside, Type: core.ImageJPEG}},
		"the wrong type":   {{Path: ims[0].Path, Type: core.ImagePNG}},
		"twice":            {ims[0], ims[0]},
	} {
		if _, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierQuick, Images: bad}); core.ErrorCode(err) != core.ErrCodeInternal {
			t.Errorf("%s: %v", name, err)
		}
	}
	if n := len(fakecli.Calls(t)); n != 0 {
		t.Errorf("codex ran %d times", n)
	}
}

func TestSynthesizeNamesPhotosWithDashI(t *testing.T) {
	a, _, folder := judgeAdapter(t)
	ims := keptPhotos(t)
	req := core.SynthRequest{Folder: folder, Prompt: synthPrompt(t), Model: "gpt-6-luna", Images: ims}
	res, err := a.Synthesize(context.Background(), req)
	if err != nil || len(res.Output) == 0 {
		t.Fatalf("Synthesize = %+v, %v", res, err)
	}
	cs := fakecli.Calls(t)
	plain := wantJudgeArgs("gpt-6-luna", "medium", a.synthSchemaPath())
	want := slices.Insert(slices.Clone(plain), 6, "-i", ims[0].Path, "-i", ims[1].Path)
	if len(cs) != 1 || !slices.Equal(cs[0].Args, want) || cs[0].Photos != 2 || cs[0].Stdin != req.Prompt {
		t.Fatalf("call = %+v\nwant args %q", cs, want)
	}
	if err := a.checkSynthArgs(want); err != nil {
		t.Errorf("checkSynthArgs: %v", err)
	}
	if err := checkArgs(want); err == nil {
		t.Error("checkArgs took the writer's command")
	}
	outside := filepath.Join(filepath.Dir(filepath.Dir(ims[0].Path)), "secret.png")
	for name, args := range map[string][]string{
		"another file":       slices.Insert(slices.Clone(plain), 6, "-i", outside),
		"photos at the end":  slices.Insert(slices.Clone(plain), len(plain)-1, "-i", ims[0].Path),
		"the same twice":     slices.Insert(slices.Clone(plain), 6, "-i", ims[0].Path, "-i", ims[0].Path),
		"refine with photos": slices.Insert(wantJudgeArgs("gpt-6-luna", "low", a.refineSchemaPath()), 6, "-i", ims[0].Path),
	} {
		if err := a.checkSynthArgs(args); err == nil {
			t.Errorf("%s: checkSynthArgs took %q", name, args)
		}
	}
	req.Refine = true
	if _, err := a.Synthesize(context.Background(), req); core.ErrorCode(err) != core.ErrCodeInternal {
		t.Errorf("refine with photos = %v", err)
	}
	if n := len(fakecli.Calls(t)); n != 1 {
		t.Errorf("codex ran %d times", n)
	}
}

func TestEveryCodexModelSeesPhotos(t *testing.T) {
	a, _, _ := judgeAdapter(t)
	var seer core.ImageSeer = a
	if !seer.SeesImages("gpt-6-luna") || !seer.ImageModels().All {
		t.Error("codex should see photos with every model")
	}
}
