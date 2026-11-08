package claude

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

func TestOpenSessionWhoseFolderIsGone(t *testing.T) {
	e := newLaunchEnv(t, nil)
	if err := os.RemoveAll(e.folder); err != nil {
		t.Fatal(err)
	}
	_, err := e.a.Open(context.Background(), id6)
	if core.ErrorCode(err) != core.ErrCodeFolderMissing {
		t.Fatalf("err = %v", err)
	}
	if got := e.launches(t); len(got) != 0 {
		t.Errorf("ran claude anyway: %+v", got)
	}
}

func TestListMarksMissingFolders(t *testing.T) {
	ss := []core.Session{
		{ID: "claude:" + id1, State: core.StateHistory, Folder: `D:\gone`, OpenModes: []string{core.OpenModeOfficial}, AttachCommand: "claude --resume x"},
		{ID: "claude:" + id2, State: core.StateStopped, Folder: `D:\gone`, OpenModes: []string{core.OpenModeOfficial}},
		{ID: "claude:" + id5, State: core.StateRunning, Folder: `D:\gone`, OpenModes: []string{core.OpenModeOfficial}},
		{ID: "claude:" + id6, State: core.StateHistory, Folder: `D:\here`, OpenModes: []string{core.OpenModeOfficial}},
	}
	checks := 0
	markMissingFolders(ss, func(p string) bool { checks++; return p == `D:\here` })
	if checks != 2 {
		t.Errorf("checked %d times, want once per folder", checks)
	}
	for i, want := range []bool{true, true, false, false} {
		got := slices.Contains(ss[i].Badges, core.BadgeFolderMissing)
		if got != want || (len(ss[i].OpenModes) == 0) != want {
			t.Errorf("session %d: badge %v, open modes %v", i, got, ss[i].OpenModes)
		}
	}
	if ss[0].AttachCommand != "" {
		t.Error("attach command kept for a session that cannot resume")
	}
	b, _ := json.Marshal(ss[0])
	if !strings.Contains(string(b), `"openModes":[]`) {
		t.Errorf("openModes must be an empty list, got %s", b)
	}
}

func TestListSessionsFlagsGoneFolder(t *testing.T) {
	e := newLaunchEnv(t, nil)
	os.RemoveAll(e.folder)
	res, err := e.a.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Sessions {
		if !slices.Contains(s.Badges, core.BadgeFolderMissing) {
			t.Errorf("%s (%s) not flagged", s.ID, s.State)
		}
	}
}
