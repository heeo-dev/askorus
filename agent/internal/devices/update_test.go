package devices

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every paired phone may update the agent unless it is turned off (#35);
// the setting stays with the device and is not written while it is on.
func TestUpdateIsOnUntilTurnedOff(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, a, _ := s.Add("Galaxy S23")
	_, b, _ := s.Add("Galaxy S10")
	if got, ok := s.Get(a.ID); !ok || got.NoUpdate {
		t.Fatalf("a new device: %+v, %v", got, ok)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "devices.json"))
	if strings.Contains(string(raw), "noUpdate") {
		t.Fatal("the file names noUpdate for devices that may update")
	}

	d, err := s.SetUpdate("Galaxy S23", false)
	if err != nil || !d.NoUpdate || d.ID != a.ID {
		t.Fatalf("SetUpdate by name: %+v, %v", d, err)
	}
	// Another process (the agent) reads it from the file.
	other, _ := Open(dir)
	if got, _ := other.Get(a.ID); !got.NoUpdate {
		t.Fatal("the setting did not reach the file")
	}
	if got, _ := other.Get(b.ID); got.NoUpdate {
		t.Fatal("turning one phone off turned another off")
	}
	if d, _ := s.SetUpdate(a.ID, true); d.NoUpdate {
		t.Fatal("SetUpdate on by id")
	}
	if _, err := s.SetUpdate("nobody", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown device: %v", err)
	}
	if _, ok := s.Get("nobody"); ok {
		t.Fatal("Get found an unknown device")
	}
	s.Add("Galaxy S10")
	if _, err := s.SetUpdate("Galaxy S10", false); err == nil || !strings.Contains(err.Error(), "more than one") {
		t.Fatalf("an ambiguous name: %v", err)
	}
}
