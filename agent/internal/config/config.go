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
	// KeepAwake stops the PC from sleeping while a background session or a
	// folder server runs (design: 절전). Off by default.
	KeepAwake bool `json:"keepAwake,omitempty"`
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

// Dir returns the agent directory: $ASKSEMBLE_AGENT_DIR, or
// <user config dir>/Asksemble.
func Dir() (string, error) {
	if d := os.Getenv("ASKSEMBLE_AGENT_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Asksemble"), nil
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
	if c.ClaudePath != "" && !filepath.IsAbs(c.ClaudePath) {
		return fmt.Errorf("claudePath must be absolute")
	}
	return nil
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
