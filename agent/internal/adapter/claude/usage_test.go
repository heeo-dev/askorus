package claude

import (
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// The stream as claude 2.1.327 printed it on 2026-11-18 (a user hook's
// events, then the usage, then the result).
const realStream = `{"type":"system","subtype":"hook_started"}
{"type":"system","subtype":"init","tools":[]}
{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}
{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","resetsAt":1795012200,"rateLimitType":"five_hour","overageStatus":"rejected","overageDisabledReason":"org_level_disabled","isUsingOverage":false,"unifiedWindows":{"five_hour":{"utilization":0.23,"resetsAt":1795012200,"status":"allowed"},"seven_day":{"utilization":0.38,"resetsAt":1795388400,"status":"allowed"}}}}
{"type":"result","subtype":"success","is_error":false,"result":"ok","session_id":"00000000-0000-4000-8000-000000000000"}
`

func TestReadRateLimits(t *testing.T) {
	g := readRateLimits([]byte(realStream))
	want := []core.UsageGroup{
		{Name: "5 hours", Left: 0.77, Window: core.Window5h, ResetAt: "2026-11-18T14:30:00Z"},
		{Name: "Weekly", Left: 0.62, Window: core.WindowWeek, ResetAt: "2026-11-22T23:00:00Z"},
	}
	if len(g) != 2 || g[0] != want[0] || g[1] != want[1] {
		t.Fatalf("real stream = %+v", g)
	}
	cases := map[string]func([]core.UsageGroup) bool{
		// The window that is used up says so.
		`{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{"five_hour":{"utilization":1,"status":"rejected"},"seven_day":{"utilization":0.5,"status":"allowed"}}}}`: func(g []core.UsageGroup) bool {
			return len(g) == 2 && g[0].Limited && g[0].Left == 0 && !g[1].Limited
		},
		// Without unifiedWindows: the one window rateLimitType names.
		`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","rateLimitType":"seven_day","utilization":0.9,"resetsAt":1795388400}}`: func(g []core.UsageGroup) bool {
			return len(g) == 1 && g[0].Window == core.WindowWeek && g[0].Left > 0.09 && g[0].Left < 0.11
		},
		// No utilization (older versions in the allowed state): nothing.
		`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","rateLimitType":"five_hour"}}`: func(g []core.UsageGroup) bool { return len(g) == 0 },
		// The last event wins; other windows are left out.
		`{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{"five_hour":{"utilization":0.9}}}}` + "\n" +
			`{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{"five_hour":{"utilization":0.1},"seven_day_opus":{"utilization":0.5}}}}`: func(g []core.UsageGroup) bool {
			return len(g) == 1 && g[0].Left == 0.9
		},
		// An answer that holds the words does not hide the event before it.
		realStream + `{"type":"result","result":"the \"rate_limit_event\" line"}`: func(g []core.UsageGroup) bool { return len(g) == 2 },
		`{"type":"rate_limit_event","rate_limit_info":`:                           func(g []core.UsageGroup) bool { return g == nil },
		`{"type":"result","result":"rate_limit_event is a word in this answer"}`:  func(g []core.UsageGroup) bool { return g == nil },
		`{"type":"result","subtype":"success","result":"ok","session_id":"x"}`:    func(g []core.UsageGroup) bool { return g == nil },
		"": func(g []core.UsageGroup) bool { return g == nil },
	}
	for in, ok := range cases {
		if g := readRateLimits([]byte(in)); !ok(g) {
			t.Errorf("%s\n= %+v", in, g)
		}
	}
}

// A run's usage is kept with the time it came, and a run without any
// leaves the kept one as it is.
func TestNoteUsage(t *testing.T) {
	a := &Adapter{}
	if g := a.keptUsage(); g != nil {
		t.Fatalf("nothing kept yet = %+v", g)
	}
	a.noteUsage([]byte(realStream))
	g := a.keptUsage()
	if len(g) != 2 || g[0].At == "" || g[0].At != g[1].At {
		t.Fatalf("kept = %+v", g)
	}
	a.noteUsage([]byte(`{"type":"result","result":"ok"}`))
	if g2 := a.keptUsage(); len(g2) != 2 || g2[0].Left != g[0].Left {
		t.Errorf("a run without usage dropped the kept one: %+v", g2)
	}
	g[0].Left = 0 // a copy
	if a.keptUsage()[0].Left == 0 {
		t.Error("keptUsage returned the kept slice")
	}
}
