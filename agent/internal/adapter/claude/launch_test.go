package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
)

type launchEnv struct {
	a        *Adapter
	argsFile string
	state    string
	projects string
	folder   string // a real folder sessions run in
}

// newLaunchEnv sets up a fake claude with state, fake transcripts whose
// folders exist, and short timeouts.
func newLaunchEnv(t *testing.T, entries []agentEntry) launchEnv {
	t.Helper()
	r, argsFile := fakeRunner(t)
	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	folder := filepath.Join(root, "work")
	os.MkdirAll(folder, 0o700)
	os.MkdirAll(filepath.Join(projects, "work"), 0o700)
	for _, id := range []string{id1, id2, id5, id6} {
		line := `{"type":"user","message":{"content":"x"},"timestamp":"2026-10-20T00:00:00.000Z","cwd":` + jsonString(folder) + `,"sessionId":"` + id + `"}` + "\n" +
			`{"type":"ai-title","aiTitle":"Title ` + id[:4] + `","sessionId":"` + id + `"}` + "\n"
		if id == id1 {
			line += `{"type":"system","subtype":"bridge_status","url":"https://claude.ai/code/session_old1","timestamp":"2026-10-20T00:00:01.000Z","sessionId":"` + id + `"}` + "\n"
		}
		os.WriteFile(filepath.Join(projects, "work", id+".jsonl"), []byte(line), 0o600)
	}
	if entries == nil {
		entries = []agentEntry{}
	}
	for i := range entries {
		entries[i].Cwd = folder
	}
	state := filepath.Join(root, "state.json")
	b, _ := json.Marshal(entries)
	os.WriteFile(state, b, 0o600)
	t.Setenv("ASKSEMBLE_FAKE_STATE", state)
	t.Setenv("ASKSEMBLE_FAKE_PROJECTS", projects)
	t.Setenv("ASKSEMBLE_FAKE_UNTRUSTED", "")
	t.Setenv("ASKSEMBLE_FAKE_NOLINK", "")
	a := New(Options{ClaudeDir: root, ClaudePath: r.Path()})
	a.linkWait, a.poll = 3*time.Second, 20*time.Millisecond
	return launchEnv{a: a, argsFile: argsFile, state: state, projects: projects, folder: folder}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// launches returns the --bg / stop / rm calls the fake received.
func (e launchEnv) launches(t *testing.T) []fakeCall {
	t.Helper()
	var out []fakeCall
	for _, c := range recordedCalls(t, e.argsFile) {
		if len(c.Args) > 0 && (c.Args[0] == "--bg" || c.Args[0] == "stop" || c.Args[0] == "rm") {
			out = append(out, c)
		}
	}
	return out
}

var (
	runningEntry = agentEntry{PID: 100, ID: "11111111", Kind: kindBackground, SessionID: id1, Name: "job one", Status: "idle", State: "blocked"}
	stoppedEntry = agentEntry{ID: "22222222", Kind: kindBackground, SessionID: id2, Name: "job two", State: "stopped"}
	termEntry    = agentEntry{PID: 200, Kind: kindInteractive, SessionID: id5, Status: "idle"}
)

func TestOpenRunningRunsNothing(t *testing.T) {
	e := newLaunchEnv(t, []agentEntry{runningEntry})
	res, err := e.a.Open(context.Background(), id1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != core.ActionNone || res.RemoteURL != "https://claude.ai/code/session_old1" || res.Session.State != core.StateRunning {
		t.Errorf("result = %+v", res)
	}
	if got := e.launches(t); len(got) != 0 {
		t.Errorf("ran %v for a running session", got)
	}
}

func TestOpenTerminalGivesHint(t *testing.T) {
	e := newLaunchEnv(t, []agentEntry{termEntry})
	res, err := e.a.Open(context.Background(), id5)
	if err != nil {
		t.Fatal(err)
	}
	if res.Hint != core.HintUseRCInTerminal || res.Action != core.ActionNone || res.RemoteURL != "" {
		t.Errorf("result = %+v", res)
	}
	if got := e.launches(t); len(got) != 0 {
		t.Errorf("ran %v for a terminal session", got)
	}
}

// A stopped background session is resumed without any flag (flags fork a copy).
func TestOpenStoppedResumesWithoutFlags(t *testing.T) {
	e := newLaunchEnv(t, []agentEntry{stoppedEntry})
	e.a.defaultModel, e.a.defaultEffort = "opus", "high" // must not be used here
	res, err := e.a.Open(context.Background(), id2)
	if err != nil {
		t.Fatal(err)
	}
	got := e.launches(t)
	if len(got) != 1 || !slices.Equal(got[0].Args, []string{"--bg", "--resume", id2}) {
		t.Fatalf("calls = %+v", got)
	}
	if got[0].Cwd != e.folder {
		t.Errorf("ran in %q, want the session folder %q", got[0].Cwd, e.folder)
	}
	if res.Action != core.ActionResumed || !strings.HasPrefix(res.RemoteURL, "https://claude.ai/code/session_") || res.Session.State != core.StateRunning {
		t.Errorf("result = %+v", res)
	}
}

// A transcript-only session is resumed with remote control, its title and
// the defaults (only when set).
func TestOpenHistoryResumesWithRemoteControl(t *testing.T) {
	e := newLaunchEnv(t, nil)
	if _, err := e.a.Open(context.Background(), id6); err != nil {
		t.Fatal(err)
	}
	e.a.defaultModel, e.a.defaultEffort = "opus", "high"
	if _, err := e.a.Open(context.Background(), id5); err != nil {
		t.Fatal(err)
	}
	got := e.launches(t)
	want0 := []string{"--bg", "--resume", id6, "--remote-control", "-n", "Title 6666"}
	want1 := []string{"--bg", "--resume", id5, "--remote-control", "-n", "Title 5555", "--model", "opus", "--effort", "high"}
	if len(got) != 2 || !slices.Equal(got[0].Args, want0) || !slices.Equal(got[1].Args, want1) {
		t.Fatalf("calls = %+v", got)
	}
}

func TestOpenUntrustedFolder(t *testing.T) {
	e := newLaunchEnv(t, nil)
	t.Setenv("ASKSEMBLE_FAKE_UNTRUSTED", "1")
	_, err := e.a.Open(context.Background(), id6)
	if core.ErrorCode(err) != core.ErrCodeUntrusted {
		t.Errorf("err = %v", err)
	}
}

func TestOpenLinkTimeout(t *testing.T) {
	e := newLaunchEnv(t, nil)
	e.a.linkWait = 300 * time.Millisecond
	t.Setenv("ASKSEMBLE_FAKE_NOLINK", "1")
	_, err := e.a.Open(context.Background(), id6)
	if core.ErrorCode(err) != core.ErrCodeLinkTimeout || !strings.Contains(err.Error(), "claude logs") {
		t.Errorf("err = %v", err)
	}
}

func TestOpenUnknownAndBadIDs(t *testing.T) {
	e := newLaunchEnv(t, nil)
	for _, id := range []string{"99999999-9999-4999-8999-999999999999", "../../x", ""} {
		if _, err := e.a.Open(context.Background(), id); core.ErrorCode(err) != core.ErrCodeNotFound {
			t.Errorf("Open(%q) = %v", id, err)
		}
	}
}

func TestOpenNeedsProcessList(t *testing.T) {
	e := newLaunchEnv(t, nil)
	t.Setenv("ASKSEMBLE_FAKE_EXIT", "1")
	if _, err := e.a.Open(context.Background(), id6); core.ErrorCode(err) != core.ErrCodeStateUnknown {
		t.Errorf("err = %v", err)
	}
	if got := e.launches(t); len(got) != 0 {
		t.Errorf("launched without knowing the state: %v", got)
	}
}

func TestStartNewSession(t *testing.T) {
	e := newLaunchEnv(t, nil)
	res, err := e.a.Start(context.Background(), core.StartRequest{Folder: e.folder, Name: "  --evil name\t", Model: "sonnet"})
	if err != nil {
		t.Fatal(err)
	}
	got := e.launches(t)
	want := []string{"--bg", "--remote-control", "evil name", "-n", "evil name", "--model", "sonnet"}
	if len(got) != 1 || !slices.Equal(got[0].Args, want) || got[0].Cwd != e.folder {
		t.Fatalf("calls = %+v", got)
	}
	if res.Action != core.ActionStarted || res.RemoteURL == "" || res.Session.State != core.StateRunning || res.Session.Name != "evil name" {
		t.Errorf("result = %+v", res)
	}
	if _, err := e.a.Start(context.Background(), core.StartRequest{Folder: filepath.Join(e.folder, "missing")}); core.ErrorCode(err) != core.ErrCodeFolderNotFound {
		t.Errorf("missing folder: %v", err)
	}
}

func TestStopAndRemove(t *testing.T) {
	e := newLaunchEnv(t, []agentEntry{runningEntry, termEntry})
	s, err := e.a.Stop(context.Background(), id1)
	if err != nil {
		t.Fatal(err)
	}
	if s.State != core.StateStopped {
		t.Errorf("after stop: %q", s.State)
	}
	if _, err := e.a.Stop(context.Background(), id5); core.ErrorCode(err) != core.ErrCodeNotBackground {
		t.Errorf("stop terminal session: %v", err)
	}
	if err := e.a.Remove(context.Background(), id1); err != nil {
		t.Fatal(err)
	}
	if err := e.a.Remove(context.Background(), id6); err != nil { // transcript only: nothing to run
		t.Fatal(err)
	}
	got := e.launches(t)
	if len(got) != 2 || !slices.Equal(got[0].Args, []string{"stop", "11111111"}) || !slices.Equal(got[1].Args, []string{"rm", "11111111"}) {
		t.Fatalf("calls = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(e.projects, "work", id1+".jsonl")); err != nil {
		t.Error("remove deleted the transcript")
	}
}

func TestRemoveRunningStopsFirst(t *testing.T) {
	e := newLaunchEnv(t, []agentEntry{runningEntry})
	if err := e.a.Remove(context.Background(), id1); err != nil {
		t.Fatal(err)
	}
	got := e.launches(t)
	if len(got) != 2 || got[0].Args[0] != "stop" || got[1].Args[0] != "rm" {
		t.Errorf("calls = %+v", got)
	}
}

// A new session that never got a prompt leaves claude's list as soon as
// it is stopped; removing it must still succeed (seen on a real PC).
func TestRemoveWhenStopDropsTheSession(t *testing.T) {
	e := newLaunchEnv(t, []agentEntry{runningEntry})
	t.Setenv("ASKSEMBLE_FAKE_STOP_DROPS", "1")
	if err := e.a.Remove(context.Background(), id1); err != nil {
		t.Fatalf("Remove = %v", err)
	}
	got := e.launches(t)
	if len(got) != 1 || got[0].Args[0] != "stop" {
		t.Errorf("calls = %+v (rm should be skipped)", got)
	}
}

func TestOneActionPerSession(t *testing.T) {
	e := newLaunchEnv(t, nil)
	unlock, err := e.a.lock(id6)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.a.Open(context.Background(), id6); core.ErrorCode(err) != core.ErrCodeBusy {
		t.Errorf("second action: %v", err)
	}
	unlock()
	if _, err := e.a.Open(context.Background(), id6); err != nil {
		t.Errorf("after unlock: %v", err)
	}
}

func TestSessionName(t *testing.T) {
	cases := map[string]string{
		"my job":                "my job",
		"  --dangerously  x ":   "dangerously x",
		"a\nb\x07c":             "a b c",
		strings.Repeat("가", 70): strings.Repeat("가", 64),
		"---":                   "",
	}
	for in, want := range cases {
		if got := sessionName(in); got != want {
			t.Errorf("sessionName(%q) = %q, want %q", in, got, want)
		}
	}
}
