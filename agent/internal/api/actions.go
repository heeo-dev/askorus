package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/jobs"
)

// HiddenStore keeps the sessions removed from the list.
type HiddenStore interface {
	Set() map[string]bool
	Add(id string) error
}

// Error codes for actions, beside the adapter's core.ErrCode* codes.
const (
	CodeBadSessionID  = "bad_session_id"
	CodeNotSupported  = "not_supported"
	CodeFolderUnknown = "folder_not_found"
	CodeJobNotFound   = "job_not_found"
	CodeNotGit        = "not_a_git_repository"
)

// actionStatus maps adapter error codes to HTTP status.
var actionStatus = map[string]int{
	core.ErrCodeNotFound:         http.StatusNotFound,
	core.ErrCodeBusy:             http.StatusConflict,
	core.ErrCodeInTerminal:       http.StatusConflict,
	core.ErrCodeNotBackground:    http.StatusConflict,
	core.ErrCodeStateUnknown:     http.StatusServiceUnavailable,
	core.ErrCodeUntrusted:        http.StatusPreconditionFailed,
	core.ErrCodeLinkTimeout:      http.StatusGatewayTimeout,
	core.ErrCodeLaunchFailed:     http.StatusBadGateway,
	core.ErrCodeFolderNotFound:   http.StatusNotFound,
	core.ErrCodeAgentUnavailable: http.StatusServiceUnavailable,
	core.ErrCodeServiceElevated:  http.StatusServiceUnavailable,
	core.ErrCodeFolderMissing:    http.StatusGone,
}

// clip keeps the first n bytes of s, cut at a rune boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

func (s *Server) actionError(w http.ResponseWriter, err error) {
	var ae *core.ActionError
	if errors.As(err, &ae) {
		status, ok := actionStatus[ae.Code]
		if !ok {
			status = http.StatusInternalServerError
		}
		// The detail is claude's own message (never conversation content);
		// without it a failure like R-13 cannot be traced afterwards.
		if s.o.Logger != nil && status >= 500 {
			s.o.Logger.Warn("action failed", "code", ae.Code, "detail", clip(ae.Detail, 300))
		}
		writeError(w, status, ae.Code, ae.Detail)
		return
	}
	if s.o.Logger != nil {
		s.o.Logger.Error("action failed", "err", err.Error())
	}
	writeError(w, http.StatusInternalServerError, CodeInternal, "action failed")
}

func (s *Server) adapterFor(agent string) core.Adapter {
	for _, a := range s.o.Adapters {
		if a.Agent() == agent {
			return a
		}
	}
	return nil
}

// launcherFor resolves {id} to an adapter that can act on it.
func (s *Server) launcherFor(w http.ResponseWriter, r *http.Request) (core.Launcher, string, string, bool) {
	id := r.PathValue("id")
	agent, native, err := core.ParseID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, CodeBadSessionID, "session id must be <agent>:<id>")
		return nil, "", "", false
	}
	a := s.adapterFor(agent)
	if a == nil {
		writeError(w, http.StatusNotFound, core.ErrCodeNotFound, "")
		return nil, "", "", false
	}
	l, ok := a.(core.Launcher)
	if !ok {
		writeError(w, http.StatusNotImplemented, CodeNotSupported, "this agent cannot do that")
		return nil, "", "", false
	}
	if s.judgeSession(r.Context(), a, id) {
		writeError(w, http.StatusNotFound, core.ErrCodeNotFound, "")
		return nil, "", "", false
	}
	return l, id, native, true
}

// judgeSession reports whether session id of adapter a ran in the judge
// folder (P2-3). Such a session is a judge's, never the person's: the
// phone never lists it, and open, stop, remove and preview answer 404 as
// for a session that does not exist. A failing list lets the action go on:
// this is a second line of defence, after the list's hiding, and the
// adapter still checks the id itself (S3 decision 9).
func (s *Server) judgeSession(ctx context.Context, a core.Adapter, id string) bool {
	if s.o.AskFolder == "" {
		return false
	}
	dir := core.JudgeFolder(s.o.AskFolder)
	res, err := a.ListSessions(ctx)
	if err != nil {
		return false
	}
	for _, sess := range res.Sessions {
		if strings.EqualFold(sess.ID, id) && core.InFolder(sess.Folder, dir) {
			return true
		}
	}
	return false
}

