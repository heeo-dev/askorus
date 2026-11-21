package api

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/made"
)

// Made pictures (#14, G1).

var madePNGBytes = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 13, 'I', 'H', 'D', 'R', 0, 0, 4, 0, 0, 0, 4, 0}, bytes.Repeat([]byte{3}, 300)...)

// makerAdapter is an askAdapter that makes pictures: given its tool, it
// writes count pictures in a folder of its own and says where.
type makerAdapter struct {
	*askAdapter
	dir     string
	count   int
	makes   *core.MakesInfo
	resumes map[string]bool
}

func (a *makerAdapter) Makes() *core.MakesInfo { return a.makes }
func (a *makerAdapter) MakesIn(resume string) bool {
	return a.resumes == nil || a.resumes[resume]
}
func (a *makerAdapter) Ask(ctx context.Context, req core.AskRequest) (core.AskResult, error) {
	res, err := a.askAdapter.Ask(ctx, req)
	if err != nil || !req.Pictures {
		return res, err
	}
	var files []core.MadeFile
	for i := range a.count {
		p := filepath.Join(a.dir, "exec-"+string(rune('a'+i))+".png")
		if err := os.WriteFile(p, madePNGBytes, 0o600); err != nil {
			return res, err
		}
		files = append(files, core.MadeFile{Path: p, Type: core.ImagePNG})
	}
	res.Made = files
	res.Answer = core.ScrubMadePaths("Here it is: "+filepath.Join(a.dir, "exec-a.png"), a.dir, files)
	return res, nil
}

