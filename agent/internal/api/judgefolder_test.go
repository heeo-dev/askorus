package api

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// Judges run in <ask folder>\judge (P2-3). Their sessions never reach the
// phone's list, whatever the filter, and the folder is never offered for
// new sessions; a sibling whose name only starts the same stays.
func TestJudgeFolderHiddenFromSessions(t *testing.T) {
	cl := &askAdapter{name: "claude", available: true}
	secret := filepath.Join(t.TempDir(), "secret")
	// Spare capacity: appending to the settings' list would write here.
	hiddenFolders := make([]string, 1, 8)
	hiddenFolders[0] = secret
	ask := filepath.Join(t.TempDir(), "ask")
	e := newEnv(t, cl, func(o *Options) {
		o.AskFolder = ask
		o.HistoryFolders = func() bool { return true }
		o.HiddenFolders = hiddenFolders
	})
	judgeDir := core.JudgeFolder(ask)
	work := filepath.Join(filepath.Dir(ask), "work")
	evalDir := filepath.Join(judgeDir, "eval")
	if runtime.GOOS == "windows" {
		evalDir = strings.ToUpper(evalDir) // paths compare without case there
	}
	now := time.Now()
	cl.sessions = []core.Session{
		{ID: "claude:ask", Agent: "claude", Folder: ask, State: core.StateHistory, LastActive: now},
		{ID: "claude:judge", Agent: "claude", Folder: judgeDir, State: core.StateTerminal, LastActive: now.Add(-time.Minute)},
		{ID: "claude:eval", Agent: "claude", Folder: evalDir, State: core.StateHistory, LastActive: now.Add(-2 * time.Minute)},
		{ID: "claude:judge-x", Agent: "claude", Folder: judgeDir + "-x", State: core.StateHistory, LastActive: now.Add(-3 * time.Minute)},
		{ID: "claude:work", Agent: "claude", Folder: work, State: core.StateHistory, LastActive: now.Add(-4 * time.Minute)},
		{ID: "claude:secret", Agent: "claude", Folder: filepath.Join(secret, "sub"), State: core.StateHistory, LastActive: now.Add(-5 * time.Minute)},
	}
	cl.recent = []core.RecentFolder{{Path: judgeDir}, {Path: evalDir}, {Path: ask}, {Path: judgeDir + "-x"}, {Path: work}}

	list := func(target string) []string {
		t.Helper()
		w := e.do("GET", target, nil)
		if w.Code != 200 {
			t.Fatalf("GET %s = %d %s", target, w.Code, w.Body)
		}
		var resp SessionsResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, s := range resp.Sessions {
			ids = append(ids, s.ID)
		}
		return ids
	}
	for target, want := range map[string][]string{
		"/v1/sessions": {"claude:ask", "claude:judge-x", "claude:work"},
		"/v1/sessions?folder=" + url.QueryEscape(ask):      {"claude:ask", "claude:judge-x"},
		"/v1/sessions?folder=" + url.QueryEscape(judgeDir): nil,
		"/v1/sessions?folder=" + url.QueryEscape(evalDir):  nil,
		"/v1/sessions?state=terminal":                      nil,
	} {
		if got := list(target); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", target, got, want)
		}
	}
	if extra := hiddenFolders[:cap(hiddenFolders)][1:]; slices.ContainsFunc(extra, func(s string) bool { return s != "" }) {
		t.Errorf("the settings' hidden folders were appended to: %q", extra)
	}

	var folders struct {
		Folders []FolderInfo `json:"folders"`
	}
	json.Unmarshal(e.do("GET", "/v1/folders", nil).Body.Bytes(), &folders)
	if len(folders.Folders) != 1 || folders.Folders[0].Path != work {
		t.Errorf("folders = %+v: neither the ask nor the judge folder may be offered", folders.Folders)
	}
}

// Without an ask folder there is no judge folder to hide, and the list is
// the settings' hidden folders only.
func TestJudgeFolderNoAskFolder(t *testing.T) {
	cl := &askAdapter{name: "claude", available: true}
	dir := t.TempDir()
	cl.sessions = []core.Session{
		{ID: "claude:judge", Agent: "claude", Folder: filepath.Join(dir, "ask", core.JudgeFolderName), State: core.StateHistory, LastActive: time.Now()},
	}
	e := newEnv(t, cl, nil)
	var resp SessionsResponse
	json.Unmarshal(e.do("GET", "/v1/sessions", nil).Body.Bytes(), &resp)
	if len(resp.Sessions) != 1 {
		t.Errorf("sessions = %+v", resp.Sessions)
	}
}
