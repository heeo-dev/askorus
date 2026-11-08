package compares

// Tests added after the S3 review (11-01, R-30) that still hold since the
// judging went (C6d, #24): a cancel while the round takes the answers, a
// Retry after Close, and the twin Start.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/runs"
)

// endedLogs counts the "comparison ended" lines of a Manager's log.
func endedLogs(buf *syncBuffer) int {
	return strings.Count(buf.String(), `"msg":"comparison ended"`)
}

// planBlock is a fakeAI whose Family of the answer model claude-m waits:
// the round is taking the answers then (Manager.answers).
type planBlock struct {
	*fakeAI
	once             sync.Once
	entered, release chan struct{}
}

func (p *planBlock) Family(model string) string {
	if model == "claude-m" {
		p.once.Do(func() { close(p.entered) })
		<-p.release
	}
	return p.fakeAI.Family(model)
}

// A cancel that lands while the round takes the answers (the phone's '새
// 질문') ends the comparison cancelled, once.
func TestCancelWhilePlanningEndsOnce(t *testing.T) {
	ais := threeAIs()
	pb := &planBlock{fakeAI: newFake("claude", "anthropic"), entered: make(chan struct{}), release: make(chan struct{})}
	ais[0] = pb
	m, buf := newTestManager(t, ais, nil)
	c := startDirect(t, m, threeAnswers("phone", ""))
	select {
	case <-pb.entered:
	case <-time.After(10 * time.Second):
		close(pb.release)
		t.Fatal("the round did not take the answers")
	}
	if got, err := m.Cancel("phone", c.ID); err != nil || got.Status != Running {
		t.Errorf("Cancel = %s %v", got.Status, err)
	}
	close(pb.release)
	c = waitIdle(t, m, "phone", c.ID)
	if c.Status != Failed || c.Error == nil || c.Error.Code != core.ErrCodeCancelled || c.Error.Message != messages[core.ErrCodeCancelled] {
		t.Fatalf("comparison %s %+v", c.Status, c.Error)
	}
	if n := endedLogs(buf); n != 1 {
		t.Errorf("comparison ended logged %d times, want 1:\n%s", n, buf.String())
	}
}

// A Retry after Close of a comparison cancelled while it waited ends it at
// once as cancelled, and once; no round starts.
func TestRetryAfterCloseEndsOnce(t *testing.T) {
	fr := &fakeRuns{}
	m, buf := newTestManager(t, threeAIs(), func(o *Options) { o.Runs = fr })
	fr.set(runs.Run{ID: "run-a", Status: runs.Running})
	id, _ := m.Attach(runSpec("phone", "run-a"))
	if c, err := m.Cancel("phone", id); err != nil || c.Status != Failed {
		t.Fatalf("Cancel = %s %v", c.Status, err)
	}
	m.Close()
	if n := endedLogs(buf); n != 1 {
		t.Fatalf("comparison ended logged %d times before the retry", n)
	}
	fr.set(endedRun("run-a", threeParts()...))
	if _, err := m.Retry("phone", id); err != nil {
		t.Fatal(err)
	}
	c := waitIdle(t, m, "phone", id)
	if n := endedLogs(buf) - 1; n != 1 {
		t.Errorf("retry's round ended %d times, want 1:\n%s", n, buf.String())
	}
	if c.Status != Failed || c.Error == nil || c.Error.Code != core.ErrCodeCancelled || c.Error.Message != msgClosed {
		t.Errorf("comparison %s %+v", c.Status, c.Error)
	}
	if strings.Contains(buf.String(), `"msg":"comparison started"`) {
		t.Errorf("a round started after Close:\n%s", buf.String())
	}
}

// barrierInfo is a fakeAI whose Info waits until want callers are in it,
// so twin Starts both pass the key lookup before either registers.
type barrierInfo struct {
	*fakeAI
	mu   sync.Mutex
	n    int
	want int
	all  chan struct{}
}

func (b *barrierInfo) Info(ctx context.Context) core.AdapterInfo {
	b.mu.Lock()
	if b.n++; b.n == b.want {
		close(b.all)
	}
	b.mu.Unlock()
	select {
	case <-b.all:
	case <-time.After(5 * time.Second):
	}
	return b.fakeAI.Info(ctx)
}

// Two Starts with the same key at once make one comparison.
func TestTwinStart(t *testing.T) {
	ais := threeAIs()
	bi := &barrierInfo{fakeAI: newFake("claude", "anthropic"), want: 2, all: make(chan struct{})}
	ais[0] = bi
	m, buf := newTestManager(t, ais, nil)
	var wg sync.WaitGroup
	ids := make([]string, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := m.Start(threeAnswers("phone", "twin"))
			ids[i], errs[i] = c.ID, err
		}()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || ids[0] == "" || ids[0] != ids[1] {
		t.Fatalf("twin Starts = %q %v, %q %v", ids[0], errs[0], ids[1], errs[1])
	}
	waitIdle(t, m, "phone", ids[0])
	m.mu.Lock()
	n := len(m.byID)
	m.mu.Unlock()
	started := strings.Count(buf.String(), `"msg":"comparison started"`)
	if n != 1 || started != 1 {
		t.Errorf("%d comparisons, %d rounds; want 1 and 1", n, started)
	}
}
