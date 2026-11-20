package core

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

var testLines = []Line{
	{Base: "gpt-6.1-sol", Re: regexp.MustCompile(`^gpt-([0-9]+(?:\.[0-9]+)*)-sol$`)},
	{Base: "gpt-6-luna", Re: regexp.MustCompile(`^gpt-([0-9]+(?:\.[0-9]+)*)-luna$`)},
}

// listed makes a follower whose list is list, read once.
func listed(t *testing.T, list ...ListedModel) *Follower {
	t.Helper()
	f := &Follower{Lines: testLines, List: func(context.Context) ([]ListedModel, error) { return list, nil }}
	f.Check("1.0.0")
	f.Wait(context.Background())
	return f
}

func TestVersions(t *testing.T) {
	for s, want := range map[string][]int{"6": {6}, "6.1": {6, 1}, "6.10.2": {6, 10, 2}, "": nil, "6.": nil, "6.x": nil, "06": nil, "-1": nil} {
		if got := parseVersion(s); !slices.Equal(got, want) {
			t.Errorf("parseVersion(%q) = %v, want %v", s, got, want)
		}
	}
	for _, c := range []struct {
		a, b string
		want int
	}{{"6.2", "6.1", 1}, {"6", "6.0", 0}, {"6.10", "6.9", 1}, {"6", "6.1", -1}, {"7", "6.9.9", 1}} {
		if got := compareVersions(parseVersion(c.a), parseVersion(c.b)); (got > 0) != (c.want > 0) || (got < 0) != (c.want < 0) {
			t.Errorf("compare %s %s = %d, want sign of %d", c.a, c.b, got, c.want)
		}
	}
}

// TestNewest: the newest listed model of the line that takes the effort
// and was not refused; never an older one, never another line's.
func TestNewest(t *testing.T) {
	f := listed(t,
		ListedModel{Name: "gpt-6.2-sol", Efforts: []string{"low", "medium"}},
		ListedModel{Name: "gpt-6.1-sol", Efforts: []string{"low", "medium", "high"}},
		ListedModel{Name: "gpt-5.6-sol"},
		ListedModel{Name: "gpt-6.2-astra"},
		ListedModel{Name: "gpt-6-luna"},
		ListedModel{Name: "gpt-6.2-sol-mini"},
	)
	for _, c := range []struct{ model, effort, want string }{
		{"gpt-6.1-sol", "medium", "gpt-6.2-sol"},
		{"gpt-6.1-sol", "", "gpt-6.2-sol"},
		{"gpt-6.1-sol", "high", "gpt-6.1-sol"}, // 6.2 does not take high
		{"gpt-6-sol", "medium", "gpt-6.2-sol"}, // an old pick moves up
		{"gpt-7-sol", "medium", "gpt-7-sol"},   // newer than any listed: kept
		{"gpt-6-luna", "medium", "gpt-6-luna"},
		{"gpt-6.1-terra", "medium", "gpt-6.1-terra"}, // no line
		{"opus", "", "opus"},
	} {
		if got := f.Newest(c.model, c.effort); got != c.want {
			t.Errorf("Newest(%q, %q) = %q, want %q", c.model, c.effort, got, c.want)
		}
	}
	if !f.Refuse("gpt-6.2-sol") || f.Newest("gpt-6.1-sol", "medium") != "gpt-6.1-sol" {
		t.Error("a refused model is still followed")
	}
	if f.Refuse("gpt-6.1-sol") || f.Refuse("gpt-6.1-terra") {
		t.Error("Refuse took a shipped model or one of no line")
	}
	// R-149 (2ho's 0.5.1 review): the refused model named again goes to
	// the shipped one, or a newer one not refused, not back to the AI.
	if got := f.Newest("gpt-6.2-sol", ""); got != "gpt-6.1-sol" {
		t.Errorf("Newest(refused) = %q", got)
	}
	if f.Base("gpt-6.2-sol") != "gpt-6.1-sol" || f.Base("opus") != "" {
		t.Error("Base")
	}
	if es := f.Efforts("gpt-6.2-sol"); !slices.Equal(es, []string{"low", "medium"}) || f.Efforts("gpt-5.6-sol") != nil {
		t.Errorf("Efforts = %v", es)
	}
}

// TestNoList: no list read, a nil follower or a follower without List
// keep every model.
func TestNoList(t *testing.T) {
	var nilF *Follower
	off := &Follower{Lines: testLines}
	off.Check("1")
	for _, f := range []*Follower{nilF, off} {
		if f.Newest("gpt-6.1-sol", "") != "gpt-6.1-sol" || f.Listed() || f.Efforts("x") != nil {
			t.Errorf("%v follows", f)
		}
		f.Wait(context.Background())
	}
	if nilF.Base("gpt-6.2-sol") != "" || nilF.Refuse("gpt-6.2-sol") {
		t.Error("nil Base or Refuse")
	}
	// Without a list a newer model the phone named is still asked again
	// with the shipped one when refused.
	if !off.Refuse("gpt-6.2-sol") || off.Base("gpt-6.2-sol") != "gpt-6.1-sol" {
		t.Error("Refuse without a list")
	}
}

