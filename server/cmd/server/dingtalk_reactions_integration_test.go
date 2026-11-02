package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/dingtalk"
	"github.com/multica-ai/multica/server/internal/realtime"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type dingTalkReactionTransport func(*http.Request) (*http.Response, error)

func (f dingTalkReactionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the actual server assembly: the inbound notifier and outbound bus
// subscriber must share the same local source cache and reaction lifecycle.
func TestDingTalkReactionsThroughServerRouter(t *testing.T) {
	key := bytes.Repeat([]byte{0x83}, secretbox.KeySize)
	t.Setenv("MULTICA_DINGTALK_SECRET_KEY", base64.StdEncoding.EncodeToString(key))
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := box.Seal([]byte("fixture-secret"))
	if err != nil {
		t.Fatal(err)
	}
	appID := "reaction-" + dbid.NewV7().String()
	config, err := json.Marshal(map[string]string{"app_id": appID, "robot_code": appID, "app_secret_encrypted": base64.StdEncoding.EncodeToString(encrypted)})
	if err != nil {
		t.Fatal(err)
	}
	fx := dbfx.New(testPool, testWorkspaceID, testUserID)
	agentID := fx.Agent(t, "DingTalk reaction fixture", fx.Runtime(t, "DingTalk fixture runtime"))
	installation := fx.Insert(t, "channel_installation", dbfx.Cols{"workspace_id": testWorkspaceID, "agent_id": agentID, "channel_type": "dingtalk", "config": string(config), "installer_user_id": testUserID, "status": "active"})
	fx.Insert(t, "channel_user_binding", dbfx.Cols{"installation_id": installation, "workspace_id": testWorkspaceID, "channel_type": "dingtalk", "channel_user_id": "sender", "multica_user_id": testUserID})
	fx.Cleanup(t, "DELETE FROM channel_inbound_dedup WHERE installation_id = $1", installation)
	fx.Cleanup(t, "DELETE FROM channel_task_delivery WHERE installation_id = $1", installation)
	var mu sync.Mutex
	var actions []string
	received := make(chan struct{}, 1)
	originalClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: dingTalkReactionTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.dingtalk.com" {
			return nil, fmt.Errorf("unexpected outbound host: %s", r.URL.Host)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		response := `{"success":true}`
		action := ""
		switch r.URL.Path {
		case "/v1.0/oauth2/accessToken":
			if body["appKey"] != appID || body["appSecret"] != "fixture-secret" {
				return nil, fmt.Errorf("incorrect installation credentials")
			}
			response = `{"accessToken":"fixture-token","expireIn":7200}`
		case "/v1.0/robot/emotion/reply", "/v1.0/robot/emotion/recall":
			action = r.URL.Path + ":" + fmt.Sprint(body["emotionName"]) + ":" + fmt.Sprint(body["openMsgId"])
		case "/v1.0/robot/oToMessages/batchSend":
			var param struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(fmt.Sprint(body["msgParam"])), &param); err != nil {
				return nil, err
			}
			if param.Text != "answer" {
				return nil, fmt.Errorf("unexpected reply: %q", param.Text)
			}
			action = "answer"
			response = `{"processQueryKey":"sent"}`
		default:
			return nil, fmt.Errorf("unexpected provider endpoint: %s", r.URL.Path)
		}
		if action != "" {
			mu.Lock()
			actions = append(actions, action)
			mu.Unlock()
		}
		if action == "/v1.0/robot/emotion/reply:收到:source" {
			received <- struct{}{}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
	})}
	t.Cleanup(func() { http.DefaultClient = originalClient })
	bus := events.New()
	_, h := NewRouterWithOptions(testPool, realtime.NewHub(), bus, analytics.NoopClient{}, nil, RouterOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	raw, err := json.Marshal(map[string]string{"app_id": appID, "current_text": "question"})
	if err != nil {
		t.Fatal(err)
	}
	msg := channel.InboundMessage{EventID: "source", MessageID: "source", Type: channel.MsgTypeText, Text: "question", CommandText: "question", AddressedToBot: true, Raw: raw,
		Source: channel.Source{ChannelType: dingtalk.TypeDingTalk, ChatType: channel.ChatTypeP2P, ChatID: appID, SenderID: "sender"}}
	if err := h.ChannelRouter.Handle(ctx, msg); err != nil {
		t.Fatal(err)
	}
	var sessionID string
	if err := testPool.QueryRow(ctx, "SELECT chat_session_id FROM channel_chat_session_binding WHERE installation_id=$1", installation).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"chat_session", "channel_chat_session_binding", "channel_chat_context_generation", "chat_message", "agent_task_queue"} {
		column := "chat_session_id"
		if table == "chat_session" {
			column = "id"
		}
		fx.Cleanup(t, "DELETE FROM "+table+" WHERE "+column+"=$1", sessionID)
	}
	if !h.ChannelRouter.Drain(ctx) {
		t.Fatal("inbound work did not drain")
	}
	// Drain owns flush/media work, not the detached typing hook. Establish the
	// receipt before testing its terminal transition; terminal-before-ingest is
	// exercised separately and correctly suppresses the receipt altogether.
	select {
	case <-received:
	case <-ctx.Done():
		t.Fatal("receipt did not arrive")
	}
	var taskID string
	if err := testPool.QueryRow(ctx, "SELECT id FROM agent_task_queue WHERE chat_session_id=$1", sessionID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	bus.Publish(events.Event{Type: protocol.EventChatDone, TaskID: taskID, ChatSessionID: sessionID, Payload: protocol.ChatDonePayload{Content: "answer"}})
	mu.Lock()
	defer mu.Unlock()
	want := []string{"/v1.0/robot/emotion/reply:收到:source", "answer", "/v1.0/robot/emotion/recall:收到:source", "/v1.0/robot/emotion/reply:Done:source"}
	if !slices.Equal(actions, want) {
		t.Fatalf("assembled reaction lifecycle = %v, want %v", actions, want)
	}
}
