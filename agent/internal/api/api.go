// Package api is the agent's HTTP API. It is served only on the agent's own
// Tailscale node (package tsnode), which also says who sent each request
// (WithCaller). It knows adapters only through core.Adapter (principle 2).
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/compares"
	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/devices"
	"github.com/heeo-dev/askorus/agent/internal/jobs"
	"github.com/heeo-dev/askorus/agent/internal/pairing"
	"github.com/heeo-dev/askorus/agent/internal/runs"
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
	CodePairUnknown    = "pair_unknown"
	// CodePairAskLimit: this device's requests without a code kept ending
	// refused or unanswered, so nobody is asked about it for a while (#58).
	CodePairAskLimit = "pair_ask_limit"
)

// TokenVerifier checks device tokens.
type TokenVerifier interface {
	Verify(token string) (devices.Device, bool)
}

// Pairer joins a new device (see package pairing).
type Pairer interface {
	Start(code, deviceName string, o pairing.Origin) (requestID string, err error)
	Status(requestID string) (pairing.State, string, devices.Device, error)
	// Pairing without a code (#58).
	StartDirect(deviceName, match string, o pairing.Origin, ended func(pairing.State)) (requestID string, err error)
	WindowIsOpen() bool
	Pending() []pairing.Question
	Answer(questionID string, allow bool) error
}

// Options configures the server.
type Options struct {
	Version  string
	Hostname string
	Adapters []core.Adapter
	Devices  TokenVerifier
	Pairer   Pairer // nil: POST /v1/pair is refused
	Logger   *slog.Logger
	// Hosts returns the names and addresses the agent's Tailscale node
	// answers on (its MagicDNS name, with and without the tailnet's part,
	// and its addresses). A request to any other Host is refused. It is
	// called per request: they are known only once the node has signed in.
	Hosts func() []string
	// Owner returns the login of the Tailscale account the node is signed
	// in as, which is the PC owner's; "" until it is. Every request must
	// come from a device of that account (checkCaller).
	Owner         func() string
	HiddenFolders []string
	// Hidden sessions are left out of the list; DELETE adds to it.
	Hidden HiddenStore
	// Folders returns the folders new sessions may start in. It is called
	// per request so edits to the settings file apply without a restart.
	Folders func() []config.Folder
	// SetFolderServer saves a folder's server setting (F10) and returns
	// the saved folder; ApplyFolders then makes the servers match.
	SetFolderServer func(id string, fs config.FolderServer) (config.Folder, error)
	ApplyFolders    func()
	// HistoryFolders reports whether folders with past sessions may be
	// used too (기록 폴더); nil means no. AddFolder saves one of them to the
	// allowed list, for keeping it open.
	HistoryFolders func() bool
	AddFolder      func(path string) (config.Folder, error)
	// AskFolder is where asks run (mode ask, P0-9): an empty folder
	// outside every project. Sessions there get the ask badge and it is
	// never offered for new sessions. Empty turns asks off. Sessions in
	// its judge folder (core.JudgeFolder) are left out of the list, and
	// no session action reaches them. Without it no AI writes combined
	// answers.
	AskFolder string
	// Judge is agent.json's judge block (P2-3): combined answers off, and
	// the one-off processes per AI.
	Judge config.JudgeConfig
	// JudgeMinVersions maps an AI to the oldest version of its CLI the
	// one-off command was tested with (its adapter's JudgeMinVersion). An
	// AI not listed never writes.
	JudgeMinVersions map[string]string
	// Refine turns 질문 정제 on (milestone C): a run's refine field then
	// asks the moderator first and every AI gets the question with its
	// answer format. Off since 2026-11-04 (C4: the formats did not make the
	// combined answer better; the user's decision): the field is ignored
	// and every AI gets the question as written. The code stays.
	Refine bool
}

// Server serves /v1.
type Server struct {
	o       Options
	limiter *limiter
	// asks counts the requests to pair without a code that somebody had to
	// be asked about and that ended refused or unanswered.
	asks *limiter
	jobs *jobs.Manager
	runs *runs.Manager
	// gate decides which AIs write combined answers; /v1/info and the
	// comparisons share it.
	gate     compares.Gate
	compares *compares.Manager
}

