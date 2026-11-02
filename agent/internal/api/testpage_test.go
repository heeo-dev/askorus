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

// S5c: the test page carries the brand lightly (mark B, the wordmark and
// apricot buttons, light and dark) under the same CSP, which has no
// 'unsafe-inline' style and no images: so no style attribute, no style
// element and no img. Its not-official line names no company.
func TestTestPageBrand(t *testing.T) {
	e := newEnv(t, fakeAdapter{}, func(o *Options) { o.TestPage = true })
	const csp = "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; form-action 'none'; frame-ancestors 'none'; base-uri 'none'"
	page := e.do("GET", "/test/", map[string]string{"Authorization": ""})
	css := e.do("GET", "/test/style.css", map[string]string{"Authorization": ""})
	if page.Code != 200 || css.Code != 200 {
		t.Fatalf("page %d, style %d", page.Code, css.Code)
	}
	for _, w := range []string{page.Header().Get("Content-Security-Policy"), css.Header().Get("Content-Security-Policy")} {
		if w != csp {
			t.Errorf("CSP = %q, want %q", w, csp)
		}
	}
	html := page.Body.String()
	for _, bad := range []string{"style=", "<style", "<img", "Anthropic의 공식", "Anthropic"} {
		if strings.Contains(html, bad) {
			t.Errorf("index.html holds %q", bad)
		}
	}
	for _, want := range []string{
		"AI 회사의 공식 제품이 아닙니다.",
		`<svg class="mark" width="32" height="32" viewBox="0 0 100 100" aria-hidden="true"`,
		`<path d="M28 59.5L27 73L40 65.5Z" fill="#2A1408"/>`, // the bubble's tail: icon B
		`<span class="ask">Ask</span><span class="semble">semble</span>`,
		`aria-label="Asksemble 시험 페이지"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html lacks %q", want)
		}
	}
	style := css.Body.String()
	for _, want := range []string{"#FFAD85", "#2A1408", "#DD5A26", "#151618", "#FCFAF8", "@media (prefers-color-scheme: dark)", "font-weight: 800; letter-spacing: -0.025em", "border-radius: 22px"} {
		if !strings.Contains(style, want) {
			t.Errorf("style.css lacks %q", want)
		}
	}
}
