package servicepreview

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Manager, Create) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, "REMOTE:"+r.Host+r.URL.Path) }))
	t.Cleanup(server.Close)
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	m := New(filepath.Join(t.TempDir(), "policy.json"), func() bool { return true }, func(_ context.Context, scope int64, id string) error {
		if scope != 1 || id != "conversation" {
			return fmt.Errorf("scope mismatch")
		}
		return nil
	}, nil)
	t.Cleanup(m.Revoke)
	if err := m.SetPolicy(Policy{Enabled: true, Ports: []int{p}}); err != nil {
		t.Fatal(err)
	}
	return m, Create{Version: 1, RequestID: "create", ConversationID: "conversation", Target: Target{Host: "localhost", Port: p}}
}
func connect(t *testing.T, m *Manager, peer string, result Result) net.Conn {
	t.Helper()
	conn, err := Dial(context.Background(), func(context.Context) (net.Conn, error) { a, b := net.Pipe(); go m.ServeStream(peer, b); return a, nil }, Hello{Version: 1, ServiceID: result.ID, ConnectionID: "connection", Capability: result.Capability})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}
func TestPreviewScopePolicyAndReceipts(t *testing.T) {
	m, cmd := fixture(t)
	for _, change := range []func(*Create){func(c *Create) { c.Target.Host = "example.com" }, func(c *Create) { c.Target.Port = 0 }, func(c *Create) { c.Target.Port++ }, func(c *Create) { c.ConversationID = "foreign" }} {
		other := cmd
		change(&other)
		if _, err := m.Create(context.Background(), "peer", 1, other); err == nil {
			t.Fatalf("accepted invalid target: %+v", other)
		}
	}
	result, err := m.Create(context.Background(), "peer", 1, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if repeated, err := m.Create(context.Background(), "peer", 1, cmd); err != nil || repeated.ID != result.ID || repeated.Capability != result.Capability {
		t.Fatalf("receipt replay: %+v %v", repeated, err)
	}
	other := cmd
	other.Target.Host = "127.0.0.1"
	if _, err := m.Create(context.Background(), "peer", 1, other); err == nil {
		t.Fatal("reused request with different target")
	}
	if _, err := m.change("foreign", 1, result.ID, true); err == nil {
		t.Fatal("foreign peer renewed a service")
	}
	if _, err := m.change("peer", 2, result.ID, false); err == nil {
		t.Fatal("foreign workspace revoked a service")
	}
}
func TestPreviewTransfersHTTPAndRevokesLiveStreams(t *testing.T) {
	m, cmd := fixture(t)
	result, err := m.Create(context.Background(), "peer", 1, cmd)
	if err != nil {
		t.Fatal(err)
	}
	conn := connect(t, m, "peer", result)
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = fmt.Fprint(conn, "GET /login HTTP/1.1\r\nHost: localhost:3000\r\n\r\n")
	reply, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(reply.Body)
	reply.Body.Close()
	if string(body) != "REMOTE:localhost:3000/login" {
		t.Fatalf("wrong target/Host: %s", body)
	}
	if err := m.SetPolicy(Policy{Enabled: false, Ports: []int{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("revoked stream stayed readable")
	}
	if _, err := m.Create(context.Background(), "peer", 1, cmd); err == nil {
		t.Fatal("disabled sharing accepted a new grant")
	}
}
func TestPreviewRejectsWrongPeerAndExpiredCapabilities(t *testing.T) {
	m, cmd := fixture(t)
	result, err := m.Create(context.Background(), "peer", 1, cmd)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ peer, capability string }{{"foreign", result.Capability}, {"peer", "wrong"}} {
		_, err := Dial(context.Background(), func(context.Context) (net.Conn, error) {
			a, b := net.Pipe()
			go m.ServeStream(tc.peer, b)
			return a, nil
		}, Hello{Version: 1, ServiceID: result.ID, ConnectionID: "x", Capability: tc.capability})
		if err == nil {
			t.Fatal("invalid credential admitted")
		}
	}
	m.mu.Lock()
	m.grants[result.ID].result.ExpiresAt = time.Now().Add(-time.Second)
	m.mu.Unlock()
	if _, err := m.change("peer", 1, result.ID, true); err == nil {
		t.Fatal("expired service renewed")
	}
	if _, err := m.Create(context.Background(), "peer", 1, cmd); err == nil {
		t.Fatal("expired receipt silently created a new capability")
	}
}
func TestPreviewLoopbackWithoutDNS(t *testing.T) {
	for _, host := range []string{"localhost.", "APP.localhost", "127.0.0.2", "::1", "::ffff:127.0.0.1", "0.0.0.0"} {
		if _, ok := Loopback(host); !ok {
			t.Errorf("loopback rejected: %s", host)
		}
	}
	for _, host := range []string{"localhost.example.com", "192.168.0.1", "169.254.169.254", "example.com", "::", "fe80::1"} {
		if _, ok := Loopback(host); ok {
			t.Errorf("non-loopback admitted: %s", host)
		}
	}
}

func TestPreviewRenewsAndLimitsStreamsPerPeerScope(t *testing.T) {
	m, cmd := fixture(t)
	result, err := m.Create(context.Background(), "peer", 1, cmd)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := m.change("peer", 1, result.ID, true)
	if err != nil || !renewed.ExpiresAt.After(result.ExpiresAt) || renewed.Capability != result.Capability {
		t.Fatalf("renewal changed ownership or failed: %v", err)
	}
	for range 32 {
		connect(t, m, "peer", result)
	}
	_, err = Dial(context.Background(), func(context.Context) (net.Conn, error) {
		a, b := net.Pipe()
		go m.ServeStream("peer", b)
		return a, nil
	}, Hello{Version: 1, ServiceID: result.ID, ConnectionID: "over-limit", Capability: result.Capability})
	if failure, ok := err.(*Failure); !ok || failure.Code != "preview_connection_limited" {
		t.Fatalf("limit did not reject another stream: %v", err)
	}
	other, err := m.Create(context.Background(), "other-peer", 1, cmd)
	if err != nil {
		t.Fatal(err)
	}
	connect(t, m, "other-peer", other)
	if err := m.SetPolicy(Policy{Enabled: true, Ports: []int{cmd.Target.Port%65535 + 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.change("peer", 1, result.ID, true); err == nil {
		t.Fatal("removed port retained its grant")
	}
}

func TestPreviewIPTargetsRetainAddressAndRequireSharedPort(t *testing.T) {
	m, cmd := fixture(t)
	for i, host := range []string{"192.168.1.10", "8.8.8.8", "169.254.169.254", "0.0.0.0", "::", "2001:db8::10", "::ffff:192.168.1.10"} {
		cmd.RequestID = fmt.Sprintf("ip-%d", i)
		cmd.Target.Host = host
		result, err := m.Create(context.Background(), "peer", 1, cmd)
		if err != nil {
			t.Fatalf("IP target rejected: %s: %v", host, err)
		}
		if got := m.grants[result.ID].target.Host; got != host {
			t.Fatalf("address rewritten: %s -> %s", host, got)
		}
	}
	if err := m.SetPolicy(Policy{Enabled: true, Ports: []int{}}); err != nil {
		t.Fatal(err)
	}
	cmd.RequestID = "unshared-ip"
	if _, err := m.Create(context.Background(), "peer", 1, cmd); err == nil {
		t.Fatal("unshared IP port accepted")
	}
}

func TestPreviewDialsOriginalNonLoopbackIP(t *testing.T) {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	var host string
	for _, address := range addresses {
		network, ok := address.(*net.IPNet)
		if ok && network.IP.To4() != nil && !network.IP.IsLoopback() {
			host = network.IP.String()
			break
		}
	}
	if host == "" {
		t.Skip("no non-loopback IPv4 interface available")
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, r.Host+r.URL.Path) }))
	server.Listener.Close()
	server.Listener = listener
	server.Start()
	defer server.Close()
	m, cmd := fixture(t)
	cmd.Target.Host = host
	cmd.Target.Port = listener.Addr().(*net.TCPAddr).Port
	if err := m.SetPolicy(Policy{Enabled: true, Ports: []int{cmd.Target.Port}}); err != nil {
		t.Fatal(err)
	}
	result, err := m.Create(context.Background(), "peer", 1, cmd)
	if err != nil {
		t.Fatal(err)
	}
	conn := connect(t, m, "peer", result)
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	authority := net.JoinHostPort(host, strconv.Itoa(cmd.Target.Port))
	_, _ = fmt.Fprintf(conn, "GET /original HTTP/1.1\r\nHost: %s\r\n\r\n", authority)
	reply, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reply.Body.Close()
	body, err := io.ReadAll(reply.Body)
	if err != nil || string(body) != authority+"/original" {
		t.Fatalf("wrong original address: %q: %v", body, err)
	}
}
