package claude

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// Launch rules (design: 실행 규칙).
//
//	running background session   nothing is run; return its remote link
//	stopped background session   claude --bg --resume <id>      (no flags: flags would fork a copy)
//	open in another terminal     nothing is run; the person types /rc there
//	transcript only              claude --bg --resume <id> --remote-control -n <title> [--model] [--effort]
//	new session                  claude --bg --remote-control <name> -n <name> [--model] [--effort]
//
// Resuming runs in the session's own folder, because claude looks sessions
// up by the current folder. Every argument list passes checkArgs.

const (
	launchTimeout = 60 * time.Second // the first --bg also starts the service
	linkTimeout   = 20 * time.Second
	stopTimeout   = 15 * time.Second
	linkSkew      = 3 * time.Second // clock slack when comparing line times
)

var (
	shortIDRe       = regexp.MustCompile(`backgrounded\s*·\s*([0-9a-f]{6,32})`)
	untrustedMarker = "Workspace not trusted"
)

// lock allows one action per session at a time.
func (a *Adapter) lock(id string) (func(), error) {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	if a.busy == nil {
		a.busy = map[string]bool{}
	}
	if a.busy[id] {
		return nil, core.Fail(core.ErrCodeBusy, "")
	}
	a.busy[id] = true
	return func() {
		a.opMu.Lock()
		delete(a.busy, id)
		a.opMu.Unlock()
	}, nil
}

// find reads the transcript and the live process entry of a session, with
// a fresh process list.
func (a *Adapter) find(ctx context.Context, id string) (Transcript, agentEntry, bool, error) {
	if !ValidUUID(id) {
		return Transcript{}, agentEntry{}, false, core.Fail(core.ErrCodeNotFound, "")
	}
	if a.runner == nil {
		return Transcript{}, agentEntry{}, false, core.Fail(core.ErrCodeAgentUnavailable, "claude was not found")
	}
	a.Invalidate()
	entries, ok := a.agents(ctx)
	if !ok {
		return Transcript{}, agentEntry{}, false, core.Fail(core.ErrCodeStateUnknown, "")
	}
	entries = withLive(entries, a.live())
	ts, err := a.transcripts.Scan()
	if err != nil {
		return Transcript{}, agentEntry{}, false, err
	}
	var t Transcript
	found := false
	for _, x := range ts {
		if x.SessionID == id {
			t, found = x, true
			break
		}
	}
	e, has := bestEntry(entries, id)
	if !found && !has {
		return Transcript{}, agentEntry{}, false, core.Fail(core.ErrCodeNotFound, "")
	}
	return t, e, has, nil
}

func bestEntry(entries []agentEntry, id string) (agentEntry, bool) {
	var best agentEntry
	has := false
	for _, e := range entries {
		if e.SessionID == id && (!has || stateRank(e.state()) > stateRank(best.state())) {
			best, has = e, true
		}
	}
	return best, has
}

