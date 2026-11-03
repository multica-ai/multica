package p2p

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/net/swarm"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// testAccount is a mutable server-side device list.
type testAccount struct {
	mu    sync.Mutex
	peers []Peer
}

func (a *testAccount) set(peers ...Peer) {
	a.mu.Lock()
	a.peers = peers
	a.mu.Unlock()
}

func (a *testAccount) fetch(context.Context) ([]Peer, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Peer(nil), a.peers...), nil
}

type testDevice struct {
	key  crypto.PrivKey
	id   peer.ID
	svc  *Service
	acct *testAccount
}

func newKey(t *testing.T) (crypto.PrivKey, peer.ID) {
	t.Helper()
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return key, id
}

func startDevice(t *testing.T, key crypto.PrivKey, acct *testAccount, mdnsName string, listen ...string) *Service {
	t.Helper()
	if len(listen) == 0 && mdnsName == "" {
		listen = []string{"/ip4/127.0.0.1/udp/0/quic-v1", "/ip4/127.0.0.1/tcp/0"}
	}
	svc, err := Start(context.Background(), Config{
		Key:             key,
		Fetch:           acct.fetch,
		ListenAddrs:     listen,
		MDNSServiceName: mdnsName,
		RefreshInterval: time.Hour,
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

func introduce(from, to *Service) {
	from.AddAddrs(peer.AddrInfo{ID: to.ID(), Addrs: to.host.Addrs()})
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

func TestLoadOrCreateKeyPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "p2p.key")
	first, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Equals(second) {
		t.Fatal("key changed across loads")
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if os.PathSeparator == '/' && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("key file readable by others: %v", info.Mode().Perm())
	}
	if err := os.WriteFile(path, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateKey(path); err == nil {
		t.Fatal("corrupt key file should fail instead of minting a new identity")
	}
}

func TestSameAccountDevicesPingAndStream(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA, DeviceName: "a"}, Peer{ID: idB, DeviceName: "b"})
	a := startDevice(t, keyA, acct, "")
	b := startDevice(t, keyB, acct, "")

	const echo = "/multica/test-echo/1"
	b.Handle(echo, func(s network.Stream) {
		defer s.Close()
		if s.Conn().RemotePeer() != idA {
			t.Errorf("remote = %s, want %s", s.Conn().RemotePeer(), idA)
		}
		io.Copy(s, s)
	})
	introduce(a, b)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := a.Ping(ctx, idB); err != nil {
		t.Fatalf("ping: %v", err)
	}
	s, err := a.Open(ctx, idB, echo)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	payload := bytes.Repeat([]byte("multica "), 64<<10)
	go func() {
		s.Write(payload)
		s.CloseWrite()
	}()
	got, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("echo mismatch: %d bytes, want %d", len(got), len(payload))
	}

	var st PeerStatus
	for _, p := range a.Status().Peers {
		if p.ID == idB {
			st = p
		}
	}
	if !st.Connected || st.DeviceName != "b" || len(st.Addrs) == 0 {
		t.Fatalf("status for b = %+v", st)
	}
}

func TestOpenRejectsPeerOutsideAccount(t *testing.T) {
	keyA, idA := newKey(t)
	_, idX := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA})
	a := startDevice(t, keyA, acct, "")
	if _, err := a.Open(context.Background(), idX, "/multica/test/1"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("got %v, want ErrNotAllowed", err)
	}
}

func TestResponderRejectsDeviceItDoesNotTrust(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	trustsBoth := &testAccount{}
	trustsBoth.set(Peer{ID: idA}, Peer{ID: idB})
	trustsSelf := &testAccount{}
	trustsSelf.set(Peer{ID: idB})
	a := startDevice(t, keyA, trustsBoth, "")
	b := startDevice(t, keyB, trustsSelf, "")
	introduce(a, b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := a.Ping(ctx, idB); err == nil {
		t.Fatal("responder admitted a device outside its account")
	}
}

func TestRevokedDeviceIsDisconnected(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA}, Peer{ID: idB})
	a := startDevice(t, keyA, acct, "")
	b := startDevice(t, keyB, acct, "")
	introduce(a, b)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := a.Ping(ctx, idB); err != nil {
		t.Fatalf("ping before revoke: %v", err)
	}

	acct.set(Peer{ID: idA})
	if err := a.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 3*time.Second, func() bool {
		return a.host.Network().Connectedness(idB) != network.Connected
	}) {
		t.Fatal("revoked device still connected")
	}
	// b has not refreshed yet and still lists a; a must refuse it anyway.
	if _, err := b.Ping(ctx, idA); err == nil {
		t.Fatal("revoked device could reconnect")
	}
}

