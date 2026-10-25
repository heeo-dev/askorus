// Package api is the agent's HTTP API. It listens on 127.0.0.1 only and is
// reached through tailscale serve. It knows adapters only through
// core.Adapter (principle 2).
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/devices"
	"github.com/asksemble/asksemble/agent/internal/pairing"
)

// APIVersion is the major API version in the URL path.
const APIVersion = 1

// Error codes. The app turns them into text (N9).
const (
	CodeBadHost        = "bad_host"
	CodeCrossOrigin    = "cross_origin"
	CodeUnauthorized   = "unauthorized"
	CodeWrongAccount   = "wrong_account"
	CodeNotFound       = "not_found"
	CodeNotImplemented = "not_implemented"
	CodeMethod         = "method_not_allowed"
	CodeBadRequest     = "bad_request"
	CodeInternal       = "internal"
	CodeTooMany        = "too_many_attempts"
	CodeNotTailnet     = "pair_not_tailnet"
	CodeOwnerUnknown   = "owner_unknown"
	CodePairClosed     = "pair_closed"
	CodePairBadCode    = "pair_bad_code"
	CodePairBusy       = "pair_busy"
	CodePairDenied     = "pair_denied"
	CodePairTimeout    = "pair_timeout"
)

// TokenVerifier checks device tokens.
type TokenVerifier interface {
	Verify(token string) (devices.Device, bool)
}

// Pairer joins a new device (see package pairing).
type Pairer interface {
	Pair(ctx context.Context, code, deviceName string, o pairing.Origin) (string, devices.Device, error)
}

// Options configures the server.
type Options struct {
	Version    string
	Hostname   string
	Adapters   []core.Adapter
	Devices    TokenVerifier
	Pairer     Pairer // nil: POST /v1/pair is refused
	Logger     *slog.Logger
	ServeHosts []string // extra accepted Host names
	// TailscaleOwner is the PC owner's Tailscale login. Requests naming
	// another login are refused, and pairing needs it to be known.
	TailscaleOwner string
	HiddenFolders  []string
}

// Server serves /v1.
type Server struct {
	o       Options
	hosts   map[string]bool
	limiter *limiter
}

// New builds a server.
func New(o Options) *Server {
	s := &Server{
		o:       o,
		hosts:   map[string]bool{"127.0.0.1": true, "localhost": true, "::1": true},
		limiter: newLimiter(),
	}
	for _, h := range o.ServeHosts {
		s.hosts[strings.ToLower(h)] = true
	}
	return s
}

// Handler returns the full middleware chain.
func (s *Server) Handler() http.Handler {
	top := http.NewServeMux()
	// Pairing is the only route without a device token.
	top.HandleFunc("POST /v1/pair", s.pair)
	top.HandleFunc("/v1/pair", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, CodeMethod, "method not allowed")
	})
	mux := http.NewServeMux()
	top.Handle("/", s.authenticate(mux))
	route := func(method, path string, h http.HandlerFunc) {
		mux.HandleFunc(method+" "+path, h)
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusMethodNotAllowed, CodeMethod, "method not allowed")
		})
	}
	route("GET", "/v1/info", s.info)
	route("GET", "/v1/sessions", s.sessions)
	// Reserved for the realtime channel (principle 5).
	mux.HandleFunc("/v1/stream", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, CodeNotImplemented, "not implemented yet")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, CodeNotFound, "not found")
	})
	return s.logRequests(s.recoverPanics(s.checkHost(s.checkOrigin(top))))
}

type reqInfoKey struct{}

