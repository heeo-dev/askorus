// Package logx builds the agent logger. Logs keep time, device, action and
// result only. Attributes whose key looks like a secret or conversation
// content are replaced, as a second line of defence (N8).
package logx

import (
	"io"
	"log/slog"
	"strings"
)

// Redacted replaces the value of a sensitive attribute.
const Redacted = "[redacted]"

var sensitive = []string{
	"token", "authorization", "bearer", "secret", "password", "credential",
	"cookie", "pair_code", "paircode", "prompt", "content", "message_text",
}

// IsSensitive reports whether an attribute key must not be logged.
func IsSensitive(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitive {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// New returns a JSON logger writing to w.
func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if IsSensitive(a.Key) {
				return slog.String(a.Key, Redacted)
			}
			return a
		},
	}))
}

// Discard is a logger that drops everything, for tests.
func Discard() *slog.Logger { return slog.New(slog.DiscardHandler) }
