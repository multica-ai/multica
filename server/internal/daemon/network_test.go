package daemon

import (
	"net"
	"testing"
)

func TestVisibleIPAddressesFromAddrsFiltersAndSortsDiagnosticAddresses(t *testing.T) {
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("fe80::1")},
		&net.IPNet{IP: net.ParseIP("127.0.0.1")},
		&net.IPNet{IP: net.ParseIP("10.0.0.8")},
		&net.IPNet{IP: net.ParseIP("2001:db8::10")},
		&net.IPNet{IP: net.ParseIP("192.168.1.20")},
		&net.IPAddr{IP: net.ParseIP("10.0.0.8")},
	}

	got := visibleIPAddressesFromAddrs(addrs)
	want := []string{"10.0.0.8", "192.168.1.20", "2001:db8::10"}
	if len(got) != len(want) {
		t.Fatalf("addresses = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("addresses = %v, want %v", got, want)
		}
	}
}
