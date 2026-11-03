package p2p

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/net/swarm"
	relayclient "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"

	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

// DefaultMDNSServiceName keeps Multica's LAN announcements apart from other
// libp2p applications on the same network.
const DefaultMDNSServiceName = "_multica-p2p._udp"

const (
	defaultRefreshInterval = time.Minute
	// minKickInterval rate-limits refreshes triggered by unknown peers, so a
	// stranger repeatedly connecting cannot turn into server load. A request
	// inside the window is deferred, not dropped: the unknown peer is often
	// a device that registered seconds after our last refresh.
	minKickInterval = 5 * time.Second
	connectTimeout  = 10 * time.Second
	// addrsDebounce coalesces the address churn of startup, port mapping
	// and relay reservation into one re-registration.
	addrsDebounce = 250 * time.Millisecond
	// relayRenewMargin renews a relay reservation this long before expiry.
	relayRenewMargin = 5 * time.Minute
	// relayRecheckInterval is how often a registry without a relay is asked
	// again.
	relayRecheckInterval = 10 * time.Minute
	peerProbeInterval    = 15 * time.Second
	peerProbeTimeout     = 5 * time.Second
	peerRetryMax         = 30 * time.Second
)

var relayRetrySchedule = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}

// RelayFunc returns the relay node to reserve a slot on, or nil when the
// registry offers none.
type RelayFunc func(ctx context.Context) (*peer.AddrInfo, error)

// ErrNotAllowed means the peer is not a device of this account.
var ErrNotAllowed = errors.New("p2p: peer is not a device of this account")

// Config configures a Service.
type Config struct {
	Key   crypto.PrivKey
	Fetch FetchFunc
	// Port is used for both TCP and QUIC on every interface; 0 picks a random
	// port per transport. Ignored when ListenAddrs is set.
	Port int
	// ListenAddrs overrides the listen multiaddrs (tests bind to loopback).
	ListenAddrs []string
	// MDNSServiceName enables LAN discovery under this service name; empty
	// disables it.
	MDNSServiceName string
	RefreshInterval time.Duration
	// Relay enables reachability across networks: the service keeps a
	// reservation on the relay, advertises its circuit address, and devices
	// that cannot dial each other directly connect through it and then try
	// to hole-punch. Nil keeps the service LAN-only.
	Relay RelayFunc
	// OnAddrsChanged is called, debounced, when AdvertisedAddrs changes, so
	// the new addresses can be registered.
	OnAddrsChanged func()
	// OnPeersRead is called after each device list read, with the read's error;
	// on success the list is already in place.
	OnPeersRead func(err error)
	Logger      *slog.Logger
	// Marks records connection state changes; nil records nothing.
	Marks *timing.Marks
}

type peerDial struct {
	wake   chan struct{}
	cancel context.CancelFunc
}

// Service is this device's endpoint on the account's peer-to-peer network.
type Service struct {
	marks      *timing.Marks
	connState  connectionStates
	fetchState fetchState
	stopReason atomic.Value

	host     host.Host
	allow    *allowlist
	fetch    FetchFunc
	logger   *slog.Logger
	interval time.Duration
	mdns     mdns.Service

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	kick   chan struct{}

	refreshMu   sync.Mutex
	lastRefresh time.Time
	dialMu      sync.Mutex
	dialing     map[peer.ID]*peerDial

	discoveredMu sync.Mutex
	discovered   map[peer.ID]peer.AddrInfo

	relayFetch RelayFunc
	onAddrs    func()
	onPeers    func(error)
	relayLost  chan struct{}
	probePeers chan struct{}

	relayMu       sync.Mutex
	relayInfo     *peer.AddrInfo
	relayReserved bool
	relayExpires  time.Time
	relayCancel   context.CancelFunc
}

