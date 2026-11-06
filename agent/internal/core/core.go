// Package core holds the agent-neutral types shared by the API, storage and
// adapters. Nothing in this package knows about a specific AI (principle 2).
package core

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// State is where a session is right now, as the phone app shows it.
type State string

const (
	StateRunning  State = "running"  // background session with a live process
	StateStopped  State = "stopped"  // background session that can be woken up
	StateTerminal State = "terminal" // open in another terminal on the PC
	StateHistory  State = "history"  // only the transcript exists
	StateUnknown  State = "unknown"  // the live process list could not be read
)

// ValidState reports whether s is one of the states above.
func ValidState(s State) bool {
	switch s {
	case StateRunning, StateStopped, StateTerminal, StateHistory, StateUnknown:
		return true
	}
	return false
}

// Capability is something an adapter can do. The app draws buttons only
// for capabilities that are announced (principle 3).
type Capability string

const (
	CapList         Capability = "list"
	CapStart        Capability = "start"
	CapResume       Capability = "resume"
	CapStop         Capability = "stop"
	CapRemove       Capability = "remove"
	CapOpenOfficial Capability = "open_official"
	CapServe        Capability = "serve"
	CapPreview      Capability = "preview" // F8: the last question and answer, read only
	CapAsk          Capability = "ask"
	CapTUI          Capability = "tui"
	CapChat         Capability = "chat"
	// CapSynthesize: the AI writes combined answers (종합 답변, P2-3 S5b).
	// It is announced for an AI that passes the gate (compares.Gate: its
	// CLI is at least the version the one-off command was tested with,
	// combined answers are on and the judge folder exists) and can ask: the
	// writer runs its ask tier model with the locked-down one-off command.
	// The capability judge went with the judging (C6d, #24).
	CapSynthesize Capability = "synthesize"
)

// AllCapabilities lists every name the API knows, implemented or not.
var AllCapabilities = []Capability{
	CapList, CapStart, CapResume, CapStop, CapRemove,
	CapOpenOfficial, CapServe, CapPreview, CapAsk, CapTUI, CapChat, CapSynthesize,
}

// OpenModeOfficial opens a session in the AI's official app (principle 6).
const OpenModeOfficial = "official"

// Session is one entry of the session list. Fields may be added inside /v1
// but never removed or renamed (principle 4).
type Session struct {
	ID            string    `json:"id"`    // "<agent>:<native id>"
	Agent         string    `json:"agent"` // e.g. "claude"
	Title         string    `json:"title"`
	Name          string    `json:"name,omitempty"`
	Folder        string    `json:"folder,omitempty"`
	LastActive    time.Time `json:"lastActive"`
	State         State     `json:"state"`
	Activity      string    `json:"activity,omitempty"` // adapter sub-state such as busy or blocked
	RemoteURL     string    `json:"remoteUrl,omitempty"`
	Model         string    `json:"model,omitempty"`
	LastPrompt    string    `json:"lastPrompt,omitempty"` // first few words only
	AttachCommand string    `json:"attachCommand,omitempty"`
	Badges        []string  `json:"badges,omitempty"`
	OpenModes     []string  `json:"openModes"`

	// SearchText is matched by the q filter and never sent to the app.
	SearchText string `json:"-"`
}

// AdapterInfo describes one adapter for GET /v1/info.
type AdapterInfo struct {
	Agent        string       `json:"agent"`
	Version      string       `json:"version,omitempty"`
	Available    bool         `json:"available"`
	Capabilities []Capability `json:"capabilities"`
	Notices      []string     `json:"notices,omitempty"` // codes the app turns into text
	Ask          *AskInfo     `json:"ask,omitempty"`     // set with the ask capability
}

// AskInfo tells the app which model each ask tier uses (P2-2).
type AskInfo struct {
	Tiers map[string]AskModel `json:"tiers"`
	// Efforts lists, for each model of Tiers, the efforts the person may
	// pick with it (11-06: the app's own pick of model and effort), least
	// to most thinking. Absent when the adapter does not say.
	Efforts map[string][]string `json:"efforts,omitempty"`
}

