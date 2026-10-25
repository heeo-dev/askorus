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
	Status    string `json:"status"` // e.g. busy, blocked
	State     string `json:"state"`  // e.g. working
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

var stoppedWords = map[string]bool{
	"stopped": true, "exited": true, "dead": true, "done": true, "failed": true,
	"killed": true, "completed": true, "terminated": true, "stale": true,
}

// state maps an entry to the list state (design: 실행 규칙).
func (e agentEntry) state() core.State {
	alive := e.PID > 0 && !stoppedWords[strings.ToLower(e.Status)] && !stoppedWords[strings.ToLower(e.State)]
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

func (e agentEntry) activity() string {
	return firstNonEmpty(strings.ToLower(e.Status), strings.ToLower(e.State))
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
