package agy

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// Made pictures (#14, G3): a question that may make pictures runs with a
// draw agent (generate_image), the adapter takes the new pictures at the top
// of the conversation's brain folder, remembers the conversation for its
// follow-ups (R-130), and reads the picture limit from the tool's 429.

// drawAgy is fakeAgy whose brain folder the fake writes in too.
func drawAgy(t *testing.T, pictures string) (*Adapter, string, string) {
	t.Helper()
	exe := fakecli.Use(t, fakecli.RoleAgy)
	brain := t.TempDir()
	t.Setenv(fakecli.EnvAgyBrain, brain)
	t.Setenv(fakecli.EnvPictures, pictures)
	return New(Options{Path: exe, ConfigDir: t.TempDir(), BrainDir: brain}), brain, t.TempDir()
}

func TestAskWithPicturesRunsTheDrawAgent(t *testing.T) {
	a, brain, folder := drawAgy(t, "2")
	r, err := ask(t, a, core.AskRequest{Folder: folder, Pictures: true})
	if err != nil {
		t.Fatal(err)
	}
	c := fakecli.Calls(t)[0]
	if !slices.Equal(c.Args, runArgs(drawAgent, askSettings, "gemini-3.8-flash-medium", "")) || c.Dir != filepath.Join(folder, FolderName) {
		t.Fatalf("call = %q in %q", c.Args, c.Dir)
	}
	if b, err := os.ReadFile(filepath.Join(c.Dir, ".agents", "agents", drawAgent+".md")); err != nil || string(b) != drawAgentFile {
		t.Errorf("draw agent file = %q %v", b, err)
	}
	if !strings.Contains(drawAgentFile, "tools: [search_web, generate_image]\n") || !strings.Contains(photoDrawAgentFile, "tools: [search_web, view_file, generate_image]\n") {
		t.Error("the draw agents' tools changed")
	}
	dir := filepath.Join(brain, r.SessionID)
	if len(r.Made) != 2 || r.MadeLeftOut != 0 || r.PictureLimit != nil {
		t.Fatalf("result = %+v", r)
	}
	for _, f := range r.Made {
		if filepath.Dir(f.Path) != dir || f.Type != core.ImageJPEG {
			t.Errorf("picture = %+v", f)
		}
		if b, _ := os.ReadFile(f.Path); !bytes.Equal(b, fakecli.FakeJPEG) {
			t.Errorf("%s is not what agy made", f.Path)
		}
	}
	if strings.Contains(r.Answer, brain) || !strings.HasSuffix(r.Answer, "[saved: picture-1.jpg]") {
		t.Errorf("answer = %q", r.Answer)
	}
	if !a.MakesIn(r.SessionID) {
		t.Error("the draw conversation is not remembered")
	}

	// Its follow-up runs with the draw agent and takes only the new picture.
	t.Setenv(fakecli.EnvPictures, "1")
	r2, err := ask(t, a, core.AskRequest{Folder: folder, Resume: r.SessionID, Pictures: true})
	if err != nil {
		t.Fatal(err)
	}
	if c := fakecli.Calls(t)[1]; !slices.Equal(c.Args, runArgs(drawAgent, askSettings, "gemini-3.8-flash-medium", r.SessionID)) {
		t.Errorf("follow-up args = %q", c.Args)
	}
	if len(r2.Made) != 1 || slices.ContainsFunc(r.Made, func(f core.MadeFile) bool { return f.Path == r2.Made[0].Path }) {
		t.Errorf("follow-up made = %+v", r2.Made)
	}
	// With pictures off it still runs with the draw agent (the conversation
	// has the tool) but takes nothing.
	r3, err := ask(t, a, core.AskRequest{Folder: folder, Resume: r.SessionID})
	if err != nil || len(r3.Made) != 0 {
		t.Errorf("pictures off = %+v %v", r3, err)
	}
	if c := fakecli.Calls(t)[2]; c.Args[1] != drawAgent {
		t.Errorf("pictures off ran %q", c.Args[1])
	}
}

