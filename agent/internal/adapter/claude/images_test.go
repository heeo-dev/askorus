package claude

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// Photos with a question (#14): claude gets them on stdin, in one
// stream-json message with the prompt. No path reaches the command line.

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

func TestAskSendsPhotosOnStdin(t *testing.T) {
	a, folder := newJudgeAdapter(t)
	ims := keptPhotos(t)
	res, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "이 사진은 뭐야? @" + ims[0].Path, Tier: core.TierQuick, Images: ims})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(res.Answer, " [photos: 2]") || res.SessionID == "" {
		t.Errorf("answer = %+v", res)
	}
	cs := fakecli.Calls(t)
	if len(cs) != 1 {
		t.Fatalf("%d calls", len(cs))
	}
	want := []string{"-p", "--output-format", "stream-json", "--model", "haiku", "--effort", "medium",
		"--tools", "WebSearch,WebFetch", "--allowedTools", "WebSearch,WebFetch", "--strict-mcp-config",
		"--settings", `{"permissions":{"deny":["Read"]}}`, "--input-format", "stream-json", "--verbose"}
	if !slices.Equal(cs[0].Args, want) {
		t.Errorf("args = %q\nwant  %q", cs[0].Args, want)
	}
	// stdin is one JSON line: the prompt with its "@" made plain, then the
	// photos as base64. The only path in it is the one the question wrote.
	var m imageMessage
	if err := json.Unmarshal([]byte(cs[0].Stdin), &m); err != nil || strings.Count(cs[0].Stdin, "\n") != 1 {
		t.Fatalf("stdin is not one JSON line: %v", err)
	}
	c := m.Message.Content
	if m.Type != "user" || m.Message.Role != "user" || len(c) != 3 || c[0].Type != "text" || c[0].Text != plainAt("이 사진은 뭐야? @"+ims[0].Path) {
		t.Fatalf("message = %+v", m)
	}
	for i, data := range [][]byte{photoJPEG, photoPNG} {
		b := c[i+1]
		if b.Type != "image" || b.Source == nil || b.Source.Type != "base64" || b.Source.MediaType != ims[i].Type ||
			b.Source.Data != base64.StdEncoding.EncodeToString(data) {
			t.Errorf("block %d = %+v", i+1, b)
		}
	}
	if strings.Contains(cs[0].Stdin, filepath.Base(ims[1].Path)) || strings.Contains(strings.Join(cs[0].Args, " "), "up_") {
		t.Error("a photo's file name went to claude")
	}
}

func TestAskWithPhotosCarriesOnAndOneOff(t *testing.T) {
	a, folder := newJudgeAdapter(t)
	ims := keptPhotos(t)
	const sid = "12345678-1234-4234-8234-123456789abc"
	if _, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierStandard, Resume: sid, Images: ims[:1]}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierStandard, OneOff: true, Images: ims[:1]}); err != nil {
		t.Fatal(err)
	}
	cs := fakecli.Calls(t)
	tail := []string{"--input-format", "stream-json", "--verbose"}
	if len(cs) != 2 || !slices.Equal(cs[0].Args[len(cs[0].Args)-5:], append([]string{"--resume", sid}, tail...)) ||
		!slices.Equal(cs[1].Args[len(cs[1].Args)-4:], append([]string{"--no-session-persistence"}, tail...)) {
		t.Fatalf("calls = %+v", cs)
	}
	// Without photos the command is the one it always was.
	if _, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierStandard}); err != nil {
		t.Fatal(err)
	}
	last := fakecli.Calls(t)[2]
	if !slices.Equal(last.Args, askArgs("sonnet", "medium", "", false)) || last.Stdin != "q" || slices.Contains(last.Args, "--verbose") {
		t.Errorf("plain ask = %q, stdin %q", last.Args, last.Stdin)
	}
}

func TestAskRefusesPhotosItCannotUse(t *testing.T) {
	a, folder := newJudgeAdapter(t)
	ims := keptPhotos(t)
	gone := []core.Image{{Path: filepath.Join(filepath.Dir(ims[0].Path), "up_"+strings.Repeat("c", 32)+".jpg"), Type: core.ImageJPEG}}
	outside := filepath.Join(t.TempDir(), "notes.jpg")
	os.WriteFile(outside, photoJPEG, 0o600)
	for name, bad := range map[string][]core.Image{
		"a file that is gone":    gone,
		"not a kept photo":       {{Path: outside, Type: core.ImageJPEG}},
		"a relative path":        {{Path: filepath.Base(ims[0].Path), Type: core.ImageJPEG}},
		"the wrong type":         {{Path: ims[0].Path, Type: core.ImagePNG}},
		"a type that is no type": {{Path: ims[0].Path, Type: "application/pdf"}},
		"the same photo twice":   {ims[0], ims[0]},
		"five photos":            {ims[0], ims[1], ims[0], ims[1], ims[0]},
	} {
		_, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierQuick, Images: bad})
		var ae *core.ActionError
		if !errors.As(err, &ae) || ae.Code != core.ErrCodeAskFailed || strings.Contains(ae.Detail, filepath.Dir(ims[0].Path)) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if n := len(fakecli.Calls(t)); n != 0 {
		t.Errorf("claude ran %d times", n)
	}
}

