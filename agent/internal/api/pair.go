package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/pairing"
)

// PairRequest is the body of POST /v1/pair. With a code it is the phone
// that scanned the QR code of an open pairing window. Without one (#58)
// it is a phone that found this PC in the person's own Tailscale account:
// Match is then the two digits the phone shows, and the PC shows them too
// wherever it asks or reports.
type PairRequest struct {
	Code       string `json:"code,omitempty"`
	DeviceName string `json:"deviceName"`
	Match      string `json:"match,omitempty"`
}

// HelloResponse is GET /v1/hello: what a device of the PC owner's own
// Tailscale account may know before it is paired (#58), so the phone can
// list this PC by its name. Nothing here is more than the account's own
// device list already says.
type HelloResponse struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	APIVersion int    `json:"apiVersion"`
	Hostname   string `json:"hostname"`
	OS         string `json:"os"`
	// Pairing is how a request without a code is answered right now:
	// "window" (a pairing window is open on the PC, so it is taken at
	// once) or "ask" (the PC's screen or a paired phone is asked).
	Pairing string `json:"pairing"`
}

// PairQuestion is a request without a code as a paired phone sees it.
type PairQuestion struct {
	ID         string    `json:"id"`
	DeviceName string    `json:"deviceName"`
	Match      string    `json:"match"`
	At         time.Time `json:"at"`
}

// PairQuestions is GET /v1/pair/requests.
type PairQuestions struct {
	Requests []PairQuestion `json:"requests"`
}

// PairAnswer is the body of POST /v1/pair/requests/answer.
type PairAnswer struct {
	ID    string `json:"id"`
	Allow bool   `json:"allow"`
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

// pairPreflight applies the checks every pairing call needs: the client is
// not locked out, and the request came from a device of the PC owner's own
// Tailscale account (checkCaller).
func (s *Server) pairPreflight(w http.ResponseWriter, r *http.Request) (pairing.Origin, bool) {
	client := clientKey(r)
	if s.refuseIfLimited(w, client) {
		return pairing.Origin{}, false
	}
	if s.o.Pairer == nil {
		writeError(w, http.StatusNotFound, CodePairClosed, "pairing is not available")
		return pairing.Origin{}, false
	}
	c, ok := s.checkCaller(w, r)
	if !ok {
		return pairing.Origin{}, false
	}
	return pairing.Origin{Login: c.Login, RemoteIP: c.IP}, true
}

// pair starts the handshake and returns at once.
func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	origin, ok := s.pairPreflight(w, r)
	if !ok {
		return
	}
	var req PairRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "body must be {code, deviceName} or {deviceName, match}")
		return
	}
	var id string
	var err error
	if req.Code == "" {
		// No code: the phone found this PC in the account's device list.
		id, err = s.o.Pairer.StartDirect(req.DeviceName, req.Match, origin)
	} else {
		id, err = s.o.Pairer.Start(req.Code, req.DeviceName, origin)
	}
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
	case errors.Is(err, pairing.ErrBadName), errors.Is(err, pairing.ErrBadMatch):
		writeError(w, http.StatusBadRequest, CodeBadRequest, err.Error())
	case errors.Is(err, pairing.ErrNoApprover):
		writeError(w, http.StatusNotFound, CodePairClosed, "nobody can allow it on the PC right now: run `askorus-agent pair` there, then ask again")
	default:
		s.pairFailed(w, err)
	}
}

// hello tells a device of the owner's own Tailscale account that an
// Askorus agent answers here, and under which name.
func (s *Server) hello(w http.ResponseWriter, r *http.Request) {
	if s.refuseIfLimited(w, clientKey(r)) {
		return
	}
	if _, ok := s.checkCaller(w, r); !ok {
		return
	}
	resp := HelloResponse{Name: "askorus-agent", Version: s.o.Version, APIVersion: APIVersion, Hostname: s.o.Hostname, OS: runtime.GOOS, Pairing: "ask"}
	if s.o.Pairer != nil && s.o.Pairer.WindowIsOpen() {
		resp.Pairing = "window"
	}
	writeJSON(w, http.StatusOK, resp)
}

// pairRequests lists the request without a code that waits, for a paired
// phone to answer when nobody is at the PC.
func (s *Server) pairRequests(w http.ResponseWriter, r *http.Request) {
	resp := PairQuestions{Requests: []PairQuestion{}}
	if s.o.Pairer != nil {
		for _, q := range s.o.Pairer.Pending() {
			resp.Requests = append(resp.Requests, PairQuestion{ID: q.ID, DeviceName: q.DeviceName, Match: q.Match, At: q.At})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// pairAnswer is a paired phone's answer to the waiting request.
func (s *Server) pairAnswer(w http.ResponseWriter, r *http.Request) {
	var req PairAnswer
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.ID == "" {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "body must be {id, allow}")
		return
	}
	if s.o.Pairer == nil || s.o.Pairer.Answer(req.ID, req.Allow) != nil {
		writeError(w, http.StatusNotFound, CodePairUnknown, "no such pairing request (answered already, or its time is up)")
		return
	}
	if s.o.Logger != nil {
		device := ""
		if info, _ := r.Context().Value(reqInfoKey{}).(*reqInfo); info != nil {
			device = info.device
		}
		s.o.Logger.Info("pairing answered by a paired device", "device", device, "allow", req.Allow)
	}
	w.WriteHeader(http.StatusNoContent)
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
		// A device that keeps asking and being refused is shut out for a while,
		// so it cannot fill the PC's screen with questions.
		s.limiter.fail(clientKey(r))
		writeError(w, http.StatusForbidden, CodePairDenied, "pairing was refused")
	case pairing.StateTimeout:
		s.limiter.fail(clientKey(r))
		writeError(w, http.StatusRequestTimeout, CodePairTimeout, "nobody answered on the PC")
	case pairing.StateUnattended:
		writeError(w, http.StatusNotFound, CodePairClosed, "nobody can allow it on the PC right now: run `askorus-agent pair` there, then ask again")
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
// refused, which also covers senders that come from many addresses.
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
