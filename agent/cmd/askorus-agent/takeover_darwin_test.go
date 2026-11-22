//go:build darwin

package main

import (
	"slices"
	"testing"
)

// R-178 (#71): kern.procargs2 as macOS lays it out.
func TestParseProcArgs(t *testing.T) {
	raw := []byte("\x02\x00\x00\x00/Users/a b/.local/bin/askorus-agent\x00\x00\x00\x00/Users/a b/.local/bin/askorus-agent\x00serve\x00HOME=/Users/a b\x00")
	exe, args, ok := parseProcArgs(raw)
	if !ok || exe != "/Users/a b/.local/bin/askorus-agent" || !slices.Equal(args, []string{exe, "serve"}) {
		t.Fatalf("parseProcArgs = %q, %q, %v", exe, args, ok)
	}
	for _, bad := range []string{"", "\x01\x00", "\x01\x00\x00\x00/no/end", "\xff\xff\xff\xff/x\x00one\x00"} {
		if _, _, ok := parseProcArgs([]byte(bad)); ok {
			t.Errorf("parseProcArgs(%q) took it", bad)
		}
	}
}