// Open follows the launch rules for the session's state.
func (a *Adapter) Open(ctx context.Context, id string) (core.OpenResult, error) {
	unlock, err := a.lock(id)
	if err != nil {
		return core.OpenResult{}, err
	}
	defer unlock()
	t, e, has, err := a.find(ctx, id)
	if err != nil {
		return core.OpenResult{}, err
	}
	state := core.StateHistory
	if has {
		state = e.state()
	}
	switch state {
	case core.StateTerminal:
		liveURL := a.live()[id].remoteURL()
		if a.srv.serverSessions([]agentEntry{e}, processParents)[id] {
			// A folder server runs it: reachable in the official app, and
			// never resumed from here.
			s := a.sessionWith(id, e, core.StateRunning)
			s.AttachCommand = ""
			s.Badges = append(s.Badges, "server")
			if u := firstNonEmpty(liveURL, t.RemoteURL); u != "" {
				s.RemoteURL = u
				return core.OpenResult{Session: s, RemoteURL: u, Action: core.ActionNone}, nil
			}
			return core.OpenResult{Session: s, Action: core.ActionNone, Hint: core.HintServerSession}, nil
		}
		s := a.sessionWith(id, e, state)
		if liveURL != "" {
			// Opened in a terminal with remote control on (--rc or /rc):
			// nothing to run, just hand over the link.
			s.RemoteURL = liveURL
			s.Badges = append(s.Badges, "rc")
			return core.OpenResult{Session: s, RemoteURL: liveURL, Action: core.ActionNone}, nil
		}
		return core.OpenResult{Session: s, Action: core.ActionNone, Hint: core.HintUseRCInTerminal}, nil
	case core.StateRunning:
		url, err := a.waitLink(ctx, id, time.Time{}, e.ID)
		if err != nil {
			return core.OpenResult{}, err
		}
		return core.OpenResult{Session: a.sessionWith(id, e, state), RemoteURL: url, Action: core.ActionNone}, nil
	}

	// Never resume while a process still runs the session: claude would
	// fork a copy. state() already treats any pid as running; this guards
	// the rule if that ever changes.
	if has && e.PID > 0 {
		return core.OpenResult{}, core.Fail(core.ErrCodeBusy, "the session still has a running process")
	}
	folder := firstNonEmpty(t.Cwd, e.Cwd)
	if !dirExists(folder) {
		return core.OpenResult{}, core.Fail(core.ErrCodeLaunchFailed, "the session's folder no longer exists")
	}
	var args []string
	title := ""
	if state == core.StateStopped {
		args = []string{"--bg", "--resume", id}
	} else {
		title = sessionName(firstNonEmpty(t.Title, "asksemble "+id[:8]))
		args = append([]string{"--bg", "--resume", id, "--remote-control", "-n", title}, a.modelArgs("", "")...)
	}
	if err := a.checkService(); err != nil {
		return core.OpenResult{}, err
	}
	start := time.Now()
	res, err := a.runner.RunFor(ctx, launchTimeout, folder, args...)
	a.Invalidate()
	if err != nil {
		return core.OpenResult{}, launchError(res, err, folder)
	}
	short := firstNonEmpty(e.ID, parseShortID(res.Stdout))
	url, err := a.waitLink(ctx, id, start, short)
	if err != nil {
		return core.OpenResult{}, err
	}
	if !has {
		e = agentEntry{ID: short, SessionID: id, Kind: kindBackground, Cwd: folder}
	}
	e.Name = firstNonEmpty(e.Name, title)
	return core.OpenResult{Session: a.sessionWith(id, e, core.StateRunning), RemoteURL: url, Action: core.ActionResumed}, nil
}

// Start creates a new remote session in req.Folder.
func (a *Adapter) Start(ctx context.Context, req core.StartRequest) (core.OpenResult, error) {
	if a.runner == nil {
		return core.OpenResult{}, core.Fail(core.ErrCodeAgentUnavailable, "claude was not found")
	}
	if !filepath.IsAbs(req.Folder) || !dirExists(req.Folder) {
		return core.OpenResult{}, core.Fail(core.ErrCodeFolderNotFound, "")
	}
	name := sessionName(req.Name)
	if name == "" {
		name = sessionName(filepath.Base(req.Folder) + " " + time.Now().Format("01-02 15:04"))
	}
	args := append([]string{"--bg", "--remote-control", name, "-n", name}, a.modelArgs(req.Model, req.Effort)...)
	if err := a.checkService(); err != nil {
		return core.OpenResult{}, err
	}
	start := time.Now()
	res, err := a.runner.RunFor(ctx, launchTimeout, req.Folder, args...)
	a.Invalidate()
	if err != nil {
		return core.OpenResult{}, launchError(res, err, req.Folder)
	}
	short := parseShortID(res.Stdout)
	if short == "" {
		return core.OpenResult{}, core.Fail(core.ErrCodeLaunchFailed, "claude did not report the new session")
	}
	id, url, err := a.waitNewLink(ctx, short, start)
	if err != nil {
		return core.OpenResult{}, err
	}
	e := agentEntry{ID: short, SessionID: id, Kind: kindBackground, Name: name, Cwd: req.Folder}
	return core.OpenResult{Session: a.sessionWith(id, e, core.StateRunning), RemoteURL: url, Action: core.ActionStarted}, nil
}

// waitNewLink finds a new session by the short id claude printed: its
// transcript file name starts with it. This needs no claude agents call
// (1.3–1.5 s each). If the file does not show up, the process list maps
// the short id instead.
func (a *Adapter) waitNewLink(ctx context.Context, short string, since time.Time) (string, string, error) {
	timeout := a.linkWait
	if timeout == 0 {
		timeout = linkTimeout
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for id, l := range a.live() {
			if u := l.remoteURL(); u != "" && strings.HasPrefix(id, short) {
				return id, u, nil
			}
		}
		if ts, err := a.transcripts.Scan(); err == nil {
			for _, t := range ts {
				if strings.HasPrefix(t.SessionID, short) && t.RemoteURL != "" && !t.RemoteURLAt.Before(since.Add(-linkSkew)) {
					return t.SessionID, t.RemoteURL, nil
				}
			}
		}
		if !sleepCtx(ctx, a.pollEvery()) {
			return "", "", ctx.Err()
		}
	}
	id, err := a.sessionIDFor(ctx, short)
	if err != nil {
		return "", "", err
	}
	url, err := a.waitLink(ctx, id, since, short)
	return id, url, err
}

