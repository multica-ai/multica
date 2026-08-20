package dingtalk

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type dingtalkRouteCleanupOwner struct {
	workspaceID    string
	runtimeID      string
	agentID        string
	installationID string
	routeID        string
	appID          string
}

func seedDingTalkRouteCleanupOwner(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	kind string,
	status string,
) dingtalkRouteCleanupOwner {
	t.Helper()
	owner := dingtalkRouteCleanupOwner{
		workspaceID: uuid.NewString(),
		runtimeID:   uuid.NewString(),
	}
	slug := "dingtalk-route-cleanup-" + uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspace (id, name, slug, description)
		VALUES ($1, 'DingTalk route cleanup', $2, '')
	`, owner.workspaceID, slug); err != nil {
		t.Fatalf("seed cleanup workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider)
		VALUES ($1, $2, 'DingTalk cleanup runtime', 'local', 'multica_daemon')
	`, owner.runtimeID, owner.workspaceID); err != nil {
		t.Fatalf("seed cleanup runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM dingtalk_group_route WHERE workspace_id = $1`, owner.workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_installation WHERE workspace_id = $1`, owner.workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, owner.workspaceID)
	})
	return seedDingTalkRouteCleanupAgent(t, ctx, pool, owner, kind, status)
}

func seedDingTalkRouteCleanupAgent(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner dingtalkRouteCleanupOwner,
	kind string,
	status string,
) dingtalkRouteCleanupOwner {
	t.Helper()
	owner.agentID = uuid.NewString()
	owner.installationID = uuid.NewString()
	owner.routeID = uuid.NewString()
	owner.appID = "dingtalk-route-cleanup-" + uuid.NewString()
	systemKey := ""
	if kind == "system" {
		systemKey = "dingtalk_cleanup_" + uuid.NewString()
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent (
			id, workspace_id, name, runtime_mode, runtime_id, kind, system_key
		) VALUES ($1, $2, $6, 'local', $3, $4, NULLIF($5, ''))
	`, owner.agentID, owner.workspaceID, owner.runtimeID, kind, systemKey, "DingTalk cleanup "+owner.agentID); err != nil {
		t.Fatalf("seed cleanup agent: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO channel_installation (
			id, workspace_id, agent_id, channel_type, config, installer_user_id, status
		) VALUES (
			$1, $2, $3, 'dingtalk', jsonb_build_object('app_id', $4::text), $5, $6
		)
	`, owner.installationID, owner.workspaceID, owner.agentID, owner.appID, uuid.NewString(), status); err != nil {
		t.Fatalf("seed cleanup installation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO dingtalk_group_route (
			id, workspace_id, installation_id, conversation_id, conversation_title, agent_id
		) VALUES ($1, $2, $3, $4, 'DingTalk cleanup group', $5)
	`, owner.routeID, owner.workspaceID, owner.installationID, "cid-"+owner.routeID, owner.agentID); err != nil {
		t.Fatalf("seed cleanup group route: %v", err)
	}
	return owner
}

func assertDingTalkRouteCleanupState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner dingtalkRouteCleanupOwner,
	want int,
) {
	t.Helper()
	var routes, installations int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM dingtalk_group_route WHERE id = $1`, owner.routeID).Scan(&routes); err != nil {
		t.Fatalf("count cleanup routes: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM channel_installation WHERE id = $1`, owner.installationID).Scan(&installations); err != nil {
		t.Fatalf("count cleanup installations: %v", err)
	}
	if routes != want || installations != want {
		t.Fatalf(
			"cleanup owner route/installation rows = %d/%d, want %d/%d",
			routes,
			installations,
			want,
			want,
		)
	}
}

func assertDingTalkRouteCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner dingtalkRouteCleanupOwner,
	want int,
) {
	t.Helper()
	var routes int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM dingtalk_group_route WHERE id = $1`, owner.routeID).Scan(&routes); err != nil {
		t.Fatalf("count cleanup routes: %v", err)
	}
	if routes != want {
		t.Fatalf("cleanup owner route rows = %d, want %d", routes, want)
	}
}

func requireDingTalkRouteCleanupSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var present bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('public.dingtalk_group_route') IS NOT NULL`).Scan(&present); err != nil || !present {
		t.Skip("dingtalk_group_route not present (database not migrated)")
	}
}

