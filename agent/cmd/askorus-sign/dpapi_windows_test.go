package main

import (
	"bytes"
	"testing"
)

// The real DPAPI: what one user locks, that user opens, and the entropy
// is part of it.
func TestDPAPIOpensForThisUserOnly(t *testing.T) {
	locked, err := dpapiLock([]byte("seed"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(locked, []byte("seed")) {
		t.Fatal("the locked bytes hold the seed")
	}
	got, err := dpapiUnlock(locked)
	if err != nil || string(got) != "seed" {
		t.Fatalf("unlock = %q, %v", got, err)
	}
	saved := dpapiEntropy
	dpapiEntropy = []byte("another program")
	defer func() { dpapiEntropy = saved }()
	if _, err := dpapiUnlock(locked); err == nil {
		t.Fatal("opened with another program's entropy")
	}
}