// Stop stops a running background session and waits until it is down.
func (a *Adapter) Stop(ctx context.Context, id string) (core.Session, error) {
	unlock, err := a.lock(id)
	if err != nil {
		return core.Session{}, err
	}
	defer unlock()
	_, e, has, err := a.find(ctx, id)
	if err != nil {
		return core.Session{}, err
	}
	if !has || e.Kind != kindBackground {
		return core.Session{}, core.Fail(core.ErrCodeNotBackground, "")
	}
	if e.state() == core.StateRunning {
		if err := a.stopEntry(ctx, e); err != nil {
			return core.Session{}, err
		}
	}
	e.PID = 0 // stopEntry waited until the process was gone
	return a.sessionWith(id, e, core.StateStopped), nil
}

// Remove takes a background session off claude's list (claude rm). The
// transcript stays; the caller hides the session from the app's list.
func (a *Adapter) Remove(ctx context.Context, id string) error {
	unlock, err := a.lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	_, e, has, err := a.find(ctx, id)
	if err != nil {
		return err
	}
	if !has || e.Kind != kindBackground {
		return nil // nothing in claude's list; hiding is enough
	}
	if e.state() == core.StateRunning {
		if err := a.stopEntry(ctx, e); err != nil {
			return err
		}
		// claude can drop a stopped session from its list by itself (seen
		// with a new session that never got a prompt); then there is
		// nothing left to rm.
		if !a.inAgents(ctx, id) {
			return nil
		}
	}
	res, err := a.runner.Run(ctx, "", "rm", firstNonEmpty(e.ID, id))
	a.Invalidate()
	if err != nil {
		if !a.inAgents(ctx, id) {
			return nil // gone anyway
		}
		return core.Fail(core.ErrCodeLaunchFailed, tail(res, 300))
	}
	return nil
}

// inAgents reports whether claude still lists the session. When the list
// cannot be read it answers true, so callers do not assume it is gone.
func (a *Adapter) inAgents(ctx context.Context, id string) bool {
	a.Invalidate()
	entries, ok := a.agents(ctx)
	if !ok {
		return true
	}
	_, has := bestEntry(entries, id)
	return has
}

func (a *Adapter) stopEntry(ctx context.Context, e agentEntry) error {
	res, err := a.runner.Run(ctx, "", "stop", firstNonEmpty(e.ID, e.SessionID))
	a.Invalidate()
	if err != nil {
		return core.Fail(core.ErrCodeLaunchFailed, tail(res, 300))
	}
	// Resuming right after a stop forks a copy, so wait for the process
	// to be gone before anyone can act again.
	deadline := time.Now().Add(stopTimeout)
	for time.Now().Before(deadline) {
		a.Invalidate()
		entries, ok := a.agents(ctx)
		if ok {
			cur, has := bestEntry(entries, e.SessionID)
			_, stillLive := a.live()[e.SessionID]
			if (!has || cur.PID == 0) && !stillLive {
				return nil // the process is gone
			}
		}
		if !sleepCtx(ctx, a.pollEvery()) {
			return ctx.Err()
		}
	}
	return core.Fail(core.ErrCodeLaunchFailed, "the session did not stop in time")
}

// waitLink waits for a remote link written after since (any link when since
// is zero). If the transcript keeps its old link but the session is running
// again, that link is used: resuming keeps the same link.
func (a *Adapter) waitLink(ctx context.Context, id string, since time.Time, short string) (string, error) {
	timeout := a.linkWait
	if timeout == 0 {
		timeout = linkTimeout
	}
	deadline := time.Now().Add(timeout)
	lastCheck := time.Time{}
	for {
		// A running process with remote control on names its link in
		// ~/.claude/sessions; that file belongs to the current process,
		// so it is never a link from before a resume.
		if u := a.live()[id].remoteURL(); u != "" {
			return u, nil
		}
		t, _ := a.transcript(id)
		if t.RemoteURL != "" {
			if since.IsZero() || !t.RemoteURLAt.Before(since.Add(-linkSkew)) {
				return t.RemoteURL, nil
			}
			if time.Since(lastCheck) > 3*time.Second {
				lastCheck = time.Now()
				a.Invalidate()
				if entries, ok := a.agents(ctx); ok {
					if e, has := bestEntry(entries, id); has && e.state() == core.StateRunning && e.PID > 0 && time.Since(since) > 3*time.Second {
						return t.RemoteURL, nil
					}
				}
			}
		}
		if time.Now().After(deadline) {
			hint := "no remote link after " + timeout.String()
			if short != "" {
				hint += "; see: claude logs " + short
			}
			return "", core.Fail(core.ErrCodeLinkTimeout, hint)
		}
		if !sleepCtx(ctx, a.pollEvery()) {
			return "", ctx.Err()
		}
	}
}

