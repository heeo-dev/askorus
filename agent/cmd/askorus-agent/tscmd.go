package main

import "runtime"

// tailscaleCmd is how a command that changes Tailscale's settings starts
// in the hints the agent prints. On Linux those settings belong to root
// unless the user was made the operator, so the hint says sudo: without
// it `tailscale serve` is refused (#52, seen on Ubuntu, 2026-11-12).
func tailscaleCmd(goos string) string {
	if goos == "linux" {
		return "sudo tailscale"
	}
	return "tailscale"
}

var tsCmd = tailscaleCmd(runtime.GOOS)
