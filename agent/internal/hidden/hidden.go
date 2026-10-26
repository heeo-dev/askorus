// Package hidden keeps the sessions the person removed from the list (F5).
// Removing never deletes a transcript; it only hides the session here. Like
// the device file, it is reread when it changes, so the server and the CLI
// can both use it.
package hidden

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/asksemble/asksemble/agent/internal/config"
)

// FileName is the hidden-session file inside the agent directory.
const FileName = "hidden.json"

// Store is the hidden-session list, safe for concurrent use.
type Store struct {
	path string

	mu    sync.Mutex
	ids   []string // full ids, e.g. "claude:<uuid>"
	size  int64
	mtime time.Time
}

type file struct {
	Sessions []string `json:"sessions"`
}

// Open loads dir/hidden.json, or starts empty.
func Open(dir string) (*Store, error) {
	s := &Store{path: filepath.Join(dir, FileName)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refresh(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) refresh() error {
	info, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.ids, s.size, s.mtime = nil, 0, time.Time{}
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() == s.size && info.ModTime().Equal(s.mtime) {
		return nil
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return fmt.Errorf("%s: %w", FileName, err)
	}
	s.ids, s.size, s.mtime = f.Sessions, info.Size(), info.ModTime()
	return nil
}

func (s *Store) save(ids []string) error {
	if ids == nil {
		ids = []string{}
	}
	b, err := json.MarshalIndent(file{Sessions: ids}, "", "  ")
	if err != nil {
		return err
	}
	if err := config.WriteFileAtomic(s.path, append(b, '\n')); err != nil {
		return err
	}
	s.ids = ids
	if info, err := os.Stat(s.path); err == nil {
		s.size, s.mtime = info.Size(), info.ModTime()
	}
	return nil
}

// Add hides a session.
func (s *Store) Add(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refresh(); err != nil {
		return err
	}
	if slices.Contains(s.ids, id) {
		return nil
	}
	return s.save(append(append([]string{}, s.ids...), id))
}

// Remove shows a session again. It reports whether it was hidden.
func (s *Store) Remove(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refresh(); err != nil {
		return false, err
	}
	i := slices.Index(s.ids, id)
	if i < 0 {
		return false, nil
	}
	return true, s.save(slices.Delete(append([]string{}, s.ids...), i, i+1))
}

// Set returns the hidden ids. If the file cannot be read it returns what
// was last loaded.
func (s *Store) Set() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.refresh()
	m := make(map[string]bool, len(s.ids))
	for _, id := range s.ids {
		m[id] = true
	}
	return m
}

// List returns the hidden ids in the order they were hidden.
func (s *Store) List() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refresh(); err != nil {
		return nil, err
	}
	return append([]string{}, s.ids...), nil
}
