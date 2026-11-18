package api

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/uploads"
)

// Photos with a question (#14). The phone uploads each photo first (POST
// /v1/uploads, the bytes as the body) and gets an id; POST /v1/runs then
// names the ids in attachments. The agent decides what the bytes are by
// looking at them, keeps the photo under its own name, and deletes it ten
// minutes after its last use (internal/uploads). Neither the photo nor
// anything about it is logged beyond the request line.

// Error codes of the uploads.
const (
	CodeUploadsOff     = "uploads_unavailable" // this agent keeps no photos (no uploads folder)
	CodeUploadTooLarge = "upload_too_large"
	CodeUploadBadType  = "upload_bad_type"
	// CodeUploadLimit: the device keeps too many photos on the PC already,
	// or too many uploads are coming in at once. Try again a little later.
	CodeUploadLimit = "upload_limit"
	// CodeUploadGone: a run named a photo the PC no longer keeps (deleted
	// ten minutes after its last use, or the agent restarted). The phone
	// uploads its copy again and sends the same run again.
	CodeUploadGone = "upload_gone"
)

const (
	// uploadReadLimit is how long one photo may take to arrive. The server's
	// own limit for a request (15 s) is too short for 4 MiB over a relayed
	// connection.
	uploadReadLimit = 2 * time.Minute
	// uploadAnswerLimit is how long after that the answer may take. The
	// server counts its write limit (60 s) from when the request began, so
	// a photo that took longer to arrive would be kept and never answered:
	// the phone would send it again (R-119).
	uploadAnswerLimit = 30 * time.Second
	// maxUploading bounds the uploads being read at once, each of which
	// holds up to core.MaxImageBytes in memory.
	maxUploading = 4
)

// AttachmentsInfo is GET /v1/info's attachments: what a question may
// carry. It is there only when the agent keeps photos; an older agent, or
// one without an uploads folder, has none and the phone offers no photos.
// Which AI sees photos is in each AI's ask.images.
type AttachmentsInfo struct {
	MaxCount int      `json:"maxCount"`
	MaxBytes int      `json:"maxBytes"`
	Types    []string `json:"types"`
}

func (s *Server) attachmentsInfo() *AttachmentsInfo {
	if s.o.Uploads == nil {
		return nil
	}
	return &AttachmentsInfo{MaxCount: core.MaxImages, MaxBytes: core.MaxImageBytes, Types: core.ImageTypes}
}

// upload is POST /v1/uploads: the body is one photo. 201 with {id, type,
// bytes}. The Content-Type is not read: the agent looks at the bytes.
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if s.o.Uploads == nil {
		writeError(w, http.StatusServiceUnavailable, CodeUploadsOff, "this PC takes no photos")
		return
	}
	select {
	case s.uploading <- struct{}{}:
		defer func() { <-s.uploading }()
	default:
		writeError(w, http.StatusTooManyRequests, CodeUploadLimit, "too many photos are coming in at once")
		return
	}
	// Not every ResponseWriter can move its deadlines; the server's own then stay.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(uploadReadLimit))
	_ = rc.SetWriteDeadline(time.Now().Add(uploadReadLimit + uploadAnswerLimit))
	up, err := s.o.Uploads.Put(deviceOf(r), http.MaxBytesReader(w, r.Body, core.MaxImageBytes+1))
	var tooLong *http.MaxBytesError
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, up)
	case errors.Is(err, uploads.ErrTooLarge), errors.As(err, &tooLong):
		writeError(w, http.StatusRequestEntityTooLarge, CodeUploadTooLarge, "the photo is larger than the limit")
	case errors.Is(err, uploads.ErrBadType):
		writeError(w, http.StatusUnsupportedMediaType, CodeUploadBadType, "the file is not a JPEG, PNG or WebP image")
	case errors.Is(err, uploads.ErrLimit):
		writeError(w, http.StatusTooManyRequests, CodeUploadLimit, "this phone keeps too many photos on the PC; try again in a few minutes")
	default:
		if s.o.Logger != nil {
			s.o.Logger.Error("upload failed") // the error may hold a path
		}
		writeError(w, http.StatusInternalServerError, CodeInternal, "the PC could not keep the photo")
	}
}

// deleteUpload is DELETE /v1/uploads/{id}: the phone took a photo off the
// question before sending it. It answers 204 whether or not there was such
// a photo, so it tells nothing about other devices' ids.
func (s *Server) deleteUpload(w http.ResponseWriter, r *http.Request) {
	if id := r.PathValue("id"); s.o.Uploads != nil && core.ValidImageID(id) {
		s.o.Uploads.Delete(deviceOf(r), id)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// runHolds keeps, for each run with photos, the function that lets the
// photos go when the run has ended.
type runHolds struct {
	mu      sync.Mutex
	release map[string]func()
}

// keep registers release for runID and reports whether it was the first
// for that run; a run sent again holds its photos already.
func (h *runHolds) keep(runID string, release func()) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.release == nil {
		h.release = map[string]func(){}
	}
	if _, ok := h.release[runID]; ok {
		return false
	}
	h.release[runID] = release
	return true
}

// done lets the photos of runID go.
func (h *runHolds) done(runID string) {
	h.mu.Lock()
	release := h.release[runID]
	delete(h.release, runID)
	h.mu.Unlock()
	if release != nil {
		release()
	}
}

// imageHolder gives a comparison the way to keep its question's photos on
// the PC while its combined answer waits and is written: the same hold a
// run takes, by the photos' ids. It reports false when one is gone.
func (s *Server) imageHolder(owner string, images []core.Image) func() (func(), bool) {
	ids := make([]string, len(images))
	for i, im := range images {
		name := filepath.Base(im.Path)
		ids[i] = strings.TrimSuffix(name, filepath.Ext(name))
	}
	return func() (func(), bool) {
		if s.o.Uploads == nil {
			return nil, false
		}
		_, release, err := s.o.Uploads.Hold(owner, ids)
		if err != nil {
			return nil, false
		}
		return release, true
	}
}

// holdAttachments checks a run's attachments and holds the photos for it.
// It answers the HTTP error itself and returns ok=false. Without
// attachments it returns nothing to release.
func (s *Server) holdAttachments(w http.ResponseWriter, owner string, ids []string) (images []core.Image, release func(), ok bool) {
	if len(ids) == 0 {
		return nil, func() {}, true
	}
	if s.o.Uploads == nil {
		writeError(w, http.StatusServiceUnavailable, CodeUploadsOff, "this PC takes no photos")
		return nil, nil, false
	}
	if len(ids) > core.MaxImages {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "attachments: at most 4 photos")
		return nil, nil, false
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !core.ValidImageID(id) || seen[id] {
			writeError(w, http.StatusBadRequest, CodeBadRequest, "attachments must be ids of uploaded photos, each once")
			return nil, nil, false
		}
		seen[id] = true
	}
	images, release, err := s.o.Uploads.Hold(owner, ids)
	if err != nil {
		writeError(w, http.StatusConflict, CodeUploadGone, "a photo of the question is no longer on the PC; send it again")
		return nil, nil, false
	}
	return images, release, true
}