func TestCheckAskArgsStreamFormsGoTogether(t *testing.T) {
	good := askImageArgs("sonnet", "medium", "", false)
	if err := checkAskArgs(good); err != nil {
		t.Fatalf("checkAskArgs(%q) = %v", good, err)
	}
	if err := checkAskArgs(askImageArgs("sonnet", "", "12345678-1234-4234-8234-123456789abc", false)); err != nil {
		t.Errorf("a follow-up with photos: %v", err)
	}
	plain := askArgs("sonnet", "medium", "", false)
	bad := map[string][]string{
		"stream output alone":    slices.Replace(slices.Clone(plain), 2, 3, "stream-json"),
		"verbose alone":          append(slices.Clone(plain), "--verbose"),
		"stream input alone":     append(slices.Clone(plain), "--input-format", "stream-json"),
		"no verbose":             good[:len(good)-1],
		"text input":             append(slices.Clone(good[:len(good)-2]), "text", "--verbose"),
		"a file argument":        append(slices.Clone(good), "--file", "x:y.png"),
		"an image path argument": append(slices.Clone(good), "C:\\photo.png"),
	}
	for name, args := range bad {
		if err := checkAskArgs(args); !errors.Is(err, ErrArgRejected) {
			t.Errorf("%s: checkAskArgs(%q) = %v, want rejected", name, args, err)
		}
	}
}

func TestSynthesizeSendsPhotosOnStdin(t *testing.T) {
	a, folder := newJudgeAdapter(t)
	ims := keptPhotos(t)
	req, _ := synthRequest(t, folder)
	req.Images = ims
	res, err := a.Synthesize(context.Background(), req)
	if err != nil || len(res.Output) == 0 {
		t.Fatalf("Synthesize = %+v, %v", res, err)
	}
	cs := fakecli.Calls(t)
	want := append(wantSynthArgs("sonnet", "medium"), "--input-format", "stream-json", "--verbose")
	want[2] = "stream-json"
	if len(cs) != 1 || !slices.Equal(cs[0].Args, want) || cs[0].Photos != 2 {
		t.Fatalf("call = %+v\nwant args %q", cs, want)
	}
	var m imageMessage
	if err := json.Unmarshal([]byte(cs[0].Stdin), &m); err != nil || m.Message.Content[0].Text != plainAt(req.Prompt) {
		t.Errorf("the prompt is not the message's text: %v", err)
	}
	if err := checkSynthArgs(want); err != nil {
		t.Errorf("checkSynthArgs: %v", err)
	}
	// Still not an ask, and nothing but the three stream arguments more.
	if err := checkAskArgs(want); !errors.Is(err, ErrArgRejected) {
		t.Errorf("checkAskArgs took the writer's command: %v", err)
	}
	for name, args := range map[string][]string{
		"stream input alone": append(wantSynthArgs("sonnet", "medium"), "--input-format", "stream-json", "--verbose"),
		"another tail":       append(slices.Clone(want[:16]), "--input-format", "stream-json", "--resume"),
		"refine with photos": func() []string {
			r := append(refineArgs("haiku", "low"), "--input-format", "stream-json", "--verbose")
			r[2] = "stream-json"
			return r
		}(),
	} {
		if err := checkSynthArgs(args); !errors.Is(err, ErrArgRejected) {
			t.Errorf("%s: checkSynthArgs = %v, want rejected", name, err)
		}
	}
	// A question spec takes no photos.
	req.Refine = true
	if _, err := a.Synthesize(context.Background(), req); core.ErrorCode(err) != core.ErrCodeInternal {
		t.Errorf("refine with photos = %v", err)
	}
	if n := len(fakecli.Calls(t)); n != 1 {
		t.Errorf("claude ran %d times", n)
	}
}

func TestEveryClaudeModelSeesPhotos(t *testing.T) {
	a, _ := newJudgeAdapter(t)
	var seer core.ImageSeer = a
	if !seer.SeesImages("haiku") || !seer.SeesImages("anything") || !seer.ImageModels().All {
		t.Error("claude should see photos with every model")
	}
}
