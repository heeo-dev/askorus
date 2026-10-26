// Package jobs runs slow actions (opening or starting a session takes
// 10–20 s) in the background. The phone gets a job id at once and asks
// for the result, so leaving the screen or losing the connection for a
// moment does not lose the outcome. The PC finishes the work either way.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// State is where a job is.
type State string

const (
	Running State = "running"
	Done    State = "done"
	Failed  State = "failed"
)

const (
	// RunLimit caps one job.
	RunLimit = 2 * time.Minute
	// KeepFor is how long a finished job can still be read.
	KeepFor = 10 * time.Minute
	// maxJobs bounds memory; the oldest finished jobs go first.
	maxJobs = 200
)

// Job is a snapshot of one job.
type Job struct {
	ID         string    `json:"jobId"`
	Kind       string    `json:"kind"`             // e.g. "open", "start"
	Target     string    `json:"target,omitempty"` // e.g. the session id
	State      State     `json:"status"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
	Result     any       `json:"result,omitempty"`
	Err        error     `json:"-"`

	owner string
}

// Manager keeps jobs in memory. Jobs do not survive an agent restart.
type Manager struct {
	Now func() time.Time

	mu   sync.Mutex
	jobs map[string]*Job
}

// NewManager returns an empty manager.
func NewManager() *Manager { return &Manager{jobs: map[string]*Job{}} }

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Start runs fn in the background and returns the job. If the same owner
// already has a running job of this kind for this target, that job is
// returned instead, so a phone that retries does not start the work twice.
func (m *Manager) Start(owner, kind, target string, fn func(ctx context.Context) (any, error)) Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	if target != "" {
		for _, j := range m.jobs {
			if j.State == Running && j.owner == owner && j.Kind == kind && j.Target == target {
				return *j
			}
		}
	}
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	j := &Job{ID: hex.EncodeToString(raw), Kind: kind, Target: target, State: Running, StartedAt: m.now().UTC(), owner: owner}
	m.jobs[j.ID] = j
	go m.run(j, fn)
	return *j
}

func (m *Manager) run(j *Job, fn func(ctx context.Context) (any, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), RunLimit)
	defer cancel()
	res, err := fn(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	j.FinishedAt = m.now().UTC()
	if err != nil {
		j.State, j.Err = Failed, err
		return
	}
	j.State, j.Result = Done, res
}

// Get returns the job if it exists and belongs to owner.
func (m *Manager) Get(owner, id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	j, ok := m.jobs[id]
	if !ok || j.owner != owner {
		return Job{}, false
	}
	return *j, true
}

// Wait blocks until the job is finished or ctx ends, for tests and the CLI.
func (m *Manager) Wait(ctx context.Context, owner, id string) (Job, bool) {
	for {
		j, ok := m.Get(owner, id)
		if !ok || j.State != Running {
			return j, ok
		}
		select {
		case <-ctx.Done():
			return j, true
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// prune drops old finished jobs. Callers hold mu.
func (m *Manager) prune() {
	now := m.now()
	for id, j := range m.jobs {
		if j.State != Running && now.Sub(j.FinishedAt) > KeepFor {
			delete(m.jobs, id)
		}
	}
	for len(m.jobs) > maxJobs {
		var oldest *Job
		for _, j := range m.jobs {
			if j.State != Running && (oldest == nil || j.FinishedAt.Before(oldest.FinishedAt)) {
				oldest = j
			}
		}
		if oldest == nil {
			return
		}
		delete(m.jobs, oldest.ID)
	}
}
