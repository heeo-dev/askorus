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

// PairStarted is the 202 answer: the PC is asking the person. The phone
// then calls POST /v1/pair/status with the request id until it is final.
type PairStarted struct {
	RequestID   string `json:"requestId"`
	Status      string `json:"status"` // "pending"
	PollAfterMs int    `json:"pollAfterMs"`
}

// PairStatusRequest is the body of POST /v1/pair/status. The id goes in
// the body, not the path, so it never lands in a request log.
type PairStatusRequest struct {
	RequestID string `json:"requestId"`
}

// PairStatus is the answer to POST /v1/pair/status. Token and device are
// set once, on the first answer after the PC allowed it.
type PairStatus struct {
	Status      string `json:"status"` // pending | approved
	PollAfterMs int    `json:"pollAfterMs,omitempty"`
	Token       string `json:"token,omitempty"`
	DeviceID    string `json:"deviceId,omitempty"`
	DeviceName  string `json:"deviceName,omitempty"`
	APIVersion  int    `json:"apiVersion"`
}

const pollAfterMs = 1000

// pairPreflight applies the checks every pairing call needs: the request
// came through tailscale serve from a tailnet address, from the PC owner's
// account, and the client is not locked out.
func (s *Server) pairPreflight(w http.ResponseWriter, r *http.Request) (pairing.Origin, bool) {
	client := clientKey(r)
	if s.refuseIfLimited(w, client) {
		return pairing.Origin{}, false
	}
	if s.o.Pairer == nil {
		writeError(w, http.StatusNotFound, CodePairClosed, "pairing is not available")
		return pairing.Origin{}, false
	}
	login := strings.TrimSpace(r.Header.Get("Tailscale-User-Login"))
	ip := forwardedIP(r)
	if login == "" || !tailnet.IsTailnetIP(ip) {
		writeError(w, http.StatusForbidden, CodeNotTailnet, "pairing must come through Tailscale")
		return pairing.Origin{}, false
	}
	if s.o.TailscaleOwner == "" {
		writeError(w, http.StatusServiceUnavailable, CodeOwnerUnknown, "the PC owner's Tailscale account is not known")
		return pairing.Origin{}, false
	}
	if !strings.EqualFold(login, s.o.TailscaleOwner) {
		writeError(w, http.StatusForbidden, CodeWrongAccount, "tailscale account is not the PC owner")
		return pairing.Origin{}, false
	}
	return pairing.Origin{Login: login, RemoteIP: ip}, true
}

// pair starts the handshake and returns at once.
func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	origin, ok := s.pairPreflight(w, r)
	if !ok {
		return
	}
	var req PairRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "body must be {code, deviceName}")
		return
	}
	id, err := s.o.Pairer.Start(req.Code, req.DeviceName, origin)
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, PairStarted{RequestID: id, Status: string(pairing.StatePending), PollAfterMs: pollAfterMs})
	case errors.Is(err, pairing.ErrBadCode):
		s.limiter.fail(clientKey(r))
		writeError(w, http.StatusForbidden, CodePairBadCode, "wrong pairing code")
	case errors.Is(err, pairing.ErrNoWindow):
		writeError(w, http.StatusNotFound, CodePairClosed, "no pairing window is open on the PC")
	case errors.Is(err, pairing.ErrBusy):
		writeError(w, http.StatusConflict, CodePairBusy, "another device is pairing")
	case errors.Is(err, pairing.ErrBadName):
		writeError(w, http.StatusBadRequest, CodeBadRequest, err.Error())
	default:
		s.pairFailed(w, err)
	}
}

// pairStatus reports a started handshake.
func (s *Server) pairStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.pairPreflight(w, r); !ok {
		return
	}
	var req PairStatusRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "body must be {requestId}")
		return
	}
	st, token, d, err := s.o.Pairer.Status(req.RequestID)
	if errors.Is(err, pairing.ErrUnknownRequest) {
		s.limiter.fail(clientKey(r))
		writeError(w, http.StatusNotFound, CodePairUnknown, "no such pairing request (finished, fetched or expired)")
		return
	}
	if err != nil {
		s.pairFailed(w, err)
		return
	}
	switch st {
	case pairing.StatePending:
		writeJSON(w, http.StatusOK, PairStatus{Status: string(st), PollAfterMs: pollAfterMs, APIVersion: APIVersion})
	case pairing.StateApproved:
		if info, _ := r.Context().Value(reqInfoKey{}).(*reqInfo); info != nil {
			info.device = d.ID
		}
		writeJSON(w, http.StatusOK, PairStatus{Status: string(st), Token: token, DeviceID: d.ID, DeviceName: d.Name, APIVersion: APIVersion})
	case pairing.StateDenied:
		writeError(w, http.StatusForbidden, CodePairDenied, "pairing was refused on the PC")
	case pairing.StateTimeout:
		writeError(w, http.StatusRequestTimeout, CodePairTimeout, "nobody answered on the PC")
	default:
		s.pairFailed(w, errors.New(string(st)))
	}
}

func (s *Server) pairFailed(w http.ResponseWriter, err error) {
	if s.o.Logger != nil {
		s.o.Logger.Error("pairing failed", "err", err.Error())
	}
	writeError(w, http.StatusInternalServerError, CodeInternal, "pairing failed")
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
