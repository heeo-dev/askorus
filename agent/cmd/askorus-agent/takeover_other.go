//go:build !linux && !darwin

package main

import "io"

// Elsewhere update-finish has no service to leave, and an old agent that
// does not stop is not ended: the update goes back (Phase 1 plan part 27).

func leaveService() error             { return nil }
func inService(int) bool              { return false }
func stopService() error              { return nil }
func endStuck(string, int, bool) bool { return false }

func endStuckCmd(string, []string, io.Writer) error {
	return fail("end-stuck is only for macOS and Linux", "end-stuck은 macOS와 Linux에서만 씁니다")
}
