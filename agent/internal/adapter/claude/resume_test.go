package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// A woken session keeps its old link archived until Claude reconnects it.
// Handing out that old link early showed the session as archived in the
// Claude app (2026-10-27), so only a link written after the resume counts.
// Right after stopping, the Claude service can still be settling the job
// and refuse rm once. Remove waits a moment and tries again (R-13, found
// by E2E-04 on 2026-10-28).
func TestRemoveRetriesWhileTheServiceSettles(t *testing.T) {
	e := newLaunchEnv(t, []agentEntry{runningEntry})
	busy := filepath.Join(t.TempDir(), "busy")
	os.WriteFile(busy, nil, 0o600)
	t.Setenv("ASKSEMBLE_FAKE_RM_BUSY", busy)
	if err := e.a.Remove(context.Background(), id1); err != nil {
		t.Fatalf("remove = %v, want it to retry rm", err)
	}
	var rms int
	for _, c := range e.launches(t) {
		if c.Args[0] == "rm" {
			rms++
		}
	}
	if rms != 2 {
		t.Errorf("rm ran %d times, want 2 (one refused, one done)", rms)
	}
}

// R-17: settling once outlasted three rm tries 2 s apart, and the failure
// said nothing because rm printed nothing.
func TestRemoveWaitsForALongSettle(t *testing.T) {
	wait := rmRetryWait
	rmRetryWait = 10 * time.Millisecond
	t.Cleanup(func() { rmRetryWait = wait })
	e := newLaunchEnv(t, []agentEntry{runningEntry})
	busy := filepath.Join(t.TempDir(), "busy")
	os.WriteFile(busy, []byte("4"), 0o600)
	t.Setenv("ASKSEMBLE_FAKE_RM_BUSY", busy)
	t.Setenv("ASKSEMBLE_FAKE_RM_SILENT", "1")
	if err := e.a.Remove(context.Background(), id1); err != nil {
		t.Fatalf("remove = %v, want it to keep trying while the service settles", err)
	}

	// When it never settles, the error still says why.
	e = newLaunchEnv(t, []agentEntry{runningEntry})
	os.WriteFile(busy, []byte("99"), 0o600)
	err := e.a.Remove(context.Background(), id1)
	var ae *core.ActionError
	if !errors.As(err, &ae) || ae.Code != core.ErrCodeLaunchFailed || strings.TrimSpace(ae.Detail) == "" {
		t.Fatalf("remove = %v, want launch_failed with a detail", err)
	}
}

func TestOpenStoppedNeverHandsOutTheOldLink(t *testing.T) {
	stopped := agentEntry{ID: "11111111", Kind: kindBackground, SessionID: id1, Name: "job one", State: "stopped"}
	e := newLaunchEnv(t, []agentEntry{stopped}) // id1's transcript already has a link
	t.Setenv("ASKSEMBLE_FAKE_NOLINK", "1")      // the resumed process never reconnects
	res, err := e.a.Open(context.Background(), id1)
	if core.ErrorCode(err) != core.ErrCodeLinkTimeout {
		t.Fatalf("got %+v, %v; want a link timeout, not the old link", res, err)
	}
}
