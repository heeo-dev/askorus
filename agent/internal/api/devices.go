package api

import (
	"net/http"

	"github.com/heeo-dev/askorus/agent/internal/devices"
)

// deviceRevoker is the part of the device store that can drop a device.
// The PC's `devices revoke` uses the same call.
type deviceRevoker interface {
	Revoke(idOrName string) (devices.Device, error)
}

// forgetSelf is DELETE /v1/devices/self: the phone unpairs itself, so the
// token it is about to forget stops working on the PC too.
func (s *Server) forgetSelf(w http.ResponseWriter, r *http.Request) {
	rv, ok := s.o.Devices.(deviceRevoker)
	id := deviceOf(r)
	if !ok || id == "" {
		writeError(w, http.StatusNotImplemented, CodeNotSupported, "devices cannot be removed here")
		return
	}
	if _, err := rv.Revoke(id); err != nil {
		if s.o.Logger != nil {
			s.o.Logger.Warn("unpair failed", "device", id)
		}
		writeError(w, http.StatusInternalServerError, CodeInternal, "could not remove the device")
		return
	}
	if s.o.Logger != nil {
		s.o.Logger.Info("device unpaired itself", "device", id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"deviceId": id, "removed": true})
}
