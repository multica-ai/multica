package daemon

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/daemon/localenv"
	"github.com/multica-ai/multica/server/internal/envtunnel"
)

// allowsTunnelPort limits relay connections to ports reported for this environment.
func (s *environmentService) allowsTunnelPort(key string, port int) bool {
	if s == nil || port < 1 || port > 65535 {
		return false
	}
	if s.manager != nil {
		for _, env := range s.manager.Snapshot() {
			if env.Key == key && env.Port == port && (env.State == localenv.StateStarting || env.State == localenv.StateRunning) {
				return true
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, env := range s.detected {
		if env.Key == key && env.Kind != localenv.KindApp && env.Port == port {
			return true
		}
	}
	return false
}

// dialLocalEnvironment reaches a declared or detected server on this machine.
// Node 17+ may bind localhost to the IPv6 loopback only, so a refused IPv4
// connect falls back to the literal [::1]; neither attempt resolves DNS.
func dialLocalEnvironment(ctx context.Context, port int) (net.Conn, error) {
	portText := strconv.Itoa(port)
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", portText))
	if err == nil || ctx.Err() != nil {
		return conn, err
	}
	return dialer.DialContext(ctx, "tcp", net.JoinHostPort("::1", portText))
}

func (d *Daemon) openEnvironmentTunnel(ctx context.Context, runtimeID, streamID string) {
	if d.environments == nil || ctx.Err() != nil {
		return
	}
	id, secret, ok := strings.Cut(streamID, ".")
	if !ok {
		return
	}
	if _, err := uuid.Parse(id); err != nil {
		return
	}
	if _, err := uuid.Parse(secret); err != nil {
		return
	}
	endpoint := strings.TrimRight(d.client.baseURL, "/") + "/api/daemon/runtimes/" + runtimeID + "/environment-tunnel/" + id
	endpoint = strings.Replace(strings.Replace(endpoint, "https://", "wss://", 1), "http://", "ws://", 1)
	dialer := websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: 10 * time.Second}
	ws, resp, err := dialer.DialContext(ctx, endpoint, http.Header{"Authorization": {"Bearer " + secret}})
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return
	}
	defer ws.Close()
	port, err := strconv.Atoi(resp.Header.Get("X-Env-Tunnel-Port"))
	if err != nil || !d.environments.allowsTunnelPort(resp.Header.Get("X-Env-Tunnel-Key"), port) {
		return
	}
	local, err := dialLocalEnvironment(ctx, port)
	if err != nil {
		return
	}
	defer local.Close()
	tunnel := envtunnel.NewConn(ws)
	stop := context.AfterFunc(ctx, func() {
		tunnel.Close()
		local.Close()
	})
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(local, tunnel)
		local.Close()
	}()
	_, _ = io.Copy(tunnel, local)
	tunnel.Close()
	<-done
}
