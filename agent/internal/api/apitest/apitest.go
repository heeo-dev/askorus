// Package apitest lets tests in other packages drive the API over a plain
// loopback socket (httptest). In the agent the API is served only on its
// Tailscale node, which says who sent each request; here a stand-in says
// every request is from a device of the owner's account. Tests only.
package apitest

import (
	"net/http"

	"github.com/heeo-dev/askorus/agent/internal/api"
)

const (
	// Owner is the account the stand-in node is signed in as.
	Owner = "owner@example.com"
	// CallerIP is the tailnet address the stand-in gives every sender.
	CallerIP = "100.64.0.9"
)

// LoginHeader lets a test say a request comes from another account. Only
// the stand-in reads it.
const LoginHeader = "Test-Caller-Login"

// Node fills in what the node supplies: the owner, and the names the tests'
// requests are sent to.
func Node(o api.Options) api.Options {
	o.Hosts = func() []string { return []string{"127.0.0.1", "localhost", "askorus-pc.tail.ts.net"} }
	o.Owner = func() string { return Owner }
	return o
}

// AsOwner wraps the server's handler as the node's listener would.
func AsOwner(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := api.Caller{Login: Owner, IP: CallerIP}
		if v := r.Header.Get(LoginHeader); v != "" {
			c.Login = v
		}
		h.ServeHTTP(w, api.WithCaller(r, c))
	})
}
