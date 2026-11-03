package p2p

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

func TestPathMoved(t *testing.T) {
	t.Parallel()
	wifi := pathRoute{Interface: "en0", Gateway: "192.168.1.1", Source: "192.168.1.5"}
	assigned := map[string]bool{"192.168.1.5": true, "192.168.1.6": true}
	for name, tc := range map[string]struct {
		current pathRoute
		local   map[string]bool
		want    bool
	}{
		"same route":                 {wifi, assigned, false},
		"another interface":          {pathRoute{Interface: "utun4", Gateway: "198.18.0.1", Source: "198.18.0.1"}, assigned, true},
		"another gateway":            {pathRoute{Interface: "en0", Gateway: "192.168.2.1", Source: "192.168.1.5"}, assigned, true},
		"source rotated but valid":   {pathRoute{Interface: "en0", Gateway: "192.168.1.1", Source: "192.168.1.6"}, assigned, false},
		"established source removed": {pathRoute{Interface: "en0", Gateway: "192.168.1.1", Source: "192.168.1.6"}, map[string]bool{"192.168.1.6": true}, true},
	} {
		if got := pathMoved(wifi, tc.current, tc.local); got != tc.want {
			t.Errorf("%s = %v, want %v", name, got, tc.want)
		}
	}
	if pathMoved(pathRoute{Interface: "en0"}, pathRoute{Interface: "en0"}, nil) {
		t.Error("a route without a source moved")
	}
}

func TestIsTunnelInterface(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		flags net.Flags
		want  bool
	}{
		"utun4":    {net.FlagUp | net.FlagPointToPoint, true},
		"wg0":      {net.FlagUp | net.FlagPointToPoint, true},
		"ipsec0":   {net.FlagUp | net.FlagPointToPoint, true},
		"en0":      {net.FlagUp | net.FlagBroadcast, false},
		"en9":      {net.FlagUp | net.FlagBroadcast, false},
		"pdp_ip0":  {net.FlagUp | net.FlagPointToPoint, false},
		"ppp0":     {net.FlagUp | net.FlagPointToPoint, false},
		"bridge0":  {net.FlagUp | net.FlagBroadcast, false},
		"feth1234": {net.FlagUp | net.FlagBroadcast, false},
	} {
		if got := IsTunnelInterface(net.Interface{Name: name, Flags: tc.flags}); got != tc.want {
			t.Errorf("%s = %v, want %v", name, got, tc.want)
		}
	}
}

func TestUsableByOtherDevices(t *testing.T) {
	t.Parallel()
	tunnels := map[netip.Addr]bool{}
	for _, ip := range []string{"fd93:e235:3ffc::2", "fdfe:dcba:9876::1", "10.8.0.2", "100.100.1.2", "fd7a:115c:a1e0::1"} {
		tunnels[netip.MustParseAddr(ip)] = true
	}
	for addr, want := range map[string]bool{
		"/ip4/192.168.1.91/tcp/1":              true,
		"/ip6/240e:3bc::1/udp/1/quic-v1":       true,
		"/ip4/101.33.221.22/tcp/1/p2p-circuit": true,
		"/ip4/198.18.0.1/tcp/1":                false,
		"/ip4/198.19.255.1/udp/1/quic-v1":      false,
		"/ip6/fd93:e235:3ffc::2/tcp/1":         false,
		"/ip6/fdfe:dcba:9876::1/udp/1/quic-v1": false,
		"/ip4/10.8.0.2/tcp/1":                  false,
		"/ip4/100.100.1.2/tcp/1":               true,
		"/ip6/fd7a:115c:a1e0::1/udp/1/quic-v1": true,
		"/ip4/100.64.9.9/tcp/1":                true, // Carrier-grade NAT on a real uplink.
		"/ip6/fd00::1/tcp/1":                   true, // A ULA on a real LAN.
	} {
		a, err := ma.NewMultiaddr(addr)
		if err != nil {
			t.Fatal(err)
		}
		if got := usableByOtherDevices(a, tunnels); got != want {
			t.Errorf("%s = %v, want %v", addr, got, want)
		}
	}
}

