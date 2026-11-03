package p2p

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/multica-ai/multica/server/internal/p2pserver"
)

// relayAccount is a device list whose addresses are read live from the
// running services, the way the registry serves what devices last reported.
type relayAccount struct {
	mu    sync.Mutex
	svcs  []*Service
	only  func(string) bool // filters which reported addresses are served
	extra map[peer.ID][]ma.Multiaddr
}

func (a *relayAccount) fetch(context.Context) ([]Peer, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []Peer
	for _, s := range a.svcs {
		p := Peer{ID: s.ID()}
		for _, addr := range s.AdvertisedAddrs() {
			if a.only != nil && !a.only(addr) {
				continue
			}
			if m, err := ma.NewMultiaddr(addr); err == nil {
				p.RegisteredAddrs = append(p.RegisteredAddrs, m)
			}
		}
		p.RegisteredAddrs = append(p.RegisteredAddrs, a.extra[p.ID]...)
		out = append(out, p)
	}
	return out, nil
}

func startRelay(t *testing.T, devices ...peer.ID) (*p2pserver.Relay, *p2pserver.Store) {
	t.Helper()
	store, err := p2pserver.OpenStore(filepath.Join(t.TempDir(), "p2p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	key, _ := newKey(t)
	relay, err := p2pserver.StartRelay(p2pserver.RelayConfig{Key: key, ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"}, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { relay.Close() })
	for i, id := range devices {
		pub, err := id.ExtractPublicKey()
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := crypto.MarshalPublicKey(pub)
		if _, err := store.Upsert(context.Background(), p2pserver.Device{
			UserID: "user-1", DaemonID: "d" + string(rune('a'+i)), PeerID: id.String(),
			PublicKey: base64.StdEncoding.EncodeToString(raw),
		}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	return relay, store
}

func relayFunc(r *p2pserver.Relay) RelayFunc {
	return func(context.Context) (*peer.AddrInfo, error) {
		info := r.Info()
		id, err := peer.Decode(info.PeerID)
		if err != nil {
			return nil, err
		}
		ai := &peer.AddrInfo{ID: id}
		for _, s := range info.Addrs {
			a, err := ma.NewMultiaddr(s)
			if err != nil {
				return nil, err
			}
			ai.Addrs = append(ai.Addrs, a)
		}
		return ai, nil
	}
}

func TestDevicesWithoutDirectRouteConnectThroughRelay(t *testing.T) {
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	relay, _ := startRelay(t, idA, idB)

	// Each device registers only its relay circuit address, so the only way
	// between them is the relay; loopback is not advertised.
	acct := &relayAccount{only: func(a string) bool { return strings.Contains(a, "/p2p-circuit") }}
	addrsChanged := make(chan struct{}, 8)
	start := func(key crypto.PrivKey) *Service {
		svc, err := Start(context.Background(), Config{
			Key:             key,
			Fetch:           acct.fetch,
			ListenAddrs:     []string{"/ip4/127.0.0.1/tcp/0"},
			RefreshInterval: time.Hour,
			Relay:           relayFunc(relay),
			OnAddrsChanged: func() {
				select {
				case addrsChanged <- struct{}{}:
				default:
				}
			},
			Logger: testLogger,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { svc.Close() })
		return svc
	}
	a := start(keyA)
	b := start(keyB)
	acct.mu.Lock()
	acct.svcs = []*Service{a, b}
	acct.mu.Unlock()

	hasCircuit := func(s *Service) bool {
		for _, addr := range s.AdvertisedAddrs() {
			if strings.Contains(addr, "/p2p/"+relay.Info().PeerID+"/p2p-circuit") {
				return true
			}
		}
		return false
	}
	if !waitFor(t, 15*time.Second, func() bool { return hasCircuit(a) && hasCircuit(b) }) {
		t.Fatalf("no circuit address advertised: a=%v b=%v", a.AdvertisedAddrs(), b.AdvertisedAddrs())
	}
	select {
	case <-addrsChanged:
	case <-time.After(10 * time.Second):
		t.Fatal("OnAddrsChanged not called after the reservation")
	}
	if st := a.Status().Relay; st == nil || !st.Reserved || st.PeerID != relay.Info().PeerID {
		t.Fatalf("relay status = %+v", st)
	}

	const echo = "/multica/test-echo/1"
	a.Handle(echo, func(s network.Stream) {
		defer s.Close()
		io.Copy(s, s)
	})
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 15*time.Second, func() bool { return peerStatus(b, idA).Connected }) {
		t.Fatalf("b did not connect to a: %+v", b.Status())
	}
	if c := peerStatus(b, idA).Connection; c != ConnectionRelayed {
		t.Fatalf("connection = %q, want relayed", c)
	}

	// An available relay must carry application traffic without waiting for hole punching.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s, err := b.Dial(ctx, idA, echo)
	if err != nil {
		t.Fatalf("open through relay: %v", err)
	}
	// More than the relay's default 128KB data limit, which this relay lifts.
	payload := bytes.Repeat([]byte("multica "), 64<<10)
	go func() {
		s.Write(payload)
		s.(streamConn).CloseWrite()
	}()
	got, err := io.ReadAll(s)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("echo through relay: %d bytes, err %v", len(got), err)
	}
}

func TestRelayNodeCannotOpenDeviceStreams(t *testing.T) {
	keyA, idA := newKey(t)
	relay, _ := startRelay(t, idA)
	acct := &relayAccount{}
	a, err := Start(context.Background(), Config{
		Key: keyA, Fetch: acct.fetch, ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"},
		RefreshInterval: time.Hour, Relay: relayFunc(relay), Logger: testLogger,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	const proto = "/multica/test-private/1"
	a.Handle(proto, func(s network.Stream) {
		s.Write([]byte("secret"))
		s.Close()
	})
	if !waitFor(t, 15*time.Second, func() bool { st := a.Status().Relay; return st != nil && st.Reserved }) {
		t.Fatal("no reservation")
	}
	s, err := relay.Host().NewStream(context.Background(), idA, proto)
	if err != nil {
		return // refused at negotiation: fine
	}
	defer s.Close()
	if got, _ := io.ReadAll(s); len(got) != 0 {
		t.Fatalf("relay node read %q from a device-only protocol", got)
	}
}

func TestReservationIsRestoredAfterRelayDisconnect(t *testing.T) {
	keyA, idA := newKey(t)
	relay, _ := startRelay(t, idA)
	a, err := Start(context.Background(), Config{
		Key: keyA, Fetch: (&relayAccount{}).fetch, ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"},
		RefreshInterval: time.Hour, Relay: relayFunc(relay), Logger: testLogger,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	reserved := func() bool { st := a.Status().Relay; return st != nil && st.Reserved }
	if !waitFor(t, 15*time.Second, reserved) {
		t.Fatal("no reservation")
	}
	relay.Host().Network().ClosePeer(idA)
	// The reservation renews hours later; a dropped relay connection must
	// not wait for that.
	if !waitFor(t, 15*time.Second, func() bool {
		return reserved() && relay.Host().Network().Connectedness(idA) == network.Connected
	}) {
		t.Fatalf("reservation not restored: %+v", a.Status().Relay)
	}
}

func TestUnresponsiveRelayConnectionRenewsReservation(t *testing.T) {
	key, id := newKey(t)
	relay, _ := startRelay(t, id)
	a, err := Start(context.Background(), Config{
		Key: key, Fetch: (&relayAccount{}).fetch, ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"},
		RefreshInterval: time.Hour, Relay: relayFunc(relay), Logger: testLogger,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	reserved := func() bool { st := a.Status().Relay; return st != nil && st.Reserved }
	if !waitFor(t, 15*time.Second, reserved) {
		t.Fatal("no reservation")
	}
	conns := a.host.Network().ConnsToPeer(relay.Host().ID())
	relay.Host().SetStreamHandler(ping.ID, func(st network.Stream) { defer st.Close(); _, _ = io.Copy(io.Discard, st) })
	a.checkPeer(relay.Host().ID())
	for _, c := range conns {
		if !c.IsClosed() {
			t.Fatal("unresponsive relay connection remained cached")
		}
	}
	if !waitFor(t, 15*time.Second, func() bool {
		return reserved() && relay.Host().Network().Connectedness(id) == network.Connected
	}) {
		t.Fatal("relay reservation was not restored after the failed probe")
	}
}

func peerStatus(s *Service, id peer.ID) PeerStatus {
	for _, p := range s.Peers() {
		if p.ID == id {
			return p
		}
	}
	return PeerStatus{}
}

var testLogger = slog.New(slog.DiscardHandler)
