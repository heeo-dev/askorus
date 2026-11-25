package core

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// gate is a Read whose calls wait until open is called, counting them.
type gate struct {
	calls atomic.Int32
	ch    chan struct{}
	val   atomic.Int32
}

func newGate(v int32) *gate {
	g := &gate{ch: make(chan struct{})}
	g.val.Store(v)
	return g
}

func (g *gate) read(ctx context.Context) int32 {
	g.calls.Add(1)
	select {
	case <-g.ch:
	case <-ctx.Done():
	}
	return g.val.Load()
}

func (g *gate) open() { close(g.ch) }

func waitIdle(t *testing.T, k *Kept[int32]) {
	t.Helper()
	for range 400 {
		k.mu.Lock()
		idle := k.done == nil
		k.mu.Unlock()
		if idle {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the read never ended")
}

// R-187: the first Get waits for the read; within TTL nothing is read again.
func TestKeptFirstGetWaits(t *testing.T) {
	g := newGate(1)
	k := &Kept[int32]{TTL: time.Hour, Read: g.read}
	go func() { time.Sleep(50 * time.Millisecond); g.open() }()
	if v, ok := k.Get(context.Background()); !ok || v != 1 {
		t.Fatalf("first get = %d %v", v, ok)
	}
	if v, _ := k.Get(context.Background()); v != 1 || g.calls.Load() != 1 {
		t.Errorf("second get = %d after %d reads, want 1 read", v, g.calls.Load())
	}
}

// R-187: an older value is given at once while a new read runs (one read
// for many calls); the next Get has the new value.
func TestKeptOldValueAtOnce(t *testing.T) {
	g := newGate(1)
	g.open()
	k := &Kept[int32]{TTL: time.Hour, Read: g.read}
	k.Get(context.Background())
	slow := newGate(2)
	k.mu.Lock()
	k.Read, k.at = slow.read, time.Now().Add(-2*time.Hour)
	k.mu.Unlock()
	start := time.Now()
	for range 3 {
		if v, ok := k.Get(context.Background()); !ok || v != 1 {
			t.Errorf("get during the read = %d %v, want the old 1", v, ok)
		}
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("gets waited %v", d)
	}
	slow.open()
	waitIdle(t, k)
	if n := slow.calls.Load(); n != 1 {
		t.Errorf("%d reads ran, want 1", n)
	}
	if v, _ := k.Get(context.Background()); v != 2 {
		t.Errorf("get after the read = %d, want 2", v)
	}
}

// R-187: after Forget the next Get waits for a new read; a read that began
// before Forget does not count.
func TestKeptForget(t *testing.T) {
	g := newGate(1)
	g.open()
	k := &Kept[int32]{TTL: time.Hour, Read: g.read}
	k.Get(context.Background())

	early := newGate(10)
	k.mu.Lock()
	k.Read, k.at = early.read, time.Now().Add(-2*time.Hour)
	k.mu.Unlock()
	k.Get(context.Background()) // starts the early read
	k.Forget()
	late := newGate(20)
	late.open()
	k.mu.Lock()
	k.Read = late.read
	k.mu.Unlock()
	go func() { time.Sleep(50 * time.Millisecond); early.open() }()
	if v, ok := k.Get(context.Background()); !ok || v != 20 {
		t.Errorf("get after forget = %d %v, want 20 from the read after it", v, ok)
	}
}

// R-187: a Get that gives up (the phone's request went away) leaves the read
// running, and the next Get has its value.
func TestKeptGetGivesUp(t *testing.T) {
	g := newGate(5)
	k := &Kept[int32]{TTL: time.Hour, Read: g.read}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, ok := k.Get(ctx); ok {
		t.Error("a get whose context ended says ok")
	}
	g.open()
	if v, ok := k.Get(context.Background()); !ok || v != 5 || g.calls.Load() != 1 {
		t.Errorf("next get = %d %v after %d reads", v, ok, g.calls.Load())
	}
}

// R-187 (2호's 0.6.4 review): a read the agent ended (EndUsageReads, as a
// one-shot command does) is not kept as the value; the next Get reads
// again under the new reads.
func TestKeptReadEndedKeepsNothing(t *testing.T) {
	g := newGate(7)
	k := &Kept[int32]{TTL: time.Hour, Read: func(ctx context.Context) int32 {
		v := g.read(ctx)
		if ctx.Err() != nil {
			return -1 // what an ended read would say
		}
		return v
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	k.Get(ctx) // starts the read, gives up waiting
	EndUsageReads(2 * time.Second)
	waitIdle(t, k)
	g.open()
	if v, ok := k.Get(context.Background()); !ok || v != 7 {
		t.Errorf("get after the ended read = %d %v, want 7 from a new read", v, ok)
	}
}

// R-187: once the agent stops (StopUsageReads), a Get with no value starts
// nothing and says so; one with a value still gives it.
func TestKeptAfterStop(t *testing.T) {
	t.Cleanup(func() { EndUsageReads(time.Second) }) // new reads for the other tests
	kept := &Kept[int32]{TTL: time.Hour, Read: func(context.Context) int32 { return 3 }}
	kept.Get(context.Background())
	StopUsageReads(time.Second)
	none := &Kept[int32]{TTL: time.Hour, Read: func(context.Context) int32 { return 4 }}
	if _, ok := none.Get(context.Background()); ok {
		t.Error("a get after the stop says ok with nothing read")
	}
	if v, ok := kept.Get(context.Background()); !ok || v != 3 {
		t.Errorf("kept value after the stop = %d %v", v, ok)
	}
}

// R-187: a panic in Read keeps the zero value instead of ending the agent.
func TestKeptReadPanics(t *testing.T) {
	k := &Kept[int32]{TTL: time.Hour, Read: func(context.Context) int32 { panic("boom") }}
	if v, ok := k.Get(context.Background()); !ok || v != 0 {
		t.Errorf("get = %d %v, want the zero value", v, ok)
	}
}
