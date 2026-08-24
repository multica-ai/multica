package daemonws

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// testWSConn returns a live *websocket.Conn for tests that need a real conn
// object (the eviction path closes it) without going through HandleWebSocket.
func testWSConn(t *testing.T) *websocket.Conn {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		time.Sleep(30 * time.Second) // hold until test cleanup closes us
		_ = conn
	}))
	t.Cleanup(ts.Close)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func registerTestClient(t *testing.T, h *Hub, conn *websocket.Conn, runtimeID string) *client {
	t.Helper()
	c := &client{
		hub:      h,
		conn:     conn,
		send:     make(chan []byte, 1),
		identity: ClientIdentity{DaemonID: "d-" + runtimeID},
		runtimes: map[string]struct{}{runtimeID: {}},
	}
	h.mu.Lock()
	h.clients[c] = true
	h.byRuntime[runtimeID] = map[*client]bool{c: true}
	h.mu.Unlock()
	M.ConnectsTotal.Add(1)
	M.ActiveConnections.Add(1)
	return c
}

func TestSoftDropBeforeEviction(t *testing.T) {
	defer M.Reset()
	h := NewHub()
	c := registerTestClient(t, h, testWSConn(t), "runtime-1")

	// Fill the send buffer so every notify takes the default branch.
	c.send <- []byte("backlog")

	for i := 0; i < softDropLimit-1; i++ {
		delivered, _ := h.notifyFrame("runtime-1", []byte("x"), fmt.Sprintf("evt-%d", i))
		if delivered {
			t.Fatalf("notify %d delivered despite full buffer", i)
		}
	}
	if got := M.SoftDropsTotal.Load(); got != int64(softDropLimit-1) {
		t.Fatalf("soft_drops_total = %d, want %d", got, softDropLimit-1)
	}
	if got := M.SlowEvictionsTotal.Load(); got != 0 {
		t.Fatalf("slow_evictions_total = %d, want 0 before limit", got)
	}
	if h.RuntimeConnectionCount("runtime-1") != 1 {
		t.Fatal("client evicted before soft-drop limit")
	}

	// Nth consecutive drop evicts.
	if delivered, _ := h.notifyFrame("runtime-1", []byte("x"), "evt-evict"); delivered {
		t.Fatal("evicting notify reported delivery")
	}
	if got := M.SlowEvictionsTotal.Load(); got != 1 {
		t.Fatalf("slow_evictions_total = %d, want 1 at limit", got)
	}
	if h.RuntimeConnectionCount("runtime-1") != 0 {
		t.Fatal("client not evicted after consecutive-drop limit")
	}
}

func TestSuccessfulSendResetsDropCounter(t *testing.T) {
	defer M.Reset()
	h := NewHub()
	c := registerTestClient(t, h, testWSConn(t), "runtime-r")

	c.send <- []byte("backlog") // buffer full
	if delivered, _ := h.notifyFrame("runtime-r", []byte("x"), "drop-1"); delivered {
		t.Fatal("expected soft drop")
	}
	if c.drops.Load() != 1 {
		t.Fatalf("drops = %d, want 1", c.drops.Load())
	}

	<-c.send // drain backlog; next notify delivers and must reset the counter
	if delivered, _ := h.notifyFrame("runtime-r", []byte("y"), "deliver"); !delivered {
		t.Fatal("expected delivery after drain")
	}
	if c.drops.Load() != 0 {
		t.Fatalf("drops after delivery = %d, want 0", c.drops.Load())
	}
}
