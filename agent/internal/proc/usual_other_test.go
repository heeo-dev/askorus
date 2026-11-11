//go:build !windows

package proc

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// #52: an agent started at logon keeps the PATH of the day it was told to.
// A program installed later into a folder of its own is still found.
func TestLookExeFallsBackToTheUsualFolders(t *testing.T) {
	home := t.TempDir()
	empty := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", empty)
	if _, _, err := LookExe("opencode"); err != ErrNotFound {
		t.Fatalf("found a program that is nowhere: %v", err)
	}
	dir := filepath.Join(home, ".opencode", "bin")
	if err := os.MkdirAll(filepath.Join(dir, "claude"), 0o755); err != nil { // a folder with a program's name
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\n"), 0o644); err != nil { // not runnable
		t.Fatal(err)
	}
	want := filepath.Join(dir, "opencode")
	if err := os.WriteFile(want, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, shim, err := LookExe("opencode")
	if err != nil || got != want || shim != "" {
		t.Errorf("LookExe = %q, %q, %v, want %q", got, shim, err, want)
	}
	for _, name := range []string{"claude", "codex"} {
		if p, ok := LookUsual(name); ok {
			t.Errorf("LookUsual(%s) = %q: a folder or a file that cannot be run", name, p)
		}
	}
	// PATH comes first.
	onPath := filepath.Join(empty, "opencode")
	if err := os.WriteFile(onPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := LookExe("opencode"); got != onPath {
		t.Errorf("LookExe = %q, want the one on PATH %q", got, onPath)
	}
	if mac := usualDirs("/Users/me", "darwin"); !slices.Contains(mac, "/opt/homebrew/bin") || !slices.Contains(mac, "/Users/me/.local/bin") {
		t.Errorf("macOS folders = %q", mac)
	}
	if slices.Contains(usualDirs("/home/me", "linux"), "/opt/homebrew/bin") {
		t.Error("Homebrew's folder on Linux")
	}
}
