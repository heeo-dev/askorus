package core

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

var (
	madePNG  = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 13, 'I', 'H', 'D', 'R', 0, 0, 4, 0, 0, 0, 6, 0}, bytes.Repeat([]byte{1}, 64)...)
	madeJPEG = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 4, 1, 1, 0xFF, 0xC0, 0, 11, 8, 0x02, 0x58, 0x03, 0x20, 1, 1, 0x11, 0, 0xFF, 0xD9}
	madeWebP = []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P', 'V', 'P', '8', 'X', 10, 0, 0, 0, 0, 0, 0, 0, 0xFF, 0x01, 0, 0x7F, 0x01, 0}
)

var codexName = regexp.MustCompile(`^exec-[0-9a-f-]{36}\.png$`)

func writeAt(t *testing.T, p string, data []byte, at time.Time) {
	t.Helper()
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

func execName(n int) string {
	return "exec-0000000" + string(rune('0'+n)) + "-0000-0000-0000-000000000000.png"
}

func TestFindMadeTakesOnlyNewCheckedPicturesOldestFirst(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	writeAt(t, filepath.Join(dir, execName(0)), madePNG, t0) // there before the ask
	before := MadeSnapshot(dir)
	for i := 1; i <= 6; i++ {
		// Written newest first, so the order is the times', not the names'.
		writeAt(t, filepath.Join(dir, execName(i)), madePNG, t0.Add(time.Duration(10-i)*time.Minute))
	}
	writeAt(t, filepath.Join(dir, "notes.png"), madePNG, t0)                                              // not the AI's name
	writeAt(t, filepath.Join(dir, "exec-11111111-0000-0000-0000-000000000000.png"), []byte("<svg/>"), t0) // not a picture
	writeAt(t, filepath.Join(dir, "exec-22222222-0000-0000-0000-000000000000.png"), nil, t0)              // empty
	writeAt(t, filepath.Join(dir, "exec-33333333-0000-0000-0000-000000000000.png"), append(madePNG, make([]byte, MaxMadeBytes)...), t0)
	if err := os.Mkdir(filepath.Join(dir, "exec-44444444-0000-0000-0000-000000000000.png"), 0o700); err != nil {
		t.Fatal(err)
	}

	found, leftOut := FindMade(dir, before, codexName)
	if len(found) != MaxMade || leftOut != 2 {
		t.Fatalf("found %d, left out %d: %+v", len(found), leftOut, found)
	}
	for i, f := range found {
		if want := filepath.Join(dir, execName(6-i)); f.Path != want || f.Type != ImagePNG {
			t.Errorf("found[%d] = %+v, want %s", i, f, want)
		}
	}
}

func TestFindMadeLeavesOutALinkOutOfTheFolder(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	secret := filepath.Join(outside, "secret.png")
	writeAt(t, secret, madePNG, time.Now())
	before := MadeSnapshot(dir)
	link := filepath.Join(dir, execName(1))
	if err := os.Symlink(secret, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("cannot make a symbolic link here:", err)
		}
		t.Fatal(err)
	}
	if found, _ := FindMade(dir, before, codexName); len(found) != 0 {
		t.Errorf("a link was taken: %+v", found)
	}
}

func TestFindMadeOfAFolderNotThereYet(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "thread")
	before := MadeSnapshot(dir) // nil: the conversation has made no picture yet
	if before != nil {
		t.Fatal("a missing folder has a snapshot")
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeAt(t, filepath.Join(dir, execName(1)), madePNG, time.Now())
	if found, _ := FindMade(dir, before, codexName); len(found) != 1 {
		t.Errorf("found = %+v", found)
	}
	if found, _ := FindMade(filepath.Join(dir, "nope"), nil, codexName); found != nil {
		t.Errorf("a missing folder gave %+v", found)
	}
}

func TestImageSize(t *testing.T) {
	for _, c := range []struct {
		name string
		data []byte
		w, h int
	}{
		{"png", madePNG, 1024, 1536},
		{"jpeg", madeJPEG, 800, 600},
		{"webp", madeWebP, 512, 384},
		{"svg", []byte("<svg/>"), 0, 0},
		{"short png", madePNG[:12], 0, 0},
	} {
		if w, h := ImageSize(c.data); w != c.w || h != c.h {
			t.Errorf("%s: %dx%d, want %dx%d", c.name, w, h, c.w, c.h)
		}
	}
}

