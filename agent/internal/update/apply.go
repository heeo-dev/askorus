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
	"os"
	"os/exec"
	"path/filepath"
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

// LoadState reads update.json; a missing or broken file is a fresh state.
// A broken file loses the key number seen, which only loosens the check
// back to the root keys: it never lets an unsigned file through.
func LoadState(dir string) State {
	var s State
	b, err := os.ReadFile(filepath.Join(dir, stateFile))
	if err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
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
}

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
// fresh is false; otherwise it asks GitHub and checks the signatures.
func (c *Checker) Check(ctx context.Context, fresh bool) (Found, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.Now()
	st := LoadState(c.Dir)
	if !fresh && st.Latest != "" && now.Sub(st.Checked) >= 0 && now.Sub(st.Checked) < CacheFor {
		return Found{Current: c.Current, Latest: st.Latest, Checked: st.Checked, Newer: Newer(st.Latest, c.Current)}, nil
	}
	roots, err := c.Roots()
	if err != nil {
		return Found{}, err
	}
	signed, err := c.Source.Latest(ctx)
	if err != nil {
		return Found{}, err
	}
	r, err := Check(roots, st.KeySeq, signed, now)
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

// Progress is the last update's course, for the phone to read once the
// agent is back (update-progress.json).
type Progress struct {
	From   string    `json:"from"`
	To     string    `json:"to"`
	State  string    `json:"state"` // ProgressSwapped, ProgressDone, ProgressRolledBack, ProgressFailed, ProgressNotTaken
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
}

// The states of Progress.
const (
	ProgressSwapped    = "swapped"     // the new program is in place, starting it
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