func TestDeleteDingTalkInstallationForReplacementCleansOnlyTargetRoutes(t *testing.T) {
	pool := dingtalkInstallTestDB(t)
	requireDingTalkRouteCleanupSchema(t, pool)
	ctx := context.Background()
	target := seedDingTalkRouteCleanupOwner(t, ctx, pool, "user", "active")
	unrelated := seedDingTalkRouteCleanupOwner(t, ctx, pool, "user", "active")

	if _, err := db.New(pool).DeleteDingTalkInstallationForReplacement(ctx, db.DeleteDingTalkInstallationForReplacementParams{
		InstallationID: util.MustParseUUID(target.installationID),
		WorkspaceID:    util.MustParseUUID(target.workspaceID),
		AgentID:        util.MustParseUUID(target.agentID),
	}); err != nil {
		t.Fatalf("DeleteDingTalkInstallationForReplacement: %v", err)
	}

	assertDingTalkRouteCleanupState(t, ctx, pool, target, 0)
	assertDingTalkRouteCleanupState(t, ctx, pool, unrelated, 1)
}

func TestReclaimDeadChannelInstallationByAppIDCleansOnlyDeadRoutes(t *testing.T) {
	pool := dingtalkInstallTestDB(t)
	requireDingTalkRouteCleanupSchema(t, pool)
	ctx := context.Background()
	dead := seedDingTalkRouteCleanupOwner(t, ctx, pool, "user", "revoked")
	live := seedDingTalkRouteCleanupOwner(t, ctx, pool, "user", "active")

	if _, err := db.New(pool).ReclaimDeadChannelInstallationByAppID(ctx, db.ReclaimDeadChannelInstallationByAppIDParams{
		ChannelType: "dingtalk",
		AppID:       dead.appID,
		WorkspaceID: util.MustParseUUID(live.workspaceID),
		AgentID:     util.MustParseUUID(live.agentID),
	}); err != nil {
		t.Fatalf("ReclaimDeadChannelInstallationByAppID: %v", err)
	}

	assertDingTalkRouteCleanupState(t, ctx, pool, dead, 0)
	assertDingTalkRouteCleanupState(t, ctx, pool, live, 1)
}

func TestDeleteChannelInstallationsBySystemRuntimeAgentsCleansOnlySystemRoutes(t *testing.T) {
	pool := dingtalkInstallTestDB(t)
	requireDingTalkRouteCleanupSchema(t, pool)
	ctx := context.Background()
	systemOwner := seedDingTalkRouteCleanupOwner(t, ctx, pool, "system", "active")
	userOwner := seedDingTalkRouteCleanupAgent(t, ctx, pool, systemOwner, "user", "active")

	if err := db.New(pool).DeleteChannelInstallationsBySystemRuntimeAgents(
		ctx,
		util.MustParseUUID(systemOwner.runtimeID),
	); err != nil {
		t.Fatalf("DeleteChannelInstallationsBySystemRuntimeAgents: %v", err)
	}

	assertDingTalkRouteCleanupState(t, ctx, pool, systemOwner, 0)
	assertDingTalkRouteCleanupState(t, ctx, pool, userOwner, 1)
}

func TestDeleteWorkspaceCleansOnlyWorkspaceDingTalkRoutes(t *testing.T) {
	pool := dingtalkInstallTestDB(t)
	requireDingTalkRouteCleanupSchema(t, pool)
	ctx := context.Background()
	target := seedDingTalkRouteCleanupOwner(t, ctx, pool, "user", "active")
	unrelated := seedDingTalkRouteCleanupOwner(t, ctx, pool, "user", "active")

	if err := db.New(pool).DeleteWorkspace(ctx, util.MustParseUUID(target.workspaceID)); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	assertDingTalkRouteCleanupState(t, ctx, pool, target, 0)
	assertDingTalkRouteCleanupState(t, ctx, pool, unrelated, 1)
}

func TestDeleteWorkspaceLeafDataCleansOnlyWorkspaceDingTalkRoutes(t *testing.T) {
	pool := dingtalkInstallTestDB(t)
	requireDingTalkRouteCleanupSchema(t, pool)
	ctx := context.Background()
	target := seedDingTalkRouteCleanupOwner(t, ctx, pool, "user", "active")
	unrelated := seedDingTalkRouteCleanupOwner(t, ctx, pool, "user", "active")

	if err := db.New(pool).DeleteWorkspaceLeafData(ctx, util.MustParseUUID(target.workspaceID)); err != nil {
		t.Fatalf("DeleteWorkspaceLeafData: %v", err)
	}

	// This is the handler's first-stage route cleanup. Installations are
	// deliberately removed by later teardown steps, so only the target route is
	// expected to be gone at this point.
	assertDingTalkRouteCount(t, ctx, pool, target, 0)
	assertDingTalkRouteCleanupState(t, ctx, pool, unrelated, 1)
}
