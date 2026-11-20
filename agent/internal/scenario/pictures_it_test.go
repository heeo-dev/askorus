package scenario

import (
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/compares"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// IT-28 (#14, R-154, R-155): pictures. Every AI's own ask carries the
// picture guide after the preamble, on stdin, so that asked for a picture
// it draws an SVG, which the app shows as one. The combined answer of a
// phone that says it shows every AI's pictures in a row (pictureRow) is
// written with synth-v5.2, whose rule 9 keeps pictures out of it; a phone
// that does not say so gets synth-v5.1 as before.
func TestIT28Pictures(t *testing.T) {
	for _, c := range []struct {
		name      string
		synthesis map[string]any
		label     string
		rule9     bool
	}{
		{"a phone that shows the picture row", map[string]any{"agent": "claude", "pictureRow": true}, "[Askorus synthesis v5.2] ", true},
		{"a phone from before", map[string]any{"agent": "claude"}, "[Askorus synthesis v5.1] ", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, nil)
			r.addDevice()
			_, cmp, _ := r.synthRun(map[string]any{"synthesis": c.synthesis})
			if s := synthOf(t, cmp); s.Status != compares.SynthDone || s.Agent != fakecli.RoleClaude {
				t.Fatalf("synthesis %+v", s)
			}
			asks := calls(t, 0, kindAsk, "")
			if len(asks) != len(agents) {
				t.Fatalf("%d asks", len(asks))
			}
			want := core.AskPromptGuided(core.QuestionLanguage(question), question, core.GuideDraw)
			for _, a := range asks {
				if strings.TrimRight(a.Prompt, "\n") != want {
					t.Errorf("%s's ask is not the question with the picture guide: %q", a.Role, a.Prompt)
				}
				for _, arg := range a.Args {
					if strings.Contains(arg, "SVG code block") {
						t.Errorf("%s got the picture guide as an argument", a.Role)
					}
				}
			}
			synths := calls(t, 0, kindSynth, "")
			if len(synths) != 1 {
				t.Fatalf("%d writes", len(synths))
			}
			p := synths[0].Prompt
			if !strings.HasPrefix(p, c.label) || strings.Contains(p, "\n9. Pictures. ") != c.rule9 || strings.Contains(p, core.GuideDraw) {
				t.Errorf("the combined answer's prompt: %q", p[:min(len(p), 120)])
			}
			r.waitIdle(cmp.ID)
		})
	}
}