// JobStarted is the 202 answer to a slow action. The phone then reads
// GET /v1/jobs/{jobId} until it is no longer running.
type JobStarted struct {
	JobID       string `json:"jobId"`
	Kind        string `json:"kind"`
	Status      string `json:"status"` // "running"
	PollAfterMs int    `json:"pollAfterMs"`
}

// JobStatus is GET /v1/jobs/{jobId}.
type JobStatus struct {
	JobID       string           `json:"jobId"`
	Kind        string           `json:"kind"`
	Status      string           `json:"status"` // running | done | failed
	PollAfterMs int              `json:"pollAfterMs,omitempty"`
	Result      *core.OpenResult `json:"result,omitempty"`
	Error       *jobError        `json:"error,omitempty"`
}

type jobError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

const jobPollMs = 1000

func deviceOf(r *http.Request) string {
	if info, _ := r.Context().Value(reqInfoKey{}).(*reqInfo); info != nil {
		return info.device
	}
	return ""
}

func (s *Server) startJob(w http.ResponseWriter, r *http.Request, kind, target string, fn func(ctx context.Context) (core.OpenResult, error)) {
	j := s.jobs.Start(deviceOf(r), kind, target, func(ctx context.Context) (any, error) {
		res, err := fn(ctx)
		if err != nil {
			if s.o.Logger != nil {
				code := core.ErrorCode(err)
				if code == "" {
					code = err.Error()
				}
				detail := ""
				var ae *core.ActionError
				if errors.As(err, &ae) {
					detail = clip(ae.Detail, 300)
				}
				s.o.Logger.Warn("job failed", "kind", kind, "code", code, "detail", detail)
			}
			return nil, err
		}
		return res, nil
	})
	writeJSON(w, http.StatusAccepted, JobStarted{JobID: j.ID, Kind: j.Kind, Status: string(j.State), PollAfterMs: jobPollMs})
}

// openSession is POST /v1/sessions/{id}/open (F3, F6). It answers 202 at
// once; opening can take 20 s and a phone may drop a request that long.
func (s *Server) openSession(w http.ResponseWriter, r *http.Request) {
	l, id, native, ok := s.launcherFor(w, r)
	if !ok {
		return
	}
	s.startJob(w, r, "open", id, func(ctx context.Context) (core.OpenResult, error) {
		return l.Open(ctx, native)
	})
}

// jobStatus is GET /v1/jobs/{jobId}. A device only sees its own jobs.
func (s *Server) jobStatus(w http.ResponseWriter, r *http.Request) {
	j, ok := s.jobs.Get(deviceOf(r), r.PathValue("jobId"))
	if !ok {
		writeError(w, http.StatusNotFound, CodeJobNotFound, "no such job (finished more than 10 minutes ago, or the agent restarted)")
		return
	}
	st := JobStatus{JobID: j.ID, Kind: j.Kind, Status: string(j.State)}
	switch j.State {
	case jobs.Running:
		st.PollAfterMs = jobPollMs
	case jobs.Done:
		if res, ok := j.Result.(core.OpenResult); ok {
			st.Result = &res
		}
	case jobs.Failed:
		var ae *core.ActionError
		if errors.As(j.Err, &ae) {
			st.Error = &jobError{Code: ae.Code, Message: ae.Detail}
		} else {
			st.Error = &jobError{Code: CodeInternal, Message: "action failed"}
		}
	}
	writeJSON(w, http.StatusOK, st)
}

