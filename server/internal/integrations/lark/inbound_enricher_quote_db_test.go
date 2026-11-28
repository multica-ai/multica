package lark

import (
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestEnrichTopicRootQuotedOncePerContextDB drives a topic conversation
// through the real session store: the topic's first message is inlined on
// the first turn only, an /issue command does not count as showing it, and
// a /clear starts a context that needs it inlined again.
func TestEnrichTopicRootQuotedOncePerContextDB(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("explicit disposable DATABASE_URL required")
	}
	pool := channelScopeTestDB(t)
	fx := testutil.New(pool, "", "")
	suffix := uuid.NewString()
	fx.UserID = fx.User(t, "Reporter", "quote-"+suffix+"@example.invalid")
	fx.WorkspaceID = fx.Workspace(t, "Quote once", "quote-"+suffix)
	fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
	agentID := fx.Agent(t, "Quote agent", "")
	sessionID := util.MustParseUUID(fx.ChatSession(t, agentID))
	installationID := util.MustParseUUID(fx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id": fx.WorkspaceID, "agent_id": agentID, "channel_type": "lark", "config": "{}", "installer_user_id": fx.UserID,
	}))
	chatID := "oc_" + suffix
	q := db.New(pool)
	if _, err := q.CreateChannelChatSessionBinding(t.Context(), db.CreateChannelChatSessionBindingParams{
		ChatSessionID: sessionID, InstallationID: installationID, ChannelType: "lark",
		ChannelChatID: larkRouteKey(chatID, ChatTypeGroup, "omt_1"), ChatType: "group", Config: []byte("{}"),
	}); err != nil {
		t.Fatal(err)
	}
	fx.Cleanup(t, "DELETE FROM channel_chat_session_binding WHERE chat_session_id=$1", sessionID)
	fx.Cleanup(t, "DELETE FROM channel_chat_context_generation WHERE chat_session_id=$1", sessionID)
	fx.Cleanup(t, "DELETE FROM chat_message WHERE chat_session_id=$1", sessionID)

	session := engine.NewChatSession(q, pool, channel.TypeFeishu, engine.SessionTitles{})
	sender := util.MustParseUUID(fx.UserID)
	enricher := NewInboundEnricher(quoteFake(), InboundEnricherConfig{Quotes: NewChannelStore(q)})
	turn := func(body string) string {
		t.Helper()
		in := topicReply(body)
		in.InstallationID, in.ChatID = installationID, ChatID(chatID)
		out := enricher.Enrich(t.Context(), in, InstallationCredentials{})
		if _, err := session.AppendUserMessage(t.Context(), engine.AppendInput{
			SessionID: sessionID, Sender: sender, Body: out.Body, CommandText: out.CommandBody,
		}); err != nil {
			t.Fatal(err)
		}
		return out.Body
	}
	full := func(body string) bool { return strings.Contains(body, quotedFullMarker("om_root")) }

	if body := turn("/issue Track the report"); !full(body) {
		t.Fatalf("first turn must inline the quote: %s", body)
	}
	if body := turn("what happened?"); !full(body) {
		t.Fatalf("an /issue command is not agent input, so the quote must be inlined again: %s", body)
	}
	if body := turn("and then?"); full(body) || !strings.Contains(body, `type="shown_earlier"`) {
		t.Fatalf("a quote already in this context must not be repeated: %s", body)
	}
	if err := session.MarkPendingFresh(t.Context(), sessionID, "om_clear"); err != nil {
		t.Fatal(err)
	}
	if body := turn("start over"); !full(body) {
		t.Fatalf("after /clear the new context must get the quote again: %s", body)
	}
}
