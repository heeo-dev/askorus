package update

// What the agent does with a checked release (Phase 1 plan part 27): it
// remembers the highest release key number and the latest version it saw
// (update.json in its folder), downloads the program next to itself and
// checks it, then swaps it in keeping the old one as askorus-agent.prev
// (the name the install scripts use), and puts the old one back when the
// new one does not come up. The steps that stop and start the agent are
// the command's (cmd/askorus-agent); this package only handles files.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/config"
)

const (
	stateFile    = "update.json"
	progressFile = "update-progress.json"

	// CacheFor is how long a check is remembered (part 27).
	CacheFor = 6 * time.Hour
)

// State is what the agent keeps between checks.
type State struct {
	KeySeq  int       `json:"keySeq"`           // highest release key number seen
	Checked time.Time `json:"checked,omitzero"` // last check that reached GitHub
	Latest  string    `json:"latest,omitempty"` // the latest version seen then
}

// ErrBadState: update.json cannot be read. The check stops rather than
// forget the release key number it saw (R-140, review of #35): starting
// over would take a release key that was replaced. Deleting the file
// starts over from MinKeySeq on purpose.
var ErrBadState = errors.New("the update state (update.json) cannot be read; delete it to start over")

// LoadState reads update.json; a missing file is a fresh state.
func LoadState(dir string) (State, error) {
	var s State
	b, err := os.ReadFile(filepath.Join(dir, stateFile))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("%w: %v", ErrBadState, err)
	}
	if err := json.Unmarshal(b, &s); err != nil || s.KeySeq < 0 {
		return State{}, ErrBadState
	}
	return s, nil
}

// SaveState writes update.json.
func SaveState(dir string, s State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(filepath.Join(dir, stateFile), append(b, '\n'))
}

// Fetcher is what a check needs of a Source.
type Fetcher interface {
	Latest(ctx context.Context) (Signed, error)
}

// Checker finds out whether a newer release is there.
type Checker struct {
	Dir     string // the agent's folder, for update.json
	Current string // this agent's version
	GOOS    string
	GOARCH  string
	Source  Fetcher
	Roots   func() ([]PublicKey, error)
	Now     func() time.Time

	mu sync.Mutex
	// The last time GitHub was asked and what came of it (R-133): a fresh
	// check within FreshFor answers from it, and a failed one is not
	// tried again for FailFor, so no phone can make the agent ask GitHub
	// over and over.
	askedAt time.Time
	asked   Found
	askErr  error
}

// FreshFor and FailFor bound how often GitHub is asked (R-133).
var (
	FreshFor = time.Minute
	FailFor  = 30 * time.Minute
)

// Found is what a check learned.
type Found struct {
	Current string
	Latest  string    // "" until a check reached GitHub
	Checked time.Time // when it did
	// Newer: Latest is a release newer than Current.
	Newer bool
	// Release and Program are set by a fresh check that found a newer
	// release with a program for this system.
	Release *Release
	Program Program
}

// Check answers from the last check when it is younger than CacheFor and
// fresh is false; otherwise it asks GitHub and checks the signatures. A
// failed ask is remembered for FailFor, and any ask for FreshFor.
func (c *Checker) Check(ctx context.Context, fresh bool) (Found, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.Now()
	st, err := LoadState(c.Dir)
	if err != nil {
		return Found{}, err
	}
	if !fresh && st.Latest != "" && now.Sub(st.Checked) >= 0 && now.Sub(st.Checked) < CacheFor {
		return Found{Current: c.Current, Latest: st.Latest, Checked: st.Checked, Newer: Newer(st.Latest, c.Current)}, nil
	}
	if !c.askedAt.IsZero() && now.Sub(c.askedAt) >= 0 {
		since := now.Sub(c.askedAt)
		if since < FreshFor || (c.askErr != nil && since < FailFor) {
			return c.asked, c.askErr
		}
	}
	roots, err := c.Roots()
	if err != nil {
		return Found{}, err
	}
	// Built without a root key, the agent could check nothing it fetched:
	// it asks GitHub nothing at all.
	if len(roots) == 0 {
		return Found{}, ErrNoRoot
	}
	f, err := c.ask(ctx, roots, st, now)
	c.askedAt, c.asked, c.askErr = now, f, err
	return f, err
}