func TestDialRetrySlowsOnlyOnceADeviceLooksOffline(t *testing.T) {
	t.Parallel()
	delay := minKickInterval
	for range 8 {
		delay = nextDialDelay(delay, time.Minute)
	}
	if delay != peerRetryMax {
		t.Fatalf("recently lost device retries every %s", delay)
	}
	for range 8 {
		delay = nextDialDelay(delay, peerOfflineAfter)
	}
	if delay != peerOfflineRetryMax {
		t.Fatalf("offline device retries every %s", delay)
	}
}

func TestDeviceRetriesCannotLockOutTheRelayIP(t *testing.T) {
	t.Parallel()
	limiter := deviceConnRateLimiter()
	relay := netip.MustParseAddr("101.33.221.22")
	for i := range 32 {
		if !limiter.Allow(relay) {
			t.Fatalf("dial %d refused", i)
		}
	}
}

// routes is a routing table the test controls.
type routes struct {
	mu   sync.Mutex
	next pathRoute
	err  error
}

func (r *routes) set(next pathRoute, err error) {
	r.mu.Lock()
	r.next, r.err = next, err
	r.mu.Unlock()
}

func (r *routes) route(net.IP) (pathRoute, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.next, r.err
}

func startRouted(t *testing.T, key crypto.PrivKey, acct *testAccount, r *routes, marks *timing.Marks) *Service {
	t.Helper()
	svc, err := Start(context.Background(), Config{
		Key: key, Fetch: acct.fetch, ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"}, RefreshInterval: time.Hour, Marks: marks,
		route:    r.route,
		localIPs: func() (map[string]bool, error) { return map[string]bool{"127.0.0.1": true, "192.168.1.5": true}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return svc
}

var wifiRoute = pathRoute{Interface: "en0", Gateway: "192.168.1.1", Source: "192.168.1.5"}

func connected(t *testing.T, a, b *Service) network.Conn {
	t.Helper()
	introduce(a, b)
	if !waitFor(t, 5*time.Second, func() bool {
		a.healthMu.Lock()
		defer a.healthMu.Unlock()
		conns := a.host.Network().ConnsToPeer(b.ID())
		return len(conns) > 0 && a.health[conns[0].ID()] != nil
	}) {
		t.Fatal("not connected")
	}
	return a.host.Network().ConnsToPeer(b.ID())[0]
}

func TestNetworkChangeClosesOnlyTheConnectionWhoseRouteMoved(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	keyC, idC := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA}, Peer{ID: idB}, Peer{ID: idC})
	r := &routes{next: wifiRoute}
	a := startRouted(t, keyA, acct, r, nil)
	b, c := startDevice(t, keyB, acct, ""), startDevice(t, keyC, acct, "")
	toB, toC := connected(t, a, b), connected(t, a, c)
	// All test connections share one loopback IP, so give B its own earlier route.
	a.healthMu.Lock()
	a.health[toB.ID()].route = pathRoute{Interface: "utun4", Gateway: "198.18.0.1"}
	a.healthMu.Unlock()

	a.NetworkChanged()

	if !toB.IsClosed() {
		t.Fatal("connection on the moved route stayed open")
	}
	if toC.IsClosed() {
		t.Fatal("connection on an unchanged route was closed")
	}
}

func TestNetworkChangeOnAnUnrelatedInterfaceKeepsConnectionsAndStreams(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA}, Peer{ID: idB})
	a := startRouted(t, keyA, acct, &routes{next: wifiRoute}, nil)
	b := startDevice(t, keyB, acct, "")
	conn := connected(t, a, b)
	const held = "/multica/held/1"
	done := make(chan struct{})
	defer close(done)
	b.Handle(held, func(st network.Stream) { defer st.Close(); _, _ = st.Write([]byte("x")); <-done })
	st, err := a.Open(context.Background(), idB, held)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a.NetworkChanged()
	a.NetworkChanged()

	if conn.IsClosed() {
		t.Fatal("working connection was closed")
	}
	if _, err := io.ReadFull(st, make([]byte, 1)); err != nil {
		t.Fatalf("stream did not survive: %v", err)
	}
}

