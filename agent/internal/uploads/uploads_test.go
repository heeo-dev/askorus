package uploads

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

var (
	jpeg = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{1}, 60)...)
	png  = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{2}, 60)...)
	webp = append([]byte("RIFF\x00\x00\x00\x00WEBP"), bytes.Repeat([]byte{3}, 60)...)
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "uploads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestPutKeepsAPhotoUnderItsOwnName(t *testing.T) {
	s := open(t)
	for typ, data := range map[string][]byte{core.ImageJPEG: jpeg, core.ImagePNG: png, core.ImageWebP: webp} {
		up, err := s.Put("phone", bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		if up.Type != typ || up.Bytes != int64(len(data)) || !core.ValidImageID(up.ID) {
			t.Fatalf("%s: got %+v", typ, up)
		}
		ims, release, err := s.Hold("phone", []string{up.ID})
		if err != nil {
			t.Fatalf("%s: hold: %v", typ, err)
		}
		release()
		if err := core.CheckImage(ims[0]); err != nil {
			t.Fatalf("%s: the kept photo does not pass CheckImage: %v", typ, err)
		}
		if filepath.Dir(ims[0].Path) != s.Dir() || ims[0].Type != typ {
			t.Fatalf("%s: kept as %+v", typ, ims[0])
		}
		got, err := os.ReadFile(ims[0].Path)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("%s: the file does not hold the photo (%v)", typ, err)
		}
	}
}

// R-129: the folder went under a running agent (a test run of uninstall
// removed it): the next photo makes it again instead of failing.
func TestPutMakesAGoneFolderAgain(t *testing.T) {
	s := open(t)
	if err := os.Remove(s.Dir()); err != nil {
		t.Fatal(err)
	}
	up, err := s.Put("phone", bytes.NewReader(jpeg))
	if err != nil {
		t.Fatalf("put after the folder went: %v", err)
	}
	if info, err := os.Stat(s.Dir()); err != nil || !info.IsDir() {
		t.Fatalf("the folder is not back: %v", err)
	}
	if _, release, err := s.Hold("phone", []string{up.ID}); err != nil {
		t.Errorf("hold: %v", err)
	} else {
		release()
	}
}