func TestDeviceSeenBeforeRegistrationConnectsAfterRefresh(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acctA := &testAccount{}
	acctA.set(Peer{ID: idA})
	acctB := &testAccount{}
	acctB.set(Peer{ID: idA}, Peer{ID: idB})
	a := startDevice(t, keyA, acctA, "")
	startB := startDevice(t, keyB, acctB, "")
	// a sees b on the LAN before b's key reached the server.
	introduce(a, startB)

	acctA.set(Peer{ID: idA}, Peer{ID: idB})
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 10*time.Second, func() bool {
		return a.host.Network().Connectedness(idB) == network.Connected
	}) {
		t.Fatal("newly registered device seen earlier was not dialed")
	}
}

func TestPeerReconnectsAtNewAddressAfterDisconnect(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acctA, acctB := &testAccount{}, &testAccount{}
	acctA.set(Peer{ID: idA}, Peer{ID: idB})
	acctB.set(Peer{ID: idA}, Peer{ID: idB})
	a := startDevice(t, keyA, acctA, "")
	b := startDevice(t, keyB, acctB, "")
	acctA.set(Peer{ID: idA}, Peer{ID: idB, RegisteredAddrs: b.host.Addrs()})
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 5*time.Second, func() bool { return b.host.Network().Connectedness(idA) == network.Connected }) {
		t.Fatal("initial connection failed")
	}
	// The same device publishes new listeners as its old network goes away.
	next := startDevice(t, keyB, acctB, "")
	acctA.set(Peer{ID: idA}, Peer{ID: idB, RegisteredAddrs: next.host.Addrs()})
	b.host.Network().(*swarm.Swarm).ListenClose(b.host.Network().ListenAddresses()...)
	if err := b.host.Network().ClosePeer(idA); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, minKickInterval+5*time.Second, func() bool { return next.host.Network().Connectedness(idA) == network.Connected }) {
		t.Fatal("did not refresh addresses and reconnect before the hourly discovery tick")
	}
}

func TestUnresponsivePeerConnectionIsClosedForReconnect(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA}, Peer{ID: idB})
	a, b := startDevice(t, keyA, acct, ""), startDevice(t, keyB, acct, "")
	introduce(a, b)
	if !waitFor(t, 5*time.Second, func() bool { return a.host.Network().Connectedness(idB) == network.Connected }) {
		t.Fatal("initial connection failed")
	}
	// Keep the transport open but stop answering pings, as a stale network path does.
	b.host.SetStreamHandler(ping.ID, func(st network.Stream) { defer st.Close(); _, _ = io.Copy(io.Discard, st) })
	conns := a.host.Network().ConnsToPeer(idB)
	a.checkPeer(idB)
	for _, c := range conns {
		if !c.IsClosed() {
			t.Fatal("unresponsive connection remained cached")
		}
	}
}

func TestRegistryInvalidationInterruptsPeerDialBackoff(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA}, Peer{ID: idB})
	a, b := startDevice(t, keyA, acct, ""), startDevice(t, keyB, acct, "")
	// A failed address leaves the existing job in its five-second retry delay.
	bad, _ := ma.NewMultiaddr("/ip4/127.0.0.1/tcp/1")
	acct.set(Peer{ID: idA}, Peer{ID: idB, RegisteredAddrs: []ma.Multiaddr{bad}})
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, time.Second, func() bool {
		a.dialMu.Lock()
		defer a.dialMu.Unlock()
		job := a.dialing[idB]
		return job != nil && job.cancel == nil && a.host.Network().(*swarm.Swarm).Backoff().Backoff(idB, bad)
	}) {
		t.Fatal("dial did not enter retry delay")
	}
	acct.set(Peer{ID: idA}, Peer{ID: idB, RegisteredAddrs: b.host.Addrs()})
	if err := a.PeersChanged(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, time.Second, func() bool { return peerStatus(a, idB).Connected }) {
		t.Fatal("registry invalidation did not bypass dial backoff")
	}
}

func TestNetworkChangeRebuildsPathsWithoutWaitingForHeartbeat(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA}, Peer{ID: idB})
	a, b := startDevice(t, keyA, acct, ""), startDevice(t, keyB, acct, "")
	acct.set(Peer{ID: idA, RegisteredAddrs: a.host.Addrs()}, Peer{ID: idB, RegisteredAddrs: b.host.Addrs()})
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, time.Second, func() bool { return peerStatus(a, idB).Connected }) {
		t.Fatal("not connected")
	}
	old := a.host.Network().ConnsToPeer(idB)
	a.localIPs = func() (map[string]bool, error) { return map[string]bool{}, nil }
	a.NetworkChanged()
	for _, conn := range old {
		if !conn.IsClosed() && !localAddrIsWildcard(conn) {
			t.Fatal("path on a vanished address retained")
		}
	}
	if !waitFor(t, 2*time.Second, func() bool { return peerStatus(a, idB).Connected }) {
		t.Fatal("did not reconnect promptly after a local network change")
	}
}