// Start builds the libp2p host, starts LAN discovery and the allowlist
// refresh loop. The first refresh runs in the background; until it finishes
// no peer is admitted.
func Start(ctx context.Context, cfg Config) (*Service, error) {
	if cfg.Key == nil || cfg.Fetch == nil {
		return nil, errors.New("p2p: key and fetch are required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	self, err := peer.IDFromPrivateKey(cfg.Key)
	if err != nil {
		return nil, err
	}
	listen := cfg.ListenAddrs
	if len(listen) == 0 {
		port := strconv.Itoa(cfg.Port)
		listen = []string{
			"/ip4/0.0.0.0/udp/" + port + "/quic-v1",
			"/ip6/::/udp/" + port + "/quic-v1",
			"/ip4/0.0.0.0/tcp/" + port,
			"/ip6/::/tcp/" + port,
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	began := cfg.Marks.Now()
	cfg.Marks.Write(timing.Record{Stage: timing.StageLinkStart})
	s := &Service{
		marks:      cfg.Marks,
		allow:      newAllowlist(self),
		fetch:      cfg.Fetch,
		logger:     logger,
		interval:   cfg.RefreshInterval,
		ctx:        ctx,
		cancel:     cancel,
		kick:       make(chan struct{}, 1),
		dialing:    map[peer.ID]*peerDial{},
		discovered: map[peer.ID]peer.AddrInfo{},
		relayFetch: cfg.Relay,
		onAddrs:    cfg.OnAddrsChanged,
		onPeers:    cfg.OnPeersRead,
		relayLost:  make(chan struct{}, 1),
		probePeers: make(chan struct{}, 1),
	}
	if s.interval <= 0 {
		s.interval = defaultRefreshInterval
	}
	gater := &accountGater{allow: s.allow, isRelay: s.isRelay, onUnknown: func(peer.ID) { s.requestRefresh() }}

	tracer := &holePunchMarks{marks: cfg.Marks}
	h, err := newHost(cfg.Key, listen, gater, s.withCircuitAddrs, holepunch.WithMetricsAndEventTracer(tracer, tracer))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("p2p: start host: %w", err)
	}
	s.host = h

	if cfg.MDNSServiceName != "" {
		s.mdns = mdns.NewMdnsService(h, cfg.MDNSServiceName, s)
		if err := s.mdns.Start(); err != nil {
			s.Close()
			return nil, fmt.Errorf("p2p: start mdns: %w", err)
		}
	}

	addrSub, err := h.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated))
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("p2p: subscribe to address updates: %w", err)
	}
	h.Network().Notify(&network.NotifyBundle{ConnectedF: func(_ network.Network, c network.Conn) { s.noteConnection(c, true) }, DisconnectedF: func(n network.Network, c network.Conn) {
		s.noteConnection(c, false)
		s.onDisconnected(n, c)
	}})
	s.wg.Add(3)
	go func() {
		defer s.wg.Done()
		s.refreshLoop()
	}()
	go func() {
		defer s.wg.Done()
		defer addrSub.Close()
		s.addrsLoop(addrSub.Out())
	}()
	go func() {
		defer s.wg.Done()
		s.checkPeersLoop()
	}()
	if s.relayFetch != nil {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.relayLoop()
		}()
	}
	logger.Info("p2p: host started", "peer_id", self.String(), "addrs", h.Addrs())
	s.marks.Since(timing.Record{Stage: timing.StageLinkReady}, began, nil)
	return s, nil
}

// CloseBecause is Close with a closed-enum reason on its record, such as a phone
// that stopped the link because the app went to the background.
func (s *Service) CloseBecause(reason string) error {
	s.stopReason.Store(reason)
	return s.Close()
}

// Close stops discovery and the host, closing every connection.
func (s *Service) Close() error {
	reason, _ := s.stopReason.Load().(string)
	if reason == "" {
		reason = timing.ReasonStopped
	}
	s.marks.Write(timing.Record{Stage: timing.StageLinkStop, Reason: reason})
	s.dialMu.Lock()
	s.cancel()
	s.dialMu.Unlock()
	if s.mdns != nil {
		s.mdns.Close()
	}
	err := s.host.Close()
	s.wg.Wait()
	return err
}

// ID returns this device's peer ID.
func (s *Service) ID() peer.ID { return s.host.ID() }

// Handle serves inbound streams for protocol from devices of this account,
// whether connected directly or through the relay. The gater keeps
// strangers from connecting at all; the relay node itself can connect, so
// streams are checked here too.
func (s *Service) Handle(p protocol.ID, h network.StreamHandler) {
	s.host.SetStreamHandler(p, func(st network.Stream) {
		if !s.allow.allowed(st.Conn().RemotePeer()) {
			st.Reset()
			return
		}
		h(st)
	})
}

// Open opens a stream to a device of this account, over a direct or relayed
// connection.
func (s *Service) Open(ctx context.Context, p peer.ID, proto protocol.ID) (network.Stream, error) {
	if !s.allow.allowed(p) {
		return nil, ErrNotAllowed
	}
	return s.host.NewStream(network.WithAllowLimitedConn(ctx, "multica-device-link"), p, proto)
}

