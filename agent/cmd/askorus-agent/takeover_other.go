//go:build !linux && !darwin

package main

// Elsewhere update-finish has no service to leave, and an old agent that
// does not stop is not ended: the update goes back (Phase 1 plan part 27).

func leaveService() error       { return nil }
func inService(int) bool        { return false }
func stopService()              {}
func endStuck(string, int) bool { return false }
