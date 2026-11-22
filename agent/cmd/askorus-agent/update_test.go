package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/update"
)

// pc is a pretend PC for the update command: a signed release 0.5.1 on a
// pretend GitHub, an installed agent 0.5.0, and agent processes that are
// only flags. "Starting" the new program as update-finish runs it here.
type updatePC struct {
	t       *testing.T
	dir     string
	exe     string
	newProg []byte
	files   map[string][]byte

	mu       sync.Mutex
	alive    map[int]bool
	up       bool
	nextPID  int
	launches []string
	// comesUp says whether a started serve comes up (by the program's bytes).
	comesUp func(program []byte) bool
	// neverReady: a started serve that comes up never listens (R-134).
	neverReady bool
	ready      map[int]bool
	// helperDies: the new program ends before it takes over (R-135).
	helperDies bool
	killed     []int
	hits       int // requests to the pretend GitHub
	done       chan struct{}
}

func newUpdatePC(t *testing.T) *updatePC {
	p := &updatePC{t: t, dir: t.TempDir(), newProg: []byte("agent 0.5.1"), alive: map[int]bool{}, ready: map[int]bool{}, nextPID: 200, done: make(chan struct{}, 4)}
	p.comesUp = func([]byte) bool { return true }

	// The release, signed by a root this test trusts.
	root, rootSec, _ := update.NewKey()
	_, relSec, _ := update.NewKey()
	cert, _ := json.Marshal(update.KeyCert{Kind: update.KindKey, Key: relSec.Public().Base64(), Seq: 1, Issued: time.Now()})
	sum := sha256.Sum256(p.newProg)
	man := update.Manifest{Kind: update.KindRelease, Product: update.Product, Version: "0.5.1", Published: time.Now().Add(-time.Hour), Expires: time.Now().Add(24 * time.Hour)}
	for _, s := range update.Systems {
		man.Programs = append(man.Programs, update.Program{OS: s[0], Arch: s[1], Name: update.ProgramName(s[0], s[1]), Size: int64(len(p.newProg)), SHA256: hex.EncodeToString(sum[:])})
	}
	rel, _ := json.Marshal(man)
	certSig, _ := update.Sign(rootSec, cert, update.KeyTrusted(1))
	relSig, _ := update.Sign(relSec, rel, update.ReleaseTrusted("0.5.1"))
	p.files = map[string][]byte{update.KeyFile: cert, update.KeySigFile: certSig, update.ReleaseFile: rel, update.ReleaseSigFile: relSig}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.hits++
		p.mu.Unlock()
		if name, ok := strings.CutPrefix(r.URL.Path, "/releases/latest/download/"); ok && p.files[name] != nil {
			w.Write(p.files[name])
			return
		}
		if strings.HasPrefix(r.URL.Path, "/releases/download/v0.5.1/askorus-agent-") {
			w.Write(p.newProg)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)

	// The installed agent, where the install script puts it.
	home := t.TempDir()
	t.Setenv("LOCALAPPDATA", home)
	p.exe = update.InstallPath(runtime.GOOS, os.Getenv, home)
	os.MkdirAll(filepath.Dir(p.exe), 0o755)
	os.WriteFile(p.exe, []byte("agent 0.5.0"), 0o755)

	saved := []any{version, updateSource, updateRoots, updateExe, updateHome, updateLaunch, updatePID, updateUp, updateStop, updateWaitOld, updateWaitNew, updateSettle, updatePoll, update.Says}
	savedReady, savedKill, savedHandshake := updateReady, killProcess, updateHandshake
	savedLeave, savedIn, savedStopService, savedEnd := updateLeave, updateInService, updateStopService, updateEndStuck
	t.Cleanup(func() {
		updateReady, killProcess, updateHandshake = savedReady, savedKill, savedHandshake
		updateLeave, updateInService, updateStopService, updateEndStuck = savedLeave, savedIn, savedStopService, savedEnd
		version = saved[0].(string)
		updateSource, updateRoots = saved[1].(func() update.Source), saved[2].(func() ([]update.PublicKey, error))
		updateExe, updateHome = saved[3].(func() (string, error)), saved[4].(func() (string, error))
		updateLaunch = saved[5].(func(string, []string, string, string) (int, error))
		updatePID, updateUp, updateStop = saved[6].(func(int) bool), saved[7].(func(int) bool), saved[8].(func(int) error)
		updateWaitOld, updateWaitNew, updateSettle, updatePoll = saved[9].(time.Duration), saved[10].(time.Duration), saved[11].(time.Duration), saved[12].(time.Duration)
		update.Says = saved[13].(func(string) (string, error))
	})
	version = "0.5.0"
	updateSource = func() update.Source {
		return update.Source{Base: srv.URL + "/releases", Hosts: []string{u.Hostname()}, Agent: "askorus-agent/0.5.0", Transport: srv.Client().Transport}
	}
	updateRoots = func() ([]update.PublicKey, error) { return []update.PublicKey{root}, nil }
	updateExe = func() (string, error) { return p.exe, nil }
	updateHome = func() (string, error) { return home, nil }
	update.Says = func(path string) (string, error) {
		b, _ := os.ReadFile(path)
		return "askorus-agent " + strings.TrimPrefix(string(b), "agent "), nil
	}
	updateWaitOld, updateWaitNew, updateSettle, updatePoll = 2*time.Second, 300*time.Millisecond, 50*time.Millisecond, 5*time.Millisecond
	updateHandshake = time.Second
	// The pids here are pretend ones: no real process or service is looked at or ended.
	updateLeave = func() error { return nil }
	updateInService = func(int) bool { return false }
	updateStopService = func() {}
	updateEndStuck = func(string, int) bool { return false }
	updateReady = func(_ string, pid int) bool { p.mu.Lock(); defer p.mu.Unlock(); return p.ready[pid] }
	killProcess = func(pid int) { p.mu.Lock(); p.killed = append(p.killed, pid); p.alive[pid] = false; p.mu.Unlock() }
	updatePID = func(pid int) bool { p.mu.Lock(); defer p.mu.Unlock(); return p.alive[pid] }
	updateUp = func(int) bool { p.mu.Lock(); defer p.mu.Unlock(); return p.up }
	updateStop = func(int) error {
		p.mu.Lock()
		defer p.mu.Unlock()
		for pid := range p.alive {
			p.alive[pid] = false
		}
		p.up = false
		return nil
	}
	updateLaunch = func(exe string, args []string, dir, log string) (int, error) {
		p.mu.Lock()
		p.launches = append(p.launches, strings.Join(args, " "))
		p.nextPID++
		pid := p.nextPID
		p.mu.Unlock()
		if args[0] == updateFinishArg {
			if p.helperDies {
				return pid, nil // started, and gone before it said anything
			}
			// The new program takes over in the background, as it would.
			go func() { updateFinishCmd(dir, args[1:]); p.done <- struct{}{} }()
			return pid, nil
		}
		prog, _ := os.ReadFile(exe)
		if p.comesUp(prog) {
			p.mu.Lock()
			p.alive[pid], p.up = true, true
			p.ready[pid] = !p.neverReady
			p.mu.Unlock()
		}
		return pid, nil
	}
	return p
}

