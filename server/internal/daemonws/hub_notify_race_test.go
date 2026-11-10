package daemonws

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

// TestTrySendAfterCloseReturnsFalse locks the teardown state the slow-client
// path depends on: once unregister has marked the channel closed under sendMu,
// a later send must be dropped instead of panicking.
func TestTrySendAfterCloseReturnsFalse(t *testing.T) {
	c := &client{send: make(chan []byte, 1)}
	c.sendMu.Lock()
	c.sendClosed = true
	c.sendMu.Unlock()

	if c.trySend([]byte("late")) {
		t.Fatal("trySend returned true after sendClosed was set")
	}
}

// TestNotifyDoesNotSendOnClosedClient races the three notify paths against
// unregister. Each notify holds only h.mu's read lock while unregister closes
// c.send under the write lock, so a bare send on c.send panics the process.
// trySend observes the close under sendMu and returns false instead.
//
// A full send buffer is what makes the window real: the notify then takes the
// slow path and calls unregister itself, closing the channel while the other
// goroutines are still inside their read-locked send.
func TestNotifyDoesNotSendOnClosedClient(t *testing.T) {
	const rounds = 2000
	frame := []byte("wakeup")

	// The slow path closes the connection, so the client needs a real socket.
	// Nothing is read from it: a full send buffer is what drives eviction.
	serverConn, clientConn := dialDaemonPair(t)
	defer serverConn.Close()
	defer clientConn.Close()

	for i := 0; i < rounds; i++ {
		hub := NewHub()
		c := &client{
			conn:     serverConn,
			send:     make(chan []byte, 1),
			identity: ClientIdentity{UserID: "user-1", WorkspaceIDs: []string{"ws-1"}},
			runtimes: map[string]struct{}{"rt-1": {}},
		}
		hub.mu.Lock()
		hub.clients[c] = true
		hub.byRuntime["rt-1"] = map[*client]bool{c: true}
		hub.byWorkspace["ws-1"] = map[*client]bool{c: true}
		hub.byUser["user-1"] = map[*client]bool{c: true}
		hub.mu.Unlock()
		c.send <- []byte("full")

		var wg sync.WaitGroup
		wg.Add(4)
		go func() {
			defer wg.Done()
			hub.notifyFrame("rt-1", frame, "")
		}()
		go func() {
			defer wg.Done()
			hub.notifyWorkspaceFrame("ws-1", frame, "")
		}()
		go func() {
			defer wg.Done()
			hub.notifyUserFrame("user-1", frame, "")
		}()
		go func() {
			defer wg.Done()
			hub.unregister(c)
		}()
		wg.Wait()

		// The next round re-registers the same socket, so reopen the channel
		// unregister just closed and put the client back on the books.
		c.send = make(chan []byte, 1)
		c.sendClosed = false
	}
}

// dialDaemonPair opens one WebSocket and returns both ends. The server end is
// what the hub closes when it evicts a slow client.
func dialDaemonPair(t *testing.T) (serverConn, clientConn *websocket.Conn) {
	t.Helper()
	upgrader := websocket.Upgrader{}
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
	}))
	t.Cleanup(server.Close)

	clientConn, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):], nil)
	if err != nil {
		t.Fatalf("dial daemon pair: %v", err)
	}
	return <-accepted, clientConn
}
