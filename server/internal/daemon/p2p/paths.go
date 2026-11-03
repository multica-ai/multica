package p2p

import (
	"net"
	"net/netip"
	"strings"

	netroute "github.com/libp2p/go-netroute"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// pathRoute is the operating system's route to one connection's remote address.
type pathRoute struct {
	Interface string
	Gateway   string
	Source    string
}

func systemRoute(ip net.IP) (pathRoute, error) {
	router, err := netroute.New()
	if err != nil {
		return pathRoute{}, err
	}
	iface, gateway, source, err := router.Route(ip)
	if err != nil {
		return pathRoute{}, err
	}
	route := pathRoute{}
	if iface != nil {
		route.Interface = iface.Name
	}
	if gateway != nil {
		route.Gateway = gateway.String()
	}
	if source != nil {
		route.Source = source.String()
	}
	return route, nil
}

// pathMoved reports whether a connection's traffic can no longer use the path it
// was established on. The preferred source may change while the original address
// stays valid, as when IPv6 temporary addresses rotate, so only its removal counts.
func pathMoved(established, current pathRoute, localIPs map[string]bool) bool {
	if established.Interface != current.Interface || established.Gateway != current.Gateway {
		return true
	}
	return established.Source != "" && !localIPs[established.Source]
}

// IsTunnelInterface reports whether iface is a virtual point-to-point tunnel.
// Cellular and PPPoE links are point-to-point too, but they are real uplinks.
func IsTunnelInterface(iface net.Interface) bool {
	if iface.Flags&net.FlagPointToPoint == 0 {
		return false
	}
	return !strings.HasPrefix(iface.Name, "pdp_ip") && !strings.HasPrefix(iface.Name, "ppp")
}

func systemTunnelIPs() map[netip.Addr]bool {
	ips := map[netip.Addr]bool{}
	interfaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, iface := range interfaces {
		if !IsTunnelInterface(iface) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if prefix, err := netip.ParsePrefix(addr.String()); err == nil {
				ips[prefix.Addr().Unmap()] = true
			}
		}
	}
	return ips
}

var (
	// Proxy TUN modes hand out addresses from this range, often the same one on
	// every machine, so it never identifies a device.
	fakeIPRange = netip.MustParsePrefix("198.18.0.0/15")
	// Tailscale's ranges are the tunnel addresses another device can actually use.
	// 100.64.0.0/10 is also carrier-grade NAT, so it only counts on a tunnel.
	meshTunnelRanges = []netip.Prefix{
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("fd7a:115c:a1e0::/48"),
	}
)

// usableByOtherDevices reports whether another device could dial a. Addresses
// without an IP, such as relay circuits, are left to their own checks.
func usableByOtherDevices(a ma.Multiaddr, tunnelIPs map[netip.Addr]bool) bool {
	if isCircuit(a) {
		return true
	}
	ip, err := manet.ToIP(a)
	if err != nil {
		return true
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	addr = addr.Unmap()
	if fakeIPRange.Contains(addr) {
		return false
	}
	if !tunnelIPs[addr] {
		return true
	}
	for _, mesh := range meshTunnelRanges {
		if mesh.Contains(addr) {
			return true
		}
	}
	return false
}
