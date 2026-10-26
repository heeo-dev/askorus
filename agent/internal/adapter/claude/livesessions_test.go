package claude

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
)

func TestReadLiveSessions(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) { os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600) }
	write("100.json", `{"pid":100,"sessionId":"`+id1+`","kind":"interactive","procStart":"7","bridgeSessionId":"session_01LIVE"}`)
	write("200.json", `{"pid":200,"sessionId":"`+id2+`","kind":"interactive","bridgeSessionId":"session_01DEAD"}`) // process gone
	write("300.json", `{"pid":300,"sessionId":"`+id5+`","kind":"interactive"}`)                                    // no remote control
	write("400.json", `{"pid":400,"sessionId":"not-a-uuid","bridgeSessionId":"session_01X"}`)
	write("500.json", `{"pid":500,"sessionId":"`+id7+`","bridgeSessionId":"session_bad!/x"}`)
	// Key files may hold secrets and must never be read, whatever they contain.
	write("600.0123abcd.key", `{"pid":600,"sessionId":"`+id6+`","bridgeSessionId":"session_01KEY"}`)
	write("notes.json", `{"pid":700,"sessionId":"`+id6+`","bridgeSessionId":"session_01NOTES"}`)

	var asked []int
	alive := func(pid int, procStart string) bool {
		asked = append(asked, pid)
		return pid != 200
	}
	live := readLiveSessions(dir, alive)
	if got := live[id1].remoteURL(); got != "https://claude.ai/code/session_01LIVE" {
		t.Errorf("live link = %q", got)
	}
	if _, ok := live[id2]; ok {
		t.Error("a dead process counted as live")
	}
	if l, ok := live[id5]; !ok || l.remoteURL() != "" {
		t.Errorf("terminal session without remote control = %+v %v", l, ok)
	}
	if u := live[id7].remoteURL(); u != "" {
		t.Errorf("malformed link accepted: %q", u)
	}
	if _, ok := live[id6]; ok {
		t.Error("read a .key file or a non-<pid>.json file")
	}
	if slices.Contains(asked, 600) || slices.Contains(asked, 700) {
		t.Errorf("looked at files it must skip: %v", asked)
	}
	if readLiveSessions(filepath.Join(dir, "missing"), alive) != nil {
		t.Error("missing folder should give nothing")
	}
}

// A session open in a terminal but missing from claude agents (seen on a
// real PC) must not read as transcript-only: opening it would resume a
// session another process is using.
func TestLiveSessionMissingFromAgents(t *testing.T) {
	e := newLaunchEnv(t, nil) // claude agents lists nothing
	e.a.live = func() map[string]liveSession {
		return map[string]liveSession{id6: {PID: 900, SessionID: id6, Kind: "interactive", Cwd: e.folder}}
	}
	list, _ := e.a.ListSessions(context.Background())
	for _, s := range list.Sessions {
		if s.ID == "claude:"+id6 && s.State != core.StateTerminal {
			t.Errorf("listed as %q, want terminal", s.State)
		}
	}
	res, err := e.a.Open(context.Background(), id6)
	if err != nil || res.Hint != core.HintUseRCInTerminal {
		t.Errorf("open = %+v, %v", res, err)
	}
	if got := e.launches(t); len(got) != 0 {
		t.Errorf("resumed a session open elsewhere: %+v", got)
	}
}

// A terminal session opened with --rc is opened by handing over its link.
func TestOpenTerminalWithRemoteControl(t *testing.T) {
	e := newLaunchEnv(t, []agentEntry{termEntry})
	e.a.live = func() map[string]liveSession {
		return map[string]liveSession{id5: {PID: 200, SessionID: id5, Kind: kindInteractive, BridgeSessionID: "session_01TERMRC"}}
	}
	res, err := e.a.Open(context.Background(), id5)
	if err != nil {
		t.Fatal(err)
	}
	if res.RemoteURL != "https://claude.ai/code/session_01TERMRC" || res.Hint != "" || res.Action != core.ActionNone || !slices.Contains(res.Session.Badges, "rc") {
		t.Errorf("result = %+v", res)
	}
	if got := e.launches(t); len(got) != 0 {
		t.Errorf("ran %+v", got)
	}
	list, _ := e.a.ListSessions(context.Background())
	for _, s := range list.Sessions {
		if s.ID == "claude:"+id5 && (s.RemoteURL != res.RemoteURL || !slices.Contains(s.Badges, "rc") || s.State != core.StateTerminal) {
			t.Errorf("listed as %+v", s)
		}
	}
}
