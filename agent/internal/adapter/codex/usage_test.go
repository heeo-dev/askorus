package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// The agent writes three constant lines to codex app-server and no other
// method; the fake refuses anything else.
func TestUsageLines(t *testing.T) {
	if usageLines != fakecli.CodexUsageLines {
		t.Fatalf("the adapter and the fake write different lines:\n%s\n%s", usageLines, fakecli.CodexUsageLines)
	}
	var methods []string
	for _, l := range strings.Split(strings.TrimSuffix(usageLines, "\n"), "\n") {
		var m struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("line %q: %v", l, err)
		}
		methods = append(methods, m.Method)
	}
	if strings.Join(methods, ",") != "initialize,initialized,account/rateLimits/read" {
		t.Errorf("methods = %v", methods)
	}
	if args := usageArgs(); len(args) != 1 || args[0] != "app-server" {
		t.Errorf("args = %v", args)
	}
	for line, want := range map[string]bool{`{"id":2,"result":{}}`: true, `{"id":2,"error":{"code":-32601}}`: true, `{"id":1,"result":{}}`: false, `{"method":"account/updated"}`: false, `{"id":"2"}`: false, `not json`: false} {
		if usageAnswered([]byte(line)) != want {
			t.Errorf("answered(%s) = %v", line, !want)
		}
	}
}

// The answer as codex 0.167.1 gave it on 2026-11-18 (account id and
// credits changed), after the notifications it sends first.
const realRateLimits = `{"id":1,"result":{"userAgent":"codex_cli_rs/0.167.1"}}
{"method":"remoteControl/status/changed","params":{"status":"disabled"}}
{"id":2,"result":{"accountId":"acct-x","ordinaryUsageAllowed":true,"rateLimits":{"limitId":"codex","limitName":null,"primary":{"usedPercent":1,"windowDurationMins":300,"resetsAt":1795014251},"secondary":{"usedPercent":5,"windowDurationMins":10080,"resetsAt":1795476164},"credits":{"hasCredits":false,"unlimited":false,"balance":"0"},"planType":"plus","rateLimitReachedType":null},"rateLimitResetCredits":{"availableCount":1}}}
`

func TestParseRateLimits(t *testing.T) {
	g := parseRateLimits([]byte(realRateLimits))
	want := []core.UsageGroup{
		{Name: "5 hours", Left: 0.99, Window: core.Window5h, ResetAt: "2026-11-18T15:04:11Z"},
		{Name: "Weekly", Left: 0.95, Window: core.WindowWeek, ResetAt: "2026-11-23T23:22:44Z"},
	}
	if len(g) != 2 || g[0] != want[0] || g[1] != want[1] {
		t.Fatalf("real answer = %+v", g)
	}
	if b, _ := json.Marshal(g); strings.Contains(string(b), "acct") || strings.Contains(string(b), "plus") {
		t.Errorf("account values went through: %s", b)
	}
	cases := map[string]func([]core.UsageGroup) bool{
		// Codex says the limit is reached: the fullest window is used up.
		`{"id":2,"result":{"rateLimits":{"primary":{"usedPercent":99.6,"windowDurationMins":300},"secondary":{"usedPercent":40,"windowDurationMins":10080},"rateLimitReachedType":"rate_limit_reached"}}}`: func(g []core.UsageGroup) bool {
			return len(g) == 2 && g[0].Limited && !g[1].Limited
		},
		`{"id":2,"result":{"ordinaryUsageAllowed":false,"rateLimits":{"primary":{"usedPercent":10},"secondary":{"usedPercent":70}}}}`: func(g []core.UsageGroup) bool {
			return len(g) == 2 && !g[0].Limited && g[1].Limited && g[0].Window == core.Window5h && g[1].Window == core.WindowWeek
		},
		// Over 100 is all used; a window of another length is left out.
		`{"id":2,"result":{"rateLimits":{"primary":{"usedPercent":130,"windowDurationMins":300},"secondary":{"usedPercent":5,"windowDurationMins":1440}}}}`: func(g []core.UsageGroup) bool {
			return len(g) == 1 && g[0].Left == 0 && g[0].Limited
		},
		// resetsAt as text, or something else.
		`{"id":2,"result":{"rateLimits":{"primary":{"usedPercent":50,"resetsAt":"2025-12-04T19:15:00Z"},"secondary":{"usedPercent":5,"resetsAt":"soon"}}}}`: func(g []core.UsageGroup) bool {
			return len(g) == 2 && g[0].ResetAt == "2025-12-04T19:15:00Z" && g[1].ResetAt == ""
		},
		`{"id":2,"result":{"rateLimits":{"primary":{"windowDurationMins":300}}}}`: func(g []core.UsageGroup) bool { return len(g) == 0 },
		`{"id":2,"result":{"rateLimits":null}}`:                                   func(g []core.UsageGroup) bool { return g == nil },
		`{"id":2,"error":{"code":-32601,"message":"method not found"}}`:           func(g []core.UsageGroup) bool { return g == nil },
		`{"id":1,"result":{"rateLimits":{"primary":{"usedPercent":1}}}}`:          func(g []core.UsageGroup) bool { return g == nil },
		"{not json": func(g []core.UsageGroup) bool { return g == nil },
		"":          func(g []core.UsageGroup) bool { return g == nil },
	}
	for in, ok := range cases {
		if g := parseRateLimits([]byte(in)); !ok(g) {
			t.Errorf("%s\n= %+v", in, g)
		}
	}
}

