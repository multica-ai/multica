package lark

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestContextMediaCollisionPreservesBindingDB(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("explicit disposable DATABASE_URL required")
	}
	pool := channelScopeTestDB(t)
	for _, source := range []string{"quoted-post", "partial-failure"} {
		for _, issue := range []bool{false, true} {
			name := source + "/chat"
			if issue {
				name = source + "/issue"
			}
			t.Run(name, func(t *testing.T) {
				fx := testutil.New(pool, "", "")
				suffix := uuid.NewString()
				fx.UserID = fx.User(t, "Reporter", "marker-"+suffix+"@example.invalid")
				fx.WorkspaceID = fx.Workspace(t, "Marker collision", "marker-"+suffix)
				fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
				agentID := fx.Agent(t, "Marker agent", "")
				var issueID pgtype.UUID
				if issue {
					issueID = util.MustParseUUID(fx.Issue(t, "Compare", testutil.Cols{"description": "Reporter details"}))
				}
				sessionID := util.MustParseUUID(fx.ChatSession(t, agentID))
				installationID := util.MustParseUUID(fx.Insert(t, "channel_installation", testutil.Cols{
					"workspace_id": fx.WorkspaceID, "agent_id": agentID, "channel_type": "lark", "config": "{}", "installer_user_id": fx.UserID,
				}))
				q := db.New(pool)
				if _, err := q.CreateChannelChatSessionBinding(t.Context(), db.CreateChannelChatSessionBindingParams{
					ChatSessionID: sessionID, InstallationID: installationID, ChannelType: "lark", ChannelChatID: suffix, ChatType: "group", Config: []byte("{}"),
				}); err != nil {
					t.Fatal(err)
				}
				fx.Cleanup(t, "DELETE FROM channel_chat_session_binding WHERE chat_session_id=$1", sessionID)
				fx.Cleanup(t, "DELETE FROM channel_chat_context_generation WHERE chat_session_id=$1", sessionID)
				fx.Cleanup(t, "DELETE FROM chat_message WHERE chat_session_id=$1", sessionID)
				fx.Cleanup(t, "DELETE FROM attachment WHERE workspace_id=$1", fx.WorkspaceID)
				fx.Cleanup(t, "DELETE FROM channel_media_pending_object WHERE workspace_id=$1", fx.WorkspaceID)

				literal := "Literal [Image 1: unavailable] and [Image 2: unavailable]"
				fake := newEnricherFake()
				fake.userNames = map[string]string{"alice": "Alice", "bob": "Bob", "reporter": "Reporter"}
				fake.byChat[ChatID(suffix)] = []LarkMessage{textMsg("literal", "alice", literal, "1000")}
				command := "Inspect"
				if issue {
					command = "/issue Compare\nReporter details"
				}
				in := InboundMessage{MessageID: "trigger", MessageType: "text", Body: command, CommandBody: command,
					SenderOpenID: "reporter", ChatID: ChatID(suffix), ChatType: ChatTypeGroup, AddressedToBot: true, CreateTime: "2000"}
				media := LarkMessage{MessageID: "media", SenderID: "bob", SenderType: "user", CreateTime: "1500", MessageType: "post",
					Content: `{"content":[[{"tag":"text","text":"[Image 1: unavailable]"},{"tag":"img","image_key":"good"},{"tag":"text","text":"[Image 1: unavailable]"},{"tag":"img","image_key":"good"}]]}`}
				in.ParentID = media.MessageID
				if source == "partial-failure" {
					media.Content = strings.Replace(media.Content, `"image_key":"good"`, `"image_key":"bad"`, 1)
				}
				fake.byID[in.ParentID] = []LarkMessage{media}
				enricher := NewInboundEnricher(fake, InboundEnricherConfig{RecentContextSize: 5})
				out := enricher.Enrich(t.Context(), in, InstallationCredentials{})
				session := engine.NewChatSession(q, pool, channel.TypeFeishu, engine.SessionTitles{})
				sender, workspaceID := util.MustParseUUID(fx.UserID), util.MustParseUUID(fx.WorkspaceID)
				appended, err := session.AppendUserMessage(t.Context(), engine.AppendInput{
					SessionID: sessionID, Sender: sender, Body: out.Body, CommandText: command, MediaPendingSeconds: 60,
				})
				if err != nil {
					t.Fatal(err)
				}
				api := &fakeSender{downloaded: DownloadedResource{Data: []byte("source image"), Filename: "source.png", ContentType: "image/png"},
					downloadErrByKey: map[string]error{"bad": errors.New("denied")}}
				inst := engine.ResolvedInstallation{ID: installationID, WorkspaceID: workspaceID,
					Platform: Installation{AppID: "cli_test", Region: "feishu"}}
				resolver := NewFeishuMediaResolver(api, fakeCreds{secret: "plain"}, &fakeMediaStorage{}, engine.NewDBMediaIntentLedger(q), newDiscardLogger())
				resolved := resolver.ResolveMedia(t.Context(), inst, engine.ResolvedIdentity{}, sessionID, appended.MessageID, channelMessageFromLark(out))
				if len(resolved.MediaRefs) != 1 {
					t.Fatalf("expected one deduplicated successful attachment: %+v", resolved.MediaRefs)
				}
				if err := session.BindMediaRefs(t.Context(), engine.BindMediaInput{
					MessageID: appended.MessageID, SessionID: sessionID, WorkspaceID: workspaceID, Sender: sender,
					Body: resolved.Text, MediaRefs: resolved.MediaRefs, IssueID: issueID,
					IssueDescriptionBase: pgtype.Text{String: "Reporter details", Valid: issue}, IssueCommandText: command,
				}); err != nil {
					t.Fatal(err)
				}
				var id, content string
				if err := pool.QueryRow(t.Context(), "SELECT id::text FROM attachment WHERE workspace_id=$1", workspaceID).Scan(&id); err != nil {
					t.Fatal(err)
				}
				if issue {
					err = pool.QueryRow(t.Context(), "SELECT description FROM issue WHERE id=$1", issueID).Scan(&content)
				} else {
					err = pool.QueryRow(t.Context(), "SELECT content FROM chat_message WHERE id=$1", appended.MessageID).Scan(&content)
				}
				if err != nil {
					t.Fatal(err)
				}
				ref := resolved.MediaRefs[0]
				bound := strings.TrimSuffix(ref.InlinePlaceholder, "unavailable]") + id + "]"
				if (!issue && !strings.Contains(content, "[Alice]: "+literal)) || strings.Count(content, bound) != ref.InlineCount || strings.Contains(content, ref.InlinePlaceholder) {
					t.Fatalf("attachment replaced original text or lost its source position: %s", content)
				}
				if issue && (!strings.Contains(content, "Referenced channel context:") || !strings.Contains(content, "Reporter details")) {
					t.Fatalf("issue lost source context or reporter text: %s", content)
				}
				if !strings.Contains(content, `sender="Bob"`) || !strings.Contains(content, "[Image 1: unavailable] "+bound) {
					t.Fatalf("rich-text literal or source attribution changed: %s", content)
				}
				resolvedNative, err := larkMsgFromRaw(resolved)
				if err != nil {
					t.Fatal(err)
				}
				if source == "partial-failure" && !strings.Contains(content, resolvedNative.ContextMedia[0].Placeholder) {
					t.Fatalf("failed media lost its explicit unavailable position: %s", content)
				}
			})
		}
	}
}
