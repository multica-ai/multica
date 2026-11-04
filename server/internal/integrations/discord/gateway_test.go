package discord

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestGatewayIdentifyHeartbeatAndDispatch drives one Gateway session end to
// end: HELLO → IDENTIFY → a MESSAGE_CREATE dispatch reaches onMessage, and a
// server-requested reconnect ends run with an error so the Supervisor redials.
func TestGatewayIdentifyHeartbeatAndDispatch(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var identified bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{"op": opHello, "d": map[string]any{"heartbeat_interval": 50}})

		var identify gatewayPayload
		if err := conn.ReadJSON(&identify); err != nil {
			t.Errorf("read identify: %v", err)
			return
		}
		if identify.Op != opIdentify {
			t.Errorf("op = %d, want identify", identify.Op)
			return
		}
		var d struct {
			Token   string `json:"token"`
			Intents int    `json:"intents"`
		}
		if err := json.Unmarshal(identify.D, &d); err != nil {
			t.Errorf("identify payload: %v", err)
			return
		}
		if d.Token != "tok" || d.Intents&intentMessageContent == 0 {
			t.Errorf("identify = %+v", d)
		}
		identified = true

		_ = conn.WriteJSON(map[string]any{"op": opDispatch, "s": 1, "t": "MESSAGE_CREATE", "d": map[string]any{
			"id": "m1", "channel_id": "ch-1", "content": "hi", "author": map[string]any{"id": "u1"},
		}})
		// One heartbeat must arrive, then the server asks for a reconnect.
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var hb gatewayPayload
		if err := conn.ReadJSON(&hb); err != nil {
			t.Errorf("read heartbeat: %v", err)
			return
		}
		if hb.Op != opHeartbeat {
			t.Errorf("op = %d, want heartbeat", hb.Op)
		}
		_ = conn.WriteJSON(map[string]any{"op": opHeartbeatAck})
		_ = conn.WriteJSON(map[string]any{"op": opReconnect})
	}))
	defer srv.Close()

	got := make(chan gatewayMessage, 1)
	g := &gatewayConn{
		botID:  "bot-1",
		token:  "tok",
		url:    "ws" + strings.TrimPrefix(srv.URL, "http"),
		dial:   gorillaDial,
		logger: slog.Default(),
		onMessage: func(_ context.Context, m gatewayMessage) error {
			got <- m
			return nil
		},
	}
	err := g.run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "reconnect") {
		t.Fatalf("run error = %v, want a reconnect error", err)
	}
	if !identified {
		t.Fatal("server never received IDENTIFY")
	}
	select {
	case m := <-got:
		if m.ID != "m1" || m.ChannelID != "ch-1" || m.Content != "hi" {
			t.Fatalf("message = %+v", m)
		}
	default:
		t.Fatal("MESSAGE_CREATE never reached onMessage")
	}
}
