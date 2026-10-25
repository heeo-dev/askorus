package devices

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddVerifyRevoke(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	tok, d, err := s.Add("Galaxy S23")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, TokenPrefix) || len(tok) < 40 {
		t.Errorf("token looks weak: %d chars", len(tok))
	}
	b, _ := os.ReadFile(filepath.Join(dir, FileName))
	if strings.Contains(string(b), tok) || strings.Contains(string(b), strings.TrimPrefix(tok, TokenPrefix)) {
		t.Fatal("device file contains the raw token")
	}

	s2, err := Open(dir) // reload from disk
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := s2.Verify(tok); !ok || got.ID != d.ID {
		t.Errorf("Verify after reload = %+v %v", got, ok)
	}
	for _, bad := range []string{"", tok + "x", "ask_", strings.TrimPrefix(tok, TokenPrefix)} {
		if _, ok := s2.Verify(bad); ok {
			t.Errorf("Verify(%q) accepted", bad)
		}
	}
	if _, err := s2.Revoke(d.Name); err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Verify(tok); ok {
		t.Error("revoked token still works")
	}
	if _, err := s2.Revoke(d.ID); err != ErrNotFound {
		t.Errorf("second revoke err = %v", err)
	}
}

func TestAddRejectsBadName(t *testing.T) {
	s, _ := Open(t.TempDir())
	if _, _, err := s.Add("  "); err == nil {
		t.Error("blank name accepted")
	}
	if _, _, err := s.Add(strings.Repeat("가", 65)); err == nil {
		t.Error("long name accepted")
	}
}

func TestRevokeAmbiguousName(t *testing.T) {
	s, _ := Open(t.TempDir())
	s.Add("phone")
	s.Add("phone")
	if _, err := s.Revoke("phone"); err == nil {
		t.Error("ambiguous name revoked a device")
	}
}
