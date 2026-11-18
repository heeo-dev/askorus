package agy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// R-124..R-127: what 2ho's review of #20 found (2026-11-18).

// R-126: a hook or a plugin of the person's agy could approve a read
// outside a photo run's folder, so agy then takes no photos.
func TestPhotosNeedNoHooks(t *testing.T) {
	for name, setup := range map[string]func(dir string){
		"hook": func(dir string) {
			os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(`{"hooks":{"PreToolUse":[{"matcher":"view_file","command":"x"}]}}`), 0o600)
		},
		"broken hooks":   func(dir string) { os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(`{not json`), 0o600) },
		"plugin":         func(dir string) { os.MkdirAll(filepath.Join(dir, "plugins", "some-plugin"), 0o700) },
		"no config read": func(dir string) { os.MkdirAll(filepath.Join(dir, "hooks.json"), 0o700) },
	} {
		t.Run(name, func(t *testing.T) {
			exe := fakecli.Use(t, fakecli.RoleAgy)
			cfg := t.TempDir()
			setup(cfg)
			a := New(Options{Path: exe, ConfigDir: cfg})
			if a.SeesImages("gemini-3.8-flash") || a.ImageModels().All {
				t.Fatal("agy takes photos with a hook or a plugin")
			}
			folder := t.TempDir()
			if _, err := ask(t, a, core.AskRequest{Folder: folder, Images: keptPhotos(t)}); core.ErrorCode(err) != core.ErrCodeNoImages {
				t.Errorf("ask with photos = %v", err)
			}
			judgeDir := core.JudgeFolder(folder)
			os.MkdirAll(judgeDir, 0o700)
			if _, err := a.Synthesize(context.Background(), core.SynthRequest{Folder: judgeDir, Prompt: "x", Model: "gemini-3.8-flash", Images: keptPhotos(t)}); core.ErrorCode(err) != core.ErrCodeInternal {
				t.Errorf("writer with photos = %v", err)
			}
			if n := len(fakecli.Calls(t)); n != 0 {
				t.Errorf("%d runs", n)
			}
		})
	}
	// Empty hooks and an empty plugins folder are no hook.
	cfg := t.TempDir()
	os.WriteFile(filepath.Join(cfg, "hooks.json"), []byte(`{"hooks":{"PreToolUse":[]}}`), 0o600)
	os.MkdirAll(filepath.Join(cfg, "plugins"), 0o700)
	if a := New(Options{Path: "x", ConfigDir: cfg}); !a.SeesImages("gemini-3.8-flash") {
		t.Error("empty hooks and plugins took photos away")
	}
}

// An agent of the same name in agy's own config folder: the run is refused.
func TestShadowedAgent(t *testing.T) {
	exe := fakecli.Use(t, fakecli.RoleAgy)
	cfg := t.TempDir()
	os.MkdirAll(filepath.Join(cfg, "agents"), 0o700)
	os.WriteFile(filepath.Join(cfg, "agents", "askorus-ask.md"), []byte("---\nname: askorus-ask\ntools: [run_command]\n---\n"), 0o600)
	a := New(Options{Path: exe, ConfigDir: cfg})
	if _, err := ask(t, a, core.AskRequest{Folder: t.TempDir()}); core.ErrorCode(err) != core.ErrCodeInternal {
		t.Errorf("ask = %v", err)
	}
	if n := len(fakecli.Calls(t)); n != 0 {
		t.Errorf("%d runs", n)
	}
}

// R-125: a run folder left behind is removed by the next photo run once
// it is stale; others' folders and fresh ones stay.
func TestSweepPhotoFolders(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "0123456789abcdef01234567")
	fresh := filepath.Join(root, "fedcba9876543210fedcba98")
	other := filepath.Join(root, "keep-me")
	for _, d := range []string{old, fresh, other} {
		os.MkdirAll(d, 0o700)
		os.WriteFile(filepath.Join(d, "photo-1.jpg"), testJPEG, 0o600)
	}
	past := time.Now().Add(-time.Hour)
	os.Chtimes(old, past, past)
	os.Chtimes(other, past, past)
	sweepPhotoFolders(root, time.Now())
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("a stale run folder stayed")
	}
	for _, d := range []string{fresh, other} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("%s went: %v", d, err)
		}
	}
}

// R-127: the writer's JSON loses file links in every string.
func TestStripOutput(t *testing.T) {
	in := `{"answer":"See [photo-1.jpg](file:///C:/Users/someone/AppData/photo-1.jpg) and FILE:///C:/Users/someone/b.png."}`
	out := stripOutput([]byte(in))
	var v struct{ Answer string }
	if err := json.Unmarshal(out, &v); err != nil || v.Answer != "See photo-1.jpg and b.png." {
		t.Errorf("stripOutput = %s %v", out, err)
	}
	spec := `{"kind":"compare","intent":"x","depth":"short","language":"ko"}`
	var a, b map[string]string
	json.Unmarshal([]byte(spec), &a)
	json.Unmarshal(stripOutput([]byte(spec)), &b)
	if len(a) != 4 || a["kind"] != b["kind"] || a["language"] != b["language"] {
		t.Errorf("spec = %v", b)
	}
	if got := string(stripOutput([]byte(`not json`))); got != "not json" {
		t.Errorf("not json = %q", got)
	}
	if strings.Contains(string(stripOutput([]byte(`["file:/C:/Users/x/a.jpg", 3]`))), "Users") {
		t.Error("a list kept a path")
	}
}
