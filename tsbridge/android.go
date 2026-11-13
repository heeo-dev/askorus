//go:build android

package tsbridge

import "tailscale.com/net/netmon"

func init() {
	UseMadeUpInterfaces() // see madeup.go
	// The monitor only rebinds after a network change when the default
	// route's interface is up, so name ours.
	netmon.UpdateLastKnownDefaultRouteInterface(madeUpInterface)
}
