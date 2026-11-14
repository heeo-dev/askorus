package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/devices"
	"github.com/heeo-dev/askorus/agent/internal/logx"
	"github.com/heeo-dev/askorus/agent/internal/pairing"
)

// Pairing without a code (#58): a phone of the owner's own Tailscale account
// finds the PC in the account's device list and asks; the person consents by
// an open pairing window, on the PC's screen, or from a paired phone.

type directEnv struct {
	pairEnv
	store  *devices.Store
	pairer *pairing.Pairer
}

func newDirectEnv(t *testing.T) directEnv {
	t.Helper()
	dir := t.TempDir()
	store, err := devices.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := &pairing.Pairer{Dir: dir, Devices: store, Poll: 10 * time.Millisecond, Timeout: 2 * time.Second}
	h := New(Options{
		Adapters: []core.Adapter{fakeAdapter{}}, Devices: store, Pairer: p,
		Logger: logx.Discard(), Hostname: "DESKTOP-TEST", Version: "9.9.9",
		Hosts: func() []string { return testHosts },
		Owner: func() string { return testOwner },
	}).Handler()
	return directEnv{pairEnv: pairEnv{h: viaNode(h), dir: dir}, store: store, pairer: p}
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

// poll plays the asking phone: it asks for the status until it is final.
func (e directEnv) poll(t *testing.T, requestID string) *PairStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w := e.serve(viaServe("POST", "/v1/pair/status", `{"requestId":"`+requestID+`"}`, nil))
		if w.Code != 200 {
			t.Fatalf("status: %d %s", w.Code, w.Body)
		}
		var st PairStatus
		if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
			t.Fatal(err)
		}
		if st.Status != string(pairing.StatePending) {
			return &st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("still pending")
	return nil
}

