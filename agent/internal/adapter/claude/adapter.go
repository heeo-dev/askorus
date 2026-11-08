// Package claude is the Claude Code adapter. It is the only package that
// runs claude, reads Claude Code transcripts or knows their formats
// (principle 2). It never reads Claude login files (N10).
package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// Name is the agent name used in session ids ("claude:<uuid>").
const Name = "claude"

// Warning and notice codes. The app turns them into text (N9).
const (
	WarnAgentsUnavailable = "claude_agents_unavailable"
	NoticeNotFound        = "claude_not_found"
	NoticeVersionFailed   = "claude_version_failed"
	NoticeRCDisabledEnv   = "claude_remote_control_disabled_env"
)

// listPromptRunes is how much of the last prompt the list shows.
const listPromptRunes = 80

const versionTTL = 5 * time.Minute

// `claude agents --json --all` takes over a second, so its result is
// reused: fresh results directly, older ones while a refresh runs in the
// background (N5). Actions that change state call Invalidate.
const (
	agentsFresh = 2 * time.Second
	agentsStale = time.Minute
)

// Options configures the adapter.
type Options struct {
	ClaudeDir  string // ~/.claude, or $CLAUDE_CONFIG_DIR
	ClaudePath string // empty: look up claude on PATH
	// DefaultModel and DefaultEffort are passed when a request names none.
	// Empty leaves Claude Code's own defaults.
	DefaultModel  string
	DefaultEffort string
	// AskTiers overrides the ask tier models (settings ask.tiers.claude).
	AskTiers map[string]core.AskModel
}

// Adapter implements core.Adapter for Claude Code.
type Adapter struct {
	runner      *Runner // nil when claude was not found
	runnerErr   error
	transcripts *Transcripts

	mu      sync.Mutex
	version string
	verErr  error
	verAt   time.Time

	fetchMu    sync.Mutex // one agents run at a time
	amu        sync.Mutex
	agentsList []agentEntry
	agentsAt   time.Time // zero: nothing usable cached
	agentsGen  int       // bumped by Invalidate; a fetch started before it is dropped
	refreshing bool

	defaultModel, defaultEffort string
	askTiers                    map[string]core.AskModel

	opMu sync.Mutex
	busy map[string]bool // sessions with an action in progress

	linkWait time.Duration // linkTimeout by default; tests shorten it
	poll     time.Duration

	srv *servers // folder servers (F10)

	// live returns the running session processes and their remote links
	// (~/.claude/sessions/<pid>.json, see livesessions.go).
	live func() map[string]liveSession

	// serviceCheck reports an elevated Claude service (daemon.go).
	serviceCheck func() (elevated bool, pid int)

	// folderExists tells whether a session's folder is still on the PC.
	folderExists func(string) bool
}

// DefaultClaudeDir is $CLAUDE_CONFIG_DIR or ~/.claude.
func DefaultClaudeDir() (string, error) {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// New builds the adapter. A missing claude executable is not an error: the
// list still works from transcripts and /v1/info reports the problem.
func New(opts Options) *Adapter {
	r, err := NewRunner(opts.ClaudePath)
	projects := filepath.Join(opts.ClaudeDir, "projects")
	a := &Adapter{
		runner:        r,
		runnerErr:     err,
		transcripts:   NewTranscripts(projects),
		defaultModel:  opts.DefaultModel,
		defaultEffort: opts.DefaultEffort,
		askTiers:      opts.AskTiers,
	}
	a.srv = &servers{
		exe: func() (string, error) {
			if a.runner == nil {
				return "", ErrNotFound
			}
			return a.runner.Path(), nil
		},
		start:   startPTY,
		pointer: livePointer(projects),
	}
	sessionsDir := filepath.Join(opts.ClaudeDir, "sessions")
	a.live = func() map[string]liveSession { return readLiveSessions(sessionsDir, processAlive) }
	a.serviceCheck = func() (bool, int) { return serviceElevated(opts.ClaudeDir, processAlive, processElevated) }
	a.folderExists = dirExists
	return a
}

// Folder servers (core.Server). See server.go.

func (a *Adapter) StartServer(folder string, opts core.ServeOptions) error {
	if a.runner == nil {
		return core.Fail(core.ErrCodeAgentUnavailable, "claude was not found")
	}
	if !ptySupported() {
		return core.Fail(core.ErrCodeLaunchFailed, "folder servers are only supported on Windows for now")
	}
	return a.srv.StartServer(folder, opts)
}
func (a *Adapter) StopServer(folder string) error              { return a.srv.StopServer(folder) }
func (a *Adapter) ServerStatus(folder string) core.ServeStatus { return a.srv.ServerStatus(folder) }
func (a *Adapter) ServerFolders() []string                     { return a.srv.ServerFolders() }
func (a *Adapter) StopAllServers()                             { a.srv.StopAllServers() }

// OnServerState reports folder server state changes (for the agent log).
// Call it before starting any server.
func (a *Adapter) OnServerState(f func(folder string, st core.ServeStatus)) { a.srv.notify = f }

// Agent returns "claude".
func (a *Adapter) Agent() string { return Name }

// capabilities lists what works here; serve needs a pseudo console.
func (a *Adapter) capabilities() []core.Capability {
	caps := []core.Capability{core.CapList, core.CapPreview} // preview only reads transcripts
	if a.runner != nil {
		caps = append(caps, core.CapStart, core.CapResume, core.CapStop, core.CapRemove, core.CapOpenOfficial, core.CapAsk)
		if ptySupported() {
			caps = append(caps, core.CapServe)
		}
	}
	return caps
}

// Info reports the Claude Code version and what works.
func (a *Adapter) Info(ctx context.Context) core.AdapterInfo {
	info := core.AdapterInfo{Agent: Name, Capabilities: a.capabilities()}
	if a.runner == nil {
		info.Notices = append(info.Notices, NoticeNotFound)
	} else if v, err := a.cachedVersion(ctx); err != nil {
		info.Notices = append(info.Notices, NoticeVersionFailed)
	} else {
		info.Version, info.Available = v, true
	}
	if os.Getenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC") != "" || os.Getenv("DISABLE_GROWTHBOOK") != "" {
		info.Notices = append(info.Notices, NoticeRCDisabledEnv)
	}
	if elevated, _ := a.serviceCheck(); elevated {
		info.Notices = append(info.Notices, NoticeServiceElevated)
	}
	return info
}

