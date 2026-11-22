package main

// How serve ends (#70): in bounded time, and as a success when it was
// asked to. A service manager starts an agent again that ends with an
// error (systemd's Restart=on-failure), so a stop that someone asked for
// must never end as one.

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

// stopLimit bounds serve's whole stop, the node's Close included. It is
// shorter than the 30 seconds shutdown waits.
const stopLimit = 25 * time.Second

// requestsWait is how long a stop lets requests in flight finish.
const requestsWait = 5 * time.Second

// stopBound ends the process when a stop that began does not finish.
type stopBound struct {
	limit time.Duration
	end   func(code int)

	mu       sync.Mutex
	timer    *time.Timer
	finished bool
}

// begin starts the clock; code is the exit code if it runs out. Only the
// first begin counts.
func (b *stopBound) begin(code int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.timer == nil && !b.finished {
		b.timer = time.AfterFunc(b.limit, func() { b.end(code) })
	}
}

// finish takes the clock away: serve is done.
func (b *stopBound) finish() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.finished = true
	if b.timer != nil {
		b.timer.Stop()
	}
}

// beginWhenAsked begins when a stop is asked for, also while serve is
// still starting. ctx is serve's own, so the wait ends with it.
func (b *stopBound) beginWhenAsked(ctx context.Context, stop, handedOver <-chan struct{}) {
	go func() {
		select {
		case <-ctx.Done():
		case <-stop:
		case <-handedOver:
		}
		b.begin(0)
	}()
}

// stopHTTP lets requests in flight finish for wait and cuts off the rest.
// A request that is cut off is no failure of the stop.
func stopHTTP(srv *http.Server, wait time.Duration, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Warn("requests cut off by the stop", "err", err.Error())
		_ = srv.Close()
	}
}

// startedByService reports whether systemd started this serve from the
// unit of `autostart on` (INVOCATION_ID is systemd's, the mark the unit's).
func startedByService() bool {
	return os.Getenv("INVOCATION_ID") != "" && os.Getenv(detachedEnv) == "1"
}