func TestAConversationWithoutTheToolStaysAnAsk(t *testing.T) {
	a, brain, folder := drawAgy(t, "1")
	r, err := ask(t, a, core.AskRequest{Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	if c := fakecli.Calls(t)[0]; c.Args[1] != askAgent || len(r.Made) != 0 || a.MakesIn(r.SessionID) {
		t.Fatalf("ask = %q, %+v", c.Args[1], r)
	}
	// Pictures asked for in its follow-up: the conversation has no tool, so
	// it runs as an ask and nothing is looked for.
	r2, err := ask(t, a, core.AskRequest{Folder: folder, Resume: r.SessionID, Pictures: true})
	if err != nil || len(r2.Made) != 0 {
		t.Fatalf("follow-up = %+v %v", r2, err)
	}
	if c := fakecli.Calls(t)[1]; c.Args[1] != askAgent {
		t.Errorf("follow-up ran %q", c.Args[1])
	}
	if entries, _ := os.ReadDir(brain); len(entries) != 0 {
		t.Errorf("%d brain folders: a picture was made without the tool", len(entries))
	}
	// A one-off ask never gets the tool.
	if _, err := ask(t, a, core.AskRequest{Folder: folder, OneOff: true, Pictures: true}); core.ErrorCode(err) != core.ErrCodeInternal {
		t.Errorf("one-off with pictures = %v", err)
	}
}

func TestAskWithPhotosAndPicturesRunsThePhotoDrawAgent(t *testing.T) {
	a, _, folder := drawAgy(t, "1")
	r, err := ask(t, a, core.AskRequest{Folder: folder, Images: keptPhotos(t), Pictures: true})
	if err != nil {
		t.Fatal(err)
	}
	c := fakecli.Calls(t)[0]
	if !slices.Equal(c.Args, runArgs(photoDrawAgent, photoSettings, "gemini-3.8-flash-medium", "")) {
		t.Errorf("args = %q", c.Args)
	}
	if len(r.Made) != 1 || !strings.Contains(r.Answer, "[photos: 2]") {
		t.Errorf("result = %+v", r)
	}
}

func TestThePictureLimitIsReadAndKeptUntilItRefreshes(t *testing.T) {
	a, _, folder := drawAgy(t, fakecli.PicturesLimit)
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	if m := a.Makes(); m == nil || m.LimitedUntil != "" {
		t.Fatalf("before = %+v", m)
	}
	r, err := ask(t, a, core.AskRequest{Folder: folder, Pictures: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.PictureLimit == nil || r.PictureLimit.Until != fakecli.AgyPictureReset || len(r.Made) != 0 {
		t.Fatalf("result = %+v", r)
	}
	if m := a.Makes(); m == nil || m.LimitedUntil != fakecli.AgyPictureReset {
		t.Errorf("makes = %+v", m)
	}
	now = now.Add(4 * 24 * time.Hour) // past 11-24 16:26
	if m := a.Makes(); m == nil || m.LimitedUntil != "" {
		t.Errorf("after the refresh = %+v", m)
	}
}

func TestPictureLimitOf(t *testing.T) {
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name    string
		msg     string
		limited bool
		until   string
	}{
		{"agy's 429", fakecli.AgyPictureLimitError, true, fakecli.AgyPictureReset},
		{"no time", "429 Too Many Requests", true, ""},
		{"a past time", `429 "quotaResetTimeStamp": "2026-11-01T00:00:00Z"`, true, ""},
		{"too far", `429 "quotaResetTimeStamp": "2027-11-01T00:00:00Z"`, true, ""},
		{"another failure", "image failed to generate: blocked by safety filters", false, ""},
	} {
		limited, until := pictureLimitOf(c.msg, now)
		got := ""
		if !until.IsZero() {
			got = until.UTC().Format(time.RFC3339)
		}
		if limited != c.limited || got != c.until {
			t.Errorf("%s: %v %q", c.name, limited, got)
		}
	}
}

func TestMadeNames(t *testing.T) {
	for name, ok := range map[string]bool{
		"icons_cat_icon_probe_1795218883827.jpg": true,
		"고양이_1795218883827.jpg":                  true,
		"dog_icon_probe_1795218925025.png":       true,
		".hidden_1795218925025.jpg":              false,
		"notes.jpg":                              false,
		"cat_1795218883827.jpg.exe":              false,
		"cat_1795218883827.txt":                  false,
	} {
		if madeNameRe.MatchString(name) != ok {
			t.Errorf("%q: %v", name, !ok)
		}
	}
}

func TestMakesAndCheckArgs(t *testing.T) {
	a, _, _ := drawAgy(t, "0")
	if m := a.Makes(); m == nil || m.MaxCount != core.MaxMade {
		t.Errorf("makes = %+v", m)
	}
	a.loginMu.Lock()
	a.login = loginState{known: true}
	a.loginMu.Unlock()
	if a.Makes() != nil {
		t.Error("a signed-out agy makes pictures")
	}
	if New(Options{Path: filepath.Join(t.TempDir(), "agy.exe"), BrainDir: t.TempDir()}).Makes() != nil {
		t.Error("an agy that is not there makes pictures")
	}
	if New(Options{Path: filepath.Join(t.TempDir(), "agy.exe"), BrainDir: "relative"}).brainDir != "" {
		t.Error("a relative brain folder was taken")
	}
	for _, args := range [][]string{
		runArgs(drawAgent, askSettings, "gemini-3.8-flash-medium", ""),
		runArgs(photoDrawAgent, photoSettings, "gemini-3.8-flash-medium", ""),
	} {
		if err := checkArgs(args); err != nil {
			t.Errorf("checkArgs(%q) = %v", args, err)
		}
	}
	// A draw agent with the photo settings (reads allowed) is not one of them.
	if err := checkArgs(runArgs(drawAgent, photoSettings, "gemini-3.8-flash-medium", "")); err == nil {
		t.Error("the draw agent with reads allowed was taken")
	}
}

// R-161 (2호's review, middle): the same for agy, whose stripFileLinks
// takes out links to files but not a plain path.
func TestAgyTakesThePlaceOutWithPicturesOff(t *testing.T) {
	a, brain, folder := drawAgy(t, "0")
	saved := filepath.Join(brain, "0f6c6a2e-1111-4222-8333-444455556666", "cat_1795218883827.jpg")
	fakecli.SetAnswers(t, map[string]string{fakecli.RoleAgy: "Saved as " + saved + "."})
	r, err := ask(t, a, core.AskRequest{Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.Answer, brain) || !strings.Contains(r.Answer, "pictures") {
		t.Errorf("answer = %q", r.Answer)
	}
}

// R-166: the same for agy, whose empty answer was ask_empty before its
// pictures were looked for.
func TestAgyAnswerOfPicturesOnly(t *testing.T) {
	a, _, folder := drawAgy(t, "1")
	t.Setenv(fakecli.EnvAsk, fakecli.AskEmpty)
	r, err := ask(t, a, core.AskRequest{Folder: folder, Pictures: true})
	if err != nil || r.Answer != "" || len(r.Made) != 1 || !a.MakesIn(r.SessionID) {
		t.Fatalf("pictures only = %+v, %v", r, err)
	}
	// Without pictures an empty answer is still ask_empty.
	if _, err := ask(t, a, core.AskRequest{Folder: folder}); core.ErrorCode(err) != core.ErrCodeAskEmpty {
		t.Errorf("empty without pictures = %v", err)
	}
}