// Ping measures the round trip to a device using libp2p's ping protocol.
func (s *Service) Ping(ctx context.Context, p peer.ID) (time.Duration, error) {
	if !s.allow.allowed(p) {
		return 0, ErrNotAllowed
	}
	ctx, cancel := context.WithCancel(network.WithAllowLimitedConn(ctx, "multica-device-link"))
	defer cancel()
	select {
	case res := <-ping.Ping(ctx, s.host, p):
		return res.RTT, res.Error
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// AddAddrs records addresses for a peer learned outside mDNS and the
// registry.
func (s *Service) AddAddrs(info peer.AddrInfo) { s.addDiscovered(info) }

// HandlePeerFound implements mdns.Notifee. Addresses are remembered even for
// peers not yet on the allowlist, because mDNS reports each peer once and the
// device may register on the server after it was seen.
func (s *Service) HandlePeerFound(info peer.AddrInfo) {
	if info.ID != s.host.ID() && len(info.Addrs) > 0 {
		s.marks.Write(timing.Record{Stage: timing.StagePeerMDNSFound, Peer: info.ID.String(), AddrType: bestAddrType(info.Addrs)})
	}
	s.addDiscovered(info)
}

func (s *Service) addDiscovered(info peer.AddrInfo) {
	if info.ID == s.host.ID() || len(info.Addrs) == 0 {
		return
	}
	s.discoveredMu.Lock()
	s.discovered[info.ID] = info
	s.discoveredMu.Unlock()
	if s.allow.allowed(info.ID) {
		s.connect(info)
	}
}

func (s *Service) connect(info peer.AddrInfo) {
	s.host.Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.AddressTTL)
	if s.host.Network().Connectedness(info.ID) == network.Connected {
		return
	}
	s.dialMu.Lock()
	if s.ctx.Err() != nil || s.dialing[info.ID] != nil {
		s.dialMu.Unlock()
		return
	}
	job := &peerDial{wake: make(chan struct{}, 1)}
	s.dialing[info.ID] = job
	s.wg.Add(1)
	s.dialMu.Unlock()
	go func() {
		defer s.wg.Done()
		defer func() {
			s.dialMu.Lock()
			delete(s.dialing, info.ID)
			pending := false
			select {
			case <-job.wake:
				pending = true
			default:
			}
			s.dialMu.Unlock()
			// Do not lose a network event racing with a successful dial's exit.
			if pending && s.ctx.Err() == nil {
				s.connect(info)
			}
		}()
		attempts := int64(0)
		began := s.marks.Now()
		for delay := minKickInterval; s.ctx.Err() == nil; delay = min(delay*2, peerRetryMax) {
			p, allowed := s.allow.get(info.ID)
			if !allowed || s.host.Network().Connectedness(info.ID) == network.Connected {
				return
			}
			// Rebuild the target each attempt: a device keeps its identity when its network changes.
			target := peer.AddrInfo{ID: info.ID, Addrs: append([]ma.Multiaddr(nil), p.RegisteredAddrs...)}
			s.discoveredMu.Lock()
			target.Addrs = append(target.Addrs, s.discovered[info.ID].Addrs...)
			s.discoveredMu.Unlock()
			if len(target.Addrs) == 0 {
				return
			}
			// One start and the first failure say a device is unreachable; every
			// later retry would only tick while it stays that way.
			if attempts == 0 {
				s.marks.Write(timing.Record{Stage: timing.StagePeerConnect, Event: timing.EventStart, Peer: info.ID.String(), AddrType: bestAddrType(target.Addrs), Count: timing.N(int64(len(target.Addrs)))})
			}
			attempts++
			// This loop owns the bounded retry delay; old transport backoff must not hide recovery.
			if sw, ok := s.host.Network().(*swarm.Swarm); ok {
				sw.Backoff().Clear(info.ID)
			}
			s.dialMu.Lock()
			// A network event interrupts both the current dial and its retry delay.
			select {
			case <-job.wake:
				s.dialMu.Unlock()
				continue
			default:
			}
			ctx, cancel := context.WithTimeout(network.WithAllowLimitedConn(s.ctx, "multica-device-link"), connectTimeout)
			job.cancel = cancel
			s.dialMu.Unlock()
			err := s.host.Connect(ctx, target)
			cancel()
			s.dialMu.Lock()
			job.cancel = nil
			s.dialMu.Unlock()
			if err == nil {
				s.marks.Since(timing.Record{Stage: timing.StagePeerConnect, Peer: info.ID.String(), AddrType: s.connectedAddrType(info.ID), Count: timing.N(attempts)}, began, nil)
				return
			}
			if s.ctx.Err() != nil {
				return
			}
			if attempts == 1 {
				s.marks.Since(timing.Record{Stage: timing.StagePeerConnect, Peer: info.ID.String(), Count: timing.N(attempts)}, began, err)
				s.noteCircuitRefused(err)
			}
			s.logger.Debug("p2p: connect failed; retrying", "peer_id", info.ID.String(), "error", err, "retry_in", delay)
			s.requestRefresh()
			timer := time.NewTimer(delay)
			select {
			case <-s.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			case <-job.wake:
				timer.Stop()
				delay = minKickInterval / 2
			}
		}
	}()
}

// Probe established paths too: a network switch can leave a TCP connection
// looking connected until a read/write eventually times out.
func (s *Service) checkPeersLoop() {
	ticker := time.NewTicker(peerProbeInterval)
	defer ticker.Stop()
	for {
		timeout := peerProbeTimeout
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		case <-s.probePeers:
			timeout = time.Second
		}
		s.relayMu.Lock()
		var relayID peer.ID
		if s.relayInfo != nil {
			relayID = s.relayInfo.ID
		}
		s.relayMu.Unlock()
		var probes sync.WaitGroup
		probe := func(id peer.ID) {
			probes.Add(1)
			go func() { defer probes.Done(); s.checkPeerWithin(id, timeout) }()
		}
		if relayID != "" {
			probe(relayID)
		}
		for _, p := range s.allow.list() {
			probe(p.ID)
		}
		probes.Wait()
	}
}