func TestMovedConnectionReconnectsAndSaysWhyItWasLost(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA}, Peer{ID: idB})
	ring := timing.OpenRing("", 64)
	r := &routes{next: wifiRoute}
	a := startRouted(t, keyA, acct, r, timing.NewMarks(ring, timing.SourceDaemon))
	b := startDevice(t, keyB, acct, "")
	acct.set(Peer{ID: idA}, Peer{ID: idB, RegisteredAddrs: b.host.Addrs()})
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return peerStatus(a, idB).Connected }) {
		t.Fatal("not connected")
	}
	old := a.host.Network().ConnsToPeer(idB)
	r.set(pathRoute{Interface: "en1", Gateway: "10.0.0.1", Source: "192.168.1.5"}, nil)

	a.NetworkChanged()

	for _, conn := range old {
		if !conn.IsClosed() {
			t.Fatal("connection on the old route retained")
		}
	}
	if !waitFor(t, 2*time.Second, func() bool { return peerStatus(a, idB).Connected }) {
		t.Fatal("did not reconnect promptly on the new route")
	}
	wantLostBecause(t, ring, timing.ReasonPathChanged)
}

// wantLostBecause waits because libp2p reports a closed connection asynchronously.
func wantLostBecause(t *testing.T, ring *timing.Ring, reason string) {
	t.Helper()
	if !waitFor(t, 2*time.Second, func() bool {
		lost, ok := findMark(recordsOf(ring), timing.StageConnectionLost)
		return ok && lost.Reason == reason
	}) {
		t.Fatalf("no lost record with reason %s in %+v", reason, recordsOf(ring))
	}
}

func probeMisses(s *Service, conn network.Conn) int {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	return s.health[conn.ID()].misses
}

func silence(s *Service) {
	s.host.SetStreamHandler(ping.ID, func(st network.Stream) { defer st.Close(); _, _ = io.Copy(io.Discard, st) })
}

func TestConnectionSurvivesMissedProbesUntilTheyAreConsecutive(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA}, Peer{ID: idB})
	ring := timing.OpenRing("", 64)
	a := startRouted(t, keyA, acct, &routes{next: wifiRoute}, timing.NewMarks(ring, timing.SourceDaemon))
	b := startDevice(t, keyB, acct, "")
	conn := connected(t, a, b)
	const timeout = 100 * time.Millisecond

	silence(b)
	for range peerProbeMisses - 1 {
		if !a.checkPeerWithin(idB, timeout) {
			t.Fatal("probe was answered")
		}
	}
	if conn.IsClosed() {
		t.Fatal("closed before the misses were consecutive")
	}
	// An answer means the stall ended, so the count starts over.
	ping.NewPingService(b.host)
	if a.checkPeerWithin(idB, 2*time.Second) {
		t.Fatal("probe was not answered")
	}
	silence(b)
	for range peerProbeMisses - 1 {
		a.checkPeerWithin(idB, timeout)
	}
	if conn.IsClosed() {
		t.Fatal("an answered probe did not reset the count")
	}
	a.checkPeerWithin(idB, timeout)
	if !conn.IsClosed() {
		t.Fatal("unresponsive connection stayed open")
	}
	wantLostBecause(t, ring, timing.ReasonProbeFailed)
}

