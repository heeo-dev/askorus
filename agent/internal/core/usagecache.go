package core

import (
	"context"
	"slices"
	"sync"
	"time"
)

// UsageCache keeps what an AI says is left of its plan for a while (#20).
// Reading it asks the AI's company and takes a second or two, and Info is
// asked for every /v1/info and before every question, so Get never waits:
// it returns what it has (nothing at first) and starts a new read in the
// background when that is older than TTL. The next Info has it.
type UsageCache struct {
	TTL  time.Duration
	Read func(context.Context) []UsageGroup // bounds its own time

	mu     sync.Mutex
	groups []UsageGroup
	at     time.Time
	done   chan struct{} // set while a read runs
}

// Get returns the kept usage, and starts a read when it is missing or old.
func (c *UsageCache) Get() []UsageGroup {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done == nil && (c.at.IsZero() || time.Since(c.at) >= c.TTL) {
		done := make(chan struct{})
		c.done = done
		go func() {
			g := c.Read(context.Background())
			c.mu.Lock()
			c.groups, c.at, c.done = g, time.Now(), nil
			c.mu.Unlock()
			close(done)
		}()
	}
	return slices.Clone(c.groups)
}

// Wait waits for a read that is running, or until ctx ends.
func (c *UsageCache) Wait(ctx context.Context) {
	c.mu.Lock()
	done := c.done
	c.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// Forget drops what is kept, so the next Get reads again and returns
// nothing until then (the AI signed out, or said its limit is used up).
func (c *UsageCache) Forget() {
	c.mu.Lock()
	c.groups, c.at = nil, time.Time{}
	c.mu.Unlock()
}
