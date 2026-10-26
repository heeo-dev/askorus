package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"

	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
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
}

func (s *Server) actionError(w http.ResponseWriter, err error) {
	var ae *core.ActionError
	if errors.As(err, &ae) {
		status, ok := actionStatus[ae.Code]
		if !ok {
			status = http.StatusInternalServerError
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
	return l, id, native, true
}

// openSession is POST /v1/sessions/{id}/open (F3, F6).
func (s *Server) openSession(w http.ResponseWriter, r *http.Request) {
	l, _, native, ok := s.launcherFor(w, r)
	if !ok {
		return
	}
	res, err := l.Open(r.Context(), native)
	if err != nil {
		s.actionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
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
	folder, ok := s.folderByID(req.Folder)
	if !ok {
		writeError(w, http.StatusNotFound, CodeFolderUnknown, "folder is not in the allowed list")
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
	res, err := l.Start(r.Context(), core.StartRequest{Folder: folder.Path, Name: req.Name, Model: req.Model, Effort: req.Effort})
	if err != nil {
		s.actionError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
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

// FolderInfo is one entry of GET /v1/folders.
type FolderInfo struct {
	ID     string              `json:"id"`
	Name   string              `json:"name"`
	Path   string              `json:"path"`
	Server config.FolderServer `json:"server"`
}

func (s *Server) folders(w http.ResponseWriter, r *http.Request) {
	out := []FolderInfo{}
	if s.o.Folders != nil {
		for _, f := range s.o.Folders() {
			out = append(out, FolderInfo{ID: f.ID, Name: filepath.Base(f.Path), Path: f.Path, Server: f.Server})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": out})
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
