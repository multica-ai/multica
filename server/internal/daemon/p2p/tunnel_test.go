package p2p

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

const tunnelProto = "/multica/test-tunnel/1"

// relayedPair returns two devices connected only through a real relay. The
// account serves circuit addresses until only is changed.
func relayedPair(t *testing.T) (client, server *Service, acct *relayAccount) {
	t.Helper()
	return relayedPairMarked(t, nil)
}

// relayedPairMarked is relayedPair with the client's connection marks recorded.
func relayedPairMarked(t *testing.T, clientMarks *timing.Marks) (client, server *Service, acct *relayAccount) {
	t.Helper()
	keyA, idA := newKey(t)
	keyB, idB := newKey(t)
	relay, _ := startRelay(t, idA, idB)
	acct = &relayAccount{only: func(a string) bool { return strings.Contains(a, "/p2p-circuit") }}
	start := func(key crypto.PrivKey, marks *timing.Marks) *Service {
		svc, err := Start(context.Background(), Config{Key: key, Fetch: acct.fetch, ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"},
			RefreshInterval: time.Hour, Relay: relayFunc(relay), Logger: testLogger, Marks: marks})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { svc.Close() })
		return svc
	}
	server, client = start(keyA, nil), start(keyB, clientMarks)
	acct.mu.Lock()
	acct.svcs = []*Service{server, client}
	acct.mu.Unlock()
	hasCircuit := func(s *Service) bool {
		for _, a := range s.AdvertisedAddrs() {
			if strings.Contains(a, "/p2p-circuit") {
				return true
			}
		}
		return false
	}
	if !waitFor(t, 15*time.Second, func() bool { return hasCircuit(client) && hasCircuit(server) }) {
		t.Fatal("no circuit addresses")
	}
	for _, svc := range []*Service{server, client} {
		if err := svc.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if !waitFor(t, 15*time.Second, func() bool { return client.Connection(server.ID()) == ConnectionRelayed }) {
		t.Fatalf("not relayed: %+v", client.Status())
	}
	return client, server, acct
}

func connectDirect(t *testing.T, from, to *Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(network.WithForceDirectDial(context.Background(), "test"), 10*time.Second)
	defer cancel()
	if err := from.host.Connect(ctx, peer.AddrInfo{ID: to.ID(), Addrs: to.host.Addrs()}); err != nil {
		t.Fatal(err)
	}
	if from.Connection(to.ID()) != ConnectionDirect {
		t.Fatalf("connection = %q, want direct", from.Connection(to.ID()))
	}
}

func upgradeRequest(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://device-link/upgrade", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "test")
	return req
}

// serveTunnel answers /upgrade with 101 after release and /stream with a partial
// body that finishes after release. X-Relayed reports the serving stream.
func serveTunnel(t *testing.T, svc *Service, upgrading chan<- struct{}, release <-chan struct{}) {
	t.Helper()
	listener, stop := svc.Listen(tunnelProto)
	t.Cleanup(stop)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				relayed := strconv.FormatBool(Relayed(conn))
				server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-Relayed", relayed)
					switch r.URL.Path {
					case "/upgrade":
						upgrading <- struct{}{}
						<-release
						hijacked, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							return
						}
						defer hijacked.Close()
						_, _ = io.WriteString(hijacked, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: test\r\nX-Relayed: "+relayed+"\r\n\r\n")
						_, _ = io.Copy(io.Discard, hijacked)
					case "/stream":
						_, _ = io.WriteString(w, "first")
						w.(http.Flusher).Flush()
						<-release
						_, _ = io.WriteString(w, "-last")
					}
				})}
				_ = server.Serve(&oneConnListener{conn: conn})
			}()
		}
	}()
}

func TestTunnelMoveToDirectClosesWebSocketsAndKeepsRequests(t *testing.T) {
	client, server, _ := relayedPair(t)
	upgrading, release := make(chan struct{}, 1), make(chan struct{})
	serveTunnel(t, server, upgrading, release)
	tunnel := client.NewTunnel(server.ID(), tunnelProto, &http.Transport{})
	t.Cleanup(tunnel.Close)

	req, err := http.NewRequest(http.MethodGet, "http://device-link/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := tunnel.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	first := make([]byte, len("first"))
	if _, err := io.ReadFull(stream.Body, first); err != nil {
		t.Fatal(err)
	}
	upgraded := make(chan *http.Response, 1)
	upgrade := upgradeRequest(t)
	go func() {
		res, err := tunnel.RoundTrip(upgrade)
		if err != nil {
			t.Error(err)
			close(upgraded)
			return
		}
		upgraded <- res
	}()
	// The upgrade is still waiting for 101 when the direct connection appears
	// and migration runs, so only its own completion can move it.
	<-upgrading
	connectDirect(t, client, server)
	tunnel.MoveToDirect()
	close(release)

	rest, err := io.ReadAll(stream.Body)
	if err != nil || string(rest) != "-last" {
		t.Fatalf("in-flight request: %q %v", rest, err)
	}
	res, ok := <-upgraded
	if !ok {
		t.FailNow()
	}
	defer res.Body.Close()
	if res.Header.Get("X-Relayed") != "true" {
		t.Fatalf("upgrade relayed = %q, want true", res.Header.Get("X-Relayed"))
	}
	closed := make(chan error, 1)
	go func() {
		_, err := res.Body.Read(make([]byte, 1))
		closed <- err
	}()
	select {
	case err := <-closed:
		if err == nil {
			t.Fatal("expected the relayed WebSocket to close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WebSocket upgraded during migration stayed on the relay")
	}

	// The in-flight stream went idle after migration; new traffic must not reuse it.
	next, err := http.NewRequest(http.MethodGet, "http://device-link/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := tunnel.RoundTrip(next)
	if err != nil {
		t.Fatal(err)
	}
	defer after.Body.Close()
	if after.Header.Get("X-Relayed") != "false" {
		t.Fatalf("request after migration relayed = %q, want false", after.Header.Get("X-Relayed"))
	}
}

func TestRefreshDialsDirectAddressRegisteredWhileRelayed(t *testing.T) {
	client, server, acct := relayedPair(t)
	// AdvertisedAddrs omits loopback, the only direct address in this test.
	acct.mu.Lock()
	acct.extra = map[peer.ID][]ma.Multiaddr{server.ID(): server.host.Network().ListenAddresses()}
	acct.mu.Unlock()
	if err := client.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 15*time.Second, func() bool { return client.Connection(server.ID()) == ConnectionDirect }) {
		t.Fatalf("connection = %q, want direct", client.Connection(server.ID()))
	}
}

type oneConnListener struct {
	conn net.Conn
	done chan struct{}
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	if l.conn != nil {
		conn := l.conn
		l.conn = nil
		l.done = make(chan struct{})
		return conn, nil
	}
	<-l.done
	return nil, net.ErrClosed
}

func (l *oneConnListener) Close() error   { return nil }
func (l *oneConnListener) Addr() net.Addr { return Addr("") }
