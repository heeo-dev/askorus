package core

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// Get never waits for a read: nothing at first, the read's result after,
// kept for TTL, read again once old; Forget drops it.
func TestUsageCacheNeverWaits(t *testing.T) {
	var reads atomic.Int32
	release := make(chan struct{})
	c := &UsageCache{TTL: time.Hour, Read: func(context.Context) []UsageGroup {
		reads.Add(1)
		<-release
		return []UsageGroup{{Name: "5 hours", Left: 0.72, Window: Window5h}}
	}}
	start := time.Now()
	if g := c.Get(); g != nil {
		t.Fatalf("first get = %+v", g)
	}
	if time.Since(start) > time.Second {
		t.Fatal("get waited for the read")
	}
	c.Get() // a read is running: no second one
	close(release)
	c.Wait(context.Background())
	if g := c.Get(); len(g) != 1 || g[0].Left != 0.72 || g[0].Window != Window5h {
		t.Fatalf("after the read = %+v", g)
	}
	if n := reads.Load(); n != 1 {
		t.Errorf("reads = %d, want 1", n)
	}
	c.Get()[0].Left = 0 // a copy
	if c.Get()[0].Left != 0.72 {
		t.Error("get returned the kept slice itself")
	}
	c.TTL = 0
	c.Get() // old now: a new read
	c.Wait(context.Background())
	if n := reads.Load(); n != 2 {
		t.Errorf("reads = %d, want 2", n)
	}
	c.Forget()
	c.TTL = time.Hour
	if g := c.Get(); g != nil {
		t.Errorf("after forget = %+v", g)
	}
	c.Wait(context.Background())
}

// R-147 (2ho's release review, 0.5.0): stopping ends a read that runs,
// waits for it, and no read starts afterwards.
func TestStopUsageReads(t *testing.T) {
	defer func() { usageReads.Store(newReads()) }()
	started, ended := make(chan struct{}), make(chan struct{})
	var n atomic.Int32
	c := &UsageCache{TTL: time.Hour, Read: func(ctx context.Context) []UsageGroup {
		n.Add(1)
		close(started)
		<-ctx.Done()
		close(ended)
		return nil
	}}
	c.Get()
	<-started
	StopUsageReads(5 * time.Second)
	select {
	case <-ended:
	default:
		t.Fatal("stop returned before the read ended")
	}
	c.TTL = 0
	c.Get()
	time.Sleep(50 * time.Millisecond)
	if got := n.Load(); got != 1 {
		t.Errorf("reads after stop = %d, want 1", got)
	}
}
