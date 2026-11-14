package tsbridge

import (
	"net"
	"testing"
)

// R-106 (#57): on Android an app cannot list the network interfaces, so the
// node gets one made-up interface. It must never be an error (the node has
// to start without a network too), and its address list must never be nil:
// with nil, Go asks the system again, which is what Android refuses.
func TestMadeUpInterfaces(t *testing.T) {
	got, err := madeUpInterfaces()
	if err != nil || len(got) != 1 {
		t.Fatalf("interfaces = %+v, %v", got, err)
	}
	ifc := got[0]
	if ifc.Interface == nil || ifc.Name != madeUpInterface || ifc.MTU != 1500 {
		t.Errorf("interface = %+v", ifc.Interface)
	}
	if ifc.AltAddrs == nil {
		t.Fatal("AltAddrs is nil: Go would ask the system for the addresses")
	}
	// Up only when the system would send from some address.
	up := ifc.Flags&net.FlagUp != 0
	if up != (len(ifc.AltAddrs) > 0) {
		t.Errorf("up = %v with %d addresses", up, len(ifc.AltAddrs))
	}
	for _, a := range ifc.AltAddrs {
		n, ok := a.(*net.IPNet)
		if !ok || n.IP.IsLoopback() || n.IP.IsUnspecified() {
			t.Errorf("address %v", a)
		}
	}
}
