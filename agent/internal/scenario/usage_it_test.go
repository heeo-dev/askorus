package scenario

import (
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/api"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// IT-25 (#20, 2026-11-18): what is left of the plans, through the agent as
// the phone asks it. The first /v1/info starts the reads and does not wait
// for them; a later one has Codex's two windows (codex app-server) and
// OpenCode Go's three (opencode api), named by window, with none of the
// account or workspace values. A used-up window says so; a Codex that does
// not know the read and an OpenCode that is not signed in to the Console
// have no usage.
func TestIT25Usage(t *testing.T) {
	t.Run("windows", func(t *testing.T) {
		r := newRig(t, nil)
		r.addDevice()
		start := time.Now()
		first := r.info()
		if d := time.Since(start); d > 10*time.Second {
			t.Errorf("the first info took %v", d)
		}
		if u := agentInfo(t, first, fakecli.RoleCodex).Usage; len(u) != 0 {
			t.Logf("the first info already had Codex's usage: %+v", u)
		}
		raw, info := waitUsage(t, r, fakecli.RoleCodex, fakecli.RoleOpenCode)
		cx := agentInfo(t, info, fakecli.RoleCodex).Usage
		if len(cx) != 2 || cx[0].Window != core.Window5h || cx[1].Window != core.WindowWeek ||
			cx[0].Left != (100-fakecli.CodexUsed5h)/100 || cx[1].Left != (100-fakecli.CodexUsedWeek)/100 || cx[0].ResetAt == "" || cx[0].Limited {
			t.Errorf("codex usage %+v", cx)
		}
		oc := agentInfo(t, info, fakecli.RoleOpenCode).Usage
		if len(oc) != 3 || oc[2].Window != core.WindowMonth || oc[2].Left != (100-fakecli.OpenCodeUsedMonth)/100 || oc[0].Left != 1 {
			t.Errorf("opencode usage %+v", oc)
		}
		for _, secret := range []string{fakecli.CodexAccountID, fakecli.OpenCodeWorkspaceID, "Personal", "planType", "plus"} {
			if strings.Contains(raw, secret) {
				t.Errorf("/v1/info has %q", secret)
			}
		}
	})
	t.Run("used up and none", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) {
			o.env = map[string]string{fakecli.EnvUsage: "codex=out,opencode=none"}
		})
		r.addDevice()
		r.info()
		_, info := waitUsage(t, r, fakecli.RoleCodex)
		if cx := agentInfo(t, info, fakecli.RoleCodex).Usage; len(cx) != 2 || !cx[0].Limited || cx[0].Left != 0 {
			t.Errorf("codex used up %+v", cx)
		}
		// OpenCode's read started with Codex's; give it the same time again.
		time.Sleep(time.Second)
		if oc := agentInfo(t, r.info(), fakecli.RoleOpenCode).Usage; len(oc) != 0 {
			t.Errorf("opencode not signed in to the Console %+v", oc)
		}
	})
	t.Run("codex without the read", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) {
			o.env = map[string]string{fakecli.EnvUsage: "codex=none"}
		})
		r.addDevice()
		r.info()
		waitUsage(t, r, fakecli.RoleOpenCode)
		time.Sleep(time.Second)
		if cx := agentInfo(t, r.info(), fakecli.RoleCodex); len(cx.Usage) != 0 || !cx.Available {
			t.Errorf("codex %+v", cx)
		}
	})
}

// waitUsage asks /v1/info until each of the agents has usage, or 20 s;
// it returns the last answer, raw and read.
func waitUsage(t *testing.T, r *rig, agents ...string) (string, api.InfoResponse) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		var info api.InfoResponse
		raw := r.callJSON("GET", "/v1/info", nil, 200, &info)
		all := true
		for _, a := range agents {
			if len(agentInfo(t, info, a).Usage) == 0 {
				all = false
			}
		}
		if all {
			return string(raw), info
		}
		if time.Now().After(deadline) {
			t.Fatalf("no usage for %v after 20 s", agents)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
