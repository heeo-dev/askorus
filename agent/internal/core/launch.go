package core

import (
	"context"
	"errors"
)

// Launcher is implemented by adapters that can start, wake, stop and
// remove sessions (F3–F5). The API checks for it with a type assertion and
// announces the matching capabilities.
type Launcher interface {
	// Open makes the session reachable from the official app, following
	// the adapter's rules for each state, and returns its remote link.
	Open(ctx context.Context, nativeID string) (OpenResult, error)
	// Start creates a new remote session in an allowed folder.
	Start(ctx context.Context, req StartRequest) (OpenResult, error)
	// Stop stops a running background session.
	Stop(ctx context.Context, nativeID string) (Session, error)
	// Remove takes the session off the adapter's own list. Transcripts stay.
	Remove(ctx context.Context, nativeID string) error
}

// StartRequest describes a new session. Folder is an absolute path the
// caller has already checked against the allowed folders.
type StartRequest struct {
	Folder string
	Name   string
	Model  string // empty: the AI's own default
	Effort string // empty: the AI's own default
}

// What Open did.
const (
	ActionNone    = "none"    // already reachable, nothing was run
	ActionResumed = "resumed" // an existing session was woken up
	ActionStarted = "started" // a new session was created
)

// Hints tell the app what the person has to do. The app turns them into text.
const (
	HintUseRCInTerminal = "use_rc_in_terminal" // session is open in a terminal: type /rc there
	HintServerSession   = "server_session"     // a folder server runs it: open it from the official app's PC list
)

// OpenResult is the answer to Open and Start.
type OpenResult struct {
	Session   Session `json:"session"`
	RemoteURL string  `json:"remoteUrl,omitempty"`
	Action    string  `json:"action"`
	Hint      string  `json:"hint,omitempty"`
}

// ActionError is a failure the app can explain. Code is stable; Detail is
// optional extra text for the person (never logged).
type ActionError struct {
	Code   string
	Detail string
}

func (e *ActionError) Error() string {
	if e.Detail != "" {
		return e.Code + ": " + e.Detail
	}
	return e.Code
}

// Action error codes.
const (
	ErrCodeNotFound         = "session_not_found"
	ErrCodeBusy             = "session_busy"
	ErrCodeInTerminal       = "session_in_terminal"
	ErrCodeNotBackground    = "session_not_background"
	ErrCodeStateUnknown     = "session_state_unknown"
	ErrCodeUntrusted        = "workspace_not_trusted"
	ErrCodeLinkTimeout      = "remote_link_timeout"
	ErrCodeLaunchFailed     = "launch_failed"
	ErrCodeFolderNotFound   = "folder_not_found"
	ErrCodeAgentUnavailable = "agent_unavailable"
	// The AI's background service runs as administrator, so a normal-rights
	// agent can neither use nor stop it (구현 전 테스트 6).
	ErrCodeServiceElevated = "service_elevated"
	// The folder a stopped or history session ran in is gone from the PC,
	// so it cannot be resumed.
	ErrCodeFolderMissing = "session_folder_missing"
)

// BadgeFolderMissing marks a session that can no longer be opened because
// its folder is gone; such sessions list no open modes.
const BadgeFolderMissing = "folder_missing"

// Fail builds an ActionError.
func Fail(code, detail string) error { return &ActionError{Code: code, Detail: detail} }

// ErrorCode returns the ActionError code of err, or "".
func ErrorCode(err error) string {
	var ae *ActionError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}
