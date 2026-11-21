package made

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/runs"
)

var (
	pngBytes  = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 13, 'I', 'H', 'D', 'R', 0, 0, 4, 0, 0, 0, 3, 0}, bytes.Repeat([]byte{1}, 100)...)
	jpegBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{2}, 100)...)
)

// aiPicture writes a picture as the AI would, in its own folder.
func aiPicture(t *testing.T, name string, data []byte) core.MadeFile {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return core.MadeFile{Path: p, Type: core.SniffImage(data), Data: data}
}

func openStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "made"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	return s, &now
}

func TestPutKeepsACopyForThatDeviceAndRunOnly(t *testing.T) {
	s, _ := openStore(t)
	f := aiPicture(t, "ig_abc.png", pngBytes)
	p, err := s.Put("phone-a", "run-1", f)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidID(p.ID) || p.Type != core.ImagePNG || p.Bytes != int64(len(pngBytes)) || p.Width != 1024 || p.Height != 768 {
		t.Fatalf("picture = %+v", p)
	}
	path, typ, err := s.Get("phone-a", "run-1", p.ID)
	if err != nil || typ != core.ImagePNG || filepath.Dir(path) != s.Dir() {
		t.Fatalf("Get = %q %q %v", path, typ, err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, pngBytes) {
		t.Error("the copy is not the AI's bytes as they were")
	}
	// The AI's own file is left where it was, as it was.
	if got, err := os.ReadFile(f.Path); err != nil || !bytes.Equal(got, pngBytes) {
		t.Error("the AI's original was touched")
	}
	for _, c := range []struct{ owner, run, id string }{
		{"phone-b", "run-1", p.ID}, {"phone-a", "run-2", p.ID}, {"phone-a", "run-1", "img_" + string(bytes.Repeat([]byte{'0'}, 32))},
	} {
		if _, _, err := s.Get(c.owner, c.run, c.id); err != ErrGone {
			t.Errorf("Get(%q, %q, %q) = %v, want ErrGone", c.owner, c.run, c.id, err)
		}
	}
}

func TestPutRefusesWhatIsNotThePictureThatWasChecked(t *testing.T) {
	s, _ := openStore(t)
	// The bytes are not the type that was checked.
	f := aiPicture(t, "ig_abc.png", pngBytes)
	f.Data = []byte("<svg/>")
	if _, err := s.Put("phone-a", "run-1", f); err == nil {
		t.Error("bytes that are not the checked picture were kept")
	}
	// The AI's file changed after FindMade read it: the copy is what was read (R-159).
	g := aiPicture(t, "ig_def.png", pngBytes)
	os.WriteFile(g.Path, []byte("changed"), 0o600)
	if p, err := s.Put("phone-a", "run-1", g); err != nil {
		t.Errorf("the checked bytes: %v", err)
	} else if path, _, _ := s.Get("phone-a", "run-1", p.ID); path != "" {
		if b, _ := os.ReadFile(path); !bytes.Equal(b, pngBytes) {
			t.Error("the copy is not what was checked")
		}
		s.DropOwner("phone-a")
	}
	big := aiPicture(t, "ig_big.jpg", append(jpegBytes, make([]byte, core.MaxMadeBytes)...))
	if _, err := s.Put("phone-a", "run-1", big); err == nil {
		t.Error("a file over the limit was kept")
	}
	if s.Count() != 0 {
		t.Errorf("%d copies kept", s.Count())
	}
	if left, _ := os.ReadDir(s.Dir()); len(left) != 0 {
		t.Errorf("%d files left in the folder", len(left))
	}
}

// R-162 (2호's review of G1-G6, middle): a device unpaired while its run
// was still answering got the run's pictures kept afterwards, and so did a
// store that had closed.
func TestNoCopyAfterUnpairingOrClose(t *testing.T) {
	s, _ := openStore(t)
	s.DropOwner("phone-a")
	if _, err := s.Put("phone-a", "run-1", aiPicture(t, "a.png", pngBytes)); err == nil {
		t.Error("a copy was kept for an unpaired device")
	}
	if _, err := s.Put("phone-b", "run-2", aiPicture(t, "b.png", pngBytes)); err != nil {
		t.Errorf("another device: %v", err)
	}
	s.Close()
	if _, err := s.Put("phone-b", "run-2", aiPicture(t, "c.png", pngBytes)); err == nil {
		t.Error("a copy was kept after Close")
	}
	if left, _ := os.ReadDir(s.Dir()); len(left) != 0 {
		t.Errorf("%d files left", len(left))
	}
}

// R-165 (2호's review, low): a copy is kept from its own answer, the run
// from its last; the copy must not go before the run can no longer be read.
func TestACopyLastsAsLongAsItsRun(t *testing.T) {
	if KeepFor < runs.KeepFor+runs.PartLimit {
		t.Errorf("KeepFor %v < the run's %v plus an answer's %v", KeepFor, runs.KeepFor, runs.PartLimit)
	}
}

func TestCopiesGoAfterADayWithTheDeviceAndAtClose(t *testing.T) {
	s, now := openStore(t)
	a, _ := s.Put("phone-a", "run-1", aiPicture(t, "a.png", pngBytes))
	*now = now.Add(time.Hour)
	b, _ := s.Put("phone-a", "run-2", aiPicture(t, "b.jpg", jpegBytes))
	c, _ := s.Put("phone-b", "run-3", aiPicture(t, "c.jpg", jpegBytes))

	*now = now.Add(KeepFor - 30*time.Minute) // a is a day old now, b not yet
	s.Sweep()
	if _, _, err := s.Get("phone-a", "run-1", a.ID); err != ErrGone {
		t.Error("a copy older than a day stays")
	}
	if _, _, err := s.Get("phone-a", "run-2", b.ID); err != nil {
		t.Error("a younger copy went")
	}
	s.DropOwner("phone-a")
	if _, _, err := s.Get("phone-a", "run-2", b.ID); err != ErrGone {
		t.Error("an unpaired device's copy stays")
	}
	if _, _, err := s.Get("phone-b", "run-3", c.ID); err != nil {
		t.Error("another device's copy went with it")
	}
	s.Close()
	if left, _ := os.ReadDir(s.Dir()); len(left) != 0 {
		t.Errorf("%d files left after Close", len(left))
	}
}

func TestTheOldestGoFirstOverTheLimit(t *testing.T) {
	old := maxTotalBytes
	maxTotalBytes = int64(len(pngBytes)) * 2
	t.Cleanup(func() { maxTotalBytes = old })
	s, now := openStore(t)
	var ids []string
	for i := range 3 {
		*now = now.Add(time.Minute)
		p, err := s.Put("phone-a", "run-1", aiPicture(t, "p"+string(rune('0'+i))+".png", pngBytes))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	if _, _, err := s.Get("phone-a", "run-1", ids[0]); err != ErrGone {
		t.Error("the oldest copy stays over the limit")
	}
	for _, id := range ids[1:] {
		if _, _, err := s.Get("phone-a", "run-1", id); err != nil {
			t.Errorf("a newer copy went: %v", err)
		}
	}
}

func TestOpenDeletesOnlyTheStoresOwnFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "made")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	own := "img_" + string(bytes.Repeat([]byte{'a'}, 32))
	for _, n := range []string{own + ".png", own + ".jpg.tmp", "notes.txt", "img_short.png"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var left []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if len(left) != 2 || left[0] != "img_short.png" || left[1] != "notes.txt" {
		t.Errorf("left = %v", left)
	}
}
