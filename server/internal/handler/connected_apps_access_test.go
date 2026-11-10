package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/connectedapps"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestConnectedApps_GatewayRechecksAccess(t *testing.T) {
	for _, change := range []string{"departed", "private", "archived"} {
		t.Run(change, func(t *testing.T) {
			f := newAppsFixture(t)
			ctx := context.Background()
			f.executionAccount(t, "team")
			if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "team", "level": "write"}}); w.Code != http.StatusOK {
				t.Fatalf("attach: %d %s", w.Code, w.Body.String())
			}
			dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=$1, accountable_user_id=$1 WHERE id=$2`, f.member, f.taskID)
			if !f.h.CanActFor(ctx, parseUUID(testWorkspaceID), parseUUID(f.agentID), parseUUID(f.member)) {
				t.Fatal("member must initially be allowed to invoke the shared agent")
			}
			if code, response, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL"}); code != http.StatusOK || isErr {
				t.Fatalf("authorized call: %d %s", code, response)
			}
			switch change {
			case "departed":
				member, err := f.h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: parseUUID(f.member), WorkspaceID: parseUUID(testWorkspaceID)})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.h.revokeAndRemoveMember(ctx, member.WorkspaceID, member.UserID, member.ID, parseUUID(testUserID)); err != nil {
					t.Fatal(err)
				}
			case "private":
				dbfx.Exec(t, `UPDATE agent SET permission_mode='private' WHERE id=$1`, f.agentID)
			case "archived":
				dbfx.Exec(t, `UPDATE agent SET archived_at=now() WHERE id=$1`, f.agentID)
			}
			var status string
			dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, f.taskID).Scan(&status)
			if status != "running" && status != "dispatched" {
				t.Fatalf("task must still be active to exercise gateway revocation, got %s", status)
			}
			if code, response, _ := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL"}); code != http.StatusUnauthorized {
				t.Fatalf("revoked call: %d %s", code, response)
			}
			if n := len(f.sdk.executions()); n != 1 {
				t.Fatalf("executions = %d, want only the initial authorized send", n)
			}
		})
	}
}

func TestConnectedApps_SystemRunRechecksInvocationPermission(t *testing.T) {
	for _, accountable := range []string{"none", "owner", "departed_member"} {
		for _, revoke := range []string{"private", "member_only", "no_workspace_grant"} {
			t.Run(accountable+"/"+revoke, func(t *testing.T) {
				f := newAppsFixture(t)
				ctx := context.Background()
				f.executionAccount(t, "team")
				if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "team", "level": "write"}}); w.Code != http.StatusOK {
					t.Fatalf("attach: %d %s", w.Code, w.Body.String())
				}
				var accountableID any
				if accountable == "owner" {
					accountableID = testUserID
				} else if accountable == "departed_member" {
					accountableID = f.member
				}
				dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=NULL, accountable_user_id=$1 WHERE id=$2`, accountableID, f.taskID)
				if accountable == "departed_member" {
					member, err := f.h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: parseUUID(f.member), WorkspaceID: parseUUID(testWorkspaceID)})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := f.h.revokeAndRemoveMember(ctx, member.WorkspaceID, member.UserID, member.ID, parseUUID(testUserID)); err != nil {
						t.Fatal(err)
					}
				}
				agent, err := f.h.Queries.GetAgent(ctx, parseUUID(f.agentID))
				if err != nil {
					t.Fatal(err)
				}
				if !f.h.canInvokeAgent(ctx, agent, "system", "", "", testWorkspaceID) {
					t.Fatal("workspace-wide agent must initially admit a system run")
				}
				if code, response, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_FETCH_EMAILS"}); code != http.StatusOK || isErr {
					t.Fatalf("authorized system call: %d %s", code, response)
				}
				switch revoke {
				case "private":
					dbfx.Exec(t, `UPDATE agent SET permission_mode='private' WHERE id=$1`, f.agentID)
				case "member_only":
					dbfx.Exec(t, `UPDATE agent_invocation_target SET target_type='member', target_id=$1 WHERE agent_id=$2`, testUserID, f.agentID)
				case "no_workspace_grant":
					dbfx.Exec(t, `DELETE FROM agent_invocation_target WHERE agent_id=$1`, f.agentID)
				}
				if code, response, _ := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL"}); code != http.StatusUnauthorized {
					t.Fatalf("revoked system call: %d %s", code, response)
				}
				if n := len(f.sdk.executions()); n != 1 {
					t.Fatalf("executions = %d, want only the initial authorized read", n)
				}
			})
		}
	}
}

