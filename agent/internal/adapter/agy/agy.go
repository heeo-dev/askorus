// Package agy is the Antigravity CLI adapter (#20, 0.5.0). Phase 2 uses it
// for asks and combined answers. agy has no option that takes its tools
// away and runs, by the person's own settings, in Auto mode with every
// tool approved, so an ask is locked twice and neither lock leans on the
// other (docs/phase2-plan.md 4, '#20'):
//
//   - an agent file of the agent's own in the folder agy runs in
//     (.agents/agents/<name>.md), whose tool list is search_web only, run
//     with --agent;
//   - deny rules in --settings, which apply to that run only and win over
//     any allow rule of the person: commands, file reads and writes, the
//     browser, downloads and MCP tools.
//
// The question goes on stdin as one stream-json message, never as an
// argument. The adapter checks every run's start event (its agent and
// permission mode) and throws an answer away when agy reached for a tool
// the agent file does not list. It is the only package that runs agy or
// knows its formats (principle 2), and it never reads agy's sign-in
// (N10); whether agy is signed in comes from its /usage command
// (login.go). agy keeps every run as a conversation and has no command to
// delete one; Askorus leaves them (2026-11-18 decision 3). Session listing
// comes in Phase 3.
package agy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

// Name is the agent name used in session ids ("agy:<conversation id>").
const Name = "agy"

// Notice codes. The app turns them into text (N9).
const (
	NoticeNotFound      = "agy_not_found"
	NoticeVersionFailed = "agy_version_failed"
)

const versionTTL = 5 * time.Minute

// defaultTiers are the 2026-11-18 tier models (decision 4). agy names a
// Gemini model together with its thinking level ("gemini-3.8-flash-low");
// a tier keeps them apart, as the app shows them, and cliModel joins them.
var defaultTiers = map[string]core.AskModel{
	core.TierQuick:    {Model: "gemini-3.8-flash", Effort: "low"},
	core.TierStandard: {Model: "gemini-3.8-flash", Effort: "medium"},
	core.TierDeep:     {Model: "gemini-3.8-flash", Effort: "high"},
}

// modelEfforts are the levels each known model takes, as `agy models`
// listed them on 2026-11-18.
var modelEfforts = map[string][]string{
	"gemini-3.8-flash": {"low", "medium", "high"},
}

// efforts are the levels agy names (--effort's list); a model that does
// not take one is refused by agy itself (bad_model).
var efforts = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true, "max": true}

var (
	// modelRe: Gemini models only (decision 4: the Claude and GPT-OSS
	// models agy also offers are left out), in agy's spelling.
	modelRe        = regexp.MustCompile(`^gemini-[a-z0-9][a-z0-9.-]{0,62}$`)
	conversationRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	versionRe      = regexp.MustCompile(`\d+\.\d+\.\d+`)
)

// cliModel is the model agy is given: the model and its level joined
// ("gemini-3.8-flash" and "low" make "gemini-3.8-flash-low"), or the model
// alone when it already names its level. agy refuses --effort with such a
// model, so the adapter never passes --effort.
func cliModel(model, effort string) string {
	if effort == "" {
		return model
	}
	return model + "-" + effort
}

// AskEfforts lists the levels each tier model may take (core.EffortLister):
// the model list's, else the table's.
func (a *Adapter) AskEfforts() map[string][]string {
	return core.TierEfforts(a.AskTiers(), func(m string) []string {
		if es := a.follow.Efforts(m); len(es) > 0 {
			return es
		}
		return modelEfforts[m]
	})
}

// Options configures the adapter.
type Options struct {
	Path  string                   // empty: agy.exe on PATH, else agy's own install folder
	Tiers map[string]core.AskModel // settings ask.tiers.agy
	// ConfigDir is agy's config folder, where it keeps hooks, plugins and
	// agents of the person's own; empty: ~/.gemini/config. Tests give one.
	ConfigDir string
	// BrainDir is where agy keeps each conversation's files, among them the
	// pictures it makes (#14); empty: ~/.gemini/antigravity-cli/brain.
	// Tests give one.
	BrainDir string
	// Follow lets the tier models follow the newest model of their line
	// (models.go, #64), as codex.Options.Follow.
	Follow bool
}