func usageAdapter(t *testing.T) *Adapter {
	t.Helper()
	t.Setenv(fakecli.EnvSignIn, "")
	return New(Options{Path: fakecli.Use(t, fakecli.RoleCodex), SchemaDir: t.TempDir()})
}

// The read through the fake codex app-server, which answers only while
// stdin is open and refuses any other line.
func TestUsageFromAppServer(t *testing.T) {
	a := usageAdapter(t)
	g := a.readUsage(context.Background())
	if len(g) != 2 || g[0].Window != core.Window5h || g[0].Left != (100-fakecli.CodexUsed5h)/100 || g[1].Left != (100-fakecli.CodexUsedWeek)/100 {
		t.Fatalf("usage = %+v", g)
	}
	if g[0].ResetAt == "" || g[0].Limited {
		t.Errorf("5 hours = %+v", g[0])
	}
	if b, _ := json.Marshal(g); strings.Contains(string(b), fakecli.CodexAccountID) {
		t.Errorf("the account id went through: %s", b)
	}
	t.Setenv(fakecli.EnvUsage, fakecli.UsageOut)
	if g := a.readUsage(context.Background()); len(g) != 2 || !g[0].Limited || g[0].Left != 0 {
		t.Errorf("used up = %+v", g)
	}
	for _, mode := range []string{fakecli.UsageNone, fakecli.UsageBad} {
		t.Setenv(fakecli.EnvUsage, mode)
		if g := a.readUsage(context.Background()); g != nil {
			t.Errorf("%s = %+v", mode, g)
		}
	}
	// Not signed in: the server is not asked.
	t.Setenv(fakecli.EnvUsage, "")
	t.Setenv(fakecli.EnvSignIn, "codex=out")
	b := New(Options{Path: a.exe, SchemaDir: t.TempDir()})
	if g := b.readUsage(context.Background()); g != nil {
		t.Errorf("signed out = %+v", g)
	}
}

// Info never waits for the read: the first has no usage, the next has it.
func TestInfoUsage(t *testing.T) {
	a := usageAdapter(t)
	start := time.Now()
	info := a.Info(context.Background())
	if !info.Available || len(info.Usage) != 0 {
		t.Fatalf("first info = %+v", info)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("info took %v", d)
	}
	a.usage.Wait(context.Background())
	if info := a.Info(context.Background()); len(info.Usage) != 2 {
		t.Errorf("next info = %+v", info.Usage)
	}
}
