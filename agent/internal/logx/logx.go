// Package logx builds the agent logger. Logs keep time, device, action and
// result only. Attributes whose key looks like a secret or conversation
// content are replaced, as a second line of defence (N8).
package logx

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Redacted replaces the value of a sensitive attribute.
const Redacted = "[redacted]"

// sensitive are matched as parts of a key, in any case: "inputTokens" and
// "answers" are redacted too, so keys that carry counts must not contain
// them (comparisons log "ais", not "answers"). The words of P2-3 cover
// what a comparison holds: answers, questions and earlier questions, their
// sentences and claims, and a judge's evidence and reasons.
var sensitive = []string{
	"token", "authorization", "bearer", "secret", "password", "credential",
	"cookie", "pair_code", "paircode", "prompt", "content", "message_text",
	"answer", "question", "sentence", "claim", "evidence", "reason", "earlier",
	"apikey", "api_key",
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

// File is a log file that appends and keeps at most about twice max bytes:
// when a write would take it past max, the file becomes <path>.1 (in place
// of the one before) and a new file starts. The agent log grew without end
// before, refused requests included (security review 11-08, R-87).
type File struct {
	path string
	max  int64

	mu     sync.Mutex
	f      *os.File // nil after a failed reopen: the next write tries again
	size   int64
	closed bool

	// rename moves the full file aside; a test makes it fail.
	rename func(from, to string) error
}

// OpenFile opens path for appending, rolling it over at max bytes.
func OpenFile(path string, max int64) (*File, error) {
	l := &File{path: path, max: max, rename: os.Rename}
	if err := l.open(0); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *File) open(flag int) error {
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY|flag, 0o600)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	l.f, l.size = f, fi.Size()
	return nil
}

// Write appends p, rolling the file over first when p would take it past
// max. When the file cannot be moved aside (on Windows another program may
// hold it open without delete sharing) it starts over empty in place, so
// the size stays bounded even then (Codex cross-review 11-08). When it
// cannot be opened again, this write fails and the next one tries again.
func (l *File) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, os.ErrClosed
	}
	if l.f != nil && l.size > 0 && l.size+int64(len(p)) > l.max {
		l.f.Close()
		l.f = nil
		flag := 0
		if l.rename(l.path, l.path+".1") != nil {
			flag = os.O_TRUNC
		}
		if err := l.open(flag); err != nil {
			return 0, err
		}
	}
	if l.f == nil {
		if err := l.open(0); err != nil {
			return 0, err
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

// Close closes the file. Later writes fail.
func (l *File) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// RollOver moves path to <path>.1 when it is larger than max, for a file
// another process writes (the background agent's console output).
func RollOver(path string, max int64) error {
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) || err == nil && fi.Size() <= max {
		return nil
	}
	if err != nil {
		return err
	}
	return os.Rename(path, path+".1")
}
