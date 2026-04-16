package realtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/auth"
)

const testWorkspaceID = "test-workspace"
const testUserID = "test-user"

// mockMembershipChecker always returns true.
type mockMembershipChecker struct{}

func (m *mockMembershipChecker) IsMember(_ context.Context, _, _ string) bool {
	return true
}

func makeTestToken(t *testing.T) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": testUserID,
	})
	signed, err := token.SignedString(auth.JWTSecret())
	if err != nil {
		t.Fatalf("failed to sign test JWT: %v", err)
	}
	return signed
}

func newTestHub(t *testing.T) (*Hub, *httptest.Server) {
	t.Helper()
	hub := NewHub()
	go hub.Run()

	mc := &mockMembershipChecker{}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		HandleWebSocket(hub, mc, nil, nil, w, r)
	})
	server := httptest.NewServer(mux)
	return hub, server
}

func connectWS(t *testing.T, server *httptest.Server) *websocket.Conn {
	t.Helper()
	token := makeTestToken(t)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?workspace_id=" + testWorkspaceID
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	authMsg, _ := json.Marshal(map[string]any{
		"type":    "auth",
		"payload": map[string]string{"token": token},
	})
	if err := conn.WriteMessage(websocket.TextMessage, authMsg); err != nil {
		t.Fatalf("failed to send auth message: %v", err)
	}
	// Read auth_ack before returning the connection.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, ack, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read auth_ack: %v", err)
	}
	if !strings.Contains(string(ack), "auth_ack") {
		t.Fatalf("expected auth_ack, got %s", ack)
	}
	conn.SetReadDeadline(time.Time{})
	return conn
}

// totalClients counts all clients across all rooms.
func totalClients(hub *Hub) int {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	count := 0
	for _, clients := range hub.rooms {
		count += len(clients)
	}
	return count
}

func TestHub_ClientRegistration(t *testing.T) {
	hub, server := newTestHub(t)
	defer server.Close()

	conn := connectWS(t, server)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	count := totalClients(hub)
	if count != 1 {
		t.Fatalf("expected 1 client, got %d", count)
	}
}

func TestHub_Broadcast(t *testing.T) {
	hub, server := newTestHub(t)
	defer server.Close()

	conn1 := connectWS(t, server)
	defer conn1.Close()
	conn2 := connectWS(t, server)
	defer conn2.Close()

	time.Sleep(50 * time.Millisecond)

	msg := []byte(`{"type":"issue:created","data":"test"}`)
	hub.Broadcast(msg)

	conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, received1, err := conn1.ReadMessage()
	if err != nil {
		t.Fatalf("client 1 read error: %v", err)
	}
	if string(received1) != string(msg) {
		t.Fatalf("client 1: expected %s, got %s", msg, received1)
	}

	conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, received2, err := conn2.ReadMessage()
	if err != nil {
		t.Fatalf("client 2 read error: %v", err)
	}
	if string(received2) != string(msg) {
		t.Fatalf("client 2: expected %s, got %s", msg, received2)
	}
}

func TestHub_ClientDisconnect(t *testing.T) {
	hub, server := newTestHub(t)
	defer server.Close()

	conn := connectWS(t, server)

	time.Sleep(50 * time.Millisecond)

	countBefore := totalClients(hub)
	if countBefore != 1 {
		t.Fatalf("expected 1 client before disconnect, got %d", countBefore)
	}

	conn.Close()
	time.Sleep(100 * time.Millisecond)

	countAfter := totalClients(hub)
	if countAfter != 0 {
		t.Fatalf("expected 0 clients after disconnect, got %d", countAfter)
	}
}

func TestHub_BroadcastToMultipleClients(t *testing.T) {
	hub, server := newTestHub(t)
	defer server.Close()

	const numClients = 5
	conns := make([]*websocket.Conn, numClients)
	for i := 0; i < numClients; i++ {
		conns[i] = connectWS(t, server)
		defer conns[i].Close()
	}

	time.Sleep(50 * time.Millisecond)

	count := totalClients(hub)
	if count != numClients {
		t.Fatalf("expected %d clients, got %d", numClients, count)
	}

	msg := []byte(`{"type":"test","count":5}`)
	hub.Broadcast(msg)

	for i, conn := range conns {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, received, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("client %d read error: %v", i, err)
		}
		if string(received) != string(msg) {
			t.Fatalf("client %d: expected %s, got %s", i, msg, received)
		}
	}
}

func TestHub_MultipleBroadcasts(t *testing.T) {
	hub, server := newTestHub(t)
	defer server.Close()

	conn := connectWS(t, server)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	messages := []string{
		`{"type":"issue:created"}`,
		`{"type":"issue:updated"}`,
		`{"type":"issue:deleted"}`,
	}

	for _, msg := range messages {
		hub.Broadcast([]byte(msg))
	}

	for i, expected := range messages {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, received, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("message %d read error: %v", i, err)
		}
		if string(received) != expected {
			t.Fatalf("message %d: expected %s, got %s", i, expected, received)
		}
	}
}