func TestPutRefusesWhatIsNoPhoto(t *testing.T) {
	s := open(t)
	for name, data := range map[string][]byte{
		"empty":      {},
		"text":       []byte("#!/bin/sh\necho hello\n"),
		"pdf":        []byte("%PDF-1.4\n1 0 obj\n"),
		"exe":        append([]byte("MZ"), bytes.Repeat([]byte{0}, 80)...),
		"svg":        []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`),
		"gif":        []byte("GIF89a" + strings.Repeat("\x00", 40)),
		"riff other": []byte("RIFF\x00\x00\x00\x00WAVEfmt "),
	} {
		if _, err := s.Put("phone", bytes.NewReader(data)); !errors.Is(err, ErrBadType) {
			t.Fatalf("%s: got %v, want ErrBadType", name, err)
		}
	}
	if n := s.Count(); n != 0 {
		t.Fatalf("%d photos kept", n)
	}
	if left, _ := os.ReadDir(s.Dir()); len(left) != 0 {
		t.Fatalf("%d files left in the folder", len(left))
	}
}

func TestPutRefusesALargePhoto(t *testing.T) {
	s := open(t)
	big := append([]byte{0xFF, 0xD8, 0xFF}, make([]byte, core.MaxImageBytes)...)
	if _, err := s.Put("phone", bytes.NewReader(big)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v, want ErrTooLarge", err)
	}
	fits := append([]byte{0xFF, 0xD8, 0xFF}, make([]byte, core.MaxImageBytes-3)...)
	if _, err := s.Put("phone", bytes.NewReader(fits)); err != nil {
		t.Fatalf("a photo of exactly the limit: %v", err)
	}
}

func TestEachDeviceHasItsOwnLimit(t *testing.T) {
	s := open(t)
	for i := 0; i < MaxPerOwner; i++ {
		if _, err := s.Put("phone", bytes.NewReader(jpeg)); err != nil {
			t.Fatalf("photo %d: %v", i, err)
		}
	}
	if _, err := s.Put("phone", bytes.NewReader(jpeg)); !errors.Is(err, ErrLimit) {
		t.Fatalf("got %v, want ErrLimit", err)
	}
	if _, err := s.Put("other", bytes.NewReader(jpeg)); err != nil {
		t.Fatalf("another device is not held back: %v", err)
	}
}

func TestTheByteLimitCounts(t *testing.T) {
	s := open(t)
	big := append([]byte{0xFF, 0xD8, 0xFF}, make([]byte, core.MaxImageBytes-3)...)
	for i := 0; i < MaxBytesPerOwner/core.MaxImageBytes; i++ {
		if _, err := s.Put("phone", bytes.NewReader(big)); err != nil {
			t.Fatalf("photo %d: %v", i, err)
		}
	}
	if _, err := s.Put("phone", bytes.NewReader(jpeg)); !errors.Is(err, ErrLimit) {
		t.Fatalf("got %v, want ErrLimit", err)
	}
}

func TestADeviceOnlyUsesItsOwnPhotos(t *testing.T) {
	s := open(t)
	up, _ := s.Put("phone", bytes.NewReader(jpeg))
	if _, _, err := s.Hold("other", []string{up.ID}); !errors.Is(err, ErrGone) {
		t.Fatalf("another device held it: %v", err)
	}
	s.Delete("other", up.ID)
	if s.Count() != 1 {
		t.Fatal("another device deleted it")
	}
	s.DropOwner("other")
	if s.Count() != 1 {
		t.Fatal("another device's leaving deleted it")
	}
	if _, _, err := s.Hold("phone", []string{up.ID, "up_" + strings.Repeat("0", 32)}); !errors.Is(err, ErrGone) {
		t.Fatalf("an unknown id: %v", err)
	}
	s.Delete("phone", up.ID)
	if s.Count() != 0 {
		t.Fatal("the owner could not delete it")
	}
}

func TestSweepDeletesAfterTheLastUse(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	up, _ := s.Put("phone", bytes.NewReader(png))
	ims, release, err := s.Hold("phone", []string{up.ID})
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(3 * KeepFor)
	s.Sweep()
	s.Delete("phone", up.ID)
	s.DropOwner("phone")
	if _, err := os.Stat(ims[0].Path); err != nil {
		t.Fatalf("a held photo was deleted: %v", err)
	}

	release()
	release() // a second call changes nothing
	now = now.Add(KeepFor - time.Second)
	s.Sweep()
	if s.Count() != 1 {
		t.Fatal("deleted before KeepFor had passed since the release")
	}
	now = now.Add(2 * time.Second)
	s.Sweep()
	if s.Count() != 0 {
		t.Fatal("not deleted after KeepFor")
	}
	if _, err := os.Stat(ims[0].Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the file is still there: %v", err)
	}
	if _, _, err := s.Hold("phone", []string{up.ID}); !errors.Is(err, ErrGone) {
		t.Fatalf("a deleted photo was held: %v", err)
	}
}

func TestHoldSaysGoneWhenTheFileIsMissing(t *testing.T) {
	s := open(t)
	up, _ := s.Put("phone", bytes.NewReader(jpeg))
	ims, release, _ := s.Hold("phone", []string{up.ID})
	release()
	if err := os.Remove(ims[0].Path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Hold("phone", []string{up.ID}); !errors.Is(err, ErrGone) {
		t.Fatalf("got %v, want ErrGone", err)
	}
}

// A photo's file that is no longer a plain file (something else was put in
// its place) is not handed to an AI.
func TestHoldSaysGoneWhenTheFileIsNoFile(t *testing.T) {
	s := open(t)
	up, _ := s.Put("phone", bytes.NewReader(jpeg))
	ims, release, _ := s.Hold("phone", []string{up.ID})
	release()
	if err := os.Remove(ims[0].Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(ims[0].Path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Hold("phone", []string{up.ID}); !errors.Is(err, ErrGone) {
		t.Fatalf("got %v, want ErrGone", err)
	}
}

func TestOpenClearsOnlyItsOwnLeftovers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "uploads")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := "up_" + strings.Repeat("a", 32) + ".jpg"
	for _, name := range []string{old, old + ".tmp", "notes.txt", "up_short.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	left, _ := os.ReadDir(dir)
	var names []string
	for _, e := range left {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "notes.txt,up_short.jpg" {
		t.Fatalf("left in the folder: %v", names)
	}
}

func TestCloseDeletesEverything(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "uploads"))
	if err != nil {
		t.Fatal(err)
	}
	up, _ := s.Put("phone", bytes.NewReader(jpeg))
	ims, _, _ := s.Hold("phone", []string{up.ID}) // never released
	s.Close()
	s.Close()
	if _, err := os.Stat(ims[0].Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the file outlived the store: %v", err)
	}
}

func TestOpenWantsAnAbsoluteFolder(t *testing.T) {
	if _, err := Open("uploads"); err == nil {
		t.Fatal("a relative folder was taken")
	}
}
