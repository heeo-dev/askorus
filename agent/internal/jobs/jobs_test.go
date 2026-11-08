package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func mustStart(t *testing.T, m *Manager, owner, kind, target string, fn func(ctx context.Context) (any, error)) Job {
	t.Helper()
	j, err := m.Start(owner, kind, target, fn)
	if err != nil {
		t.Fatalf("Start(%s, %s, %s): %v", owner, kind, target, err)
	}
	return j
}

func TestRunAndGet(t *testing.T) {
	m := NewManager()
	release := make(chan struct{})
	j := mustStart(t, m, "dev1", "open", "claude:a", func(ctx context.Context) (any, error) {
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
	j := mustStart(t, m, "dev1", "start", "", func(ctx context.Context) (any, error) { return nil, errors.New("boom") })
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
	a := mustStart(t, m, "dev1", "open", "claude:a", fn)
	b := mustStart(t, m, "dev1", "open", "claude:a", fn)
	c := mustStart(t, m, "dev1", "open", "claude:b", fn)
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
	d := mustStart(t, m, "dev1", "open", "claude:a", func(ctx context.Context) (any, error) { return nil, nil })
	if d.ID == a.ID {
		t.Error("finished job reused")
	}
}

func TestFinishedJobsExpire(t *testing.T) {
	m := NewManager()
	now := time.Now()
	m.Now = func() time.Time { return now }
	j := mustStart(t, m, "dev1", "open", "x", func(ctx context.Context) (any, error) { return nil, nil })
	m.Wait(context.Background(), "dev1", j.ID)
	now = now.Add(KeepFor + time.Second)
	if _, ok := m.Get("dev1", j.ID); ok {
		t.Error("old job still readable")
	}
}

// R-88 (security review 11-08): running jobs are capped per device and in
// all, so a device cannot start session after session by changing the name.
func TestRunningJobsAreCapped(t *testing.T) {
	m := NewManager()
	release := make(chan struct{})
	defer close(release)
	var runs atomic.Int32
	block := func(ctx context.Context) (any, error) { runs.Add(1); <-release; return nil, nil }
	var first Job
	for i := 0; i < MaxRunningPerOwner; i++ {
		j := mustStart(t, m, "dev1", "start", fmt.Sprintf("claude|f|s-%d", i), block)
		if i == 0 {
			first = j
		}
	}
	if _, err := m.Start("dev1", "start", "claude|f|s-new", block); !errors.Is(err, ErrBusy) {
		t.Fatalf("one device past its cap: err %v, want ErrBusy", err)
	}
	if _, err := m.Start("dev1", "open", "claude:a", block); !errors.Is(err, ErrBusy) {
		t.Fatalf("another kind past the cap: err %v, want ErrBusy", err)
	}
	// A retry of running work still joins it at the cap.
	if j, err := m.Start("dev1", "start", "claude|f|s-0", block); err != nil || j.ID != first.ID {
		t.Fatalf("retry at the cap = %s %v, want job %s", j.ID, err, first.ID)
	}
	// Other devices fill up to the cap for all.
	for i := MaxRunningPerOwner; i < MaxRunning; i++ {
		mustStart(t, m, fmt.Sprintf("dev%d", i), "open", "claude:b", block)
	}
	if _, err := m.Start("dev-last", "open", "claude:c", block); !errors.Is(err, ErrBusy) {
		t.Fatalf("past the cap for all: err %v, want ErrBusy", err)
	}
	if n := int(runs.Load()); n > MaxRunning {
		t.Errorf("%d jobs ran, cap %d", n, MaxRunning)
	}
}

// A finished job frees its place.
func TestFinishedJobFreesItsPlace(t *testing.T) {
	m := NewManager()
	release := make(chan struct{})
	defer close(release)
	block := func(ctx context.Context) (any, error) { <-release; return nil, nil }
	for i := 0; i < MaxRunningPerOwner-1; i++ {
		mustStart(t, m, "dev1", "start", fmt.Sprintf("s-%d", i), block)
	}
	quick := mustStart(t, m, "dev1", "start", "quick", func(ctx context.Context) (any, error) { return nil, nil })
	m.Wait(context.Background(), "dev1", quick.ID)
	mustStart(t, m, "dev1", "start", "after", block)
}
