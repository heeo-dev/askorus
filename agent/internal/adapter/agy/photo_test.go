package agy

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

var (
	testJPEG = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{9}, 300)...)
	testPNG  = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{4}, 300)...)
)

// keptPhotos writes two photos as internal/uploads keeps them and returns them.
func keptPhotos(t *testing.T) []core.Image {
	t.Helper()
	dir := t.TempDir()
	ims := []core.Image{
		{Path: filepath.Join(dir, "up_0123456789abcdef0123456789abcdef.jpg"), Type: core.ImageJPEG},
		{Path: filepath.Join(dir, "up_fedcba9876543210fedcba9876543210.png"), Type: core.ImagePNG},
	}
	for i, data := range [][]byte{testJPEG, testPNG} {
		if err := os.WriteFile(ims[i].Path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return ims
}

func TestPhotoSettings(t *testing.T) {
	if strings.Contains(photoSettings, "read_file") {
		t.Errorf("photoSettings deny reads: %s", photoSettings)
	}
	for _, d := range []string{"command(*)", "unsandboxed(*)", "write_file(*)", "execute_url(*)", "download_url(*)", "mcp(*)"} {
		if !strings.Contains(photoSettings, `"`+d+`"`) {
			t.Errorf("photoSettings lack %s", d)
		}
	}
	if !strings.Contains(photoAgentFile, "\ntools: [search_web, view_file]\n") || !strings.HasPrefix(photoAgentFile, "---\nname: askorus-photo\n") {
		t.Errorf("photo agent file = %q", photoAgentFile)
	}
	ok := runArgs(photoAgent, photoSettings, "gemini-3.8-flash-low", "")
	if err := checkArgs(ok); err != nil {
		t.Errorf("photo args refused: %v", err)
	}
	for name, args := range map[string][]string{
		"photo agent, ask settings": runArgs(photoAgent, askSettings, "gemini-3.8-flash-low", ""),
		"ask agent, photo settings": runArgs(askAgent, photoSettings, "gemini-3.8-flash-low", ""),
		"synth agent":               runArgs(synthAgent, askSettings, "gemini-3.8-flash-low", ""),
	} {
		if err := checkArgs(args); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	a := &Adapter{}
	if !a.SeesImages("gemini-3.8-flash") || a.SeesImages("claude-sonnet-4-6") || !a.ImageModels().All {
		t.Error("image models")
	}
}

func TestPhotoFolder(t *testing.T) {
	base := t.TempDir()
	ims := keptPhotos(t)
	dir, names, cleanup, err := photoFolder(base, photoAgent, photoAgentFile, ims)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dir) != filepath.Join(base, PhotoFolderName) || !slices.Equal(names, []string{"photo-1.jpg", "photo-2.png"}) {
		t.Errorf("dir %q names %q", dir, names)
	}
	var files []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	slices.Sort(files)
	if want := []string{".agents/agents/askorus-photo.md", "photo-1.jpg", "photo-2.png"}; !slices.Equal(files, want) {
		t.Errorf("files = %q", files)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "photo-1.jpg")); !bytes.Equal(b, testJPEG) {
		t.Error("photo-1 is not the kept photo")
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the folder outlived its run: %v", err)
	}
	// Two runs never share a folder.
	d1, _, c1, _ := photoFolder(base, photoAgent, photoAgentFile, ims)
	d2, _, c2, _ := photoFolder(base, photoAgent, photoAgentFile, ims)
	if d1 == d2 {
		t.Error("two runs got the same folder")
	}
	c1()
	c2()

	for name, bad := range map[string][]core.Image{
		"phone name": {{Path: filepath.Join(base, "IMG_0001.jpg"), Type: core.ImageJPEG}},
		"wrong type": {{Path: ims[0].Path, Type: core.ImagePNG}},
		"missing":    {{Path: filepath.Join(base, "up_00000000000000000000000000000000.jpg"), Type: core.ImageJPEG}},
		"none":       nil,
		"same twice": {ims[0], ims[0]},
	} {
		if _, _, _, err := photoFolder(base, photoAgent, photoAgentFile, bad); core.ErrorCode(err) != core.ErrCodeInternal {
			t.Errorf("%s: %v", name, err)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(base, PhotoFolderName)); len(entries) != 0 {
		t.Errorf("%d folders left after refused photos", len(entries))
	}
}

func TestAskPhotos(t *testing.T) {
	a, folder := fakeAgy(t)
	ims := keptPhotos(t)
	r, err := ask(t, a, core.AskRequest{Folder: folder, Images: ims})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(r.Answer, " [photos: 2]") {
		t.Errorf("answer = %q", r.Answer)
	}
	c := fakecli.Calls(t)[0]
	if filepath.Dir(c.Dir) != filepath.Join(folder, PhotoFolderName) {
		t.Errorf("dir = %q", c.Dir)
	}
	if !slices.Equal(c.Args, runArgs(photoAgent, photoSettings, "gemini-3.8-flash-medium", "")) {
		t.Errorf("args = %q", c.Args)
	}
	var m struct {
		Message struct {
			Content []struct{ Text string } `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(c.Stdin), &m); err != nil || !strings.HasPrefix(m.Message.Content[0].Text, "[Photos attached to this question, in the current folder: photo-1.jpg, photo-2.png]\n\n") {
		t.Errorf("stdin = %q %v", c.Stdin, err)
	}
	for _, arg := range c.Args {
		if strings.Contains(arg, "up_") || strings.Contains(arg, "photo-") {
			t.Errorf("a photo is in an argument: %q", arg)
		}
	}
	if _, err := os.Stat(c.Dir); !os.IsNotExist(err) {
		t.Errorf("the photo folder outlived the ask: %v", err)
	}
	// A follow-up with photos carries the conversation on in a new folder.
	r2, err := ask(t, a, core.AskRequest{Folder: folder, Images: ims[:1], Resume: r.SessionID})
	if err != nil || r2.SessionID != r.SessionID || !strings.HasSuffix(r2.Answer, " [photos: 1]") {
		t.Errorf("follow-up = %+v %v", r2, err)
	}
	// A read outside the folder ends the run without an answer.
	t.Setenv(fakecli.EnvAgy, fakecli.AgyDenied)
	if _, err := ask(t, a, core.AskRequest{Folder: folder, Images: ims}); core.ErrorCode(err) != core.ErrCodeAskFailed {
		t.Errorf("denied = %v", err)
	}
	if _, err := ask(t, a, core.AskRequest{Folder: folder, Images: ims, Model: "claude-sonnet-4-6"}); core.ErrorCode(err) != core.ErrCodeBadModel {
		t.Errorf("claude model = %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(folder, PhotoFolderName)); len(entries) != 0 {
		t.Errorf("%d photo folders left", len(entries))
	}
}

func TestAskPhotosContext(t *testing.T) {
	a, folder := fakeAgy(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Ask(ctx, core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierQuick, Images: keptPhotos(t)}); err == nil {
		t.Error("a cancelled ask answered")
	}
	if entries, _ := os.ReadDir(filepath.Join(folder, PhotoFolderName)); len(entries) != 0 {
		t.Errorf("%d photo folders left after a cancelled ask", len(entries))
	}
}