func TestScrubMadePathsTakesThePlacesOutOfTheAnswer(t *testing.T) {
	dir := `C:\Users\someone\.codex\generated_images\019a`
	files := []MadeFile{{Path: dir + `\exec-1.png`, Type: ImagePNG}, {Path: dir + `\exec-2.png`, Type: ImagePNG}}
	in := "Saved to C:\\Users\\someone\\.codex\\generated_images\\019a\\exec-1.png and c:/users/SOMEONE/.codex/generated_images/019a/exec-2.png, folder " + dir + "."
	got := ScrubMadePaths(in, dir, files)
	if strings.Contains(strings.ToLower(got), "someone") || !strings.Contains(got, "picture-1.png") || !strings.Contains(got, "picture-2.png") || !strings.Contains(got, "folder pictures.") {
		t.Errorf("scrubbed = %q", got)
	}
	if got := ScrubMadePaths("no paths here", dir, files); got != "no paths here" {
		t.Errorf("an answer without paths changed: %q", got)
	}
}

type fakeMaker struct {
	makes   *MakesInfo
	resumes map[string]bool // nil: not a PictureResumer's answer
}

func (f fakeMaker) Makes() *MakesInfo { return f.makes }

type fakeResumer struct{ fakeMaker }

func (f fakeResumer) MakesIn(resume string) bool { return f.resumes[resume] }

func TestPicturePlan(t *testing.T) {
	on := &MakesInfo{MaxCount: MaxMade, Types: MadeTypes}
	limited := &MakesInfo{MaxCount: MaxMade, Types: MadeTypes, LimitedUntil: "2026-11-28T00:00:00Z"}
	for _, c := range []struct {
		name     string
		a        any
		want     bool
		resume   string
		pictures bool
		guide    string
	}{
		{"not a maker", struct{}{}, true, "", false, GuideDraw},
		{"turned off", fakeMaker{makes: on}, false, "", false, GuideDraw},
		{"cannot now", fakeMaker{makes: nil}, true, "", false, GuideDraw},
		{"makes", fakeMaker{makes: on}, true, "", true, GuideMake},
		{"follow-up keeps its tools", fakeMaker{makes: on}, true, "t-1", true, GuideMake},
		{"limit reached", fakeMaker{makes: limited}, true, "", true, GuideDraw},
		{"conversation with the tool", fakeResumer{fakeMaker{makes: on, resumes: map[string]bool{"c-1": true}}}, true, "c-1", true, GuideMake},
		{"conversation without it", fakeResumer{fakeMaker{makes: on, resumes: map[string]bool{"c-1": true}}}, true, "c-2", false, GuideDraw},
		{"new conversation of a resumer", fakeResumer{fakeMaker{makes: on}}, true, "", true, GuideMake},
	} {
		pictures, guide := PicturePlan(c.a, c.want, c.resume)
		if pictures != c.pictures || guide != c.guide {
			t.Errorf("%s: %v %q", c.name, pictures, guide[:20])
		}
	}
}

// R-159 (2호's review of G1-G6, middle): the bytes were read by opening the
// path again after it was checked, so a file swapped for a link meanwhile
// was read from wherever it pointed; a hard link of a file elsewhere passed
// every check. Now the checked handle is what is read, and a file with a
// second name is left out.
func TestFindMadeLeavesOutAHardLinkAndReadsTheCheckedHandle(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	secret := filepath.Join(outside, "secret.png")
	writeAt(t, secret, madePNG, time.Now())
	before := MadeSnapshot(dir)
	if err := os.Link(secret, filepath.Join(dir, execName(1))); err != nil {
		t.Skip("cannot make a hard link here:", err)
	}
	writeAt(t, filepath.Join(dir, execName(2)), madePNG, time.Now())
	found, _ := FindMade(dir, before, codexName)
	if len(found) != 1 || filepath.Base(found[0].Path) != execName(2) || !bytes.Equal(found[0].Data, madePNG) {
		t.Errorf("found = %+v", found)
	}
}

// R-160 (2호's review, middle): one letter whose lower case is longer or
// shorter in bytes (the Kelvin sign) made ScrubMadePaths give the answer
// back as it was, with the whole path in it.
func TestScrubMadePathsWithALetterThatChangesLength(t *testing.T) {
	dir := `C:\Users\someone\.codex\generated_images\019a`
	files := []MadeFile{{Path: dir + `\exec-1.png`, Type: ImagePNG}}
	in := "\u212a: saved to " + dir + `\exec-1.png` + " and C:\\USERS\\SOMEONE\\.CODEX\\GENERATED_IMAGES\\019A."
	got := ScrubMadePaths(in, dir, files)
	if strings.Contains(strings.ToLower(got), "someone") || !strings.Contains(got, "picture-1.png") || !strings.HasPrefix(got, "\u212a: ") {
		t.Errorf("scrubbed = %q", got)
	}
	// A path written with the Kelvin sign for its k is the same path to Windows.
	if got := replaceFold("a \u212aey b", "key", "K"); got != "a K b" {
		t.Errorf("replaceFold = %q", got)
	}
}
