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

// Made pictures (#14, G2): a question's own ask with pictures leaves the
// one feature image_generation on; the adapter takes the new pictures in
// the thread's folder and takes the folder's place out of the answer.

func wantDisablesWithPictures() []string {
	var out []string
	for _, f := range wantOff {
		if f != "image_generation" {
			out = append(out, "--disable", f)
		}
	}
	return out
}

// pictureHome makes a codex home for the test and has the fake make n
// pictures in an ask with the tool.
func pictureHome(t *testing.T, n string) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), ".codex")
	t.Setenv("CODEX_HOME", home)
	t.Setenv(fakecli.EnvPictures, n)
	return home
}

func TestAskWithPicturesLeavesOnlyTheImageToolOn(t *testing.T) {
	home := pictureHome(t, "2")
	a, _, folder := judgeAdapter(t)
	res, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "고양이 사진", Tier: core.TierQuick, Pictures: true})
	if err != nil {
		t.Fatal(err)
	}
	cs := fakecli.Calls(t)
	want := slices.Concat([]string{"--search", "exec", "-s", "read-only", "--ignore-user-config", "--skip-git-repo-check",
		"--json", "-m", "gpt-6-luna", "-c", `model_reasoning_effort="medium"`}, wantDisablesWithPictures(), []string{"-"})
	if len(cs) != 1 || !slices.Equal(cs[0].Args, want) || cs[0].Stdin != "고양이 사진" {
		t.Fatalf("call = %+v\nwant args %q", cs, want)
	}
	dir := filepath.Join(home, "generated_images", res.SessionID)
	if len(res.Made) != 2 || res.MadeLeftOut != 0 {
		t.Fatalf("made = %+v (%d left out)", res.Made, res.MadeLeftOut)
	}
	for _, f := range res.Made {
		if filepath.Dir(f.Path) != dir || f.Type != core.ImagePNG || !madeNameRe.MatchString(filepath.Base(f.Path)) {
			t.Errorf("picture = %+v", f)
		}
		if b, _ := os.ReadFile(f.Path); !bytes.Equal(b, fakecli.FakePNG) {
			t.Errorf("%s is not the picture codex made", f.Path)
		}
	}
	if !strings.HasSuffix(res.Answer, "[saved: picture-1.png]") || strings.Contains(res.Answer, home) {
		t.Errorf("answer = %q", res.Answer)
	}
}

func TestAFollowUpTakesOnlyTheNewPictures(t *testing.T) {
	home := pictureHome(t, "1")
	a, _, folder := judgeAdapter(t)
	dir := filepath.Join(home, "generated_images", testThread)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "exec-00000000-0000-4000-8000-000000000000.png")
	os.WriteFile(old, fakecli.FakePNG, 0o600)
	res, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "사과만 초록색으로", Tier: core.TierQuick, Resume: testThread, Pictures: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Made) != 1 || res.Made[0].Path == old || filepath.Dir(res.Made[0].Path) != dir {
		t.Fatalf("made = %+v", res.Made)
	}
	if _, err := os.Stat(old); err != nil {
		t.Error("the earlier picture was touched")
	}
	cs := fakecli.Calls(t)
	if err := checkArgs(cs[0].Args); err != nil || slices.Contains(cs[0].Args, "image_generation") || cs[0].Args[len(cs[0].Args)-2] != testThread {
		t.Errorf("follow-up args = %q (%v)", cs[0].Args, err)
	}
}

