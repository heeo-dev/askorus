package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// R-133 (#35 review): GitHub is not asked over and over. A fresh check
// within a minute answers from the last ask, and an ask that failed is not
// tried again for half an hour, fresh or not.
func TestCheckerDoesNotAskOverAndOver(t *testing.T) {
	g := newGitHub(t)
	ctx := context.Background()
	now := t0
	c := g.checker(t.TempDir(), "0.5.0", now)
	c.Now = func() time.Time { return now }
	if _, err := c.Check(ctx, true); err != nil {
		t.Fatal(err)
	}
	hits := g.hits.Load()
	now = now.Add(30 * time.Second)
	if f, err := c.Check(ctx, true); err != nil || f.Release == nil || g.hits.Load() != hits {
		t.Fatalf("fresh within a minute: %+v, %v, %d requests", f, err, g.hits.Load()-hits)
	}
	now = now.Add(time.Minute)
	if _, err := c.Check(ctx, true); err != nil || g.hits.Load() == hits {
		t.Fatalf("fresh after a minute did not ask: %v", err)
	}

	// GitHub answers nothing useful: the failure is remembered.
	delete(g.files, ReleaseFile)
	dir := t.TempDir()
	c = g.checker(dir, "0.5.0", now)
	c.Now = func() time.Time { return now }
	if _, err := c.Check(ctx, false); !errors.Is(err, ErrNoReleaseInfo) {
		t.Fatalf("first: %v", err)
	}
	hits = g.hits.Load()
	for _, fresh := range []bool{false, true} {
		now = now.Add(5 * time.Minute)
		if _, err := c.Check(ctx, fresh); !errors.Is(err, ErrNoReleaseInfo) || g.hits.Load() != hits {
			t.Fatalf("fresh=%v after a failure: %v, %d requests", fresh, err, g.hits.Load()-hits)
		}
	}
	now = now.Add(30 * time.Minute)
	if _, err := c.Check(ctx, false); !errors.Is(err, ErrNoReleaseInfo) || g.hits.Load() == hits {
		t.Fatalf("after half an hour it did not ask again: %v", err)
	}
}

// R-136 (#35 review): the phone's update and the terminal's never run at
// once. A lock whose process ended is taken over; the lock can be handed
// to the new program and is let go only by its holder.
func TestUpdateLock(t *testing.T) {
	dir := t.TempDir()
	alive := map[int]bool{100: true, 200: true}
	isAlive := func(pid int) bool { return alive[pid] }
	unlock, err := Lock(dir, 100, isAlive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(dir, 200, isAlive); !errors.Is(err, ErrBusy) {
		t.Fatalf("a second lock: %v", err)
	}
	if err := TransferLock(dir, 999, 200); !errors.Is(err, ErrBusy) {
		t.Fatalf("a hand-over by one that does not hold it: %v", err)
	}
	if err := TransferLock(dir, 100, 300); err != nil {
		t.Fatal(err)
	}
	unlock() // 100 no longer holds it: nothing happens
	if _, err := os.Stat(filepath.Join(dir, lockFile)); err != nil {
		t.Fatal("the old holder let go of a lock it handed over")
	}
	// 300 ended without letting go: the next one takes it over.
	if _, err := Lock(dir, 200, isAlive); err != nil {
		t.Fatalf("a lock left by an ended process: %v", err)
	}
	ReleaseLock(dir, 200)
	if _, err := os.Stat(filepath.Join(dir, lockFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("ReleaseLock left the lock")
	}
	// A lock file without a pid (cut short) is not a live holder.
	os.WriteFile(filepath.Join(dir, lockFile), nil, 0o600)
	if _, err := Lock(dir, 100, isAlive); err != nil {
		t.Fatalf("an empty lock file: %v", err)
	}
}

// R-140 (#35 review): an update.json that cannot be read stops the check
// instead of forgetting the release key number seen; a deleted one starts
// over from the build's floor, which refuses a replaced key.
func TestBrokenStateStopsAndTheFloorHolds(t *testing.T) {
	g := newGitHub(t)
	ctx := context.Background()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, stateFile), []byte("{not json"), 0o600)
	before := g.hits.Load()
	if _, err := g.checker(dir, "0.5.0", t0).Check(ctx, true); !errors.Is(err, ErrBadState) {
		t.Fatalf("a broken update.json: %v", err)
	}
	if g.hits.Load() != before {
		t.Fatal("a broken update.json asked GitHub")
	}
	os.WriteFile(filepath.Join(dir, stateFile), []byte(`{"keySeq":-1}`), 0o600)
	if _, err := g.checker(dir, "0.5.0", t0).Check(ctx, true); !errors.Is(err, ErrBadState) {
		t.Fatalf("a negative key number: %v", err)
	}

	saved := MinKeySeq
	MinKeySeq = 2
	t.Cleanup(func() { MinKeySeq = saved })
	if _, err := g.checker(t.TempDir(), "0.5.0", t0).Check(ctx, true); !errors.Is(err, ErrOldKey) {
		t.Fatalf("key 1 with the build's floor at 2: %v", err)
	}
}

// R-145 (2ho's release review, 0.5.0): many updates starting at once over a
// lock left by a process that ended: one takes it, never two, and the lock
// file is never seen without a pid.
func TestUpdateLockTakenOnce(t *testing.T) {
	for round := 0; round < 20; round++ {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, lockFile), []byte("7\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		alive := func(pid int) bool { return pid != 7 }
		const n = 16
		got := make(chan error, n)
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			go func(pid int) {
				<-start
				_, err := Lock(dir, pid, alive)
				got <- err
			}(1000 + i)
		}
		close(start)
		won := 0
		for i := 0; i < n; i++ {
			if err := <-got; err == nil {
				won++
			} else if !errors.Is(err, ErrBusy) {
				t.Fatalf("lock: %v", err)
			}
		}
		if won > 1 {
			t.Fatalf("round %d: %d updates took the lock", round, won)
		}
		if h := lockHolder(filepath.Join(dir, lockFile)); won == 1 && h < 1000 {
			t.Fatalf("round %d: the lock holds %d", round, h)
		}
	}
}
