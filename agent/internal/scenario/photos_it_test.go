package scenario

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/api"
	"github.com/heeo-dev/askorus/agent/internal/compares"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
	"github.com/heeo-dev/askorus/agent/internal/runs"
	"github.com/heeo-dev/askorus/agent/internal/uploads"
)

// IT-23 (#14, 11-18): photos with a question through the whole agent and
// the real adapters around the fake CLIs. Two photos are uploaded like the
// phone does and go with one question to all three AIs: each gets them in
// its own way (Claude on stdin in one stream-json message, Codex with -i,
// OpenCode with --file), from the agent's own files, with the question on
// stdin as always. The writer of the combined answer gets them too, with
// one line in its prompt. A follow-up without photos runs the commands of
// a question without photos. A model that does not see photos is not
// asked. Nothing of a photo is logged, and none outlives the agent.

var (
	itJPEG = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{9}, 300)...)
	itPNG  = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{4}, 300)...)
)

// send posts raw bytes, as the phone uploads a photo.
func (r *rig) send(path string, data []byte) (int, []byte) {
	r.t.Helper()
	req, _ := http.NewRequest("POST", r.srv.URL+path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+r.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func (r *rig) upload(data []byte) uploads.Upload {
	r.t.Helper()
	st, b := r.send("/v1/uploads", data)
	var up uploads.Upload
	if err := json.Unmarshal(b, &up); st != 201 || err != nil || !core.ValidImageID(up.ID) {
		r.t.Fatalf("POST /v1/uploads = %d %s", st, b)
	}
	return up
}

func TestIT23Photos(t *testing.T) {
	r := newRig(t, nil)
	r.addDevice()
	info := r.info()
	if a := info.Attachments; a == nil || a.MaxCount != 4 || a.MaxBytes != 4<<20 || !slices.Equal(a.Types, []string{"image/jpeg", "image/png", "image/webp"}) {
		t.Fatalf("info attachments = %+v", a)
	}
	for _, name := range []string{fakecli.RoleClaude, fakecli.RoleCodex} {
		if im := agentInfo(t, info, name).Ask.Images; im == nil || !im.All {
			t.Errorf("%s images = %+v", name, im)
		}
	}
	if im := agentInfo(t, info, fakecli.RoleOpenCode).Ask.Images; im == nil || im.All || !slices.Equal(im.Models, []string{"opencode-go/deepseek-v4.1-flash"}) {
		t.Errorf("opencode images = %+v", im)
	}
	if st, b := r.send("/v1/uploads", []byte("not a photo")); st != 415 || !strings.Contains(string(b), api.CodeUploadBadType) {
		t.Errorf("a text upload = %d %s", st, b)
	}

	a, b := r.upload(itJPEG), r.upload(itPNG)
	dir := r.photos.Dir()
	st := r.ask(nil, map[string]any{"attachments": []string{a.ID, b.ID},
		"compare": map[string]any{"synthesis": map[string]string{"agent": fakecli.RoleClaude}}})
	run := r.waitRun(st.ID)
	if run.Status != runs.Done {
		t.Fatalf("run: %+v", run)
	}
	sessions := []map[string]string{}
	for _, p := range run.Parts {
		if !strings.HasSuffix(p.Answer, " [photos: 2]") || p.Session == "" {
			t.Errorf("%s did not get the two photos: %+v", p.Agent, p)
		}
		sessions = append(sessions, map[string]string{"agent": p.Agent, "session": p.Session})
	}
	paths := []string{filepath.Join(dir, a.ID+".jpg"), filepath.Join(dir, b.ID+".png")}
	for _, c := range calls(t, 0, kindAsk, "") {
		args := strings.Join(c.Args, "\x00")
		if c.Photos != 2 || strings.Contains(args, "상자") {
			t.Errorf("%s ask: %d photos, args %q", c.Role, c.Photos, c.Args)
		}
		switch c.Role {
		case fakecli.RoleClaude:
			// On stdin, as base64: no path or file name anywhere.
			if !slices.Equal(c.Args[len(c.Args)-3:], []string{"--input-format", "stream-json", "--verbose"}) || flag(c.Args, "--output-format") != "stream-json" ||
				flag(c.Args, "--tools") != "WebSearch,WebFetch" || flag(c.Args, "--settings") != `{"permissions":{"deny":["Read"]}}` || !slices.Contains(c.Args, "--strict-mcp-config") {
				t.Errorf("claude args = %q", c.Args)
			}
			if strings.Contains(args, "up_") || strings.Contains(c.Stdin, a.ID) || strings.Contains(c.Stdin, "uploads") || !strings.Contains(c.Stdin, `"media_type":"image/png"`) {
				t.Errorf("claude got a path or no photo: args %q", c.Args)
			}
		case fakecli.RoleCodex:
			i := slices.Index(c.Args, "-i")
			if i < 0 || !slices.Equal(c.Args[i:i+5], []string{"-i", paths[0], "-i", paths[1], "--json"}) || flag(c.Args, "-s") != "read-only" ||
				!slices.Contains(disabled(c.Args), "image_generation") || !strings.HasSuffix(strings.TrimSpace(c.Stdin), "(Write the answer in Korean.)") {
				t.Errorf("codex args = %q", c.Args)
			}
		case fakecli.RoleOpenCode:
			if n := len(c.Args); !slices.Equal(c.Args[n-4:], []string{"--file", paths[0], "--file", paths[1]}) || flag(c.Args, "--agent") != "askorus-ask" {
				t.Errorf("opencode args = %q", c.Args)
			}
		}
	}
	if len(calls(t, 0, kindAsk, "")) != 3 {
		t.Fatalf("%d asks", len(calls(t, 0, kindAsk, "")))
	}

	// The combined answer: Claude writes with the photos on stdin.
	c, _ := r.waitComparison(st.ComparisonID)
	if sy := synthOf(t, c); sy.Status != compares.SynthDone || sy.Agent != fakecli.RoleClaude {
		t.Fatalf("combined answer: %+v", sy)
	}
	r.waitIdle(st.ComparisonID)
	writers := calls(t, 0, kindSynth, "")
	if len(writers) != 1 || writers[0].Photos != 2 || flag(writers[0].Args, "--tools") != "" || !slices.Contains(writers[0].Args, "--safe-mode") ||
		!slices.Equal(writers[0].Args[len(writers[0].Args)-3:], []string{"--input-format", "stream-json", "--verbose"}) {
		t.Fatalf("writer calls: %+v", writers)
	}
	if !strings.Contains(writers[0].Stdin, `PHOTOS: The person sent 2 photo(s) with the QUESTION;`) || strings.Contains(writers[0].Stdin, "uploads") {
		t.Error("the writer's prompt does not say there are photos, or names a path")
	}

	// A follow-up without photos is a follow-up as it always was.
	from := len(fakecli.Calls(t))
	follow := r.waitRun(r.ask(sessions, map[string]any{"prompt": "그 상자의 색은?"}).ID)
	for _, p := range follow.Parts {
		if p.Status != runs.Done || strings.Contains(p.Answer, "[photos") {
			t.Errorf("%s follow-up: %+v", p.Agent, p)
		}
	}
	for _, c := range calls(t, from, kindAsk, "") {
		if c.Photos != 0 || slices.Contains(c.Args, "-i") || slices.Contains(c.Args, "--file") {
			t.Errorf("%s follow-up args = %q", c.Role, c.Args)
		}
	}

	// OpenCode's deep tier does not see photos: it is not asked.
	from = len(fakecli.Calls(t))
	deep := r.waitRun(r.ask([]map[string]string{{"agent": fakecli.RoleOpenCode}, {"agent": fakecli.RoleCodex}}, map[string]any{"tier": "deep", "attachments": []string{a.ID}}).ID)
	if p := deep.Parts[0]; p.Status != runs.Failed || p.Error == nil || p.Error.Code != core.ErrCodeNoImages {
		t.Errorf("opencode deep with a photo: %+v", p)
	}
	if p := deep.Parts[1]; p.Status != runs.Done || !strings.HasSuffix(p.Answer, " [photos: 1]") {
		t.Errorf("codex deep with a photo: %+v", p)
	}
	if n := len(calls(t, from, kindAsk, fakecli.RoleOpenCode)); n != 0 {
		t.Errorf("opencode was asked %d times", n)
	}

	// Nothing of a photo in the log; none after the agent stopped.
	if logs := r.logs.String(); strings.Contains(logs, dir) || strings.Contains(logs, a.ID) || strings.Contains(logs, b.ID) {
		t.Error("the agent log names a photo")
	}
	onlyAsksAndWriters(t)
	r.restart()
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("%d files left in the uploads folder after a restart", len(left))
	}
	if st, body := r.call("POST", "/v1/runs", map[string]any{"mode": "ask", "prompt": question, "attachments": []string{a.ID}}); st != 409 || !strings.Contains(string(body), api.CodeUploadGone) {
		t.Errorf("a run with a photo of before the restart = %d %s", st, body)
	}
}
