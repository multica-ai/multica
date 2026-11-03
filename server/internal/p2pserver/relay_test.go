package p2pserver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	ma "github.com/multiformats/go-multiaddr"
)

const echoProto = "/multica-test/echo/1"

type relayHarness struct {
	store *Store
	relay *Relay
}

func newRelayHarness(t *testing.T, maxPerUser int) *relayHarness {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "p2p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	r, err := StartRelay(RelayConfig{
		Key:                key,
		ListenAddrs:        []string{"/ip4/127.0.0.1/tcp/0"},
		Store:              store,
		MaxCircuitsPerUser: maxPerUser,
		Logger:             discardLogger,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return &relayHarness{store: store, relay: r}
}

func (rh *relayHarness) relayInfo() peer.AddrInfo {
	return peer.AddrInfo{ID: rh.relay.Host().ID(), Addrs: rh.relay.Host().Addrs()}
}

// newDevice starts a host that can only be reached through the relay: it
// has no listen addresses of its own. When user is set it is registered.
func (rh *relayHarness) newDevice(t *testing.T, user, daemonID string) host.Host {
	t.Helper()
	key, pub, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h, err := libp2p.New(libp2p.Identity(key), libp2p.NoListenAddrs, libp2p.DisableMetrics(), libp2p.EnableRelay())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	h.SetStreamHandler(echoProto, func(s network.Stream) {
		defer s.Close()
		io.Copy(s, s)
	})
	if user != "" {
		raw, _ := crypto.MarshalPublicKey(pub)
		_, _, err := rh.store.Upsert(context.Background(), Device{
			UserID: user, DaemonID: daemonID, PeerID: h.ID().String(),
			PublicKey: base64.StdEncoding.EncodeToString(raw),
		}, time.Now(), time.Time{})
		if err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func (rh *relayHarness) reserve(t *testing.T, h host.Host) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.Connect(ctx, rh.relayInfo()); err != nil {
		t.Fatal(err)
	}
	_, err := client.Reserve(ctx, h, rh.relayInfo())
	return err
}

// echoVia dials dst through the relay and round-trips a message.
func (rh *relayHarness) echoVia(src, dst host.Host) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	circuit, err := ma.NewMultiaddr("/p2p/" + rh.relay.Host().ID().String() + "/p2p-circuit")
	if err != nil {
		return err
	}
	var addrs []ma.Multiaddr
	for _, a := range rh.relay.Host().Addrs() {
		addrs = append(addrs, a.Encapsulate(circuit))
	}
	src.Peerstore().AddAddrs(dst.ID(), addrs, peerstore.TempAddrTTL)
	if err := src.Connect(ctx, peer.AddrInfo{ID: dst.ID()}); err != nil {
		return err
	}
	s, err := src.NewStream(network.WithAllowLimitedConn(ctx, "test"), dst.ID(), echoProto)
	if err != nil {
		return err
	}
	defer s.Close()
	if _, err := s.Write([]byte("ping")); err != nil {
		return err
	}
	s.CloseWrite()
	got, err := io.ReadAll(s)
	if err != nil {
		return err
	}
	if string(got) != "ping" {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func TestRelayReservationRequiresRegistration(t *testing.T) {
	rh := newRelayHarness(t, DefaultMaxCircuitsPerUser)
	if err := rh.reserve(t, rh.newDevice(t, "", "")); err == nil {
		t.Fatal("unregistered peer got a reservation")
	}
	if err := rh.reserve(t, rh.newDevice(t, "user-alice", "m1")); err != nil {
		t.Fatalf("registered peer: %v", err)
	}
}

func TestRelayLinksSameAccountOnly(t *testing.T) {
	rh := newRelayHarness(t, DefaultMaxCircuitsPerUser)
	a := rh.newDevice(t, "user-alice", "m1")
	b := rh.newDevice(t, "user-alice", "m2")
	mallory := rh.newDevice(t, "user-bob", "m3")
	if err := rh.reserve(t, a); err != nil {
		t.Fatal(err)
	}
	if err := rh.echoVia(b, a); err != nil {
		t.Fatalf("same account through relay: %v", err)
	}
	conns := b.Network().ConnsToPeer(a.ID())
	if len(conns) == 0 || !isCircuit(conns[0].RemoteMultiaddr()) {
		t.Fatal("expected a relayed connection")
	}
	// Relayed connections are a lasting fallback, so the relay sets no
	// duration or data limit and the connection is not marked limited.
	if conns[0].Stat().Limited {
		t.Fatal("relayed connection is limited")
	}
	if err := rh.echoVia(mallory, a); err == nil {
		t.Fatal("another account reached alice's device through the relay")
	}
}

func TestRelayCapsCircuitsPerAccount(t *testing.T) {
	rh := newRelayHarness(t, 1)
	a := rh.newDevice(t, "user-alice", "m1")
	b := rh.newDevice(t, "user-alice", "m2")
	c := rh.newDevice(t, "user-alice", "m3")
	bob := rh.newDevice(t, "user-bob", "b1")
	bob2 := rh.newDevice(t, "user-bob", "b2")
	for _, h := range []host.Host{a, bob} {
		if err := rh.reserve(t, h); err != nil {
			t.Fatal(err)
		}
	}
	if err := rh.echoVia(b, a); err != nil {
		t.Fatalf("first circuit: %v", err)
	}
	if err := rh.echoVia(c, a); err == nil {
		t.Fatal("second circuit for the account was admitted past the limit")
	}
	// Another account's budget is separate.
	if err := rh.echoVia(bob2, bob); err != nil {
		t.Fatalf("bob's first circuit: %v", err)
	}
	// Closing the circuit frees the slot. A fresh device avoids c's dial
	// backoff from the refused attempt.
	b.Network().ClosePeer(a.ID())
	d := rh.newDevice(t, "user-alice", "m4")
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := rh.echoVia(d, a)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("slot not freed after close: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
		d = rh.newDevice(t, "user-alice", "m4")
	}
}

func isCircuit(a ma.Multiaddr) bool {
	_, err := a.ValueForProtocol(ma.P_CIRCUIT)
	return err == nil
}

func TestRelayForgetDropsDevice(t *testing.T) {
	rh := newRelayHarness(t, DefaultMaxCircuitsPerUser)
	a := rh.newDevice(t, "user-alice", "m1")
	b := rh.newDevice(t, "user-alice", "m2")
	if err := rh.reserve(t, a); err != nil {
		t.Fatal(err)
	}
	if err := rh.echoVia(b, a); err != nil {
		t.Fatal(err)
	}
	if _, err := rh.store.Delete(context.Background(), "user-alice", "m1", ""); err != nil {
		t.Fatal(err)
	}
	rh.relay.Forget(a.ID().String())
	deadline := time.Now().Add(5 * time.Second)
	for b.Network().Connectedness(a.ID()) == network.Connected {
		if time.Now().After(deadline) {
			t.Fatal("relayed connection survived device removal")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := rh.reserve(t, a); err == nil {
		t.Fatal("removed device could reserve again")
	}
}
