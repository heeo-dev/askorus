package api

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/made"
)

// Made pictures (#14, decisions of 2026-11-21). An AI that makes pictures
// saves them in a folder of its own; its adapter finds the new, checked
// files (core.FindMade) and the run keeps copies of them here under ids the
// agent makes (internal/made). The part names them in images; the phone
// that started the run takes each with GET /v1/runs/{run}/images/{id}.
// Nothing about a picture is logged beyond the request line.

// CodeImageNotFound: no such picture for this device and run (another
// device's, more than a day old, or the agent restarted).
const CodeImageNotFound = "image_not_found"

// imageSendLimit is how long one picture (up to core.MaxMadeBytes) may take
// to leave: the server's own write limit is too short over a relayed
// connection.
const imageSendLimit = 2 * time.Minute

// keepMade copies the pictures an AI made in owner's run into the store and
// gives what the part shows of them, in order. A picture that cannot be
// copied is left out (the caller counts it as left out).
func (s *Server) keepMade(owner, run string, files []core.MadeFile) []core.MadePicture {
	if s.o.Made == nil || run == "" {
		return nil
	}
	var out []core.MadePicture
	for _, f := range files {
		p, err := s.o.Made.Put(owner, run, f)
		if err != nil {
			if s.o.Logger != nil {
				s.o.Logger.Warn("a made picture could not be kept")
			}
			continue
		}
		out = append(out, p)
	}
	return out
}

// runImage is GET /v1/runs/{runId}/images/{imageId}: the bytes of a picture
// an AI made in the run, for the device that started it only. Any other
// device, and an id that is not there, gets the same 404.
func (s *Server) runImage(w http.ResponseWriter, r *http.Request) {
	owner := deviceOf(r)
	run, id := r.PathValue("runId"), r.PathValue("imageId")
	if s.o.Made == nil || !made.ValidID(id) {
		writeError(w, http.StatusNotFound, CodeImageNotFound, "no such picture")
		return
	}
	if _, ok := s.runs.Get(owner, run); !ok {
		writeError(w, http.StatusNotFound, CodeImageNotFound, "no such picture")
		return
	}
	path, typ, err := s.o.Made.Get(owner, run, id)
	if err != nil {
		writeError(w, http.StatusNotFound, CodeImageNotFound, "no such picture")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, CodeImageNotFound, "no such picture")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > core.MaxMadeBytes {
		writeError(w, http.StatusNotFound, CodeImageNotFound, "no such picture")
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(imageSendLimit))
	h := w.Header()
	h.Set("Content-Type", typ)
	h.Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, io.LimitReader(f, core.MaxMadeBytes))
}
