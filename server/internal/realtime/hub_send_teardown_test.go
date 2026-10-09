package realtime

import (
	"testing"
	"time"
)

// TestSendOnSendChannelAfterClose reproduces the panic reported when a client
// disconnects concurrently with an inbound frame or broadcast fanout.
//
// Sequence (Hub.Run serializes removeClient, but sendJSON and
// BroadcastToScopeDedup do not hold h.mu while sending):
//  1. readPump delivers an inbound frame (e.g. "ping") to handleFrame.
//  2. A concurrent read error makes readPump hand the client to unregister.
//  3. Hub.Run's removeClient closes c.send under h.mu.
//  4. handleFrame's sendJSON, running with no lock shared with removeClient,
//     sends on the closed channel and panics the whole server.
//
// The panic kills the process; writePump draining the channel cannot help a
// send that has not happened yet.
func TestSendOnSendChannelAfterClose(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	client := &Client{
		hub:         hub,
		send:        make(chan []byte, 256),
		userID:      "scan-user",
		workspaceID: "scan-ws",
	}
	hub.register <- client
	// Give Hub.Run a moment to register the client.
	time.Sleep(50 * time.Millisecond)

	// A read error and a "pong" reply race: removeClient closes c.send
	// under h.mu while sendJSON sends on it without that lock.
	hub.unregister <- client
	client.sendJSON(map[string]string{"type": "pong"})
}
