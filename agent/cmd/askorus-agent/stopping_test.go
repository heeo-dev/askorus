package main

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// R-174 (#70): a stop that does not finish ends the process, with the
// code of the stop that began first. One that finishes does not.
func TestStopHasALimit(t *testing.T) {
	if stopLimit >= 30*time.Second || stopLimit <= requestsWait {
		t.Fatalf("stopLimit %v must be under the 30 s shutdown waits and over the %v requests get", stopLimit, requestsWait)
	}
	ended := make(chan int, 4)
	newBound := func() *stopBound {
		return &stopBound{limit: 30 * time.Millisecond, end: func(code int) { ended <- code }}
	}
	wait := func(want int) {
		t.Helper()
		select {
		case code := <-ended:
			if code != want {
				t.Errorf("ended with code %d, want %d", code, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("a stop that hangs was not ended")
		}
	}
	none := func() {
		t.Helper()
		select {
		case code := <-ended:
			t.Fatalf("ended with code %d, want not at all", code)
		case <-time.After(120 * time.Millisecond):
		}
	}

	b := newBound()
	none() // nothing began
	b.begin(1)
	b.begin(0)
	wait(1)

	b = newBound()
	b.begin(0)
	b.finish()
	none()
	b.begin(0) // after serve is done
	none()

	// Asked to stop while serve still starts: the clock runs from then.
	for _, which := range []string{"shutdown", "update", "Ctrl+C"} {
		stop, handedOver := make(chan struct{}), make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		b = newBound()
		b.beginWhenAsked(ctx, stop, handedOver)
		none()
		switch which {
		case "shutdown":
			close(stop)
		case "update":
			close(handedOver)
		default:
			cancel()
		}
		wait(0)
		cancel()
	}
}

// R-176 (#70): a request that outlasts the wait is cut off and is no
// failure of the stop. As one, it ended serve with exit code 1 and systemd
// started the agent again after shutdown.
func TestStopCutsOffRequestsThatLast(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	began, never := make(chan struct{}), make(chan struct{})
	defer close(never)
	srv := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(began)
		<-never
	})}
	go srv.Serve(ln)
	cutOff := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err == nil {
			resp.Body.Close()
		}
		cutOff <- err
	}()
	<-began
	var log bytes.Buffer
	stopHTTP(srv, 50*time.Millisecond, slog.New(slog.NewTextHandler(&log, nil)))
	select {
	case err := <-cutOff:
		if err == nil {
			t.Error("the request got an answer")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the request was not cut off")
	}
	if !strings.Contains(log.String(), "requests cut off by the stop") {
		t.Errorf("log: %q", log.String())
	}
}

// R-176: only the unit's serve takes "already runs" for a success. A serve
// a person runs in a terminal, or one `start` runs, still says the error.
func TestStartedByService(t *testing.T) {
	for _, c := range []struct {
		invocation, detached string
		want                 bool
	}{{"abc", "1", true}, {"abc", "", false}, {"", "1", false}, {"", "", false}} {
		t.Setenv("INVOCATION_ID", c.invocation)
		t.Setenv(detachedEnv, c.detached)
		if got := startedByService(); got != c.want {
			t.Errorf("INVOCATION_ID=%q %s=%q: %v", c.invocation, detachedEnv, c.detached, got)
		}
	}
}

// R-181: the limit holds although tidying up (the log, the files) hangs.
func TestEndProcessDoesNotWaitForAStuckLog(t *testing.T) {
	stuck := make(chan struct{})
	defer close(stuck)
	exited := make(chan int, 1)
	began := time.Now()
	endProcess(1, func() { <-stuck }, func(code int) { exited <- code })
	if code := <-exited; code != 1 || time.Since(began) > tidyWait+time.Second {
		t.Errorf("exit code %d after %v", code, time.Since(began))
	}
	if stopLimit+tidyWait >= 30*time.Second {
		t.Error("the limit and the tidying are not inside shutdown's 30 seconds")
	}
}

// R-182: the log is closed inside the limit, so a close that hangs is
// ended too, and one that returns ends the limit.
func TestStopLimitCoversClosingTheLog(t *testing.T) {
	ended := make(chan int, 2)
	stuck := make(chan struct{})
	b := &stopBound{limit: 30 * time.Millisecond, end: func(code int) { ended <- code }}
	b.begin(0)
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.finishAfter(func() { <-stuck })
	}()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Error("a log that does not close was outside the limit")
	}
	close(stuck)
	<-done

	b = &stopBound{limit: 100 * time.Millisecond, end: func(code int) { ended <- code }}
	b.begin(0)
	b.finishAfter(func() {})
	select {
	case <-ended:
		t.Error("a stop that finished was ended")
	case <-time.After(250 * time.Millisecond):
	}
}
