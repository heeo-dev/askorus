package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/devices"
	"github.com/asksemble/asksemble/agent/internal/logx"
	"github.com/asksemble/asksemble/agent/internal/pairing"
)

type pairEnv struct {
	h   http.Handler
	dir string
}

func newPairEnv(t *testing.T, owner string) pairEnv {
	t.Helper()
	dir := t.TempDir()
	store, err := devices.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := &pairing.Pairer{Dir: dir, Devices: store, Poll: 10 * time.Millisecond, Timeout: 2 * time.Second}
	h := New(Options{
		Adapters: []core.Adapter{fakeAdapter{}}, Devices: store, Pairer: p,
		Logger: logx.Discard(), TailscaleOwner: owner, ServeHosts: []string{"pc.tail.ts.net"},
	}).Handler()
	return pairEnv{h: h, dir: dir}
}

// viaServe builds a request as tailscale serve would forward it.
func viaServe(method, path, body string, hdr map[string]string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "pc.tail.ts.net:8800"
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("Tailscale-User-Login", "owner@example.com")
	r.Header.Set("X-Forwarded-For", "100.64.0.9")
	for k, v := range hdr {
		if v == "" {
			r.Header.Del(k)
		} else {
			r.Header.Set(k, v)
		}
	}
	return r
}

func (e pairEnv) serve(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func TestPairFlow(t *testing.T) {
	e := newPairEnv(t, "Owner@Example.com")
	code, win, err := pairing.Open(e.dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	go func() { // the person at the PC allows it
		for i := 0; i < 400; i++ {
			if _, ok := pairing.PendingRequest(e.dir, win.ID); ok {
				pairing.Decide(e.dir, win.ID, true)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	w := e.serve(viaServe("POST", "/v1/pair", `{"code":"`+code+`","deviceName":"Galaxy"}`, nil))
	if w.Code != 200 {
		t.Fatalf("pair = %d %s", w.Code, w.Body)
	}
	var resp PairResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.HasPrefix(resp.Token, devices.TokenPrefix) || resp.DeviceName != "Galaxy" || resp.APIVersion != 1 {
		t.Errorf("response = %+v", resp)
	}

	r := viaServe("GET", "/v1/info", "", map[string]string{"Authorization": "Bearer " + resp.Token})
	if w := e.serve(r); w.Code != 200 {
		t.Errorf("new token refused: %d", w.Code)
	}

	w = e.serve(viaServe("POST", "/v1/pair", `{"code":"`+code+`","deviceName":"Again"}`, nil))
	if w.Code != 404 || errCode(t, w) != CodePairClosed {
		t.Errorf("reused code = %d %s", w.Code, w.Body)
	}
}

func TestPairRefusals(t *testing.T) {
	e := newPairEnv(t, "owner@example.com")
	code, _, _ := pairing.Open(e.dir, time.Now())
	body := `{"code":"` + code + `","deviceName":"phone"}`
	cases := []struct {
		name   string
		r      *http.Request
		status int
		code   string
	}{
		{"no login header (direct local call)", viaServe("POST", "/v1/pair", body, map[string]string{"Tailscale-User-Login": ""}), 403, CodeNotTailnet},
		{"not from a tailnet address", viaServe("POST", "/v1/pair", body, map[string]string{"X-Forwarded-For": "192.168.0.5"}), 403, CodeNotTailnet},
		{"spoofed first hop", viaServe("POST", "/v1/pair", body, map[string]string{"X-Forwarded-For": "100.64.0.9, 192.168.0.5"}), 403, CodeNotTailnet},
		{"other account", viaServe("POST", "/v1/pair", body, map[string]string{"Tailscale-User-Login": "kid@example.com"}), 403, CodeWrongAccount},
		{"bad json", viaServe("POST", "/v1/pair", `{"code":`, nil), 400, CodeBadRequest},
		{"wrong code", viaServe("POST", "/v1/pair", `{"code":"AAAA","deviceName":"phone"}`, nil), 403, CodePairBadCode},
		{"GET", viaServe("GET", "/v1/pair", "", nil), 405, CodeMethod},
		{"browser", viaServe("POST", "/v1/pair", body, map[string]string{"Origin": "https://evil.example"}), 403, CodeCrossOrigin},
	}
	for _, c := range cases {
		w := e.serve(c.r)
		if w.Code != c.status || errCode(t, w) != c.code {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body)
		}
	}
	// None of those consumed the window.
	if w := e.serve(viaServe("POST", "/v1/pair", body, nil)); w.Code == 404 {
		t.Errorf("window closed by refused requests")
	}
}

func TestPairNeedsKnownOwner(t *testing.T) {
	e := newPairEnv(t, "")
	code, _, _ := pairing.Open(e.dir, time.Now())
	w := e.serve(viaServe("POST", "/v1/pair", `{"code":"`+code+`","deviceName":"phone"}`, nil))
	if w.Code != 503 || errCode(t, w) != CodeOwnerUnknown {
		t.Errorf("unknown owner = %d %s", w.Code, w.Body)
	}
}

func TestFailureLimit(t *testing.T) {
	e := newEnv(t, fakeAdapter{}, nil)
	bad := map[string]string{"Authorization": "Bearer ask_guess", "X-Forwarded-For": "100.64.0.20"}
	for i := 0; i < maxFails; i++ {
		if w := e.do("GET", "/v1/info", bad); w.Code != 401 {
			t.Fatalf("try %d = %d", i, w.Code)
		}
	}
	// Now even the right token is refused from that address...
	w := e.do("GET", "/v1/info", map[string]string{"X-Forwarded-For": "100.64.0.20"})
	if w.Code != 429 || errCode(t, w) != CodeTooMany || w.Header().Get("Retry-After") == "" {
		t.Errorf("blocked client = %d %s", w.Code, w.Body)
	}
	// ...but other addresses still work.
	if w := e.do("GET", "/v1/info", map[string]string{"X-Forwarded-For": "100.64.0.21"}); w.Code != 200 {
		t.Errorf("other client = %d", w.Code)
	}
}

func TestLimiterExpiresAndGlobalCap(t *testing.T) {
	l := newLimiter()
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < maxFails; i++ {
		l.fail("a")
	}
	if l.blocked("a") <= 0 {
		t.Fatal("a not blocked")
	}
	now = now.Add(blockFor + time.Second)
	if l.blocked("a") > 0 {
		t.Error("block did not expire")
	}
	for i := 0; i < maxTotalFails; i++ {
		l.fail("rotating-" + string(rune('a'+i%26)) + string(rune('a'+i/26)))
	}
	if l.blocked("fresh-client") <= 0 {
		t.Error("global cap did not block everyone")
	}
}
