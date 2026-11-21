package scenario

import (
	"bytes"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/compares"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// IT-29 (#14, made pictures): a question from a phone that shows made
// pictures gives Codex its image tool (the one feature left on) and the
// make guide; Claude and OpenCode draw with code only. The pictures codex
// saved in its own folder come back on its part, the phone that asked takes
// their bytes as they were and no other device can, the answer names no
// folder of the PC, and the combined answer's writer gets the number of
// pictures, never a picture.
func TestIT29MadePictures(t *testing.T) {
	r := newRig(t, func(o *rigOptions) { o.env = map[string]string{fakecli.EnvPictures: "2"} })
	r.addDevice()
	_, cmp, _ := r.synthRun(map[string]any{"synthesis": map[string]any{"agent": "claude", "pictureRow": true}})
	if s := synthOf(t, cmp); s.Status != compares.SynthDone {
		t.Fatalf("synthesis %+v", s)
	}
	// The run of synthRun asked without pictures: nothing was made.
	if n := len(calls(t, 0, kindAsk, "")); n != len(agents) {
		t.Fatalf("%d asks", n)
	}
	before := len(fakecli.Calls(t))

	st := r.ask(nil, map[string]any{"pictures": true, "compare": map[string]any{"synthesis": map[string]any{"agent": "claude", "pictureRow": true}}})
	run := r.waitRun(st.ID)
	var cx, cl int = -1, -1
	for i, p := range run.Parts {
		switch p.Agent {
		case "codex":
			cx = i
		case "claude":
			cl = i
		}
	}
	p := run.Parts[cx]
	if len(p.Images) != 2 || p.ImagesLeftOut != 0 {
		t.Fatalf("codex part = %+v", p)
	}
	if strings.Contains(p.Answer, r.root) || !strings.Contains(p.Answer, "[saved: picture-1.png]") {
		t.Errorf("codex answer = %q", p.Answer)
	}
	if len(run.Parts[cl].Images) != 0 {
		t.Errorf("claude made pictures: %+v", run.Parts[cl])
	}
	for _, c := range calls(t, before, kindAsk, "") {
		switch c.Role {
		case fakecli.RoleCodex:
			if strings.Contains(strings.Join(c.Args, " "), "image_generation") || !strings.Contains(c.Prompt, core.GuideMake) {
				t.Errorf("codex was not asked with its image tool and the make guide: %q", c.Args)
			}
		default:
			if !strings.Contains(c.Prompt, core.GuideDraw) {
				t.Errorf("%s was not guided to draw", c.Role)
			}
		}
	}
	for _, im := range p.Images {
		code, b := r.call("GET", "/v1/runs/"+st.ID+"/images/"+im.ID, nil)
		if code != 200 || !bytes.Equal(b, fakecli.FakePNG) || im.Type != core.ImagePNG || im.Width != 1024 {
			t.Errorf("picture %s = %d, %d bytes, %+v", im.ID, code, len(b), im)
		}
	}
	// Another phone of the same person gets nothing.
	mine := r.token
	other, _, err := r.store.Add("Other phone")
	if err != nil {
		t.Fatal(err)
	}
	r.token = other
	if code, _ := r.call("GET", "/v1/runs/"+st.ID+"/images/"+p.Images[0].ID, nil); code != 404 {
		t.Errorf("another device took a picture: %d", code)
	}
	r.token = mine

	c, _ := r.waitComparison(st.ComparisonID)
	if s := synthOf(t, c); s.Status != compares.SynthDone {
		t.Fatalf("synthesis %+v", s)
	}
	synths := calls(t, before, kindSynth, "")
	if len(synths) != 1 {
		t.Fatalf("%d writes", len(synths))
	}
	w := synths[0].Prompt
	if !strings.Contains(w, "MADE PICTURES: ") || !strings.Contains(w, `,"pictures":2}`) || strings.Count(w, `"pictures":`) != 1 {
		t.Errorf("the writer's prompt does not say that Codex made 2 pictures: %q", w[:min(len(w), 400)])
	}
	if strings.Contains(w, "generated_images") || strings.Contains(w, r.root) {
		t.Error("the writer's prompt names a folder of the PC")
	}
	r.waitIdle(c.ID)
}
