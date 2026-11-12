package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/devices"
	"github.com/heeo-dev/askorus/agent/internal/tailnet"
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

// R-104 (#46): the Funnel watch says when it cannot read tailscale, once
// each time that starts, says when it can again, and still ends serve when
// Funnel shows up.
func TestFunnelWatchSaysWhenItIsBlind(t *testing.T) {
	var mu sync.Mutex
	step := 0
	old := lookTailnetFn
	t.Cleanup(func() { lookTailnetFn = old })
	lookTailnetFn = func(context.Context, int) tailnetInfo {
		mu.Lock()
		defer mu.Unlock()
		step++
		switch {
		case step <= 3:
			return tailnetInfo{problem: errors.New("tailscale status failed")}
		case step <= 5:
			return tailnetInfo{served: true}
		case step <= 7:
			return tailnetInfo{problem: errors.New("tailscale status failed")}
		}
		return tailnetInfo{served: true, serve: tailnet.Serve{Funnel: true}}
	}
	var told []string
	exposed := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go funnelWatch(ctx, 8790, 5*time.Millisecond, func() { close(exposed) }, func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			told = append(told, "blind")
		} else {
			told = append(told, "sees")
		}
	})
	select {
	case <-exposed:
	case <-ctx.Done():
		t.Fatal("funnel was not noticed")
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(told, ","); got != "blind,sees,blind" {
		t.Errorf("told = %s", got)
	}
}
