package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/compares"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/uploads"
)

// Photos with a question (#14).

var (
	testJPEG = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{7}, 200)...)
	testPNG  = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{8}, 200)...)
)

// photoAdapter is an askAdapter that sees photos, or only with one model.
type photoAdapter struct {
	*askAdapter
	only string // "" every model sees; else only this one
}

func (a photoAdapter) SeesImages(model string) bool { return a.only == "" || model == a.only }
func (a photoAdapter) ImageModels() core.ImageInfo {
	if a.only == "" {
		return core.ImageInfo{All: true}
	}
	return core.ImageInfo{Models: []string{a.only}}
}

// photoEnv serves adapters with an ask folder and an uploads store whose
// clock the test moves.
func photoEnv(t *testing.T, adapters ...core.Adapter) (env, *uploads.Store, *time.Time) {
	t.Helper()
	store, err := uploads.Open(filepath.Join(t.TempDir(), "uploads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	ask := filepath.Join(t.TempDir(), "ask")
	e := newEnv(t, adapters[0], func(o *Options) {
		o.Adapters = adapters
		o.AskFolder = ask
		o.Uploads = store
	})
	return e, store, &now
}

// send makes one request with a raw body, as token (e's own when "").
func (e env) send(method, target, token string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, bytes.NewReader(body))
	r.Host = "127.0.0.1:8790"
	if token == "" {
		token = e.token
	}
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e env) uploadPhoto(t *testing.T, data []byte) uploads.Upload {
	t.Helper()
	w := e.send("POST", "/v1/uploads", "", data)
	if w.Code != 201 {
		t.Fatalf("POST /v1/uploads = %d %s", w.Code, w.Body)
	}
	var up uploads.Upload
	if err := json.Unmarshal(w.Body.Bytes(), &up); err != nil || !core.ValidImageID(up.ID) {
		t.Fatalf("upload answer = %s", w.Body)
	}
	return up
}

func TestInfoSaysWhetherPhotosAreTaken(t *testing.T) {
	cl := photoAdapter{askAdapter: &askAdapter{name: "claude", available: true}}
	oc := photoAdapter{askAdapter: &askAdapter{name: "opencode", available: true}, only: "opencode-std"}
	plain := &askAdapter{name: "codex", available: true}
	e, _, _ := photoEnv(t, cl, oc, plain)
	var info struct {
		Attachments *AttachmentsInfo `json:"attachments"`
		Agents      []struct {
			Agent string `json:"agent"`
			Ask   struct {
				Images *core.ImageInfo `json:"images"`
			} `json:"ask"`
		} `json:"agents"`
	}
	w := e.do("GET", "/v1/info", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	a := info.Attachments
	if a == nil || a.MaxCount != 4 || a.MaxBytes != 4<<20 || strings.Join(a.Types, ",") != "image/jpeg,image/png,image/webp" {
		t.Fatalf("attachments = %+v (%s)", a, w.Body)
	}
	if im := info.Agents[0].Ask.Images; im == nil || !im.All {
		t.Errorf("claude images = %+v", im)
	}
	if im := info.Agents[1].Ask.Images; im == nil || im.All || len(im.Models) != 1 || im.Models[0] != "opencode-std" {
		t.Errorf("opencode images = %+v", im)
	}
	if im := info.Agents[2].Ask.Images; im != nil {
		t.Errorf("an AI that takes no photos says images = %+v", im)
	}

	// Without an uploads store the agent says nothing and takes nothing.
	off, _ := multiEnv(t, cl)
	if w := off.do("GET", "/v1/info", nil); strings.Contains(w.Body.String(), "attachments") {
		t.Errorf("info without a store: %s", w.Body)
	}
	if w := off.send("POST", "/v1/uploads", "", testJPEG); w.Code != 503 || errCode(t, w) != CodeUploadsOff {
		t.Errorf("upload without a store = %d %s", w.Code, w.Body)
	}
	if w := off.post("/v1/runs", map[string]any{"mode": "ask", "prompt": "q", "attachments": []string{"up_" + strings.Repeat("0", 32)}}); w.Code != 503 || errCode(t, w) != CodeUploadsOff {
		t.Errorf("run with photos without a store = %d %s", w.Code, w.Body)
	}
}

func TestUploadKeepsOnlyPhotos(t *testing.T) {
	cl := photoAdapter{askAdapter: &askAdapter{name: "claude", available: true}}
	e, store, _ := photoEnv(t, cl)
	up := e.uploadPhoto(t, testPNG)
	if up.Type != "image/png" || up.Bytes != int64(len(testPNG)) || store.Count() != 1 {
		t.Fatalf("upload = %+v, kept %d", up, store.Count())
	}
	for name, body := range map[string][]byte{"text": []byte("just words"), "empty": nil, "script": []byte("#!/bin/sh\nrm -rf ~\n")} {
		if w := e.send("POST", "/v1/uploads", "", body); w.Code != 415 || errCode(t, w) != CodeUploadBadType {
			t.Errorf("%s = %d %s", name, w.Code, w.Body)
		}
	}
	big := append([]byte{0xFF, 0xD8, 0xFF}, make([]byte, core.MaxImageBytes)...)
	if w := e.send("POST", "/v1/uploads", "", big); w.Code != 413 || errCode(t, w) != CodeUploadTooLarge {
		t.Errorf("large = %d %s", w.Code, w.Body)
	}
	// A device without a token uploads nothing.
	if w := e.send("POST", "/v1/uploads", "wrong-token", testJPEG); w.Code != 401 {
		t.Errorf("no token = %d", w.Code)
	}
	for i := store.Count(); i < uploads.MaxPerOwner; i++ {
		e.uploadPhoto(t, testJPEG)
	}
	if w := e.send("POST", "/v1/uploads", "", testJPEG); w.Code != 429 || errCode(t, w) != CodeUploadLimit {
		t.Errorf("over the device's limit = %d %s", w.Code, w.Body)
	}
	// Nothing of a photo is in the log: no path, no bytes.
	if logs := e.logs.String(); strings.Contains(logs, store.Dir()) || strings.Contains(logs, "up_") {
		t.Errorf("a photo's path or id in the log: %s", logs)
	}
}

func TestRunGivesThePhotosToEveryAIThatSees(t *testing.T) {
	cl := photoAdapter{askAdapter: &askAdapter{name: "claude", available: true}}
	cx := photoAdapter{askAdapter: &askAdapter{name: "codex", available: true}}
	blind := &askAdapter{name: "opencode", available: true} // no core.ImageSeer
	e, store, now := photoEnv(t, cl, cx, blind)
	a, b := e.uploadPhoto(t, testJPEG), e.uploadPhoto(t, testPNG)

	w := e.post("/v1/runs", map[string]any{"mode": "ask", "prompt": "이 사진에 뭐가 있어?", "attachments": []string{a.ID, b.ID}, "clientRunId": "r-1"})
	if w.Code != 202 {
		t.Fatalf("POST = %d %s", w.Code, w.Body)
	}
	var st RunStatus
	json.Unmarshal(w.Body.Bytes(), &st)
	// The same run sent again asks nobody again and holds nothing twice.
	if w := e.post("/v1/runs", map[string]any{"mode": "ask", "prompt": "이 사진에 뭐가 있어?", "attachments": []string{a.ID, b.ID}, "clientRunId": "r-1"}); w.Code != 202 {
		t.Fatalf("POST again = %d %s", w.Code, w.Body)
	}
	st = e.waitRun(t, st.ID)
	if st.Parts[0].Status != "done" || st.Parts[1].Status != "done" {
		t.Fatalf("run = %+v", st)
	}
	if p := st.Parts[2]; p.Error == nil || p.Error.Code != core.ErrCodeNoImages {
		t.Fatalf("an AI that takes no photos = %+v", p)
	}
	if len(blind.requests()) != 0 {
		t.Error("the AI that takes no photos was asked")
	}
	for _, ad := range []photoAdapter{cl, cx} {
		reqs := ad.requests()
		if len(reqs) != 1 || len(reqs[0].Images) != 2 {
			t.Fatalf("%s got %+v", ad.name, reqs)
		}
		if err := core.CheckImages(reqs[0].Images); err != nil {
			t.Errorf("%s: %v", ad.name, err)
		}
		for i, want := range []struct{ typ, id string }{{"image/jpeg", a.ID}, {"image/png", b.ID}} {
			im := reqs[0].Images[i]
			if im.Type != want.typ || filepath.Dir(im.Path) != store.Dir() || !strings.HasPrefix(filepath.Base(im.Path), want.id+".") {
				t.Errorf("%s photo %d = %+v", ad.name, i, im)
			}
		}
		// The prompt is the question as it always was: no path in it.
		if reqs[0].Prompt != askPrompt("이 사진에 뭐가 있어?") {
			t.Errorf("%s prompt = %q", ad.name, reqs[0].Prompt)
		}
	}

	// The run has ended: ten minutes later the photos are gone from the PC.
	waitFor(t, func() bool {
		*now = now.Add(uploads.KeepFor + time.Second)
		store.Sweep()
		return store.Count() == 0
	})
	if left, _ := os.ReadDir(store.Dir()); len(left) != 0 {
		t.Errorf("%d files left in the uploads folder", len(left))
	}
	if w := e.post("/v1/runs", map[string]any{"mode": "ask", "prompt": "한 번 더", "attachments": []string{a.ID}}); w.Code != 409 || errCode(t, w) != CodeUploadGone {
		t.Errorf("a run with a deleted photo = %d %s", w.Code, w.Body)
	}
	if logs := e.logs.String(); strings.Contains(logs, store.Dir()) || strings.Contains(logs, a.ID) || !strings.Contains(logs, `"photos":2`) {
		t.Errorf("the log should count the photos and name none: %s", logs)
	}
}

// waitFor tries ok for up to five seconds.
func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("still waiting")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAHeldPhotoOutlivesTheSweep(t *testing.T) {
	slow := &slowAdapter{askAdapter: &askAdapter{name: "claude", available: true}, release: make(chan struct{})}
	e, store, now := photoEnv(t, slow)
	a := e.uploadPhoto(t, testJPEG)
	w := e.post("/v1/runs", map[string]any{"mode": "ask", "prompt": "q", "attachments": []string{a.ID}})
	if w.Code != 202 {
		t.Fatalf("POST = %d %s", w.Code, w.Body)
	}
	var st RunStatus
	json.Unmarshal(w.Body.Bytes(), &st)
	*now = now.Add(3 * uploads.KeepFor)
	store.Sweep()
	e.send("DELETE", "/v1/uploads/"+a.ID, "", nil)
	if store.Count() != 1 {
		t.Fatal("a photo was deleted while its question was still being answered")
	}
	close(slow.release)
	e.waitRun(t, st.ID)
	waitFor(t, func() bool {
		*now = now.Add(uploads.KeepFor + time.Second)
		store.Sweep()
		return store.Count() == 0
	})
}

// slowAdapter sees photos and answers when release is closed.
type slowAdapter struct {
	*askAdapter
	release chan struct{}
}

func (a *slowAdapter) SeesImages(string) bool      { return true }
func (a *slowAdapter) ImageModels() core.ImageInfo { return core.ImageInfo{All: true} }
func (a *slowAdapter) Ask(ctx context.Context, req core.AskRequest) (core.AskResult, error) {
	select {
	case <-a.release:
	case <-ctx.Done():
	}
	return a.askAdapter.Ask(ctx, req)
}

func TestRunRefusesPhotosThatAreNotItsOwn(t *testing.T) {
	cl := photoAdapter{askAdapter: &askAdapter{name: "claude", available: true}}
	e, store, _ := photoEnv(t, cl)
	a := e.uploadPhoto(t, testJPEG)
	run := func(token string, ids ...string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]any{"mode": "ask", "prompt": "q", "attachments": ids})
		return e.send("POST", "/v1/runs", token, b)
	}
	for name, ids := range map[string][]string{
		"a path":       {"../../secret.png"},
		"a file name":  {a.ID + ".jpg"},
		"upper case":   {strings.ToUpper(a.ID)},
		"twice":        {a.ID, a.ID},
		"five":         {a.ID, a.ID, a.ID, a.ID, a.ID},
		"an empty one": {""},
	} {
		if w := run("", ids...); w.Code != 400 || errCode(t, w) != CodeBadRequest {
			t.Errorf("%s = %d %s", name, w.Code, w.Body)
		}
	}
	if w := run("", "up_"+strings.Repeat("f", 32)); w.Code != 409 || errCode(t, w) != CodeUploadGone {
		t.Errorf("an unknown id = %d %s", w.Code, w.Body)
	}
	// Another paired device neither uses nor deletes this one's photo.
	other := e.addDevice(t, "tablet")
	if w := run(other, a.ID); w.Code != 409 || errCode(t, w) != CodeUploadGone {
		t.Errorf("another device's run = %d %s", w.Code, w.Body)
	}
	if w := e.send("DELETE", "/v1/uploads/"+a.ID, other, nil); w.Code != 204 || store.Count() != 1 {
		t.Errorf("another device's delete = %d, kept %d", w.Code, store.Count())
	}
	if len(cl.requests()) != 0 {
		t.Error("an AI was asked by a refused run")
	}
	// The owner takes it off the question: gone at once.
	if w := e.send("DELETE", "/v1/uploads/"+a.ID, "", nil); w.Code != 204 || store.Count() != 0 {
		t.Errorf("the owner's delete = %d, kept %d", w.Code, store.Count())
	}
	if w := e.send("DELETE", "/v1/uploads/not-an-id", "", nil); w.Code != 204 {
		t.Errorf("a delete of nothing = %d", w.Code)
	}
}

