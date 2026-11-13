package api

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Caller is who sent a request, as the agent's own Tailscale node saw it
// (#57): the login of the account that device belongs to, and the device's
// address inside the tailnet. The node asks Tailscale itself (WhoIs), so
// nothing the sender writes into the request can change it.
type Caller struct {
	Login string
	IP    string
}

type callerKey struct{}

// WithCaller returns r carrying who sent it. Only the listener that knows
// calls this: the one on the Tailscale node. A request without a caller is
// refused everywhere, so serving Handler on any other socket lets nobody in.
func WithCaller(r *http.Request, c Caller) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), callerKey{}, c))
}

func callerOf(r *http.Request) Caller {
	c, _ := r.Context().Value(callerKey{}).(Caller)
	c.Login = strings.TrimSpace(c.Login)
	c.IP = strings.TrimSpace(c.IP)
	return c
}

var (
	cgnat = netip.MustParsePrefix("100.64.0.0/10")
	tsULA = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

// isTailnetIP reports whether s is a Tailscale address.
func isTailnetIP(s string) bool {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return false
	}
	a = a.Unmap()
	return cgnat.Contains(a) || tsULA.Contains(a)
}

// checkCaller is what every request must pass before anything else is
// looked at: it came from a device of the PC owner's own Tailscale account.
// It writes the refusal and returns false otherwise.
func (s *Server) checkCaller(w http.ResponseWriter, r *http.Request) (Caller, bool) {
	c := callerOf(r)
	if c.Login == "" || !isTailnetIP(c.IP) {
		writeError(w, http.StatusForbidden, CodeNotTailnet, "requests must come through the owner's Tailscale network")
		return c, false
	}
	owner := ""
	if s.o.Owner != nil {
		owner = strings.TrimSpace(s.o.Owner())
	}
	if owner == "" {
		writeError(w, http.StatusServiceUnavailable, CodeOwnerUnknown, "the PC is not signed in to Tailscale yet")
		return c, false
	}
	if !strings.EqualFold(c.Login, owner) {
		writeError(w, http.StatusForbidden, CodeWrongAccount, "tailscale account is not the PC owner")
		return c, false
	}
	return c, true
}

// clientKey groups requests for the failure limit: the sender's address in
// the tailnet, or the socket's when there is none.
func clientKey(r *http.Request) string {
	if c := callerOf(r); c.IP != "" {
		return c.IP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
