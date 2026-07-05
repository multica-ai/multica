package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// fakeStreamServer stands in for DingTalk's gateway + Stream endpoint: it
// serves /v1.0/gateway/connections/open and upgrades /stream?ticket=… to a
// WebSocket the test scripts frames onto.
type fakeStreamServer struct {
	t        *testing.T
	upgrader websocket.Upgrader

	mu       sync.Mutex
	gateway  []map[string]any // captured gateway open requests
	conn     *websocket.Conn
	acks     chan streamFrameResponse
	connOpen chan struct{}
}

func newFakeStreamServer(t *testing.T) (*fakeStreamServer, *httptest.Server) {
	f := &fakeStreamServer{
		t:        t,
		acks:     make(chan streamFrameResponse, 16),
		connOpen: make(chan struct{}, 1),
	}
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc(streamGatewayPath, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.gateway = append(f.gateway, body)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		endpoint := "ws" + strings.TrimPrefix(srv.URL, "http") + "/stream"
		_ = json.NewEncoder(w).Encode(map[string]string{"endpoint": endpoint, "ticket": "tk_test"})
	})
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ticket") != "tk_test" {
			http.Error(w, "bad ticket", http.StatusForbidden)
			return
		}
		conn, err := f.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conn = conn
		f.mu.Unlock()
		f.connOpen <- struct{}{}
		// Read loop: collect ACK frames the client writes back.
		for {
			var resp streamFrameResponse
			if err := conn.ReadJSON(&resp); err != nil {
				return
			}
			f.acks <- resp
		}
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return f, srv
}