// running makes the old agent run (pid 111) with its pid file.
func (p *updatePC) running() {
	p.alive[111], p.up = true, true
	os.WriteFile(filepath.Join(p.dir, pidFile), []byte("111"), 0o600)
}

func (p *updatePC) run(args ...string) (string, error) {
	var out bytes.Buffer
	err := updateCmd(p.dir, args, &out)
	return out.String(), err
}

func (p *updatePC) program() string {
	b, _ := os.ReadFile(p.exe)
	return string(b)
}

func TestUpdateOnOffAndStatus(t *testing.T) {
	p := newUpdatePC(t)
	out, err := p.run("status")
	if err != nil || !strings.Contains(out, "updates: on") || !strings.Contains(out, "this agent: 0.5.0") {
		t.Fatalf("status by default: %q, %v", out, err)
	}
	if out, _ := p.run("off"); !strings.Contains(out, "asks GitHub for nothing") {
		t.Fatalf("off: %q", out)
	}
	cfg, _ := config.Load(p.dir)
	if cfg.UpdatesOn() {
		t.Fatal("off did not save")
	}
	if out, _ := p.run("status"); !strings.Contains(out, "updates: off") {
		t.Fatalf("status when off: %q", out)
	}
	for _, sub := range []string{"check", "now"} {
		if _, err := p.run(sub); err == nil || !strings.Contains(err.Error(), "updates are off") {
			t.Errorf("%s when off: %v", sub, err)
		}
	}
	p.run("on")
	if cfg, _ := config.Load(p.dir); !cfg.UpdatesOn() {
		t.Fatal("on did not save")
	}
	if _, err := p.run("sideways"); err == nil {
		t.Fatal("an unknown subcommand ran")
	}
}

