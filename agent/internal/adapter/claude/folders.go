package claude

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// RecentFolders lists the folders Claude Code sessions ran in, newest
// first, from the transcripts already read for the session list. Folders
// that are gone and Claude Code worktrees (short-lived copies under
// .claude/worktrees) are left out.
func (a *Adapter) RecentFolders(ctx context.Context) ([]core.RecentFolder, error) {
	ts, err := a.transcripts.Scan()
	if err != nil {
		return nil, err
	}
	latest := map[string]time.Time{}
	shown := map[string]string{} // lower-cased key -> path as written
	for _, t := range ts {
		if t.Cwd == "" || !filepath.IsAbs(t.Cwd) || isWorktree(t.Cwd) {
			continue
		}
		p := filepath.Clean(t.Cwd)
		k := strings.ToLower(p) // Windows paths differ only in case
		if t.LastActive.After(latest[k]) {
			latest[k] = t.LastActive
		}
		if _, ok := shown[k]; !ok {
			shown[k] = p
		}
	}
	out := make([]core.RecentFolder, 0, len(latest))
	for k, at := range latest {
		if a.folderExists(shown[k]) {
			out = append(out, core.RecentFolder{Path: shown[k], LastActive: at})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastActive.After(out[j].LastActive) })
	return out, nil
}

func isWorktree(p string) bool {
	return strings.Contains(filepath.ToSlash(strings.ToLower(p)), "/.claude/worktrees/")
}