func (c *Checker) ask(ctx context.Context, roots []PublicKey, st State, now time.Time) (Found, error) {
	signed, err := c.Source.Latest(ctx)
	if err != nil {
		return Found{}, err
	}
	// The build's floor (R-140) holds even when update.json was deleted.
	r, err := Check(roots, max(st.KeySeq, MinKeySeq), signed, now)
	if err != nil {
		return Found{}, err
	}
	st.KeySeq = max(st.KeySeq, r.KeySeq)
	st.Checked, st.Latest = now, r.Version
	if err := SaveState(c.Dir, st); err != nil {
		return Found{}, err
	}
	f := Found{Current: c.Current, Latest: r.Version, Checked: now, Newer: Newer(r.Version, c.Current)}
	if f.Newer {
		p, ok := r.For(c.GOOS, c.GOARCH)
		if !ok {
			return Found{}, fmt.Errorf("%w: release %s has no agent for %s/%s", ErrBadFiles, r.Version, c.GOOS, c.GOARCH)
		}
		f.Release, f.Program = &r, p
	}
	return f, nil
}

// InstallPath is where the install script puts the agent: only an agent
// there updates itself (part 27). env reads the environment, home is the
// user's home folder.
func InstallPath(goos string, env func(string) string, home string) string {
	if goos == "windows" {
		if env("LOCALAPPDATA") == "" {
			return ""
		}
		return filepath.Join(env("LOCALAPPDATA"), "Programs", "Askorus", "askorus-agent.exe")
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "bin", "askorus-agent")
}

// SamePath reports whether a and b name the same file path (Windows paths
// in any case).
func SamePath(goos, a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if goos == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// ext is the program file ending of a system.
func ext(goos string) string {
	if goos == "windows" {
		return ".exe"
	}
	return ""
}

// Sibling is a file next to exe: askorus-agent.<what>(.exe).
func Sibling(exe, goos, what string) string {
	return filepath.Join(filepath.Dir(exe), "askorus-agent."+what+ext(goos))
}

// Says runs a program with `version` and returns what it printed; tests
// replace it.
var Says = func(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").Output()
	return strings.TrimSpace(string(out)), err
}

// Stage downloads f's program next to exe as askorus-agent.new and checks
// it runs and says the version. A stale .new is removed first.
func Stage(ctx context.Context, src Source, f Found, goos, exe string) (string, error) {
	if f.Release == nil {
		return "", errors.New("no newer release to take")
	}
	dst := Sibling(exe, goos, "new")
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := src.Download(ctx, f.Release.Version, f.Program, dst); err != nil {
		return "", err
	}
	said, err := Says(dst)
	if want := Product + " " + f.Release.Version; err != nil || said != want {
		os.Remove(dst)
		return "", fmt.Errorf("the new agent says %q, not %q (%v)", said, want, err)
	}
	return dst, nil
}

// Swap puts the staged program at exe and keeps the old one as
// askorus-agent.prev. A running program can be renamed on Windows too, so
// the old agent may still be running. When the second step fails the
// first is undone.
func Swap(exe, staged, goos string) (prev string, err error) {
	prev = Sibling(exe, goos, "prev")
	if err := os.Remove(prev); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("the previous agent kept from before cannot be removed: %w", err)
	}
	if err := rename(exe, prev); err != nil {
		return "", err
	}
	if err := rename(staged, exe); err != nil {
		if rerr := rename(prev, exe); rerr != nil {
			return "", fmt.Errorf("%v; and putting the old agent back failed: %v", err, rerr)
		}
		return "", err
	}
	return prev, nil
}

// Restore puts prev back at exe and keeps the program that failed as
// askorus-agent.failed, for a look later.
func Restore(exe, prev, goos string) error {
	failed := Sibling(exe, goos, "failed")
	_ = os.Remove(failed)
	if err := rename(exe, failed); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return rename(prev, exe)
}

// renameWait is how long a rename is tried again: a virus scanner may hold
// a program it has just seen for a moment.
var renameWait = 5 * time.Second

