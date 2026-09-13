package engine_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/integrations/dingtalk"
	"github.com/multica-ai/multica/server/internal/integrations/lark"
	"github.com/multica-ai/multica/server/internal/integrations/slack"
	"github.com/multica-ai/multica/server/internal/integrations/telegram"
	"github.com/multica-ai/multica/server/internal/integrations/wecom"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Every adapter must forward the current instruction on the /new path. Testing
// the real binders also covers Slack, whose session dependency is concrete.
func TestChannelStartTitleCommandMappingDB(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is required for the channel title integration test")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"feishu", "telegram", "dingtalk", "slack", "wecom"} {
		t.Run(platform, func(t *testing.T) {
			fx := dbfx.New(pool, "", "")
			suffix := uuid.NewString()
			fx.UserID = fx.User(t, "Title tester", "title-"+suffix+"@multica.test")
			fx.WorkspaceID = fx.Workspace(t, "Title workspace", "title-"+suffix)
			fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
			agent := fx.Agent(t, "Title agent", "")
			installType := platform
			if installType == "feishu" {
				installType = "lark"
			}
			installation := fx.Insert(t, "channel_installation", dbfx.Cols{"workspace_id": fx.WorkspaceID, "agent_id": agent, "channel_type": installType, "config": dbfx.Raw("'{}'::jsonb"), "status": "active", "installer_user_id": fx.UserID})
			fx.Cleanup(t, `DELETE FROM chat_session WHERE workspace_id=$1`, fx.WorkspaceID)
			fx.Cleanup(t, `DELETE FROM channel_chat_session_binding WHERE installation_id=$1`, installation)
			fx.Cleanup(t, `DELETE FROM channel_chat_context_generation WHERE chat_session_id IN (SELECT id FROM chat_session WHERE workspace_id=$1)`, fx.WorkspaceID)
			fx.Cleanup(t, `DELETE FROM chat_message WHERE chat_session_id IN (SELECT id FROM chat_session WHERE workspace_id=$1)`, fx.WorkspaceID)
			asUUID := func(s string) pgtype.UUID {
				var u pgtype.UUID
				if err := u.Scan(s); err != nil {
					t.Fatal(err)
				}
				return u
			}
			q := db.New(pool)
			session := engine.NewChatSession(q, pool, channel.Type(platform), engine.SessionTitles{})
			var binder engine.SessionBinder
			switch platform {
			case "feishu":
				binder = lark.NewFeishuResolverSet(nil, session, nil, nil, nil, nil).Session
			case "telegram":
				binder = telegram.NewTelegramResolverSet(q, pool, nil, nil).Session
			case "dingtalk":
				binder = dingtalk.NewDingTalkResolverSet(q, pool, nil, nil, nil, nil).Session
			case "slack":
				binder = slack.NewSlackResolverSet(q, pool, nil, nil, nil).Session
			case "wecom":
				binder = wecom.NewResolverSet(nil, session, nil, nil).Session
			}
			msg := channel.InboundMessage{MessageID: "current", CommandText: "Current instruction", Text: "<quoted_message>\nHistorical subject\n</quoted_message>\n\nCurrent instruction", Source: channel.Source{ChannelType: channel.Type(platform), ChatType: channel.ChatTypeP2P, ChatID: suffix, SenderID: "sender"}}
			result, err := binder.StartSession(ctx, engine.StartSessionParams{Installation: engine.ResolvedInstallation{ID: asUUID(installation), WorkspaceID: asUUID(fx.WorkspaceID), AgentID: asUUID(agent)}, Creator: asUUID(fx.UserID), Sender: asUUID(fx.UserID), Message: msg, PersistMessage: true})
			if err != nil {
				t.Fatal(err)
			}
			var title, body string
			fx.QueryRow(t, `SELECT s.title,m.content FROM chat_session s JOIN chat_message m ON m.chat_session_id=s.id WHERE s.id=$1`, result.SessionID).Scan(&title, &body)
			if title != "Current instruction" || result.Append.InitialTitle != title {
				t.Errorf("title DB=%q result=%q, want current instruction", title, result.Append.InitialTitle)
			}
			if body != msg.Text {
				t.Errorf("persisted context=%q, want %q", body, msg.Text)
			}
		})
	}
}
