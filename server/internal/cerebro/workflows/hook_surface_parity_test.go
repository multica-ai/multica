package workflows

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

func TestWorkflowHookCapabilitySurfaceMatchesCallTimeAuthorizer(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	fixture := setupWorkflowIntegrationFixture(t, pool)
	var agentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, status)
		VALUES ($1, 'Hook parity agent', 'local', 'idle')
		RETURNING id
	`, fixture.workspaceID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	store := toolpolicy.NewStore(pool)
	authorizer := NewToolPolicyHookAuthorizer(store)
	agent := HookPermissionActor{Type: "agent", ID: util.UUIDToString(agentID)}

	assertParity := func(permission HookPermission, want toolpolicy.Setting) {
		t.Helper()
		rows, err := store.Table(ctx, toolpolicy.TableQuery{
			WorkspaceID:     fixture.workspaceID,
			AgentID:         agentID,
			IncludePlatform: true,
		})
		if err != nil {
			t.Fatalf("capability table: %v", err)
		}
		var got toolpolicy.Setting
		for _, row := range rows {
			if row.ToolKey == string(permission) {
				got = row.Effective.Setting
				break
			}
		}
		if got != want {
			t.Errorf("%s capability surface = %q, want %q", permission, got, want)
		}
		allowed := authorizer.Can(ctx, util.UUIDToString(fixture.workspaceID), agent, permission)
		if allowed != (want == toolpolicy.SettingAllow) {
			t.Errorf("%s call-time allowed = %v, capability surface = %q", permission, allowed, got)
		}
	}

	assertParity(HookPermissionRead, toolpolicy.SettingAllow)
	assertParity(HookPermissionWrite, toolpolicy.SettingDeny)
	assertParity(HookPermissionEnforce, toolpolicy.SettingDeny)
	assertParity(HookPermissionManageManaged, toolpolicy.SettingDeny)

	for _, permission := range []HookPermission{HookPermissionWrite, HookPermissionEnforce, HookPermissionManageManaged} {
		if _, err := store.Set(ctx, toolpolicy.SetParams{
			WorkspaceID: fixture.workspaceID,
			ToolKey:     string(permission),
			Layer:       toolpolicy.LayerAgent,
			SubjectID:   agentID,
			Setting:     toolpolicy.SettingAllow,
			UpdatedBy:   fixture.userID,
		}); err != nil {
			t.Fatalf("grant %s: %v", permission, err)
		}
	}

	assertParity(HookPermissionWrite, toolpolicy.SettingAllow)
	assertParity(HookPermissionEnforce, toolpolicy.SettingDeny)
	assertParity(HookPermissionManageManaged, toolpolicy.SettingDeny)

	owner := HookPermissionActor{Type: "member", ID: util.UUIDToString(fixture.userID), IsOwner: true}
	if !authorizer.Can(ctx, util.UUIDToString(fixture.workspaceID), owner, HookPermissionManageManaged) {
		t.Fatal("verified workspace owner must pass the owner-only call-time floor")
	}
}
