package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
)

func abs(t *testing.T, parts ...string) string {
	t.Helper()
	return filepath.Join(append([]string{t.TempDir()}, parts...)...)
}

func TestLoadMissingGivesDefaults(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != DefaultPort || c.Folders == nil || len(c.Folders) != 0 {
		t.Errorf("defaults = %+v", c)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := Default()
	in.DefaultModel = "opus"
	in.DefaultEffort = "high"
	in.ServeHosts = []string{"pc.example.ts.net"}
	in.Folders = []Folder{
		{Path: abs(t, "My App")},
		{Path: abs(t, "my-app")},
		{Path: abs(t, "repo"), Server: FolderServer{Enabled: true, Spawn: SpawnWorktree, Capacity: 2}},
	}
	if err := Save(dir, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{out.Folders[0].ID, out.Folders[1].ID, out.Folders[2].ID}; got[0] != "my-app" || got[1] != "my-app-2" || got[2] != "repo" {
		t.Errorf("folder ids = %v", got)
	}
	if f := out.Folders[0]; f.Server.Enabled || f.Server.Spawn != SpawnSameDir || f.Server.Capacity != DefaultCapacity {
		t.Errorf("server defaults = %+v", f.Server)
	}
	if f, ok := out.FolderByID("repo"); !ok || !f.Server.Enabled || f.Server.Capacity != 2 {
		t.Errorf("FolderByID(repo) = %+v %v", f, ok)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temporary files left behind: %v", entries)
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]func(c *Config){
		"port":   func(c *Config) { c.Port = 80 },
		"effort": func(c *Config) { c.DefaultEffort = "turbo" },
		"model":  func(c *Config) { c.DefaultModel = "opus; rm -rf" },
		"relative path": func(c *Config) {
			c.Folders = []Folder{{ID: "a", Path: "rel", Server: FolderServer{Spawn: SpawnSameDir, Capacity: 1}}}
		},
		"bad id": func(c *Config) {
			c.Folders = []Folder{{ID: "A B", Path: abs(t), Server: FolderServer{Spawn: SpawnSameDir, Capacity: 1}}}
		},
		"spawn": func(c *Config) {
			c.Folders = []Folder{{ID: "a", Path: abs(t), Server: FolderServer{Spawn: "anywhere", Capacity: 1}}}
		},
		"capacity": func(c *Config) {
			c.Folders = []Folder{{ID: "a", Path: abs(t), Server: FolderServer{Spawn: SpawnSameDir, Capacity: 99}}}
		},
		"host":        func(c *Config) { c.ServeHosts = []string{"Evil.com:80"} },
		"claude path": func(c *Config) { c.ClaudePath = "claude.exe" },
	}
	for name, mutate := range cases {
		c := Default()
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if err := Default().Validate(); err != nil {
		t.Errorf("default invalid: %v", err)
	}
}

func TestValidModel(t *testing.T) {
	for _, m := range []string{"opus", "sonnet", "claude-opus-5-5", "opus[1m]", "haiku-5.5"} {
		if !ValidModel(m) {
			t.Errorf("%q rejected", m)
		}
	}
	for _, m := range []string{"", "-x", "--dangerously-skip-permissions", "a b", "opus;rm", strings.Repeat("a", 65)} {
		if ValidModel(m) {
			t.Errorf("%q accepted", m)
		}
	}
}

func TestLoadRejectsBadFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, FileName), []byte(`{"port": 22}`), 0o600)
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "port") {
		t.Errorf("err = %v", err)
	}
}

func TestDirEnvOverride(t *testing.T) {
	t.Setenv("ASKSEMBLE_AGENT_DIR", `X:\somewhere`)
	if d, _ := Dir(); d != `X:\somewhere` {
		t.Errorf("Dir = %q", d)
	}
}

func TestAskSettings(t *testing.T) {
	c := Default()
	dir, err := c.AskFolder()
	if err != nil || !filepath.IsAbs(dir) || filepath.Base(dir) != "ask" || filepath.Base(filepath.Dir(dir)) != "Asksemble" {
		t.Fatalf("default ask folder = %q, %v", dir, err)
	}
	c.Ask.Folder = abs(t, "asks")
	c.Ask.Tiers = map[string]map[string]core.AskModel{"opencode": {"deep": {Model: "opencode-go/kimi-k3.1-pro#high"}}}
	c.CodexPath = abs(t, "codex.exe")
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.AskFolder(); got != c.Ask.Folder {
		t.Errorf("ask folder = %q", got)
	}
	tiers := func(agent, tier string, m core.AskModel) map[string]map[string]core.AskModel {
		return map[string]map[string]core.AskModel{agent: {tier: m}}
	}
	for name, mutate := range map[string]func(*Config){
		"relative folder": func(c *Config) { c.Ask.Folder = "asks" },
		"relative codex":  func(c *Config) { c.CodexPath = "codex.exe" },
		"relative oc":     func(c *Config) { c.OpenCodePath = "opencode.exe" },
		"bad tier":        func(c *Config) { c.Ask.Tiers = tiers("codex", "max", core.AskModel{Model: "x"}) },
		"flag model":      func(c *Config) { c.Ask.Tiers = tiers("codex", "deep", core.AskModel{Model: "--yolo"}) },
		"bad effort":      func(c *Config) { c.Ask.Tiers = tiers("codex", "deep", core.AskModel{Effort: "HIGH"}) },
		"bad AI name":     func(c *Config) { c.Ask.Tiers = tiers("Codex!", "deep", core.AskModel{}) },
	} {
		bad := c
		mutate(&bad)
		if err := bad.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
