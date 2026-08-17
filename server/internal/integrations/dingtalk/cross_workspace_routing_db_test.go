package dingtalk

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDingTalkCrossWorkspaceRoutingRequiresSelectionAndFencesSessionsDB(t *testing.T) {
	pool := dingtalkInstallTestDB(t)
	ctx := context.Background()
	const (
		workspaceA   = "d3470000-0000-4000-8000-000000000001"
		workspaceB   = "d3470000-0000-4000-8000-000000000002"
		runtimeA     = "d3470000-0000-4000-8000-000000000003"
		runtimeB     = "d3470000-0000-4000-8000-000000000004"
		agentA       = "d3470000-0000-4000-8000-000000000005"
		agentB       = "d3470000-0000-4000-8000-000000000006"
		memberUser   = "d3470000-0000-4000-8000-000000000007"
		connectorID  = "d3470000-0000-4000-8000-000000000008"
		sessionA     = "d3470000-0000-4000-8000-000000000009"
		appKey       = "dingtalk_cross_workspace_routing_db"
		staffID      = "staff-cross-workspace"
		directChatID = "cid-cross-workspace-direct"
		groupChatID  = "cid-cross-workspace-group"
	)

	clean := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_chat_session_binding WHERE installation_id = $1`, connectorID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionA)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_user_binding WHERE installation_id = $1`, connectorID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM dingtalk_direct_route WHERE connector_id = $1`, connectorID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM dingtalk_group_route WHERE installation_id = $1`, connectorID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM dingtalk_workspace_grant WHERE connector_id = $1`, connectorID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM dingtalk_connector WHERE id = $1`, connectorID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id IN ($1, $2)`, workspaceA, workspaceB)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id IN ($1, $2)`, workspaceA, workspaceB)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberUser)
	}
	clean()
	t.Cleanup(clean)

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seed cross-workspace routing fixture: %v", err)
		}
	}
	exec(`INSERT INTO "user" (id, name, email) VALUES ($1, 'DingTalk cross-workspace member', 'dingtalk-cross-workspace@multica.test')`, memberUser)
	exec(`INSERT INTO workspace (id, name, slug, description) VALUES ($1, 'DingTalk workspace A', 'dingtalk-routing-a', ''), ($2, 'DingTalk workspace B', 'dingtalk-routing-b', '')`, workspaceA, workspaceB)
	exec(`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $3, 'owner'), ($2, $3, 'admin')`, workspaceA, workspaceB, memberUser)
	exec(`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider) VALUES ($1, $2, 'DingTalk runtime A', 'local', 'multica_daemon'), ($3, $4, 'DingTalk runtime B', 'local', 'multica_daemon')`, runtimeA, workspaceA, runtimeB, workspaceB)
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id) VALUES ($1, $2, 'DingTalk agent A', 'local', $3), ($4, $5, 'DingTalk agent B', 'local', $6)`, agentA, workspaceA, runtimeA, agentB, workspaceB, runtimeB)
	exec(`INSERT INTO dingtalk_connector (id, app_id, config, installer_user_id) VALUES ($1, $2, jsonb_build_object('app_id', $2::text), $3)`, connectorID, appKey, memberUser)
	exec(`INSERT INTO dingtalk_workspace_grant (connector_id, workspace_id, default_agent_id, installer_user_id) VALUES ($1, $2, $3, $6), ($1, $4, $5, $6)`, connectorID, workspaceA, agentA, workspaceB, agentB, memberUser)
	exec(`INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id) VALUES ($1, $2, $3, 'dingtalk', $4)`, workspaceA, memberUser, connectorID, staffID)

	queries := db.New(pool)
	_, err := queries.DiscoverDingTalkGroupRoute(ctx, db.DiscoverDingTalkGroupRouteParams{
		InstallationID:    util.MustParseUUID(connectorID),
		MulticaUserID:     util.MustParseUUID(memberUser),
		ConversationID:    groupChatID,
		ConversationTitle: "Ambiguous group",
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("ambiguous group discovery error = %v, want no rows", err)
	}

	directA, err := queries.SelectDingTalkDirectWorkspaceRoute(ctx, db.SelectDingTalkDirectWorkspaceRouteParams{
		ConnectorID:   util.MustParseUUID(connectorID),
		MulticaUserID: util.MustParseUUID(memberUser),
		WorkspaceSlug: "dingtalk-routing-a",
		ChannelUserID: staffID,
		ChannelChatID: directChatID,
	})
	if err != nil || directA.WorkspaceID != util.MustParseUUID(workspaceA) || directA.AgentID != util.MustParseUUID(agentA) {
		t.Fatalf("select direct workspace A = %+v, err=%v", directA, err)
	}
	exec(`INSERT INTO chat_session (id, workspace_id, agent_id, creator_id, title) VALUES ($1, $2, $3, $4, 'DingTalk direct A')`, sessionA, workspaceA, agentA, memberUser)
	exec(`INSERT INTO channel_chat_session_binding (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type, config) VALUES ($1, $2, 'dingtalk', $3, 'p2p', jsonb_build_object('agent_id', $4::text))`, sessionA, connectorID, directChatID, agentA)

	directB, err := queries.SelectDingTalkDirectWorkspaceRoute(ctx, db.SelectDingTalkDirectWorkspaceRouteParams{
		ConnectorID:   util.MustParseUUID(connectorID),
		MulticaUserID: util.MustParseUUID(memberUser),
		WorkspaceSlug: "dingtalk-routing-b",
		ChannelUserID: staffID,
		ChannelChatID: directChatID,
	})
	if err != nil || directB.WorkspaceID != util.MustParseUUID(workspaceB) || directB.AgentID != util.MustParseUUID(agentB) || directB.Revision <= directA.Revision {
		t.Fatalf("select direct workspace B = %+v, err=%v", directB, err)
	}
	if _, err := queries.GetChannelChatSessionBinding(ctx, db.GetChannelChatSessionBindingParams{
		InstallationID: util.MustParseUUID(connectorID), ChannelChatID: directChatID,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("workspace A direct binding survived switch: %v", err)
	}

	groupA, err := queries.SelectDingTalkGroupWorkspaceRoute(ctx, db.SelectDingTalkGroupWorkspaceRouteParams{
		ConnectorID:       util.MustParseUUID(connectorID),
		MulticaUserID:     util.MustParseUUID(memberUser),
		WorkspaceSlug:     "dingtalk-routing-a",
		ConversationID:    groupChatID,
		ConversationTitle: "Cross-workspace group",
	})
	if err != nil || groupA.WorkspaceID != util.MustParseUUID(workspaceA) || groupA.AgentID != util.MustParseUUID(agentA) {
		t.Fatalf("select group workspace A = %+v, err=%v", groupA, err)
	}
	groupB, err := queries.SelectDingTalkGroupWorkspaceRoute(ctx, db.SelectDingTalkGroupWorkspaceRouteParams{
		ConnectorID:       util.MustParseUUID(connectorID),
		MulticaUserID:     util.MustParseUUID(memberUser),
		WorkspaceSlug:     "dingtalk-routing-b",
		ConversationID:    groupChatID,
		ConversationTitle: "Cross-workspace group",
	})
	if err != nil || groupB.WorkspaceID != util.MustParseUUID(workspaceB) || groupB.AgentID != util.MustParseUUID(agentB) || groupB.Revision <= groupA.Revision {
		t.Fatalf("select group workspace B = %+v, err=%v", groupB, err)
	}
}

func TestDeleteWorkspaceRehomesOnlyMemberDingTalkBindingsDB(t *testing.T) {
	pool := dingtalkInstallTestDB(t)
	ctx := context.Background()
	const (
		workspaceA = "d4470000-0000-4000-8000-000000000001"
		workspaceB = "d4470000-0000-4000-8000-000000000002"
		workspaceC = "d4470000-0000-4000-8000-000000000003"
		memberUser = "d4470000-0000-4000-8000-000000000004"
		connector1 = "d4470000-0000-4000-8000-000000000005"
		connector2 = "d4470000-0000-4000-8000-000000000006"
		agentA     = "d4470000-0000-4000-8000-000000000007"
		agentB     = "d4470000-0000-4000-8000-000000000008"
		agentC     = "d4470000-0000-4000-8000-000000000009"
	)

	clean := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_user_binding WHERE installation_id IN ($1, $2)`, connector1, connector2)
		_, _ = pool.Exec(context.Background(), `DELETE FROM dingtalk_workspace_grant WHERE connector_id IN ($1, $2)`, connector1, connector2)
		_, _ = pool.Exec(context.Background(), `DELETE FROM dingtalk_connector WHERE id IN ($1, $2)`, connector1, connector2)
		_, _ = pool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id IN ($1, $2, $3)`, workspaceA, workspaceB, workspaceC)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id IN ($1, $2, $3)`, workspaceA, workspaceB, workspaceC)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberUser)
	}
	clean()
	t.Cleanup(clean)

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seed workspace-delete binding fixture: %v", err)
		}
	}
	exec(`INSERT INTO "user" (id, name, email) VALUES ($1, 'DingTalk binding member', 'dingtalk-binding-member@multica.test')`, memberUser)
	exec(`INSERT INTO workspace (id, name, slug, description) VALUES ($1, 'Binding workspace A', 'dingtalk-binding-a', ''), ($2, 'Binding workspace B', 'dingtalk-binding-b', ''), ($3, 'Binding workspace C', 'dingtalk-binding-c', '')`, workspaceA, workspaceB, workspaceC)
	exec(`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $3, 'owner'), ($2, $3, 'member')`, workspaceA, workspaceB, memberUser)
	exec(`INSERT INTO dingtalk_connector (id, app_id, installer_user_id) VALUES ($1, 'dingtalk-binding-member-connector', $3), ($2, 'dingtalk-binding-nonmember-connector', $3)`, connector1, connector2, memberUser)
	exec(`INSERT INTO dingtalk_workspace_grant (connector_id, workspace_id, default_agent_id, installer_user_id) VALUES ($1, $3, $5, $7), ($1, $4, $6, $7), ($2, $3, $5, $7), ($2, $8, $9, $7)`, connector1, connector2, workspaceA, workspaceB, agentA, agentB, memberUser, workspaceC, agentC)
	exec(`INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id) VALUES ($1, $2, $3, 'dingtalk', 'staff-member'), ($1, $2, $4, 'dingtalk', 'staff-nonmember')`, workspaceA, memberUser, connector1, connector2)

	if err := db.New(pool).DeleteWorkspace(ctx, util.MustParseUUID(workspaceA)); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	var bindingWorkspace string
	if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM channel_user_binding WHERE installation_id = $1`, connector1).Scan(&bindingWorkspace); err != nil {
		t.Fatalf("read rehomed member binding: %v", err)
	}
	if bindingWorkspace != workspaceB {
		t.Fatalf("member binding workspace = %s, want %s", bindingWorkspace, workspaceB)
	}
	var nonMemberBindings int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM channel_user_binding WHERE installation_id = $1`, connector2).Scan(&nonMemberBindings); err != nil {
		t.Fatalf("count non-member binding: %v", err)
	}
	if nonMemberBindings != 0 {
		t.Fatalf("non-member binding survived workspace deletion: %d", nonMemberBindings)
	}
}
