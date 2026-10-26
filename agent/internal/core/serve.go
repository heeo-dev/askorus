package core

import "time"

// Server is implemented by adapters that can keep a folder open for the
// official app, so new sessions can be made there from the phone (F10).
// The agent keeps the wanted state in its settings and calls these to
// match it.
type Server interface {
	// StartServer starts serving folder, or restarts it when opts changed.
	// It returns at once; ServerStatus reports progress.
	StartServer(folder string, opts ServeOptions) error
	// StopServer stops serving folder and waits for the server to end.
	StopServer(folder string) error
	// ServerStatus reports one folder.
	ServerStatus(folder string) ServeStatus
	// ServerFolders lists the folders this agent serves.
	ServerFolders() []string
	// StopAllServers stops every server, for agent shutdown.
	StopAllServers()
}

// ServeOptions configures one folder server.
type ServeOptions struct {
	Name     string // shown in the official app's PC list
	Spawn    string // "same-dir" or "worktree"
	Capacity int    // concurrent sessions
}

// Folder server states.
const (
	ServeOff       = "off"
	ServeStarting  = "starting"
	ServeRunning   = "running"
	ServeFailed    = "failed"    // exited; restarting after a pause
	ServeUntrusted = "untrusted" // the folder is not trusted yet; not retried
	ServeExternal  = "external"  // someone else's server runs here; left alone
)

// ServeStatus is what the app shows for a folder server.
type ServeStatus struct {
	State         string    `json:"state"`
	Since         time.Time `json:"since,omitempty"`
	EnvironmentID string    `json:"environmentId,omitempty"`
	Sessions      int       `json:"sessions"`
	Capacity      int       `json:"capacity,omitempty"`
	Restarts      int       `json:"restarts,omitempty"`
	Error         string    `json:"error,omitempty"`
}