func (a *Adapter) cachedVersion(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.verAt.IsZero() && time.Since(a.verAt) < versionTTL {
		return a.version, a.verErr
	}
	// Not cancelled with the phone's request: that would be cached as a
	// broken claude for versionTTL (and asks check it). Run has its own limit.
	a.version, a.verErr = a.runner.Version(context.WithoutCancel(ctx))
	a.verAt = time.Now()
	return a.version, a.verErr
}

// ListSessions merges the transcripts with the live process list. Both are
// read at the same time.
func (a *Adapter) ListSessions(ctx context.Context) (core.ListResult, error) {
	type agentsResult struct {
		entries []agentEntry
		ok      bool
	}
	ch := make(chan agentsResult, 1)
	go func() {
		e, ok := a.agents(ctx)
		ch <- agentsResult{e, ok}
	}()
	ts, err := a.transcripts.Scan()
	ar := <-ch
	if err != nil {
		return core.ListResult{}, err
	}
	entries, agentsOK := ar.entries, ar.ok
	live := a.live()
	if agentsOK {
		entries = withLive(entries, live)
	}
	res := core.ListResult{Sessions: merge(ts, entries, agentsOK)}
	markServerSessions(res.Sessions, a.srv.serverSessions(entries, processParents))
	applyLive(res.Sessions, live)
	markMissingFolders(res.Sessions, a.folderExists)
	if !agentsOK {
		res.Warnings = append(res.Warnings, WarnAgentsUnavailable)
	}
	return res, nil
}

// agents returns the process list, from the cache when it is recent enough.
func (a *Adapter) agents(ctx context.Context) ([]agentEntry, bool) {
	if a.runner == nil {
		return nil, false
	}
	a.amu.Lock()
	age := time.Since(a.agentsAt)
	cached := !a.agentsAt.IsZero()
	switch {
	case cached && age < agentsFresh:
		list := a.agentsList
		a.amu.Unlock()
		return list, true
	case cached && age < agentsStale:
		list := a.agentsList
		if !a.refreshing {
			a.refreshing = true
			go func() {
				a.fetchAgents(context.Background())
				a.amu.Lock()
				a.refreshing = false
				a.amu.Unlock()
			}()
		}
		a.amu.Unlock()
		return list, true
	}
	a.amu.Unlock()
	return a.fetchAgents(ctx)
}

func (a *Adapter) fetchAgents(ctx context.Context) ([]agentEntry, bool) {
	a.fetchMu.Lock()
	defer a.fetchMu.Unlock()
	a.amu.Lock()
	if !a.agentsAt.IsZero() && time.Since(a.agentsAt) < agentsFresh {
		list := a.agentsList // another caller just fetched
		a.amu.Unlock()
		return list, true
	}
	gen := a.agentsGen
	a.amu.Unlock()
	start := time.Now()
	list, err := a.runner.Agents(ctx)
	a.amu.Lock()
	defer a.amu.Unlock()
	if err != nil {
		a.agentsList, a.agentsAt = nil, time.Time{}
		return nil, false
	}
	if gen == a.agentsGen {
		a.agentsList, a.agentsAt = list, start
	}
	return list, true
}

