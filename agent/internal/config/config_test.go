package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
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

func TestJudgeDefaultsWithoutBlock(t *testing.T) {
	dir := t.TempDir()
	// An agent.json from before P2-3 S3: no judge block.
	old := `{"port": 8790, "folders": [], "ask": {"tiers": {"codex": {"deep": {"model": "gpt-6.1-sol"}}}}}`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	j := c.Judge
	// S6 (11-01 user decision 2 and 3): the number checks are on next to
	// unsure, and 30 units are judged; the setting still accepts 40.
	if j.Off || j.PerAIOrDefault() != 2 || j.MaxUnitsOrDefault() != 30 || !slices.Equal(j.ChecksOrDefault(), []string{"unsure", "numberMismatch", "numbersAbsent"}) {
		t.Errorf("defaults = off %v, perAI %d, maxUnits %d, checks %v", j.Off, j.PerAIOrDefault(), j.MaxUnitsOrDefault(), j.ChecksOrDefault())
	}
	if DefaultJudgePerAI != 2 || judge.MaxUnits != 40 || judge.DefaultMaxUnits != 30 {
		t.Errorf("default constants changed: %d %d %d", DefaultJudgePerAI, judge.MaxUnits, judge.DefaultMaxUnits)
	}
	for _, n := range []int{1, 30, 40} {
		if got := (JudgeConfig{MaxUnits: n}).MaxUnitsOrDefault(); got != n {
			t.Errorf("maxUnits %d = %d", n, got)
		}
	}
	// ChecksOrDefault is a copy: changing it changes neither the defaults
	// nor the settings.
	got := j.ChecksOrDefault()
	got[0] = "x"
	if judge.DefaultChecks[0] != judge.CheckUnsure {
		t.Fatalf("ChecksOrDefault shares judge.DefaultChecks")
	}
	j.Checks = []string{judge.CheckNoEvidence}
	got = j.ChecksOrDefault()
	got[0] = "x"
	if j.Checks[0] != judge.CheckNoEvidence {
		t.Errorf("ChecksOrDefault shares Checks")
	}
}

func TestJudgeSaveLoad(t *testing.T) {
	dir := t.TempDir()
	// A zero block is not written.
	if err := Save(dir, Default()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, FileName))
	if strings.Contains(string(b), "judge") {
		t.Errorf("zero judge block written: %s", b)
	}
	in := Default()
	in.Judge = JudgeConfig{
		Off:      true,
		Models:   map[string]core.AskModel{"codex": {Model: "gpt-6-luna", Effort: "low"}, "claude": {Effort: "high"}},
		PerAI:    1,
		Checks:   []string{judge.CheckUnsure, judge.CheckNumberMismatch},
		MaxUnits: 20,
	}
	if err := Save(dir, in); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, FileName))
	for _, key := range []string{`"judge"`, `"off": true`, `"models"`, `"perAI": 1`, `"checks"`, `"maxUnits": 20`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("saved file lacks %s: %s", key, b)
		}
	}
	out, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.Judge, in.Judge) {
		t.Errorf("judge block = %+v, want %+v", out.Judge, in.Judge)
	}
	if out.Judge.PerAIOrDefault() != 1 || out.Judge.MaxUnitsOrDefault() != 20 || !slices.Equal(out.Judge.ChecksOrDefault(), in.Judge.Checks) {
		t.Errorf("accessors = %d %d %v", out.Judge.PerAIOrDefault(), out.Judge.MaxUnitsOrDefault(), out.Judge.ChecksOrDefault())
	}
}

func TestJudgeValidate(t *testing.T) {
	good := Default()
	good.Judge = JudgeConfig{
		Models:   map[string]core.AskModel{"opencode": {Model: "opencode-go/kimi-k3.1-pro#high"}, "codex": {Effort: "low"}},
		PerAI:    4,
		Checks:   slices.Clone(judge.AllChecks),
		MaxUnits: 40,
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("good judge block refused: %v", err)
	}
	good.Judge.PerAI, good.Judge.MaxUnits = 1, 1
	if err := good.Validate(); err != nil {
		t.Fatalf("perAI 1, maxUnits 1 refused: %v", err)
	}
	cases := map[string]struct {
		j    JudgeConfig
		want string
	}{
		"AI name":     {JudgeConfig{Models: map[string]core.AskModel{"Codex!": {}}}, `judge.models: "Codex!"`},
		"flag model":  {JudgeConfig{Models: map[string]core.AskModel{"codex": {Model: "--yolo"}}}, "judge.models.codex: "},
		"space model": {JudgeConfig{Models: map[string]core.AskModel{"codex": {Model: "gpt 6"}}}, "judge.models.codex: "},
		"effort":      {JudgeConfig{Models: map[string]core.AskModel{"claude": {Effort: "HIGH"}}}, "judge.models.claude: "},
		"perAI 5":     {JudgeConfig{PerAI: 5}, "judge.perAI"},
		"perAI -1":    {JudgeConfig{PerAI: -1}, "judge.perAI"},
		"maxUnits 41": {JudgeConfig{MaxUnits: 41}, "judge.maxUnits"},
		"maxUnits -1": {JudgeConfig{MaxUnits: -1}, "judge.maxUnits"},
		"check":       {JudgeConfig{Checks: []string{"unsure", "vibes"}}, `judge.checks: "vibes"`},
		"check case":  {JudgeConfig{Checks: []string{"Unsure"}}, `judge.checks: "Unsure"`},
		"check twice": {JudgeConfig{Checks: []string{"unsure", "noEvidence", "unsure"}}, `judge.checks: "unsure" is listed twice`},
	}
	for name, tc := range cases {
		c := Default()
		c.Judge = tc.j
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	// Load refuses a file with a bad judge block.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, FileName), []byte(`{"judge": {"perAI": 9}}`), 0o600)
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "judge.perAI") {
		t.Errorf("Load err = %v", err)
	}
}

func TestJudgeModelFor(t *testing.T) {
	def := core.AskModel{Model: "haiku", Effort: "medium"}
	j := JudgeConfig{Models: map[string]core.AskModel{
		"claude":   {Model: "sonnet"},
		"codex":    {Effort: "low"},
		"opencode": {Model: "opencode-go/glm-5", Effort: "max"},
		"other":    {},
	}}
	cases := []struct {
		agent string
		want  core.AskModel
	}{
		{"none", def}, // no override
		{"claude", core.AskModel{Model: "sonnet"}}, // a model: its own effort, empty, and the adapter decides
		{"codex", core.AskModel{Model: "haiku", Effort: "low"}},
		{"opencode", core.AskModel{Model: "opencode-go/glm-5", Effort: "max"}},
		{"other", def},
	}
	for _, c := range cases {
		if got := j.ModelFor(c.agent, def); got != c.want {
			t.Errorf("ModelFor(%s) = %+v, want %+v", c.agent, got, c.want)
		}
	}
	if got := (JudgeConfig{}).ModelFor("claude", def); got != def {
		t.Errorf("zero settings: %+v", got)
	}
}
