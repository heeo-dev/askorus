package main

import "testing"

// #52: on Linux `tailscale serve` is refused without sudo, so the commands
// the agent tells the user to run start with it there, and only there.
func TestTailscaleCmd(t *testing.T) {
	for goos, want := range map[string]string{"linux": "sudo tailscale", "windows": "tailscale", "darwin": "tailscale"} {
		if got := tailscaleCmd(goos); got != want {
			t.Errorf("%s: %q, want %q", goos, got, want)
		}
	}
}
