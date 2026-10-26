package api

import (
	"embed"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
)

// The test page lets a phone browser try pairing and the session list
// before the app exists. It is off unless the server runs with -test-page.
//
//go:embed testpage/*
var testPageFiles embed.FS

func (s *Server) testPage() http.Handler {
	sub, _ := fs.Sub(testPageFiles, "testpage")
	files := http.StripPrefix("/test/", http.FileServer(http.FS(sub)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; form-action 'none'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		files.ServeHTTP(w, r)
	})
}

// sameOrigin reports whether a browser request comes from a page served by
// this host, which is only true for the test page.
func (s *Server) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.Path != "" {
		return false
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host) && s.hosts[strings.ToLower(u.Hostname())]
}
