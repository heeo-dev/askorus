package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunAndGet(t *testing.T) {
	m := NewManager()
	release := make(chan struct{})
	j := m.Start("dev1", "open", "claude:a", func(ctx context.Context) (any, error) {
		<-release
		return "link", nil
	})
	if j.State != Running || len(j.ID) != 32 {
		t.Fatalf("started = %+v", j)
	}
	if got, ok := m.Get("dev1", j.ID); !ok || got.State != Running {
		t.Errorf("while running = %+v %v", got, ok)
	}
	if _, ok := m.Get("dev2", j.ID); ok {
		t.Error("another device can read the job")
	}
	close(release)
	got, _ := m.Wait(context.Background(), "dev1", j.ID)
	if got.State != Done || got.Result != "link" || got.FinishedAt.IsZero() {
		t.Errorf("done = %+v", got)
	}
}

func TestFailure(t *testing.T) {
	m := NewManager()
	j := m.Start("dev1", "start", "", func(ctx context.Context) (any, error) { return nil, errors.New("boom") })
	got, _ := m.Wait(context.Background(), "dev1", j.ID)
	if got.State != Failed || got.Err == nil {
		t.Errorf("failed = %+v", got)
	}
}

// A retry while the same work runs gets the same job, not a second run.
func TestSameTargetJoinsRunningJob(t *testing.T) {
	m := NewManager()
	var runs atomic.Int32
	release := make(chan struct{})
	fn := func(ctx context.Context) (any, error) { runs.Add(1); <-release; return nil, nil }
	a := m.Start("dev1", "open", "claude:a", fn)
	b := m.Start("dev1", "open", "claude:a", fn)
	c := m.Start("dev1", "open", "claude:b", fn)
	if a.ID != b.ID || a.ID == c.ID {
		t.Errorf("ids = %s %s %s", a.ID, b.ID, c.ID)
	}
	close(release)
	m.Wait(context.Background(), "dev1", a.ID)
	m.Wait(context.Background(), "dev1", c.ID)
	if n := runs.Load(); n != 2 {
		t.Errorf("ran %d times, want 2", n)
	}
	// Once finished, a new request starts new work.
	d := m.Start("dev1", "open", "claude:a", func(ctx context.Context) (any, error) { return nil, nil })
	if d.ID == a.ID {
		t.Error("finished job reused")
	}
}

func TestFinishedJobsExpire(t *testing.T) {
	m := NewManager()
	now := time.Now()
	m.Now = func() time.Time { return now }
	j := m.Start("dev1", "open", "x", func(ctx context.Context) (any, error) { return nil, nil })
	m.Wait(context.Background(), "dev1", j.ID)
	now = now.Add(KeepFor + time.Second)
	if _, ok := m.Get("dev1", j.ID); ok {
		t.Error("old job still readable")
	}
}
