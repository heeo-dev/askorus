package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
)

// Claude Code keeps a small file per running session process in
// ~/.claude/sessions/<pid>.json. When remote control is on, it names the
// remote session ("bridgeSessionId": "session_…"); a terminal session
// opened without --rc has none. The same folder holds <pid>.<hash>.key
// files, which may be secrets: they are never opened (N10).

// liveFileRe matches the only files this reads.
var liveFileRe = regexp.MustCompile(`^[0-9]{1,10}\.json$`)

const maxLiveFile = 64 << 10

type liveSession struct {
	PID             int    `json:"pid"`
	SessionID       string `json:"sessionId"`
	Kind            string `json:"kind"` // "bg" or "interactive"
	Cwd             string `json:"cwd"`
	ProcStart       string `json:"procStart"`
	BridgeSessionID string `json:"bridgeSessionId"`
}

// withLive adds the running processes claude agents left out. Seen on a
// real PC (2026-10-26): two terminal sessions open with --rc, one of them
// started from an administrator terminal, were missing from
// `claude agents --json --all` while their process files were there. Left
// out, they read as transcript-only, and opening one would resume a
// session another process is using.
func withLive(entries []agentEntry, live map[string]liveSession) []agentEntry {
	for id, l := range live {
		if e, ok := bestEntry(entries, id); ok && e.PID > 0 {
			continue
		}
		kind := kindInteractive
		if l.Kind == "bg" {
			kind = kindBackground
		}
		entries = append(entries, agentEntry{PID: l.PID, Kind: kind, SessionID: id, Cwd: l.Cwd})
	}
	return entries
}

// remoteURL is the official link of a live remote session, or "".
func (l liveSession) remoteURL() string {
	u := "https://claude.ai/code/" + l.BridgeSessionID
	if l.BridgeSessionID == "" || !isRemoteURL(u) {
		return ""
	}
	return u
}

// readLiveSessions returns the sessions whose process still runs, by
// session id. A file left behind by a crashed process is ignored.
func readLiveSessions(dir string, alive func(pid int, procStart string) bool) map[string]liveSession {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := map[string]liveSession{}
	for _, e := range entries {
		if e.IsDir() || !liveFileRe.MatchString(e.Name()) {
			continue
		}
		if info, err := e.Info(); err != nil || info.Size() > maxLiveFile {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var l liveSession
		if json.Unmarshal(b, &l) != nil || l.PID <= 0 || !ValidUUID(l.SessionID) {
			continue
		}
		if !alive(l.PID, l.ProcStart) {
			continue
		}
		out[l.SessionID] = l
	}
	return out
}
