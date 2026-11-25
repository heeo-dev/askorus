package core

import (
	"context"
	"sync"
	"time"
)

// Kept keeps a value that takes a while to find out, such as what an AI
// program says of its version or its sign-in, and finds it again in the
// background once it is older than TTL. Get gives an older value at once
// and starts a new read; the next Get has it. Only while there is no value
// (none read yet, or Forget dropped it) does Get wait for the read. Such
// checks used to run inside the phone's requests whenever their value had
// aged, and one slow check held up /v1/info (R-185, R-187).
//
// Reads run under the usage reads' context (GoRead), so stopping the agent
// ends them, and one runs at a time; a read so ended keeps nothing. Read
// bounds its own time. A panic in Read keeps the zero value for TTL instead
// of ending the agent.
type Kept[T any] struct {
	TTL  time.Duration
	Read func(ctx context.Context) T

	mu   sync.Mutex
	val  T
	has  bool      // val was read and not dropped since
	at   time.Time // when val was read
	done chan struct{}
	gen  int // bumped by Forget: a read that began before it does not count
}

// start begins a read unless one runs, and returns the channel closed when
// it ends; ok is false when no read can start (the agent is stopping). The
// caller holds mu.
func (k *Kept[T]) start() (done chan struct{}, ok bool) {
	if k.done != nil {
		return k.done, true
	}
	done = make(chan struct{})
	gen, read := k.gen, k.Read
	if !GoRead(func(ctx context.Context) {
		var v T
		func() {
			defer func() { _ = recover() }()
			v = read(ctx)
		}()
		k.mu.Lock()
		// A read the agent ended (StopUsageReads, EndUsageReads) says
		// nothing of the AI: it is not kept as its value for TTL.
		if gen == k.gen && ctx.Err() == nil {
			k.val, k.has, k.at = v, true, time.Now()
		}
		k.done = nil
		k.mu.Unlock()
		close(done)
	}) {
		return nil, false
	}
	k.done = done
	return done, true
}

// Get returns the value: a fresh one at once, an older one at once while a
// new read starts, and with none it waits for the read, or until ctx ends
// (ok is false then, and when the agent is stopping).
func (k *Kept[T]) Get(ctx context.Context) (v T, ok bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	for {
		if k.has {
			if time.Since(k.at) >= k.TTL {
				k.start()
			}
			return k.val, true
		}
		done, started := k.start()
		if !started {
			return v, false
		}
		k.mu.Unlock()
		select {
		case <-done:
			k.mu.Lock()
		case <-ctx.Done():
			k.mu.Lock()
			return v, false
		}
	}
}

// Forget drops the value (an ask has just shown it wrong): the next Get
// waits for a new read, which starts now (or, when one runs, after it: that
// one began before Forget and does not count).
func (k *Kept[T]) Forget() {
	k.mu.Lock()
	defer k.mu.Unlock()
	var zero T
	k.val, k.has, k.at = zero, false, time.Time{}
	k.gen++
	k.start()
}
