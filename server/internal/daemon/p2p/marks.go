package p2p

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	relayclient "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	pbv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/pb"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"

	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

// slowFetch is when a device list read stops being normal enough to leave out.
const slowFetch = 5 * time.Second

// connectionStates remembers each device's last recorded state, because a device
// gains and loses connections without its state changing.
type connectionStates struct {
	mu     sync.Mutex
	states map[peer.ID]string
}

// addrType says what kind of address a is, never the address.
func addrType(a ma.Multiaddr) string {
	switch {
	case isCircuit(a):
		return timing.AddrRelay
	case manet.IsIPLoopback(a):
		return timing.AddrLoopback
	case manet.IsPrivateAddr(a) || manet.IsIP6LinkLocal(a):
		return timing.AddrLAN
	case manet.IsPublicAddr(a):
		return timing.AddrPublic
	}
	return timing.AddrUnknown
}

// transportOf says what carries a connection at a.
func transportOf(a ma.Multiaddr) string {
	if isCircuit(a) {
		return timing.TransportRelay
	}
	for _, c := range a {
		switch c.Code() {
		case ma.P_QUIC_V1:
			return timing.TransportQUIC
		case ma.P_TCP:
			return timing.TransportTCP
		}
	}
	return timing.TransportUnknown
}

// connectedTransport is the direct connection's transport, the one streams prefer.
func (s *Service) connectedTransport(id peer.ID) string {
	for _, c := range s.host.Network().ConnsToPeer(id) {
		if !isCircuit(c.RemoteMultiaddr()) {
			return transportOf(c.RemoteMultiaddr())
		}
	}
	return timing.TransportUnknown
}

// bestAddrType is the most direct kind among addrs. A connect that starts with
// lan and ends on relay is what a refused local network permission looks like.
func bestAddrType(addrs []ma.Multiaddr) string {
	rank := map[string]int{timing.AddrLAN: 4, timing.AddrPublic: 3, timing.AddrLoopback: 2, timing.AddrRelay: 1}
	best := timing.AddrUnknown
	for _, a := range addrs {
		if t := addrType(a); rank[t] > rank[best] {
			best = t
		}
	}
	return best
}

func (s *Service) connectedAddrType(id peer.ID) string {
	addrs := []ma.Multiaddr{}
	for _, c := range s.host.Network().ConnsToPeer(id) {
		addrs = append(addrs, c.RemoteMultiaddr())
	}
	return bestAddrType(addrs)
}

// noteConnection records a device's state when it changes.
func (s *Service) noteConnection(c network.Conn, connected bool) {
	if s.marks == nil {
		return
	}
	id := c.RemotePeer()
	if connected && s.isRelay(id) {
		s.marks.Write(timing.Record{Stage: timing.StageRelayConnected, AddrType: addrType(c.RemoteMultiaddr())})
	}
	if !s.allow.allowed(id) {
		return
	}
	state := s.Connection(id)
	s.connState.mu.Lock()
	if s.connState.states == nil {
		s.connState.states = map[peer.ID]string{}
	}
	changed := s.connState.states[id] != state
	s.connState.states[id] = state
	s.connState.mu.Unlock()
	if !changed {
		return
	}
	stage, kind, transport := timing.StageConnectionLost, "", ""
	switch state {
	case ConnectionDirect:
		stage, kind, transport = timing.StageConnectionDirect, s.connectedAddrType(id), s.connectedTransport(id)
	case ConnectionRelayed:
		stage, kind, transport = timing.StageConnectionRelay, timing.AddrRelay, timing.TransportRelay
	}
	s.marks.Write(timing.Record{Stage: stage, Peer: id.String(), AddrType: kind, Transport: transport})
}

// fetchState keeps a registry that stays down to one record, not one per refresh.
type fetchState struct{ failing bool }