// StartRequest is the body of POST /v1/sessions (F4).
type StartRequest struct {
	Agent  string `json:"agent,omitempty"` // default "claude"
	Folder string `json:"folder"`          // folder id from GET /v1/folders
	Name   string `json:"name"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request) {
	var req StartRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "body must be {folder, name, model, effort}")
		return
	}
	if req.Model != "" && !config.ValidModel(req.Model) {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "model is not a model name")
		return
	}
	if req.Effort != "" && !config.ValidEffort(req.Effort) {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "effort must be low, medium, high, xhigh or max")
		return
	}
	folder, _, ok := s.resolveFolder(r.Context(), req.Folder)
	if !ok {
		writeError(w, http.StatusNotFound, CodeFolderUnknown, "folder is not allowed and has no past sessions")
		return
	}
	agent := req.Agent
	if agent == "" {
		agent = "claude"
	}
	l, ok := s.adapterFor(agent).(core.Launcher)
	if !ok {
		writeError(w, http.StatusNotImplemented, CodeNotSupported, "this agent cannot start sessions")
		return
	}
	sreq := core.StartRequest{Folder: folder.Path, Name: req.Name, Model: req.Model, Effort: req.Effort}
	// The same request sent twice while the first still runs joins it.
	target := agent + "|" + folder.ID + "|" + req.Name + "|" + req.Model + "|" + req.Effort
	s.startJob(w, r, "start", target, func(ctx context.Context) (core.OpenResult, error) {
		return l.Start(ctx, sreq)
	})
}

// stopSession is POST /v1/sessions/{id}/stop (F5).
func (s *Server) stopSession(w http.ResponseWriter, r *http.Request) {
	l, _, native, ok := s.launcherFor(w, r)
	if !ok {
		return
	}
	sess, err := l.Stop(r.Context(), native)
	if err != nil {
		s.actionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": sess})
}

// previewSession is GET /v1/sessions/{id}/preview (F8): the last question
// and its answer, read only. The body is conversation text, so it is never
// logged (the request log has method, path and status only) or cached.
func (s *Server) previewSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	agent, native, err := core.ParseID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, CodeBadSessionID, "session id must be <agent>:<id>")
		return
	}
	a := s.adapterFor(agent)
	if a == nil {
		writeError(w, http.StatusNotFound, core.ErrCodeNotFound, "")
		return
	}
	p, ok := a.(core.Previewer)
	if !ok {
		writeError(w, http.StatusNotImplemented, CodeNotSupported, "this agent cannot do that")
		return
	}
	if s.judgeSession(r.Context(), a, id) {
		writeError(w, http.StatusNotFound, core.ErrCodeNotFound, "")
		return
	}
	pv, err := p.Preview(r.Context(), native)
	if err != nil {
		s.actionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pv)
}

// removeSession is DELETE /v1/sessions/{id} (F5): claude rm, then hidden
// from the list. The transcript is never deleted.
func (s *Server) removeSession(w http.ResponseWriter, r *http.Request) {
	l, id, native, ok := s.launcherFor(w, r)
	if !ok {
		return
	}
	if err := l.Remove(r.Context(), native); err != nil {
		s.actionError(w, err)
		return
	}
	if s.o.Hidden != nil {
		if err := s.o.Hidden.Add(id); err != nil {
			s.actionError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "hidden": s.o.Hidden != nil})
}

// FolderInfo is one entry of GET /v1/folders. Server is the setting (what
// the person asked for); Status is what runs now (F10).
type FolderInfo struct {
	ID     string              `json:"id"`
	Name   string              `json:"name"`
	Path   string              `json:"path"`
	Server config.FolderServer `json:"server"`
	Status *core.ServeStatus   `json:"status,omitempty"`
	IsGit  bool                `json:"isGit"` // worktree spawn needs a git repository
	// Source is "config" for the allowed list or "history" for a folder
	// with past sessions; LastActive is set for history folders.
	Source     string     `json:"source"`
	LastActive *time.Time `json:"lastActive,omitempty"`
}

// serverAdapter is the adapter that can serve folders, if any.
func (s *Server) serverAdapter() core.Server {
	for _, a := range s.o.Adapters {
		if srv, ok := a.(core.Server); ok {
			return srv
		}
	}
	return nil
}

func (s *Server) folderInfo(f config.Folder) FolderInfo {
	fi := FolderInfo{ID: f.ID, Name: filepath.Base(f.Path), Path: f.Path, Server: f.Server, IsGit: isGitRepo(f.Path), Source: sourceConfig}
	if srv := s.serverAdapter(); srv != nil {
		st := srv.ServerStatus(f.Path)
		fi.Status = &st
	}
	return fi
}

func isGitRepo(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

func (s *Server) folders(w http.ResponseWriter, r *http.Request) {
	out := []FolderInfo{}
	if s.o.Folders != nil {
		for _, f := range s.o.Folders() {
			out = append(out, s.folderInfo(f))
		}
	}
	for _, h := range s.historyFolders(r.Context()) {
		out = append(out, historyInfo(h))
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": out})
}

// ServerRequest is the body of PUT /v1/folders/{id}/server. Spawn and
// capacity keep their current values when left out.
type ServerRequest struct {
	Enabled  bool   `json:"enabled"`
	Spawn    string `json:"spawn,omitempty"`
	Capacity int    `json:"capacity,omitempty"`
}

// setFolderServer is PUT /v1/folders/{id}/server (F10): it saves the
// setting and lets the agent start or stop the server to match. The answer
// carries the status right away ("starting"); GET /v1/folders follows it.
func (s *Server) setFolderServer(w http.ResponseWriter, r *http.Request) {
	if s.serverAdapter() == nil || s.o.SetFolderServer == nil {
		writeError(w, http.StatusNotImplemented, CodeNotSupported, "folder servers are not available here")
		return
	}
	var req ServerRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "body must be {enabled, spawn, capacity}")
		return
	}
	f, source, ok := s.resolveFolder(r.Context(), r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, CodeFolderUnknown, "folder is not allowed and has no past sessions")
		return
	}
	if source == sourceHistory {
		// Keeping a history folder open saves it to the allowed list first;
		// turning one off changes nothing.
		if !req.Enabled || s.o.AddFolder == nil {
			writeJSON(w, http.StatusOK, historyInfo(historyFolder{ID: f.ID, Path: f.Path}))
			return
		}
		added, err := s.o.AddFolder(f.Path)
		if err != nil {
			s.actionError(w, err)
			return
		}
		f = added
	}
	fs := f.Server
	fs.Enabled = req.Enabled
	if req.Spawn != "" {
		fs.Spawn = req.Spawn
	}
	if req.Capacity != 0 {
		fs.Capacity = req.Capacity
	}
	switch {
	case fs.Spawn != config.SpawnSameDir && fs.Spawn != config.SpawnWorktree:
		writeError(w, http.StatusBadRequest, CodeBadRequest, "spawn must be same-dir or worktree")
		return
	case fs.Spawn == config.SpawnWorktree && !isGitRepo(f.Path):
		writeError(w, http.StatusBadRequest, CodeNotGit, "worktree spawn needs a git repository")
		return
	case fs.Capacity < 1 || fs.Capacity > 32:
		writeError(w, http.StatusBadRequest, CodeBadRequest, "capacity must be 1-32")
		return
	}
	saved, err := s.o.SetFolderServer(f.ID, fs)
	if err != nil {
		s.actionError(w, err)
		return
	}
	if s.o.ApplyFolders != nil {
		s.o.ApplyFolders()
	}
	writeJSON(w, http.StatusOK, s.folderInfo(saved))
}

func (s *Server) folderByID(id string) (config.Folder, bool) {
	if s.o.Folders == nil || id == "" {
		return config.Folder{}, false
	}
	for _, f := range s.o.Folders() {
		if f.ID == id {
			return f, true
		}
	}
	return config.Folder{}, false
}