func (s *Service) checkPeer(id peer.ID) { s.checkPeerWithin(id, peerProbeTimeout) }

func (s *Service) checkPeerWithin(id peer.ID, timeout time.Duration) {
	if !s.allow.allowed(id) && !s.isRelay(id) {
		return
	}
	var probes sync.WaitGroup
	for _, conn := range s.host.Network().ConnsToPeer(id) {
		probes.Add(1)
		go func() { defer probes.Done(); s.checkConnection(conn, timeout) }()
	}
	probes.Wait()
}

func (s *Service) checkConnection(conn network.Conn, timeout time.Duration) {
	if conn.IsClosed() {
		return
	}
	id := conn.RemotePeer()
	ctx, cancel := context.WithTimeout(network.WithAllowLimitedConn(s.ctx, "multica-peer-probe"), timeout)
	var err error
	select {
	case result, ok := <-ping.Ping(ctx, connectionProbeHost{Host: s.host, conn: conn}, id):
		if !ok {
			err = errors.New("p2p: ping ended before a response")
		} else {
			err = result.Error
		}
	case <-ctx.Done():
		err = ctx.Err()
	}
	cancel()
	if err == nil || s.ctx.Err() != nil {
		return
	}
	s.logger.Info("p2p: peer path stopped responding; reconnecting", "peer_id", id.String(), "connection_id", conn.ID(), "error", err)
	_ = conn.Close()
	s.requestRefresh()
	if p, allowed := s.allow.get(id); allowed {
		s.connect(peer.AddrInfo{ID: id, Addrs: p.RegisteredAddrs})
	}
}

// PeersChanged refreshes after an authenticated registry invalidation. It also
// checks stale paths and interrupts dial backoff even if a relay address is unchanged.
func (s *Service) PeersChanged(ctx context.Context) error {
	if err := s.Refresh(ctx); err != nil {
		return err
	}
	s.wakeDials()
	select {
	case s.probePeers <- struct{}{}:
	default:
	}
	return nil
}

func (s *Service) wakeDials(ids ...peer.ID) {
	s.dialMu.Lock()
	defer s.dialMu.Unlock()
	for id, job := range s.dialing {
		if len(ids) > 0 && !slices.Contains(ids, id) {
			continue
		}
		if job.cancel != nil {
			job.cancel()
		}
		select {
		case job.wake <- struct{}{}:
		default:
		}
	}
}