// Invalidate drops the cached process list, so the next list call runs
// claude agents again.
func (a *Adapter) Invalidate() {
	a.amu.Lock()
	a.agentsList, a.agentsAt = nil, time.Time{}
	a.agentsGen++
	a.amu.Unlock()
}

// Warm fills the caches so the first request from the phone is fast.
func (a *Adapter) Warm(ctx context.Context) {
	_, _ = a.ListSessions(ctx)
	_ = a.Info(ctx)
}

// merge builds the session list. Without the process list every state is
// unknown, because a "history" guess could make the app resume a session
// that is open elsewhere.
// applyLive takes the current remote link from the running process when
// remote control is on. A terminal session with it on gets an "rc" badge:
// the app can open it directly instead of asking for /rc.
func applyLive(ss []core.Session, live map[string]liveSession) {
	if len(live) == 0 {
		return
	}
	for i := range ss {
		_, native, _ := core.ParseID(ss[i].ID)
		u := live[native].remoteURL()
		if u == "" {
			continue
		}
		ss[i].RemoteURL = u
		if ss[i].State == core.StateTerminal {
			ss[i].Badges = append(ss[i].Badges, "rc")
		}
	}
}

// markServerSessions shows sessions a folder server runs as running with a
// "server" badge: claude lists them as interactive, but nobody types in a
// terminal there; they are reached through the official app.
func markServerSessions(ss []core.Session, server map[string]bool) {
	if len(server) == 0 {
		return
	}
	for i := range ss {
		_, native, _ := core.ParseID(ss[i].ID)
		if server[native] {
			ss[i].State = core.StateRunning
			ss[i].AttachCommand = ""
			ss[i].Badges = append(ss[i].Badges, "server")
		}
	}
}

// markMissingFolders flags stopped and history sessions whose folder is
// gone: resuming them would fail, so they offer no open mode. Each folder
// is checked once per list.
func markMissingFolders(ss []core.Session, exists func(string) bool) {
	seen := map[string]bool{}
	for i := range ss {
		s := &ss[i]
		if (s.State != core.StateStopped && s.State != core.StateHistory) || s.Folder == "" {
			continue
		}
		ok, done := seen[s.Folder]
		if !done {
			ok = exists(s.Folder)
			seen[s.Folder] = ok
		}
		if !ok {
			s.Badges = append(s.Badges, core.BadgeFolderMissing)
			s.OpenModes = []string{}
			s.AttachCommand = ""
		}
	}
}

func merge(ts []Transcript, entries []agentEntry, agentsOK bool) []core.Session {
	best := map[string]agentEntry{}
	for _, e := range entries {
		if !ValidUUID(e.SessionID) {
			continue
		}
		if old, ok := best[e.SessionID]; !ok || stateRank(e.state()) > stateRank(old.state()) {
			best[e.SessionID] = e
		}
	}
	out := make([]core.Session, 0, len(ts)+len(best))
	seen := map[string]bool{}
	for _, t := range ts {
		e, hasEntry := best[t.SessionID]
		out = append(out, build(t, e, hasEntry, agentsOK))
		seen[t.SessionID] = true
	}
	for id, e := range best {
		if seen[id] {
			continue
		}
		t := Transcript{SessionID: id, Cwd: e.Cwd}
		if e.StartedAt > 0 {
			t.LastActive = time.UnixMilli(e.StartedAt).UTC()
		}
		out = append(out, build(t, e, true, agentsOK))
	}
	return out
}

func build(t Transcript, e agentEntry, hasEntry, agentsOK bool) core.Session {
	s := core.Session{
		ID:         core.MakeID(Name, t.SessionID),
		Agent:      Name,
		Title:      firstNonEmpty(t.Title, e.Name),
		Name:       e.Name,
		Folder:     firstNonEmpty(t.Cwd, e.Cwd),
		LastActive: t.LastActive,
		RemoteURL:  t.RemoteURL,
		Model:      t.Model,
		LastPrompt: clip(t.LastPrompt, listPromptRunes),
		SearchText: t.LastPrompt,
		OpenModes:  []string{core.OpenModeOfficial},
	}
	switch {
	case !agentsOK:
		s.State = core.StateUnknown
	case hasEntry:
		s.State = e.state()
		if s.State == core.StateRunning || s.State == core.StateTerminal {
			s.Activity = e.activity()
		}
	default:
		s.State = core.StateHistory
	}
	switch s.State {
	case core.StateRunning, core.StateStopped:
		s.AttachCommand = "claude attach " + quoteArg(firstNonEmpty(e.Name, e.ID, t.SessionID))
	case core.StateHistory:
		s.AttachCommand = "claude --resume " + t.SessionID
	}
	if t.Entrypoint == "claude-desktop" {
		s.Badges = append(s.Badges, "desktop")
	}
	return s
}

// quoteArg quotes a value for display in a copyable command line.
func quoteArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'&|<>^%!;()$`") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