func TestARefusedRunLetsItsPhotosGo(t *testing.T) {
	slow := &slowAdapter{askAdapter: &askAdapter{name: "claude", available: true}, release: make(chan struct{})}
	e, store, now := photoEnv(t, slow)
	// A conversation that is still answering refuses a second question in
	// it, after that question's photos were looked up.
	w := e.post("/v1/runs", map[string]any{"mode": "ask", "prompt": "first", "agents": []map[string]string{{"agent": "claude", "session": "claude:s-1"}}})
	if w.Code != 202 {
		t.Fatalf("POST = %d %s", w.Code, w.Body)
	}
	var first RunStatus
	json.Unmarshal(w.Body.Bytes(), &first)
	a := e.uploadPhoto(t, testJPEG)
	w = e.post("/v1/runs", map[string]any{"mode": "ask", "prompt": "second", "attachments": []string{a.ID},
		"agents": []map[string]string{{"agent": "claude", "session": "claude:s-1"}}})
	if w.Code != 409 || errCode(t, w) != core.ErrCodeBusy {
		t.Fatalf("second question = %d %s", w.Code, w.Body)
	}
	*now = now.Add(uploads.KeepFor + time.Second)
	store.Sweep()
	if store.Count() != 0 {
		t.Fatal("a refused run still holds its photo")
	}
	close(slow.release)
	e.waitRun(t, first.ID)
}