func rename(from, to string) error {
	deadline := time.Now().Add(renameWait)
	for {
		err := os.Rename(from, to)
		if err == nil || errors.Is(err, os.ErrNotExist) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// ErrBusy: another update holds the lock.
var ErrBusy = errors.New("another update is under way")

const lockFile = "update.lock"

// Lock takes the update lock in dir for pid (update.lock), so the phone's
// update and the terminal's never touch the same files at once (R-136). A
// lock whose process has ended is taken over; alive reports whether a pid
// still runs. The returned func lets it go.
//
// The lock file appears with its pid already in it (written beside it and
// hard-linked into place), and a lock left by a process that ended is
// removed only under a second, short-lived lock (update.lock.take), so two
// updates starting together can neither see an empty lock nor remove each
// other's new one (R-145, 2ho's release review).
func Lock(dir string, pid int, alive func(int) bool) (func(), error) {
	path := filepath.Join(dir, lockFile)
	tmp := fmt.Sprintf("%s.%d.tmp", path, pid)
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		return nil, err
	}
	defer os.Remove(tmp)
	for range 2 {
		err := os.Link(tmp, path)
		if err == nil {
			return func() { ReleaseLock(dir, pid) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if held := lockHolder(path); held > 0 && alive(held) {
			return nil, ErrBusy
		}
		if !removeStale(path, alive) {
			return nil, ErrBusy
		}
	}
	return nil, ErrBusy
}

// takeWait is how old a takeover lock must be to count as left behind.
const takeWait = 30 * time.Second

// removeStale removes the lock at path if its process has ended, one
// caller at a time; false when another caller is at it.
func removeStale(path string, alive func(int) bool) bool {
	take := path + ".take"
	f, err := os.OpenFile(take, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if info, serr := os.Stat(take); serr == nil && time.Since(info.ModTime()) > takeWait {
			os.Remove(take) // left by a process that ended mid-takeover
		}
		return false
	}
	f.Close()
	defer os.Remove(take)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		// Gone already (another takeover removed it): nothing to remove, and
		// removing now could take away a lock a new caller has just made
		// (R-167: two updates held the lock at once).
		return true
	}
	if held, _ := strconv.Atoi(strings.TrimSpace(string(b))); held > 0 && alive(held) {
		return false // someone took it meanwhile
	}
	// The file is there and its process has ended. While it is there no new
	// caller can make the lock, and only the holder of take removes it, so
	// this removes the stale lock and nothing else.
	os.Remove(path)
	return true
}

func lockHolder(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// TransferLock hands the lock from one process to another: the old agent
// to the new program that finishes the update.
func TransferLock(dir string, from, to int) error {
	path := filepath.Join(dir, lockFile)
	switch lockHolder(path) {
	case from:
	case 0:
		return nil // nothing is held any more
	default:
		return ErrBusy
	}
	return config.WriteFileAtomic(path, []byte(strconv.Itoa(to)+"\n"))
}

// ReleaseLock lets the lock go when pid holds it.
func ReleaseLock(dir string, pid int) {
	path := filepath.Join(dir, lockFile)
	if lockHolder(path) == pid {
		os.Remove(path)
	}
}

// Progress is the last update's course, for the phone to read once the
// agent is back (update-progress.json).
type Progress struct {
	From   string    `json:"from"`
	To     string    `json:"to"`
	State  string    `json:"state"` // ProgressSwapped, ProgressFinishing, ProgressDone, ProgressRolledBack, ProgressFailed, ProgressNotTaken
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
}

// The states of Progress.
const (
	ProgressSwapped    = "swapped"     // the new program is in place, starting it
	ProgressFinishing  = "finishing"   // the new program runs and takes over (R-135)
	ProgressDone       = "done"        // the new agent is up
	ProgressRolledBack = "rolled-back" // it did not come up; the old one is back
	ProgressFailed     = "failed"      // it did not come up, nor did the old one
	ProgressNotTaken   = "not-taken"   // the update stopped before anything changed
)

// WriteProgress records where an update is.
func WriteProgress(dir string, p Progress) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(filepath.Join(dir, progressFile), append(b, '\n'))
}

// ReadProgress reads the last update's course; false when there was none.
func ReadProgress(dir string) (Progress, bool) {
	b, err := os.ReadFile(filepath.Join(dir, progressFile))
	if err != nil {
		return Progress{}, false
	}
	var p Progress
	if json.Unmarshal(b, &p) != nil || p.State == "" {
		return Progress{}, false
	}
	return p, true
}