// Adapter implements core.Adapter and core.Asker for Antigravity CLI.
type Adapter struct {
	exe    string
	exeErr error
	tiers  map[string]core.AskModel

	mu      sync.Mutex
	version string
	verErr  error
	verAt   time.Time

	// The sign-in state and usage (login.go). loginCheck is nil outside
	// tests.
	loginMu    sync.Mutex
	login      loginState
	loginAt    time.Time
	loginCheck func(context.Context) loginState

	// configDir is agy's config folder (Options.ConfigDir); safe is
	// photosSafe's answer, kept for a minute.
	configDir string
	safeMu    sync.Mutex
	safe      bool
	safeAt    time.Time

	// The agent files (agentFile): fileMu guards ready, the files this
	// process wrote or found right, and writes (for tests).
	fileMu sync.Mutex
	ready  map[string]bool
	writes int

	// follow keeps agy's model list (models.go, #64).
	follow *core.Follower

	// Made pictures (made.go, #14): brainDir is agy's conversation folder
	// ("" when unknown: no pictures); drawConvs the conversations started
	// with a draw agent and picLimit when the picture limit refreshes, both
	// under drawMu. clock is nil outside tests.
	brainDir  string
	drawMu    sync.Mutex
	drawConvs map[string]bool
	picLimit  time.Time
	clock     func() time.Time
}

// New finds agy. A missing agy is not an error: /v1/info reports it.
func New(o Options) *Adapter {
	a := &Adapter{tiers: o.Tiers, ready: map[string]bool{}, configDir: o.ConfigDir, brainDir: brainFolder(o.BrainDir)}
	if a.configDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			a.configDir = filepath.Join(home, ".gemini", "config")
		}
	}
	a.exe, a.exeErr = findExe(o.Path)
	a.follow = &core.Follower{Lines: lines}
	if o.Follow {
		a.follow.List = a.listModels
	}
	return a
}

// findExe returns the real agy executable: the setting, agy(.exe) on PATH,
// or on Windows the folder agy's installer uses
// (%LOCALAPPDATA%\agy\bin\agy.exe), which it does not always put on PATH.
// agy ships an exe, never a script (P0-7).
func findExe(override string) (string, error) {
	if override != "" {
		return override, proc.CheckExe(override)
	}
	exe, _, err := proc.LookExe("agy")
	if err == nil {
		return exe, nil
	}
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" && filepath.IsAbs(base) {
			p := filepath.Join(base, "agy", "bin", "agy.exe")
			if proc.CheckExe(p) == nil {
				return p, nil
			}
		}
	}
	return "", err
}

// Agent returns "agy".
func (a *Adapter) Agent() string { return Name }

// Info reports the version; asking is the only capability for now.
func (a *Adapter) Info(ctx context.Context) core.AdapterInfo {
	info := core.AdapterInfo{Agent: Name, Capabilities: []core.Capability{}}
	switch {
	case a.exeErr != nil:
		info.Notices = append(info.Notices, NoticeNotFound)
	default:
		if v, err := a.cachedVersion(ctx); err != nil {
			info.Notices = append(info.Notices, NoticeVersionFailed)
		} else {
			info.Version, info.Available = v, true
			info.Capabilities = append(info.Capabilities, core.CapAsk)
			a.follow.Check(v)
			a.loginNotices(ctx, &info)
		}
	}
	return info
}

func (a *Adapter) cachedVersion(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.verAt.IsZero() && time.Since(a.verAt) < versionTTL {
		return a.version, a.verErr
	}
	// A phone request that goes away must not be cached as a broken AI.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	a.version, a.verErr = "", nil
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.exe, Dir: filepath.Dir(a.exe), Args: []string{"--version"}})
	switch v := versionRe.Find(res.Stdout); {
	case err != nil:
		a.verErr = err
	case res.ExitCode != 0 || v == nil:
		a.verErr = errors.New("agy --version: no version number")
	default:
		a.version = string(v)
	}
	a.verAt = time.Now()
	return a.version, a.verErr
}

// ListSessions lists nothing yet: agy conversations come in Phase 3.
func (a *Adapter) ListSessions(context.Context) (core.ListResult, error) {
	return core.ListResult{Sessions: []core.Session{}}, nil
}

// AskTiers returns the tier models, with the settings' overrides: a model
// of the settings stays as written, any other follows its line (#64).
func (a *Adapter) AskTiers() map[string]core.AskModel {
	out := make(map[string]core.AskModel, len(defaultTiers))
	for t, m := range defaultTiers {
		own := false
		if o, ok := a.tiers[t]; ok {
			if o.Model != "" {
				m.Model, own = o.Model, true
				m.Effort = o.Effort // a model of its own brings its own level, or none
			}
			if o.Effort != "" {
				m.Effort = o.Effort
			}
		}
		if !own {
			m.Model = a.follow.Newest(m.Model, m.Effort)
		}
		out[t] = m
	}
	return out
}

// DisplayName is "Antigravity", as a combined answer names its answers
// (core.Named).
func (a *Adapter) DisplayName() string { return "Antigravity" }

// clip collapses whitespace and keeps at most n runes.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