// sessionIDFor maps the short id claude prints to the full session id.
func (a *Adapter) sessionIDFor(ctx context.Context, short string) (string, error) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		a.Invalidate()
		if entries, ok := a.agents(ctx); ok {
			for _, e := range entries {
				if e.ID == short && ValidUUID(e.SessionID) {
					return e.SessionID, nil
				}
			}
			for _, e := range entries {
				if strings.HasPrefix(e.SessionID, short) && ValidUUID(e.SessionID) {
					return e.SessionID, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return "", core.Fail(core.ErrCodeLaunchFailed, "the new session "+short+" did not appear in claude agents")
		}
		if !sleepCtx(ctx, a.pollEvery()) {
			return "", ctx.Err()
		}
	}
}

func (a *Adapter) transcript(id string) (Transcript, bool) {
	ts, err := a.transcripts.Scan()
	if err != nil {
		return Transcript{}, false
	}
	for _, t := range ts {
		if t.SessionID == id {
			return t, true
		}
	}
	return Transcript{}, false
}

// sessionWith builds the result session from what the action already knows
// (its state after the action and the process entry), so no extra claude
// agents call (1.3–1.5 s) is needed. The app refreshes its list anyway.
func (a *Adapter) sessionWith(id string, e agentEntry, state core.State) core.Session {
	t, _ := a.transcript(id)
	if t.SessionID == "" {
		t = Transcript{SessionID: id, Cwd: e.Cwd}
	}
	s := build(t, e, true, true)
	s.State = state
	if state != core.StateRunning && state != core.StateTerminal {
		s.Activity = ""
	}
	switch state {
	case core.StateRunning, core.StateStopped:
		s.AttachCommand = "claude attach " + quoteArg(firstNonEmpty(e.Name, e.ID, id))
	case core.StateTerminal:
		s.AttachCommand = ""
	}
	return s
}

// modelArgs adds --model/--effort only when a value is set, so Claude Code's
// own defaults apply otherwise.
func (a *Adapter) modelArgs(model, effort string) []string {
	var args []string
	if m := firstNonEmpty(model, a.defaultModel); m != "" {
		args = append(args, "--model", m)
	}
	if e := firstNonEmpty(effort, a.defaultEffort); e != "" {
		args = append(args, "--effort", e)
	}
	return args
}

func (a *Adapter) pollEvery() time.Duration {
	if a.poll > 0 {
		return a.poll
	}
	return 300 * time.Millisecond
}

func launchError(res Result, err error, folder string) error {
	out := string(res.Stdout) + string(res.Stderr)
	if strings.Contains(out, untrustedMarker) {
		return core.Fail(core.ErrCodeUntrusted, folder)
	}
	if detail := tail(res, 300); detail != "" {
		return core.Fail(core.ErrCodeLaunchFailed, detail)
	}
	return core.Fail(core.ErrCodeLaunchFailed, err.Error())
}

func parseShortID(out []byte) string {
	if m := shortIDRe.FindSubmatch(out); m != nil {
		return string(m[1])
	}
	return ""
}

// tail returns the last n bytes of a run's output, trimmed, on one line.
func tail(res Result, n int) string {
	out := bytes.TrimSpace(append(append([]byte{}, res.Stdout...), res.Stderr...))
	if len(out) > n {
		out = out[len(out)-n:]
		for len(out) > 0 && !utf8.RuneStart(out[0]) {
			out = out[1:]
		}
	}
	return strings.Join(strings.Fields(string(out)), " ")
}

// sessionName makes a safe session name: printable, one line, at most 64
// characters, never starting with "-" (so it cannot read as a flag).
func sessionName(s string) string {
	s = strings.Map(func(r rune) rune {
		if !unicode.IsPrint(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimLeft(s, "- ")
	if utf8.RuneCountInString(s) > 64 {
		s = strings.TrimSpace(string([]rune(s)[:64]))
	}
	return s
}

func dirExists(p string) bool {
	if p == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
