package daemon

import (
	"net"
	"sort"
)

func localIPAddresses() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var addrs []net.Addr
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		ifaceAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		addrs = append(addrs, ifaceAddrs...)
	}
	return visibleIPAddressesFromAddrs(addrs)
}

func visibleIPAddressesFromAddrs(addrs []net.Addr) []string {
	seen := make(map[string]struct{}, len(addrs))
	result := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		ip := ipFromAddr(addr)
		if ip == nil ||
			ip.IsLoopback() ||
			ip.IsUnspecified() ||
			ip.IsMulticast() ||
			ip.IsLinkLocalUnicast() {
			continue
		}
		value := ip.String()
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}

	sort.Slice(result, func(i, j int) bool {
		left4 := net.ParseIP(result[i]).To4() != nil
		right4 := net.ParseIP(result[j]).To4() != nil
		if left4 != right4 {
			return left4
		}
		return result[i] < result[j]
	})
	return result
}

func ipFromAddr(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}
