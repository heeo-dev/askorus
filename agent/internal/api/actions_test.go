package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/hidden"
)

// fakeLauncher is an adapter that records actions.
type fakeLauncher struct {
	fakeAdapter
	calls  *[]string
	err    error
	starts *[]core.StartRequest
}

func (f fakeLauncher) Open(_ context.Context, id string) (core.OpenResult, error) {
	*f.calls = append(*f.calls, "open "+id)
	if f.err != nil {
		return core.OpenResult{}, f.err
	}
	return core.OpenResult{Session: core.Session{ID: "claude:" + id, Agent: "claude", State: core.StateRunning}, RemoteURL: "https://claude.ai/code/session_x", Action: core.ActionResumed}, nil
}
func (f fakeLauncher) Start(_ context.Context, req core.StartRequest) (core.OpenResult, error) {
	*f.starts = append(*f.starts, req)
	return core.OpenResult{Action: core.ActionStarted, RemoteURL: "https://claude.ai/code/session_y"}, f.err
}
func (f fakeLauncher) Stop(_ context.Context, id string) (core.Session, error) {
	*f.calls = append(*f.calls, "stop "+id)
	return core.Session{ID: "claude:" + id, State: core.StateStopped}, f.err
}
func (f fakeLauncher) Remove(_ context.Context, id string) error {
	*f.calls = append(*f.calls, "remove "+id)
	return f.err
}

const sid = "claude:11111111-1111-4111-8111-111111111111"

func newActionEnv(t *testing.T, launchErr error) (env, *[]string, *[]core.StartRequest, *hidden.Store) {
	t.Helper()
	calls, starts := &[]string{}, &[]core.StartRequest{}
	hid, err := hidden.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	folders := []config.Folder{{ID: "app", Path: t.TempDir(), Server: config.FolderServer{Spawn: "same-dir", Capacity: 4}}}
	l := fakeLauncher{
		fakeAdapter: fakeAdapter{sessions: []core.Session{{ID: sid, Agent: "claude", Title: "one", OpenModes: []string{"official"}}}},
		calls:       calls, err: launchErr, starts: starts,
	}
	e := newEnv(t, l, func(o *Options) {
		o.Hidden = hid
		o.Folders = func() []config.Folder { return folders }
	})
	return e, calls, starts, hid
}

