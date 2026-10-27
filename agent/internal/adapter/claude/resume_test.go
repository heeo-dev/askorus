package claude

import (
	"context"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// A woken session keeps its old link archived until Claude reconnects it.
// Handing out that old link early showed the session as archived in the
// Claude app (2026-10-27), so only a link written after the resume counts.
func TestOpenStoppedNeverHandsOutTheOldLink(t *testing.T) {
	stopped := agentEntry{ID: "11111111", Kind: kindBackground, SessionID: id1, Name: "job one", State: "stopped"}
	e := newLaunchEnv(t, []agentEntry{stopped}) // id1's transcript already has a link
	t.Setenv("ASKSEMBLE_FAKE_NOLINK", "1")      // the resumed process never reconnects
	res, err := e.a.Open(context.Background(), id1)
	if core.ErrorCode(err) != core.ErrCodeLinkTimeout {
		t.Fatalf("got %+v, %v; want a link timeout, not the old link", res, err)
	}
}