func madeEnv(t *testing.T, adapters ...core.Adapter) (env, *made.Store) {
	t.Helper()
	store, err := made.Open(filepath.Join(t.TempDir(), "made"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	ask := filepath.Join(t.TempDir(), "ask")
	e := newEnv(t, adapters[0], func(o *Options) {
		o.Adapters = adapters
		o.AskFolder = ask
		o.Made = store
	})
	return e, store
}

func newMaker(t *testing.T, name string, count int) *makerAdapter {
	return &makerAdapter{askAdapter: &askAdapter{name: name, available: true}, dir: t.TempDir(), count: count,
		makes: &core.MakesInfo{MaxCount: core.MaxMade, Types: core.MadeTypes}}
}

func TestARunBringsThePicturesAnAIMadeToItsPhoneOnly(t *testing.T) {
	cx := newMaker(t, "codex", 2)
	cl := &askAdapter{name: "claude", available: true} // draws with code only
	e, store := madeEnv(t, cx, cl)
	w := e.post("/v1/runs", map[string]any{"mode": "ask", "prompt": "고양이 사진 만들어줘", "pictures": true})
	if w.Code != 202 {
		t.Fatalf("POST = %d %s", w.Code, w.Body)
	}
	var st RunStatus
	json.Unmarshal(w.Body.Bytes(), &st)
	st = e.waitRun(t, st.ID)

	p := st.Parts[0]
	if len(p.Images) != 2 || p.ImagesLeftOut != 0 || p.PictureLimit != nil {
		t.Fatalf("codex part = %+v", p)
	}
	if p.Answer != "Here it is: picture-1.png" {
		t.Errorf("answer = %q", p.Answer)
	}
	if req := cx.requests()[0]; !req.Pictures || !strings.Contains(req.Prompt, core.GuideMake) || strings.Contains(req.Prompt, core.GuideDraw) {
		t.Errorf("codex was asked without its tool or guide: %v %q", req.Pictures, req.Prompt)
	}
	if req := cl.requests()[0]; req.Pictures || !strings.Contains(req.Prompt, core.GuideDraw) {
		t.Errorf("claude was asked with pictures: %v %q", req.Pictures, req.Prompt)
	}
	if len(st.Parts[1].Images) != 0 {
		t.Errorf("claude part has pictures: %+v", st.Parts[1])
	}
	img := p.Images[0]
	if !made.ValidID(img.ID) || img.Type != core.ImagePNG || img.Bytes != int64(len(madePNGBytes)) || img.Width != 1024 || img.Height != 1024 {
		t.Errorf("image = %+v", img)
	}

	target := "/v1/runs/" + st.ID + "/images/" + img.ID
	got := e.send("GET", target, "", nil)
	if got.Code != 200 || got.Header().Get("Content-Type") != core.ImagePNG || !bytes.Equal(got.Body.Bytes(), madePNGBytes) ||
		got.Header().Get("Cache-Control") != "no-store" || got.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("GET = %d %v %d bytes", got.Code, got.Header(), got.Body.Len())
	}
	// Another paired phone, another run's id, a made-up id, a path: nothing.
	other := e.addDevice(t, "other phone")
	for _, c := range []struct{ token, target string }{
		{other, target},
		{"", "/v1/runs/" + st.ID + "/images/img_" + strings.Repeat("0", 32)},
		{"", "/v1/runs/nope/images/" + img.ID},
		{"", "/v1/runs/" + st.ID + "/images/..%2f..%2fsecret.png"},
	} {
		if w := e.send("GET", c.target, c.token, nil); w.Code != 404 {
			t.Errorf("GET %s = %d %s", c.target, w.Code, w.Body)
		} else if c.target != "/v1/runs/"+st.ID+"/images/..%2f..%2fsecret.png" && errCode(t, w) != CodeImageNotFound {
			t.Errorf("GET %s code = %s", c.target, w.Body)
		}
	}
	if store.Count() != 2 {
		t.Errorf("store keeps %d", store.Count())
	}
	if logs := e.logs.String(); strings.Contains(logs, cx.dir) || strings.Contains(logs, store.Dir()) || !strings.Contains(logs, `"pictures":true`) {
		t.Errorf("the log should name no folder: %s", logs)
	}
	// Unpairing takes the phone's pictures off the PC.
	if w := e.send("DELETE", "/v1/devices/self", "", nil); w.Code != 200 {
		t.Fatalf("unpair = %d", w.Code)
	}
	if store.Count() != 0 {
		t.Error("an unpaired phone's pictures stay")
	}
}

func TestPicturesOnlyWhenThePhoneAsksAndTheAIIsOn(t *testing.T) {
	for _, c := range []struct {
		name string
		body map[string]any
	}{
		{"older app", map[string]any{}},
		{"turned off", map[string]any{"pictures": true, "noPictures": []string{"codex"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			cx := newMaker(t, "codex", 1)
			e, store := madeEnv(t, cx)
			body := map[string]any{"mode": "ask", "prompt": "고양이 사진 만들어줘"}
			for k, v := range c.body {
				body[k] = v
			}
			w := e.post("/v1/runs", body)
			var st RunStatus
			json.Unmarshal(w.Body.Bytes(), &st)
			st = e.waitRun(t, st.ID)
			if req := cx.requests()[0]; req.Pictures || !strings.Contains(req.Prompt, core.GuideDraw) {
				t.Errorf("asked with pictures: %+v", req)
			}
			if len(st.Parts[0].Images) != 0 || store.Count() != 0 {
				t.Errorf("part = %+v", st.Parts[0])
			}
		})
	}
}

func TestNoPicturesWithoutTheStoreOrForTheModerator(t *testing.T) {
	cx := newMaker(t, "codex", 1)
	e := newEnv(t, cx, func(o *Options) { o.AskFolder = filepath.Join(t.TempDir(), "ask") }) // no Made
	w := e.post("/v1/runs", map[string]any{"mode": "ask", "prompt": "고양이 사진 만들어줘", "pictures": true})
	var st RunStatus
	json.Unmarshal(w.Body.Bytes(), &st)
	e.waitRun(t, st.ID)
	if req := cx.requests()[0]; req.Pictures {
		t.Error("an agent without a store gave the AI its picture tool")
	}
}

func TestAPictureLimitGuidesToDrawAndAConversationWithoutTheToolMakesNone(t *testing.T) {
	ag := newMaker(t, "agy", 1)
	ag.makes.LimitedUntil = "2026-11-28T00:00:00Z"
	ag.resumes = map[string]bool{"with-tool": true}
	e, _ := madeEnv(t, ag)

	ask := func(body map[string]any) core.AskRequest {
		t.Helper()
		before := len(ag.requests())
		w := e.post("/v1/runs", body)
		if w.Code != 202 {
			t.Fatalf("POST = %d %s", w.Code, w.Body)
		}
		var st RunStatus
		json.Unmarshal(w.Body.Bytes(), &st)
		e.waitRun(t, st.ID)
		return ag.requests()[before]
	}
	// The limit is reached: the AI keeps its tool and is guided to draw.
	if req := ask(map[string]any{"mode": "ask", "prompt": "그려줘", "pictures": true}); !req.Pictures || !strings.Contains(req.Prompt, core.GuideDraw) {
		t.Errorf("limited: %v %q", req.Pictures, req.Prompt)
	}
	ag.makes.LimitedUntil = ""
	follow := func(session string) core.AskRequest {
		return ask(map[string]any{"mode": "ask", "prompt": "다시", "pictures": true,
			"agents": []map[string]any{{"agent": "agy", "session": "agy:" + session}}})
	}
	if req := follow("with-tool"); !req.Pictures || !strings.Contains(req.Prompt, core.GuideMake) {
		t.Errorf("conversation with the tool: %v", req.Pictures)
	}
	if req := follow("without-tool"); req.Pictures || !strings.Contains(req.Prompt, core.GuideDraw) {
		t.Errorf("conversation without the tool: %v", req.Pictures)
	}
}

func TestInfoSaysWhichAIsMakePictures(t *testing.T) {
	cx := newMaker(t, "codex", 1)
	ag := newMaker(t, "agy", 1)
	ag.makes = nil // cannot now (signed out)
	cl := &askAdapter{name: "claude", available: true}
	e, _ := madeEnv(t, cx, ag, cl)
	w := e.send("GET", "/v1/info", "", nil)
	var info struct {
		Agents []struct {
			Agent string `json:"agent"`
			Ask   *struct {
				Makes *core.MakesInfo `json:"makes"`
			} `json:"ask"`
		} `json:"agents"`
	}
	json.Unmarshal(w.Body.Bytes(), &info)
	got := map[string]*core.MakesInfo{}
	for _, a := range info.Agents {
		got[a.Agent] = a.Ask.Makes
	}
	if m := got["codex"]; m == nil || m.MaxCount != 4 || len(m.Types) != 3 {
		t.Errorf("codex makes = %+v", m)
	}
	if got["agy"] != nil || got["claude"] != nil {
		t.Errorf("makes = %+v", got)
	}
}