// noteFetch records a device list read only when it fails, recovers or is slow.
// Refresh holds refreshMu through this, which guards the state.
func (s *Service) noteFetch(began time.Time, err error) {
	took := s.marks.Now().Sub(began)
	failed := err != nil
	switch {
	case failed && !s.fetchState.failing:
		s.marks.Since(timing.Record{Stage: timing.StageRegistryFetch}, began, err)
	case !failed && s.fetchState.failing:
		s.marks.Since(timing.Record{Stage: timing.StageRegistryFetch}, began, nil)
	case !failed && took >= slowFetch:
		s.marks.Since(timing.Record{Stage: timing.StageRegistryFetch, Reason: timing.ReasonSlow}, began, nil)
	}
	s.fetchState.failing = failed
}

// noteReservationRefused records why the relay said no in closed terms; its own
// text can carry addresses.
func (s *Service) noteReservationRefused(err error) {
	var refused relayclient.ReservationError
	if !errors.As(err, &refused) {
		return
	}
	reason := timing.ReasonReservationRefused
	switch refused.Status {
	case pbv2.Status_RESOURCE_LIMIT_EXCEEDED, pbv2.Status_RESERVATION_REFUSED:
		reason = timing.ReasonResourceLimit
	case pbv2.Status_PERMISSION_DENIED:
		reason = timing.ReasonPermissionDenied
	case pbv2.Status_CONNECTION_FAILED:
		reason = timing.ReasonConnectionFailed
	}
	s.marks.Write(timing.Record{Stage: timing.StageRelayRefused, Reason: reason, Outcome: "failed"})
}

// noteCircuitRefused records a relay refusing a circuit. libp2p exposes that only
// as text, so the status name is matched and the text is not kept. Each account
// has a limited number of circuits, which is why this is worth seeing.
func (s *Service) noteCircuitRefused(err error) {
	text := err.Error()
	for status, reason := range map[string]string{
		pbv2.Status_RESOURCE_LIMIT_EXCEEDED.String(): timing.ReasonResourceLimit,
		pbv2.Status_PERMISSION_DENIED.String():       timing.ReasonPermissionDenied,
	} {
		if strings.Contains(text, status) {
			s.marks.Write(timing.Record{Stage: timing.StageRelayRefused, Reason: reason, Outcome: "failed", Role: timing.RoleInitiator})
			return
		}
	}
}

// holePunchMarks records hole punching. libp2p reports the attempt to its event
// tracer and the side only to its metrics tracer at the end, so a start has no
// role and success or failure do.
type holePunchMarks struct {
	marks *timing.Marks

	mu   sync.Mutex
	peer peer.ID
	took time.Duration
	err  string
}

// Trace implements holepunch.EventTracer. An event's error text stays here.
func (h *holePunchMarks) Trace(evt *holepunch.Event) {
	switch e := evt.Evt.(type) {
	case *holepunch.StartHolePunchEvt:
		h.marks.Write(timing.Record{Stage: timing.StageHolepunchStart, Event: timing.EventStart, Peer: evt.Remote.String()})
	case *holepunch.EndHolePunchEvt:
		h.mu.Lock()
		h.peer, h.took, h.err = evt.Remote, e.EllapsedTime, e.Error
		h.mu.Unlock()
	}
}

// HolePunchFinished implements holepunch.MetricsTracer.
func (h *holePunchMarks) HolePunchFinished(side string, attempts int, _ []ma.Multiaddr, _ []ma.Multiaddr, direct network.ConnMultiaddrs) {
	h.mu.Lock()
	remote, took, text := h.peer, h.took, h.err
	h.mu.Unlock()
	record := timing.Record{Stage: timing.StageHolepunchFailed, Event: timing.EventEnd, Peer: remote.String(), Role: side, Count: timing.N(int64(attempts)), DurationMs: timing.Ms(took), Outcome: "failed", Failure: timing.Classify(text)}
	if direct != nil {
		record.Stage, record.Outcome, record.Failure, record.AddrType, record.Transport = timing.StageHolepunchSuccess, "completed", "", addrType(direct.RemoteMultiaddr()), transportOf(direct.RemoteMultiaddr())
	}
	h.marks.Write(record)
}

// DirectDialFinished implements holepunch.MetricsTracer.
func (h *holePunchMarks) DirectDialFinished(bool) {}
