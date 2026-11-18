package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/config"
)

// fakeUpdater is the agent's side of the update as the API sees it.
type fakeUpdater struct {
	mu      sync.Mutex
	info    UpdateInfo
	status  UpdateStatus
	err     error
	started []string // device:version
	work    Work
}

func (f *fakeUpdater) Info(string) UpdateInfo { return f.info }

func (f *fakeUpdater) Status(context.Context, string) UpdateStatus { return f.status }

func (f *fakeUpdater) Start(device, version string, w Work) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.started = append(f.started, device+":"+version)
	f.work = w
	return nil
}

func updateEnv(t *testing.T, u *fakeUpdater, folders []config.Folder) env {
	return newEnv(t, fakeAdapter{}, func(o *Options) {
		if u != nil {
			o.Updater = u
		}
		o.Folders = func() []config.Folder { return folders }
	})
}

func TestUpdateRoutesNeedAnUpdater(t *testing.T) {
	e := updateEnv(t, nil, nil)
	if w := e.do("GET", "/v1/update", nil); w.Code != http.StatusNotFound || errCode(t, w) != CodeNotSupported {
		t.Fatalf("GET without an updater: %d %s", w.Code, w.Body)
	}
	if w := e.doBody("POST", "/v1/update", `{"version":"0.5.1"}`); w.Code != http.StatusNotFound {
		t.Fatalf("POST without an updater: %d", w.Code)
	}
	var info InfoResponse
	json.Unmarshal(e.do("GET", "/v1/info", nil).Body.Bytes(), &info)
	if info.Update != nil || len(info.Integrations) != 0 {
		t.Fatalf("info without an updater: %+v, %+v", info.Update, info.Integrations)
	}
	// Only the owner's paired phones reach it.
	if w := e.do("GET", "/v1/update", map[string]string{"Authorization": ""}); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", w.Code)
	}
	if w := e.do("DELETE", "/v1/update", nil); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE: %d", w.Code)
	}
}

// /v1/info says the agent takes updates, and names the update check among
// the services outside the PC while updates are on (N1, part 27).
func TestInfoSaysUpdates(t *testing.T) {
	u := &fakeUpdater{info: UpdateInfo{On: true, Can: true, Allowed: true}}
	e := updateEnv(t, u, nil)
	var info InfoResponse
	json.Unmarshal(e.do("GET", "/v1/info", nil).Body.Bytes(), &info)
	if info.Update == nil || !info.Update.On || !info.Update.Can || !info.Update.Allowed {
		t.Fatalf("update: %+v", info.Update)
	}
	if len(info.Integrations) != 1 || info.Integrations[0] != (IntegrationInfo{ID: "update", Kind: "update", Destination: "github.com/heeo-dev/askorus"}) {
		t.Fatalf("integrations: %+v", info.Integrations)
	}
	u.info = UpdateInfo{On: false, Why: "off", Allowed: true}
	json.Unmarshal(e.do("GET", "/v1/info", nil).Body.Bytes(), &info)
	if info.Update == nil || info.Update.On || info.Update.Why != "off" || len(info.Integrations) != 0 {
		t.Fatalf("off: %+v, %+v", info.Update, info.Integrations)
	}
}

func TestGetUpdateAddsTheWorkItWouldCutOff(t *testing.T) {
	u := &fakeUpdater{status: UpdateStatus{UpdateInfo: UpdateInfo{On: true, Can: true, Allowed: true}, Current: "0.5.0", Latest: "0.5.1", Newer: true}}
	e := updateEnv(t, u, []config.Folder{{ID: "a", Server: config.FolderServer{Enabled: true}}, {ID: "b"}, {ID: "c", Server: config.FolderServer{Enabled: true}}})
	w := e.do("GET", "/v1/update", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", w.Code, w.Body)
	}
	var st UpdateStatus
	json.Unmarshal(w.Body.Bytes(), &st)
	if st.Latest != "0.5.1" || !st.Newer || st.Busy.Folders != 2 || st.Busy.Asks != 0 {
		t.Fatalf("status: %+v", st)
	}
}

func TestPostUpdate(t *testing.T) {
	u := &fakeUpdater{}
	e := updateEnv(t, u, nil)
	w := e.doBody("POST", "/v1/update", `{"version":"0.5.1"}`)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"started"`) {
		t.Fatalf("POST: %d %s", w.Code, w.Body)
	}
	if len(u.started) != 1 || u.started[0] != e.device+":0.5.1" || u.work == nil {
		t.Fatalf("started: %q", u.started)
	}
	for _, body := range []string{``, `{}`, `{"version":""}`, `{"version":"` + strings.Repeat("9", 33) + `"}`, `not json`} {
		if w := e.doBody("POST", "/v1/update", body); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: %d", body, w.Code)
		}
	}
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{ErrUpdateNotAllowed, http.StatusForbidden, CodeUpdateNotAllowed},
		{ErrUpdateOff, http.StatusConflict, CodeUpdateOff},
		{ErrUpdateChanged, http.StatusConflict, CodeUpdateChanged},
		{ErrUpdateRunning, http.StatusConflict, CodeUpdateRunning},
		{errors.New("GitHub did not answer"), http.StatusBadGateway, CodeUpdateCheckFailed},
	}
	for _, c := range cases {
		u.err = c.err
		w := e.doBody("POST", "/v1/update", `{"version":"0.5.1"}`)
		if w.Code != c.status || errCode(t, w) != c.code {
			t.Errorf("%v: %d %s", c.err, w.Code, w.Body)
		}
	}
}

// While an update holds the agent, nothing new starts: asks, comparisons,
// combined answers, debates and sessions are told to try again.
func TestHoldRefusesNewWork(t *testing.T) {
	e := updateEnv(t, &fakeUpdater{}, nil)
	e.srv.Hold(true)
	for _, target := range []string{"/v1/runs", "/v1/comparisons", "/v1/comparisons/x/retry", "/v1/comparisons/x/synthesis", "/v1/debates", "/v1/sessions"} {
		w := e.doBody("POST", target, `{}`)
		if w.Code != http.StatusServiceUnavailable || errCode(t, w) != CodeUpdating || w.Header().Get("Retry-After") != "60" {
			t.Errorf("POST %s while holding: %d %s", target, w.Code, w.Body)
		}
	}
	// Reading goes on: the phone still sees its answers and the update.
	if w := e.do("GET", "/v1/info", nil); w.Code != http.StatusOK {
		t.Errorf("info while holding: %d", w.Code)
	}
	e.srv.Hold(false)
	if w := e.doBody("POST", "/v1/runs", `{}`); w.Code == http.StatusServiceUnavailable {
		t.Errorf("runs after the hold: %d %s", w.Code, w.Body)
	}
	// Nothing runs, so waiting ends at once.
	if err := e.srv.WaitIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}
