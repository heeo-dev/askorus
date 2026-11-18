package api

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

// R-137 (#35 review): a request that came in just before the hold (its
// body still on the way) is waited for, and none comes in after it.
func TestHoldWaitsForWorkThatCameInBefore(t *testing.T) {
	e := updateEnv(t, &fakeUpdater{}, nil)
	leave, ok := e.srv.enter(httptest.NewRecorder())
	if !ok {
		t.Fatal("a request was refused before any hold")
	}
	e.srv.Hold(true)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := e.srv.WaitIdle(ctx); err == nil {
		t.Fatal("WaitIdle ended while a request that came in before the hold was under way")
	}
	leave()
	if err := e.srv.WaitIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if _, ok := e.srv.enter(w); ok || w.Code != 503 {
		t.Fatalf("a request after the hold came in: %d", w.Code)
	}
}
