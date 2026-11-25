package api

import (
	"strings"
	"testing"
)

// #75 (2026-11-25): a phone looks for waiting pairing requests every 5 s,
// and those looks filled most of the log (3.9 MB in three days). A look
// that finds nothing is counted, one line per quietEvery; other requests
// are logged as before.
func TestQuietLooksAreCountedNotLogged(t *testing.T) {
	e := newEnv(t, &askAdapter{name: "claude", available: true}, nil)
	for range 3 {
		if w := e.do("GET", "/v1/pair/requests", nil); w.Code != 200 {
			t.Fatalf("pair requests = %d %s", w.Code, w.Body)
		}
	}
	e.do("GET", "/v1/info", nil)
	logs := e.logs.String()
	if strings.Contains(logs, "/v1/pair/requests") {
		t.Errorf("an empty pairing look was logged:\n%s", logs)
	}
	if !strings.Contains(logs, `"path":"/v1/info"`) {
		t.Errorf("other requests are no longer logged:\n%s", logs)
	}
	for range quietEvery - 3 {
		e.do("GET", "/v1/pair/requests", nil)
	}
	if n := strings.Count(e.logs.String(), `"msg":"quiet requests"`); n != 1 {
		t.Errorf("%d count lines after %d quiet looks, want 1", n, quietEvery)
	}
}