// photoWriter is a writerAdapter that sees photos, with every model or none.
type photoWriter struct {
	*writerAdapter
	blind bool
}

func (p photoWriter) SeesImages(string) bool { return !p.blind }
func (p photoWriter) ImageModels() core.ImageInfo {
	return core.ImageInfo{All: !p.blind}
}

// The writer of the combined answer gets the question's photos when its
// model sees photos, with one line in its prompt that says so; a writer
// that does not see them writes from the answers alone.
func TestTheCombinedAnswerGetsThePhotos(t *testing.T) {
	cl, cx, oc := newWriterAdapter("claude", false), newWriterAdapter("codex", false), newWriterAdapter("opencode", false)
	store, err := uploads.Open(filepath.Join(t.TempDir(), "uploads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	e, _ := judgeEnv(t, func(o *Options) { o.Uploads = store },
		photoWriter{writerAdapter: cl, blind: true}, photoWriter{writerAdapter: cx}, photoWriter{writerAdapter: oc})
	a, b := e.uploadPhoto(t, testJPEG), e.uploadPhoto(t, testPNG)

	st := startRun(t, e, map[string]any{"mode": "ask", "prompt": "사진 속 표지판은 무슨 뜻이야?", "attachments": []string{a.ID, b.ID},
		"compare": map[string]any{"synthesis": map[string]any{"agent": "codex"}}})
	c := e.waitSynthesis(t, st.ComparisonID)
	if c.Synthesis.Status != compares.SynthDone || c.Synthesis.Agent != "codex" {
		t.Fatalf("synthesis = %+v", c.Synthesis)
	}
	calls := cx.synthCalls()
	if len(calls) != 1 || len(calls[0].Images) != 2 || core.CheckImages(calls[0].Images) != nil ||
		calls[0].Images[0].Type != "image/jpeg" || filepath.Dir(calls[0].Images[1].Path) != store.Dir() {
		t.Fatalf("the writer's photos = %+v", calls)
	}
	if !strings.Contains(calls[0].Prompt, "\nPHOTOS: The person sent 2 photo(s) with the QUESTION;") || strings.Contains(calls[0].Prompt, store.Dir()) {
		t.Errorf("the writer's prompt does not say there are photos, or names a path:\n%s", calls[0].Prompt)
	}
	e.idle(t, st.ComparisonID)

	// Claude's fake sees none here: it writes from the answers alone, and
	// its prompt is the one of a question without photos.
	st = startRun(t, e, map[string]any{"mode": "ask", "prompt": "다시 물어볼게", "attachments": []string{a.ID},
		"compare": map[string]any{"synthesis": map[string]any{"agent": "claude"}}})
	c = e.waitSynthesis(t, st.ComparisonID)
	blind := cl.synthCalls()
	if c.Synthesis.Status != compares.SynthDone || len(blind) != 1 || len(blind[0].Images) != 0 || strings.Contains(blind[0].Prompt, "PHOTOS") {
		t.Fatalf("a writer that sees no photos = %+v, synthesis %+v", blind, c.Synthesis)
	}
	e.idle(t, st.ComparisonID)
	if logs := e.logs.String(); !strings.Contains(logs, `"photosSent":2`) || !strings.Contains(logs, `"photosSent":0`) || strings.Contains(logs, store.Dir()) {
		t.Errorf("the log should count the photos sent to the writer and name none: %s", logs)
	}
}

func TestUnpairingDropsTheDevicesPhotos(t *testing.T) {
	cl := photoAdapter{askAdapter: &askAdapter{name: "claude", available: true}}
	e, store, _ := photoEnv(t, cl)
	e.uploadPhoto(t, testJPEG)
	if w := e.send("DELETE", "/v1/devices/self", "", nil); w.Code != 200 {
		t.Fatalf("unpair = %d %s", w.Code, w.Body)
	}
	if store.Count() != 0 {
		t.Fatal("the photos of a device that unpaired itself stay on the PC")
	}
}
