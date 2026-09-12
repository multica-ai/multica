package dingtalk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/service"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestOutboundDB_SealedQuoteAndMemoryAnchor(t *testing.T) {
	for _, scenario := range []string{"after_seal", "merged", "cross_generation"} {
		for _, restart := range []bool{false, true} {
			name := scenario + "/warm"
			if restart {
				name = scenario + "/restart"
			}
			t.Run(name, func(t *testing.T) { testOutboundSealedInput(t, scenario, restart) })
		}
	}
}

func testOutboundSealedInput(t *testing.T, scenario string, restart bool) {
	pool := dingtalkInstallTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fx := dbfx.New(pool, "", "")
	suffix := util.UUIDToString(dbid.NewV7())
	fx.UserID = fx.User(t, "Sender", "dingtalk-sealed-"+suffix+"@multica.test")
	fx.WorkspaceID = fx.Workspace(t, "DingTalk sealed input", "dingtalk-sealed-"+suffix)
	fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
	runtimeID := fx.Runtime(t, "Runtime")
	agentID := fx.Agent(t, "Agent", runtimeID)
	box, err := secretbox.New(bytes.Repeat([]byte{0xA7}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal([]byte("test-app-secret"))
	if err != nil {
		t.Fatal(err)
	}
	config, err := json.Marshal(installConfig{AppID: "delivery-" + suffix, RobotCode: "robot", AppSecretEncrypted: base64.StdEncoding.EncodeToString(sealed)})
	if err != nil {
		t.Fatal(err)
	}
	installationID := fx.Insert(t, "channel_installation", dbfx.Cols{"workspace_id": fx.WorkspaceID, "agent_id": agentID, "channel_type": "dingtalk", "config": string(config), "installer_user_id": fx.UserID, "status": "active"})
	fx.Cleanup(t, "DELETE FROM dingtalk_bot_identity WHERE installation_id = $1", installationID)
	fx.Cleanup(t, "DELETE FROM dingtalk_group_presence WHERE installation_id = $1", installationID)
	q := db.New(pool)
	httpServer := newDingtalkSendServer(t)
	client := NewClient(nil, httpServer.srv.URL)
	var done []string
	receipts := make(map[string]bool)
	newAck := func(c *Client) *ackNotifier {
		ack := NewAckNotifier(c, box.Open, nil, q)
		ack.sendReaction = func(_ context.Context, _ engine.ResolvedInstallation, msg channel.InboundMessage, name string, recall bool) error {
			if name == emotionDone && !recall {
				done = append(done, msg.MessageID)
			}
			if name == emotionAcknowledged {
				if recall {
					delete(receipts, msg.MessageID)
				} else {
					receipts[msg.MessageID] = true
				}
			}
			return nil
		}
		return ack
	}
	ack := newAck(client)
	set := NewDingTalkResolverSet(q, pool, nil, ack, nil, nil)
	cb := textCallback(convTypeGroup, true)
	cb.ConversationId = "group-" + suffix
	cb.MsgId = "first-message"
	cb.SenderStaffId = "first-sender"
	cb.Text.Content = "first question"
	cb.Text.IsReplyMsg = true
	cb.Text.RepliedMsg = &botCallbackRepliedMessage{MsgType: "text", MsgId: "historical-message", SenderNick: "Quoted author", Content: botCallbackRepliedContent{Text: "historical <tag> || context"}}
	first, ok := inboundFromCallback(cb, "delivery-"+suffix)
	if !ok {
		t.Fatal("first callback rejected")
	}
	inst, err := set.Installation.ResolveInstallation(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	userID := util.MustParseUUID(fx.UserID)
	sid, err := set.Session.EnsureSession(ctx, engine.EnsureSessionParams{Installation: inst, Sender: userID, Message: first})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"chat_session", "channel_chat_session_binding", "channel_chat_context_generation", "chat_message", "agent_task_queue"} {
		key := "chat_session_id"
		if table == "chat_session" {
			key = "id"
		}
		fx.Cleanup(t, "DELETE FROM "+table+" WHERE "+key+" = $1", sid)
	}
	fx.Cleanup(t, "DELETE FROM channel_task_delivery WHERE installation_id = $1", installationID)
	appended, err := set.Session.AppendMessage(ctx, engine.AppendParams{SessionID: sid, Sender: userID, InstallationID: inst.ID, Message: first})
	if err != nil {
		t.Fatal(err)
	}
	ack.OnIngested(ctx, inst, first, sid)
	const firstBody = "> **Quoted author:**\n>\n> [quoted content unavailable]\n\nfirst question"
	var persisted string
	if err := pool.QueryRow(ctx, "SELECT content FROM chat_message WHERE id = $1", appended.MessageID).Scan(&persisted); err != nil || persisted != firstBody {
		t.Fatalf("canonical input=%q error=%v", persisted, err)
	}
	session, err := q.GetChatSession(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	tasks := service.NewTaskService(q, pool, nil, events.New())
	var task db.AgentTaskQueue
	enqueue := func() {
		task, err = tasks.EnqueueChannelChatTask(ctx, session, userID, false, appended.ContextRevision, appended.BindingID, appended.RouteRevision)
		if err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "after_seal" {
		enqueue()
	}
	cb.MsgId = "second-message"
	cb.SenderStaffId = "second-sender"
	cb.Text = botCallbackText{Content: "second question"}
	second, ok := inboundFromCallback(cb, "delivery-"+suffix)
	if !ok {
		t.Fatal("second callback rejected")
	}
	second.ForceFresh = scenario == "cross_generation"
	if _, err := set.Session.AppendMessage(ctx, engine.AppendParams{SessionID: sid, Sender: userID, InstallationID: inst.ID, Message: second}); err != nil {
		t.Fatal(err)
	}
	ack.OnIngested(ctx, inst, second, sid)
	wantReceipts := 2
	if scenario == "merged" {
		wantReceipts = 1
	}
	if len(receipts) != wantReceipts || !receipts["second-message"] {
		t.Fatalf("batch receipts=%v want last message in each pending/sealed batch", receipts)
	}
	if scenario != "after_seal" {
		enqueue()
	}
	input, err := q.ListChatInputMessages(ctx, task.ChatInputTaskID)
	if err != nil || len(input) == 0 || input[0].Content != firstBody {
		t.Fatalf("sealed input changed: %+v %v", input, err)
	}
	delivery, err := q.GetChannelTaskDelivery(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if scenario == "cross_generation" && len(input) != 1 {
		t.Fatal("new generation changed the original task input")
	}
	// The shared core may already snapshot the right provider message. Quote
	// and Done attribution must remain correct with either core version.
	current, err := q.LockChannelChatSessionBindingForContext(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{current.Config, delivery.Config} {
		for _, forbidden := range []string{"quote_text", "source_message_id", "session_webhook", "token="} {
			if bytes.Contains(raw, []byte(forbidden)) {
				t.Fatalf("per-message data persisted in routing: %s", raw)
			}
		}
	}
	if restart {
		client = NewClient(nil, httpServer.srv.URL)
		ack = newAck(client)
	}
	if err := NewOutbound(q, box.Open, client, ack, nil).processEvent(ctx, events.Event{Type: protocol.EventChatDone, TaskID: util.UUIDToString(task.ID), ChatSessionID: util.UUIDToString(sid), Payload: protocol.ChatDonePayload{Content: "first answer"}}); err != nil {
		t.Fatal(err)
	}
	if httpServer.lastPath != pathSendGroup || len(httpServer.sendBodies) != 1 {
		t.Fatal("expected one proactive reply")
	}
	var param markdownParam
	if err := json.Unmarshal([]byte(httpServer.lastBody["msgParam"].(string)), &param); err != nil {
		t.Fatal(err)
	}
	wantQuote := "> \\> \\*\\*Quoted author:\\*\\*  \n> \\>  \n> \\> \\[quoted content unavailable\\]  \n>   \n> first question\n\n---\n\n"
	wantSource := "first-message"
	if scenario == "merged" {
		wantQuote = "> second question\n\n---\n\n"
		wantSource = "second-message"
	}
	if param.Title != defaultMarkdownTitle || param.Text != wantQuote+"first answer" {
		t.Fatalf("wrong sealed quote: %q", param.Text)
	}
	if restart {
		if len(done) != 0 {
			t.Fatalf("restart reconstructed Done from delivery: %v", done)
		}
		if len(receipts) != wantReceipts {
			t.Fatalf("restart guessed receipt anchors: %v", receipts)
		}
	} else if strings.Join(done, ",") != wantSource {
		t.Fatalf("Done anchor=%v want %s", done, wantSource)
	}
	if !restart {
		if scenario == "merged" {
			if len(receipts) != 0 {
				t.Fatalf("completed batch left receipts: %v", receipts)
			}
		} else if len(receipts) != 1 || !receipts["second-message"] {
			t.Fatalf("older terminal changed next batch receipt: %v", receipts)
		}
	}
}
