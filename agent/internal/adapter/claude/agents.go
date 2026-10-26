package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// agentEntry is one item of `claude agents --json --all`. Unknown fields
// are ignored.
type agentEntry struct {
	PID       int    `json:"pid"`
	ID        string `json:"id"` // short id, e.g. "cf6efa4e"
	Cwd       string `json:"cwd"`
	Kind      string `json:"kind"` // "background" or "interactive"
	StartedAt int64  `json:"startedAt"`
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
	Status    string `json:"status"` // busy, idle; absent once stopped
	State     string `json:"state"`  // working, blocked (waiting for input), stopped
}

const (
	kindBackground  = "background"
	kindInteractive = "interactive"
)

func parseAgents(b []byte) ([]agentEntry, error) {
	b = bytes.TrimSpace(b)
	switch {
	case len(b) == 0:
		return nil, nil
	case b[0] == '[':
		var list []agentEntry
		err := json.Unmarshal(b, &list)
		return list, err
	case b[0] == '{':
		var wrap struct {
			Agents []agentEntry `json:"agents"`
		}
		err := json.Unmarshal(b, &wrap)
		return wrap.Agents, err
	}
	return nil, errors.New("claude agents --json: unexpected output")
}

// state maps an entry to the list state (design: 실행 규칙). A process id
// alone decides: a stopped entry has none (state "stopped"), while a live
// one may say "done" after finishing a reply. Reading "done" as stopped
// once made the agent resume a live session, which forks a copy, so state
// words never count as stopped.
func (e agentEntry) state() core.State {
	alive := e.PID > 0
	switch {
	case e.Kind == kindBackground && alive:
		return core.StateRunning
	case e.Kind == kindBackground:
		return core.StateStopped
	case alive:
		return core.StateTerminal
	}
	return core.StateHistory
}

// activity prefers state: "blocked" tells the app the session waits for
// input, which status ("idle") does not.
func (e agentEntry) activity() string {
	return firstNonEmpty(strings.ToLower(e.State), strings.ToLower(e.Status))
}

// rank orders entries of the same session: a live process wins.
func stateRank(s core.State) int {
	switch s {
	case core.StateRunning:
		return 4
	case core.StateTerminal:
		return 3
	case core.StateStopped:
		return 2
	case core.StateHistory:
		return 1
	}
	return 0
}