func (e env) doBody(method, target, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Host = "127.0.0.1:8790"
	r.Header.Set("Authorization", "Bearer "+e.token)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

// startAndWait sends a slow action, checks the 202 and polls the job
// like the phone does.
func (e env) startAndWait(t *testing.T, method, target, body string) JobStatus {
	t.Helper()
	w := e.doBody(method, target, body)
	if w.Code != 202 {
		t.Fatalf("%s %s = %d %s, want 202", method, target, w.Code, w.Body)
	}
	var started JobStarted
	json.Unmarshal(w.Body.Bytes(), &started)
	if len(started.JobID) != 32 || started.Status != "running" || started.PollAfterMs <= 0 {
		t.Fatalf("started = %+v", started)
	}
	for i := 0; i < 500; i++ {
		w := e.doBody("GET", "/v1/jobs/"+started.JobID, "")
		if w.Code != 200 {
			t.Fatalf("job = %d %s", w.Code, w.Body)
		}
		var st JobStatus
		json.Unmarshal(w.Body.Bytes(), &st)
		if st.Status != "running" {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job still running")
	return JobStatus{}
}

func TestOpenStopRemove(t *testing.T) {
	e, calls, _, hid := newActionEnv(t, nil)
	st := e.startAndWait(t, "POST", "/v1/sessions/"+sid+"/open", "")
	if st.Status != "done" || st.Kind != "open" || st.Result == nil || st.Result.RemoteURL == "" || st.Result.Action != core.ActionResumed {
		t.Fatalf("open job = %+v", st)
	}
	// Browsers send the colon escaped (encodeURIComponent).
	if w := e.doBody("POST", "/v1/sessions/"+strings.Replace(sid, ":", "%3A", 1)+"/stop", ""); w.Code != 200 {
		t.Errorf("stop = %d %s", w.Code, w.Body)
	}
	if w := e.doBody("DELETE", "/v1/sessions/"+sid, ""); w.Code != 200 {
		t.Errorf("delete = %d %s", w.Code, w.Body)
	}
	native := strings.TrimPrefix(sid, "claude:")
	if got := strings.Join(*calls, ","); got != "open "+native+",stop "+native+",remove "+native {
		t.Errorf("calls = %s", got)
	}
	if !hid.Set()[sid] {
		t.Error("removed session not hidden")
	}
	if w := e.doBody("GET", "/v1/sessions", ""); strings.Contains(w.Body.String(), sid) {
		t.Error("hidden session still listed")
	}
}

func TestActionErrorsMapToStatus(t *testing.T) {
	cases := map[string]int{
		core.ErrCodeNotFound:      404,
		core.ErrCodeBusy:          409,
		core.ErrCodeUntrusted:     412,
		core.ErrCodeLinkTimeout:   504,
		core.ErrCodeLaunchFailed:  502,
		core.ErrCodeStateUnknown:  503,
		core.ErrCodeNotBackground: 409,
	}
	for code, status := range cases {
		e, _, _, hid := newActionEnv(t, core.Fail(code, "detail for "+code))
		st := e.startAndWait(t, "POST", "/v1/sessions/"+sid+"/open", "")
		if st.Status != "failed" || st.Error == nil || st.Error.Code != code || st.Error.Message != "detail for "+code {
			t.Errorf("%s: job = %+v", code, st)
		}
		if w := e.doBody("DELETE", "/v1/sessions/"+sid, ""); w.Code != status || hid.Set()[sid] {
			t.Errorf("%s: failed remove still hid the session (%d)", code, w.Code)
		}
	}
}

func TestBadSessionIDs(t *testing.T) {
	e, calls, _, _ := newActionEnv(t, nil)
	cases := map[string]int{
		"/v1/sessions/nocolon/open":      400,
		"/v1/sessions/Bad:1/open":        400,
		"/v1/sessions/codex:abc/open":    404,
		"/v1/sessions/" + sid + "/fly":   405,
		"/v1/sessions/" + sid + "/open/": 404,
	}
	for path, status := range cases {
		if w := e.doBody("POST", path, ""); w.Code != status {
			t.Errorf("%s = %d %s", path, w.Code, w.Body)
		}
	}
	if w := e.doBody("GET", "/v1/sessions/"+sid+"/open", ""); w.Code != 405 {
		t.Errorf("GET open = %d", w.Code)
	}
	if len(*calls) != 0 {
		t.Errorf("bad requests reached the adapter: %v", *calls)
	}
}

func TestStartSession(t *testing.T) {
	e, _, starts, _ := newActionEnv(t, nil)
	st := e.startAndWait(t, "POST", "/v1/sessions", `{"folder":"app","name":"fix login","model":"opus","effort":"high"}`)
	if st.Status != "done" || st.Kind != "start" || st.Result == nil || st.Result.RemoteURL == "" {
		t.Fatalf("start job = %+v", st)
	}
	if len(*starts) != 1 || (*starts)[0].Name != "fix login" || (*starts)[0].Model != "opus" || (*starts)[0].Folder == "" {
		t.Errorf("start request = %+v", *starts)
	}
	bad := map[string]int{
		`{"folder":"elsewhere","name":"x"}`:                  404,
		`{"folder":"app","model":"opus; rm"}`:                400,
		`{"folder":"app","effort":"turbo"}`:                  400,
		`{"folder":"app","model":"--dangerously-skip-perm"}`: 400,
		`not json`: 400,
	}
	for body, status := range bad {
		if w := e.doBody("POST", "/v1/sessions", body); w.Code != status {
			t.Errorf("%s = %d %s", body, w.Code, w.Body)
		}
	}
	if len(*starts) != 1 {
		t.Errorf("bad requests started sessions: %+v", *starts)
	}
}

// Jobs belong to the device that started them, and unknown ids are 404.
func TestJobsPerDevice(t *testing.T) {
	e, _, _, _ := newActionEnv(t, nil)
	w := e.doBody("POST", "/v1/sessions/"+sid+"/open", "")
	var started JobStarted
	json.Unmarshal(w.Body.Bytes(), &started)

	other := e
	other.token = e.addDevice(t, "other phone")
	if w := other.doBody("GET", "/v1/jobs/"+started.JobID, ""); w.Code != 404 || errCode(t, w) != CodeJobNotFound {
		t.Errorf("other device reads the job: %d %s", w.Code, w.Body)
	}
	if w := e.doBody("GET", "/v1/jobs/"+strings.Repeat("0", 32), ""); w.Code != 404 {
		t.Errorf("unknown job = %d", w.Code)
	}
	if w := e.doBody("POST", "/v1/jobs/"+started.JobID, ""); w.Code != 405 {
		t.Errorf("POST job = %d", w.Code)
	}
}

// fakeServing adds folder servers to fakeLauncher.
type fakeServing struct {
	fakeLauncher
}

func (fakeServing) StartServer(string, core.ServeOptions) error { return nil }
func (fakeServing) StopServer(string) error                     { return nil }
func (fakeServing) ServerStatus(string) core.ServeStatus {
	return core.ServeStatus{State: core.ServeRunning, Sessions: 1, Capacity: 4}
}
func (fakeServing) ServerFolders() []string { return nil }
func (fakeServing) StopAllServers()         {}

func TestFolderServerSetting(t *testing.T) {
	gitDir := t.TempDir()
	os.Mkdir(filepath.Join(gitDir, ".git"), 0o700)
	folders := []config.Folder{
		{ID: "app", Path: t.TempDir(), Server: config.FolderServer{Spawn: "same-dir", Capacity: 4}},
		{ID: "repo", Path: gitDir, Server: config.FolderServer{Spawn: "same-dir", Capacity: 4}},
	}
	applied := 0
	l := fakeServing{fakeLauncher{calls: &[]string{}, starts: &[]core.StartRequest{}}}
	e := newEnv(t, l, func(o *Options) {
		o.Folders = func() []config.Folder { return folders }
		o.SetFolderServer = func(id string, fs config.FolderServer) (config.Folder, error) {
			for i := range folders {
				if folders[i].ID == id {
					folders[i].Server = fs
					return folders[i], nil
				}
			}
			return config.Folder{}, core.Fail(core.ErrCodeFolderNotFound, "")
		}
		o.ApplyFolders = func() { applied++ }
	})

	w := e.doBody("PUT", "/v1/folders/app/server", `{"enabled":true,"capacity":2}`)
	var fi FolderInfo
	json.Unmarshal(w.Body.Bytes(), &fi)
	if w.Code != 200 || !fi.Server.Enabled || fi.Server.Capacity != 2 || fi.Server.Spawn != "same-dir" || fi.Status == nil || fi.Status.State != core.ServeRunning {
		t.Fatalf("put = %d %s", w.Code, w.Body)
	}
	if !folders[0].Server.Enabled || applied != 1 {
		t.Errorf("not saved or applied: %+v, applied %d", folders[0].Server, applied)
	}
	if w := e.doBody("PUT", "/v1/folders/repo/server", `{"enabled":true,"spawn":"worktree"}`); w.Code != 200 {
		t.Errorf("worktree in a git repo = %d %s", w.Code, w.Body)
	}
	bad := map[string]struct {
		path, body string
		status     int
		code       string
	}{
		"worktree outside git": {"/v1/folders/app/server", `{"enabled":true,"spawn":"worktree"}`, 400, CodeNotGit},
		"spawn":                {"/v1/folders/app/server", `{"enabled":true,"spawn":"anywhere"}`, 400, CodeBadRequest},
		"capacity":             {"/v1/folders/app/server", `{"enabled":true,"capacity":99}`, 400, CodeBadRequest},
		"unknown folder":       {"/v1/folders/nope/server", `{"enabled":true}`, 404, CodeFolderUnknown},
		"bad json":             {"/v1/folders/app/server", `{`, 400, CodeBadRequest},
	}
	for name, c := range bad {
		w := e.doBody("PUT", c.path, c.body)
		if w.Code != c.status || errCode(t, w) != c.code {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	if applied != 2 {
		t.Errorf("bad requests applied settings: %d", applied)
	}
	w = e.doBody("GET", "/v1/folders", "")
	var resp struct{ Folders []FolderInfo }
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Folders) != 2 || resp.Folders[0].IsGit || !resp.Folders[1].IsGit || resp.Folders[1].Status == nil {
		t.Errorf("folders = %s", w.Body)
	}
}

func TestFolderServerNeedsSupport(t *testing.T) {
	e, _, _, _ := newActionEnv(t, nil) // fakeLauncher cannot serve folders
	if w := e.doBody("PUT", "/v1/folders/app/server", `{"enabled":true}`); w.Code != 501 {
		t.Errorf("put without server support = %d", w.Code)
	}
}

func TestFolders(t *testing.T) {
	e, _, _, _ := newActionEnv(t, nil)
	w := e.doBody("GET", "/v1/folders", "")
	var resp struct{ Folders []FolderInfo }
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != 200 || len(resp.Folders) != 1 || resp.Folders[0].ID != "app" || resp.Folders[0].Server.Capacity != 4 {
		t.Errorf("folders = %d %s", w.Code, w.Body)
	}
}

func TestActionsNeedToken(t *testing.T) {
	e, calls, _, _ := newActionEnv(t, nil)
	r := httptest.NewRequest("POST", "/v1/sessions/"+sid+"/open", nil)
	r.Host = "127.0.0.1:8790"
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != 401 || len(*calls) != 0 {
		t.Errorf("open without token = %d, calls %v", w.Code, *calls)
	}
}
