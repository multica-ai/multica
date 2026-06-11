//go:build manual

// Manual end-to-end PoC against a real Octo server. Excluded from normal builds
// and CI by the `manual` build tag (no credentials in CI).
//
// Run:
//
//	OCTO_API=https://<octo-host>/api \
//	OCTO_TOKEN=bf_xxx \
//	go test -tags manual -run TestManual -v ./internal/integrations/im/
//
// TestManualSend           — register + send one message to the bot owner (DM).
// TestManualReceive        — register + open WS, print inbound messages for 60s.
// TestManualEcho           — register + WS; echo every inbound DM back to sender.
package transport

import (
	"context"
	"os"
	"testing"
	"time"
)

func manualEnv(t *testing.T) (apiURL, token string) {
	t.Helper()
	apiURL = os.Getenv("OCTO_API")
	token = os.Getenv("OCTO_TOKEN")
	if apiURL == "" || token == "" {
		t.Skip("set OCTO_API and OCTO_TOKEN to run manual PoC")
	}
	return apiURL, token
}

// TestManualSend verifies the REST path: register, then DM the bot owner.
func TestManualSend(t *testing.T) {
	apiURL, token := manualEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hc := NewHTTPClient(apiURL, token)
	reg, err := hc.Register(ctx, false, "Multica", "0.1.0")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Logf("registered: robot_id=%s owner_uid=%s api_url=%s ws_url=%s", reg.RobotID, reg.OwnerUID, reg.APIURL, reg.WSURL)
	hc.SetAPIURL(reg.APIURL)

	res, err := hc.SendMessage(ctx, SendMessageParams{
		ChannelID:   reg.OwnerChannelID, // DM the owner
		ChannelType: ChannelDM,
		Content:     "PoC ✅ multica im 包出站测试 — " + time.Now().Format("15:04:05"),
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	t.Logf("sent OK: message_id=%s message_seq=%d client_msg_no=%s", res.MessageID, res.MessageSeq, res.ClientMsgNo)
	if res.MessageID == "" {
		t.Errorf("empty message_id in response — int64 decode likely broken")
	}
}

// TestManualReceive verifies the WS path: register, connect, print every inbound
// message for 60 seconds. Send the bot a DM from the Octo client to see it here.
func TestManualReceive(t *testing.T) {
	apiURL, token := manualEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()

	hc := NewHTTPClient(apiURL, token)
	reg, err := hc.Register(ctx, false, "Multica", "0.1.0")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Logf("registered robot_id=%s ws_url=%s — send the bot a DM now", reg.RobotID, reg.WSURL)

	got := make(chan BotMessage, 8)
	sock := NewSocket(SocketOptions{
		WSURL:          reg.WSURL,
		UID:            reg.RobotID,
		Token:          reg.IMToken,
		OnConnected:    func() { t.Logf("WS connected") },
		OnDisconnected: func() { t.Logf("WS disconnected") },
		OnError:        func(e error) { t.Logf("WS error: %v", e) },
		OnMessage: func(m BotMessage) {
			t.Logf("RECV from=%s ch=%s/%d type=%d content=%q stream=%v",
				m.FromUID, m.ChannelID, m.ChannelType, m.Payload.Type, m.Payload.Content, m.StreamOn)
			select {
			case got <- m:
			default:
			}
		},
		Logf: t.Logf,
	})
	if err := sock.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sock.Disconnect()

	select {
	case <-got:
		t.Logf("received at least one message — inbound path works")
	case <-ctx.Done():
		t.Logf("timeout reached (no message received in window)")
	}
}

// TestManualEcho is the full round-trip: register, connect, and echo every
// inbound DM back to its sender via REST. Exercises decrypt + reply together.
func TestManualEcho(t *testing.T) {
	apiURL, token := manualEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	hc := NewHTTPClient(apiURL, token)
	reg, err := hc.Register(ctx, false, "Multica", "0.1.0")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	hc.SetAPIURL(reg.APIURL)
	t.Logf("echo bot up: robot_id=%s — DM it and watch for an echo reply", reg.RobotID)

	sock := NewSocket(SocketOptions{
		WSURL:       reg.WSURL,
		UID:         reg.RobotID,
		Token:       reg.IMToken,
		OnConnected: func() { t.Logf("WS connected") },
		OnError:     func(e error) { t.Logf("WS error: %v", e) },
		OnMessage: func(m BotMessage) {
			// Ignore our own messages and non-text.
			if m.FromUID == reg.RobotID || m.Payload.Type != MsgText {
				return
			}
			t.Logf("RECV %q from %s — echoing back", m.Payload.Content, m.FromUID)
			sendCtx, c := context.WithTimeout(context.Background(), 15*time.Second)
			defer c()
			res, err := hc.SendMessage(sendCtx, SendMessageParams{
				ChannelID:   m.ChannelID,
				ChannelType: m.ChannelType,
				Content:     "echo: " + m.Payload.Content,
				ReplyMsgID:  m.MessageID,
			})
			if err != nil {
				t.Logf("echo send failed: %v", err)
				return
			}
			t.Logf("echo sent: message_id=%s", res.MessageID)
		},
		Logf: t.Logf,
	})
	if err := sock.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sock.Disconnect()

	<-ctx.Done()
	t.Logf("echo window ended")
}