// NetworkChanged is called when local interfaces or routes change, rather than
// waiting for the old TCP/QUIC paths to time out. New incoming paths are retained.
func (s *Service) NetworkChanged() {
	s.marks.Write(timing.Record{Stage: timing.StageNetworkChanged})
	conns := s.host.Network().Conns()
	s.relayMu.Lock()
	if s.relayCancel != nil {
		s.relayCancel()
	}
	s.relayMu.Unlock()
	s.logger.Info("p2p: local network changed; rebuilding paths")
	for _, conn := range conns {
		_ = conn.Close()
	}
	s.wakeDials()
	select {
	case s.relayLost <- struct{}{}:
	default:
	}
	s.requestRefresh()
	if s.onAddrs != nil {
		s.onAddrs()
	}
}

func (s *Service) requestRefresh() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

func (s *Service) refreshLoop() {
	if err := s.Refresh(s.ctx); err != nil && s.ctx.Err() == nil {
		s.logger.Warn("p2p: initial allowlist refresh failed", "error", err)
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	// deferred fires a kick that arrived inside the rate-limit window.
	var deferred <-chan time.Time
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		case <-deferred:
			deferred = nil
		case <-s.kick:
			if deferred != nil {
				continue
			}
			s.refreshMu.Lock()
			wait := minKickInterval - time.Since(s.lastRefresh)
			s.refreshMu.Unlock()
			if wait > 0 {
				deferred = time.After(wait)
				continue
			}
		}
		if err := s.Refresh(s.ctx); err != nil && s.ctx.Err() == nil {
			s.logger.Warn("p2p: allowlist refresh failed", "error", err)
		}
	}
}

// Refresh reloads the allowlist now. Devices that left the account are
// disconnected; listed devices that are not connected are dialed on the
// addresses they registered and any seen on the LAN.
func (s *Service) Refresh(ctx context.Context) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.lastRefresh = time.Now()
	fetching := s.marks.Now()
	peers, err := s.fetch(ctx)
	s.noteFetch(fetching, err)
	if err != nil {
		if s.onPeers != nil {
			s.onPeers(err)
		}
		return err
	}
	var changed []peer.ID
	for _, p := range peers {
		if previous, ok := s.allow.get(p.ID); ok && !slices.EqualFunc(previous.RegisteredAddrs, p.RegisteredAddrs, func(a, b ma.Multiaddr) bool { return a.Equal(b) }) {
			changed = append(changed, p.ID)
			if s.Connection(p.ID) == ConnectionRelayed {
				s.dialDirect(p)
			}
		}
	}
	added, removed := s.allow.replace(peers)
	if s.onPeers != nil {
		s.onPeers(nil)
	}
	for _, id := range added {
		s.marks.Write(timing.Record{Stage: timing.StagePeerKnown, Peer: id.String()})
	}
	for _, id := range removed {
		s.host.Network().ClosePeer(id)
	}
	s.discoveredMu.Lock()
	var infos []peer.AddrInfo
	for _, p := range s.allow.list() {
		if s.host.Network().Connectedness(p.ID) == network.Connected {
			continue
		}
		info := peer.AddrInfo{ID: p.ID, Addrs: append([]ma.Multiaddr(nil), p.RegisteredAddrs...)}
		if d, ok := s.discovered[p.ID]; ok {
			info.Addrs = append(info.Addrs, d.Addrs...)
		}
		if len(info.Addrs) > 0 {
			infos = append(infos, info)
		}
	}
	s.discoveredMu.Unlock()
	for _, info := range infos {
		s.connect(info)
	}
	if len(changed) > 0 {
		s.wakeDials(changed...)
	}
	return nil
}

// dialDirect tries a relayed device's newly registered direct addresses once.
// connect treats a relayed device as connected, and hole punching ignores
// private addresses, so nothing else would reach a LAN address registered late.
func (s *Service) dialDirect(p Peer) {
	addrs := slices.DeleteFunc(slices.Clone(p.RegisteredAddrs), isCircuit)
	if len(addrs) == 0 {
		return
	}
	s.dialMu.Lock()
	if s.ctx.Err() != nil {
		s.dialMu.Unlock()
		return
	}
	s.wg.Add(1)
	s.dialMu.Unlock()
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(network.WithForceDirectDial(s.ctx, "multica-registered-direct"), connectTimeout)
		defer cancel()
		began := s.marks.Now()
		err := s.host.Connect(ctx, peer.AddrInfo{ID: p.ID, Addrs: addrs})
		s.marks.Since(timing.Record{Stage: timing.StagePeerConnect, Peer: p.ID.String(), AddrType: bestAddrType(addrs), Reason: timing.ReasonLateDirect}, began, err)
		if err != nil {
			s.logger.Debug("p2p: direct dial failed; staying on the relay", "peer_id", p.ID.String(), "error", err)
		}
	}()
}

