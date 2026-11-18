package opencode

// What is left of the person's OpenCode Go plan (#20, decision 14).
// OpenCode's own server has the operation experimental.usage.limits: the
// Go allowance of the OpenCode Console workspace the person signed in to,
// or nothing when they did not. The agent asks it through
// `opencode api --standalone`, a private server of that one run (never the
// person's own OpenCode service), in the folder and with the environment of
// the sign-in check. No model is asked and nothing is spent. Of the answer
// it keeps the three Go windows only; the workspace (its id, name and
// console address), the budgets and the folder are dropped. The operation
// is experimental and not in OpenCode's documents: anything that is not the
// expected answer leaves the usage out.

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

const (
	usageTTL     = 5 * time.Minute
	usageTimeout = 20 * time.Second
)

// usageArgs is the only command the read runs.
var usageArgs = []string{"api", "--standalone", "experimental.usage.limits"}

// Usage reads what is left now, without the kept value (the live test).
func (a *Adapter) Usage(ctx context.Context) []core.UsageGroup { return a.readUsage(ctx) }

func (a *Adapter) readUsage(ctx context.Context) []core.UsageGroup {
	if a.exeErr != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, usageTimeout)
	defer cancel()
	dir := filepath.Dir(a.exe)
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.exe, Dir: dir, Args: usageArgs, Env: map[string]string{"PWD": dir}})
	if err != nil || res.ExitCode != 0 {
		return nil
	}
	return parseLimits(res.Stdout)
}

type meter struct {
	Percent  *float64 `json:"percent"` // used, 0 to 100
	Limited  bool     `json:"limited"`
	ResetsAt *float64 `json:"resetsAt"` // milliseconds since 1970
}

// parseLimits reads the answer: data.go's rolling (5 hours), weekly and
// monthly meters, as UsageGroups.
func parseLimits(out []byte) []core.UsageGroup {
	var answer struct {
		Data *struct {
			Go *struct {
				Rolling *meter `json:"rolling"`
				Weekly  *meter `json:"weekly"`
				Monthly *meter `json:"monthly"`
			} `json:"go"`
		} `json:"data"`
	}
	if json.Unmarshal(out, &answer) != nil || answer.Data == nil || answer.Data.Go == nil {
		return nil
	}
	g := answer.Data.Go
	var groups []core.UsageGroup
	for _, w := range []struct {
		m      *meter
		window string
		name   string
	}{{g.Rolling, core.Window5h, "5 hours"}, {g.Weekly, core.WindowWeek, "Weekly"}, {g.Monthly, core.WindowMonth, "Monthly"}} {
		if w.m == nil || w.m.Percent == nil || math.IsNaN(*w.m.Percent) {
			continue
		}
		used := math.Min(100, math.Max(0, *w.m.Percent))
		groups = append(groups, core.UsageGroup{Name: w.name, Left: (100 - used) / 100, Window: w.window, ResetAt: resetTime(w.m.ResetsAt), Limited: w.m.Limited || used >= 100})
	}
	return groups
}

// resetTime reads milliseconds since 1970 as RFC 3339 in UTC.
func resetTime(ms *float64) string {
	if ms == nil || *ms <= 0 || *ms > 1e14 {
		return ""
	}
	return time.UnixMilli(int64(*ms)).UTC().Format(time.RFC3339)
}
