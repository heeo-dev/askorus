package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/devices"
)

// R-103 (#45): revoking a device says when folders are still kept open,
// because those outlive the phone that asked for them. With none kept open
// it says nothing more.
func TestRevokeTellsAboutKeptOpenFolders(t *testing.T) {
	dir := t.TempDir()
	store, err := devices.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, a, _ := store.Add("phone a")
	_, b, _ := store.Add("phone b")

	var out bytes.Buffer
	if err := devicesCmd(dir, []string{"revoke", a.ID}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "kept open") {
		t.Errorf("nothing is kept open, yet: %s", out.String())
	}

	cfg := config.Default()
	cfg.Folders = []config.Folder{
		{ID: "one", Path: t.TempDir(), Server: config.FolderServer{Enabled: true, Spawn: "same-dir", Capacity: 4}},
		{ID: "two", Path: t.TempDir(), Server: config.FolderServer{Enabled: true, Spawn: "same-dir", Capacity: 4}},
		{ID: "off", Path: t.TempDir()},
	}
	if err := config.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := devicesCmd(dir, []string{"revoke", b.ID}, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "2 folder(s) are still kept open") || !strings.Contains(got, "열어 둔 폴더 2개") {
		t.Errorf("revoke said: %s", got)
	}
}
