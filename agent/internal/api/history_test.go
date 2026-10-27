package api

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/hidden"
)

// historyLauncher is a launcher that also knows where past sessions ran.
type historyLauncher struct {
	fakeLauncher
	recent []core.RecentFolder
}

func (h historyLauncher) RecentFolders(context.Context) ([]core.RecentFolder, error) {
	return h.recent, nil
}

func (historyLauncher) StartServer(string, core.ServeOptions) error { return nil }
func (historyLauncher) StopServer(string) error                     { return nil }
func (historyLauncher) ServerStatus(string) core.ServeStatus {
	return core.ServeStatus{State: core.ServeOff}
}
func (historyLauncher) ServerFolders() []string { return nil }
func (historyLauncher) StopAllServers()         {}

type historyEnv struct {
	env
	starts  *[]core.StartRequest
	allowed *[]config.Folder
	on      *bool
	added   *[]string
	app     string // the allowed folder
	recent  []string
}

func newHistoryEnv(t *testing.T) historyEnv {
	t.Helper()
	app, old, recent, hiddenDir := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	calls, starts := &[]string{}, &[]core.StartRequest{}
	hid, _ := hidden.Open(t.TempDir())
	now := time.Now()
	l := historyLauncher{
		fakeLauncher: fakeLauncher{calls: calls, starts: starts},
		recent: []core.RecentFolder{
			{Path: recent, LastActive: now},
			{Path: strings.ToUpper(app), LastActive: now.Add(-time.Hour)}, // allowed already, other case
			{Path: hiddenDir, LastActive: now.Add(-2 * time.Hour)},
			{Path: old, LastActive: now.Add(-48 * time.Hour)},
		},
	}
	allowed := &[]config.Folder{{ID: "app", Path: app, Server: config.FolderServer{Spawn: "same-dir", Capacity: 4}}}
	on, added := new(bool), &[]string{}
	*on = true
	e := newEnv(t, l, func(o *Options) {
		o.Hidden = hid
		o.HiddenFolders = []string{hiddenDir}
		o.Folders = func() []config.Folder { return *allowed }
		o.HistoryFolders = func() bool { return *on }
		o.AddFolder = func(path string) (config.Folder, error) {
			*added = append(*added, path)
			f := config.Folder{ID: filepath.Base(path), Path: path, Server: config.FolderServer{Spawn: "same-dir", Capacity: 4}}
			*allowed = append(*allowed, f)
			return f, nil
		}
		o.SetFolderServer = func(id string, fs config.FolderServer) (config.Folder, error) {
			for i, f := range *allowed {
				if f.ID == id {
					(*allowed)[i].Server = fs
					return (*allowed)[i], nil
				}
			}
			return config.Folder{}, core.Fail(core.ErrCodeFolderNotFound, "")
		}
	})
	return historyEnv{env: e, starts: starts, allowed: allowed, on: on, added: added, app: app, recent: []string{recent, old}}
}

func (h historyEnv) folders(t *testing.T) []FolderInfo {
	t.Helper()
	w := h.doBody("GET", "/v1/folders", "")
	if w.Code != 200 {
		t.Fatalf("folders = %d %s", w.Code, w.Body)
	}
	var resp struct{ Folders []FolderInfo }
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp.Folders
}

func TestFoldersIncludeHistory(t *testing.T) {
	h := newHistoryEnv(t)
	got := h.folders(t)
	if len(got) != 3 {
		t.Fatalf("folders = %+v, want the allowed one and two history ones", got)
	}
	if got[0].ID != "app" || got[0].Source != sourceConfig {
		t.Errorf("first = %+v", got[0])
	}
	for i, want := range h.recent {
		f := got[i+1]
		if f.Source != sourceHistory || f.Path != want || !strings.HasPrefix(f.ID, "h-") || f.LastActive == nil || f.Server.Enabled {
			t.Errorf("history %d = %+v", i, f)
		}
	}
	if got[1].ID == got[2].ID || got[1].ID != historyID(h.recent[0]) {
		t.Error("history ids are not stable and distinct")
	}
}

func TestHistoryFoldersCanBeTurnedOff(t *testing.T) {
	h := newHistoryEnv(t)
	*h.on = false
	if got := h.folders(t); len(got) != 1 {
		t.Errorf("folders = %+v", got)
	}
	if w := h.doBody("POST", "/v1/sessions", `{"folder":"`+historyID(h.recent[0])+`","name":"x"}`); w.Code != 404 {
		t.Errorf("start in a history folder while off = %d", w.Code)
	}
}

func TestStartInHistoryFolder(t *testing.T) {
	h := newHistoryEnv(t)
	st := h.startAndWait(t, "POST", "/v1/sessions", `{"folder":"`+historyID(h.recent[1])+`","name":"x"}`)
	if st.Status != "done" || len(*h.starts) != 1 || (*h.starts)[0].Folder != h.recent[1] {
		t.Fatalf("job %+v, starts %+v", st, *h.starts)
	}
}

// The phone can only name folders by id; a path, an unknown h- id or a
// hidden folder's id start nothing.
func TestHistoryFoldersRefuseAnythingElse(t *testing.T) {
	h := newHistoryEnv(t)
	for _, folder := range []string{
		jsonQuote(h.recent[0]),
		`"h-000000000000"`,
		`"` + historyID(t.TempDir()) + `"`,
	} {
		if w := h.doBody("POST", "/v1/sessions", `{"folder":`+folder+`,"name":"x"}`); w.Code != 404 {
			t.Errorf("folder %s = %d %s", folder, w.Code, w.Body)
		}
	}
	if len(*h.starts) != 0 {
		t.Errorf("started %+v", *h.starts)
	}
}

func TestKeepingHistoryFolderOpenAllowsIt(t *testing.T) {
	h := newHistoryEnv(t)
	id := historyID(h.recent[0])

	// Turning it off is a no-op and saves nothing.
	if w := h.doBody("PUT", "/v1/folders/"+id+"/server", `{"enabled":false}`); w.Code != 200 || len(*h.added) != 0 {
		t.Fatalf("off = %d %s, added %v", w.Code, w.Body, *h.added)
	}

	w := h.doBody("PUT", "/v1/folders/"+id+"/server", `{"enabled":true}`)
	if w.Code != 200 {
		t.Fatalf("on = %d %s", w.Code, w.Body)
	}
	var fi FolderInfo
	json.Unmarshal(w.Body.Bytes(), &fi)
	if fi.Source != sourceConfig || !fi.Server.Enabled || fi.Path != h.recent[0] {
		t.Errorf("answer = %+v", fi)
	}
	if len(*h.added) != 1 || (*h.added)[0] != h.recent[0] {
		t.Errorf("added = %v", *h.added)
	}
	// Now it is an allowed folder, no longer listed as history.
	for _, f := range h.folders(t) {
		if f.Path == h.recent[0] && f.Source != sourceConfig {
			t.Errorf("still history: %+v", f)
		}
	}
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
