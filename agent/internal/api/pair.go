package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/asksemble/asksemble/agent/internal/pairing"
	"github.com/asksemble/asksemble/agent/internal/tailnet"
)

// PairRequest is the body of POST /v1/pair.
type PairRequest struct {
	Code       string `json:"code"`
	DeviceName string `json:"deviceName"`
}

// PairResponse hands the device token to the phone, once.
type PairResponse struct {
	Token      string `json:"token"`
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	APIVersion int    `json:"apiVersion"`
}

// pair runs the pairing handshake. The request must come through tailscale
// serve from the PC owner's account, then the person at the PC must allow it.
func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	client := clientKey(r)
	if s.refuseIfLimited(w, client) {
		return
	}
	if s.o.Pairer == nil {
		writeError(w, http.StatusNotFound, CodePairClosed, "pairing is not available")
		return
	}
	login := strings.TrimSpace(r.Header.Get("Tailscale-User-Login"))
	ip := forwardedIP(r)
	if login == "" || !tailnet.IsTailnetIP(ip) {
		writeError(w, http.StatusForbidden, CodeNotTailnet, "pairing must come through Tailscale")
		return
	}
	if s.o.TailscaleOwner == "" {
		writeError(w, http.StatusServiceUnavailable, CodeOwnerUnknown, "the PC owner's Tailscale account is not known")
		return
	}
	if !strings.EqualFold(login, s.o.TailscaleOwner) {
		writeError(w, http.StatusForbidden, CodeWrongAccount, "tailscale account is not the PC owner")
		return
	}
	var req PairRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "body must be {code, deviceName}")
		return
	}
	// The phone waits while the person at the PC decides.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(pairing.DecisionTimeout + 30*time.Second))

	token, d, err := s.o.Pairer.Pair(r.Context(), req.Code, req.DeviceName, pairing.Origin{Login: login, RemoteIP: ip})
	switch {
	case err == nil:
		if info, _ := r.Context().Value(reqInfoKey{}).(*reqInfo); info != nil {
			info.device = d.ID
		}
		writeJSON(w, http.StatusOK, PairResponse{Token: token, DeviceID: d.ID, DeviceName: d.Name, APIVersion: APIVersion})
	case errors.Is(err, pairing.ErrBadCode):
		s.limiter.fail(client)
		writeError(w, http.StatusForbidden, CodePairBadCode, "wrong pairing code")
	case errors.Is(err, pairing.ErrNoWindow):
		writeError(w, http.StatusNotFound, CodePairClosed, "no pairing window is open on the PC")
	case errors.Is(err, pairing.ErrBusy):
		writeError(w, http.StatusConflict, CodePairBusy, "another device is pairing")
	case errors.Is(err, pairing.ErrDenied):
		writeError(w, http.StatusForbidden, CodePairDenied, "pairing was refused on the PC")
	case errors.Is(err, pairing.ErrTimeout):
		writeError(w, http.StatusRequestTimeout, CodePairTimeout, "nobody answered on the PC")
	case errors.Is(err, pairing.ErrBadName):
		writeError(w, http.StatusBadRequest, CodeBadRequest, err.Error())
	default:
		if s.o.Logger != nil {
			s.o.Logger.Error("pairing failed", "err", err.Error())
		}
		writeError(w, http.StatusInternalServerError, CodeInternal, "pairing failed")
	}
}

// forwardedIP is the phone address tailscale serve puts in
// X-Forwarded-For. Only the last entry is used: it is the one serve added.
func forwardedIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if i := strings.LastIndex(xff, ","); i >= 0 {
		xff = xff[i+1:]
	}
	return strings.TrimSpace(xff)
}

// clientKey groups requests for the failure limit.
func clientKey(r *http.Request) string {
	if ip := forwardedIP(r); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) refuseIfLimited(w http.ResponseWriter, client string) bool {
	wait := s.limiter.blocked(client)
	if wait <= 0 {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
	writeError(w, http.StatusTooManyRequests, CodeTooMany, "too many failed attempts, try again later")
	return true
}

// Failure limits. Past maxFails failures inside failWindow a client is
// refused for blockFor. Past maxTotalFails from everyone, all clients are
// refused, which also covers a local program that changes its forwarded
// address on every try.
const (
	maxFails      = 10
	maxTotalFails = 100
	failWindow    = 10 * time.Minute
	blockFor      = 10 * time.Minute
)

type limiter struct {
	mu    sync.Mutex
	now   func() time.Time
	fails map[string][]time.Time
	all   []time.Time
	until map[string]time.Time // "" blocks everyone
}

func newLimiter() *limiter {
	return &limiter{now: time.Now, fails: map[string][]time.Time{}, until: map[string]time.Time{}}
}

func (l *limiter) blocked(client string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for _, k := range []string{"", client} {
		if t, ok := l.until[k]; ok {
			if now.Before(t) {
				return t.Sub(now)
			}
			delete(l.until, k)
		}
	}
	return 0
}

func (l *limiter) fail(client string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	keep := func(ts []time.Time) []time.Time {
		out := ts[:0]
		for _, t := range ts {
			if now.Sub(t) < failWindow {
				out = append(out, t)
			}
		}
		return out
	}
	l.fails[client] = append(keep(l.fails[client]), now)
	l.all = append(keep(l.all), now)
	if len(l.fails[client]) >= maxFails {
		l.until[client] = now.Add(blockFor)
		delete(l.fails, client)
	}
	if len(l.all) >= maxTotalFails {
		l.until[""] = now.Add(blockFor)
		l.all = nil
	}
	if len(l.fails) > 10000 { // bound memory
		l.fails = map[string][]time.Time{}
	}
}
