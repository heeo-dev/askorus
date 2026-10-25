// Package devices keeps the paired phones. Only a SHA-256 hash of each
// device token is stored (N4, N8). The file is shared by the running server
// and the CLI, so every call rereads it when it changed on disk: a device
// revoked from another window stops working at once.
package devices

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/config"
)

// FileName is the device file inside the agent directory.
const FileName = "devices.json"

// TokenPrefix marks Asksemble device tokens so they are easy to spot.
const TokenPrefix = "ask_"

// Device is one paired phone.
type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TokenHash string    `json:"tokenHash"`
	CreatedAt time.Time `json:"createdAt"`
}

// Store is the device file, safe for concurrent use.
type Store struct {
	path string

	mu    sync.Mutex
	list  []Device
	size  int64
	mtime time.Time
}

var ErrNotFound = errors.New("device not found")

// Open loads dir/devices.json, or starts empty.
func Open(dir string) (*Store, error) {
	s := &Store{path: filepath.Join(dir, FileName)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refresh(); err != nil {
		return nil, err
	}
	return s, nil
}

// refresh rereads the file when its size or time changed. Callers hold mu.
func (s *Store) refresh() error {
	info, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.list, s.size, s.mtime = nil, 0, time.Time{}
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
	var list []Device
	if err := json.Unmarshal(b, &list); err != nil {
		return fmt.Errorf("%s: %w", FileName, err)
	}
	s.list, s.size, s.mtime = list, info.Size(), info.ModTime()
	return nil
}

// Add registers a device and returns its token. The token is handed to the
// phone once and never stored.
func (s *Store) Add(name string) (token string, d Device, err error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return "", Device{}, errors.New("device name must be 1-64 characters")
	}
	raw := make([]byte, 32) // 256 bits
	if _, err := rand.Read(raw); err != nil {
		return "", Device{}, err
	}
	idRaw := make([]byte, 6)
	if _, err := rand.Read(idRaw); err != nil {
		return "", Device{}, err
	}
	token = TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	d = Device{ID: hex.EncodeToString(idRaw), Name: name, TokenHash: hash(token), CreatedAt: time.Now().UTC()}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refresh(); err != nil {
		return "", Device{}, err
	}
	if err := s.save(append(append([]Device(nil), s.list...), d)); err != nil {
		return "", Device{}, err
	}
	return token, d, nil
}

// Verify returns the device that owns token. If the file cannot be read,
// nothing is accepted.
func (s *Store) Verify(token string) (Device, bool) {
	if !strings.HasPrefix(token, TokenPrefix) || len(token) > 128 {
		return Device{}, false
	}
	h := []byte(hash(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refresh() != nil {
		return Device{}, false
	}
	var found Device
	ok := false
	for _, d := range s.list {
		if subtle.ConstantTimeCompare(h, []byte(d.TokenHash)) == 1 {
			found, ok = d, true
		}
	}
	return found, ok
}

// List returns the paired devices.
func (s *Store) List() ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refresh(); err != nil {
		return nil, err
	}
	return append([]Device(nil), s.list...), nil
}

// Revoke removes the device with that id or exact name.
func (s *Store) Revoke(idOrName string) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refresh(); err != nil {
		return Device{}, err
	}
	idx := -1
	for i, d := range s.list {
		if d.ID == idOrName || d.Name == idOrName {
			if idx >= 0 {
				return Device{}, fmt.Errorf("%q matches more than one device; use the id", idOrName)
			}
			idx = i
		}
	}
	if idx < 0 {
		return Device{}, ErrNotFound
	}
	gone := s.list[idx]
	next := append(append([]Device(nil), s.list[:idx]...), s.list[idx+1:]...)
	if err := s.save(next); err != nil {
		return Device{}, err
	}
	return gone, nil
}

// save writes list and makes it the loaded state. Callers hold mu.
func (s *Store) save(list []Device) error {
	if list == nil {
		list = []Device{}
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := config.WriteFileAtomic(s.path, append(b, '\n')); err != nil {
		return err
	}
	s.list = list
	if info, err := os.Stat(s.path); err == nil {
		s.size, s.mtime = info.Size(), info.ModTime()
	}
	return nil
}

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
