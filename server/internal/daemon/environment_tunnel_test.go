package daemon

import (
	"context"
	"errors"
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

// dialLocalEnvironment prefers the IPv4 loopback and falls back to the literal
// [::1] when a server bound localhost to the IPv6 loopback only.
func TestEnvironmentTunnelLoopbackFallback(t *testing.T) {
	serve := func(network, address, reply string) int {
		listener, err := net.Listen(network, address)
		if err != nil {
			t.Skipf("loopback unavailable: %v", err)
		}
		t.Cleanup(func() { listener.Close() })
		go func() {
			for conn, err := listener.Accept(); err == nil; conn, err = listener.Accept() {
				io.WriteString(conn, reply)
				conn.Close()
			}
		}()
		return listener.Addr().(*net.TCPAddr).Port
	}
	reaches := func(port int, want string) {
		conn, err := dialLocalEnvironment(context.Background(), port)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(conn)
		if err != nil || string(body) != want {
			t.Fatalf("dialLocalEnvironment reached %q, %v; want %q", body, err, want)
		}
	}
	// Nothing listens on 127.0.0.1 for this port, so the refused IPv4 dial
	// falls back to [::1]; once both answer on it, IPv4 wins.
	port := serve("tcp6", "[::1]:0", "ipv6")
	reaches(port, "ipv6")
	serve("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), "ipv4")
	reaches(port, "ipv4")
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
