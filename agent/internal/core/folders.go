package core

import (
	"context"
	"time"
)

// RecentFolder is a folder an AI has worked in, from its own history.
type RecentFolder struct {
	Path       string
	LastActive time.Time
}

// FolderHistory is implemented by adapters that know where their past
// sessions ran. The agent offers those folders for new sessions next to
// the allowed list (기록 폴더, 2026-10-27).
type FolderHistory interface {
	// RecentFolders returns folders that still exist, newest first.
	RecentFolders(ctx context.Context) ([]RecentFolder, error)
}