// TestCheck: one list at a time, again after the TTL or when the CLI's
// version changes; a failed or empty list keeps the last good one, and a
// new list tries a refused model again.
func TestCheck(t *testing.T) {
	var runs atomic.Int32
	var fail atomic.Bool
	release := make(chan struct{})
	f := &Follower{Lines: testLines, TTL: time.Hour, List: func(context.Context) ([]ListedModel, error) {
		n := runs.Add(1)
		if n == 1 {
			<-release
		}
		if fail.Load() {
			return nil, errors.New("no list")
		}
		return []ListedModel{{Name: "gpt-6.2-sol"}}, nil
	}}
	f.Check("1.0")
	f.Check("1.0") // one runs: not a second
	close(release)
	f.Wait(context.Background())
	if runs.Load() != 1 || f.Newest("gpt-6.1-sol", "") != "gpt-6.2-sol" {
		t.Fatalf("runs %d, newest %q", runs.Load(), f.Newest("gpt-6.1-sol", ""))
	}
	f.Check("1.0")
	f.Check("") // a version not known changes nothing
	f.Wait(context.Background())
	if runs.Load() != 1 {
		t.Errorf("listed again within the TTL: %d", runs.Load())
	}
	f.Refuse("gpt-6.2-sol")
	fail.Store(true)
	f.Check("1.1") // a new CLI version lists again
	f.Wait(context.Background())
	if runs.Load() != 2 || f.Newest("gpt-6.1-sol", "") != "gpt-6.1-sol" || !f.Listed() {
		t.Errorf("after a failed list: runs %d, newest %q", runs.Load(), f.Newest("gpt-6.1-sol", ""))
	}
	fail.Store(false)
	f.mu.Lock()
	f.at = time.Now().Add(-2 * time.Hour) // the TTL is over
	f.mu.Unlock()
	f.Check("1.1")
	f.Wait(context.Background())
	if runs.Load() != 3 || f.Newest("gpt-6.1-sol", "") != "gpt-6.2-sol" {
		t.Errorf("after the TTL: runs %d, newest %q", runs.Load(), f.Newest("gpt-6.1-sol", ""))
	}
}

// R-152 (2ho's 0.5.1 review): EndUsageReads ends a list that runs and
// waits for it, and a later list (another command in the same process)
// starts again.
func TestEndUsageReads(t *testing.T) {
	started, ended := make(chan struct{}), make(chan struct{})
	var runs atomic.Int32
	f := &Follower{Lines: testLines, List: func(ctx context.Context) ([]ListedModel, error) {
		if runs.Add(1) == 1 {
			close(started)
			<-ctx.Done()
			close(ended)
			return nil, ctx.Err()
		}
		return []ListedModel{{Name: "gpt-6.2-sol"}}, nil
	}}
	f.Check("1")
	<-started
	EndUsageReads(5 * time.Second)
	select {
	case <-ended:
	default:
		t.Fatal("EndUsageReads returned before the list ended")
	}
	g := &Follower{Lines: testLines, List: f.List}
	g.Check("1")
	g.Wait(context.Background())
	if runs.Load() != 2 || g.Newest("gpt-6.1-sol", "") != "gpt-6.2-sol" {
		t.Errorf("a later list: runs %d, newest %q", runs.Load(), g.Newest("gpt-6.1-sol", ""))
	}
}

// TestListedNames: a listed name that could pass for a flag is dropped.
func TestListedNames(t *testing.T) {
	f := listed(t, ListedModel{Name: "-gpt-9-sol"}, ListedModel{Name: "gpt-6.3-sol"})
	if f.Newest("gpt-6.1-sol", "") != "gpt-6.3-sol" {
		t.Error("the good name was not kept")
	}
	f.mu.Lock()
	_, bad := f.listed["-gpt-9-sol"]
	f.mu.Unlock()
	if bad {
		t.Error("a name starting with - was kept")
	}
}

func TestFollowPickAndNewer(t *testing.T) {
	f := listed(t, ListedModel{Name: "gpt-6.2-sol"}, ListedModel{Name: "gpt-6-luna"})
	tiers := map[string]AskModel{TierQuick: {Model: "gpt-6-luna"}, TierDeep: {Model: f.Newest("gpt-6.1-sol", "")}}
	if got := FollowPick(tiers, f, "gpt-6.1-sol"); got != "gpt-6.2-sol" {
		t.Errorf("old pick = %q", got)
	}
	pinned := map[string]AskModel{TierDeep: {Model: "gpt-6.1-sol"}}
	if got := FollowPick(pinned, f, "gpt-6.1-sol"); got != "gpt-6.1-sol" {
		t.Errorf("a tier model of agent.json moved: %q", got)
	}
	if n := NewerTiers(tiers, f); len(n) != 1 || n["gpt-6.1-sol"] != "gpt-6.2-sol" {
		t.Errorf("Newer = %v", n)
	}
	if n := NewerTiers(pinned, f); len(n) != 0 {
		t.Errorf("Newer of shipped = %v", n)
	}
}
