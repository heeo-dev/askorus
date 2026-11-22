//go:build !windows && !linux && !darwin

package main

// isZombie: not told apart on the other unix systems.
func isZombie(int) bool { return false }
