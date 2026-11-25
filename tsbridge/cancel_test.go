package tsbridge

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

// #76 (2026-11-25): the app gave up on a request after its own 15 s, and
// the request went on in the node until its 90 s and was thrown away. A
// request runs under the app's id while it runs; Cancel ends it.
func TestCancelEndsTheRequestWithThatID(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forget := track("req-1", cancel)
	Cancel("other") // another id: nothing
	if ctx.Err() != nil {
		t.Fatal("another id ended the request")
	}
	Cancel("req-1")
	if ctx.Err() == nil {
		t.Fatal("Cancel did not end the request")
	}
	forget()
	runningMu.Lock()
	n := len(running)
	runningMu.Unlock()
	if n != 0 {
		t.Errorf("%d requests still kept after they ended", n)
	}

	// No id, or one too long, is not kept.
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	for _, id := range []string{"", strings.Repeat("x", maxRequestID+1)} {
		forget := track(id, cancel2)
		Cancel(id)
		forget()
	}
	if ctx2.Err() != nil {
		t.Error("a request without a kept id was ended")
	}
}

// #76, 2호's review: the app may give up while its call is still on the
// way to the node; the cancel comes before the request begins and is kept,
// so the request ends as it begins instead of running to its limit.
func TestCancelBeforeTheRequestBegins(t *testing.T) {
	Cancel("req-early")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forget := track("req-early", cancel)
	defer forget()
	if ctx.Err() == nil {
		t.Fatal("a request whose cancel came first went on")
	}
	runningMu.Lock()
	_, kept := early["req-early"]
	_, runs := running["req-early"]
	runningMu.Unlock()
	if kept || runs {
		t.Errorf("early cancel kept %v, request kept %v after it began", kept, runs)
	}
	// Early cancels are bounded.
	for i := range maxEarly + 10 {
		Cancel("x-" + strconv.Itoa(i))
	}
	runningMu.Lock()
	n := len(early)
	runningMu.Unlock()
	if n > maxEarly {
		t.Errorf("%d early cancels kept, at most %d", n, maxEarly)
	}
}
