//go:build live

package live

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/adapter/claude"
	"github.com/heeo-dev/askorus/agent/internal/adapter/codex"
	"github.com/heeo-dev/askorus/agent/internal/adapter/opencode"
	"github.com/heeo-dev/askorus/agent/internal/api"
	"github.com/heeo-dev/askorus/agent/internal/api/apitest"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/devices"
	"github.com/heeo-dev/askorus/agent/internal/judge"
	"github.com/heeo-dev/askorus/agent/internal/logx"
	"github.com/heeo-dev/askorus/agent/internal/uploads"
)

// The codes drawn in testdata/photo-k7392q.png and photo-m5521x.png. They
// are in no prompt, so an answer that has them read the photo.
const (
	photoCode1 = "K7392Q"
	photoCode2 = "M5521X"
)

// upload sends one photo like the phone does and returns its id.
func (e *liveEnv) upload(data []byte) string {
	e.t.Helper()
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/uploads", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+e.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var up uploads.Upload
	json.NewDecoder(res.Body).Decode(&up)
	if res.StatusCode != 201 || !core.ValidImageID(up.ID) {
		e.t.Fatalf("POST /v1/uploads = %d %+v", res.StatusCode, up)
	}
	return up.ID
}

// LT-16 Photos with a question (#14) with the real Claude Code, Codex and
// OpenCode on the quick tier. Two photos, each with a code drawn in it, are
// uploaded like the phone does and go with one question to all three: every
// answer must have both codes (Claude gets them on stdin, Codex with -i,
// OpenCode with --file). A follow-up without photos asks for the second
// code again: each AI still knows it from its own conversation. OpenCode's
// deep tier, whose model does not see photos, is not asked. Then each AI
// writes a combined answer with the photos attached to its locked-down
// one-off command. No file may be left in the uploads folder at the end.
// 9 AI calls.
func TestLT16Photos(t *testing.T) {
	claudeDir, err := claude.DefaultClaudeDir()
	if err != nil {
		t.Fatal(err)
	}
	folder := askFolder(t)
	store, err := uploads.Open(filepath.Join(t.TempDir(), "uploads"))
	if err != nil {
		t.Fatal(err)
	}
	devs, _ := devices.Open(t.TempDir())
	token, _, err := devs.Add("live test")
	if err != nil {
		t.Fatal(err)
	}
	cl := claude.New(claude.Options{ClaudeDir: claudeDir})
	cx := codex.New(codex.Options{SchemaDir: t.TempDir()})
	oc := opencode.New(opencode.Options{})
	srv := httptest.NewServer(apitest.AsOwner(api.New(apitest.Node(api.Options{
		Version: "live", Hostname: "PC", Adapters: []core.Adapter{cl, cx, oc}, Devices: devs, Logger: logx.Discard(),
		AskFolder: folder, Uploads: store,
	})).Handler()))
	t.Cleanup(srv.Close)
	e := &liveEnv{t: t, srv: srv, token: token, folder: folder}

	_, info := e.call("GET", "/v1/info", nil)
	if at, _ := info["attachments"].(map[string]any); at == nil || at["maxCount"] != float64(core.MaxImages) {
		t.Fatalf("info.attachments = %v", info["attachments"])
	}
	var agents []map[string]string
	for _, x := range info["agents"].([]any) {
		a := x.(map[string]any)
		ask, _ := a["ask"].(map[string]any)
		t.Logf("%s %v available=%v images=%v", a["agent"], a["version"], a["available"], ask["images"])
		if a["available"] != true || ask == nil || ask["images"] == nil {
			t.Errorf("%s takes no photos on this PC", a["agent"])
			continue
		}
		agents = append(agents, map[string]string{"agent": a["agent"].(string)})
	}
	if len(agents) == 0 {
		t.Fatal("no AI takes photos")
	}

	var photos [][]byte
	for _, name := range []string{"photo-k7392q.png", "photo-m5521x.png"} {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		photos = append(photos, b)
	}
	ids := []string{e.upload(photos[0]), e.upload(photos[1])}

	parts, took := e.run(map[string]any{"mode": "ask", "tier": "quick", "agents": agents, "attachments": ids,
		"prompt": "Each photo shows one code. Answer with both codes only, the first photo's first."})
	t.Logf("question with two photos: %s in all", took.Round(100*time.Millisecond))
	sessions := map[string]string{}
	for name, p := range parts {
		t.Logf("  %s %v: %v ms, %v in / %v out tokens", name, p["model"], p["ms"], p["inputTokens"], p["outputTokens"])
		answer, _ := p["answer"].(string)
		if p["status"] != "done" || !strings.Contains(answer, photoCode1) || !strings.Contains(answer, photoCode2) || p["session"] == nil {
			t.Errorf("%s did not read the two photos: %v", name, p)
			continue
		}
		sessions[name] = p["session"].(string)
	}

	// A follow-up without photos: each AI's own conversation has them.
	var follow []map[string]string
	for name, s := range sessions {
		follow = append(follow, map[string]string{"agent": name, "session": s})
	}
	if len(follow) > 0 {
		parts, took = e.run(map[string]any{"mode": "ask", "tier": "quick", "agents": follow,
			"prompt": "What was the code in the second photo I sent? Answer with the code only."})
		t.Logf("follow-up without photos: %s in all", took.Round(100*time.Millisecond))
		for name, p := range parts {
			answer, _ := p["answer"].(string)
			if p["status"] != "done" || !strings.Contains(answer, photoCode2) || strings.Contains(answer, photoCode1) {
				t.Errorf("%s follow-up: %v", name, p)
			}
		}
	}

	// A model that does not see photos is not asked: OpenCode's deep tier.
	parts, _ = e.run(map[string]any{"mode": "ask", "tier": "deep", "attachments": ids[:1],
		"agents": []map[string]string{{"agent": "opencode"}}, "prompt": "What code is in the photo?"})
	if perr, _ := parts["opencode"]["error"].(map[string]any); parts["opencode"]["status"] != "failed" || perr == nil || perr["code"] != core.ErrCodeNoImages {
		t.Errorf("OpenCode's deep tier with a photo: %v", parts["opencode"])
	}

	// The combined answer's locked-down command takes the photos too.
	held, release, err := store.Hold(devs2owner(t, devs, token), ids)
	if err != nil {
		t.Fatalf("the photos are gone before the combined answers: %v", err)
	}
	judgeDir := core.JudgeFolder(folder)
	if err := os.MkdirAll(judgeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	task := judge.SynthTask{Question: "What code does each photo show?", Photos: len(held), Answers: []judge.SynthAnswer{
		{AI: "Claude", Text: "The first photo shows " + photoCode1 + " and the second " + photoCode2 + "."},
		{AI: "Codex", Text: "First: " + photoCode1 + ". Second: " + photoCode2 + "."},
	}}
	for _, w := range []struct {
		name string
		s    core.Synthesizer
		tier core.AskModel
	}{{"claude", cl, cl.AskTiers()[core.TierQuick]}, {"codex", cx, cx.AskTiers()[core.TierQuick]}, {"opencode", oc, oc.AskTiers()[core.TierQuick]}} {
		sp := judge.SynthPrompt(task, judge.SynthNonce(task))
		ctx, cancel := context.WithTimeout(context.Background(), core.SynthTimeout+30*time.Second)
		start := time.Now()
		out, err := w.s.Synthesize(ctx, core.SynthRequest{Folder: judgeDir, Prompt: sp, Model: w.tier.Model, Effort: w.tier.Effort, Images: held})
		cancel()
		if err != nil {
			t.Errorf("%s combined answer with photos: %v", w.name, err)
			continue
		}
		text, verr := judge.ValidateSynth(out.Output)
		if verr != nil || !strings.Contains(text, photoCode1) || !strings.Contains(text, photoCode2) {
			t.Errorf("%s combined answer: %v %q", w.name, verr, text)
			continue
		}
		t.Logf("  %s combined answer with photos: %s, %d in tokens", w.name, time.Since(start).Round(100*time.Millisecond), out.InputTokens)
		if c, ok := w.s.(core.JudgeCleaner); ok && out.Cleanup != "" {
			cctx, ccancel := context.WithTimeout(context.Background(), core.JudgeCleanupTimeout)
			_ = c.CleanupJudge(cctx, judgeDir, []string{out.Cleanup})
			ccancel()
		}
	}
	release()

	// Closing the store is what the agent does when it stops: nothing stays.
	dir := store.Dir()
	store.Close()
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("%d files left in the uploads folder", len(left))
	}
}

// devs2owner is the device id of token: the owner of its uploads.
func devs2owner(t *testing.T, devs *devices.Store, token string) string {
	t.Helper()
	d, ok := devs.Verify(token)
	if !ok {
		t.Fatal("the test's own token is not a device")
	}
	return d.ID
}
