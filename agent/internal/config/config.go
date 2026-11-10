// Package config loads and saves the agent settings file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// FileName is the settings file inside the agent directory.
const FileName = "agent.json"

// DefaultPort is the 127.0.0.1 port when none is set. It avoids the ports
// already used by other local services on the reference PC.
const DefaultPort = 8790

// Spawn modes for folder server mode (F10).
const (
	SpawnSameDir  = "same-dir"
	SpawnWorktree = "worktree"
)

// DefaultCapacity is the concurrent session limit for server mode.
const DefaultCapacity = 4

// Config is the whole settings file. New fields get zero-value defaults so
// old files keep loading.
type Config struct {
	Port          int      `json:"port"`
	Folders       []Folder `json:"folders"`
	DefaultModel  string   `json:"defaultModel,omitempty"`
	DefaultEffort string   `json:"defaultEffort,omitempty"`
	// HiddenFolders are left out of the session list.
	HiddenFolders []string `json:"hiddenFolders,omitempty"`
	// ServeHosts are extra Host header names to accept, such as the
	// tailscale serve name "<pc>.<tailnet>.ts.net".
	ServeHosts []string `json:"serveHosts,omitempty"`
	// TailscaleOwner is the login of the PC owner. When set, requests whose
	// Tailscale-User-Login header names someone else are refused.
	TailscaleOwner string `json:"tailscaleOwner,omitempty"`
	// ClaudePath overrides where the claude executable is looked up.
	ClaudePath string `json:"claudePath,omitempty"`
	// CodexPath and OpenCodePath do the same for the AIs that only answer
	// questions (Phase 2). They must be real executables, never .cmd.
	CodexPath    string `json:"codexPath,omitempty"`
	OpenCodePath string `json:"opencodePath,omitempty"`
	// Ask configures mode ask (Phase 2).
	Ask AskConfig `json:"ask,omitzero"`
	// Judge configures the combined answers (P2-3; the name is the judging
	// days'). Everything has a default.
	Judge JudgeConfig `json:"judge,omitzero"`
	// KeepAwake stops the PC from sleeping while a background session or a
	// folder server runs (design: 절전). Off by default.
	KeepAwake bool `json:"keepAwake,omitempty"`
	// HistoryFolders lets the phone also start sessions in folders that
	// already have conversations (2026-10-27). On unless set to false.
	HistoryFolders *bool `json:"historyFolders,omitempty"`
}

// AskConfig is the ask settings. Everything has a default.
type AskConfig struct {
	// Folder is where asks run: an empty folder outside every project, so
	// no CLAUDE.md or AGENTS.md reaches the answers (P0-9). Default
	// <user cache dir>/Askorus/ask, %LOCALAPPDATA%\Askorus\ask on Windows.
	Folder string `json:"folder,omitempty"`
	// Tiers overrides the model of a tier for one AI, e.g.
	// {"codex": {"deep": {"model": "gpt-6.1-sol", "effort": "high"}}}.
	Tiers map[string]map[string]core.AskModel `json:"tiers,omitempty"`
}

// DefaultJudgePerAI is how many one-off processes (combined answers,
// refining) one AI runs at once when the settings name no number (spec
// part 3, 'AI마다 2개').
const DefaultJudgePerAI = 2

// MaxJudgePerAI bounds judge.perAI. Every AI together runs at most six.
const MaxJudgePerAI = 4

// JudgeConfig is the settings of agent.json judge (P2-3), which since C6d
// (#24) holds only what the combined answers use. Everything has a
// default, so a file without the block loads as before.
type JudgeConfig struct {
	// Off turns the combined answers off: no AI announces synthesize.
	Off bool `json:"off,omitempty"`
	// PerAI is how many one-off processes one AI runs at once, 1-4; 0
	// means DefaultJudgePerAI.
	PerAI int `json:"perAI,omitempty"`
	// models, checks and maxUnits went with the judging (C6d, #24): a file
	// that still has them loads, and they are dropped when it is saved.
}

