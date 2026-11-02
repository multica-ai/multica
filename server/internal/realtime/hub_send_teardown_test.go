package realtime

import "testing"

// TestSendOnSendChannelAfterClose verifies that a late inbound reply cannot
// panic the server after a broadcast evicts a slow client and closes its send
// channel. The broadcast returns only after eviction completes, so the test
// does not depend on goroutine scheduling to reach the closed-channel case.
func TestSendOnSendChannelAfterClose(t *testing.T) {
	hub := NewHub()
	key := sk(ScopeWorkspace, "scan-ws")
	client := &Client{
		hub:           hub,
		send:          make(chan []byte, 1),
		userID:        "scan-user",
		workspaceID:   key.ID,
		subscriptions: map[scopeKey]bool{key: true},
	}
	hub.clients[client] = true
	hub.rooms[key] = map[*Client]bool{client: true}
	client.send <- []byte(`{"type":"queued"}`)

	hub.BroadcastToScope(key.Type, key.ID, []byte(`{"type":"broadcast"}`))
	if hub.clients[client] {
		t.Fatal("full send buffer did not evict client")
	}
	select {
	case _, open := <-client.send: // Drain the message queued before eviction.
		if !open {
			t.Fatal("queued message missing after eviction")
		}
	default:
		t.Fatal("queued message missing after eviction")
	}
	select {
	case _, open := <-client.send:
		if open {
			t.Fatal("evicted client's send channel remained open")
		}
	default:
		t.Fatal("evicted client's send channel remained open")
	}

	client.handleFrame([]byte(`{"type":"ping"}`))
}