func localAddrIsWildcard(conn network.Conn) bool {
	ip, err := manet.ToIP(conn.LocalMultiaddr())
	return err == nil && ip.IsUnspecified()
}

func TestNetworkChangeKeepsHealthyPathsAndItsStreams(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA}, Peer{ID: idB})
	a := startDevice(t, keyA, acct, "", "/ip4/127.0.0.1/tcp/0")
	b := startDevice(t, keyB, acct, "", "/ip4/127.0.0.1/tcp/0")
	introduce(a, b)
	if !waitFor(t, 5*time.Second, func() bool { return peerStatus(a, idB).Connected }) {
		t.Fatal("not connected")
	}
	const echo = "/multica/test-echo/1"
	b.Handle(echo, func(st network.Stream) { defer st.Close(); io.Copy(st, st) })
	st, err := a.Open(context.Background(), idB, echo)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a.NetworkChanged()
	// Longer than the one-second probe it starts.
	time.Sleep(1500 * time.Millisecond)
	if _, err := st.Write([]byte("x")); err != nil {
		t.Fatalf("stream did not survive a network change on a healthy path: %v", err)
	}
	buf := make([]byte, 1)
	_ = st.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(st, buf); err != nil {
		t.Fatalf("stream did not survive a network change on a healthy path: %v", err)
	}
}

func TestNetworkChangeClosesAPathThatStoppedResponding(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA}, Peer{ID: idB})
	a := startDevice(t, keyA, acct, "", "/ip4/127.0.0.1/tcp/0")
	b := startDevice(t, keyB, acct, "", "/ip4/127.0.0.1/tcp/0")
	introduce(a, b)
	if !waitFor(t, 5*time.Second, func() bool { return peerStatus(a, idB).Connected }) {
		t.Fatal("not connected")
	}
	old := a.host.Network().ConnsToPeer(idB)
	// Accepts a ping and never answers, like a path whose route has gone.
	b.host.SetStreamHandler(ping.ID, func(st network.Stream) { defer st.Close(); _, _ = io.Copy(io.Discard, st) })
	a.NetworkChanged()
	if !waitFor(t, 3*time.Second, func() bool {
		for _, conn := range old {
			if !conn.IsClosed() {
				return false
			}
		}
		return true
	}) {
		t.Fatal("unresponsive path retained after a network change")
	}
}

func TestLocalAddrVanished(t *testing.T) {
	ips := map[string]bool{"192.168.1.5": true}
	for addr, want := range map[string]bool{
		"/ip4/192.168.1.5/tcp/4001":     false,
		"/ip4/192.168.1.6/tcp/4001":     true,
		"/ip4/0.0.0.0/udp/4001/quic-v1": false,
		"/ip6/::/udp/4001/quic-v1":      false,
	} {
		if got := localAddrVanished(ma.StringCast(addr), ips); got != want {
			t.Errorf("%s vanished = %v", addr, got)
		}
	}
}

func TestDirectRetriesBackOffAndResetOnChange(t *testing.T) {
	var r directRetries
	_, id := newKey(t)
	now := time.Now()
	first, due := r.begin(id, now)
	if !first || !due {
		t.Fatalf("first attempt = %v %v", first, due)
	}
	if _, due := r.begin(id, now); due {
		t.Fatal("a second dial started during the first")
	}
	r.finish(id, false, now)
	if _, due := r.begin(id, now.Add(directRetryMin-time.Second)); due {
		t.Fatal("retried before the delay")
	}
	first, due = r.begin(id, now.Add(directRetryMin))
	if first || !due {
		t.Fatalf("retry = %v %v", first, due)
	}
	for range 16 {
		r.finish(id, false, now)
		r.state[id].dialing = true
	}
	if delay := r.state[id].delay; delay != directRetryMax {
		t.Fatalf("delay = %v", delay)
	}
	r.state[id].dialing = false
	r.reset(id)
	if first, due := r.begin(id, now); !first || !due {
		t.Fatalf("after reset = %v %v", first, due)
	}
	// A reset during a dial must not be undone by the dial finishing.
	r.reset(id)
	r.finish(id, false, now)
	if _, due := r.begin(id, now); !due {
		t.Fatal("reset lost to a dial in flight")
	}
	r.finish(id, true, now)
	if len(r.state) != 0 {
		t.Fatalf("state after success = %v", r.state)
	}
}

