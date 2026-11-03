package p2p

import (
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	relayclient "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	pbv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/pb"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

func recordsOf(r *timing.Ring) []timing.Record {
	records, _, _ := r.After(0, 0)
	return records
}

func findMark(records []timing.Record, stage string) (timing.Record, bool) {
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Stage == stage {
			return records[i], true
		}
	}
	return timing.Record{}, false
}

func TestAddrTypeNamesAKindAndNeverAnAddress(t *testing.T) {
	t.Parallel()

	for addr, want := range map[string]string{
		"/ip4/127.0.0.1/tcp/1":           timing.AddrLoopback,
		"/ip4/192.168.1.2/udp/1/quic-v1": timing.AddrLAN,
		"/ip4/8.8.8.8/tcp/1":             timing.AddrPublic,
		"/ip4/8.8.8.8/tcp/1/p2p-circuit": timing.AddrRelay,
		"/ip6/fe80::1/tcp/1":             timing.AddrLAN,
	} {
		m, err := ma.NewMultiaddr(addr)
		if err != nil {
			t.Fatal(err)
		}
		if got := addrType(m); got != want {
			t.Errorf("%s = %s, want %s", addr, got, want)
		}
	}
	relay, _ := ma.NewMultiaddr("/ip4/8.8.8.8/tcp/1/p2p-circuit")
	lan, _ := ma.NewMultiaddr("/ip4/192.168.1.2/tcp/1")
	if got := bestAddrType([]ma.Multiaddr{relay, lan}); got != timing.AddrLAN {
		t.Fatalf("best = %s", got)
	}
	if got := bestAddrType(nil); got != timing.AddrUnknown {
		t.Fatalf("none = %s", got)
	}
}

func TestRegistryFetchIsOnlyRecordedWhenItIsNotNormal(t *testing.T) {
	t.Parallel()

	ring := timing.OpenRing("", 16)
	s := &Service{marks: timing.NewMarks(ring, timing.SourceDaemon)}
	now := s.marks.Now()
	boom := errors.New("dial tcp 10.0.0.1:443: i/o timeout")

	s.noteFetch(now, nil)
	if got := recordsOf(ring); len(got) != 0 {
		t.Fatalf("a normal read was recorded: %+v", got)
	}
	s.noteFetch(now, boom)
	s.noteFetch(now, boom)
	s.noteFetch(now, boom)
	got := recordsOf(ring)
	if len(got) != 1 || got[0].Outcome != "failed" || got[0].Failure != "transport" {
		t.Fatalf("a registry that stays down must be one record: %+v", got)
	}
	s.noteFetch(now, nil)
	s.noteFetch(now, nil)
	if got := recordsOf(ring); len(got) != 2 || got[1].Outcome != "completed" {
		t.Fatalf("recovery = %+v", got)
	}
	s.noteFetch(now.Add(-slowFetch), nil)
	if got := recordsOf(ring); len(got) != 3 || got[2].Reason != timing.ReasonSlow {
		t.Fatalf("slow read = %+v", got)
	}
}

func TestRefusalsKeepAClosedReasonAndDropTheRelaysText(t *testing.T) {
	t.Parallel()

	ring := timing.OpenRing("", 16)
	s := &Service{marks: timing.NewMarks(ring, timing.SourceDaemon)}
	s.noteReservationRefused(errors.New("not a refusal"))
	s.noteReservationRefused(relayclient.ReservationError{Status: pbv2.Status_RESOURCE_LIMIT_EXCEEDED, Reason: "at 10.0.0.1"})
	s.noteCircuitRefused(errors.New("error opening relay circuit: PERMISSION_DENIED (270) via /ip4/10.0.0.1/tcp/1"))
	s.noteCircuitRefused(errors.New("no route to /ip4/10.0.0.1/tcp/1"))
	got := recordsOf(ring)
	if len(got) != 2 || got[0].Reason != timing.ReasonResourceLimit || got[1].Reason != timing.ReasonPermissionDenied {
		t.Fatalf("records = %+v", got)
	}
}

func TestHolePunchRecordsCarryTheRoleAndNoErrorText(t *testing.T) {
	t.Parallel()

	ring := timing.OpenRing("", 16)
	h := &holePunchMarks{marks: timing.NewMarks(ring, timing.SourceDaemon)}
	_, remote := newKey(t)
	direct := connAddrs{}
	h.Trace(&holepunch.Event{Remote: remote, Evt: &holepunch.StartHolePunchEvt{RemoteAddrs: []string{"/ip4/1.2.3.4/tcp/1"}}})
	h.Trace(&holepunch.Event{Remote: remote, Evt: &holepunch.EndHolePunchEvt{Success: false, EllapsedTime: 2 * time.Second, Error: "dial /ip4/1.2.3.4/tcp/1 timed out"}})
	h.HolePunchFinished("initiator", 4, nil, nil, nil)
	h.Trace(&holepunch.Event{Remote: remote, Evt: &holepunch.EndHolePunchEvt{Success: true, EllapsedTime: time.Second}})
	h.HolePunchFinished("receiver", 1, nil, nil, direct)

	got := recordsOf(ring)
	if len(got) != 3 || got[0].Stage != timing.StageHolepunchStart {
		t.Fatalf("records = %+v", got)
	}
	if f := got[1]; f.Stage != timing.StageHolepunchFailed || f.Role != timing.RoleInitiator || *f.Count != 4 || *f.DurationMs != 2000 || f.Failure != "transport" || f.Peer != remote.String() {
		t.Fatalf("failure = %+v", f)
	}
	if s := got[2]; s.Stage != timing.StageHolepunchSuccess || s.Role != timing.RoleReceiver || s.AddrType != timing.AddrPublic || s.Failure != "" {
		t.Fatalf("success = %+v", s)
	}
}

