package api

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/hidden"
)

// R-101 (#43): wrong requests lock out the sender, and past the total
// everyone, but never a request that carries a paired phone's token. A
// program on the PC could otherwise keep the phone out by piling up wrong
// requests.
func TestRightTokenIsNeverLockedOut(t *testing.T) {
	e := newEnv(t, fakeAdapter{}, nil)
	const addr = "100.64.0.20"
	bad := map[string]string{"Authorization": "Bearer ask_guess", hdrCallerIP: addr}
	for i := 0; i < maxFails; i++ {
		if w := e.do("GET", "/v1/info", bad); w.Code != 401 {
			t.Fatalf("try %d = %d", i, w.Code)
		}
	}
	// The address is locked: a wrong token and no token are refused...
	if w := e.do("GET", "/v1/info", bad); w.Code != 429 || errCode(t, w) != CodeTooMany {
		t.Errorf("wrong token from a locked address = %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/v1/info", map[string]string{"Authorization": "", hdrCallerIP: addr}); w.Code != 429 {
		t.Errorf("no token from a locked address = %d %s", w.Code, w.Body)
	}
	// ...and the paired phone at the same address goes on working.
	if w := e.do("GET", "/v1/info", map[string]string{hdrCallerIP: addr}); w.Code != 200 {
		t.Errorf("right token from a locked address = %d %s", w.Code, w.Body)
	}

	// Past the total, every address is locked; the right token still works.
	for i := 0; i < maxTotalFails; i++ {
		e.do("GET", "/v1/info", map[string]string{"Authorization": "Bearer ask_guess", hdrCallerIP: "100.64.1." + strconv.Itoa(i)})
	}
	if w := e.do("GET", "/v1/info", map[string]string{"Authorization": "Bearer ask_guess", hdrCallerIP: "100.64.9.9"}); w.Code != 429 {
		t.Errorf("wrong token under the total lock = %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/v1/info", map[string]string{hdrCallerIP: "100.64.9.9"}); w.Code != 200 {
		t.Errorf("right token under the total lock = %d %s", w.Code, w.Body)
	}
	// Refused requests are not counted again: the lock does not grow.
	if strings.Contains(e.logs.String(), "ask_guess") {
		t.Error("a token reached the log")
	}
}

// R-103 (#45): only maxFolderServers folders can be kept open. Changing or
// turning off one that is on is still allowed at the limit.
func TestFolderServerLimit(t *testing.T) {
	var folders []config.Folder
	for i := 0; i <= maxFolderServers; i++ {
		folders = append(folders, config.Folder{ID: "f" + strconv.Itoa(i), Path: t.TempDir(),
			Server: config.FolderServer{Enabled: i < maxFolderServers, Spawn: "same-dir", Capacity: 4}})
	}
	last := "f" + strconv.Itoa(maxFolderServers)
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
	})
	w := e.doBody("PUT", "/v1/folders/"+last+"/server", `{"enabled":true}`)
	if w.Code != 409 || errCode(t, w) != CodeTooManyServers {
		t.Fatalf("one more than the limit = %d %s", w.Code, w.Body)
	}
	if folders[maxFolderServers].Server.Enabled {
		t.Fatal("the refused folder was saved as kept open")
	}
	// One that is on can still be changed, and turned off.
	if w := e.doBody("PUT", "/v1/folders/f0/server", `{"enabled":true,"capacity":2}`); w.Code != 200 {
		t.Errorf("change at the limit = %d %s", w.Code, w.Body)
	}
	if w := e.doBody("PUT", "/v1/folders/f0/server", `{"enabled":false}`); w.Code != 200 {
		t.Errorf("off at the limit = %d %s", w.Code, w.Body)
	}
	// With one off there is room again.
	if w := e.doBody("PUT", "/v1/folders/"+last+"/server", `{"enabled":true}`); w.Code != 200 {
		t.Errorf("after one was turned off = %d %s", w.Code, w.Body)
	}
}

// R-104 (#46): a session of a hidden folder is refused by id like one that
// does not exist, and the adapter is never asked to act on it. A session
// elsewhere still works.
func TestHiddenFolderSessionsRefused(t *testing.T) {
	calls, previews := &[]string{}, &[]string{}
	hid, err := hidden.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "secret")
	open := t.TempDir()
	const hiddenSid = "claude:cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	l := previewLauncher{
		fakeLauncher: fakeLauncher{
			fakeAdapter: fakeAdapter{sessions: []core.Session{
				{ID: hiddenSid, Agent: "claude", Folder: filepath.Join(secret, "sub"), State: core.StateHistory},
				{ID: sid, Agent: "claude", Folder: open, State: core.StateHistory},
			}},
			calls: calls, starts: &[]core.StartRequest{},
		},
		previews: previews,
	}
	e := newEnv(t, l, func(o *Options) {
		o.Hidden = hid
		o.HiddenFolders = []string{secret}
	})
	for _, req := range [][2]string{
		{"POST", "/v1/sessions/" + hiddenSid + "/open"},
		{"POST", "/v1/sessions/" + hiddenSid + "/stop"},
		{"DELETE", "/v1/sessions/" + hiddenSid},
		{"GET", "/v1/sessions/" + hiddenSid + "/preview"},
	} {
		w := e.doBody(req[0], req[1], "")
		if w.Code != 404 || errCode(t, w) != core.ErrCodeNotFound {
			t.Errorf("%s %s = %d %s", req[0], req[1], w.Code, w.Body)
		}
	}
	if len(*calls) != 0 || len(*previews) != 0 || hid.Set()[hiddenSid] {
		t.Fatalf("the adapter acted on a hidden folder's session: calls %v, previews %v", *calls, *previews)
	}
	if w := e.doBody("GET", "/v1/sessions/"+sid+"/preview", ""); w.Code != 200 {
		t.Errorf("a session elsewhere = %d %s", w.Code, w.Body)
	}
}
