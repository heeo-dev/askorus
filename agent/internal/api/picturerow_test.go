package api

import (
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/compares"
)

// #14 (R-154, R-155): a phone that shows every AI's pictures in a row under
// the combined answer says so with the combined answer it asks for
// (pictureRow). The moderator then gets synth-v5.2, whose rule 9 keeps
// pictures out and points at the row. A phone from before has no row to
// point at and gets synth-v5.1 as it was. Either phone's question goes to
// the AIs with the picture guide after the preamble.
func TestRunPictureRow(t *testing.T) {
	for _, c := range []struct {
		name      string
		synthesis map[string]any
		label     string
		rules     string
		rule9     bool
	}{
		{"an app from before", map[string]any{"agent": "claude"}, "[Askorus synthesis v5.1] ", `"rules":"synth-v5.1"`, false},
		{"an app that shows the row", map[string]any{"agent": "claude", "pictureRow": true}, "[Askorus synthesis v5.2] ", `"rules":"synth-v5.2"`, true},
	} {
		cl, cx := newWriterAdapter("claude", false), newWriterAdapter("codex", false)
		e, _ := judgeEnv(t, nil, cl, cx)
		st := startRun(t, e, map[string]any{
			"mode": "ask", "prompt": "고양이를 그려줘",
			"agents":  []map[string]string{{"agent": "claude"}, {"agent": "codex"}},
			"compare": map[string]any{"judges": []string{"claude", "codex"}, "synthesis": c.synthesis},
		})
		e.waitRun(t, st.ID)
		cmp := e.waitSynthesis(t, st.ComparisonID)
		synths := cl.synthCalls()
		if cmp.Synthesis == nil || cmp.Synthesis.Status != compares.SynthDone || len(synths) != 1 {
			t.Fatalf("%s: comparison = %+v, %d writes", c.name, cmp.Comparison, len(synths))
		}
		if p := synths[0].Prompt; !strings.HasPrefix(p, c.label) || strings.Contains(p, "\n9. Pictures. ") != c.rule9 {
			t.Errorf("%s: the combined answer's prompt:\n%s", c.name, p)
		}
		e.idle(t, st.ComparisonID)
		if logs := e.logs.String(); !strings.Contains(logs, c.rules) || strings.Contains(logs, "고양이") {
			t.Errorf("%s: log %s", c.name, logs)
		}
		asks := append(cl.requests(), cx.requests()...)
		if len(asks) != 2 {
			t.Fatalf("%s: %d asks", c.name, len(asks))
		}
		for _, a := range asks {
			if a.Prompt != askPrompt("고양이를 그려줘") {
				t.Errorf("%s: ask prompt %q", c.name, a.Prompt)
			}
		}
	}
}

// A combined answer written later (it was off, or failed) takes the mark of
// the phone that asks for it then.
func TestWriteSynthesisPictureRow(t *testing.T) {
	cl, cx := newWriterAdapter("claude", false), newWriterAdapter("codex", false)
	e, _ := judgeEnv(t, nil, cl, cx)
	st := startRun(t, e, map[string]any{
		"mode": "ask", "prompt": "고양이를 그려줘",
		"agents":  []map[string]string{{"agent": "claude"}, {"agent": "codex"}},
		"compare": map[string]any{"judges": []string{"claude", "codex"}, "synthesis": map[string]any{"off": true}},
	})
	e.waitRun(t, st.ID)
	e.idle(t, st.ComparisonID)
	if n := len(cl.synthCalls()) + len(cx.synthCalls()); n != 0 {
		t.Fatalf("%d writes while off", n)
	}
	if w := e.post("/v1/comparisons/"+st.ComparisonID+"/synthesis", map[string]any{"agent": "claude", "pictureRow": true}); w.Code != 202 {
		t.Fatalf("write = %d %s", w.Code, w.Body)
	}
	cmp := e.waitSynthesis(t, st.ComparisonID)
	synths := cl.synthCalls()
	if cmp.Synthesis == nil || cmp.Synthesis.Status != compares.SynthDone || len(synths) != 1 {
		t.Fatalf("comparison = %+v, %d writes", cmp.Comparison, len(synths))
	}
	if p := synths[0].Prompt; !strings.HasPrefix(p, "[Askorus synthesis v5.2] ") || !strings.Contains(p, "\n9. Pictures. ") {
		t.Errorf("the combined answer's prompt:\n%s", p)
	}
}
