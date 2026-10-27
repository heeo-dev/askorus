package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"time"

	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
)

// Folders with past sessions (기록 폴더) are offered for new sessions next
// to the allowed list. The phone only ever names them by the id made here
// from a path the agent found itself, never by a path it sends.

const (
	maxHistoryFolders = 30
	historyIDPrefix   = "h-"
	sourceConfig      = "config"
	sourceHistory     = "history"
)

type historyFolder struct {
	ID         string
	Path       string
	LastActive time.Time
}

func historyID(path string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(path))))
	return historyIDPrefix + hex.EncodeToString(sum[:6])
}

// historyFolders returns the folders with past sessions that are not
// already allowed or hidden, newest first.
func (s *Server) historyFolders(ctx context.Context) []historyFolder {
	if s.o.HistoryFolders == nil || !s.o.HistoryFolders() {
		return nil
	}
	var allowed []config.Folder
	if s.o.Folders != nil {
		allowed = s.o.Folders()
	}
	var out []historyFolder
	seen := map[string]bool{}
	for _, a := range s.o.Adapters {
		fh, ok := a.(core.FolderHistory)
		if !ok {
			continue
		}
		recent, err := fh.RecentFolders(ctx)
		if err != nil {
			continue
		}
		for _, r := range recent {
			id := historyID(r.Path)
			if seen[id] || isAllowed(r.Path, allowed) || isHidden(r.Path, s.o.HiddenFolders) {
				continue
			}
			seen[id] = true
			out = append(out, historyFolder{ID: id, Path: r.Path, LastActive: r.LastActive})
			if len(out) == maxHistoryFolders {
				return out
			}
		}
	}
	return out
}

// historyInfo is the GET /v1/folders entry for a history folder: never
// served (keeping it open moves it to the allowed list).
func historyInfo(h historyFolder) FolderInfo {
	fi := FolderInfo{
		ID:     h.ID,
		Name:   filepath.Base(h.Path),
		Path:   h.Path,
		Server: config.FolderServer{Spawn: config.SpawnSameDir, Capacity: config.DefaultCapacity},
		Status: &core.ServeStatus{State: core.ServeOff},
		IsGit:  isGitRepo(h.Path),
		Source: sourceHistory,
	}
	if !h.LastActive.IsZero() {
		at := h.LastActive
		fi.LastActive = &at
	}
	return fi
}

func isAllowed(path string, allowed []config.Folder) bool {
	for _, f := range allowed {
		if strings.EqualFold(filepath.Clean(f.Path), filepath.Clean(path)) {
			return true
		}
	}
	return false
}

func isHidden(path string, hidden []string) bool {
	for _, h := range hidden {
		if core.InFolder(path, h) {
			return true
		}
	}
	return false
}

// resolveFolder finds a folder new sessions may start in: an allowed one
// by its id, or a history one by its h- id.
func (s *Server) resolveFolder(ctx context.Context, id string) (config.Folder, string, bool) {
	if f, ok := s.folderByID(id); ok {
		return f, sourceConfig, true
	}
	if !strings.HasPrefix(id, historyIDPrefix) {
		return config.Folder{}, "", false
	}
	for _, h := range s.historyFolders(ctx) {
		if h.ID == id {
			return config.Folder{ID: h.ID, Path: h.Path}, sourceHistory, true
		}
	}
	return config.Folder{}, "", false
}
