package scenario

import (
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/compares"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
)

// IT-18 (C6, #19, 2026-11-05, the user's decision; the judging's code went
// with C6d, #24): a run's comparison asks no judge and makes no card; the
// moderator alone writes the combined answer, with synth-v5. Two AIs asked
// get one too.
func TestIT18NoJudging(t *testing.T) {
	t.Run("three AIs", func(t *testing.T) {
		r := newRig(t, nil)
		r.addDevice()
		_, c, b := r.synthRun(map[string]any{"synthesis": map[string]any{"agent": fakecli.RoleCodex}})
		s := synthOf(t, c)
		if c.Status != compares.Done || c.Error != nil || len(c.Requests) != 0 || len(c.Items) != 0 {
			t.Fatalf("comparison %s %+v, requests %d, items %d", c.Status, c.Error, len(c.Requests), len(c.Items))
		}
		if s.Status != compares.SynthDone || s.Agent != fakecli.RoleCodex || s.Judged || s.Text != fakecli.SynthLead {
			t.Fatalf("synthesis %+v", s)
		}
		onlyAsksAndWriters(t)
		synths := calls(t, 0, kindSynth, "")
		if len(synths) != 1 {
			t.Fatalf("%d synth calls", len(synths))
		}
		_, data := synthData(t, synths[0].Stdin)
		if !strings.HasPrefix(synths[0].Stdin, "[Asksemble synthesis v5] ") || strings.Contains(synths[0].Stdin, "COMPARED") ||
			len(linesWith(data, "ANSWER: ")) != 3 || len(linesWith(data, "DIFFER: "))+len(linesWith(data, "CHECK: ")) != 0 {
			t.Errorf("writer stdin is not synth-v5 with three answers:\n%s", synths[0].Stdin)
		}
		for _, want := range []string{`"requests":[]`, `"items":[]`} {
			if !strings.Contains(string(b), want) {
				t.Errorf("the snapshot lacks %s", want)
			}
		}
		r.waitIdle(c.ID)
	})

	t.Run("two AIs", func(t *testing.T) {
		r := newRig(t, nil)
		r.addDevice()
		two := []map[string]string{{"agent": fakecli.RoleClaude}, {"agent": fakecli.RoleOpenCode}}
		st := r.ask(two, map[string]any{"tier": "standard", "compare": map[string]any{
			"judges": []string{fakecli.RoleClaude, fakecli.RoleOpenCode}, "synthesis": map[string]any{"agent": fakecli.RoleClaude}}})
		if st.ComparisonID == "" || st.CompareError != nil {
			t.Fatalf("POST /v1/runs: comparisonId %q, compareError %+v", st.ComparisonID, st.CompareError)
		}
		r.waitRun(st.ID)
		c, _ := r.waitComparison(st.ComparisonID)
		if s := synthOf(t, c); c.Status != compares.Done || s.Status != compares.SynthDone || s.Agent != fakecli.RoleClaude {
			t.Fatalf("comparison %s, synthesis %+v", c.Status, s)
		}
		synths := calls(t, 0, kindSynth, "")
		if len(synths) != 1 {
			t.Fatalf("%d synth calls", len(synths))
		}
		onlyAsksAndWriters(t)
		if _, data := synthData(t, synths[0].Stdin); len(linesWith(data, "ANSWER: ")) != 2 {
			t.Errorf("writer stdin has not the two answers:\n%s", synths[0].Stdin)
		}
		r.waitIdle(c.ID)
	})
}
