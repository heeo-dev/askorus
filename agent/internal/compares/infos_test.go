package compares

import (
	"context"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// slowAI's Info takes a while, as an Info that runs its AI program does.
type slowAI struct {
	*fakeAI
	wait  time.Duration
	panic bool
}

func (s slowAI) Info(ctx context.Context) core.AdapterInfo {
	time.Sleep(s.wait)
	if s.panic {
		panic("info failed")
	}
	return s.fakeAI.Info(ctx)
}

// R-185 (2026-11-25): /v1/info and a question's start asked the four AIs
// one after another, so their version and sign-in checks added up on the
// phone's first request after a pause. They are asked at the same time,
// and the answers keep the adapters' order.
func TestInfosOfAsksAtTheSameTime(t *testing.T) {
	names := []string{"a", "b", "c", "d"}
	var ais []core.Adapter
	for _, n := range names {
		ais = append(ais, slowAI{fakeAI: newFake(n, "x"), wait: 300 * time.Millisecond})
	}
	start := time.Now()
	infos := InfosOf(context.Background(), ais)
	if d := time.Since(start); d > 900*time.Millisecond {
		t.Errorf("four Infos of 300 ms took %v", d)
	}
	for i, n := range names {
		if infos[i].Agent != n {
			t.Errorf("infos[%d] = %q, want %q", i, infos[i].Agent, n)
		}
	}
	g := testGate(t, ais...)
	got, _ := Announce(context.Background(), ais, g)
	for i, n := range names {
		if got[i].Agent != n {
			t.Errorf("announce[%d] = %q, want %q", i, got[i].Agent, n)
		}
	}
}

// R-185: a panic in an Info goes on in the caller, as before (the HTTP
// server ends that request), instead of ending the whole agent from
// another goroutine.
func TestInfosOfPanicGoesToTheCaller(t *testing.T) {
	ais := []core.Adapter{newFake("a", "x"), slowAI{fakeAI: newFake("b", "x"), panic: true}}
	defer func() {
		if recover() == nil {
			t.Error("the panic was lost")
		}
	}()
	InfosOf(context.Background(), ais)
}