// sendFrame scripts one frame from the "server" side.
func (f *fakeStreamServer) sendFrame(t *testing.T, frame streamFrame) {
	t.Helper()
	f.mu.Lock()
	conn := f.conn
	f.mu.Unlock()
	if conn == nil {
		t.Fatal("no websocket connection")
	}
	if err := conn.WriteJSON(frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

func (f *fakeStreamServer) waitAck(t *testing.T) streamFrameResponse {
	t.Helper()
	select {
	case ack := <-f.acks:
		return ack
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ack")
		return streamFrameResponse{}
	}
}

// newTestChannel builds a dingtalkChannel against the fake server with a
// recording inbound handler.
func newTestChannel(t *testing.T, srvURL string, handler channel.InboundHandler) *dingtalkChannel {
	t.Helper()
	box, err := secretbox.New(make([]byte, 32))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	sealed, err := box.Seal([]byte("s3cret"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	cfg, err := encodeInstallConfig(Installation{ClientID: "ding_client", AppSecretEncrypted: sealed})
	if err != nil {
		t.Fatalf("encode config: %v", err)
	}
	factory := newDingTalkFactory(ChannelDeps{Decrypt: box.Open, OpenAPIBase: srvURL})
	ch, err := factory(channel.Config{Type: TypeDingtalk, Raw: cfg, Handler: handler})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	return ch.(*dingtalkChannel)
}

func botCallbackFrame(t *testing.T, data botCallbackData) streamFrame {
	t.Helper()
	payload, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}
	return streamFrame{
		SpecVersion: "1.0",
		Type:        streamFrameTypeCallback,
		Headers: map[string]string{
			streamHeaderTopic:       streamTopicBotMessage,
			streamHeaderMessageID:   "frame-1",
			streamHeaderContentType: streamContentTypeJSON,
		},
		Data: string(payload),
	}
}

func TestChannelConnectHandlesPingCallbackAndDisconnect(t *testing.T) {
	f, srv := newFakeStreamServer(t)

	var mu sync.Mutex
	var received []channel.InboundMessage
	handler := func(ctx context.Context, msg channel.InboundMessage) error {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, msg)
		return nil
	}
	ch := newTestChannel(t, srv.URL, handler)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	connectErr := make(chan error, 1)
	go func() { connectErr <- ch.Connect(ctx) }()

	select {
	case <-f.connOpen:
	case <-time.After(5 * time.Second):
		t.Fatal("connection never opened")
	}

	// The gateway open carried the credentials + the bot-message topic.
	f.mu.Lock()
	gw := f.gateway[0]
	f.mu.Unlock()
	if gw["clientId"] != "ding_client" || gw["clientSecret"] != "s3cret" {
		t.Errorf("gateway credentials = %v", gw)
	}
	subs, _ := json.Marshal(gw["subscriptions"])
	if !strings.Contains(string(subs), streamTopicBotMessage) {
		t.Errorf("gateway subscriptions = %s", subs)
	}

	// SYSTEM ping → pong mirroring the data, echoing messageId.
	f.sendFrame(t, streamFrame{
		Type:    streamFrameTypeSystem,
		Headers: map[string]string{streamHeaderTopic: streamTopicPing, streamHeaderMessageID: "ping-1"},
		Data:    `{"ts": 123}`,
	})
	pong := f.waitAck(t)
	if pong.Code != streamAckCodeOK || pong.Headers[streamHeaderMessageID] != "ping-1" || pong.Data != `{"ts": 123}` {
		t.Errorf("pong = %+v", pong)
	}

	// CALLBACK bot message → ACK + normalized inbound delivery.
	f.sendFrame(t, botCallbackFrame(t, botCallbackData{
		ConversationID:   "cidXXX==",
		MsgID:            "msg_1",
		SenderStaffID:    "staff_1",
		SenderNick:       "小明",
		SessionWebhook:   "https://oapi.dingtalk.com/robot/sendBySession?session=abc",
		ConversationType: "1",
		Msgtype:          "text",
		Text: struct {
			Content string `json:"content"`
		}{Content: " 你好 "},
	}))
	ack := f.waitAck(t)
	if ack.Code != streamAckCodeOK || ack.Headers[streamHeaderMessageID] != "frame-1" {
		t.Errorf("callback ack = %+v", ack)
	}
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) == 1
	}, "inbound delivery")
	mu.Lock()
	msg := received[0]
	mu.Unlock()
	if msg.MessageID != "msg_1" || msg.Text != "你好" || msg.Source.SenderID != "staff_1" {
		t.Errorf("inbound = %+v", msg)
	}
	if msg.Source.ChatType != channel.ChatTypeP2P || msg.Source.ChatID != "cidXXX==" {
		t.Errorf("inbound source = %+v", msg.Source)
	}
	var raw dingtalkRawEvent
	if err := json.Unmarshal(msg.Raw, &raw); err != nil || raw.ClientID != "ding_client" || raw.SessionWebhook == "" {
		t.Errorf("raw = %+v err=%v", raw, err)
	}

	// SYSTEM disconnect → Connect returns an error so the supervisor
	// redials with a fresh gateway grant.
	f.sendFrame(t, streamFrame{
		Type:    streamFrameTypeSystem,
		Headers: map[string]string{streamHeaderTopic: streamTopicDisconnect, streamHeaderMessageID: "disc-1"},
	})
	select {
	case err := <-connectErr:
		if err == nil || !strings.Contains(err.Error(), "disconnect") {
			t.Errorf("Connect error = %v, want server-requested disconnect", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Connect did not return after disconnect frame")
	}
}

func TestChannelConnectReturnsNilOnContextCancel(t *testing.T) {
	f, srv := newFakeStreamServer(t)
	ch := newTestChannel(t, srv.URL, func(context.Context, channel.InboundMessage) error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	connectErr := make(chan error, 1)
	go func() { connectErr <- ch.Connect(ctx) }()
	select {
	case <-f.connOpen:
	case <-time.After(5 * time.Second):
		t.Fatal("connection never opened")
	}
	cancel()
	select {
	case err := <-connectErr:
		if err != nil {
			t.Errorf("Connect after cancel = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Connect did not return after cancel")
	}
}

func TestChannelConnectPropagatesHandlerInfraError(t *testing.T) {
	f, srv := newFakeStreamServer(t)
	infra := errors.New("db down")
	ch := newTestChannel(t, srv.URL, func(context.Context, channel.InboundMessage) error { return infra })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	connectErr := make(chan error, 1)
	go func() { connectErr <- ch.Connect(ctx) }()
	select {
	case <-f.connOpen:
	case <-time.After(5 * time.Second):
		t.Fatal("connection never opened")
	}
	f.sendFrame(t, botCallbackFrame(t, botCallbackData{
		ConversationID: "cid", MsgID: "m1", SenderStaffID: "s1", ConversationType: "2", Msgtype: "text",
	}))
	// The ACK still goes out before the handler runs.
	if ack := f.waitAck(t); ack.Code != streamAckCodeOK {
		t.Errorf("ack = %+v", ack)
	}
	select {
	case err := <-connectErr:
		if !errors.Is(err, infra) {
			t.Errorf("Connect error = %v, want handler infra error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Connect did not propagate handler error")
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
