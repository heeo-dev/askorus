// Package uploads keeps the photos a phone sends with a question (#14)
// for as long as an ask needs them, and no longer. A photo comes in as
// bytes (POST /v1/uploads), is checked to be a JPEG, PNG or WebP by its
// first bytes, and is written to the uploads folder under a name the
// store makes itself: the phone never names a file on the PC. A question
// then names the photos by id; the store hands their paths to the ask and
// keeps them until the ask has ended. Ten minutes after its last use a
// photo is deleted, and so is everything left over when the agent starts.
//
// Photos are kept by owner (the paired device that sent them): a device
// uses and deletes only its own, and each device has a limit.
package uploads

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

const (
	// MaxPerOwner and MaxBytesPerOwner bound what one device keeps on the
	// PC at a time: four questions' worth of photos.
	MaxPerOwner      = 4 * core.MaxImages
	MaxBytesPerOwner = 32 << 20
	// KeepFor is how long a photo stays after its last use (an upload, or
	// the end of an ask that carried it).
	KeepFor = 10 * time.Minute
	// sweepEvery is how often the store looks for photos to delete.
	sweepEvery = time.Minute
)

// Errors of Put and Hold.
var (
	ErrTooLarge = errors.New("the photo is larger than the limit")
	ErrBadType  = errors.New("not a JPEG, PNG or WebP image")
	ErrLimit    = errors.New("this device keeps too many photos on the PC already")
	ErrGone     = errors.New("no such photo (deleted, or the agent restarted)")
)

// Upload is one kept photo as the phone hears of it.
type Upload struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Bytes int64  `json:"bytes"`
}

type item struct {
	owner string
	typ   string
	path  string
	bytes int64
	last  time.Time
	holds int
}

// Store keeps photos in one folder. The index is in memory: photos do not
// outlive the agent.
type Store struct {
	dir string
	// Now is the clock; tests move it.
	Now func() time.Time

	mu    sync.Mutex
	items map[string]*item
	stop  chan struct{}
	done  chan struct{}
}

// Open makes the uploads folder (for the user only), deletes the photos an
// earlier agent left in it and starts the sweeper. dir must be absolute.
func Open(dir string) (*Store, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("the uploads folder must be an absolute path")
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

// Dir is the uploads folder.
func (s *Store) Dir() string { return s.dir }

// clearFolder deletes the store's own files from the folder: kept photos by
// their name, and the temporary files of a write that was cut off. Anything
// else is left alone.
func (s *Store) clearFolder() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if s.ownName(name) || (filepath.Ext(name) == tmpExt && s.ownName(name[:len(name)-len(tmpExt)])) {
			_ = os.Remove(filepath.Join(s.dir, name))
		}
	}
}

const tmpExt = ".tmp"

// ownName reports whether name is a kept photo's file name.
func (s *Store) ownName(name string) bool {
	return core.CheckImage(core.Image{Path: filepath.Join(s.dir, name), Type: typeOfExt(filepath.Ext(name))}) == nil
}

func typeOfExt(ext string) string {
	for typ, e := range core.ImageExt {
		if e == ext {
			return typ
		}
	}
	return ""
}

// Sniff names the image type of data by its first bytes, or "".
func Sniff(data []byte) string { return core.SniffImage(data) }

// Put keeps one photo of owner read from r. It reads at most
// core.MaxImageBytes and one byte more.
func (s *Store) Put(owner string, r io.Reader) (Upload, error) {
	if owner == "" {
		return Upload{}, errors.New("a photo needs an owner")
	}
	data, err := io.ReadAll(io.LimitReader(r, core.MaxImageBytes+1))
	if err != nil {
		return Upload{}, err
	}
	if len(data) > core.MaxImageBytes {
		return Upload{}, ErrTooLarge
	}
	typ := Sniff(data)
	if typ == "" {
		return Upload{}, ErrBadType
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Upload{}, err
	}
	id := "up_" + hex.EncodeToString(raw[:])
	path := filepath.Join(s.dir, core.ImageName(id, typ))
	if err := core.CheckImage(core.Image{Path: path, Type: typ}); err != nil {
		return Upload{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	n, total := 0, int64(0)
	for _, it := range s.items {
		if it.owner == owner {
			n++
			total += it.bytes
		}
	}
	if n >= MaxPerOwner || total+int64(len(data)) > MaxBytesPerOwner {
		return Upload{}, ErrLimit
	}
	// The folder may have gone under a running agent (R-129: a test run of
	// uninstall removed it); make it again rather than fail every photo.
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return Upload{}, err
	}
	tmp := path + tmpExt
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		// A write cut off half way (a full disk) leaves no file behind.
		_ = os.Remove(tmp)
		return Upload{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return Upload{}, err
	}
	s.items[id] = &item{owner: owner, typ: typ, path: path, bytes: int64(len(data)), last: s.Now()}
	return Upload{ID: id, Type: typ, Bytes: int64(len(data))}, nil
}

// Hold gives the photos ids of owner, in order, and keeps them until
// release is called (once; more calls do nothing). ErrGone when one of
// them is not a kept photo of owner: nothing is held then.
func (s *Store) Hold(owner string, ids []string) (images []core.Image, release func(), err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := make([]*item, 0, len(ids))
	for _, id := range ids {
		it, ok := s.items[id]
		if !ok || it.owner != owner {
			return nil, nil, ErrGone
		}
		// The file itself, not what a link put in its place points at.
		if info, err := os.Lstat(it.path); err != nil || !info.Mode().IsRegular() {
			return nil, nil, ErrGone
		}
		held = append(held, it)
		images = append(images, core.Image{Path: it.path, Type: it.typ})
	}
	for _, it := range held {
		it.holds++
		it.last = s.Now()
	}
	var once sync.Once
	return images, func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			for _, it := range held {
				it.holds--
				it.last = s.Now()
			}
		})
	}, nil
}

// Delete removes owner's photo id at once, unless an ask still holds it
// (then it goes KeepFor after that ask). An id that is not owner's is left
// alone.
func (s *Store) Delete(owner, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if it, ok := s.items[id]; ok && it.owner == owner && it.holds == 0 {
		s.removeLocked(id, it)
	}
}

// DropOwner removes every photo of owner that no ask holds.
func (s *Store) DropOwner(owner string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, it := range s.items {
		if it.owner == owner && it.holds == 0 {
			s.removeLocked(id, it)
		}
	}
}

// Sweep deletes the photos last used over KeepFor ago that no ask holds.
func (s *Store) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	for id, it := range s.items {
		if it.holds == 0 && now.Sub(it.last) > KeepFor {
			s.removeLocked(id, it)
		}
	}
}

// removeLocked deletes the file and forgets it. A file that cannot be
// deleted now (still open by an AI program on Windows) stays in the index
// and is tried again by the next sweep.
func (s *Store) removeLocked(id string, it *item) {
	if err := os.Remove(it.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
	delete(s.items, id)
}

// Count is how many photos the store keeps, for tests and the log.
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

// Close stops the sweeper and deletes every photo, held or not: the agent
// is going down and its asks with it.
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