// PerAIOrDefault is PerAI, or DefaultJudgePerAI when it is 0.
func (j JudgeConfig) PerAIOrDefault() int {
	if j.PerAI == 0 {
		return DefaultJudgePerAI
	}
	return j.PerAI
}

// validate checks the judge block. Its errors name the key, as ask.tiers'
// do.
func (j JudgeConfig) validate() error {
	if j.PerAI < 0 || j.PerAI > MaxJudgePerAI {
		return fmt.Errorf("judge.perAI: %d must be 1-%d, or 0 for %d", j.PerAI, MaxJudgePerAI, DefaultJudgePerAI)
	}
	return nil
}

// AskFolder returns the ask folder from the settings or the default.
func (c Config) AskFolder() (string, error) {
	if c.Ask.Folder != "" {
		return filepath.Clean(c.Ask.Folder), nil
	}
	base, err := CacheBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ask"), nil
}

// HistoryFoldersOn reports whether folders with conversations may be used.
func (c Config) HistoryFoldersOn() bool { return c.HistoryFolders == nil || *c.HistoryFolders }

// AddFolder allows path for new sessions and returns the saved entry. A
// folder that is already allowed is returned as it is.
func (c *Config) AddFolder(path, id string) (Folder, bool) {
	path = filepath.Clean(path)
	for _, f := range c.Folders {
		if strings.EqualFold(f.Path, path) {
			return f, false
		}
	}
	c.Folders = append(c.Folders, Folder{ID: id, Path: path})
	c.normalize()
	return c.Folders[len(c.Folders)-1], true
}

// Folder is a folder new sessions may be started in (F4).
type Folder struct {
	ID     string       `json:"id"`
	Path   string       `json:"path"`
	Server FolderServer `json:"server"`
}

// FolderServer is the per-folder server mode setting (F10). Off by default.
type FolderServer struct {
	Enabled  bool   `json:"enabled"`
	Spawn    string `json:"spawn,omitempty"`
	Capacity int    `json:"capacity,omitempty"`
}

// Default returns the settings used when no file exists yet.
func Default() Config {
	return Config{Port: DefaultPort, Folders: []Folder{}}
}

// Dir returns the agent directory: $ASKORUS_AGENT_DIR, or
// <user config dir>/Askorus.
func Dir() (string, error) {
	if d := os.Getenv("ASKORUS_AGENT_DIR"); d != "" {
		return d, nil
	}
	return ConfigBase()
}

// DirName is the agent's folder under the user's config and cache folders.
const DirName = "Askorus"

// Under returns base/Askorus.
func Under(base string) string {
	return filepath.Join(base, DirName)
}

// ConfigBase is the folder under the user's config folder (%APPDATA% on
// Windows): settings, paired devices, logs.
func ConfigBase() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return Under(base), nil
}

// CacheBase is the folder under the user's cache folder (%LOCALAPPDATA%
// on Windows): the ask folder and the evaluation runs.
func CacheBase() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return Under(base), nil
}

// Load reads dir/agent.json. A missing file gives the defaults.
func Load(dir string) (Config, error) {
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	c := Default()
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", FileName, err)
	}
	c.normalize()
	if err := c.Validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", FileName, err)
	}
	return c, nil
}

// Save validates c and writes it to dir/agent.json atomically.
func Save(dir string, c Config) error {
	c.normalize()
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(filepath.Join(dir, FileName), append(b, '\n'))
}

// WriteFileAtomic writes to a temporary file next to path, then renames it.
func WriteFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	_ = f.Chmod(0o600) // best effort; Windows only honours the read-only bit
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Config) normalize() {
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	if c.Folders == nil {
		c.Folders = []Folder{}
	}
	used := map[string]bool{}
	for i := range c.Folders {
		f := &c.Folders[i]
		f.Path = filepath.Clean(f.Path)
		if f.ID == "" {
			f.ID = uniqueID(slug(filepath.Base(f.Path)), used)
		}
		used[f.ID] = true
		if f.Server.Spawn == "" {
			f.Server.Spawn = SpawnSameDir
		}
		if f.Server.Capacity == 0 {
			f.Server.Capacity = DefaultCapacity
		}
	}
}

