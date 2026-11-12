package claude

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// Folder servers (F10, design: 서버 모드). For each folder the person turned
// on, the agent runs
//
//	claude remote-control --name <folder name> --spawn same-dir|worktree --capacity N
//
// in a pseudo console with no input, reads only what it needs from the
// screen (Connected, the environment id, Capacity n/m) and throws the rest
// away (the screen is redrawn constantly, about 20 KB a minute). Stopping
// presses Ctrl+C so the server cleans up its sessions. A server that ends
// on its own is started again after a pause; an untrusted folder is not.
// A server someone else started in the folder is left alone.

// serverProcess is a running folder server. Tests use a fake.
type serverProcess interface {
	PID() int
	Output() io.Reader
	Interrupt() error // press Ctrl+C
	Kill() error
	Wait() error
	Close()
}

type procStarter func(exe, dir string, args []string) (serverProcess, error)

const (
	serverStopWait = 8 * time.Second
	keepLines      = 12
)

var serverBackoff = []time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute}

var (
	ansiRe     = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[=>()][0-9A-Za-z]?|\x1b`)
	envRe      = regexp.MustCompile(`environment=(env_[A-Za-z0-9]+)`)
	capacityRe = regexp.MustCompile(`Capacity:\s*(\d+)\s*/\s*(\d+)`)
	// "Trust C:\…\folder? [y/N]" (Claude Code 2.1.310)
	trustPromptRe = regexp.MustCompile(`^Trust .+\?\s*\[y/N\]`)
	// The one question Claude Code asks before its first server on a PC (seen with
	// 2.1.322 on a new Linux account, #52): "Enable Remote Control? (y/n)".
	consentPromptRe = regexp.MustCompile(`^Enable Remote Control\?`)
)

// servers keeps the folder servers of one adapter.
type servers struct {
	exe     func() (string, error)
	start   procStarter
	pointer func(folder string) (pid int, procStart string, ok bool)
	backoff []time.Duration
	now     func() time.Time
	notify  func(folder string, st core.ServeStatus) // state changes, for the agent log

	mu       sync.Mutex
	byFolder map[string]*server
}

type server struct {
	folder string
	opts   core.ServeOptions
	stop   chan struct{}
	done   chan struct{}
	notify func(folder string, st core.ServeStatus)

	mu        sync.Mutex
	st        core.ServeStatus
	proc      serverProcess
	untrusted bool
	noConsent bool // Claude Code asked whether to turn Remote Control on
	lines     []string
}

func folderKey(folder string) string {
	k := filepath.Clean(folder)
	if runtime.GOOS == "windows" {
		k = strings.ToLower(k)
	}
	return k
}

func (ss *servers) init() {
	if ss.byFolder == nil {
		ss.byFolder = map[string]*server{}
	}
	if ss.backoff == nil {
		ss.backoff = serverBackoff
	}
	if ss.now == nil {
		ss.now = time.Now
	}
}

// StartServer starts serving folder, or restarts it with new options.
func (ss *servers) StartServer(folder string, opts core.ServeOptions) error {
	if !filepath.IsAbs(folder) || !dirExists(folder) {
		return core.Fail(core.ErrCodeFolderNotFound, "")
	}
	if opts.Spawn != "same-dir" && opts.Spawn != "worktree" {
		return errors.New("spawn must be same-dir or worktree")
	}
	if opts.Capacity < 1 || opts.Capacity > 32 {
		return errors.New("capacity must be 1-32")
	}
	opts.Name = sessionName(firstNonEmpty(opts.Name, filepath.Base(folder)))
	ss.mu.Lock()
	ss.init()
	key := folderKey(folder)
	old := ss.byFolder[key]
	if old != nil && old.opts == opts && !ss.recheck(old) {
		ss.mu.Unlock()
		return nil // already serving with these options
	}
	ss.mu.Unlock()
	if old != nil {
		ss.stopServer(old)
	}

	s := &server{folder: folder, opts: opts, stop: make(chan struct{}), done: make(chan struct{}), notify: ss.notify}
	s.st = core.ServeStatus{State: core.ServeStarting, Since: ss.now().UTC(), Capacity: opts.Capacity}
	if pid, _, ok := ss.pointer(folder); ok {
		// Someone else's server runs here (not one of ours, which would be
		// in byFolder). Two servers in one folder would fight; leave it.
		s.st = core.ServeStatus{State: core.ServeExternal, Since: ss.now().UTC(), Error: "pid " + strconv.Itoa(pid)}
		close(s.done)
	} else {
		go ss.supervise(s)
	}
	ss.mu.Lock()
	ss.byFolder[key] = s
	ss.mu.Unlock()
	return nil
}

// consentHelp is what the app shows when Claude Code has not been allowed
// to run Remote Control on this PC yet. Allowing it is the person's
// decision: the agent never answers the question.
const consentHelp = "Remote Control is not turned on in Claude Code on this PC yet. In a terminal on the PC run `claude remote-control`, answer y once, then stop it with Ctrl+C. It will not ask again."

// untrustedRetry is how long an untrusted folder waits before the agent
// tries again (the person may have trusted it on the PC meanwhile).
const untrustedRetry = time.Minute

// recheck reports whether a server with unchanged options should be tried
// again: someone else's server may have gone, and an untrusted folder may
// have been trusted since.
func (ss *servers) recheck(s *server) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.st.State {
	case core.ServeExternal:
		return true
	case core.ServeUntrusted:
		return ss.now().Sub(s.st.Since) >= untrustedRetry
	case core.ServeFailed:
		// Not allowed yet: tried again like an untrusted folder, in case the person said yes meanwhile.
		return s.noConsent && ss.now().Sub(s.st.Since) >= untrustedRetry
	}
	return false
}

func (s *server) state() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.State
}

func (s *server) set(f func(st *core.ServeStatus)) {
	s.mu.Lock()
	before := s.st.State
	f(&s.st)
	st := s.st
	s.mu.Unlock()
	if st.State != before && s.notify != nil {
		s.notify(s.folder, st)
	}
}

// supervise runs the server until stopped, restarting it after a pause
// when it ends on its own.
func (ss *servers) supervise(s *server) {
	defer close(s.done)
	restarts := 0
	for {
		err := ss.runOnce(s)
		select {
		case <-s.stop:
			s.set(func(st *core.ServeStatus) { *st = core.ServeStatus{State: core.ServeOff, Since: ss.now().UTC()} })
			return
		default:
		}
		s.mu.Lock()
		untrusted := s.untrusted
		noConsent := s.noConsent
		detail := strings.Join(s.lines, " | ")
		s.mu.Unlock()
		if untrusted {
			s.set(func(st *core.ServeStatus) {
				*st = core.ServeStatus{State: core.ServeUntrusted, Since: ss.now().UTC(), Error: s.folder}
			})
			return
		}
		if noConsent {
			// Starting it again would only ask again: it waits for the person, like an untrusted folder.
			s.set(func(st *core.ServeStatus) {
				*st = core.ServeStatus{State: core.ServeFailed, Since: ss.now().UTC(), Error: consentHelp, Capacity: s.opts.Capacity}
			})
			return
		}
		msg := serverEnded
		if err != nil {
			msg += " (" + err.Error() + ")"
		}
		if detail != "" {
			msg += ": " + tailString(detail, 300)
		}
		wait := ss.backoff[min(restarts, len(ss.backoff)-1)]
		restarts++
		s.set(func(st *core.ServeStatus) {
			*st = core.ServeStatus{State: core.ServeFailed, Since: ss.now().UTC(), Restarts: restarts, Error: msg, Capacity: s.opts.Capacity}
		})
		select {
		case <-s.stop:
			s.set(func(st *core.ServeStatus) { *st = core.ServeStatus{State: core.ServeOff, Since: ss.now().UTC()} })
			return
		case <-time.After(wait):
		}
		s.set(func(st *core.ServeStatus) { st.State = core.ServeStarting })
	}
}

func (ss *servers) runOnce(s *server) error {
	exe, err := ss.exe()
	if err != nil {
		return err
	}
	args := []string{"remote-control", "--name", s.opts.Name, "--spawn", s.opts.Spawn, "--capacity", strconv.Itoa(s.opts.Capacity)}
	if err := checkArgs(args); err != nil {
		return err
	}
	p, err := ss.start(exe, s.folder, args)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.proc = p
	s.lines = nil
	s.mu.Unlock()
	read := make(chan struct{})
	go func() {
		defer close(read)
		s.readScreen(p.Output())
	}()
	err = p.Wait()
	p.Close() // ends the output stream
	<-read
	s.mu.Lock()
	s.proc = nil
	s.mu.Unlock()
	return err
}

// readScreen picks the few facts the agent needs out of the server's
// screen and drops the rest.
func (s *server) readScreen(r io.Reader) {
	br := bufio.NewReaderSize(r, 32<<10)
	buf := make([]byte, 8<<10)
	pending := ""
	for {
		n, err := br.Read(buf)
		if n > 0 {
			pending += ansiRe.ReplaceAllString(string(buf[:n]), "\n")
			for {
				i := strings.IndexAny(pending, "\r\n")
				if i < 0 {
					break
				}
				s.screenLine(strings.TrimSpace(pending[:i]))
				pending = pending[i+1:]
			}
			if len(pending) > 4096 {
				pending = pending[len(pending)-4096:]
			}
			// A prompt waits for an answer on the same line.
			if p := strings.TrimSpace(pending); trustPromptRe.MatchString(p) || consentPromptRe.MatchString(p) {
				s.screenLine(p)
				pending = ""
			}
		}
		if err != nil {
			if pending != "" {
				s.screenLine(strings.TrimSpace(pending))
			}
			return
		}
	}
}

func (s *server) screenLine(line string) {
	if line == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lines) == 0 || s.lines[len(s.lines)-1] != line {
		s.lines = append(s.lines, line)
		if len(s.lines) > keepLines {
			s.lines = s.lines[1:]
		}
	}
	if strings.Contains(line, untrustedMarker) || strings.Contains(line, "is not trusted") {
		s.untrusted = true
	}
	// In a terminal the server asks "Trust <folder>? [y/N]" and waits.
	// Trusting a folder is the person's decision, so the agent never says
	// yes: it presses Ctrl+C (the server then ends with "is not trusted")
	// and reports the folder as untrusted.
	if trustPromptRe.MatchString(line) && !s.untrusted {
		s.untrusted = true
		if p := s.proc; p != nil {
			go p.Interrupt()
		}
	}
	// "Enable Remote Control? (y/n)": the same goes for turning Remote Control
	// on for the first time. Left alone the server would wait at the question
	// and the folder would show as starting for ever.
	if consentPromptRe.MatchString(line) && !s.noConsent {
		s.noConsent = true
		if p := s.proc; p != nil {
			go p.Interrupt()
		}
	}
	if strings.Contains(line, "Connected") && !strings.Contains(line, "Connecting") && s.st.State == core.ServeStarting {
		s.st.State = core.ServeRunning
		s.st.Since = time.Now().UTC()
		s.st.Error = ""
		if s.notify != nil {
			st := s.st
			go s.notify(s.folder, st)
		}
	}
	if m := envRe.FindStringSubmatch(line); m != nil {
		s.st.EnvironmentID = m[1]
	}
	if m := capacityRe.FindStringSubmatch(line); m != nil {
		s.st.Sessions, _ = strconv.Atoi(m[1])
		s.st.Capacity, _ = strconv.Atoi(m[2])
	}
}

// StopServer presses Ctrl+C and waits; a server that does not end in
// serverStopWait is killed.
func (ss *servers) StopServer(folder string) error {
	ss.mu.Lock()
	ss.init()
	s := ss.byFolder[folderKey(folder)]
	ss.mu.Unlock()
	if s == nil {
		return nil
	}
	ss.stopServer(s)
	return nil
}

func (ss *servers) stopServer(s *server) {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	s.mu.Lock()
	p := s.proc
	s.mu.Unlock()
	if p != nil {
		_ = p.Interrupt()
		select {
		case <-s.done:
		case <-time.After(serverStopWait):
			_ = p.Kill()
			<-s.done
		}
	} else {
		<-s.done
	}
	ss.mu.Lock()
	if ss.byFolder[folderKey(s.folder)] == s {
		delete(ss.byFolder, folderKey(s.folder))
	}
	ss.mu.Unlock()
}

// ServerStatus reports one folder ("off" when not served).
func (ss *servers) ServerStatus(folder string) core.ServeStatus {
	ss.mu.Lock()
	ss.init()
	s := ss.byFolder[folderKey(folder)]
	ss.mu.Unlock()
	if s == nil {
		return core.ServeStatus{State: core.ServeOff}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st
}

// ServerFolders lists the served folders.
func (ss *servers) ServerFolders() []string {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	out := make([]string, 0, len(ss.byFolder))
	for _, s := range ss.byFolder {
		out = append(out, s.folder)
	}
	return out
}

// StopAllServers stops every server at once.
func (ss *servers) StopAllServers() {
	ss.mu.Lock()
	list := make([]*server, 0, len(ss.byFolder))
	for _, s := range ss.byFolder {
		list = append(list, s)
	}
	ss.mu.Unlock()
	var wg sync.WaitGroup
	for _, s := range list {
		wg.Add(1)
		go func() { defer wg.Done(); ss.stopServer(s) }()
	}
	wg.Wait()
}

// pids returns the process ids of the running servers.
func (ss *servers) pids() map[int]bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	m := map[int]bool{}
	for _, s := range ss.byFolder {
		s.mu.Lock()
		if s.proc != nil {
			m[s.proc.PID()] = true
		}
		s.mu.Unlock()
	}
	return m
}

// serverSessions returns the session ids whose process is a child of one
// of our folder servers. claude agents lists them as interactive.
func (ss *servers) serverSessions(entries []agentEntry, parents func() map[int]int) map[string]bool {
	pids := ss.pids()
	if len(pids) == 0 {
		return nil
	}
	pp := parents()
	out := map[string]bool{}
	for _, e := range entries {
		if e.PID > 0 && (pids[e.PID] || pids[pp[e.PID]]) {
			out[e.SessionID] = true
		}
	}
	return out
}

// projectDirName is how Claude Code names a folder's transcript directory:
// every character that is not a letter or digit becomes "-".
func projectDirName(folder string) string {
	var b strings.Builder
	for _, r := range filepath.Clean(folder) {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// livePointer reads <projects>/<folder>/bridge-pointer.json and reports a
// server process that is still running there.
func livePointer(projectsRoot string) func(folder string) (int, string, bool) {
	return func(folder string) (int, string, bool) {
		b, err := os.ReadFile(filepath.Join(projectsRoot, projectDirName(folder), "bridge-pointer.json"))
		if err != nil {
			return 0, "", false
		}
		var p struct {
			PID       int    `json:"pid"`
			ProcStart string `json:"procStart"`
		}
		if json.Unmarshal(b, &p) != nil || p.PID <= 0 {
			return 0, "", false
		}
		if !processAlive(p.PID, p.ProcStart) {
			return 0, "", false
		}
		return p.PID, p.ProcStart, true
	}
}

func tailString(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n:])
}

// serverEnded starts the error of a folder server that ended on its own.
// What follows it after ": " is the last lines of its screen.
const serverEnded = "the server ended"

// ServeLogReason is what agent.log says about a folder server's state
// (R-102, #44). The status the phone gets ends with the last lines of the
// server's screen, and a screen can show a session's name, which may be
// words from a conversation. The log keeps the reason without the screen.
func ServeLogReason(st core.ServeStatus) string {
	if st.State == core.ServeFailed && strings.HasPrefix(st.Error, serverEnded) {
		rest := st.Error[len(serverEnded):]
		if strings.HasPrefix(rest, " (") {
			if i := strings.Index(rest, ")"); i > 0 {
				return serverEnded + rest[:i+1]
			}
		}
		return serverEnded
	}
	return st.Error
}
