package p2p

import (
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/sec"
	basichost "github.com/libp2p/go-libp2p/p2p/host/basic"
	"github.com/libp2p/go-libp2p/p2p/host/eventbus"
	"github.com/libp2p/go-libp2p/p2p/host/observedaddrs"
	"github.com/libp2p/go-libp2p/p2p/host/peerstore/pstoremem"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	libp2pconnmgr "github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/net/swarm"
	"github.com/libp2p/go-libp2p/p2p/net/upgrader"
	relayclient "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	libp2ptls "github.com/libp2p/go-libp2p/p2p/security/tls"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/libp2p/go-libp2p/p2p/transport/quicreuse"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/quic-go/quic-go"
)

// newHost assembles a libp2p host from its building blocks instead of
// calling libp2p.New. The top-level constructor links every transport libp2p
// supports, including WebRTC and WebTransport, which added about 5.5MB to the
// stripped CLI for code the daemon never runs. Building the host here keeps
// only QUIC, TCP, TLS/Noise and yamux, plus what cross-network links need:
// the circuit relay v2 client, DCUtR hole punching and UPnP/NAT-PMP port
// mapping. addrs, when set, rewrites the advertised addresses (the service
// appends its relay circuit address).
func newHost(key crypto.PrivKey, listen []string, gater connmgr.ConnectionGater, addrs basichost.AddrsFactory, holePunching ...holepunch.Option) (host.Host, error) {
	self, err := peer.IDFromPrivateKey(key)
	if err != nil {
		return nil, err
	}
	listenAddrs := make([]ma.Multiaddr, 0, len(listen))
	for _, s := range listen {
		a, err := ma.NewMultiaddr(s)
		if err != nil {
			return nil, fmt.Errorf("invalid listen address %q: %w", s, err)
		}
		listenAddrs = append(listenAddrs, a)
	}

	var cleanup []func() error
	fail := func(err error) (host.Host, error) {
		for i := len(cleanup) - 1; i >= 0; i-- {
			cleanup[i]()
		}
		return nil, err
	}

	ps, err := pstoremem.NewPeerstore()
	if err != nil {
		return nil, err
	}
	cleanup = append(cleanup, ps.Close)
	if err := ps.AddPrivKey(self, key); err != nil {
		return fail(err)
	}
	if err := ps.AddPubKey(self, key.GetPublic()); err != nil {
		return fail(err)
	}

	limits := rcmgr.DefaultLimits
	rm, err := rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(limits.AutoScale()))
	if err != nil {
		return fail(err)
	}
	cleanup = append(cleanup, rm.Close)

	// Device counts per account are small; these watermarks only matter if
	// something misbehaves.
	cm, err := libp2pconnmgr.NewConnManager(32, 128)
	if err != nil {
		return fail(err)
	}
	cleanup = append(cleanup, cm.Close)

	muxers := []upgrader.StreamMuxer{{ID: yamux.ID, Muxer: yamux.DefaultTransport}}
	tlsTpt, err := libp2ptls.New(libp2ptls.ID, key, muxers)
	if err != nil {
		return fail(err)
	}
	noiseTpt, err := noise.New(noise.ID, key, muxers)
	if err != nil {
		return fail(err)
	}
	upg, err := upgrader.New([]sec.SecureTransport{tlsTpt, noiseTpt}, muxers, nil, rm, gater)
	if err != nil {
		return fail(err)
	}

	// QUIC reset/token keys only need to be stable for this process's
	// lifetime; connections do not survive a daemon restart anyway.
	var resetKey quic.StatelessResetKey
	var tokenKey quic.TokenGeneratorKey
	if _, err := rand.Read(resetKey[:]); err != nil {
		return fail(err)
	}
	if _, err := rand.Read(tokenKey[:]); err != nil {
		return fail(err)
	}
	quicConns, err := quicreuse.NewConnManager(resetKey, tokenKey)
	if err != nil {
		return fail(err)
	}
	cleanup = append(cleanup, quicConns.Close)

	bus := eventbus.NewBus()
	sw, err := swarm.NewSwarm(self, ps, bus,
		swarm.WithConnectionGater(gater),
		swarm.WithResourceManager(rm),
	)
	if err != nil {
		return fail(err)
	}
	cleanup = append(cleanup, sw.Close)

	quicTpt, err := libp2pquic.NewTransport(key, quicConns, nil, gater, rm)
	if err != nil {
		return fail(err)
	}
	tcpTpt, err := tcp.NewTCPTransport(upg, rm, nil)
	if err != nil {
		return fail(err)
	}
	if err := sw.AddTransport(quicTpt); err != nil {
		return fail(err)
	}
	if err := sw.AddTransport(tcpTpt); err != nil {
		return fail(err)
	}

	// Behind NAT without port mapping, the address the relay observes is this
	// host's only public address, and the hole punching service stays off until
	// it has one.
	observed, err := observedaddrs.NewManager(bus, sw)
	if err != nil {
		return fail(err)
	}
	cleanup = append(cleanup, observed.Close)

	h, err := basichost.NewHost(sw, &basichost.HostOpts{
		EventBus:             bus,
		ConnManager:          cm,
		EnablePing:           true,
		AddrsFactory:         addrs,
		NATManager:           basichost.NewNATManager,
		ObservedAddrsManager: observed,
		EnableHolePunching:   true,
		HolePunchingOptions:  holePunching,
	})
	if err != nil {
		return fail(err)
	}
	// Dials /p2p-circuit addresses and accepts connections relayed to us.
	if err := relayclient.AddTransport(h, upg); err != nil {
		h.Close()
		return fail(err)
	}
	observed.Start(sw)
	if err := sw.Listen(listenAddrs...); err != nil {
		h.Close()
		return fail(err)
	}
	h.Start()
	// The host closes the swarm; the remaining components are ours.
	return &ownedHost{BasicHost: h, closers: []func() error{observed.Close, quicConns.Close, cm.Close, rm.Close, ps.Close}}, nil
}

// ownedHost closes the components newHost created alongside the host.
type ownedHost struct {
	*basichost.BasicHost
	closers []func() error
}

func (h *ownedHost) Close() error {
	errs := []error{h.BasicHost.Close()}
	for _, c := range h.closers {
		errs = append(errs, c())
	}
	return errors.Join(errs...)
}