func TestPeerProbePreservesAnAlreadyHealthyReplacementPath(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA}, Peer{ID: idB})
	a := startDevice(t, keyA, acct, "", "/ip4/127.0.0.1/tcp/0")
	b := startDevice(t, keyB, acct, "", "/ip4/127.0.0.1/tcp/0")
	introduce(a, b)
	if !waitFor(t, time.Second, func() bool { return peerStatus(a, idB).Connected }) {
		t.Fatal("initial connection failed")
	}
	const held = "/multica/held/1"
	done := make(chan struct{})
	b.Handle(held, func(st network.Stream) { defer st.Close(); <-done })
	defer close(done)
	st, err := a.Open(context.Background(), idB, held)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	old := a.host.Network().ConnsToPeer(idB)[0]
	b.host.SetStreamHandler(ping.ID, func(st network.Stream) { defer st.Close(); io.Copy(io.Discard, st) })
	next := startDevice(t, keyB, acct, "", "/ip4/127.0.0.1/tcp/0")
	introduce(next, a)
	if !waitFor(t, time.Second, func() bool { return len(a.host.Network().ConnsToPeer(idB)) == 2 }) {
		t.Fatal("replacement connection not established")
	}
	var replacement network.Conn
	for _, conn := range a.host.Network().ConnsToPeer(idB) {
		if conn.ID() != old.ID() {
			replacement = conn
		}
	}
	a.checkPeerWithin(idB, time.Second)
	if !old.IsClosed() {
		t.Fatal("stale original path retained")
	}
	if replacement.IsClosed() {
		t.Fatal("healthy replacement path closed with stale original")
	}
}

func TestMDNSDiscoveryConnectsDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("uses multicast")
	}
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acct := &testAccount{}
	acct.set(Peer{ID: idA}, Peer{ID: idB})
	// A per-test service name keeps parallel test runs from discovering
	// each other.
	name := "_multica-test-" + idA.String()[len(idA.String())-8:] + "._udp"
	a := startDevice(t, keyA, acct, name)
	startDevice(t, keyB, acct, name)

	if !waitFor(t, 15*time.Second, func() bool {
		return a.host.Network().Connectedness(idB) == network.Connected
	}) {
		t.Skip("no mDNS route on this host; LAN discovery not exercised")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := a.Ping(ctx, idB); err != nil {
		t.Fatalf("ping over mDNS-discovered connection: %v", err)
	}
}

// Regression: a device that registers right after our last refresh used to
// be rejected until the next periodic refresh, because the refresh its
// connection attempt requested fell inside the rate limit and was dropped.
func TestUnknownPeerRefreshIsDeferredNotDropped(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	acctA := &testAccount{}
	acctA.set(Peer{ID: idA})
	acctB := &testAccount{}
	acctB.set(Peer{ID: idA}, Peer{ID: idB})
	a := startDevice(t, keyA, acctA, "")
	b := startDevice(t, keyB, acctB, "")
	// b registers just after a's refresh.
	acctA.set(Peer{ID: idA}, Peer{ID: idB})
	introduce(b, a)

	ok := waitFor(t, minKickInterval+5*time.Second, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err := b.Ping(ctx, idA)
		return err == nil
	})
	if !ok {
		t.Fatal("a never admitted b; its refresh request was dropped")
	}
}

// A waiter woken by OnPeersRead must find the devices it asked about, so a read
// reports only after its list is in place.
func TestOnPeersReadRunsOnceTheListIsInstalled(t *testing.T) {
	t.Parallel()

	key, _ := newKey(t)
	_, other := newKey(t)
	var ready atomic.Bool
	var svc atomic.Pointer[Service]
	failed := make(chan struct{}, 1)
	installed := make(chan bool, 1)
	s, err := Start(context.Background(), Config{Key: key, ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"}, RefreshInterval: time.Hour, Logger: testLogger,
		Fetch: func(context.Context) ([]Peer, error) {
			if !ready.Load() {
				return nil, errors.New("registry down")
			}
			return []Peer{{ID: other, DeviceName: "Mac"}}, nil
		},
		OnPeersRead: func(err error) {
			if err != nil {
				select {
				case failed <- struct{}{}:
				default:
				}
				return
			}
			_, known := svc.Load().Peer(other)
			installed <- known
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	svc.Store(s)
	select {
	case <-failed:
	case <-time.After(10 * time.Second):
		t.Fatal("a failed read was not reported")
	}
	ready.Store(true)
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !<-installed {
		t.Fatal("reported before the list was installed")
	}
}
