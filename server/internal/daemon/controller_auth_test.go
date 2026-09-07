package daemon

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestControllerDaemonTokenIsLimitedToNativeLifecycle(t *testing.T) {
	c := &Client{token: "mul_bootstrap"}
	if c.SetControllerDaemonToken("mct_authority") == nil {
		t.Fatal("controller authority accepted as daemon credential")
	}
	if err := c.SetControllerDaemonToken("mdt_bound"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/daemon/claim", "/api/daemon/ws", "/api/daemon/tasks/run/start", "/api/daemon/runtimes/runtime/recover-orphans"} {
		if c.requestToken(path) != "mdt_bound" {
			t.Fatalf("native path uses wrong token: %s", path)
		}
	}
	for _, path := range []string{"/api/issues/run", "/api/controller/issues/run", "/api/daemon/workspaces", "/api/daemon/claim-other"} {
		if c.requestToken(path) != "mul_bootstrap" {
			t.Fatalf("bound credential escaped lifecycle scope: %s", path)
		}
	}
}

func TestControllerWakeupWebSocketUsesBoundCredential(t *testing.T) {
	captured := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured <- r.Header.Get("Authorization")
		http.Error(w, "test ends at handshake", http.StatusForbidden)
	}))
	defer srv.Close()
	d := New(Config{ServerBaseURL: srv.URL}, slog.Default())
	d.client.SetToken("mul_bootstrap")
	if err := d.client.SetControllerDaemonToken("mdt_bound"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = d.runTaskWakeupConnection(ctx, []string{"native-runtime"}, make(chan taskWakeup, 1), make(chan struct{}))
	select {
	case token := <-captured:
		if token != "Bearer mdt_bound" {
			t.Fatal("WebSocket claim transport sent bootstrap authority")
		}
	default:
		t.Fatal("no native WebSocket handshake observed")
	}
}
