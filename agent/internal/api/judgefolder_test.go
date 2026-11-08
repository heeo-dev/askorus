package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/hidden"
)

// The one-off calls (combined answers, refining) run in <ask folder>\judge
// (P2-3). Their sessions never reach the phone's list, whatever the
// filter, and the folder is never offered for new sessions; a sibling
// whose name only starts the same stays.
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

// previewLauncher is a fakeLauncher that previews too, and records it.
type previewLauncher struct {
	fakeLauncher
	previews *[]string
}

func (p previewLauncher) Preview(_ context.Context, id string) (core.Preview, error) {
	*p.previews = append(*p.previews, id)
	return core.Preview{Turns: []core.Turn{{Role: "user", Text: "q"}}}, nil
}

const (
	judgeSid = "claude:aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" // ran in the judge folder
	evalSid  = "claude:bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb" // ran in a folder inside it
)

// judgeFolderEnv serves a launcher whose sessions include ones of the
// judge folder.
func judgeFolderEnv(t *testing.T, listErr error, askFolder bool) (e env, calls, previews *[]string, hid *hidden.Store) {
	t.Helper()
	calls, previews = &[]string{}, &[]string{}
	hid, err := hidden.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ask := filepath.Join(t.TempDir(), "ask")
	judgeDir := core.JudgeFolder(ask)
	evalDir := filepath.Join(judgeDir, "eval")
	if runtime.GOOS == "windows" {
		evalDir = strings.ToUpper(evalDir) // paths compare without case there
	}
	l := previewLauncher{
		fakeLauncher: fakeLauncher{
			fakeAdapter: fakeAdapter{err: listErr, sessions: []core.Session{
				{ID: judgeSid, Agent: "claude", Folder: judgeDir, State: core.StateHistory},
				{ID: evalSid, Agent: "claude", Folder: evalDir, State: core.StateHistory},
				{ID: sid, Agent: "claude", Folder: ask, State: core.StateHistory}, // an ask: the person's own
			}},
			calls: calls, starts: &[]core.StartRequest{},
		},
		previews: previews,
	}
	e = newEnv(t, l, func(o *Options) {
		o.Hidden = hid
		if askFolder {
			o.AskFolder = ask
		}
	})
	return e, calls, previews, hid
}

// A session of the judge folder (a combined answer's or a refining's) is
// never the person's: open, stop, remove and preview answer 404
// session_not_found as for a session that does not exist, and the adapter
// is never asked to act on it (P2-3, S3 decision 9).
func TestJudgeFolderSessionsRefused(t *testing.T) {
	e, calls, previews, hid := judgeFolderEnv(t, nil, true)
	for _, id := range []string{judgeSid, evalSid, strings.ToUpper(judgeSid[:7]) + judgeSid[7:], "claude:" + strings.ToUpper(judgeSid[7:])} {
		for _, req := range [][2]string{
			{"POST", "/v1/sessions/" + id + "/open"},
			{"POST", "/v1/sessions/" + id + "/stop"},
			{"DELETE", "/v1/sessions/" + id},
			{"GET", "/v1/sessions/" + id + "/preview"},
		} {
			w := e.doBody(req[0], req[1], "")
			if w.Code == 400 && errCode(t, w) == CodeBadSessionID {
				continue // a spelling the id check refuses first is refused too
			}
			if w.Code != 404 || errCode(t, w) != core.ErrCodeNotFound {
				t.Errorf("%s %s = %d %s", req[0], req[1], w.Code, w.Body)
			}
		}
		if hid.Set()[id] {
			t.Errorf("%s was hidden", id)
		}
	}
	if len(*calls) != 0 || len(*previews) != 0 {
		t.Fatalf("the adapter acted on a judge folder session: calls %v, previews %v", *calls, *previews)
	}
	// The person's own ask session still works.
	if st := e.startAndWait(t, "POST", "/v1/sessions/"+sid+"/open", ""); st.Status != "done" {
		t.Errorf("open = %+v", st)
	}
	for _, req := range [][2]string{
		{"POST", "/v1/sessions/" + sid + "/stop"},
		{"GET", "/v1/sessions/" + sid + "/preview"},
		{"DELETE", "/v1/sessions/" + sid},
	} {
		if w := e.doBody(req[0], req[1], ""); w.Code != 200 {
			t.Errorf("%s %s = %d %s", req[0], req[1], w.Code, w.Body)
		}
	}
	native := strings.TrimPrefix(sid, "claude:")
	if got := strings.Join(*calls, ","); got != "open "+native+",stop "+native+",remove "+native || len(*previews) != 1 {
		t.Errorf("calls %s, previews %v", got, *previews)
	}
}

// The check is a second line of defence: when the list fails the action
// goes on, and the adapter still checks the id itself. Without an ask
// folder there is no judge folder to refuse.
func TestJudgeFolderSessionsListFailsOrNoAskFolder(t *testing.T) {
	for name, env := range map[string]struct {
		listErr error
		ask     bool
	}{
		"list fails":    {errors.New("disk"), true},
		"no ask folder": {nil, false},
	} {
		e, calls, previews, _ := judgeFolderEnv(t, env.listErr, env.ask)
		if w := e.doBody("POST", "/v1/sessions/"+judgeSid+"/stop", ""); w.Code != 200 {
			t.Errorf("%s: stop = %d %s", name, w.Code, w.Body)
		}
		if w := e.doBody("GET", "/v1/sessions/"+judgeSid+"/preview", ""); w.Code != 200 {
			t.Errorf("%s: preview = %d %s", name, w.Code, w.Body)
		}
		if len(*calls) != 1 || len(*previews) != 1 {
			t.Errorf("%s: calls %v, previews %v", name, *calls, *previews)
		}
	}
}
