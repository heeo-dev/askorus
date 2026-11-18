package api

// The phone's update button (#35, Phase 1 plan part 27):
//
//	GET  /v1/update  what the phone shows: on, whether this agent can update
//	                 itself and this phone may start it, the latest release
//	                 (asked of GitHub at most every six hours), the work an
//	                 update would cut off, and the last update's outcome
//	POST /v1/update  {"version": "0.5.1"}: take that release. The phone names
//	                 the version it saw, so a release that changed meanwhile
//	                 is refused; it picks nothing else.
//
// While an update is under way the agent takes no new ask, comparison,
// debate or session (503 updating) and waits up to ten minutes for those
// running; then the new program takes over (cmd/askorus-agent).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// UpdateDestination is where the update check goes, as /v1/info's
// integrations name it (internal/update.DefaultBase).
const UpdateDestination = "github.com/heeo-dev/askorus"

const (
	CodeUpdating          = "updating"            // an update is under way: new work waits
	CodeUpdateOff         = "update_unavailable"  // updates are off or this agent cannot update itself
	CodeUpdateNotAllowed  = "update_not_allowed"  // this phone may not start updates
	CodeUpdateChanged     = "update_changed"      // the latest release is not the one the phone saw
	CodeUpdateRunning     = "update_running"      // one is under way already
	CodeUpdateCheckFailed = "update_check_failed" // GitHub did not answer, or the release did not check out
)

// Updater is the agent's side of the update (cmd/askorus-agent). Nil: no
// update field in /v1/info and no /v1/update.
type Updater interface {
	// Info is the part of the status that needs no network, for /v1/info.
	Info(device string) UpdateInfo
	// Status is GET /v1/update; it may ask GitHub (remembered six hours).
	Status(ctx context.Context, device string) UpdateStatus
	// Start begins taking version for device and returns once it is under
	// way. Errors are the Err values below, or a check that failed.
	Start(device, version string, w Work) error
}

// The errors Start returns, as the phone sees them.
var (
	ErrUpdateOff        = errors.New(CodeUpdateOff)
	ErrUpdateNotAllowed = errors.New(CodeUpdateNotAllowed)
	ErrUpdateChanged    = errors.New(CodeUpdateChanged)
	ErrUpdateRunning    = errors.New(CodeUpdateRunning)
)

// Work is what an update asks of the server.
type Work interface {
	// Busy is the work an update would cut off now.
	Busy() UpdateBusy
	// Hold makes the server refuse new work (503 updating), or take it
	// again.
	Hold(on bool)
	// WaitIdle waits until no ask, comparison or debate runs.
	WaitIdle(ctx context.Context) error
}

// UpdateInfo is /v1/info's update: the agent takes updates from phones.
type UpdateInfo struct {
	On      bool   `json:"on"`            // updates are on (askorus-agent update)
	Can     bool   `json:"can"`           // this agent can update itself
	Why     string `json:"why,omitempty"` // when it cannot: off, not_release, not_installed, no_key
	Allowed bool   `json:"allowed"`       // this phone may start one
}

// UpdateBusy is the work an update would cut off.
type UpdateBusy struct {
	Asks    int `json:"asks"`    // questions, comparisons and debates still answering
	Folders int `json:"folders"` // folders kept open, whose remote sessions drop for a moment
}

// UpdateLast is the last update's outcome.
type UpdateLast struct {
	From   string    `json:"from"`
	To     string    `json:"to"`
	State  string    `json:"state"` // swapped, done, rolled-back, failed
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
}

// UpdateStatus is GET /v1/update.
type UpdateStatus struct {
	UpdateInfo
	Current string    `json:"current"`
	Latest  string    `json:"latest,omitempty"`
	Newer   bool      `json:"newer"`
	Checked time.Time `json:"checked,omitzero"`
	// CheckError says why the latest release is not known: no_release_info,
	// bad_release, expired, unreachable, no_key.
	CheckError string      `json:"checkError,omitempty"`
	Running    bool        `json:"running"`
	Busy       UpdateBusy  `json:"busy"`
	Last       *UpdateLast `json:"last,omitempty"`
}

func (s *Server) updateInfo(r *http.Request) *UpdateInfo {
	if s.o.Updater == nil {
		return nil
	}
	i := s.o.Updater.Info(deviceOf(r))
	return &i
}

func (s *Server) getUpdate(w http.ResponseWriter, r *http.Request) {
	if s.o.Updater == nil {
		writeError(w, http.StatusNotFound, CodeNotSupported, "this agent does not update from a phone")
		return
	}
	st := s.o.Updater.Status(r.Context(), deviceOf(r))
	st.Busy = s.Busy()
	writeJSON(w, http.StatusOK, st)
}

type updateRequest struct {
	Version string `json:"version"`
}

func (s *Server) startUpdate(w http.ResponseWriter, r *http.Request) {
	if s.o.Updater == nil {
		writeError(w, http.StatusNotFound, CodeNotSupported, "this agent does not update from a phone")
		return
	}
	var req updateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil || req.Version == "" || len(req.Version) > 32 {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "give the version to take")
		return
	}
	err := s.o.Updater.Start(deviceOf(r), req.Version, s)
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, map[string]string{"state": "started"})
	case errors.Is(err, ErrUpdateNotAllowed):
		writeError(w, http.StatusForbidden, CodeUpdateNotAllowed, "this phone may not update the agent (askorus-agent devices update <id> on)")
	case errors.Is(err, ErrUpdateOff):
		writeError(w, http.StatusConflict, CodeUpdateOff, "updates are off, or this agent cannot update itself")
	case errors.Is(err, ErrUpdateChanged):
		writeError(w, http.StatusConflict, CodeUpdateChanged, "the latest release is not the one the phone saw")
	case errors.Is(err, ErrUpdateRunning):
		writeError(w, http.StatusConflict, CodeUpdateRunning, "an update is under way")
	default:
		writeError(w, http.StatusBadGateway, CodeUpdateCheckFailed, err.Error())
	}
}

// Busy counts the work an update would cut off.
func (s *Server) Busy() UpdateBusy {
	b := UpdateBusy{Asks: s.runs.Running() + s.compares.Busy()}
	if s.o.Folders != nil {
		for _, f := range s.o.Folders() {
			if f.Server.Enabled {
				b.Folders++
			}
		}
	}
	return b
}

// Hold refuses new work while an update is under way.
func (s *Server) Hold(on bool) { s.holding.Store(on) }

// WaitIdle waits until no ask, comparison or debate runs, or ctx ends.
func (s *Server) WaitIdle(ctx context.Context) error {
	for {
		if b := s.Busy(); b.Asks == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// held answers 503 when an update is under way; the handlers that start
// work call it first.
func (s *Server) held(w http.ResponseWriter) bool {
	if !s.holding.Load() {
		return false
	}
	w.Header().Set("Retry-After", "60")
	writeError(w, http.StatusServiceUnavailable, CodeUpdating, "the agent is updating; try again in a minute")
	return true
}
