package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// fakePreviewer answers a preview with text that must never reach the log.
type fakePreviewer struct {
	fakeAdapter
	err error
}

const previewSecret = "the user's private question"

func (f fakePreviewer) Preview(_ context.Context, id string) (core.Preview, error) {
	if f.err != nil {
		return core.Preview{}, f.err
	}
	return core.Preview{Turns: []core.Turn{
		{Role: "user", Text: previewSecret},
		{Role: "assistant", Text: "an answer", Tools: 2},
	}}, nil
}

func TestPreviewSession(t *testing.T) {
	e := newEnv(t, fakePreviewer{}, nil)
	w := e.do("GET", "/v1/sessions/"+sid+"/preview", nil)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var p core.Preview
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || len(p.Turns) != 2 || p.Turns[1].Tools != 2 {
		t.Fatalf("preview = %+v, %v", p, err)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	// Conversation text never goes to the agent log (N8).
	if strings.Contains(e.logs.String(), previewSecret) || strings.Contains(e.logs.String(), "an answer") {
		t.Errorf("log has conversation text: %s", e.logs)
	}

	// Paired devices only.
	if w := e.do("GET", "/v1/sessions/"+sid+"/preview", map[string]string{"Authorization": ""}); w.Code != 401 {
		t.Errorf("no token: status %d", w.Code)
	}
	if w := e.do("POST", "/v1/sessions/"+sid+"/preview", nil); w.Code != 405 {
		t.Errorf("POST: status %d", w.Code)
	}
	if w := e.do("GET", "/v1/sessions/nope/preview", nil); w.Code != 400 {
		t.Errorf("bad id: status %d", w.Code)
	}
}

func TestPreviewErrors(t *testing.T) {
	e := newEnv(t, fakePreviewer{err: core.Fail(core.ErrCodeNotFound, "")}, nil)
	if w := e.do("GET", "/v1/sessions/"+sid+"/preview", nil); w.Code != 404 || errCode(t, w) != core.ErrCodeNotFound {
		t.Errorf("missing session: %d %s", w.Code, w.Body)
	}
	// An adapter that cannot preview says so.
	e = newEnv(t, fakeAdapter{}, nil)
	if w := e.do("GET", "/v1/sessions/"+sid+"/preview", nil); w.Code != 501 || errCode(t, w) != CodeNotSupported {
		t.Errorf("no previewer: %d %s", w.Code, w.Body)
	}
}