var (
	idRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	modelRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\[\]-]{0,63}$`) // never starts with "-"
	hostRe   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)
	efforts  = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true, "max": true}
	nonSlugs = regexp.MustCompile(`[^a-z0-9]+`)
)

// ValidEffort reports whether e is an accepted effort level.
func ValidEffort(e string) bool { return efforts[e] }

// ValidModel reports whether m looks like a model name or alias.
func ValidModel(m string) bool { return modelRe.MatchString(m) }

// Validate checks every field.
func (c Config) Validate() error {
	if c.Port < 1024 || c.Port > 65535 {
		return fmt.Errorf("port %d out of range 1024-65535", c.Port)
	}
	if c.DefaultModel != "" && !ValidModel(c.DefaultModel) {
		return fmt.Errorf("defaultModel %q is not a model name", c.DefaultModel)
	}
	if c.DefaultEffort != "" && !ValidEffort(c.DefaultEffort) {
		return fmt.Errorf("defaultEffort %q must be low, medium, high, xhigh or max", c.DefaultEffort)
	}
	ids := map[string]bool{}
	for _, f := range c.Folders {
		if !idRe.MatchString(f.ID) {
			return fmt.Errorf("folder id %q must be 1-32 lowercase letters, digits or dashes", f.ID)
		}
		if ids[f.ID] {
			return fmt.Errorf("folder id %q is used twice", f.ID)
		}
		ids[f.ID] = true
		if !filepath.IsAbs(f.Path) {
			return fmt.Errorf("folder %q: path must be absolute", f.ID)
		}
		if f.Server.Spawn != SpawnSameDir && f.Server.Spawn != SpawnWorktree {
			return fmt.Errorf("folder %q: spawn must be same-dir or worktree", f.ID)
		}
		if f.Server.Capacity < 1 || f.Server.Capacity > 32 {
			return fmt.Errorf("folder %q: capacity must be 1-32", f.ID)
		}
	}
	for _, h := range c.HiddenFolders {
		if !filepath.IsAbs(h) {
			return fmt.Errorf("hidden folder %q must be absolute", h)
		}
	}
	for _, h := range c.ServeHosts {
		if !hostRe.MatchString(h) {
			return fmt.Errorf("serve host %q must be a lowercase host name", h)
		}
	}
	for name, p := range map[string]string{"claudePath": c.ClaudePath, "codexPath": c.CodexPath, "opencodePath": c.OpenCodePath} {
		if p != "" && !filepath.IsAbs(p) {
			return fmt.Errorf("%s must be absolute", name)
		}
	}
	if c.Ask.Folder != "" && !filepath.IsAbs(c.Ask.Folder) {
		return fmt.Errorf("ask.folder must be absolute")
	}
	for agent, tiers := range c.Ask.Tiers {
		if !idRe.MatchString(agent) {
			return fmt.Errorf("ask.tiers: %q is not an AI name", agent)
		}
		for tier, m := range tiers {
			switch {
			case !core.ValidTier(tier):
				return fmt.Errorf("ask.tiers.%s: tier %q must be quick, standard or deep", agent, tier)
			case m.Model != "" && !core.ValidAskModel(m.Model):
				return fmt.Errorf("ask.tiers.%s.%s: %q is not a model name", agent, tier, m.Model)
			case m.Effort != "" && !core.ValidAskEffort(m.Effort):
				return fmt.Errorf("ask.tiers.%s.%s: %q is not an effort level", agent, tier, m.Effort)
			}
		}
	}
	return c.Judge.validate()
}

// FolderByID returns the folder with that id.
func (c Config) FolderByID(id string) (Folder, bool) {
	for _, f := range c.Folders {
		if f.ID == id {
			return f, true
		}
	}
	return Folder{}, false
}

func slug(s string) string {
	s = strings.Trim(nonSlugs.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 28 {
		s = strings.TrimRight(s[:28], "-")
	}
	if s == "" {
		s = "folder"
	}
	return s
}

func uniqueID(base string, used map[string]bool) string {
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		id := fmt.Sprintf("%s-%d", base, i)
		if !used[id] {
			return id
		}
	}
}
