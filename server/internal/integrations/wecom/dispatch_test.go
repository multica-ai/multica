package wecom

// dispatch_test.go — dispatchFrame routing, driven with a recording wsConn and
// a capturing handler (no network). Covers the receipt for a kind that cannot
// be read, the text path reaching the handler, the superseded-disconnect
// escalation, and the server-ping / pong frames. Media routing lives next
// door in inbound_media_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func testChannel(handler channel.InboundHandler) *wecomChannel {
	return &wecomChannel{botID: "bot-1", handler: handler}
}

func msgCallbackFrame(t *testing.T, msgType, content string) frameEnvelope {
	t.Helper()
	mc := aibotMsgCallback{MsgID: "m1", ChatID: "CHAT_1", ChatType: "single", MsgType: msgType}
	mc.From.UserID = "USER_A"
	mc.Text.Content = content
	body, err := json.Marshal(mc)
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}
	return frameEnvelope{Cmd: cmdMsgCallback, Body: body}
}

func TestDispatchFrame_TextReachesHandler(t *testing.T) {
	t.Parallel()
	var got channel.InboundMessage
	called := false
	c := testChannel(func(_ context.Context, m channel.InboundMessage) error {
		called, got = true, m
		return nil
	})
	conn := &recordingConn{}
	err := c.dispatchFrame(context.Background(), msgCallbackFrame(t, "text", "hello"), conn.autoAck(newWSSender(conn, nil)), slog.Default())
	if err != nil {
		t.Fatalf("dispatchFrame: %v", err)
	}
	if !called {
		t.Fatal("handler was not called for a text message")
	}
	if got.Text != "hello" {
		t.Errorf("handler got text %q, want hello", got.Text)
	}
	if len(conn.frames) != 0 {
		t.Errorf("text message should not send a receipt, got %d frames", len(conn.frames))
	}
}

func TestDispatchFrame_UnreadableKindGetsReceiptAndSkipsHandler(t *testing.T) {
	t.Parallel()
	// Two ways a callback can be unreadable: a kind this adapter does not
	// model at all, and a kind it does model that arrived without the one
	// field that makes it usable (an image frame carrying no url).
	for _, name := range []string{"location", "image"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			called := false
			c := testChannel(func(context.Context, channel.InboundMessage) error {
				called = true
				return nil
			})
			conn := &recordingConn{}
			err := c.dispatchFrame(context.Background(), msgCallbackFrame(t, name, ""), conn.autoAck(newWSSender(conn, nil)), slog.Default())
			if err != nil {
				t.Fatalf("dispatchFrame: %v", err)
			}
			if called {
				t.Error("handler must NOT be called for a message with nothing readable in it")
			}
			if len(conn.frames) != 1 {
				t.Fatalf("expected one receipt, got %d frames", len(conn.frames))
			}
			body := conn.sendBody(t, 0)
			if body["chatid"] != "CHAT_1" {
				t.Errorf("receipt chatid = %v, want the same chat CHAT_1", body["chatid"])
			}
		})
	}
}

func TestDispatchFrame_DisconnectedEventEscalates(t *testing.T) {
	t.Parallel()
	c := testChannel(func(context.Context, channel.InboundMessage) error { return nil })
	ec := aibotEventCallback{}
	ec.Event.EventType = eventDisconnected
	body, _ := json.Marshal(ec)
	err := c.dispatchFrame(context.Background(), frameEnvelope{Cmd: cmdEventCallback, Body: body}, newWSSender(&recordingConn{}, nil), slog.Default())
	if err == nil {
		t.Fatal("a disconnected_event must return an error so the Supervisor reconnects")
	}
}

func TestDispatchFrame_ServerPingIsPonged(t *testing.T) {
	t.Parallel()
	c := testChannel(func(context.Context, channel.InboundMessage) error { return nil })
	conn := &recordingConn{}
	err := c.dispatchFrame(context.Background(), frameEnvelope{Cmd: cmdServerPing, Headers: frameHeaders{ReqID: "r1"}}, conn.autoAck(newWSSender(conn, nil)), slog.Default())
	if err != nil {
		t.Fatalf("dispatchFrame: %v", err)
	}
	if len(conn.frames) != 1 || conn.frames[0].Cmd != cmdPong {
		t.Errorf("server ping should be answered with a pong, got %+v", conn.frames)
	}
}

func TestDispatchFrame_HandlerErrorPropagates(t *testing.T) {
	t.Parallel()
	c := testChannel(func(context.Context, channel.InboundMessage) error { return errors.New("boom") })
	err := c.dispatchFrame(context.Background(), msgCallbackFrame(t, "text", "hi"), newWSSender(&recordingConn{}, nil), slog.Default())
	if err == nil {
		t.Fatal("a handler error must propagate so the read loop can escalate")
	}
}

func TestChannelGetters(t *testing.T) {
	t.Parallel()
	c := testChannel(nil)
	if c.Type() != TypeWecom {
		t.Errorf("Type() = %v, want %v", c.Type(), TypeWecom)
	}
	if c.Capabilities()&channel.CapText == 0 {
		t.Error("Capabilities() should include CapText")
	}
	if err := c.Disconnect(context.Background()); err != nil {
		t.Errorf("Disconnect() = %v, want nil", err)
	}
	if _, err := c.Send(context.Background(), channel.OutboundMessage{}); !errors.Is(err, ErrSendNotSupported) {
		t.Errorf("Send() = %v, want ErrSendNotSupported", err)
	}
}

// TestSend_NeverGuessesChatType pins the fix for the removed len(ChatID)>32
// heuristic: Send must reject regardless of ChatID length — a long id must not
// route as a group send, a short one must not route as a private send. If a
// length heuristic ever comes back, one of these stops returning the sentinel.
func TestSend_NeverGuessesChatType(t *testing.T) {
	t.Parallel()
	c := testChannel(func(context.Context, channel.InboundMessage) error { return nil })
	for _, chatID := range []string{"short", "a-very-long-chat-id-well-over-thirty-two-characters-long"} {
		_, err := c.Send(context.Background(), channel.OutboundMessage{ChatID: chatID, Text: "hi"})
		if !errors.Is(err, ErrSendNotSupported) {
			t.Errorf("Send(ChatID=%q) = %v, want ErrSendNotSupported (no length heuristic)", chatID, err)
		}
	}
}
