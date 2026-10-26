package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
)

func TestServiceElevated(t *testing.T) {
	dir := t.TempDir()
	lock := `{"pid":4321,"procStartFt":134374641897462426,"version":"2.1.310","origin":"transient"}`
	os.WriteFile(filepath.Join(dir, "daemon.lock"), []byte(lock), 0o600)
	alive := func(pid int, start string) bool { return pid == 4321 && start == "134374641897462426" }
	cases := []struct {
		name     string
		alive    func(int, string) bool
		elevated func(int) (bool, bool)
		want     bool
	}{
		{"elevated", alive, func(int) (bool, bool) { return true, true }, true},
		{"normal rights", alive, func(int) (bool, bool) { return false, true }, false},
		{"unknown", alive, func(int) (bool, bool) { return false, false }, false},
		{"stale lock", func(int, string) bool { return false }, func(int) (bool, bool) { return true, true }, false},
	}
	for _, c := range cases {
		if got, _ := serviceElevated(dir, c.alive, c.elevated); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if got, pid := serviceElevated(t.TempDir(), alive, cases[0].elevated); got || pid != 0 {
		t.Errorf("no lock = %v %d", got, pid)
	}
}

// With an elevated service, opening and starting fail at once instead of
// hanging ~45 s on the lock; nothing is launched.
func TestLaunchRefusedWithElevatedService(t *testing.T) {
	e := newLaunchEnv(t, []agentEntry{stoppedEntry})
	e.a.serviceCheck = func() (bool, int) { return true, 4321 }
	if _, err := e.a.Open(context.Background(), id2); core.ErrorCode(err) != core.ErrCodeServiceElevated {
		t.Errorf("open = %v", err)
	}
	if _, err := e.a.Start(context.Background(), core.StartRequest{Folder: e.folder, Name: "x"}); core.ErrorCode(err) != core.ErrCodeServiceElevated {
		t.Errorf("start = %v", err)
	}
	if got := e.launches(t); len(got) != 0 {
		t.Errorf("launched %+v", got)
	}
	info := e.a.Info(context.Background())
	found := false
	for _, n := range info.Notices {
		found = found || n == NoticeServiceElevated
	}
	if !found {
		t.Errorf("notices = %v", info.Notices)
	}
}
