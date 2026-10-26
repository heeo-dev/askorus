package claude

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// newTestServers runs the fake claude through the real pseudo console.
func newTestServers(t *testing.T) (*servers, string) {
	t.Helper()
	if !ptySupported() {
		t.Skip("folder servers need a pseudo console (Windows)")
	}
	r, argsFile := fakeRunner(t)
	t.Setenv("ASKSEMBLE_FAKE_UNTRUSTED", "")
	t.Setenv("ASKSEMBLE_FAKE_SERVE_EXIT", "")
	ss := &servers{
		exe:     func() (string, error) { return r.Path(), nil },
		start:   startPTY,
		pointer: func(string) (int, string, bool) { return 0, "", false },
		backoff: []time.Duration{50 * time.Millisecond},
	}
	return ss, argsFile
}

func waitState(t *testing.T, ss *servers, folder, want string) core.ServeStatus {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		st := ss.ServerStatus(folder)
		if st.State == want {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("state %q, want %q", ss.ServerStatus(folder).State, want)
	return core.ServeStatus{}
}

func TestServerRunsAndStopsCleanly(t *testing.T) {
	ss, argsFile := newTestServers(t)
	dir := t.TempDir()
	opts := core.ServeOptions{Spawn: "same-dir", Capacity: 3}
	if err := ss.StartServer(dir, opts); err != nil {
		t.Fatal(err)
	}
	st := waitState(t, ss, dir, core.ServeRunning)
	// The pseudo console may send the rest of the screen a moment later.
	for i := 0; i < 100 && st.EnvironmentID == ""; i++ {
		time.Sleep(20 * time.Millisecond)
		st = ss.ServerStatus(dir)
	}
	if st.EnvironmentID != "env_FAKE123" || st.Capacity != 3 || st.Sessions != 0 {
		ss.mu.Lock()
		s := ss.byFolder[folderKey(dir)]
		ss.mu.Unlock()
		s.mu.Lock()
		t.Errorf("status = %+v, screen lines %q", st, s.lines)
		s.mu.Unlock()
	}
	calls := recordedCalls(t, argsFile)
	want := []string{"remote-control", "--name", filepath.Base(dir), "--spawn", "same-dir", "--capacity", "3"}
	if len(calls) != 1 || !slices.Equal(calls[0].Args, want) || !strings.EqualFold(calls[0].Cwd, dir) {
		t.Fatalf("calls = %+v", calls)
	}
	if got := ss.ServerFolders(); len(got) != 1 {
		t.Errorf("folders = %v", got)
	}

	start := time.Now()
	if err := ss.StopServer(dir); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > serverStopWait {
		t.Errorf("stop took %s: Ctrl+C did not end the server, it was killed", d)
	}
	if st := ss.ServerStatus(dir); st.State != core.ServeOff {
		t.Errorf("after stop = %+v", st)
	}
	if got := ss.ServerFolders(); len(got) != 0 {
		t.Errorf("folders after stop = %v", got)
	}
}

func TestServerSameOptionsIsNoop(t *testing.T) {
	ss, argsFile := newTestServers(t)
	dir := t.TempDir()
	opts := core.ServeOptions{Spawn: "same-dir", Capacity: 2}
	ss.StartServer(dir, opts)
	waitState(t, ss, dir, core.ServeRunning)
	ss.StartServer(dir, opts) // the agent re-applies settings every few seconds
	if n := len(recordedCalls(t, argsFile)); n != 1 {
		t.Errorf("started %d times", n)
	}
	opts.Capacity = 5 // a change restarts it
	ss.StartServer(dir, opts)
	st := waitState(t, ss, dir, core.ServeRunning)
	if n := len(recordedCalls(t, argsFile)); n != 2 || st.Capacity != 5 {
		t.Errorf("after change: %d starts, %+v", n, st)
	}
	ss.StopAllServers()
	if st := ss.ServerStatus(dir); st.State != core.ServeOff {
		t.Errorf("after stop all = %+v", st)
	}
}

func TestServerUntrustedIsNotRetried(t *testing.T) {
	ss, argsFile := newTestServers(t)
	t.Setenv("ASKSEMBLE_FAKE_UNTRUSTED", "1")
	dir := t.TempDir()
	ss.StartServer(dir, core.ServeOptions{Spawn: "same-dir", Capacity: 1})
	st := waitState(t, ss, dir, core.ServeUntrusted)
	time.Sleep(300 * time.Millisecond) // well past the 50 ms backoff
	if n := len(recordedCalls(t, argsFile)); n != 1 {
		t.Errorf("untrusted folder started %d times", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "TRUSTED-BY-AGENT")); err == nil {
		t.Fatal("the agent answered yes to the trust prompt")
	}
	if !strings.Contains(st.Error, dir) {
		t.Errorf("status = %+v", st)
	}
	ss.StartServer(dir, core.ServeOptions{Spawn: "same-dir", Capacity: 1}) // the agent re-applies settings
	if n := len(recordedCalls(t, argsFile)); n != 1 {
		t.Errorf("retried at once: %d starts", n)
	}
	// After a minute it tries again, in case the folder was trusted meanwhile.
	ss.now = func() time.Time { return time.Now().Add(untrustedRetry + time.Second) }
	t.Setenv("ASKSEMBLE_FAKE_UNTRUSTED", "")
	ss.StartServer(dir, core.ServeOptions{Spawn: "same-dir", Capacity: 1})
	waitState(t, ss, dir, core.ServeRunning)
	ss.StopServer(dir)
}

func TestServerRestartsAfterCrash(t *testing.T) {
	ss, argsFile := newTestServers(t)
	t.Setenv("ASKSEMBLE_FAKE_SERVE_EXIT", "1")
	dir := t.TempDir()
	ss.StartServer(dir, core.ServeOptions{Spawn: "same-dir", Capacity: 1})
	deadline := time.Now().Add(15 * time.Second)
	for len(recordedCalls(t, argsFile)) < 3 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(recordedCalls(t, argsFile)); n < 3 {
		t.Fatalf("restarted %d times", n)
	}
	st := ss.ServerStatus(dir)
	if st.Restarts < 1 && st.State != core.ServeStarting && st.State != core.ServeRunning {
		t.Errorf("status = %+v", st)
	}
	ss.StopServer(dir)
	if st := ss.ServerStatus(dir); st.State != core.ServeOff {
		t.Errorf("after stop = %+v", st)
	}
}

// A server someone else started in the folder is left alone.
func TestServerLeavesExternalServerAlone(t *testing.T) {
	ss, argsFile := newTestServers(t)
	ss.pointer = func(string) (int, string, bool) { return 4321, "1", true }
	dir := t.TempDir()
	ss.StartServer(dir, core.ServeOptions{Spawn: "same-dir", Capacity: 1})
	if st := ss.ServerStatus(dir); st.State != core.ServeExternal {
		t.Fatalf("status = %+v", st)
	}
	if n := len(recordedCalls(t, argsFile)); n != 0 {
		t.Errorf("started a second server next to someone else's")
	}
	ss.StopServer(dir) // must not hang or touch the other server
	// Once it is gone, the next check starts ours.
	ss.pointer = func(string) (int, string, bool) { return 0, "", false }
	ss.StartServer(dir, core.ServeOptions{Spawn: "same-dir", Capacity: 1})
	waitState(t, ss, dir, core.ServeRunning)
	ss.StopAllServers()
}

func TestServerRejectsBadOptions(t *testing.T) {
	ss, _ := newTestServers(t)
	dir := t.TempDir()
	if err := ss.StartServer(filepath.Join(dir, "missing"), core.ServeOptions{Spawn: "same-dir", Capacity: 1}); core.ErrorCode(err) != core.ErrCodeFolderNotFound {
		t.Errorf("missing folder: %v", err)
	}
	for _, o := range []core.ServeOptions{{Spawn: "anywhere", Capacity: 1}, {Spawn: "same-dir", Capacity: 0}, {Spawn: "same-dir", Capacity: 99}} {
		if err := ss.StartServer(dir, o); err == nil {
			t.Errorf("accepted %+v", o)
		}
	}
}

// readScreen against a recorded real screen (Claude Code 2.1.310).
func TestReadScreen(t *testing.T) {
	screen := "\x1b[?25l·|· Connecting · rc-test · main\r\n" +
		"\x1b[1A\x1b[J·✔︎· Connected · rc-test · main\r\n" +
		"    Capacity: 0/2 · New sessions will be created in the current directory\r\n\r\n" +
		"Continue coding in the Claude mobile app or https://claude.ai/code?environment=env_01SisCPLgc4jz4sYqJQU9Yxs\r\n" +
		"\x1b[8A\x1b[J·✔︎· Connected · rc-test · main\r\n" +
		"    Capacity: 1/2 · New sessions will be created in the current directory\r\n" +
		"    \x1b]8;;https://claude.ai/code/session_01FakeServeProtoSession00?from=cli\x1b\\asksemble-serve-proto\x1b]8;;\x1b\\\r\n"
	s := &server{st: core.ServeStatus{State: core.ServeStarting}}
	s.readScreen(strings.NewReader(screen))
	if s.st.State != core.ServeRunning || s.st.EnvironmentID != "env_01SisCPLgc4jz4sYqJQU9Yxs" || s.st.Sessions != 1 || s.st.Capacity != 2 {
		t.Errorf("status = %+v", s.st)
	}
	for _, l := range s.lines {
		if strings.Contains(l, "\x1b") {
			t.Errorf("escape left in %q", l)
		}
	}
}

// Sessions a folder server runs are its child processes (seen on a real
// PC: the pre-made session ran as `claude --print --sdk-url …` under the
// server). They show as running with a "server" badge.
func TestServerSessionsAreMarked(t *testing.T) {
	p := newMemProc(false)
	ss := &servers{
		exe:     func() (string, error) { return "claude", nil },
		start:   func(string, string, []string) (serverProcess, error) { return p, nil },
		pointer: func(string) (int, string, bool) { return 0, "", false },
	}
	dir := t.TempDir()
	ss.StartServer(dir, core.ServeOptions{Spawn: "same-dir", Capacity: 1})
	waitState(t, ss, dir, core.ServeRunning)
	defer ss.StopAllServers()

	entries := []agentEntry{
		{PID: 500, Kind: kindInteractive, SessionID: id1}, // child of the server (pid 99)
		{PID: 600, Kind: kindInteractive, SessionID: id5}, // someone's terminal
	}
	parents := func() map[int]int { return map[int]int{500: 99, 600: 1} }
	got := ss.serverSessions(entries, parents)
	if !got[id1] || got[id5] {
		t.Fatalf("server sessions = %v", got)
	}
	list := []core.Session{
		{ID: "claude:" + id1, State: core.StateTerminal},
		{ID: "claude:" + id5, State: core.StateTerminal},
	}
	markServerSessions(list, got)
	if list[0].State != core.StateRunning || !slices.Contains(list[0].Badges, "server") || list[1].State != core.StateTerminal {
		t.Errorf("marked = %+v", list)
	}
}

// The real untrusted-folder screen (Claude Code 2.1.310) is recognised.
func TestTrustPromptRecognised(t *testing.T) {
	p := newMemProc(true)
	s := &server{st: core.ServeStatus{State: core.ServeStarting}, proc: p}
	s.readScreen(strings.NewReader("Remote Control starts Claude Code sessions in this folder, driven from your other devices:\r\n" +
		"Quick safety check: is this a project you created or one you trust? Sessions started here can read\r\n" +
		`Trust C:\work\new? [y/N] ` + "\r\n"))
	if !s.untrusted {
		t.Error("trust prompt not recognised")
	}
}

func TestProjectDirName(t *testing.T) {
	cases := map[string]string{
		`D:\ClaudeHome\Asksemble\.local\rc-test`: "D--ClaudeHome-Asksemble--local-rc-test",
		`D:\ClaudeHome`:                          "D--ClaudeHome",
		`D:\가나`:                                  "D----",
	}
	for in, want := range cases {
		if got := projectDirName(in); got != want && os.PathSeparator == '\\' {
			t.Errorf("projectDirName(%q) = %q, want %q", in, got, want)
		}
	}
}

// memProc is an in-memory serverProcess for the stop-timeout path.
type memProc struct {
	mu      sync.Mutex
	r       *io.PipeReader
	w       *io.PipeWriter
	exited  chan struct{}
	killed  bool
	ignores bool // ignores Ctrl+C
}

func newMemProc(ignoreCtrlC bool) *memProc {
	r, w := io.Pipe()
	p := &memProc{r: r, w: w, exited: make(chan struct{}), ignores: ignoreCtrlC}
	go w.Write([]byte("·✔︎· Connected · mem · main\r\n"))
	return p
}
func (p *memProc) PID() int          { return 99 }
func (p *memProc) Output() io.Reader { return p.r }
func (p *memProc) Interrupt() error {
	if !p.ignores {
		p.exit()
	}
	return nil
}
func (p *memProc) Kill() error { p.mu.Lock(); p.killed = true; p.mu.Unlock(); p.exit(); return nil }
func (p *memProc) Wait() error { <-p.exited; return errors.New("ended") }
func (p *memProc) Close()      { p.w.Close() }
func (p *memProc) exit() {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.exited:
	default:
		close(p.exited)
	}
}

func TestServerKilledWhenCtrlCIgnored(t *testing.T) {
	if testing.Short() {
		t.Skip("waits serverStopWait")
	}
	p := newMemProc(true)
	ss := &servers{
		exe:     func() (string, error) { return "claude", nil },
		start:   func(string, string, []string) (serverProcess, error) { return p, nil },
		pointer: func(string) (int, string, bool) { return 0, "", false },
	}
	dir := t.TempDir()
	ss.StartServer(dir, core.ServeOptions{Spawn: "same-dir", Capacity: 1})
	waitState(t, ss, dir, core.ServeRunning)
	ss.StopServer(dir)
	if !p.killed {
		t.Error("a server ignoring Ctrl+C was not killed")
	}
}