// New builds a server.
func New(o Options) *Server {
	s := &Server{
		o:       o,
		limiter: newLimiter(),
		asks:    newAskLimiter(),
		jobs:    jobs.NewManager(),
		runs:    runs.NewManager(),
	}
	s.runs.OnPart = func(mode string, p runs.Part) {
		if o.Logger == nil {
			return
		}
		code := ""
		if p.Error != nil {
			code = p.Error.Code // the message is the AI's text and is not logged
		}
		o.Logger.Info("run part", "mode", mode, "agent", p.Agent, "status", string(p.Status), "ms", p.Ms, "code", code)
	}
	s.gate = compares.Gate{Settings: o.Judge, MinVersions: o.JudgeMinVersions}
	if filepath.IsAbs(o.AskFolder) {
		s.gate.Folder = core.JudgeFolder(o.AskFolder)
	}
	s.compares = compares.New(compares.Options{Adapters: o.Adapters, Gate: s.gate, Runs: s.runs, Logger: o.Logger})
	// A run's comparison starts when its last answer is in (P2-3).
	s.runs.OnEnd = s.compares.RunEnded
	return s
}

// Compares is the server's comparisons, for tests that wait for one.
func (s *Server) Compares() *compares.Manager { return s.compares }

// Handler returns the full middleware chain.
func (s *Server) Handler() http.Handler {
	top := http.NewServeMux()
	// Pairing is the only route without a device token.
	top.HandleFunc("POST /v1/pair", s.pair)
	top.HandleFunc("/v1/pair", methodNotAllowed)
	top.HandleFunc("POST /v1/pair/status", s.pairStatus)
	top.HandleFunc("/v1/pair/status", methodNotAllowed)
	// So is hello: a phone of the same Tailscale account lists this PC before it is paired (#58).
	top.HandleFunc("GET /v1/hello", s.hello)
	top.HandleFunc("/v1/hello", methodNotAllowed)
	mux := http.NewServeMux()
	top.Handle("/", s.authenticate(mux))
	route := func(method, path string, h http.HandlerFunc) {
		mux.HandleFunc(method+" "+path, h)
		mux.HandleFunc(path, methodNotAllowed)
	}
	route("GET", "/v1/info", s.info)
	// A paired phone answers a pairing request when nobody is at the PC (#58).
	route("GET", "/v1/pair/requests", s.pairRequests)
	route("POST", "/v1/pair/requests/answer", s.pairAnswer)
	mux.HandleFunc("GET /v1/sessions", s.sessions)
	mux.HandleFunc("POST /v1/sessions", s.startSession)
	mux.HandleFunc("/v1/sessions", methodNotAllowed)
	mux.HandleFunc("POST /v1/sessions/{id}/open", s.openSession)
	mux.HandleFunc("POST /v1/sessions/{id}/stop", s.stopSession)
	mux.HandleFunc("DELETE /v1/sessions/{id}", s.removeSession)
	route("GET", "/v1/sessions/{id}/preview", s.previewSession)
	route("GET", "/v1/jobs/{jobId}", s.jobStatus)
	route("POST", "/v1/runs", s.startRun)
	route("GET", "/v1/runs/{runId}", s.getRun)
	route("POST", "/v1/comparisons", s.startComparison)
	route("GET", "/v1/comparisons/{id}", s.getComparison)
	route("POST", "/v1/comparisons/{id}/retry", s.retryComparison)
	route("POST", "/v1/comparisons/{id}/cancel", s.cancelComparison)
	route("POST", "/v1/comparisons/{id}/synthesis", s.writeSynthesis)
	route("POST", "/v1/debates", s.startDebate)
	route("GET", "/v1/debates/{id}", s.getDebate)
	route("POST", "/v1/debates/{id}/stop", s.stopDebate)
	route("GET", "/v1/folders", s.folders)
	route("PUT", "/v1/folders/{id}/server", s.setFolderServer)
	route("DELETE", "/v1/devices/self", s.forgetSelf)
	mux.HandleFunc("/v1/sessions/{id}/{action}", methodNotAllowed)
	mux.HandleFunc("/v1/sessions/{id}", methodNotAllowed)
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

// maxLogPath is how much of a request path the log keeps (R-87). The
// agent's own paths are far shorter.
const maxLogPath = 200

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		info := &reqInfo{}
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r.WithContext(context.WithValue(r.Context(), reqInfoKey{}, info)))
		if s.o.Logger != nil {
			s.o.Logger.Info("request",
				"method", r.Method,
				// The query may hold search words, so it is left out. The
				// path is cut: a refused request can carry a long one (R-87).
				"path", clip(r.URL.Path, maxLogPath),
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
					s.o.Logger.Error("panic", "path", clip(r.URL.Path, maxLogPath))
				}
				writeError(w, http.StatusInternalServerError, CodeInternal, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// checkHost blocks DNS rebinding: a web page in a browser on one of the
// person's devices cannot make that browser send our node's names.
func (s *Server) checkHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
		ok := false
		if s.o.Hosts != nil {
			for _, h := range s.o.Hosts() {
				if host != "" && host == strings.ToLower(strings.TrimSuffix(h, ".")) {
					ok = true
					break
				}
			}
		}
		if !ok {
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

// authenticate requires two things of every request (N4): it came from a
// device of the PC owner's own Tailscale account (checkCaller, what the
// node itself saw), and it carries a paired device's token. Neither alone
// lets anyone in.
//
// The token is looked at before the failure limit (R-101, #43): a request
// with a paired phone's token is never refused for the wrong requests of
// others, which another device of the tailnet could pile up on purpose.
// Only requests without a right token are counted and refused. A token is
// 32 random bytes, so a locked-out sender gains nothing from still being
// checked.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.checkCaller(w, r); !ok {
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
			client := clientKey(r)
			if s.refuseIfLimited(w, client) {
				return
			}
			s.limiter.fail(client)
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, CodeUnauthorized, "device token required")
			return
		}
		if info, _ := r.Context().Value(reqInfoKey{}).(*reqInfo); info != nil {
			info.device = d.ID
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
	// judging (how comparisons were judged, P2-3) went with the judging
	// (C6d, #24): it was only sent when an AI judged, so it is just never
	// there now.
	// Integrations are the services outside the PC the agent sends to
	// itself. There are none (N1): it is always [].
	Integrations []IntegrationInfo `json:"integrations"`
	// Debate is there when the agent runs debates (P2-5, #26): the phone
	// shows '토론하기' only then, so an older agent never gets one.
	Debate *DebateInfo `json:"debate,omitempty"`
	// Context is there when the agent takes a conversation record with a
	// follow-up, a comparison and a debate (#29): the phone sends one only
	// then.
	Context *ContextInfo `json:"context,omitempty"`
	// Moderator is there when the agent asks the moderator alone (#25):
	// the phone offers '사회자' in the follow-up picker only then.
	Moderator *ModeratorInfo `json:"moderator,omitempty"`
	// Pairing is there when the agent takes pairing requests without a code
	// and lets a paired phone answer them (#58): the phone looks for
	// waiting requests only then.
	Pairing *PairingInfo `json:"pairing,omitempty"`
}

// PairingInfo says what the agent's pairing can do beyond the QR code.
type PairingInfo struct {
	// Approve: GET /v1/pair/requests and POST /v1/pair/requests/answer exist.
	Approve bool `json:"approve"`
}

// IntegrationInfo is one service outside the PC the agent would send to.
// None exists in P2-3; it is an object so that later entries can gain
// fields inside /v1 (principle 4, S3 decision 13).
type IntegrationInfo struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Destination string `json:"destination"`
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	resp := InfoResponse{
		Name: "askorus-agent", Version: s.o.Version, APIVersion: APIVersion,
		Hostname: s.o.Hostname, OS: runtime.GOOS,
		Agents: []core.AdapterInfo{}, Capabilities: []core.Capability{}, Reserved: []core.Capability{},
		Integrations: []IntegrationInfo{},
		Debate:       s.debateInfo(),
		Context:      contextInfo(),
		Moderator:    s.moderatorInfo(),
	}
	if s.o.Pairer != nil {
		resp.Pairing = &PairingInfo{Approve: true}
	}
	// The CLI's info calls Announce too, so the two cannot disagree on
	// the ask tiers or on which AIs write combined answers.
	infos, _ := compares.Announce(r.Context(), s.o.Adapters, s.gate)
	for _, ai := range infos {
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
	if s.o.AskFolder != "" {
		// The one-off calls run in <ask folder>\judge (P2-3). Concat makes a
		// new slice, so the settings' list is never appended to.
		f.Hidden = slices.Concat(s.o.HiddenFolders, []string{core.JudgeFolder(s.o.AskFolder)})
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
	if s.o.Hidden != nil {
		f.HiddenIDs = s.o.Hidden.Set()
	}
	s.markAskSessions(all)
	resp.Sessions = append(resp.Sessions, f.Apply(all)...)
	writeJSON(w, http.StatusOK, resp)
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, CodeMethod, "method not allowed")
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

// Close ends the work the server still runs in the background (asks and
// comparisons), for agent shutdown.
func (s *Server) Close() {
	s.runs.Close()
	s.compares.Close()
}

// HTTPServer wraps h with conservative timeouts. The agent serves it only
// on its Tailscale node's listener, never on the PC's own addresses.
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