func TestUncomparableRouteOnlyLowersTheProbeThreshold(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA}, Peer{ID: idB})
	r := &routes{next: wifiRoute}
	a := startRouted(t, keyA, acct, r, nil)
	b := startDevice(t, keyB, acct, "")
	conn := connected(t, a, b)
	r.set(pathRoute{}, errors.New("no routing table on this platform"))

	a.NetworkChanged()

	if conn.IsClosed() {
		t.Fatal("closed without evidence")
	}
	if !waitFor(t, 2*time.Second, func() bool { return probeMisses(a, conn) == 0 }) {
		t.Fatal("healthy connection was not probed after the network change")
	}
	if conn.IsClosed() {
		t.Fatal("healthy connection closed")
	}
	silence(b)
	a.closeMovedPaths()
	a.checkPeerWithin(idB, 100*time.Millisecond)
	if !conn.IsClosed() {
		t.Fatal("one missed probe should close a connection whose route is unknown")
	}
}

func TestRecheckPathsKeepsAResponsiveConnectionAfterResume(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA}, Peer{ID: idB})
	a := startRouted(t, keyA, acct, &routes{next: wifiRoute}, nil)
	b := startDevice(t, keyB, acct, "")
	conn := connected(t, a, b)

	a.RecheckPaths()
	if !waitFor(t, 2*time.Second, func() bool { return probeMisses(a, conn) == 0 }) {
		t.Fatal("resume did not probe now")
	}
	if conn.IsClosed() {
		t.Fatal("responsive connection closed on resume")
	}
	silence(b)
	a.RecheckPaths()
	a.checkPeerWithin(idB, 100*time.Millisecond)
	if !conn.IsClosed() {
		t.Fatal("dead connection survived the check after resume")
	}
}

func TestRegistryReadyEventLeavesConnectionsAndDialRetriesAlone(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	_, idGone := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA}, Peer{ID: idB})
	a := startRouted(t, keyA, acct, &routes{next: wifiRoute}, nil)
	b := startDevice(t, keyB, acct, "")
	conn := connected(t, a, b)
	silence(b) // A probe from the event would now count as missed.
	bad, _ := ma.NewMultiaddr("/ip4/127.0.0.1/tcp/1")
	acct.set(Peer{ID: idA}, Peer{ID: idB}, Peer{ID: idGone, RegisteredAddrs: []ma.Multiaddr{bad}})
	if err := a.PeersChanged(context.Background()); err != nil {
		t.Fatal(err)
	}
	waiting := func() (*peerDial, bool) {
		a.dialMu.Lock()
		defer a.dialMu.Unlock()
		job := a.dialing[idGone]
		return job, job != nil && job.cancel == nil
	}
	if !waitFor(t, 2*time.Second, func() bool { _, ok := waiting(); return ok }) {
		t.Fatal("dial did not enter its retry delay")
	}

	for range 3 {
		if err := a.PeersChanged(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	job, _ := waiting()
	if job == nil || len(job.wake) != 0 {
		t.Fatal("an unchanged device list woke the dial retry")
	}
	select {
	case <-a.probePeers:
		t.Fatal("an unchanged device list requested a probe")
	default:
	}
	if conn.IsClosed() {
		t.Fatal("an unchanged device list closed a connection")
	}
}

func TestAdvertisedAddrsLeaveOutTunnelsOtherDevicesCannotDial(t *testing.T) {
	key, _ := newKey(t)
	acct := &testAccount{}
	svc, err := Start(context.Background(), Config{
		Key: key, Fetch: acct.fetch, ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"}, RefreshInterval: time.Hour,
		tunnelIPs: func() map[netip.Addr]bool { return map[netip.Addr]bool{netip.MustParseAddr("10.8.0.2"): true} },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	lan, _ := ma.NewMultiaddr("/ip4/192.168.1.5/tcp/1")
	vpn, _ := ma.NewMultiaddr("/ip4/10.8.0.2/tcp/1")
	proxy, _ := ma.NewMultiaddr("/ip4/198.18.0.1/tcp/1")
	got := svc.advertisableAddrs([]ma.Multiaddr{lan, vpn, proxy})
	if len(got) != 1 || !got[0].Equal(lan) {
		t.Fatalf("advertised %v", got)
	}
}
