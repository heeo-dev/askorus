package fakecli

// What is left of the fake AIs' plans (#20): `codex app-server` with the
// one read account/rateLimits/read, and `opencode api --standalone
// experimental.usage.limits`. Like the sign-in checks they are not
// recorded; each refuses any other input.

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

// EnvUsage sets what the usage reads answer: "ok" (the default), "out"
// (a window used up), "none" (Codex: the method is unknown, an older
// version; OpenCode: not signed in to the Console) or "bad" (not JSON);
// for every AI or per AI, as "codex=out,opencode=none".
const EnvUsage = "ASKORUS_FAKECLI_USAGE"

// Usage modes of ASKORUS_FAKECLI_USAGE.
const (
	UsageOK   = "ok"
	UsageOut  = "out"
	UsageNone = "none"
	UsageBad  = "bad"
)

// UsageModes lists the usage modes.
var UsageModes = []string{UsageOK, UsageOut, UsageNone, UsageBad}

// CodexUsageLines is what the agent must write to `codex app-server`, line
// for line (the adapter's constant, kept here too so the fake refuses
// anything else).
const CodexUsageLines = `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"askorus","title":"Askorus","version":"1"}}}` + "\n" +
	`{"method":"initialized"}` + "\n" +
	`{"id":2,"method":"account/rateLimits/read"}` + "\n"

// The fake's usage (ok): Codex used 28% of its 5 hours and 38% of its
// week; OpenCode Go used nothing of its 5 hours and week and 84% of its
// month, as this PC's real plans on 2026-11-18.
const (
	CodexUsed5h       = 28.0
	CodexUsedWeek     = 38.0
	OpenCodeUsedMonth = 84.0
)

// CodexAccountID and OpenCodeWorkspaceID are in the answers and must never
// reach /v1/info.
const (
	CodexAccountID      = "acct-fake-0001"
	OpenCodeWorkspaceID = "wrk_fake0001"
)

func usageMode(role string) (string, error) {
	return pick(os.Getenv(EnvUsage), role, UsageOK, func(v string) bool { return slices.Contains(UsageModes, v) })
}

// codexAppServer plays `codex app-server`: it reads stdin line by line,
// answers each request about 50 ms later, and ends when stdin closes,
// without the answers not yet written (as the real one does).
func codexAppServer(args []string) int {
	if !slices.Equal(args, []string{"app-server"}) {
		return unexpected(RoleCodex)
	}
	mode, err := usageMode(RoleCodex)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	want := splitLines(CodexUsageLines)
	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	n := 0
	var due []map[string]any
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				return 0
			}
			if n >= len(want) || l != want[n] {
				fmt.Fprintf(os.Stderr, "fakecli: codex app-server: unexpected line %d: %s\n", n+1, l)
				return 2
			}
			n++
			switch n {
			case 1:
				due = append(due, map[string]any{"id": 1, "result": map[string]any{"userAgent": "codex_cli_rs/0.167.1"}})
			case 3:
				due = append(due, codexRateLimits(mode))
			}
		case <-tick.C:
			for _, m := range due {
				if mode == UsageBad && m["id"] == 2 {
					fmt.Println("{not json")
					continue
				}
				printJSON(m)
			}
			if len(due) > 0 {
				printJSON(map[string]any{"method": "remoteControl/status/changed", "params": map[string]any{"status": "disabled"}})
			}
			due = nil
		}
	}
}

func codexRateLimits(mode string) map[string]any {
	if mode == UsageNone {
		return map[string]any{"id": 2, "error": map[string]any{"code": -32601, "message": "method not found"}}
	}
	used5h, reached := CodexUsed5h, any(nil)
	if mode == UsageOut {
		used5h, reached = 100, "rate_limit_reached"
	}
	now := time.Now()
	return map[string]any{"id": 2, "result": map[string]any{
		"accountId":            CodexAccountID,
		"ordinaryUsageAllowed": mode != UsageOut,
		"rateLimits": map[string]any{
			"limitId":              "codex",
			"primary":              map[string]any{"usedPercent": used5h, "windowDurationMins": 300, "resetsAt": now.Add(2 * time.Hour).Unix()},
			"secondary":            map[string]any{"usedPercent": CodexUsedWeek, "windowDurationMins": 10080, "resetsAt": now.Add(5 * 24 * time.Hour).Unix()},
			"credits":              map[string]any{"hasCredits": false, "balance": "0"},
			"planType":             "plus",
			"rateLimitReachedType": reached,
		},
	}}
}

// The fake Claude's usage (ok), as fractions used: its stream says 23% of
// the 5 hours and 38% of the week are used, as this PC's on 2026-11-18.
const (
	ClaudeUsed5h   = 0.23
	ClaudeUsedWeek = 0.38
)

// claudeRateLimits prints the rate_limit_event of Claude Code's stream
// (2.1.327): "ok" both windows allowed, "out" the 5 hours rejected,
// "none" no event (an API key sign-in), "bad" a broken event.
func claudeRateLimits() {
	mode, err := usageMode(RoleClaude)
	if err != nil || mode == UsageNone {
		return
	}
	if mode == UsageBad {
		fmt.Println(`{"type":"rate_limit_event","rate_limit_info":`)
		return
	}
	used5h, status := ClaudeUsed5h, "allowed"
	if mode == UsageOut {
		used5h, status = 1, "rejected"
	}
	now := time.Now()
	printJSON(map[string]any{"type": "rate_limit_event", "rate_limit_info": map[string]any{
		"status": status, "resetsAt": now.Add(3 * time.Hour).Unix(), "rateLimitType": "five_hour",
		"overageStatus": "rejected", "isUsingOverage": false,
		"unifiedWindows": map[string]any{
			"five_hour": map[string]any{"utilization": used5h, "resetsAt": now.Add(3 * time.Hour).Unix(), "status": status},
			"seven_day": map[string]any{"utilization": ClaudeUsedWeek, "resetsAt": now.Add(6 * 24 * time.Hour).Unix(), "status": "allowed"},
		},
	}})
}

// opencodeUsage plays `opencode api --standalone experimental.usage.limits`.
func opencodeUsage(args []string) int {
	if !slices.Equal(args, []string{"api", "--standalone", "experimental.usage.limits"}) {
		return unexpected(RoleOpenCode)
	}
	mode, err := usageMode(RoleOpenCode)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	dir, _ := os.Getwd()
	switch mode {
	case UsageBad:
		fmt.Println("<!doctype html>")
		return 0
	case UsageNone:
		printJSON(map[string]any{"location": map[string]any{"directory": dir}, "data": nil})
		return 0
	}
	monthUsed, limited := OpenCodeUsedMonth, false
	if mode == UsageOut {
		monthUsed, limited = 100, true
	}
	now := time.Now()
	m := func(p float64, lim bool, in time.Duration) map[string]any {
		return map[string]any{"percent": p, "limited": lim, "resetsAt": now.Add(in).UnixMilli()}
	}
	printJSON(map[string]any{
		"location": map[string]any{"directory": dir},
		"data": map[string]any{
			"workspace": map[string]any{"id": OpenCodeWorkspaceID, "name": "Personal", "usageURL": "https://opencode.ai/workspace/" + OpenCodeWorkspaceID + "/go"},
			"go": map[string]any{
				"rolling": m(0, false, 3*time.Hour),
				"weekly":  m(0, false, 4*24*time.Hour),
				"monthly": m(monthUsed, limited, 24*time.Hour),
			},
			"budgets": []any{},
		},
	})
	return 0
}

func splitLines(s string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}