func TestWithoutPicturesTheToolStaysOffAndNothingIsTaken(t *testing.T) {
	home := pictureHome(t, "2")
	a, _, folder := judgeAdapter(t)
	res, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "고양이 사진", Tier: core.TierQuick})
	if err != nil {
		t.Fatal(err)
	}
	cs := fakecli.Calls(t)
	if !slices.Equal(cs[0].Args, askArgs("gpt-6-luna", "medium", "", false)) || len(res.Made) != 0 {
		t.Errorf("args = %q, made %+v", cs[0].Args, res.Made)
	}
	if _, err := os.Stat(filepath.Join(home, "generated_images")); err == nil {
		t.Error("a picture was made without the tool")
	}
	// A one-off ask never gets the tool: refused before codex runs.
	if _, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierQuick, OneOff: true, Pictures: true}); core.ErrorCode(err) != core.ErrCodeInternal {
		t.Errorf("one-off with pictures = %v", err)
	}
	if n := len(fakecli.Calls(t)); n != 1 {
		t.Errorf("codex ran %d times", n)
	}
}

func TestCheckArgsTakesTheTwoFeatureListsOnly(t *testing.T) {
	on := askImageArgs("gpt-6-luna", "medium", "", false, nil, true)
	if err := checkArgs(on); err != nil {
		t.Fatalf("pictures on: %v", err)
	}
	if err := checkArgs(askImageArgs("gpt-6-luna", "medium", testThread, false, nil, true)); err != nil {
		t.Fatalf("follow-up with pictures: %v", err)
	}
	drop := func(args []string, feature string) []string {
		i := slices.Index(args, feature)
		return slices.Delete(slices.Clone(args), i-1, i+1)
	}
	off := askArgs("gpt-6-luna", "medium", "", false)
	bad := map[string][]string{
		"another feature left on": drop(off, "view_image"),
		"two features left on":    drop(on, "view_image"),
		"a one-off with pictures": slices.Insert(slices.Clone(on), 6, "--ephemeral"),
		"enable instead":          slices.Insert(drop(off, "image_generation"), len(off)-3, "--enable", "image_generation"),
		"image tool at the end":   append(slices.Clone(on[:len(on)-1]), "--disable", "image_generation", "-"),
		"the shell left on":       drop(on, "shell_tool"),
	}
	for name, args := range bad {
		if err := checkArgs(args); err == nil {
			t.Errorf("%s: checkArgs(%q) took it", name, args)
		}
	}
}

func TestMakesWhileCodexIsThereAndSignedIn(t *testing.T) {
	pictureHome(t, "0")
	a, _, _ := judgeAdapter(t)
	a.Info(context.Background())
	if m := a.Makes(); m == nil || m.MaxCount != core.MaxMade || len(m.Types) != len(core.MadeTypes) {
		t.Errorf("Makes = %+v", m)
	}
	a.loginMu.Lock()
	a.login = loginState{known: true}
	a.loginMu.Unlock()
	if m := a.Makes(); m != nil {
		t.Errorf("a signed-out codex makes pictures: %+v", m)
	}
	missing := New(Options{Path: filepath.Join(t.TempDir(), "codex.exe")})
	if m := missing.Makes(); m != nil {
		t.Errorf("a codex that is not there makes pictures: %+v", m)
	}
	rel := New(Options{Path: filepath.Join(t.TempDir(), "codex.exe"), Home: "relative"})
	if rel.pictureRoot() != "" {
		t.Error("a relative home was taken")
	}
}

// R-161 (2호's review of G1-G6, middle): with pictures turned off for a
// conversation that made pictures, a follow-up asked where they were saved
// brought the whole path, with the user folder in it: the place was taken
// out only when the ask collected pictures.
func TestAnAskWithPicturesOffStillTakesThePlaceOut(t *testing.T) {
	home := pictureHome(t, "0")
	a, _, folder := judgeAdapter(t)
	saved := filepath.Join(home, "generated_images", testThread, "exec-00000000-0000-4000-8000-000000000000.png")
	fakecli.SetAnswers(t, map[string]string{fakecli.RoleCodex: "It is at " + saved + " (folder " + filepath.Join(home, "generated_images") + ")."})
	res, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "어디에 저장했어?", Tier: core.TierQuick, Resume: testThread})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Answer, home) || !strings.Contains(res.Answer, "pictures") || len(res.Made) != 0 {
		t.Errorf("answer = %q, made %+v", res.Answer, res.Made)
	}
}