func TestConnectedApps_SystemAccessRevokedDuringToolLookup(t *testing.T) {
	for _, accountable := range []string{"none", "owner"} {
		t.Run(accountable, func(t *testing.T) {
			f := newAppsFixture(t)
			f.executionAccount(t, "team")
			if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "team", "level": "write"}}); w.Code != http.StatusOK {
				t.Fatalf("attach: %d %s", w.Code, w.Body.String())
			}
			var accountableID any
			if accountable == "owner" {
				accountableID = testUserID
			}
			dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=NULL, accountable_user_id=$1 WHERE id=$2`, accountableID, f.taskID)
			f.sdk.beforeListTools = func() {
				dbfx.Exec(t, `UPDATE agent SET permission_mode='private' WHERE id=$1`, f.agentID)
			}
			if _, text, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL"}); !isErr {
				t.Fatalf("system access revoked during lookup: %s", text)
			}
			if n := len(f.sdk.executions()); n != 0 {
				t.Fatalf("executed %d system calls after revocation during lookup", n)
			}
		})
	}
}

func TestConnectedApps_GatewayRequiresInvocationAuthorizer(t *testing.T) {
	for _, principal := range []string{"human", "system"} {
		t.Run(principal, func(t *testing.T) {
			f := newAppsFixture(t)
			f.executionAccount(t, "team")
			if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "team", "level": "write"}}); w.Code != http.StatusOK {
				t.Fatalf("attach: %d %s", w.Code, w.Body.String())
			}
			if principal == "system" {
				dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=NULL WHERE id=$1`, f.taskID)
			}
			f.h.ConnectedApps.SetResumer(nil)
			if code, response, _ := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL"}); code != http.StatusUnauthorized {
				t.Fatalf("call without invocation authorizer: %d %s", code, response)
			}
			if n := len(f.sdk.executions()); n != 0 {
				t.Fatalf("executed %d calls without an invocation authorizer", n)
			}
		})
	}
}

func TestConnectedApps_AccessRevokedDuringToolLookup(t *testing.T) {
	f := newAppsFixture(t)
	f.executionAccount(t, "team")
	if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "team", "level": "write"}}); w.Code != http.StatusOK {
		t.Fatalf("attach: %d %s", w.Code, w.Body.String())
	}
	f.sdk.beforeListTools = func() {
		dbfx.Exec(t, `UPDATE agent SET archived_at=now() WHERE id=$1`, f.agentID)
	}
	if _, text, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL"}); !isErr {
		t.Fatalf("revoked during lookup: %s", text)
	}
	if n := len(f.sdk.executions()); n != 0 {
		t.Fatalf("executed %d calls after revocation during lookup", n)
	}
}

