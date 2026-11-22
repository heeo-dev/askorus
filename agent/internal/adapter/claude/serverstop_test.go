package claude

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// heldProcess is a folder server whose screen never ends (a child holds
// the terminal) and that may not even end when it is killed.
type heldProcess struct {
	screen *io.PipeReader
	holder *io.PipeWriter
	end    chan struct{}
	once   sync.Once

	endsOnInterrupt, endsOnKill bool

	mu          sync.Mutex
	interrupted bool
}

func newHeldProcess(onInterrupt, onKill bool) *heldProcess {
	r, w := io.Pipe()
	return &heldProcess{screen: r, holder: w, end: make(chan struct{}), endsOnInterrupt: onInterrupt, endsOnKill: onKill}
}

func (p *heldProcess) PID() int          { return 4242 }
func (p *heldProcess) Output() io.Reader { return p.screen }
func (p *heldProcess) Close()            {} // does not end the screen
func (p *heldProcess) Wait() error       { <-p.end; return nil }
func (p *heldProcess) ended()            { p.once.Do(func() { close(p.end) }) }

func (p *heldProcess) Interrupt() error {
	p.mu.Lock()
	p.interrupted = true
	p.mu.Unlock()
	if p.endsOnInterrupt {
		p.ended()
	}
	return nil
}

func (p *heldProcess) Kill() error {
	if p.endsOnKill {
		p.ended()
	}
	return nil
}

func (p *heldProcess) wasInterrupted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.interrupted
}

// release lets the test's goroutines go.
func (p *heldProcess) release() {
	p.ended()
	p.holder.Close()
}

func serversOf(start procStarter) *servers {
	return &servers{
		exe:     func() (string, error) { return "claude", nil },
		start:   start,
		pointer: func(string) (int, string, bool) { return 0, "", false },
		backoff: []time.Duration{50 * time.Millisecond},
	}
}

func stopsWithin(t *testing.T, ss *servers, limit time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ss.StopAllServers()
	}()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("stopping the folder servers took over %v", limit)
	}
}

// R-174 (#70): stopping never waits for ever for a folder server, whether
// its screen has no end or the process itself does not end.
func TestServerStopHasALimit(t *testing.T) {
	for _, c := range []struct {
		name            string
		interrupt, kill bool
		limit           time.Duration
	}{
		{"the screen has no end", true, true, screenEndWait + 2*time.Second},
		{"not even killed does it end", false, false, serverStopWait + killedWait + 2*time.Second},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := newHeldProcess(c.interrupt, c.kill)
			defer p.release()
			started := make(chan struct{})
			var once sync.Once
			ss := serversOf(func(string, string, []string) (serverProcess, error) {
				once.Do(func() { close(started) })
				return p, nil
			})
			if err := ss.StartServer(t.TempDir(), core.ServeOptions{Spawn: "same-dir", Capacity: 1}); err != nil {
				t.Fatal(err)
			}
			<-started
			stopsWithin(t, ss, c.limit)
			if len(ss.ServerFolders()) != 0 {
				t.Error("the stopped folder is still listed")
			}
		})
	}
}

// R-174: a stop that comes while the server is being started stops that
// server too. It used to wait for a process nobody told to end.
func TestServerStopWhileStarting(t *testing.T) {
	p := newHeldProcess(true, true)
	defer p.release()
	starting := make(chan struct{})
	ss := serversOf(func(string, string, []string) (serverProcess, error) {
		close(starting)
		time.Sleep(300 * time.Millisecond)
		return p, nil
	})
	if err := ss.StartServer(t.TempDir(), core.ServeOptions{Spawn: "same-dir", Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	<-starting
	stopsWithin(t, ss, screenEndWait+3*time.Second)
	if !p.wasInterrupted() {
		t.Error("the server that was starting was not stopped")
	}
}