// AdvertisedAddrs are the addresses to register for other devices: listen,
// port-mapped and observed public addresses plus the relay circuit address
// while a reservation is held. Loopback and link-local addresses are left
// out since no other device can use them.
func (s *Service) AdvertisedAddrs() []string {
	out := []string{}
	for _, a := range s.host.Addrs() {
		// A circuit address starts with the relay's address, which says
		// nothing about this device's own interfaces.
		if isCircuit(a) {
			out = append(out, a.String())
			continue
		}
		if manet.IsIPLoopback(a) || manet.IsIP6LinkLocal(a) || manet.IsIPUnspecified(a) {
			continue
		}
		if ip, err := manet.ToIP(a); err == nil && ip.IsLinkLocalUnicast() {
			continue
		}
		out = append(out, a.String())
	}
	sort.Strings(out)
	return out
}

func (s *Service) addrsLoop(events <-chan any) {
	var timer *time.Timer
	var fire <-chan time.Time
	for {
		select {
		case <-s.ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case _, ok := <-events:
			if !ok {
				return
			}
			if timer == nil {
				timer = time.NewTimer(addrsDebounce)
			} else {
				timer.Reset(addrsDebounce)
			}
			fire = timer.C
		case <-fire:
			fire = nil
			s.requestRefresh()
			select {
			case s.probePeers <- struct{}{}:
			default:
			}
			if s.onAddrs != nil {
				s.onAddrs()
			}
		}
	}
}

func (s *Service) isRelay(p peer.ID) bool {
	s.relayMu.Lock()
	defer s.relayMu.Unlock()
	return s.relayInfo != nil && s.relayInfo.ID == p
}

// withCircuitAddrs is the host's address factory: it appends this device's
// address on the relay while a reservation is held.
func (s *Service) withCircuitAddrs(addrs []ma.Multiaddr) []ma.Multiaddr {
	s.relayMu.Lock()
	info, reserved := s.relayInfo, s.relayReserved
	s.relayMu.Unlock()
	if !reserved || info == nil {
		return addrs
	}
	circuit, err := ma.NewMultiaddr("/p2p/" + info.ID.String() + "/p2p-circuit")
	if err != nil {
		return addrs
	}
	out := append([]ma.Multiaddr(nil), addrs...)
	for _, a := range info.Addrs {
		out = append(out, a.Encapsulate(circuit))
	}
	return out
}

func (s *Service) onDisconnected(n network.Network, c network.Conn) {
	if s.ctx.Err() != nil || n.Connectedness(c.RemotePeer()) == network.Connected {
		return
	}
	if p, allowed := s.allow.get(c.RemotePeer()); allowed {
		s.requestRefresh()
		s.connect(peer.AddrInfo{ID: p.ID, Addrs: p.RegisteredAddrs})
	}
	if !s.isRelay(c.RemotePeer()) {
		return
	}
	select {
	case s.relayLost <- struct{}{}:
	default:
	}
}

// relayLoop keeps a reservation on the relay: it reserves, renews before
// expiry, and reserves again right away when the relay connection drops.
// The relay address is fetched for every reservation, so a relay that moved
// or changed identity is picked up within one reservation period.
func (s *Service) relayLoop() {
	failures := 0
	for {
		wait, err := s.reserve()
		if err != nil {
			s.setReservation(false, time.Time{})
			if s.ctx.Err() != nil {
				return
			}
			wait = relayRetrySchedule[min(failures, len(relayRetrySchedule)-1)]
			failures++
			s.logger.Warn("p2p: relay reservation failed", "error", err, "retry_in", wait)
		} else {
			failures = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-s.relayLost:
			timer.Stop()
			s.setReservation(false, time.Time{})
			s.logger.Info("p2p: relay connection lost; reserving again")
		}
	}
}