func TestConnectedApps_RestoreSavedAccess(t *testing.T) {
	f := newAppsFixture(t)
	f.executionAccount(t, "team")
	ctx := context.Background()
	for _, owner := range []pgtype.UUID{{}, parseUUID(testUserID), parseUUID(f.member)} {
		if _, err := f.h.Queries.UpsertAppActionRule(ctx, db.UpsertAppActionRuleParams{
			WorkspaceID: parseUUID(testWorkspaceID), OwnerUserID: owner, ToolkitSlug: "gmail",
			ActionClass: "send", Policy: "block", UpdatedBy: parseUUID(testUserID),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "write"}}); w.Code != http.StatusOK {
		t.Fatalf("attach: %d %s", w.Code, w.Body.String())
	}
	var before AppsOverviewResponse
	testutil.Call(t, f.h.GetAppsOverview, appsReqAs(t, testUserID, http.MethodGet, "/api/apps", nil)).Want(http.StatusOK).JSON(&before)
	if len(before.Team) != 1 || len(before.Personal) != 1 || !before.Team[0].ActionsBlocked || !before.Personal[0].ActionsBlocked {
		t.Fatalf("saved restrictions are not visible: %+v", before)
	}
	if _, text, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL"}); !isErr || !strings.HasPrefix(text, "blocked") {
		t.Fatalf("saved rule must apply until restored: %s", text)
	}
	rules := []map[string]string{}
	for _, class := range connectedapps.Classes {
		rules = append(rules, map[string]string{"action_class": class, "policy": "allow"})
	}
	previous := f.h.TxStarter
	f.h.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
	testutil.Call(t, f.h.UpdateAppRules, appsReqAs(t, testUserID, http.MethodPut, "/api/apps/rules?toolkit_slug=gmail&scope=personal", map[string]any{"rules": rules})).Want(http.StatusInternalServerError)
	f.h.TxStarter = previous
	if n := dbfx.Count(t, `SELECT count(*) FROM app_action_rule WHERE workspace_id=$1 AND owner_user_id=$2 AND policy='block'`, testWorkspaceID, testUserID); n != 1 {
		t.Fatal("failed restore partially changed the account's rules")
	}
	// Restoring a personal account changes only the caller's rules.
	testutil.Call(t, f.h.UpdateAppRules, appsReqAs(t, testUserID, http.MethodPut, "/api/apps/rules?toolkit_slug=gmail&scope=personal", map[string]any{"rules": rules})).Want(http.StatusOK)
	var after AppsOverviewResponse
	testutil.Call(t, f.h.GetAppsOverview, appsReqAs(t, testUserID, http.MethodGet, "/api/apps", nil)).Want(http.StatusOK).JSON(&after)
	if after.Personal[0].ActionsBlocked || !after.Team[0].ActionsBlocked {
		t.Fatalf("restore crossed account scopes: %+v", after)
	}
	if _, text, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL"}); isErr {
		t.Fatalf("restored account could not send: %s", text)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM app_action_rule WHERE workspace_id=$1 AND owner_user_id=$2 AND policy='block'`, testWorkspaceID, f.member); n != 1 {
		t.Fatal("another member's restriction was changed")
	}
	testutil.Call(t, f.h.UpdateAppRules, appsReqAs(t, f.member, http.MethodPut, "/api/apps/rules?toolkit_slug=gmail&scope=team", map[string]any{"rules": rules})).Want(http.StatusForbidden)
	testutil.Call(t, f.h.UpdateAppRules, appsReqAs(t, testUserID, http.MethodPut, "/api/apps/rules?toolkit_slug=gmail&scope=team", map[string]any{"rules": rules})).Want(http.StatusOK)

	// Even an empty saved catalog can be recovered without reconnecting.
	testutil.Call(t, f.h.UpdateAppSettings, appsReqAs(t, testUserID, http.MethodPut, "/api/apps/settings", map[string]any{"catalog_mode": "custom", "allowed_toolkits": []string{}})).Want(http.StatusOK)
	testutil.Call(t, f.h.GetAppsOverview, appsReqAs(t, testUserID, http.MethodGet, "/api/apps", nil)).Want(http.StatusOK).JSON(&after)
	if after.CatalogMode != "custom" {
		t.Fatal("saved empty catalog must remain visible until restored")
	}
	testutil.Call(t, f.h.UpdateAppSettings, appsReqAs(t, f.member, http.MethodPut, "/api/apps/settings", map[string]any{"catalog_mode": "all"})).Want(http.StatusForbidden)
	testutil.Call(t, f.h.UpdateAppSettings, appsReqAs(t, testUserID, http.MethodPut, "/api/apps/settings", map[string]any{"catalog_mode": "all"})).Want(http.StatusOK)
	testutil.Call(t, f.h.GetAppsOverview, appsReqAs(t, testUserID, http.MethodGet, "/api/apps", nil)).Want(http.StatusOK).JSON(&after)
	if after.CatalogMode != "all" || after.Team[0].ActionsBlocked || len(after.AgentApps) != 1 {
		t.Fatalf("restore did not preserve the connected apps: %+v", after)
	}
}
