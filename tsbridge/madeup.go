package tsbridge

import (
	"net"
	"net/netip"
	"time"

	"tailscale.com/net/netmon"
)

// Android does not let an app list the network interfaces
// (golang/go#40569: "route ip+net: netlinkrib: permission denied"), and
// without a list Tailscale's network monitor does not start. Tailscale's own
// Android app gets the list from Java; its package for programs without
// Java (feature/androidbin) leaves cgo builds like this one alone. So this
// does what that package does: one made-up interface that carries the
// addresses the system would send from, found by connecting UDP sockets
// (which sends nothing). A later stage may take the real list from the
// app's side instead (#57).

const madeUpInterface = "askorus0"

// UseMadeUpInterfaces makes the node use the made-up interface list. On
// Android it is always on (android.go); elsewhere it is only for trying the
// same path on a desk computer. Call it before Start.
func UseMadeUpInterfaces() {
	netmon.RegisterInterfaceGetter(madeUpInterfaces)
}

func madeUpInterfaces() ([]netmon.Interface, error) {
	addrs := []net.Addr{} // not nil: nil would make Go ask the system again
	if ip, ok := outboundIP("udp4", "8.8.8.8:53"); ok {
		addrs = append(addrs, &net.IPNet{IP: ip.AsSlice(), Mask: net.CIDRMask(32, 32)})
	}
	if ip, ok := outboundIP("udp6", "[2001:4860:4860::8888]:53"); ok {
		addrs = append(addrs, &net.IPNet{IP: ip.AsSlice(), Mask: net.CIDRMask(128, 128)})
	}
	ifc := &net.Interface{Index: 1, MTU: 1500, Name: madeUpInterface}
	if len(addrs) > 0 {
		// With no network the interface is reported down rather than as an
		// error, so the node can start offline and come up later.
		ifc.Flags = net.FlagUp | net.FlagRunning
	}
	return []netmon.Interface{{Interface: ifc, AltAddrs: addrs}}, nil
}

// outboundIP is the address the system would send from to reach addr.
func outboundIP(network, addr string) (netip.Addr, bool) {
	d := net.Dialer{Timeout: 2 * time.Second}
	c, err := d.Dial(network, addr)
	if err != nil {
		return netip.Addr{}, false
	}
	defer c.Close()
	ua, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok {
		return netip.Addr{}, false
	}
	ip, ok := netip.AddrFromSlice(ua.IP)
	if !ok {
		return netip.Addr{}, false
	}
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsUnspecified() {
		return netip.Addr{}, false
	}
	return ip, true
}