// reserve makes or renews the reservation and returns how long to wait
// before the next one.
func (s *Service) reserve() (time.Duration, error) {
	ctx, cancel := context.WithTimeout(s.ctx, connectTimeout+20*time.Second)
	s.relayMu.Lock()
	s.relayCancel = cancel
	s.relayMu.Unlock()
	defer func() { cancel(); s.relayMu.Lock(); s.relayCancel = nil; s.relayMu.Unlock() }()
	info, err := s.relayFetch(ctx)
	if err != nil {
		return 0, fmt.Errorf("fetch relay: %w", err)
	}
	if info == nil {
		s.relayMu.Lock()
		s.relayInfo, s.relayReserved = nil, false
		s.relayMu.Unlock()
		return relayRecheckInterval, nil
	}
	s.relayMu.Lock()
	if s.relayInfo != nil && s.relayInfo.ID != info.ID {
		s.relayReserved = false
	}
	s.relayInfo = info
	s.relayMu.Unlock()

	reserving := s.marks.Now()
	rsvp, err := relayclient.Reserve(ctx, s.host, *info)
	s.marks.Since(timing.Record{Stage: timing.StageRelayReserve}, reserving, err)
	if err != nil {
		s.noteReservationRefused(err)
		return 0, err
	}
	s.host.ConnManager().Protect(info.ID, "multica-relay")
	s.setReservation(true, rsvp.Expiration)
	s.logger.Info("p2p: relay reservation held", "relay", info.ID.String(), "expires", rsvp.Expiration)
	return max(time.Until(rsvp.Expiration)-relayRenewMargin, 30*time.Second), nil
}

func (s *Service) setReservation(reserved bool, expires time.Time) {
	s.relayMu.Lock()
	s.relayReserved, s.relayExpires = reserved, expires
	s.relayMu.Unlock()
}

// Connection kinds reported in PeerStatus.
const (
	ConnectionDirect  = "direct"
	ConnectionRelayed = "relayed"
)

// PeerStatus is one device of the account as seen from here.
type PeerStatus struct {
	Peer
	Connected bool `json:"connected"`
	// Connection is ConnectionDirect when any connection to the device
	// avoids the relay, ConnectionRelayed when all go through it, and empty
	// when not connected.
	Connection string   `json:"connection"`
	Addrs      []string `json:"addrs"`
}

// RelayStatus is this device's reservation on the relay.
type RelayStatus struct {
	PeerID    string     `json:"peer_id"`
	Reserved  bool       `json:"reserved"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// Status is a point-in-time snapshot for diagnostics.
type Status struct {
	PeerID      string       `json:"peer_id"`
	ListenAddrs []string     `json:"listen_addrs"`
	Relay       *RelayStatus `json:"relay,omitempty"`
	Peers       []PeerStatus `json:"peers"`
}

// Peers lists the account's other devices with their connection state.
func (s *Service) Peers() []PeerStatus {
	list := s.allow.list()
	out := make([]PeerStatus, 0, len(list))
	for _, p := range list {
		ps := PeerStatus{Peer: p, Addrs: []string{}}
		ps.Connected = s.host.Network().Connectedness(p.ID) == network.Connected
		for _, c := range s.host.Network().ConnsToPeer(p.ID) {
			if isCircuit(c.RemoteMultiaddr()) {
				if ps.Connection == "" {
					ps.Connection = ConnectionRelayed
				}
			} else {
				ps.Connection = ConnectionDirect
			}
		}
		for _, a := range s.host.Peerstore().Addrs(p.ID) {
			ps.Addrs = append(ps.Addrs, a.String())
		}
		sort.Strings(ps.Addrs)
		out = append(out, ps)
	}
	return out
}

// Status returns this device's identity, listen addresses and peers.
func (s *Service) Status() Status {
	st := Status{PeerID: s.host.ID().String(), ListenAddrs: []string{}, Peers: s.Peers()}
	s.relayMu.Lock()
	if s.relayInfo != nil {
		st.Relay = &RelayStatus{PeerID: s.relayInfo.ID.String(), Reserved: s.relayReserved}
		if s.relayReserved {
			exp := s.relayExpires
			st.Relay.ExpiresAt = &exp
		}
	}
	s.relayMu.Unlock()
	for _, a := range s.host.Addrs() {
		st.ListenAddrs = append(st.ListenAddrs, a.String())
	}
	sort.Strings(st.ListenAddrs)
	return st
}

func isCircuit(a ma.Multiaddr) bool {
	_, err := a.ValueForProtocol(ma.P_CIRCUIT)
	return err == nil
}
