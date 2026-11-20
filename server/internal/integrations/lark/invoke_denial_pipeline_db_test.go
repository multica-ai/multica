package lark

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/service"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestFeishuDeniedMentionRepliesOnceWithoutStartingRun(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("explicit disposable DATABASE_URL required")
	}
	pool := channelScopeTestDB(t)
	fx := dbfx.New(pool, "", "")
	suffix := uuid.NewString()
	fx.UserID = fx.User(t, "Agent owner", "owner-"+suffix+"@example.invalid")
	fx.WorkspaceID = fx.Workspace(t, "Denied mention", "denied-mention-"+suffix)
	sender := fx.User(t, "Group sender", "sender-"+suffix+"@example.invalid")
	fx.Member(t, fx.WorkspaceID, sender, "member")
	agentID := fx.Agent(t, "Private agent", fx.Runtime(t, "Denial fixture"))
	appID := "cli_denial_" + suffix
	cfg, err := json.Marshal(map[string]string{"app_id": appID, "region": "lark"})
	if err != nil {
		t.Fatal(err)
	}
	installationID := fx.Insert(t, "channel_installation", dbfx.Cols{
		"workspace_id": fx.WorkspaceID, "agent_id": agentID, "channel_type": channelTypeFeishu,
		"installer_user_id": fx.UserID, "config": string(cfg), "status": "active",
	})
	fx.Insert(t, "channel_user_binding", dbfx.Cols{
		"workspace_id": fx.WorkspaceID, "multica_user_id": sender, "installation_id": installationID,
		"channel_type": channelTypeFeishu, "channel_user_id": "ou_member",
	})
	fx.Cleanup(t, "DELETE FROM channel_inbound_audit WHERE installation_id=$1", installationID)
	fx.Cleanup(t, "DELETE FROM channel_inbound_message_dedup WHERE installation_id=$1", installationID)

	fake := newLarkFake(t)
	fake.stubToken("test-token", 7200)
	fake.stubReply(map[string]any{"code": 0, "data": map[string]string{"message_id": "om_denial"}},
		func(_ *http.Request, id string, body map[string]any) {
			if id != "om_addressed" || body["msg_type"] != "text" || body["reply_in_thread"] != true {
				t.Errorf("expected text reply to addressed topic message, got ID %q, body %v", id, body)
			}
			var content struct{ Text string }
			if raw, ok := body["content"].(string); !ok || json.Unmarshal([]byte(raw), &content) != nil ||
				!strings.Contains(content.Text, "ask the agent's owner") {
				t.Errorf("missing permission recovery guidance: %v", body["content"])
			}
		})
	logger := newDiscardLogger()
	queries := db.New(pool)
	replier := NewLarkOutcomeReplier(OutcomeReplierConfig{
		APIClient:  NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL(), Logger: logger}),
		BindingSvc: &BindingTokenService{}, Credentials: stubCredentialsResolver{secret: "test-secret"},
		Queries: queries, AppURL: "https://multica.test", Logger: logger,
	})
	router := engine.NewRouter(nil, &service.TaskService{Queries: queries}, queries, engine.RouterConfig{Logger: logger})
	router.Register(channel.TypeFeishu, NewFeishuResolverSet(NewChannelStore(queries),
		engine.NewChatSession(queries, pool, channel.TypeFeishu, engine.SessionTitles{}), NewAuditLogger(queries), replier, nil, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msg := InboundMessage{
		AppID: appID, EventType: "im.message.receive_v1", EventID: "evt_unaddressed", MessageID: "om_unaddressed",
		ChatID: "oc_group", ChatType: ChatTypeGroup, SenderOpenID: "ou_member",
		ThreadID: "omt_topic", MessageType: "text", Body: "Help me with the report",
	}
	if err := router.Handle(ctx, channelMessageFromLark(msg)); err != nil {
		t.Fatal(err)
	}
	msg.EventID, msg.MessageID, msg.AddressedToBot = "evt_addressed", "om_addressed", true
	for range 2 {
		if err := router.Handle(ctx, channelMessageFromLark(msg)); err != nil {
			t.Fatal(err)
		}
	}
	if !router.Drain(ctx) {
		t.Fatal("outbound reply did not settle")
	}
	if n := fake.sendN.Load(); n != 1 {
		t.Errorf("permission notice attempts = %d, want 1 across redelivery and unaddressed chatter", n)
	}
	if n := ccCount(t, ctx, pool, "SELECT count(*) FROM channel_inbound_audit WHERE installation_id=$1 AND channel_event_id=$2 AND drop_reason=$3",
		installationID, msg.EventID, string(engine.DropReasonInvokeDenied)); n != 1 {
		t.Errorf("recorded denial audits = %d, want 1", n)
	}
	if n := ccCount(t, ctx, pool, "SELECT count(*) FROM chat_session WHERE agent_id=$1", agentID); n != 0 {
		t.Errorf("denied mention created %d chat sessions", n)
	}
	if n := ccCount(t, ctx, pool, "SELECT count(*) FROM agent_task_queue WHERE agent_id=$1", agentID); n != 0 {
		t.Errorf("denied mention enqueued %d tasks", n)
	}
}
