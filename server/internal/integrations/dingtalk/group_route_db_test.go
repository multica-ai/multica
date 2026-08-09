package dingtalk

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDingTalkGroupRoute_DiscoverReassignAndFenceStaleSessionDB(t *testing.T) {
	pool := dingtalkInstallTestDB(t)
	ctx := context.Background()
	var migrated bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.dingtalk_group_route') IS NOT NULL`).Scan(&migrated); err != nil || !migrated {
		t.Skip("dingtalk group route table not present (database not migrated)")
	}

	const (
		workspaceID   = "d2480000-0000-4000-8000-000000000001"
		runtimeID     = "d2480000-0000-4000-8000-000000000002"
		defaultAgent  = "d2480000-0000-4000-8000-000000000003"
		routedAgent   = "d2480000-0000-4000-8000-000000000004"
		installerID   = "d2480000-0000-4000-8000-000000000005"
		installation  = "d2480000-0000-4000-8000-000000000006"
		chatSessionID = "d2480000-0000-4000-8000-000000000007"
		conversation  = "cid-dingtalk-multi-agent-group"
		appKey        = "dingtalk_multi_agent_group_app"
	)

	clean := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_outbound_card_message WHERE chat_session_id = $1`, chatSessionID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, chatSessionID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM dingtalk_group_route WHERE installation_id = $1`, installation)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, installation)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	}
	clean()
	t.Cleanup(clean)

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seed group route fixture: %v", err)
		}
	}
	exec(`INSERT INTO workspace (id, name, slug, description) VALUES ($1, 'DingTalk routes', 'dingtalk-routes-db', '')`, workspaceID)
	exec(`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider) VALUES ($1, $2, 'DingTalk route runtime', 'local', 'multica_daemon')`, runtimeID, workspaceID)
	for _, agent := range []string{defaultAgent, routedAgent} {
		exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id) VALUES ($1, $2, $3, 'local', $4)`, agent, workspaceID, "DingTalk route "+agent, runtimeID)
	}
	exec(`INSERT INTO channel_installation (id, workspace_id, agent_id, channel_type, config, installer_user_id) VALUES ($1, $2, $3, 'dingtalk', jsonb_build_object('app_id', $4::text), $5)`, installation, workspaceID, defaultAgent, appKey, installerID)

	queries := db.New(pool)
	resolved, err := queries.ResolveDingTalkInstallationForInboundGroup(ctx, db.ResolveDingTalkInstallationForInboundGroupParams{
		AppID: appKey, ConversationID: conversation, ConversationTitle: "Platform team",
	})
	if err != nil {
		t.Fatalf("discover group: %v", err)
	}
	if resolved.RouteAgentID != util.MustParseUUID(defaultAgent) {
		t.Fatalf("new group agent = %s, want installation default", util.UUIDToString(resolved.RouteAgentID))
	}
	routes, err := queries.ListDingTalkGroupRoutesByWorkspace(ctx, util.MustParseUUID(workspaceID))
	if err != nil || len(routes) != 1 {
		t.Fatalf("list discovered routes = %d, err=%v", len(routes), err)
	}

	exec(`INSERT INTO channel_chat_session_binding (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type, config) VALUES ($1, $2, 'dingtalk', $3, 'group', jsonb_build_object('agent_id', $4::text))`, chatSessionID, installation, conversation, defaultAgent)
	exec(`INSERT INTO channel_outbound_card_message (chat_session_id, channel_type, channel_chat_id, channel_card_message_id) VALUES ($1, 'dingtalk', $2, 'old-agent-reply')`, chatSessionID, conversation)

	updated, err := queries.ReassignDingTalkGroupRoute(ctx, db.ReassignDingTalkGroupRouteParams{
		ID: routes[0].ID, WorkspaceID: util.MustParseUUID(workspaceID), AgentID: util.MustParseUUID(routedAgent),
	})
	if err != nil {
		t.Fatalf("reassign group: %v", err)
	}
	if updated.AgentID != util.MustParseUUID(routedAgent) {
		t.Fatalf("updated group agent = %s", util.UUIDToString(updated.AgentID))
	}
	if _, err := queries.GetChannelChatSessionBinding(ctx, db.GetChannelChatSessionBindingParams{
		InstallationID: util.MustParseUUID(installation), ChannelChatID: conversation,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("old group binding survived reassignment: %v", err)
	}

	// Simulate an old-agent inbound turn that resolved just before the update
	// and created its binding just after it. The next routed turn must retire it.
	exec(`INSERT INTO channel_chat_session_binding (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type, config) VALUES ($1, $2, 'dingtalk', $3, 'group', jsonb_build_object('agent_id', $4::text))`, chatSessionID, installation, conversation, defaultAgent)
	cleared, err := queries.DeleteDingTalkStaleGroupChatBinding(ctx, db.DeleteDingTalkStaleGroupChatBindingParams{
		InstallationID: util.MustParseUUID(installation), ConversationID: conversation, AgentID: util.MustParseUUID(routedAgent),
	})
	if err != nil {
		t.Fatalf("fence stale group binding: %v", err)
	}
	if cleared != 1 {
		t.Fatalf("cleared stale group bindings = %d, want 1", cleared)
	}
	if _, err := queries.GetChannelChatSessionBinding(ctx, db.GetChannelChatSessionBindingParams{
		InstallationID: util.MustParseUUID(installation), ChannelChatID: conversation,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("late old-agent binding survived guard: %v", err)
	}

	resolved, err = queries.ResolveDingTalkInstallationForInboundGroup(ctx, db.ResolveDingTalkInstallationForInboundGroupParams{
		AppID: appKey, ConversationID: conversation, ConversationTitle: "Platform team renamed",
	})
	if err != nil || resolved.RouteAgentID != util.MustParseUUID(routedAgent) {
		t.Fatalf("re-resolve group agent = %s, err=%v", util.UUIDToString(resolved.RouteAgentID), err)
	}
}
