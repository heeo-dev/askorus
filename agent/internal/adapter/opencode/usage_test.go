package opencode

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// The answer as opencode 2.2.7 gave it on 2026-11-18 (workspace and folder
// changed).
const realLimits = `{"location":{"directory":"C:\\Users\\x\\tmp"},"data":{"workspace":{"id":"wrk_x","name":"Personal","usageURL":"https://opencode.ai/workspace/wrk_x/go"},"go":{"rolling":{"percent":0,"limited":false,"resetsAt":1795013671000},"weekly":{"percent":0,"limited":false,"resetsAt":1795392000000},"monthly":{"percent":84,"limited":false,"resetsAt":1795136845000}},"budgets":[]}}`

func TestParseLimits(t *testing.T) {
	g := parseLimits([]byte(realLimits))
	want := []core.UsageGroup{
		{Name: "5 hours", Left: 1, Window: core.Window5h, ResetAt: "2026-11-18T14:54:31Z"},
		{Name: "Weekly", Left: 1, Window: core.WindowWeek, ResetAt: "2026-11-23T00:00:00Z"},
		{Name: "Monthly", Left: 0.16, Window: core.WindowMonth, ResetAt: "2026-11-20T01:07:25Z"},
	}
	if !slices.Equal(g, want) {
		t.Fatalf("real answer = %+v", g)
	}
	if b, _ := json.Marshal(g); strings.Contains(string(b), "wrk_") || strings.Contains(string(b), "Personal") || strings.Contains(string(b), `tmp`) {
		t.Errorf("workspace or folder went through: %s", b)
	}
	cases := map[string]func([]core.UsageGroup) bool{
		`{"data":{"go":{"rolling":{"percent":100,"limited":true,"resetsAt":1795013671000},"monthly":{"percent":30,"limited":true}}}}`: func(g []core.UsageGroup) bool {
			return len(g) == 2 && g[0].Limited && g[0].Left == 0 && g[1].Limited && g[1].Window == core.WindowMonth && g[1].ResetAt == ""
		},
		`{"data":{"go":{"weekly":{"percent":12.5}}}}`:              func(g []core.UsageGroup) bool { return len(g) == 1 && g[0].Left == 0.875 },
		`{"data":{"workspace":{"id":"w"},"budgets":[]}}`:           func(g []core.UsageGroup) bool { return g == nil },
		`{"location":{"directory":"x"},"data":null}`:               func(g []core.UsageGroup) bool { return g == nil },
		`{"data":{"go":{"rolling":{"limited":false}}}}`:            func(g []core.UsageGroup) bool { return len(g) == 0 },
		`{"data":{"go":{"rolling":{"percent":-5,"resetsAt":-1}}}}`: func(g []core.UsageGroup) bool { return len(g) == 1 && g[0].Left == 1 && g[0].ResetAt == "" },
		"<!doctype html>": func(g []core.UsageGroup) bool { return g == nil },
		"":                func(g []core.UsageGroup) bool { return g == nil },
	}
	for in, ok := range cases {
		if g := parseLimits([]byte(in)); !ok(g) {
			t.Errorf("%s\n= %+v", in, g)
		}
	}
}

// The read through the fake opencode, which refuses any other arguments.
func TestUsageFromAPI(t *testing.T) {
	a := New(Options{Path: fakecli.Use(t, fakecli.RoleOpenCode)})
	g := a.readUsage(context.Background())
	if len(g) != 3 || g[2].Window != core.WindowMonth || g[2].Left != (100-fakecli.OpenCodeUsedMonth)/100 || g[0].Left != 1 {
		t.Fatalf("usage = %+v", g)
	}
	if b, _ := json.Marshal(g); strings.Contains(string(b), fakecli.OpenCodeWorkspaceID) {
		t.Errorf("the workspace went through: %s", b)
	}
	t.Setenv(fakecli.EnvUsage, fakecli.UsageOut)
	if g := a.readUsage(context.Background()); len(g) != 3 || !g[2].Limited || g[2].Left != 0 {
		t.Errorf("used up = %+v", g)
	}
	for _, mode := range []string{fakecli.UsageNone, fakecli.UsageBad} {
		t.Setenv(fakecli.EnvUsage, mode)
		if g := a.readUsage(context.Background()); g != nil {
			t.Errorf("%s = %+v", mode, g)
		}
	}
	if !slices.Equal(usageArgs, []string{"api", "--standalone", "experimental.usage.limits"}) {
		t.Errorf("args = %v", usageArgs)
	}
}

// Info never waits for the read: the first has no usage, the next has it.
func TestInfoUsage(t *testing.T) {
	a := New(Options{Path: fakecli.Use(t, fakecli.RoleOpenCode)})
	a.loginCheck = func(context.Context) loginState { return loginState{} }
	start := time.Now()
	info := a.Info(context.Background())
	if !info.Available || len(info.Usage) != 0 {
		t.Fatalf("first info = %+v", info)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("info took %v", d)
	}
	a.usage.Wait(context.Background())
	if info := a.Info(context.Background()); len(info.Usage) != 3 {
		t.Errorf("next info = %+v", info.Usage)
	}
}