// check and setup say updates are on, what goes outside the PC, and how
// to turn them off; and why this agent cannot take them, when it cannot.
func TestUpdateLines(t *testing.T) {
	p := newUpdatePC(t)
	text := func() string {
		cfg, _ := config.Load(p.dir)
		var b strings.Builder
		for _, l := range updateLines(p.dir, cfg) {
			b.WriteString(l.level + " " + l.text.en + " | " + l.text.ko + "\n")
		}
		return b.String()
	}
	got := text()
	if !strings.Contains(got, "ok updates are on") || !strings.Contains(got, "https://github.com/heeo-dev/askorus/releases") || !strings.Contains(got, "askorus-agent update off") || !strings.Contains(got, "업데이트가 켜져 있습니다") {
		t.Fatalf("on:\n%s", got)
	}
	if strings.Contains(got, "info ") || strings.Contains(got, "—") {
		t.Fatalf("on, with a note or a long dash:\n%s", got)
	}
	version = "0.5.0-dev"
	if got := text(); !strings.Contains(got, "info this agent is not a release build") {
		t.Fatalf("a branch build:\n%s", got)
	}
	version = "0.5.0"
	updateRoots = func() ([]update.PublicKey, error) { return nil, nil }
	if got := text(); !strings.Contains(got, "without an update key") {
		t.Fatalf("no key:\n%s", got)
	}
	update.WriteProgress(p.dir, update.Progress{From: "0.5.0", To: "0.5.1", State: update.ProgressRolledBack, Reason: "it did not come up within a minute", At: time.Now()})
	if got := text(); !strings.Contains(got, "warn last update: 0.5.0 → 0.5.1 did not come up") || !strings.Contains(got, "되돌렸습니다") {
		t.Fatalf("a rolled-back update:\n%s", got)
	}
	p.run("off")
	if got := text(); !strings.Contains(got, "info updates are off") || strings.Contains(got, "github") {
		t.Fatalf("off:\n%s", got)
	}
}

func TestUpdateCheck(t *testing.T) {
	p := newUpdatePC(t)
	out, err := p.run("check")
	if err != nil || !strings.Contains(out, "a newer release is there: 0.5.1") {
		t.Fatalf("check: %q, %v", out, err)
	}
	if out, _ := p.run("status"); !strings.Contains(out, "latest release seen: 0.5.1") {
		t.Fatalf("status after a check: %q", out)
	}
	version = "0.5.1"
	if out, _ := p.run("check"); !strings.Contains(out, "is up to date") {
		t.Fatalf("check when up to date: %q", out)
	}
}

// The agent is not running: the program is swapped and nothing is started.
func TestUpdateNowWhileStopped(t *testing.T) {
	p := newUpdatePC(t)
	out, err := p.run("now")
	if err != nil {
		t.Fatalf("now: %v\n%s", err, out)
	}
	if p.program() != "agent 0.5.1" {
		t.Fatalf("the program is %q", p.program())
	}
	if b, _ := os.ReadFile(update.Sibling(p.exe, runtime.GOOS, "prev")); string(b) != "agent 0.5.0" {
		t.Fatalf("prev is %q", b)
	}
	if len(p.launches) != 1 || p.launches[0] != "update-finish 0 0.5.0 0.5.1 0" {
		t.Fatalf("launches: %q", p.launches)
	}
	if !strings.Contains(out, "0.5.0 → 0.5.1, done") {
		t.Fatalf("now said %q", out)
	}
}