type connAddrs struct{ network.ConnMultiaddrs }

func (connAddrs) RemoteMultiaddr() ma.Multiaddr {
	m, _ := ma.NewMultiaddr("/ip4/8.8.8.8/tcp/1")
	return m
}

// The stream's own connection must win: once a direct connection exists, device
// level state reads direct for a request still on the relay.
func TestTunnelMarksReadTheStreamsOwnConnection(t *testing.T) {
	ring := timing.OpenRing("", 512)
	client, server, _ := relayedPairMarked(t, timing.NewMarks(ring, timing.SourceMobile))
	upgrading, release := make(chan struct{}, 1), make(chan struct{})
	serveTunnel(t, server, upgrading, release)
	tunnel := client.NewTunnel(server.ID(), tunnelProto, &http.Transport{})
	t.Cleanup(tunnel.Close)

	get := func(id string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, "http://device-link/stream", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(timing.HeaderRequestID, id)
		res, err := tunnel.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	connOf := func(id string) string {
		t.Helper()
		for _, r := range recordsOf(ring) {
			if r.Stage == timing.StageTunnelConn && r.RequestID == id {
				return r.Connection
			}
		}
		t.Fatalf("no tunnel.conn for %s", id)
		return ""
	}
	stream := get("on-relay")
	defer stream.Body.Close()
	if got := connOf("on-relay"); got != timing.ConnectionRelayed || stream.Header.Get("X-Relayed") != "true" {
		t.Fatalf("mark says %s, server saw relayed=%s", got, stream.Header.Get("X-Relayed"))
	}
	connectDirect(t, client, server)
	tunnel.MoveToDirect()
	if client.Connection(server.ID()) != ConnectionDirect {
		t.Fatal("device state should read direct by now")
	}
	close(release)
	_, _ = io.Copy(io.Discard, stream.Body)
	after := get("on-direct")
	defer after.Body.Close()
	if got := connOf("on-direct"); got != timing.ConnectionDirect || after.Header.Get("X-Relayed") != "false" {
		t.Fatalf("mark says %s, server saw relayed=%s", got, after.Header.Get("X-Relayed"))
	}

	records := recordsOf(ring)
	for _, stage := range []string{timing.StageLinkStart, timing.StageLinkReady, timing.StagePeerKnown, timing.StagePeerConnect, timing.StageRelayReserve, timing.StageRelayConnected, timing.StageConnectionRelay, timing.StageConnectionDirect, timing.StageTunnelStreamOpen, timing.StageProxyRequest, timing.StageProxyFirstByte, timing.StageLinkRequestSent, timing.StageLinkResponseReceived} {
		if _, ok := findMark(records, stage); !ok {
			t.Errorf("no %s among %d records", stage, len(records))
		}
	}
	for _, r := range records {
		if strings.Contains(r.Stage+r.AddrType+r.Reason+r.Connection+r.Peer, "127.0.0.1") {
			t.Fatalf("an address reached a record: %+v", r)
		}
	}
}

func TestMigrationMarksFollowTheWebSocketToDirect(t *testing.T) {
	ring := timing.OpenRing("", 512)
	client, server, _ := relayedPairMarked(t, timing.NewMarks(ring, timing.SourceMobile))
	upgrading, release := make(chan struct{}, 2), make(chan struct{})
	close(release)
	serveTunnel(t, server, upgrading, release)
	tunnel := client.NewTunnel(server.ID(), tunnelProto, &http.Transport{})
	t.Cleanup(tunnel.Close)

	res, err := tunnel.RoundTrip(upgradeRequest(t))
	if err != nil || res.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade = %+v, %v", res, err)
	}
	defer res.Body.Close()
	connectDirect(t, client, server)
	tunnel.MoveToDirect()
	again, err := tunnel.RoundTrip(upgradeRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	defer again.Body.Close()
	records := recordsOf(ring)
	stages := []string{}
	for _, r := range records {
		stages = append(stages, r.Stage)
	}
	if !slices.Contains(stages, timing.StageMigrateStart) || !slices.Contains(stages, timing.StageMigrateStreamClosed) {
		t.Fatalf("stages = %v", stages)
	}
	if r, ok := findMark(records, timing.StageMigrateReconnected); !ok || r.Connection != timing.ConnectionDirect || r.DurationMs == nil {
		t.Fatalf("reconnected = %+v in %v", r, stages)
	}
}
