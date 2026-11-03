package p2p

import (
	"slices"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	ma "github.com/multiformats/go-multiaddr"
)

// observingConn is the relay's connection as identify reports it.
type observingConn struct {
	network.Conn
	local ma.Multiaddr
}

func (c *observingConn) IsClosed() bool               { return false }
func (c *observingConn) LocalMultiaddr() ma.Multiaddr { return c.local }
func (c *observingConn) RemoteMultiaddr() ma.Multiaddr {
	return ma.StringCast("/ip4/101.33.221.22/tcp/4001")
}

// Behind NAT with no port mapping, one relay's observation is all a device has.
func TestHolePunchingStartsFromTheAddressTheRelayObserved(t *testing.T) {
	key, id := newKey(t)
	self := newAllowlist(id)
	h, err := newHost(key, []string{"/ip4/127.0.0.1/tcp/0"}, &accountGater{allow: self}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	enabled := func() bool { return slices.Contains(h.Mux().Protocols(), holepunch.Protocol) }
	if enabled() {
		t.Fatal("hole punching is on before this host knows a public address")
	}
	var listen ma.Multiaddr
	var port string
	for _, a := range h.Network().ListenAddresses() {
		if p, err := a.ValueForProtocol(ma.P_TCP); err == nil {
			listen, port = a, p
		}
	}
	if listen == nil {
		t.Fatal("no TCP listen address")
	}
	emitter, err := h.EventBus().Emitter(new(event.EvtPeerIdentificationCompleted))
	if err != nil {
		t.Fatal(err)
	}
	defer emitter.Close()
	if err := emitter.Emit(event.EvtPeerIdentificationCompleted{
		Conn:         &observingConn{local: listen},
		ObservedAddr: ma.StringCast("/ip4/8.8.8.8/tcp/" + port),
	}); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 10*time.Second, enabled) {
		t.Fatal("hole punching stayed off after the relay observed a public address")
	}
	// Dialing an address only one observer reported would mislead other devices.
	for _, a := range h.Addrs() {
		if a.String() == "/ip4/8.8.8.8/tcp/"+port {
			t.Fatalf("advertised a single-observer address: %v", h.Addrs())
		}
	}
}