// The agent runs: it is stopped, the new program waits for it to end,
// starts the new agent and sees it up.
func TestUpdateNowWhileRunning(t *testing.T) {
	p := newUpdatePC(t)
	p.running()
	out, err := p.run("now")
	if err != nil {
		t.Fatalf("now: %v\n%s", err, out)
	}
	<-p.done
	if p.program() != "agent 0.5.1" {
		t.Fatalf("the program is %q", p.program())
	}
	if len(p.launches) != 2 || p.launches[0] != "update-finish 111 0.5.0 0.5.1 1" || p.launches[1] != "serve" {
		t.Fatalf("launches: %q", p.launches)
	}
	if pr, _ := update.ReadProgress(p.dir); pr.State != update.ProgressDone {
		t.Fatalf("progress: %+v", pr)
	}
	if !strings.Contains(out, "phones lose the PC for a moment") || !strings.Contains(out, "done") {
		t.Fatalf("now said %q", out)
	}
}

// The new agent does not come up: the old program goes back and runs,
// and the one that failed is kept for a look.
func TestUpdateNowRollsBack(t *testing.T) {
	p := newUpdatePC(t)
	p.running()
	p.comesUp = func(prog []byte) bool { return string(prog) != "agent 0.5.1" }
	out, err := p.run("now")
	if err == nil || !strings.Contains(err.Error(), "did not take") {
		t.Fatalf("now: %v\n%s", err, out)
	}
	<-p.done
	if p.program() != "agent 0.5.0" {
		t.Fatalf("the program is %q, not the old one", p.program())
	}
	if b, _ := os.ReadFile(update.Sibling(p.exe, runtime.GOOS, "failed")); string(b) != "agent 0.5.1" {
		t.Fatalf("failed is %q", b)
	}
	pr, _ := update.ReadProgress(p.dir)
	if pr.State != update.ProgressRolledBack || !strings.Contains(pr.Reason, "within a minute") {
		t.Fatalf("progress: %+v", pr)
	}
	if len(p.launches) != 3 || p.launches[1] != "serve" || p.launches[2] != "serve" {
		t.Fatalf("launches: %q", p.launches)
	}
	if !strings.Contains(out, "did not come up and 0.5.0 is back") {
		t.Fatalf("now said %q", out)
	}
	if !p.up {
		t.Fatal("the old agent does not run again")
	}
}

// Neither comes up: the state says so and points at the install line.
func TestUpdateNowFailsBothWays(t *testing.T) {
	p := newUpdatePC(t)
	p.running()
	p.comesUp = func([]byte) bool { return false }
	out, err := p.run("now")
	<-p.done
	if err == nil || !strings.Contains(out, "run the install line again") {
		t.Fatalf("now: %v\n%s", err, out)
	}
	if pr, _ := update.ReadProgress(p.dir); pr.State != update.ProgressFailed {
		t.Fatalf("progress: %+v", pr)
	}
}

func TestUpdateNowRefuses(t *testing.T) {
	p := newUpdatePC(t)
	version = "0.5.0-dev+abc"
	if _, err := p.run("now"); err == nil || !strings.Contains(err.Error(), "not a release build") {
		t.Errorf("a branch build: %v", err)
	}
	version = "0.5.0"
	updateExe = func() (string, error) { return filepath.Join(t.TempDir(), "askorus-agent.exe"), nil }
	if _, err := p.run("now"); err == nil || !strings.Contains(err.Error(), "not where the install script puts it") {
		t.Errorf("another folder: %v", err)
	}
	updateExe = func() (string, error) { return p.exe, nil }

	// A release whose signature does not check out changes nothing.
	p.files[update.ReleaseFile] = bytes.Replace(p.files[update.ReleaseFile], []byte("0.5.1"), []byte("0.5.2"), 1)
	if _, err := p.run("now"); err == nil || !strings.Contains(err.Error(), "does not check out") {
		t.Errorf("a changed release: %v", err)
	}
	if p.program() != "agent 0.5.0" || len(p.launches) != 0 {
		t.Errorf("something changed: %q, %q", p.program(), p.launches)
	}
	// A release that says it is newer but is not: nothing happens.
	delete(p.files, update.ReleaseFile)
	if _, err := p.run("now"); err == nil || !strings.Contains(err.Error(), "no signed update information") {
		t.Errorf("no release.json: %v", err)
	}
}
