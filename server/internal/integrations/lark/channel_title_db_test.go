package lark

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This regression uses the existing Feishu producer format. It must not depend
// on a quote-format migration to keep context out of a user's Chat title.
func TestFeishuChannelTitleUsesCurrentInstructionDB(t *testing.T) {
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
	for _, tc := range []struct {
		name                 string
		start, recent, fresh bool
		command, want        string
	}{
		{name: "quoted first turn", command: "Compare current alternatives", want: "Compare current alternatives"},
		{name: "recent group first turn", recent: true, command: "Compare current alternatives", want: "Compare current alternatives"},
		{name: "new chat with quote", start: true, command: "/new Compare current alternatives", want: "Compare current alternatives"},
		{name: "consumed clear with quote", fresh: true, command: "/clear Compare current alternatives", want: "Compare current alternatives"},
		{name: "new body keeps literal clear", start: true, command: "/new /clear literal instruction", want: "/clear literal instruction"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := dbfx.New(pool, "", "")
			suffix := uuid.NewString()
			fx.UserID = fx.User(t, "Title tester", "title-"+suffix+"@multica.test")
			fx.WorkspaceID = fx.Workspace(t, "Title workspace", "title-"+suffix)
			fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
			agentID := fx.Agent(t, "Title agent", "")
			installationID := fx.Insert(t, "channel_installation", dbfx.Cols{"workspace_id": fx.WorkspaceID, "agent_id": agentID, "channel_type": "lark", "config": dbfx.Raw("'{}'::jsonb"), "status": "active", "installer_user_id": fx.UserID})
			// Sessions/messages are created by the production binder, so clean their
			// workspace scope before the fixture removes the installation and owner.
			fx.Cleanup(t, `DELETE FROM chat_session WHERE workspace_id = $1`, fx.WorkspaceID)
			fx.Cleanup(t, `DELETE FROM channel_chat_session_binding WHERE installation_id = $1`, installationID)
			fx.Cleanup(t, `DELETE FROM channel_chat_context_generation WHERE chat_session_id IN (SELECT id FROM chat_session WHERE workspace_id = $1)`, fx.WorkspaceID)
			fx.Cleanup(t, `DELETE FROM chat_message WHERE chat_session_id IN (SELECT id FROM chat_session WHERE workspace_id = $1)`, fx.WorkspaceID)
			asUUID := func(s string) pgtype.UUID {
				var u pgtype.UUID
				if err := u.Scan(s); err != nil {
					t.Fatal(err)
				}
				return u
			}
			inst := engine.ResolvedInstallation{ID: asUUID(installationID), WorkspaceID: asUUID(fx.WorkspaceID), AgentID: asUUID(agentID)}
			sender := asUUID(fx.UserID)
			fake := newEnricherFake()
			fake.byID["parent"] = []LarkMessage{textMsg("parent", "other", "Old unrelated discussion", "1000")}
			fake.byChat[ChatID(suffix)] = []LarkMessage{textMsg("recent", "other", "Old unrelated discussion", "1000")}
			in := InboundMessage{MessageID: "current", MessageType: "text", ChatID: ChatID(suffix), ChatType: ChatTypeGroup, SenderOpenID: "current-user", Body: tc.command, CommandBody: tc.command, AddressedToBot: true}
			cfg := InboundEnricherConfig{}
			if tc.recent {
				cfg.RecentContextSize = 10
			} else {
				in.ParentID = "parent"
			}
			msg := channelMessageFromLark(enrich(t, fake, in, cfg))
			if !strings.Contains(msg.Text, "Old unrelated discussion") || msg.CommandText != tc.command {
				t.Fatalf("producer lost context/source: %+v", msg)
			}
			// Router consumes /new in CommandText before calling StartSession. Text
			// has already been enriched by the real adapter and must remain intact.
			if tc.start {
				msg.CommandText, _ = engine.ParseNewChatCommand(msg.CommandText)
			}
			binder := &feishuSessionBinder{session: engine.NewChatSession(db.New(pool), pool, channel.TypeFeishu, engine.SessionTitles{})}
			var sessionID pgtype.UUID
			var result engine.AppendResult
			if tc.start {
				started, e := binder.StartSession(ctx, engine.StartSessionParams{Installation: inst, Creator: sender, Sender: sender, Message: msg, PersistMessage: true})
				if e != nil {
					t.Fatal(e)
				}
				sessionID = started.SessionID
				result = started.Append
			} else {
				sessionID, err = binder.EnsureSession(ctx, engine.EnsureSessionParams{Installation: inst, Sender: sender, Message: msg})
				if err != nil {
					t.Fatal(err)
				}
				result, err = binder.AppendMessage(ctx, engine.AppendParams{InstallationID: inst.ID, Sender: sender, SessionID: sessionID, Message: msg})
				if err != nil {
					t.Fatal(err)
				}
			}
			var title, body string
			fx.QueryRow(t, `SELECT s.title,m.content FROM chat_session s JOIN chat_message m ON m.chat_session_id=s.id WHERE s.id=$1`, sessionID).Scan(&title, &body)
			if body != msg.Text {
				t.Errorf("persisted agent context changed: got %q want %q", body, msg.Text)
			}
			if title != tc.want || result.InitialTitle != tc.want {
				t.Errorf("title = DB %q result %q; want current instruction %q", title, result.InitialTitle, tc.want)
			}
		})
	}
}
