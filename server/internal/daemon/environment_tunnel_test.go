package daemon

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/daemon/localenv"
	"github.com/multica-ai/multica/server/internal/envtunnel"
)

func TestEnvironmentTunnelWhitelist(t *testing.T) {
	s := &environmentService{detected: []localenv.Environment{
		{Key: "detected:server", Port: 4321}, {Key: "detected:app", Port: 9876, Kind: localenv.KindApp},
	}}
	for _, tc := range []struct {
		key  string
		port int
		want bool
	}{
		{"detected:server", 4321, true}, {"detected:server", 4322, false}, {"unknown", 4321, false}, {"detected:app", 9876, false}, {"detected:server", 0, false},
	} {
		if got := s.allowsTunnelPort(tc.key, tc.port); got != tc.want {
			t.Errorf("%+v got %v", tc, got)
		}
	}
	var empty *environmentService
	if empty.allowsTunnelPort("x", 80) {
		t.Fatal("nil service allowed port")
	}
}

func TestEnvironmentTunnelForwardAndReject(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(strconv.FormatBool(allowed), func(t *testing.T) {
			local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("local response")) }))
			defer local.Close()
			_, portText, _ := net.SplitHostPort(strings.TrimPrefix(local.URL, "http://"))
			port, _ := strconv.Atoi(portText)
			result := make(chan error, 1)
			streamID, token := uuid.NewString(), uuid.NewString()
			pending := streamID + "." + token
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+token || !strings.HasSuffix(r.URL.Path, "/"+streamID) {
					result <- errors.New("invalid environment callback authentication")
					http.Error(w, "auth", 401)
					return
				}
				ws, err := (&websocket.Upgrader{}).Upgrade(w, r, http.Header{"X-Env-Tunnel-Key": {"detected:test"}, "X-Env-Tunnel-Port": {portText}})
				if err != nil {
					result <- err
					return
				}
				conn := envtunnel.NewConn(ws)
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(2 * time.Second))
				_, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
				if err == nil {
					var b []byte
					b, err = io.ReadAll(conn)
					if strings.Contains(string(b), "local response") {
						err = nil
					}
				}
				result <- err
			}))
			defer server.Close()
			s := &environmentService{}
			if allowed {
				s.detected = []localenv.Environment{{Key: "detected:test", Port: port}}
			}
			client := NewClient(server.URL)
			client.SetToken("test-token")
			d := &Daemon{client: client, environments: s}
			d.openEnvironmentTunnel(context.Background(), uuid.NewString(), pending)
			err := <-result
			if allowed && err != nil {
				t.Fatal(err)
			}
			if !allowed && err == nil {
				t.Fatal("unregistered port was forwarded")
			}
		})
	}
}

func TestEnvironmentTunnelIPv6LoopbackFallback(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	local := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "ipv6 loopback response")
	})}
	go func() { _ = local.Serve(listener) }()
	t.Cleanup(func() {
		local.Close()
		listener.Close()
	})
	port := listener.Addr().(*net.TCPAddr).Port

	streamID, token := uuid.NewString(), uuid.NewString()
	result := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || !strings.HasSuffix(r.URL.Path, "/"+streamID) {
			result <- errors.New("invalid environment callback authentication")
			http.Error(w, "auth", 401)
			return
		}
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, http.Header{
			"X-Env-Tunnel-Key":  {"detected:test"},
			"X-Env-Tunnel-Port": {strconv.Itoa(port)},
		})
		if err != nil {
			result <- err
			return
		}
		conn := envtunnel.NewConn(ws)
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"); err != nil {
			result <- err
			return
		}
		req, err := http.NewRequest(http.MethodGet, "http://localhost/", nil)
		if err != nil {
			result <- err
			return
		}
		resp, err := http.ReadResponse(bufio.NewReader(conn), req)
		if err != nil {
			result <- err
			return
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			result <- err
			return
		}
		if resp.StatusCode != http.StatusOK || string(body) != "ipv6 loopback response" {
			result <- fmt.Errorf("unexpected tunneled response: %d %q", resp.StatusCode, body)
			return
		}
		result <- nil
	}))
	defer server.Close()

	d := &Daemon{
		client:       NewClient(server.URL),
		environments: &environmentService{detected: []localenv.Environment{{Key: "detected:test", Port: port}}},
	}
	d.openEnvironmentTunnel(context.Background(), uuid.NewString(), streamID+"."+token)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("IPv6 loopback tunnel failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("tunnel callback never reached the local IPv6 server")
	}
}

func TestEnvironmentTunnelPrefersIPv4Loopback(t *testing.T) {
	v4, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer v4.Close()
	port := v4.Addr().(*net.TCPAddr).Port
	v6, err := net.Listen("tcp6", net.JoinHostPort("::1", strconv.Itoa(port)))
	if err != nil {
		t.Skipf("dual-stack loopback unavailable: %v", err)
	}
	defer v6.Close()

	serve := func(listener net.Listener, reply string) {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			io.WriteString(conn, reply)
			conn.Close()
		}
	}
	go serve(v4, "ipv4")
	go serve(v6, "ipv6")

	conn, err := dialLocalEnvironment(context.Background(), port)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ipv4" {
		t.Fatalf("dialLocalEnvironment reached %q, want the IPv4 listener", body)
	}
}

func TestEnvironmentTunnelRejectsIncompleteStreamID(t *testing.T) {
	var dials atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		dials.Add(1)
		http.Error(w, "unexpected dial", http.StatusTeapot)
	}))
	defer server.Close()
	for _, tc := range []struct {
		name     string
		streamID string
	}{
		{"missing separator", uuid.NewString()},
		{"empty secret", uuid.NewString() + "."},
		{"invalid id", "not-a-uuid." + uuid.NewString()},
		{"invalid secret", uuid.NewString() + ".not-a-uuid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dials.Store(0)
			d := &Daemon{client: NewClient(server.URL), environments: &environmentService{}}
			d.openEnvironmentTunnel(context.Background(), uuid.NewString(), tc.streamID)
			if got := dials.Load(); got != 0 {
				t.Fatalf("incomplete stream id dialed %d times", got)
			}
		})
	}
}

func TestEnvironmentTunnelDeclaredWhitelist(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local environments require Unix")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".multica"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".multica/environments.toml"), []byte("[environments.web]\nstart='exec sleep 30'\nurl=''\n"), 0600); err != nil {
		t.Fatal(err)
	}
	manager := localenv.NewManager(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	task := localenv.Task{TaskID: "task", IssueID: "issue", WorkspaceID: "workspace", RootDir: root}
	env, err := manager.Start(task, root, "web", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Stop(task, "web") })
	s := &environmentService{manager: manager}
	if !s.allowsTunnelPort(env.Key, env.Port) || s.allowsTunnelPort(env.Key, env.Port+1) {
		t.Fatal("declared environment not bound to exact port")
	}
	if _, err := manager.Stop(task, "web"); err != nil {
		t.Fatal(err)
	}
	if s.allowsTunnelPort(env.Key, env.Port) {
		t.Fatal("stopped environment allowed")
	}
}
