package api

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/compares"
	"github.com/heeo-dev/askorus/agent/internal/uploads"
)

// What the review of the photo upload found (#14, 2026-11-18).

// R-120: a photo that arrives more slowly than the server's write limit is
// still answered. The server counts that limit from the start of the
// request, so the answer to a slow upload came after it: the photo was kept
// and the phone, hearing nothing, sent it again.
func TestASlowUploadIsStillAnswered(t *testing.T) {
	cl := photoAdapter{askAdapter: &askAdapter{name: "claude", available: true}}
	e, store, _ := photoEnv(t, cl)
	srv := httptest.NewUnstartedServer(e.h)
	// The agent's server with its limits scaled down: reading 15 s and writing 60 s there.
	srv.Config.ReadTimeout = 150 * time.Millisecond
	srv.Config.WriteTimeout = 300 * time.Millisecond
	srv.Start()
	defer srv.Close()

	pr, pw := io.Pipe()
	go func() {
		pw.Write(testJPEG[:20])
		time.Sleep(700 * time.Millisecond) // longer than both limits
		pw.Write(testJPEG[20:])
		pw.Close()
	}()
	req, _ := http.NewRequest("POST", srv.URL+"/v1/uploads", pr)
	req.Host = "127.0.0.1:8790"
	req.Header.Set("Authorization", "Bearer "+e.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("a slow upload got no answer: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 201 || !bytes.Contains(body, []byte(`"type":"image/jpeg"`)) || store.Count() != 1 {
		t.Fatalf("a slow upload = %d %s, kept %d", res.StatusCode, body, store.Count())
	}
}

// R-121: the photos stay on the PC while the combined answer waits and is
// written. The run let them go when it ended and the writer only looked
// whether the files were there, so a sweep in between deleted photos the
// writer had already been given.
func TestTheCombinedAnswerHoldsThePhotos(t *testing.T) {
	cl, cx, oc := newWriterAdapter("claude", false), newWriterAdapter("codex", false), newWriterAdapter("opencode", false)
	store, err := uploads.Open(filepath.Join(t.TempDir(), "uploads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	e, _ := judgeEnv(t, func(o *Options) { o.Uploads = store },
		photoWriter{writerAdapter: cl}, photoWriter{writerAdapter: cx}, photoWriter{writerAdapter: oc})
	a := e.uploadPhoto(t, testJPEG)

	writing := make(chan struct{})
	cx.set("", writing) // Codex keeps writing until the test lets it end
	st := startRun(t, e, map[string]any{"mode": "ask", "prompt": "q", "attachments": []string{a.ID},
		"compare": map[string]any{"synthesis": map[string]any{"agent": "codex"}}})
	e.waitRun(t, st.ID)
	waitFor(t, func() bool { return len(cx.synthCalls()) == 1 })
	if got := cx.synthCalls()[0].Images; len(got) != 1 {
		t.Fatalf("the writer's photos = %+v", got)
	}

	// Long after the run ended, with the writer still at work: the photo stays.
	now = now.Add(3 * uploads.KeepFor)
	store.Sweep()
	e.send("DELETE", "/v1/uploads/"+a.ID, "", nil)
	if store.Count() != 1 {
		t.Fatal("the photo was deleted while the combined answer was being written")
	}
	close(writing)
	if c := e.waitSynthesis(t, st.ComparisonID); c.Synthesis.Status != compares.SynthDone {
		t.Fatalf("synthesis = %+v", c.Synthesis)
	}
	e.idle(t, st.ComparisonID)

	// Written: ten minutes later it is gone.
	waitFor(t, func() bool {
		now = now.Add(uploads.KeepFor + time.Second)
		store.Sweep()
		return store.Count() == 0
	})

	// A combined answer asked for only after the photos went (it was off
	// when the question was sent) is written from the answers alone: no
	// photo, and no line about photos in the prompt.
	b := e.uploadPhoto(t, testPNG)
	st = startRun(t, e, map[string]any{"mode": "ask", "prompt": "q2", "attachments": []string{b.ID},
		"compare": map[string]any{"synthesis": map[string]any{"off": true}}})
	e.waitRun(t, st.ID)
	e.waitSynthesis(t, st.ComparisonID)
	waitFor(t, func() bool {
		now = now.Add(uploads.KeepFor + time.Second)
		store.Sweep()
		return store.Count() == 0
	})
	if w := e.post("/v1/comparisons/"+st.ComparisonID+"/synthesis", map[string]any{"agent": "claude"}); w.Code != 202 {
		t.Fatalf("write = %d %s", w.Code, w.Body)
	}
	e.waitSynthesis(t, st.ComparisonID)
	e.idle(t, st.ComparisonID)
	calls := cl.synthCalls()
	if len(calls) != 1 || len(calls[0].Images) != 0 || bytes.Contains([]byte(calls[0].Prompt), []byte("PHOTOS")) {
		t.Fatalf("a combined answer after the photos went = %+v", calls)
	}
}