// reqInfo collects what the request log line needs. Never add tokens,
// queries or bodies here.
type reqInfo struct{ device string }

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		info := &reqInfo{}
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r.WithContext(context.WithValue(r.Context(), reqInfoKey{}, info)))
		if s.o.Logger != nil {
			s.o.Logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path, // the query may hold search words, so it is left out
				"device", info.device,
				"status", rw.status,
				"ms", time.Since(start).Milliseconds(),
			)
		}
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				if s.o.Logger != nil {
					s.o.Logger.Error("panic", "path", r.URL.Path)
				}
				writeError(w, http.StatusInternalServerError, CodeInternal, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// checkHost blocks DNS rebinding: a web page cannot make the browser send
// our host names.
func (s *Server) checkHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
		if !s.hosts[host] {
			writeError(w, http.StatusForbidden, CodeBadHost, "host not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// checkOrigin refuses browser cross-origin requests. The app is native and
// sends no Origin header; CORS is never enabled.
func (s *Server) checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" || r.Method == http.MethodOptions {
			writeError(w, http.StatusForbidden, CodeCrossOrigin, "cross-origin requests are not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authenticate requires a device token on every request (N4). The
// Tailscale login header is checked too, but never trusted alone, because a
// local program can forge it.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client := clientKey(r)
		if s.refuseIfLimited(w, client) {
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		var d devices.Device
		if ok && s.o.Devices != nil {
			d, ok = s.o.Devices.Verify(strings.TrimSpace(token))
		} else {
			ok = false
		}
		if !ok {
			s.limiter.fail(client)
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, CodeUnauthorized, "device token required")
			return
		}
		if info, _ := r.Context().Value(reqInfoKey{}).(*reqInfo); info != nil {
			info.device = d.ID
		}
		if login := r.Header.Get("Tailscale-User-Login"); s.o.TailscaleOwner != "" && login != "" && !strings.EqualFold(login, s.o.TailscaleOwner) {
			writeError(w, http.StatusForbidden, CodeWrongAccount, "tailscale account is not the PC owner")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// InfoResponse is GET /v1/info. Fields are only ever added (principle 4).
type InfoResponse struct {
	Name         string             `json:"name"`
	Version      string             `json:"version"`
	APIVersion   int                `json:"apiVersion"`
	Hostname     string             `json:"hostname"`
	OS           string             `json:"os"`
	Agents       []core.AdapterInfo `json:"agents"`
	Capabilities []core.Capability  `json:"capabilities"` // union over agents
	Reserved     []core.Capability  `json:"reserved"`     // known names not offered yet
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	resp := InfoResponse{
		Name: "asksemble-agent", Version: s.o.Version, APIVersion: APIVersion,
		Hostname: s.o.Hostname, OS: runtime.GOOS,
		Agents: []core.AdapterInfo{}, Capabilities: []core.Capability{}, Reserved: []core.Capability{},
	}
	for _, a := range s.o.Adapters {
		ai := a.Info(r.Context())
		resp.Agents = append(resp.Agents, ai)
		for _, c := range ai.Capabilities {
			if !slices.Contains(resp.Capabilities, c) {
				resp.Capabilities = append(resp.Capabilities, c)
			}
		}
	}
	for _, c := range core.AllCapabilities {
		if !slices.Contains(resp.Capabilities, c) {
			resp.Reserved = append(resp.Reserved, c)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// SessionsResponse is GET /v1/sessions.
type SessionsResponse struct {
	Sessions []core.Session `json:"sessions"`
	Warnings []string       `json:"warnings"`
}

const maxQuery = 200

func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := core.Filter{
		Query:  q.Get("q"),
		Folder: q.Get("folder"),
		Agent:  q.Get("agent"),
		Hidden: s.o.HiddenFolders,
	}
	if len(f.Query) > maxQuery || len(f.Folder) > 1024 {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "query too long")
		return
	}
	if st := q.Get("state"); st != "" {
		for _, part := range strings.Split(st, ",") {
			state := core.State(strings.TrimSpace(part))
			if !core.ValidState(state) {
				writeError(w, http.StatusBadRequest, CodeBadRequest, "unknown state")
				return
			}
			f.States = append(f.States, state)
		}
	}
	resp := SessionsResponse{Sessions: []core.Session{}, Warnings: []string{}}
	var all []core.Session
	for _, a := range s.o.Adapters {
		if f.Agent != "" && a.Agent() != f.Agent {
			continue
		}
		res, err := a.ListSessions(r.Context())
		if err != nil {
			resp.Warnings = append(resp.Warnings, a.Agent()+"_list_failed")
			if s.o.Logger != nil {
				s.o.Logger.Warn("list sessions failed", "agent", a.Agent(), "err", err.Error())
			}
			continue
		}
		all = append(all, res.Sessions...)
		resp.Warnings = append(resp.Warnings, res.Warnings...)
	}
	resp.Sessions = append(resp.Sessions, f.Apply(all)...)
	writeJSON(w, http.StatusOK, resp)
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	var b errorBody
	b.Error.Code, b.Error.Message = code, msg
	writeJSON(w, status, b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Listen opens 127.0.0.1:port. The agent never listens on other addresses.
func Listen(port int) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}

// HTTPServer wraps h with conservative timeouts.
func HTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}
