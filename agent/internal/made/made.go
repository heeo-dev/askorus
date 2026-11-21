// Package made keeps copies of the pictures AIs make while they answer
// (#14, made pictures) until the phone has taken them. The AI saves a
// picture in a folder of its own; the agent copies the checked file
// (core.FindMade) into its own folder under a name it makes itself, and the
// run's part names the copy by id. The phone that started the run takes it
// with GET /v1/runs/{run}/images/{id}; no other device can, and nobody names
// a file on the PC. A copy goes after KeepFor, with the run's own life, and
// the oldest go first when the folder holds more than MaxTotalBytes. Every
// copy goes when the agent starts and when it stops, and a device's copies
// when it is unpaired. The AI's originals are never touched.
package made

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

const (
	// KeepFor is how long a copy stays: as long as a finished run can be
	// read (runs.KeepFor, decision 3).
	KeepFor = 24 * time.Hour
	// MaxTotalBytes bounds the folder; the oldest copies go first beyond it.
	MaxTotalBytes = 200 << 20
	sweepEvery    = 10 * time.Minute
	tmpExt        = ".tmp"
)

// maxTotalBytes is MaxTotalBytes; tests make it smaller.
var maxTotalBytes int64 = MaxTotalBytes

// ErrGone: no such copy for that device and run (taken away, never there,
// or the agent restarted).
var ErrGone = errors.New("no such picture")

var idRe = regexp.MustCompile(`^img_[0-9a-f]{32}$`)

// ValidID reports whether id has the shape of a copy's id.
func ValidID(id string) bool { return idRe.MatchString(id) }

type item struct {
	owner, run string
	path, typ  string
	bytes      int64
	at         time.Time
}

// Store keeps the copies in one folder; the index is in memory.
type Store struct {
	dir string
	// Now is the clock; tests move it.
	Now func() time.Time

	mu    sync.Mutex
	items map[string]*item
	stop  chan struct{}
	done  chan struct{}
}

// Open makes the folder (for the user only), deletes what an earlier agent
// left in it and starts the sweeper. dir must be absolute.
func Open(dir string) (*Store, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("the made pictures folder must be an absolute path")
	}
	dir = filepath.Clean(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, Now: time.Now, items: map[string]*item{}, stop: make(chan struct{}), done: make(chan struct{})}
	s.clearFolder()
	go s.sweepLoop()
	return s, nil
}

// Dir is the store's folder.
func (s *Store) Dir() string { return s.dir }

// clearFolder deletes the store's own files: copies by their name and the
// temporary files of a copy cut off half way. Anything else stays.
func (s *Store) clearFolder() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		n := e.Name()
		base := n
		if filepath.Ext(n) == tmpExt {
			base = n[:len(n)-len(tmpExt)]
		}
		ext := filepath.Ext(base)
		if !e.IsDir() && ValidID(base[:len(base)-len(ext)]) {
			_ = os.Remove(filepath.Join(s.dir, n))
		}
	}
}

// Put copies the picture f an AI made into the store for owner's run and
// gives what the part shows of it.
func (s *Store) Put(owner, run string, f core.MadeFile) (core.MadePicture, error) {
	if owner == "" || run == "" {
		return core.MadePicture{}, errors.New("a picture needs an owner and a run")
	}
	ext, ok := core.ImageExt[f.Type]
	if !ok {
		return core.MadePicture{}, errors.New("not a picture type")
	}
	src, err := os.Open(f.Path)
	if err != nil {
		return core.MadePicture{}, err
	}
	defer src.Close()
	data, err := io.ReadAll(io.LimitReader(src, core.MaxMadeBytes+1))
	if err != nil {
		return core.MadePicture{}, err
	}
	if len(data) == 0 || len(data) > core.MaxMadeBytes || core.SniffImage(data) != f.Type {
		return core.MadePicture{}, errors.New("not the picture that was checked")
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return core.MadePicture{}, err
	}
	id := "img_" + hex.EncodeToString(raw[:])
	path := filepath.Join(s.dir, id+ext)
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return core.MadePicture{}, err
	}
	tmp := path + tmpExt
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		_ = os.Remove(tmp)
		return core.MadePicture{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return core.MadePicture{}, err
	}
	w, h := core.ImageSize(data)
	s.mu.Lock()
	s.items[id] = &item{owner: owner, run: run, path: path, typ: f.Type, bytes: int64(len(data)), at: s.Now()}
	s.trimLocked()
	s.mu.Unlock()
	return core.MadePicture{ID: id, Type: f.Type, Bytes: int64(len(data)), Width: w, Height: h}, nil
}

// Get gives the copy id of owner's run: its path and type, or ErrGone.
func (s *Store) Get(owner, run, id string) (path, typ string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	if !ok || it.owner != owner || it.run != run {
		return "", "", ErrGone
	}
	if info, err := os.Lstat(it.path); err != nil || !info.Mode().IsRegular() {
		return "", "", ErrGone
	}
	return it.path, it.typ, nil
}

// DropOwner removes every copy of owner (an unpaired device).
func (s *Store) DropOwner(owner string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, it := range s.items {
		if it.owner == owner {
			s.removeLocked(id, it)
		}
	}
}

// Sweep removes the copies older than KeepFor.
func (s *Store) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	for id, it := range s.items {
		if now.Sub(it.at) > KeepFor {
			s.removeLocked(id, it)
		}
	}
}

// trimLocked removes the oldest copies while the store holds more than
// MaxTotalBytes.
func (s *Store) trimLocked() {
	total := int64(0)
	ids := make([]string, 0, len(s.items))
	for id, it := range s.items {
		total += it.bytes
		ids = append(ids, id)
	}
	if total <= maxTotalBytes {
		return
	}
	sort.Slice(ids, func(i, j int) bool { return s.items[ids[i]].at.Before(s.items[ids[j]].at) })
	for _, id := range ids {
		if total <= maxTotalBytes {
			return
		}
		it := s.items[id]
		total -= it.bytes
		s.removeLocked(id, it)
	}
}

// removeLocked deletes the file and forgets it; a file that cannot go now
// stays and is tried again by the next sweep.
func (s *Store) removeLocked(id string, it *item) {
	if err := os.Remove(it.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
	delete(s.items, id)
}

// Count is how many copies the store keeps, for tests.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

func (s *Store) sweepLoop() {
	defer close(s.done)
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.Sweep()
		}
	}
}

// Close stops the sweeper and deletes every copy: the agent is going down.
func (s *Store) Close() {
	select {
	case <-s.stop:
		return
	default:
		close(s.stop)
	}
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, it := range s.items {
		_ = os.Remove(it.path)
		delete(s.items, id)
	}
}