func (e directEnv) ask(t *testing.T, name, match string) string {
	t.Helper()
	w := e.serve(viaServe("POST", "/v1/pair", `{"deviceName":"`+name+`","match":"`+match+`"}`, nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("pair without a code: %d %s", w.Code, w.Body)
	}
	var st PairStarted
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil || st.RequestID == "" {
		t.Fatalf("answer %s", w.Body)
	}
	return st.RequestID
}

// hello lets a device of the owner's account list the PC before it is
// paired, and nobody else: no token is needed, but who sent it is checked
// as for every other call.
func TestHello(t *testing.T) {
	e := newDirectEnv(t)
	w := e.serve(viaServe("GET", "/v1/hello", "", nil))
	var h HelloResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &h) != nil {
		t.Fatalf("hello: %d %s", w.Code, w.Body)
	}
	if h.Name != "askorus-agent" || h.Hostname != "DESKTOP-TEST" || h.Version != "9.9.9" || h.OS == "" || h.APIVersion != APIVersion || h.Pairing != "ask" {
		t.Errorf("hello = %+v", h)
	}
	if _, _, err := pairing.Open(e.dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	w = e.serve(viaServe("GET", "/v1/hello", "", nil))
	if json.Unmarshal(w.Body.Bytes(), &h); h.Pairing != "window" {
		t.Errorf("with a pairing window open: %+v", h)
	}
	for name, hdr := range map[string]map[string]string{
		"another account": {hdrCallerLogin: "kid@example.com"},
		"no caller":       {hdrNoCaller: "1"},
		"a browser page":  {"Origin": "https://evil.example"},
	} {
		if w := e.serve(viaServe("GET", "/v1/hello", "", hdr)); w.Code != 403 {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	if w := e.serve(viaServe("POST", "/v1/hello", "", nil)); w.Code != 405 {
		t.Errorf("POST: %d", w.Code)
	}
}

// A pairing window that is open on the PC takes the request at once, and
// the pair command learns who came in, with the two digits.
func TestPairDirectIntoAnOpenWindow(t *testing.T) {
	e := newDirectEnv(t)
	_, win, err := pairing.Open(e.dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	st := e.poll(t, e.ask(t, "Galaxy S23", "47"))
	if st.Status != string(pairing.StateApproved) || st.Token == "" || st.DeviceName != "Galaxy S23" {
		t.Fatalf("status = %+v", st)
	}
	// The token works.
	if w := e.serve(viaServe("GET", "/v1/info", "", bearer(st.Token))); w.Code != 200 {
		t.Errorf("info with the new token: %d %s", w.Code, w.Body)
	}
	if res, ok := pairing.ResultFor(e.dir, win.ID); !ok || !res.OK || res.Match != "47" {
		t.Errorf("result = %+v %v", res, ok)
	}
}

// Nobody at the PC: a paired phone sees the request (not the id that
// fetches the token) and allows it. A phone that is not paired sees and
// answers nothing.
func TestPairDirectAnsweredByAPairedPhone(t *testing.T) {
	e := newDirectEnv(t)
	oldToken, _, err := e.store.Add("Old phone")
	if err != nil {
		t.Fatal(err)
	}
	// The agent says that it can do this.
	var info InfoResponse
	if w := e.serve(viaServe("GET", "/v1/info", "", bearer(oldToken))); json.Unmarshal(w.Body.Bytes(), &info) != nil || info.Pairing == nil || !info.Pairing.Approve {
		t.Fatalf("info.pairing = %+v (%s)", info.Pairing, w.Body)
	}
	if w := e.serve(viaServe("GET", "/v1/pair/requests", "", bearer(oldToken))); w.Code != 200 || !strings.Contains(w.Body.String(), `"requests":[]`) {
		t.Errorf("no request yet: %d %s", w.Code, w.Body)
	}

	requestID := e.ask(t, "New phone", "08")
	w := e.serve(viaServe("GET", "/v1/pair/requests", "", bearer(oldToken)))
	var list PairQuestions
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Requests) != 1 {
		t.Fatalf("requests: %d %s", w.Code, w.Body)
	}
	q := list.Requests[0]
	if q.DeviceName != "New phone" || q.Match != "08" || q.ID == "" || q.ID == requestID || strings.Contains(w.Body.String(), requestID) {
		t.Errorf("request = %+v (the asking phone's id %s)", q, requestID)
	}
	// Without a token: nothing to see, nothing to answer.
	if w := e.serve(viaServe("GET", "/v1/pair/requests", "", nil)); w.Code != 401 {
		t.Errorf("list without a token: %d", w.Code)
	}
	if w := e.serve(viaServe("POST", "/v1/pair/requests/answer", `{"id":"`+q.ID+`","allow":true}`, nil)); w.Code != 401 {
		t.Errorf("answer without a token: %d", w.Code)
	}
	if w := e.serve(viaServe("POST", "/v1/pair/requests/answer", `{"id":"nope","allow":true}`, bearer(oldToken))); w.Code != 404 || errCode(t, w) != CodePairUnknown {
		t.Errorf("answer to another id: %d %s", w.Code, w.Body)
	}
	if w := e.serve(viaServe("POST", "/v1/pair/requests/answer", `{"id":"`+q.ID+`","allow":true}`, bearer(oldToken))); w.Code != 204 {
		t.Fatalf("answer: %d %s", w.Code, w.Body)
	}
	st := e.poll(t, requestID)
	if st.Status != string(pairing.StateApproved) || st.Token == "" || st.Token == oldToken {
		t.Fatalf("status = %+v", st)
	}
	if w := e.serve(viaServe("GET", "/v1/pair/requests", "", bearer(st.Token))); w.Code != 200 || !strings.Contains(w.Body.String(), `"requests":[]`) {
		t.Errorf("after the answer: %d %s", w.Code, w.Body)
	}
}

// A refusal on the PC's screen: the phone learns it and no device is added.
func TestPairDirectRefused(t *testing.T) {
	e := newDirectEnv(t)
	e.pairer.Confirm = func(context.Context, pairing.Question) (bool, bool) { return false, true }
	body := `{"requestId":"` + e.ask(t, "Galaxy S23", "47") + `"}`
	var w *httptest.ResponseRecorder
	for i := 0; i < 300; i++ {
		w = e.serve(viaServe("POST", "/v1/pair/status", body, nil))
		if w.Code != 200 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if w.Code != 403 || errCode(t, w) != CodePairDenied {
		t.Errorf("refused: %d %s", w.Code, w.Body)
	}
	if list, _ := e.store.List(); len(list) != 0 {
		t.Errorf("%d devices after a refusal", len(list))
	}
}

// With no window, no screen and no paired phone nobody could answer: the
// phone is told to open a window on the PC, at once.
func TestPairDirectWithNobodyToAnswer(t *testing.T) {
	e := newDirectEnv(t)
	w := e.serve(viaServe("POST", "/v1/pair", `{"deviceName":"Galaxy S23","match":"47"}`, nil))
	if w.Code != 404 || errCode(t, w) != CodePairClosed || !strings.Contains(w.Body.String(), "askorus-agent pair") {
		t.Errorf("nobody to answer: %d %s", w.Code, w.Body)
	}
	for _, body := range []string{`{"deviceName":"Galaxy S23"}`, `{"deviceName":"Galaxy S23","match":"4x"}`, `{"deviceName":"","match":"47"}`} {
		if w := e.serve(viaServe("POST", "/v1/pair", body, nil)); w.Code != 400 {
			t.Errorf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	// Who sent it is checked before anything else, as with a code.
	if w := e.serve(viaServe("POST", "/v1/pair", `{"deviceName":"x","match":"47"}`, map[string]string{hdrCallerLogin: "kid@example.com"})); w.Code != 403 || errCode(t, w) != CodeWrongAccount {
		t.Errorf("another account: %d %s", w.Code, w.Body)
	}
}
