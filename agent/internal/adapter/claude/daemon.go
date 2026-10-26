package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// The Claude background service (`claude daemon`) takes over the rights
// of whoever started it first. Started from an administrator terminal, a
// normal-rights agent can neither see it, use it nor stop it, and a new
// `--bg` fails after ~45 s on the lock (구현 전 테스트 6). The agent reads
// ~/.claude/daemon.lock to notice this early and tell the person to stop it
// from an administrator terminal (`claude daemon stop --any`).

// NoticeServiceElevated: the Claude service runs as administrator.
const NoticeServiceElevated = "claude_service_elevated"

// ElevatedServiceHint is what the person has to do.
const ElevatedServiceHint = "the Claude background service runs as administrator; in an administrator terminal run: claude daemon stop --any"

type daemonLock struct {
	PID         int         `json:"pid"`
	ProcStartFt json.Number `json:"procStartFt"`
}

// serviceElevated reports whether the running Claude service is elevated
// and its pid. A missing or stale lock means no service runs.
func serviceElevated(claudeDir string, alive func(int, string) bool, elevated func(int) (bool, bool)) (bool, int) {
	b, err := os.ReadFile(filepath.Join(claudeDir, "daemon.lock"))
	if err != nil || len(b) > 64<<10 {
		return false, 0
	}
	var l daemonLock
	if json.Unmarshal(b, &l) != nil || l.PID <= 0 {
		return false, 0
	}
	if !alive(l.PID, l.ProcStartFt.String()) {
		return false, 0
	}
	if e, known := elevated(l.PID); known && e {
		return true, l.PID
	}
	return false, l.PID
}

// checkService fails fast when the Claude service is elevated, instead of
// letting `claude --bg` hang on the lock.
func (a *Adapter) checkService() error {
	if a.serviceCheck == nil {
		return nil
	}
	if elevated, pid := a.serviceCheck(); elevated {
		return core.Fail(core.ErrCodeServiceElevated, ElevatedServiceHint+" (pid "+strconv.Itoa(pid)+")")
	}
	return nil
}

// SelfElevated reports whether the agent itself runs as administrator.
// The agent refuses to serve then (N3): every session it starts would run
// with full rights.
func SelfElevated() bool { return selfElevated() }