// ListResult is what an adapter returns for the session list.
type ListResult struct {
	Sessions []Session
	Warnings []string // codes, e.g. "agents_unavailable"
}

// Adapter is the only way the core talks to an AI.
type Adapter interface {
	Agent() string
	Info(ctx context.Context) AdapterInfo
	ListSessions(ctx context.Context) (ListResult, error)
}

var agentRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// ErrBadID is returned for session ids that are not "<agent>:<native id>".
var ErrBadID = errors.New("bad session id")

// MakeID joins an agent name and its native session id (principle 1).
func MakeID(agent, native string) string { return agent + ":" + native }

// ParseID splits "<agent>:<native id>". Adapters still validate the native part.
func ParseID(id string) (agent, native string, err error) {
	agent, native, ok := strings.Cut(id, ":")
	if !ok || !agentRe.MatchString(agent) || native == "" || len(native) > 128 {
		return "", "", ErrBadID
	}
	return agent, native, nil
}

// Filter narrows the session list (F2).
type Filter struct {
	Query  string  // matched against title, name and search text
	Folder string  // exact folder, or a parent of it
	States []State // empty means all
	Agent  string  // empty means all
	Hidden []string
	// HiddenIDs are sessions the person removed from the list (F5).
	HiddenIDs map[string]bool
}

// Apply returns the matching sessions, most recent first.
func (f Filter) Apply(in []Session) []Session {
	q := strings.ToLower(strings.TrimSpace(f.Query))
	out := make([]Session, 0, len(in))
	for _, s := range in {
		if f.Agent != "" && s.Agent != f.Agent {
			continue
		}
		if len(f.States) > 0 && !containsState(f.States, s.State) {
			continue
		}
		if f.Folder != "" && !InFolder(s.Folder, f.Folder) {
			continue
		}
		if hidden(s.Folder, f.Hidden) || f.HiddenIDs[s.ID] {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(s.Title+"\n"+s.Name+"\n"+s.SearchText), q) {
			continue
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastActive.After(out[j].LastActive) })
	return out
}

func containsState(list []State, s State) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func hidden(folder string, list []string) bool {
	for _, h := range list {
		if InFolder(folder, h) {
			return true
		}
	}
	return false
}

// InFolder reports whether path is dir or inside it. Windows paths compare
// without case.
func InFolder(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	p, d := filepath.Clean(path), filepath.Clean(dir)
	if runtime.GOOS == "windows" {
		p, d = strings.ToLower(p), strings.ToLower(d)
	}
	if p == d {
		return true
	}
	if !strings.HasSuffix(d, string(filepath.Separator)) {
		d += string(filepath.Separator)
	}
	return strings.HasPrefix(p, d)
}

// VersionAtLeast reports whether version have is min or newer. Both are
// dotted numbers such as "2.1.314"; a missing part counts as 0, so "2.1"
// is "2.1.0". It is false when have or min is not dotted numbers ("",
// "v2", "2.1.0-beta"): a version the agent cannot read never passes a
// version gate.
func VersionAtLeast(have, min string) bool {
	h, ok := versionParts(have)
	if !ok {
		return false
	}
	m, ok := versionParts(min)
	if !ok {
		return false
	}
	for i := 0; i < max(len(h), len(m)); i++ {
		var a, b int
		if i < len(h) {
			a = h[i]
		}
		if i < len(m) {
			b = m[i]
		}
		if a != b {
			return a > b
		}
	}
	return true
}

// versionParts reads "2.1.314" as [2 1 314]: digits only, no part empty.
func versionParts(v string) ([]int, bool) {
	if v == "" {
		return nil, false
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		if p == "" || len(p) > 9 || strings.Trim(p, "0123456789") != "" {
			return nil, false
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}
