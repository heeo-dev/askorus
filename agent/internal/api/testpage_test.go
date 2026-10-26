package api

import (
	"strings"
	"testing"
)

func TestTestPageOffByDefault(t *testing.T) {
	e := newEnv(t, fakeAdapter{}, nil)
	if w := e.do("GET", "/test/", map[string]string{"Authorization": ""}); w.Code != 401 {
		t.Errorf("test page reachable while off: %d", w.Code)
	}
	same := map[string]string{"Host": "pc.tail.ts.net:8790", "Origin": "https://pc.tail.ts.net:8790"}
	if w := e.do("GET", "/v1/info", same); w.Code != 403 {
		t.Errorf("same-origin call accepted while the test page is off: %d", w.Code)
	}
}

func TestTestPage(t *testing.T) {
	e := newEnv(t, fakeAdapter{}, func(o *Options) { o.TestPage = true })
	for _, path := range []string{"/test/", "/test/app.js", "/test/style.css"} {
		w := e.do("GET", path, map[string]string{"Authorization": ""})
		if w.Code != 200 {
			t.Errorf("%s = %d", path, w.Code)
			continue
		}
		if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
			t.Errorf("%s CSP = %q", path, csp)
		}
	}
	if w := e.do("GET", "/test", map[string]string{"Authorization": ""}); w.Code != 302 {
		t.Errorf("/test = %d", w.Code)
	}

	cases := []struct {
		host, origin string
		ok           bool
	}{
		{"pc.tail.ts.net:8790", "https://pc.tail.ts.net:8790", true},
		{"PC.tail.ts.net:8790", "https://pc.tail.ts.net:8790", true},
		{"127.0.0.1:8790", "http://127.0.0.1:8790", true},
		{"pc.tail.ts.net:8790", "https://evil.example", false},
		{"pc.tail.ts.net:8790", "https://pc.tail.ts.net:8443", false},
		{"pc.tail.ts.net:8790", "https://pc.tail.ts.net:8790/x", false},
		{"pc.tail.ts.net:8790", "null", false},
		{"pc.tail.ts.net:8790", "ftp://pc.tail.ts.net:8790", false},
	}
	for _, c := range cases {
		w := e.do("GET", "/v1/info", map[string]string{"Host": c.host, "Origin": c.origin})
		if got := w.Code == 200; got != c.ok {
			t.Errorf("host %s origin %s: %d", c.host, c.origin, w.Code)
		}
	}
	if w := e.do("OPTIONS", "/v1/info", map[string]string{"Host": "pc.tail.ts.net:8790", "Origin": "https://pc.tail.ts.net:8790"}); w.Code != 403 {
		t.Errorf("preflight = %d", w.Code)
	}
}
