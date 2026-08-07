package wecom

import (
	"context"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestConn_SilentPeerFailsRunSoUpervisorReconnects covers spec §9:
// 「心跳无响应 | 关连接触发重连」. A peer that completes the handshake and then
// answers nothing — the observable shape of a half-open TCP connection behind
// corporate NAT, routine for long-lived WebSockets — must make Run return so
// engine.Supervisor redials.
//
// Without a read deadline this is unbounded: readPump parks in ReadMessage
// forever while writePump's pings keep succeeding into the kernel send buffer,
// so neither pump errors and Run never returns. Inbound messages are silently
// dropped until TCP retransmission gives up (tcp_retries2 default ≈ 15 min on
// Linux), and the installation looks connected the whole time.
func TestConn_SilentPeerFailsRunSoSupervisorReconnects(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, c *websocket.Conn) {
		reqID, ok := awaitSubscribe(t, c)
		if !ok {
			return
		}
		writeResponse(t, c, reqID, 0, "")
		// Now go silent: keep draining so the socket stays writable (pings
		// succeed) but never send another frame.
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	})

	conn, err := NewConn(ConnConfig{
		DialURL:      fs.url,
		BotID:        "bot-1",
		Secret:       "sec-1",
		PingInterval: 40 * time.Millisecond,
		ReadTimeout:  200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- conn.Run(context.Background()) }()

	select {
	case runErr := <-done:
		// A silent peer is a connection loss, not a clean shutdown: Run must
		// report an error so Supervisor treats it as a drop and redials.
		if runErr == nil {
			t.Fatal("Run returned nil for a silent peer; Supervisor would treat it as a clean exit")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run never returned for a silent peer — readPump has no read deadline, " +
			"so a half-open connection wedges the installation until the kernel gives up")
	}
}

// TestConn_TrafficKeepsConnectionAlive is the control: the read deadline must
// be refreshed by inbound frames, so a peer that keeps answering pings stays
// connected well past one ReadTimeout window.
func TestConn_TrafficKeepsConnectionAlive(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, c *websocket.Conn) {
		reqID, ok := awaitSubscribe(t, c)
		if !ok {
			return
		}
		writeResponse(t, c, reqID, 0, "")
		for {
			f, ok := readFrame(t, c)
			if !ok {
				return
			}
			// Answer every ping, as the real endpoint does.
			writeResponse(t, c, f.Headers.ReqID, 0, "")
		}
	})

	conn, err := NewConn(ConnConfig{
		DialURL:      fs.url,
		BotID:        "bot-1",
		Secret:       "sec-1",
		PingInterval: 40 * time.Millisecond,
		ReadTimeout:  200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- conn.Run(ctx) }()

	// Several ReadTimeout windows worth of healthy traffic.
	select {
	case runErr := <-done:
		t.Fatalf("Run exited early on a healthy connection: %v", runErr)
	case <-time.After(700 * time.Millisecond):
	}

	cancel()
	select {
	case runErr := <-done:
		if runErr != nil {
			t.Fatalf("Run after ctx cancel = %v, want nil (clean shutdown)", runErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}
