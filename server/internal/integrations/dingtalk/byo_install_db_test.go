package dingtalk

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func dingtalkInstallTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("no database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
	}
	var migrated bool
	if err := pool.QueryRow(ctx, `
SELECT to_regclass('public.dingtalk_connector') IS NOT NULL
   AND to_regclass('public.dingtalk_workspace_grant') IS NOT NULL
`).Scan(&migrated); err != nil || !migrated {
		pool.Close()
		t.Skip("DingTalk connector tables not present (database not migrated)")
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestRegisterBYO_SharesConnectorAcrossWorkspaceGrantsDB(t *testing.T) {
	pool := dingtalkInstallTestDB(t)
	ctx := context.Background()
	const (
		workspaceA = "d1470000-0000-4000-8000-000000000001"
		workspaceB = "d1470000-0000-4000-8000-000000000002"
		agentA     = "d1470000-0000-4000-8000-000000000003"
		agentB     = "d1470000-0000-4000-8000-000000000004"
		installer  = "d1470000-0000-4000-8000-000000000005"
		runtimeA   = "d1470000-0000-4000-8000-000000000006"
		runtimeB   = "d1470000-0000-4000-8000-000000000007"
		appKey     = "dingtalk_cross_workspace_connector_db"
	)

	clean := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_inbound_audit WHERE installation_id IN (SELECT id FROM dingtalk_connector WHERE app_id = $1)`, appKey)
		_, _ = pool.Exec(context.Background(), `
DELETE FROM dingtalk_workspace_grant
WHERE connector_id IN (SELECT id FROM dingtalk_connector WHERE app_id = $1)
`, appKey)
		_, _ = pool.Exec(context.Background(), `DELETE FROM dingtalk_connector WHERE app_id = $1`, appKey)
		_, _ = pool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id IN ($1, $2)`, workspaceA, workspaceB)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id IN ($1, $2)`, workspaceA, workspaceB)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, installer)
	}
	clean()
	t.Cleanup(clean)
	if _, err := pool.Exec(ctx, `INSERT INTO "user" (id, name, email) VALUES ($1, 'DingTalk shared connector owner', 'dingtalk-shared-connector@multica.test')`, installer); err != nil {
		t.Fatalf("seed shared connector user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO workspace (id, name, slug, description) VALUES ($1, 'DingTalk install A', 'dingtalk-install-a', ''), ($2, 'DingTalk install B', 'dingtalk-install-b', '')`, workspaceA, workspaceB); err != nil {
		t.Fatalf("seed shared connector workspaces: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $3, 'owner'), ($2, $3, 'owner')`, workspaceA, workspaceB, installer); err != nil {
		t.Fatalf("seed shared connector memberships: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider) VALUES ($1, $2, 'DingTalk install runtime A', 'local', 'multica_daemon'), ($3, $4, 'DingTalk install runtime B', 'local', 'multica_daemon')`, runtimeA, workspaceA, runtimeB, workspaceB); err != nil {
		t.Fatalf("seed shared connector runtimes: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id) VALUES ($1, $2, 'DingTalk install agent A', 'local', $3), ($4, $5, 'DingTalk install agent B', 'local', $6)`, agentA, workspaceA, runtimeA, agentB, workspaceB, runtimeB); err != nil {
		t.Fatalf("seed shared connector agents: %v", err)
	}

	srv := dingtalkMockServer(t, true)
	defer srv.Close()
	svc, err := NewInstallService(db.New(pool), pool, testBox(t), nil)
	if err != nil {
		t.Fatalf("NewInstallService: %v", err)
	}
	svc.apiBase = srv.URL

	register := func(workspaceID, agentID string) db.ChannelInstallation {
		t.Helper()
		row, registerErr := svc.RegisterBYO(ctx, RegisterBYOParams{
			WorkspaceID: util.MustParseUUID(workspaceID),
			AgentID:     util.MustParseUUID(agentID),
			InitiatorID: util.MustParseUUID(installer),
			AppKey:      appKey,
			AppSecret:   "shared-secret",
		})
		if registerErr != nil {
			t.Fatalf("RegisterBYO(%s): %v", workspaceID, registerErr)
		}
		return row
	}

	first := register(workspaceA, agentA)
	second := register(workspaceB, agentB)
	if first.ID != second.ID {
		t.Fatalf("connector ids differ: %s vs %s", util.UUIDToString(first.ID), util.UUIDToString(second.ID))
	}

	var connectorCount, activeGrantCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM dingtalk_connector WHERE app_id = $1`, appKey).Scan(&connectorCount); err != nil {
		t.Fatalf("count connectors: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM dingtalk_workspace_grant
WHERE connector_id = $1 AND status = 'active'
`, first.ID).Scan(&activeGrantCount); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if connectorCount != 1 || activeGrantCount != 2 {
		t.Fatalf("connector/grant counts = %d/%d, want 1/2", connectorCount, activeGrantCount)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channel_inbound_audit (installation_id, channel_type, event_type, channel_event_id, drop_reason) VALUES ($1, 'dingtalk', 'message', 'dingtalk-unscoped-last-revoke', 'workspace_required')`, first.ID); err != nil {
		t.Fatalf("seed unscoped connector audit: %v", err)
	}

	if err := svc.Revoke(ctx, first.ID, util.MustParseUUID(workspaceA)); err != nil {
		t.Fatalf("revoke first workspace: %v", err)
	}
	var connectorStatus, secondGrantStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM dingtalk_connector WHERE id = $1`, first.ID).Scan(&connectorStatus); err != nil {
		t.Fatalf("read connector after first revoke: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT status FROM dingtalk_workspace_grant
WHERE connector_id = $1 AND workspace_id = $2
`, first.ID, workspaceB).Scan(&secondGrantStatus); err != nil {
		t.Fatalf("read second grant: %v", err)
	}
	if connectorStatus != "active" || secondGrantStatus != "active" {
		t.Fatalf("first revoke stopped shared connector: connector=%s second_grant=%s", connectorStatus, secondGrantStatus)
	}

	if err := svc.Revoke(ctx, first.ID, util.MustParseUUID(workspaceB)); err != nil {
		t.Fatalf("revoke final workspace: %v", err)
	}
	var secretRetained bool
	if err := pool.QueryRow(ctx, `SELECT status, config ? 'app_secret_encrypted' FROM dingtalk_connector WHERE id = $1`, first.ID).Scan(&connectorStatus, &secretRetained); err != nil {
		t.Fatalf("read connector after final revoke: %v", err)
	}
	if connectorStatus != "revoked" || secretRetained {
		t.Fatalf("final revoke connector status/secret = %s/%t, want revoked/false", connectorStatus, secretRetained)
	}
	var unscopedAuditCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM channel_inbound_audit WHERE installation_id = $1 AND workspace_id IS NULL`, first.ID).Scan(&unscopedAuditCount); err != nil {
		t.Fatalf("count unscoped connector audit after final revoke: %v", err)
	}
	if unscopedAuditCount != 0 {
		t.Fatalf("unscoped connector audit rows after final revoke = %d, want 0", unscopedAuditCount)
	}
}
