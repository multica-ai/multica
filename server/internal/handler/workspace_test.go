package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCreateWorkspace_RejectsReservedSlug(t *testing.T) {
	// Drive the test off the actual reservedSlugs map so the test can never
	// drift from the source of truth. New entries are covered automatically.
	reserved := make([]string, 0, len(reservedSlugs))
	for slug := range reservedSlugs {
		reserved = append(reserved, slug)
	}
	sort.Strings(reserved) // deterministic test order

	for _, slug := range reserved {
		t.Run(slug, func(t *testing.T) {
			req := newRequest("POST", "/api/workspaces", map[string]any{
				"name": fmt.Sprintf("Test %s", slug),
				"slug": slug,
			})
			w := testutil.Call(t, testHandler.CreateWorkspace, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("slug %q: expected 400, got %d: %s", slug, w.Code, w.Body.String())
			}
		})
	}
}

// TestCreateWorkspace_DoesNotMarkOnboarded guards the onboarding
// contract: creating a workspace MUST leave user.onboarded_at NULL so
// the route guard in apps/web/app/[workspaceSlug]/layout.tsx (and the
// desktop App.tsx overlay decision) can redirect the un-onboarded user
// back to /onboarding to finish Step 3. The previous behavior atomically
// set onboarded_at inside CreateWorkspace; this test makes the new
// invariant explicit and regression-protected.
//
// CompleteOnboarding (Step 3 exit) and AcceptInvitation are the only
// remaining handlers that flip onboarded_at.
func TestCreateWorkspace_DoesNotMarkOnboarded(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	const slug = "handler-tests-onboarded-null"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	// Ensure the test user starts un-onboarded so the assertion is meaningful.
	_, _ = testPool.Exec(ctx, `UPDATE "user" SET onboarded_at = NULL WHERE id = $1`, testUserID)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE slug = $1`, slug)
		_, _ = testPool.Exec(context.Background(), `UPDATE "user" SET onboarded_at = NULL WHERE id = $1`, testUserID)
	})

	req := newRequest("POST", "/api/workspaces", map[string]any{
		"name": "Onboarding Invariant Probe",
		"slug": slug,
	})
	testutil.Call(t, testHandler.CreateWorkspace, req).Want(http.StatusCreated)

	var onboardedAt *string
	dbfx.QueryRow(t, `SELECT onboarded_at FROM "user" WHERE id = $1`, testUserID).Scan(&onboardedAt)
	if onboardedAt != nil {
		t.Fatalf("CreateWorkspace marked user as onboarded; expected NULL, got %q. The workspace layout hard gate relies on this staying NULL until Step 3 CompleteOnboarding fires.", *onboardedAt)
	}
}

// TestCreateWorkspace_DisabledByConfig guards the self-host gate added by
// #3433: when DisableWorkspaceCreation is true on the handler config, every
// caller — even an already-authenticated user — must receive 403 and the
// workspace row must not be written.
func TestCreateWorkspace_DisabledByConfig(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const slug = "handler-tests-disabled-create"
	ctx := context.Background()
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	dbfx.Cleanup(t, `DELETE FROM workspace WHERE slug = $1`, slug)

	prev := testHandler.cfg
	testHandler.cfg = Config{
		AllowSignup:              prev.AllowSignup,
		DisableWorkspaceCreation: true,
	}
	t.Cleanup(func() { testHandler.cfg = prev })

	req := newRequest("POST", "/api/workspaces", map[string]any{
		"name": "Disabled Create",
		"slug": slug,
	})
	testutil.Call(t, testHandler.CreateWorkspace, req).Want(http.StatusForbidden)

	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM workspace WHERE slug = $1`, slug).Scan(&count)
	if count != 0 {
		t.Fatalf("expected no workspace row to be written when gate fires, found %d", count)
	}
}

// TestDeleteWorkspace_RequiresOwner exercises the in-handler authorization
// added to DeleteWorkspace by calling the handler directly (bypassing the
// router-level RequireWorkspaceRoleFromURL middleware). Without the handler
// check, a non-owner member request would reach DeleteWorkspace and erase the
// workspace; with it, the handler must return 403 and leave the workspace
// intact.
func TestDeleteWorkspace_RequiresOwner(t *testing.T) {
	ctx := context.Background()

	const slug = "handler-tests-delete-403"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	wsID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":        "Handler Test Delete 403",
		"slug":        slug,
		"description": "DeleteWorkspace handler permission test",
	})

	dbfx.Exec(t, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'admin')
`, wsID, testUserID)

	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusForbidden)

	var exists bool
	dbfx.QueryRow(t, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists)
	if !exists {
		t.Fatal("workspace was deleted despite non-owner request — handler-level check did not fire")
	}
}

// TestDeleteWorkspace_OwnerSucceeds is the positive counterpart: an owner
// calling DeleteWorkspace directly must succeed (204) and the workspace must
// be gone. This guards the handler check against being too strict.
func TestDeleteWorkspace_OwnerSucceeds(t *testing.T) {
	ctx := context.Background()

	const slug = "handler-tests-delete-ok"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	wsID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":        "Handler Test Delete OK",
		"slug":        slug,
		"description": "DeleteWorkspace handler owner test",
	})

	dbfx.Exec(t, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, wsID, testUserID)
	dbfx.Exec(t, `
INSERT INTO github_pending_check_suite (
	workspace_id, installation_id, repo_owner, repo_name, pr_number,
	suite_id, head_sha, app_id, status, suite_updated_at
)
VALUES ($1, 123456789, 'multica-ai', 'multica', 3366, 987654321, 'abc123', 15368, 'completed', now())
`, wsID)
	githubPRID := dbfx.Insert(t, "github_pull_request", testutil.Cols{
		"workspace_id":    wsID,
		"installation_id": 123456789,
		"repo_owner":      "multica-ai",
		"repo_name":       "multica",
		"pr_number":       5265,
		"title":           "Workspace cleanup snapshot",
		"state":           "open",
		"html_url":        "https://github.com/multica-ai/multica/pull/5265",
		"pr_created_at":   testutil.Raw("now()"),
		"pr_updated_at":   testutil.Raw("now()"),
		"head_sha":        "head-a",
	})
	dbfx.Exec(t, `
INSERT INTO github_pull_request_check_run (
	pr_id, head_sha, ordinal, name, status, conclusion, is_status_context
)
VALUES ($1, 'head-a', 0, 'backend', 'completed', 'success', false)
`, githubPRID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM github_pull_request_check_run WHERE pr_id = $1`, githubPRID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id = $1`, githubPRID)
	})
	propertyID := dbfx.Insert(t, "issue_property", testutil.Cols{
		"workspace_id": wsID,
		"name":         "Delete cleanup property",
		"type":         "text",
	})

	runtimeID := dbfx.Insert(t, "agent_runtime", testutil.Cols{
		"workspace_id": wsID,
		"name":         "Workspace delete runtime",
		"runtime_mode": "cloud",
		"provider":     "delete-test",
		"status":       "offline",
		"device_info":  "",
		"metadata":     testutil.Raw("'{}'::jsonb"),
		"owner_id":     testUserID,
	})

	agentID := dbfx.Insert(t, "agent", testutil.Cols{
		"workspace_id":   wsID,
		"name":           "Workspace delete agent",
		"runtime_mode":   "cloud",
		"runtime_config": testutil.Raw("'{}'::jsonb"),
		"runtime_id":     runtimeID,
		"owner_id":       testUserID,
	})

	issueID := dbfx.Insert(t, "issue", testutil.Cols{
		"workspace_id": wsID,
		"title":        "Workspace delete issue",
		"creator_type": "member",
		"creator_id":   testUserID,
	})

	autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    wsID,
		"title":           "Workspace delete autopilot",
		"assignee_id":     agentID,
		"created_by_type": "member",
		"created_by_id":   testUserID,
	})

	autopilotRunID := dbfx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": autopilotID,
		"source":       "manual",
		"status":       "completed",
		"issue_id":     issueID,
	})

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":       runtimeID,
		"issue_id":         issueID,
		"status":           "completed",
		"autopilot_run_id": autopilotRunID,
	})
	dbfx.Exec(t, `
UPDATE autopilot_run SET task_id = $2 WHERE id = $1
`, autopilotRunID, taskID)
	dbfx.Exec(t, `
INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens)
VALUES ($1, 'delete-test', 'workspace-delete', 10, 5)
`, taskID)

	var rollupRuntimeID, rollupAgentID string
	dbfx.QueryRow(t, `SELECT gen_random_uuid(), gen_random_uuid()`).Scan(&rollupRuntimeID, &rollupAgentID)
	dbfx.Exec(t, `
INSERT INTO task_usage_hourly (
	bucket_hour, workspace_id, runtime_id, agent_id, provider, model,
	input_tokens, output_tokens, task_count, event_count
)
VALUES (date_trunc('hour', now()), $1, $2, $3, 'delete-test', 'workspace-rollup', 10, 5, 1, 1)
`, wsID, rollupRuntimeID, rollupAgentID)
	dbfx.Exec(t, `
INSERT INTO task_usage_hourly_dirty (
	bucket_hour, workspace_id, runtime_id, agent_id, provider, model
)
VALUES (date_trunc('hour', now()), $1, $2, $3, 'delete-test', 'workspace-rollup')
`, wsID, rollupRuntimeID, rollupAgentID)

	runtimeProfileID := dbfx.Insert(t, "runtime_profile", testutil.Cols{
		"workspace_id":    wsID,
		"display_name":    "Delete cleanup profile",
		"protocol_family": "codex",
		"command_name":    "codex",
		"created_by":      testUserID,
	})

	ruleVersionID := dbfx.Insert(t, "autopilot_rule_version", testutil.Cols{
		"autopilot_id":      testutil.Raw("gen_random_uuid()"),
		"workspace_id":      wsID,
		"published_by_type": "member",
		"published_by_id":   testUserID,
	})

	const pendingObjectKey = "workspace-delete-pending-object"
	dbfx.Exec(t, `
INSERT INTO channel_media_pending_object (
	storage_key, workspace_id, chat_message_id, storage_url
)
VALUES ($1, $2, gen_random_uuid(), 's3://workspace-delete/pending-object')
`, pendingObjectKey, wsID)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task_usage_hourly_dirty WHERE workspace_id = $1`, wsID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task_usage_hourly WHERE workspace_id = $1`, wsID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM runtime_profile WHERE id = $1`, runtimeProfileID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot_rule_version WHERE id = $1`, ruleVersionID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_media_pending_object WHERE storage_key = $1`, pendingObjectKey)
	})

	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusNoContent)

	var exists bool
	dbfx.QueryRow(t, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists)
	if exists {
		t.Fatal("workspace still exists after owner DELETE")
	}

	var pendingCount int
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM github_pending_check_suite WHERE workspace_id = $1`, wsID).Scan(&pendingCount)
	if pendingCount != 0 {
		t.Fatalf("pending check suites were not cleaned up for deleted workspace: %d", pendingCount)
	}

	var checkRunCount int
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM github_pull_request_check_run WHERE pr_id = $1`, githubPRID).Scan(&checkRunCount)
	if checkRunCount != 0 {
		t.Fatalf("github PR check runs were not cleaned up for deleted workspace: %d", checkRunCount)
	}

	var propertyCount int
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM issue_property WHERE id = $1`, propertyID).Scan(&propertyCount)
	if propertyCount != 0 {
		t.Fatalf("issue properties were not cleaned up for deleted workspace: %d", propertyCount)
	}

	for _, table := range []string{
		"task_usage_hourly_dirty",
		"task_usage_hourly",
		"runtime_profile",
		"autopilot_rule_version",
	} {
		var count int
		dbfx.QueryRow(t, `SELECT COUNT(*) FROM `+table+` WHERE workspace_id = $1`, wsID).Scan(&count)
		if count != 0 {
			t.Fatalf("%s rows survived workspace delete: %d", table, count)
		}
	}

	var pendingObjectState string
	dbfx.QueryRow(t, `
SELECT state
FROM channel_media_pending_object
WHERE storage_key = $1
`, pendingObjectKey).Scan(&pendingObjectState)
	if pendingObjectState != "deleting" {
		t.Fatalf("pending channel media object state = %q, want deleting", pendingObjectState)
	}
}

func TestDeleteWorkspace_DirtyTriggersHaveTeardownGuard(t *testing.T) {
	for _, triggerName := range []string{
		"trg_atq_dirty_hourly",
		"trg_issue_delete_dirty_hourly",
		"trg_tu_dirty_hourly",
	} {
		var definition string
		dbfx.QueryRow(t, `
SELECT pg_get_triggerdef(oid)
FROM pg_trigger
WHERE tgname = $1
  AND NOT tgisinternal
`, triggerName).Scan(&definition)
		if !strings.Contains(definition, "multica.workspace_teardown") {
			t.Fatalf("trigger %s does not guard workspace teardown: %s", triggerName, definition)
		}
	}
}

func TestWorkspaceTeardownModeDoesNotLeakIntoOrdinaryDeletes(t *testing.T) {
	ctx := context.Background()
	conn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin teardown marker transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('multica.workspace_teardown', 'on', true)`); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("set transaction-local teardown mode: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit teardown marker transaction: %v", err)
	}

	var teardownMode string
	if err := conn.QueryRow(ctx, `SELECT current_setting('multica.workspace_teardown', true)`).Scan(&teardownMode); err != nil {
		t.Fatalf("read teardown mode after commit: %v", err)
	}
	if teardownMode != "" {
		t.Fatalf("teardown mode leaked after commit: %q", teardownMode)
	}

	var runtimeID, agentID string
	if err := conn.QueryRow(ctx, `
SELECT runtime.id, agent.id
FROM agent_runtime AS runtime
JOIN agent ON agent.runtime_id = runtime.id
WHERE runtime.workspace_id = $1
LIMIT 1
`, testWorkspaceID).Scan(&runtimeID, &agentID); err != nil {
		t.Fatalf("load ordinary delete fixture agent: %v", err)
	}

	var issueID string
	if err := conn.QueryRow(ctx, `
INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
VALUES (
	$1, 'ordinary delete after workspace teardown', $2, 'member',
	(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1)
)
RETURNING id
`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create ordinary delete issue: %v", err)
	}
	var taskID string
	if err := conn.QueryRow(ctx, `
INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status)
VALUES ($1, $2, $3, 'completed')
RETURNING id
`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("create ordinary delete task: %v", err)
	}

	const provider = "workspace-teardown-ordinary-delete"
	_, _ = conn.Exec(ctx, `DELETE FROM task_usage_hourly_dirty WHERE provider = $1`, provider)
	_, _ = conn.Exec(ctx, `DELETE FROM task_usage_hourly WHERE provider = $1`, provider)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task_usage_hourly_dirty WHERE provider = $1`, provider)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task_usage_hourly WHERE provider = $1`, provider)
	})

	var usageID string
	if err := conn.QueryRow(ctx, `
INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens)
VALUES ($1, $2, 'task-usage-delete', 10, 5)
RETURNING id
`, taskID, provider).Scan(&usageID); err != nil {
		t.Fatalf("create ordinary task usage: %v", err)
	}
	if _, err := conn.Exec(ctx, `DELETE FROM task_usage WHERE id = $1`, usageID); err != nil {
		t.Fatalf("delete ordinary task usage: %v", err)
	}

	var dirtyCount int
	if err := conn.QueryRow(ctx, `
SELECT COUNT(*)
FROM task_usage_hourly_dirty
WHERE provider = $1 AND model = 'task-usage-delete'
`, provider).Scan(&dirtyCount); err != nil {
		t.Fatalf("count task-usage delete dirty keys: %v", err)
	}
	if dirtyCount != 1 {
		t.Fatalf("ordinary task_usage DELETE dirty keys = %d, want 1", dirtyCount)
	}

	if _, err := conn.Exec(ctx, `
INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens)
VALUES ($1, $2, 'issue-delete', 10, 5)
`, taskID, provider); err != nil {
		t.Fatalf("create ordinary issue-delete usage: %v", err)
	}
	if _, err := conn.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID); err != nil {
		t.Fatalf("delete ordinary issue: %v", err)
	}
	if err := conn.QueryRow(ctx, `
SELECT COUNT(*)
FROM task_usage_hourly_dirty
WHERE provider = $1 AND model = 'issue-delete'
`, provider).Scan(&dirtyCount); err != nil {
		t.Fatalf("count issue delete dirty keys: %v", err)
	}
	if dirtyCount != 1 {
		t.Fatalf("ordinary issue DELETE dirty keys = %d, want 1", dirtyCount)
	}
}

func TestDeleteWorkspace_PreservesOtherWorkspaceData(t *testing.T) {
	ctx := context.Background()
	const targetSlug = "handler-tests-delete-tenant-target"
	const neighborSlug = "handler-tests-delete-tenant-neighbor"
	const targetMediaKey = "workspace-delete-tenant-target-media"
	const neighborMediaKey = "workspace-delete-tenant-neighbor-media"

	_, _ = testPool.Exec(ctx, `DELETE FROM channel_media_pending_object WHERE storage_key IN ($1, $2)`, targetMediaKey, neighborMediaKey)
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug IN ($1, $2)`, targetSlug, neighborSlug)

	var targetWorkspaceID, neighborWorkspaceID string
	dbfx.QueryRow(t, `
INSERT INTO workspace (name, slug)
VALUES ('Workspace delete tenant target', $1)
RETURNING id
`, targetSlug).Scan(&targetWorkspaceID)
	dbfx.QueryRow(t, `
INSERT INTO workspace (name, slug)
VALUES ('Workspace delete tenant neighbor', $1)
RETURNING id
`, neighborSlug).Scan(&neighborWorkspaceID)
	t.Cleanup(func() {
		for _, workspaceID := range []string{targetWorkspaceID, neighborWorkspaceID} {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
			_, _ = testPool.Exec(context.Background(), `DELETE FROM task_usage_hourly_dirty WHERE workspace_id = $1`, workspaceID)
			_, _ = testPool.Exec(context.Background(), `DELETE FROM runtime_profile WHERE workspace_id = $1`, workspaceID)
			_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_media_pending_object WHERE workspace_id = $1`, workspaceID)
		}
	})

	dbfx.Exec(t, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, targetWorkspaceID, testUserID)

	type tenantFixture struct {
		workspaceID string
		mediaKey    string
		issueID     string
	}
	fixtures := []*tenantFixture{
		{workspaceID: targetWorkspaceID, mediaKey: targetMediaKey},
		{workspaceID: neighborWorkspaceID, mediaKey: neighborMediaKey},
	}
	for _, fixture := range fixtures {
		dbfx.QueryRow(t, `
INSERT INTO issue (workspace_id, title, creator_type, creator_id)
VALUES ($1, 'Workspace delete tenant isolation', 'member', $2)
RETURNING id
`, fixture.workspaceID, testUserID).Scan(&fixture.issueID)
		dbfx.Exec(t, `
INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
VALUES ($1, $2, 'member', $3, 'Workspace delete tenant isolation')
`, fixture.issueID, fixture.workspaceID, testUserID)
		dbfx.Exec(t, `
INSERT INTO inbox_item (
	workspace_id, recipient_type, recipient_id, type, issue_id, title
)
VALUES ($1, 'member', $2, 'workspace-delete-test', $3, 'Workspace delete tenant isolation')
`, fixture.workspaceID, testUserID, fixture.issueID)
		dbfx.Exec(t, `
INSERT INTO runtime_profile (
	workspace_id, display_name, protocol_family, command_name, created_by
)
VALUES ($1, 'Workspace delete tenant isolation', 'codex', 'codex', $2)
`, fixture.workspaceID, testUserID)
		dbfx.Exec(t, `
INSERT INTO task_usage_hourly_dirty (
	bucket_hour, workspace_id, runtime_id, agent_id, provider, model
)
VALUES (date_trunc('hour', now()), $1, gen_random_uuid(), gen_random_uuid(), 'workspace-delete-tenant', 'isolation')
`, fixture.workspaceID)
		dbfx.Exec(t, `
INSERT INTO channel_media_pending_object (
	storage_key, workspace_id, chat_message_id, storage_url
)
VALUES ($1, $2, gen_random_uuid(), 's3://workspace-delete/tenant-isolation')
`, fixture.mediaKey, fixture.workspaceID)
	}

	request := newRequest(http.MethodDelete, "/api/workspaces/"+targetWorkspaceID, nil)
	request = withURLParam(request, "id", targetWorkspaceID)
	testutil.Call(t, testHandler.DeleteWorkspace, request).Want(http.StatusNoContent)

	for table, predicate := range map[string]string{
		"workspace":                    "id",
		"issue":                        "workspace_id",
		"comment":                      "workspace_id",
		"inbox_item":                   "workspace_id",
		"runtime_profile":              "workspace_id",
		"task_usage_hourly_dirty":      "workspace_id",
		"channel_media_pending_object": "workspace_id",
	} {
		var count int
		dbfx.QueryRow(t, `
SELECT COUNT(*) FROM `+table+` WHERE `+predicate+` = $1
`, neighborWorkspaceID).Scan(&count)
		if count != 1 {
			t.Fatalf("neighbor %s rows = %d, want 1", table, count)
		}
	}

	var neighborMediaState string
	dbfx.QueryRow(t, `
SELECT state
FROM channel_media_pending_object
WHERE storage_key = $1
`, neighborMediaKey).Scan(&neighborMediaState)
	if neighborMediaState != "pending" {
		t.Fatalf("neighbor media state = %q, want pending", neighborMediaState)
	}
}

// TestUpdateWorkspace_AvatarURL covers the avatar_url field added to
// UpdateWorkspaceRequest: a PATCH with avatar_url is persisted and surfaced
// back on the response, and partial updates leave other fields untouched.
// Route-level authorization (owner/admin) is enforced by middleware in
// router.go; the handler test calls UpdateWorkspace directly to verify the
// payload wiring.
func TestUpdateWorkspace_AvatarURL(t *testing.T) {
	ctx := context.Background()

	const slug = "handler-tests-avatar-url"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	wsID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":        "Handler Test Avatar URL",
		"slug":        slug,
		"description": "UpdateWorkspace avatar_url test",
	})

	dbfx.Exec(t, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, wsID, testUserID)

	const avatarURL = "https://cdn.example.com/workspaces/abc/logo.png"

	req := newRequest("PATCH", "/api/workspaces/"+wsID, map[string]any{
		"avatar_url": avatarURL,
	})
	req = withURLParam(req, "id", wsID)
	w := testutil.Call(t, testHandler.UpdateWorkspace, req).Want(http.StatusOK)

	var resp WorkspaceResponse
	w.JSON(&resp)
	if resp.AvatarURL == nil || *resp.AvatarURL != avatarURL {
		t.Fatalf("expected avatar_url %q in response, got %v", avatarURL, resp.AvatarURL)
	}
	if resp.Name != "Handler Test Avatar URL" {
		t.Fatalf("name should be unchanged by avatar-only update, got %q", resp.Name)
	}

	var dbAvatar *string
	dbfx.QueryRow(t, `SELECT avatar_url FROM workspace WHERE id = $1`, wsID).Scan(&dbAvatar)
	if dbAvatar == nil || *dbAvatar != avatarURL {
		t.Fatalf("expected avatar_url %q persisted, got %v", avatarURL, dbAvatar)
	}

	// A follow-up update that doesn't include avatar_url must leave it alone.
	req2 := newRequest("PATCH", "/api/workspaces/"+wsID, map[string]any{
		"description": "new description",
	})
	req2 = withURLParam(req2, "id", wsID)
	w2 := testutil.Call(t, testHandler.UpdateWorkspace, req2).Want(http.StatusOK)

	var resp2 WorkspaceResponse
	w2.JSON(&resp2)
	if resp2.AvatarURL == nil || *resp2.AvatarURL != avatarURL {
		t.Fatalf("avatar_url should be preserved by partial update, got %v", resp2.AvatarURL)
	}
}

func TestUpdateWorkspace_ReposValidation(t *testing.T) {
	ctx := context.Background()

	const slug = "handler-tests-repos-validation"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	wsID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":        "Handler Test Repos Validation",
		"slug":        slug,
		"description": "UpdateWorkspace repos validation test",
	})

	dbfx.Exec(t, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, wsID, testUserID)

	t.Run("rejects invalid repo URLs without persisting", func(t *testing.T) {
		req := newRequest("PATCH", "/api/workspaces/"+wsID, map[string]any{
			"repos": []map[string]any{
				{"url": "not-a-url"},
			},
		})
		req = withURLParam(req, "id", wsID)
		testutil.Call(t, testHandler.UpdateWorkspace, req).Want(http.StatusBadRequest)

		var raw []byte
		dbfx.QueryRow(t, `SELECT repos FROM workspace WHERE id = $1`, wsID).Scan(&raw)
		if string(raw) != "[]" {
			t.Fatalf("invalid repos update should not persist, got %s", raw)
		}
	})

	t.Run("normalizes valid repos", func(t *testing.T) {
		req := newRequest("PATCH", "/api/workspaces/"+wsID, map[string]any{
			"repos": []map[string]any{
				{
					"url":         "  https://github.com/multica-ai/multica.git  ",
					"description": "  main monorepo  ",
				},
				{
					"url": "https://github.com/multica-ai/multica.git",
				},
				{
					"url": "git@github.com:multica-ai/multica-cloud.git",
				},
			},
		})
		req = withURLParam(req, "id", wsID)
		testutil.Call(t, testHandler.UpdateWorkspace, req).Want(http.StatusOK)

		var raw []byte
		dbfx.QueryRow(t, `SELECT repos FROM workspace WHERE id = $1`, wsID).Scan(&raw)
		var repos []workspaceRepoRef
		if err := json.Unmarshal(raw, &repos); err != nil {
			t.Fatalf("decode repos: %v", err)
		}
		if len(repos) != 2 {
			t.Fatalf("expected duplicate URL to be deduped, got %d repos: %s", len(repos), raw)
		}
		if repos[0].URL != "https://github.com/multica-ai/multica.git" || repos[0].Description != "main monorepo" {
			t.Fatalf("first repo not normalized: %+v", repos[0])
		}
		if repos[1].URL != "git@github.com:multica-ai/multica-cloud.git" {
			t.Fatalf("second repo not preserved: %+v", repos[1])
		}
	})
}

// revocationFixture is a minimal (workspace, member-to-revoke, runtime,
// agent, queued-task, daemon-token) bundle used to drive the revocation
// tests. The "requester" is always testUserID (owner of the workspace) so
// `newRequest` passes the existing fixtures' auth context unchanged.
type revocationFixture struct {
	WorkspaceID  string
	TargetUserID string
	MemberID     string
	RuntimeID    string
	AgentID      string
	TaskID       string
	DaemonID     string
	TokenHash    string
}

func setupRevocationFixture(t *testing.T, slug, daemonID string) revocationFixture {
	t.Helper()
	ctx := context.Background()

	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	wsID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "Revocation " + slug,
		"slug":         slug,
		"description":  "revocation test",
		"issue_prefix": "REV",
	})

	// Requester (= testUserID) is always an owner so DeleteMember authorization
	// passes. Two owners total so LeaveWorkspace doesn't trip the "must keep
	// at least one owner" guard.
	dbfx.Exec(t, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
`, wsID, testUserID)

	targetEmail := fmt.Sprintf("revocation-%s@multica.ai", slug)
	targetUserID := dbfx.Insert(t, "user", testutil.Cols{
		"name":  "Revocation Target " + slug,
		"email": targetEmail,
	})

	// Cleanup ordering: workspace first (cascade clears agent_runtime,
	// agent, member, daemon_token), then user (whose deletion would
	// otherwise be blocked by agent.owner_id / agent_runtime.owner_id FKs).
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, targetUserID)
	})

	memberID := dbfx.Insert(t, "member", testutil.Cols{
		"workspace_id": wsID,
		"user_id":      targetUserID,
		"role":         "owner",
	})

	runtimeID := dbfx.Runtime(t, "Target Runtime", testutil.Cols{
		"workspace_id": wsID,
		"daemon_id":    daemonID,
		"runtime_mode": "local",
		"provider":     "multica_daemon",
		"owner_id":     targetUserID,
	})

	agentID := dbfx.Agent(t, "Target Agent", runtimeID, testutil.Cols{
		"workspace_id": wsID,
		"runtime_mode": "local",
		"visibility":   "workspace",
		"owner_id":     targetUserID,
	})

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
	})

	// daemon_token row — paired with the runtime's daemon_id so the
	// revocation should sweep its hash up via DeleteDaemonTokensByWorkspaceAndDaemons.
	rawToken := "mdt_test_" + slug
	sum := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(sum[:])
	dbfx.Exec(t, `
INSERT INTO daemon_token (token_hash, workspace_id, daemon_id, expires_at)
VALUES ($1, $2, $3, now() + interval '1 day')
`, tokenHash, wsID, daemonID)

	return revocationFixture{
		WorkspaceID:  wsID,
		TargetUserID: targetUserID,
		MemberID:     memberID,
		RuntimeID:    runtimeID,
		AgentID:      agentID,
		TaskID:       taskID,
		DaemonID:     daemonID,
		TokenHash:    tokenHash,
	}
}

func assertRevoked(t *testing.T, fx revocationFixture) {
	t.Helper()

	var memberExists bool
	dbfx.QueryRow(t, `SELECT EXISTS (SELECT 1 FROM member WHERE id = $1)`, fx.MemberID).Scan(&memberExists)
	if memberExists {
		t.Fatal("member row was not deleted")
	}

	var runtimeStatus string
	dbfx.QueryRow(t, `SELECT status FROM agent_runtime WHERE id = $1`, fx.RuntimeID).Scan(&runtimeStatus)
	if runtimeStatus != "offline" {
		t.Fatalf("expected runtime offline, got %q", runtimeStatus)
	}

	var archivedAt *string
	dbfx.QueryRow(t, `SELECT archived_at::text FROM agent WHERE id = $1`, fx.AgentID).Scan(&archivedAt)
	if archivedAt == nil {
		t.Fatal("agent was not archived")
	}

	var taskStatus string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, fx.TaskID).Scan(&taskStatus)
	if taskStatus != "cancelled" {
		t.Fatalf("expected task cancelled, got %q", taskStatus)
	}

	var tokenExists bool
	dbfx.QueryRow(t, `SELECT EXISTS (SELECT 1 FROM daemon_token WHERE token_hash = $1)`, fx.TokenHash).Scan(&tokenExists)
	if tokenExists {
		t.Fatal("daemon_token row was not deleted")
	}
}

// TestDeleteMember_RevokesTargetRuntimes verifies that when an admin removes
// another member from a workspace, every runtime owned by the removed member
// has its agents archived, its in-flight tasks cancelled, its row flipped
// offline, and its daemon_token rows deleted — all atomically with the member
// row deletion.
func TestDeleteMember_RevokesTargetRuntimes(t *testing.T) {
	fx := setupRevocationFixture(t, "handler-tests-revoke-kick", "daemon-revoke-kick")

	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/members/"+fx.MemberID, nil)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParams(req, "id", fx.WorkspaceID, "memberId", fx.MemberID)
	testutil.Call(t, testHandler.DeleteMember, req).Want(http.StatusNoContent)

	assertRevoked(t, fx)
}

// TestDeleteMember_PrunesChannelUserBindings verifies the application-layer
// replacement for the channel_user_binding member-FK cascade (MUL-3515 §4):
// removing a member prunes that member's channel bindings, in the same tx as
// the member-row delete, while leaving a remaining member's binding intact.
func TestDeleteMember_PrunesChannelUserBindings(t *testing.T) {
	fx := setupRevocationFixture(t, "handler-tests-revoke-binding", "daemon-revoke-binding")

	const appID = "cli_revoke_binding"
	const removedOpenID = "ou_revoke_binding_removed"
	const keepOpenID = "ou_revoke_binding_keep"

	// channel_* rows have no FK to workspace (MUL-3515 §4), so the fixture's
	// workspace-delete cleanup never reaches them; clear by deterministic key
	// both before (in case a prior run was killed mid-test) and after.
	cleanChannel := func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM channel_user_binding WHERE channel_user_id = ANY($1)`,
			[]string{removedOpenID, keepOpenID})
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM channel_installation WHERE channel_type = 'feishu' AND config->>'app_id' = $1`, appID)
	}
	cleanChannel()
	t.Cleanup(cleanChannel)

	var installID string
	dbfx.QueryRow(t, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id)
VALUES ($1, $2, 'feishu', jsonb_build_object('app_id', $3::text), $4)
RETURNING id
`, fx.WorkspaceID, fx.AgentID, appID, testUserID).Scan(&installID)

	// Binding for the member being removed — must be pruned.
	dbfx.Exec(t, `
INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id)
VALUES ($1, $2, $3, 'feishu', $4)
`, fx.WorkspaceID, fx.TargetUserID, installID, removedOpenID)

	// Binding for the requester (an owner who stays) — must survive, proving
	// the prune is scoped to the removed user, not the whole workspace.
	dbfx.Exec(t, `
INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id)
VALUES ($1, $2, $3, 'feishu', $4)
`, fx.WorkspaceID, testUserID, installID, keepOpenID)

	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/members/"+fx.MemberID, nil)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParams(req, "id", fx.WorkspaceID, "memberId", fx.MemberID)
	testutil.Call(t, testHandler.DeleteMember, req).Want(http.StatusNoContent)

	var removedExists bool
	dbfx.QueryRow(t,
		`SELECT EXISTS (SELECT 1 FROM channel_user_binding WHERE channel_user_id = $1)`, removedOpenID).Scan(&removedExists)
	if removedExists {
		t.Fatal("removed member's channel_user_binding was not pruned")
	}

	var keepExists bool
	dbfx.QueryRow(t,
		`SELECT EXISTS (SELECT 1 FROM channel_user_binding WHERE channel_user_id = $1)`, keepOpenID).Scan(&keepExists)
	if !keepExists {
		t.Fatal("remaining member's channel_user_binding was wrongly pruned")
	}
}

// TestLeaveWorkspace_RevokesOwnRuntimes is the self-removal counterpart: when
// a member leaves a workspace voluntarily, their own runtimes are revoked
// with the same atomic write set as DeleteMember.
func TestLeaveWorkspace_RevokesOwnRuntimes(t *testing.T) {
	fx := setupRevocationFixture(t, "handler-tests-revoke-leave", "daemon-revoke-leave")

	// Re-target the request from the leaving member's perspective: the
	// leaver is the request actor, not the workspace owner.
	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/leave", nil)
	req.Header.Set("X-User-ID", fx.TargetUserID)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParam(req, "id", fx.WorkspaceID)
	testutil.Call(t, testHandler.LeaveWorkspace, req).Want(http.StatusNoContent)

	assertRevoked(t, fx)
}

// TestDeleteMember_CancelsTasksFromAgentReassignment covers a subtle
// case: an agent's runtime_id can be changed via UpdateAgent, but
// agent_task_queue.runtime_id keeps the value from when the task was
// queued. So after a leaving member is removed, an agent currently bound
// to their runtime gets archived — but tasks that agent queued under a
// PRIOR runtime (still owned by another active member) keep their old
// runtime_id and would not be caught by a runtime-only sweep. Because
// ClaimAgentTask does not gate on agent.archived_at, those orphaned
// queued tasks would remain claimable.
func TestDeleteMember_CancelsTasksFromAgentReassignment(t *testing.T) {
	fx := setupRevocationFixture(t, "handler-tests-revoke-reassign", "daemon-revoke-reassign")

	// Create a SECOND runtime in the workspace owned by the requester
	// (not the leaving member). The agent originally lived here.
	otherRuntimeID := dbfx.Runtime(t, "Other Runtime", testutil.Cols{
		"workspace_id": fx.WorkspaceID,
		"daemon_id":    "daemon-revoke-reassign-other",
		"runtime_mode": "local",
		"provider":     "multica_daemon",
	})

	// Queue a task on the agent while it was still pinned to the OTHER
	// runtime (simulating a task created before the agent was reassigned
	// to the leaving member's runtime).
	orphanTaskID := dbfx.Task(t, fx.AgentID, testutil.Cols{
		"runtime_id": otherRuntimeID,
	})

	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/members/"+fx.MemberID, nil)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParams(req, "id", fx.WorkspaceID, "memberId", fx.MemberID)
	testutil.Call(t, testHandler.DeleteMember, req).Want(http.StatusNoContent)

	assertRevoked(t, fx)

	// The orphan task — same agent, different runtime — must also be
	// cancelled. Without the by-agent leg in CancelAgentTasksByRuntimeOrAgent
	// this stays 'queued' and would be picked up by the other runtime.
	var orphanStatus string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, orphanTaskID).Scan(&orphanStatus)
	if orphanStatus != "cancelled" {
		t.Fatalf("expected orphan task cancelled (archived agent leftover on other runtime), got %q", orphanStatus)
	}

	// And the OTHER runtime — owned by an active member — must still be
	// online: revocation is scoped to the leaving member's owned runtimes.
	var otherStatus string
	dbfx.QueryRow(t, `SELECT status FROM agent_runtime WHERE id = $1`, otherRuntimeID).Scan(&otherStatus)
	if otherStatus != "online" {
		t.Fatalf("expected other-member runtime to stay online, got %q", otherStatus)
	}
}

// TestDeleteMember_CancelsDeferredTasks covers the second caller of
// CancelAgentTasksByRuntimeOrAgent. Member revocation must cancel a scheduled
// fallback just like queued/running work; otherwise it could become claimable
// after its owner and runtime access have been removed.
func TestDeleteMember_CancelsDeferredTasks(t *testing.T) {
	fx := setupRevocationFixture(t, "handler-tests-revoke-deferred", "daemon-revoke-deferred")

	deferredTaskID := dbfx.Task(t, fx.AgentID, testutil.Cols{
		"runtime_id": fx.RuntimeID,
		"status":     "deferred",
		"fire_at":    testutil.Raw("now() + interval '1 hour'"),
	})

	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/members/"+fx.MemberID, nil)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParams(req, "id", fx.WorkspaceID, "memberId", fx.MemberID)
	testutil.Call(t, testHandler.DeleteMember, req).Want(http.StatusNoContent)

	assertRevoked(t, fx)

	var status string
	var completedAt *time.Time
	dbfx.QueryRow(t,
		`SELECT status, completed_at FROM agent_task_queue WHERE id = $1`,
		deferredTaskID,
	).Scan(&status, &completedAt)
	if status != "cancelled" || completedAt == nil {
		t.Fatalf("deferred task = (%q, %v), want cancelled with completed_at", status, completedAt)
	}
}

// TestDeleteMember_NoRuntimes_DeletesMember covers the empty-revocation
// path: a member with no owned runtimes should still have their member row
// deleted by the same atomic transaction, with no spurious archive/cancel
// writes.
func TestDeleteMember_NoRuntimes_DeletesMember(t *testing.T) {
	ctx := context.Background()
	const slug = "handler-tests-revoke-no-runtimes"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	wsID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "Revocation no runtimes",
		"slug":         slug,
		"description":  "revocation no-runtimes test",
		"issue_prefix": "REV",
	})

	dbfx.Exec(t, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
`, wsID, testUserID)

	targetUserID := dbfx.Insert(t, "user", testutil.Cols{
		"name":  "Revocation No Runtimes Target",
		"email": "revocation-no-runtimes@multica.ai",
	})
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, targetUserID)
	})

	memberID := dbfx.Insert(t, "member", testutil.Cols{
		"workspace_id": wsID,
		"user_id":      targetUserID,
		"role":         "admin",
	})

	req := newRequest("DELETE", "/api/workspaces/"+wsID+"/members/"+memberID, nil)
	req.Header.Set("X-Workspace-ID", wsID)
	req = withURLParams(req, "id", wsID, "memberId", memberID)
	testutil.Call(t, testHandler.DeleteMember, req).Want(http.StatusNoContent)

	var memberExists bool
	dbfx.QueryRow(t, `SELECT EXISTS (SELECT 1 FROM member WHERE id = $1)`, memberID).Scan(&memberExists)
	if memberExists {
		t.Fatal("member row was not deleted")
	}
}

// TestDefaultIssuePrefixFromSlug pins the derivation new workspaces get
// (MUL-6050): alphanumerics of the slug, first 4, uppercased. The Chinese
// cases are the point of the change — under the old name-based derivation
// every one of them collapsed to "WS".
//
// Keep this table in sync with the client-side preview
// (packages/views/onboarding/steps/step-workspace.tsx). If the two ever
// disagree, the create screen lies about the identifier the user will get.
func TestDefaultIssuePrefixFromSlug(t *testing.T) {
	cases := []struct {
		slug string
		want string
	}{
		{"acme", "ACME"},
		{"front-end", "FRON"},
		{"growth", "GROW"},
		{"team-2", "TEAM"},
		{"a1b2c3", "A1B2"},
		{"ab", "AB"},
		{"x", "X"},
		// Slugs the create handler would have rejected anyway; the function
		// stays total so no caller can persist an empty prefix.
		{"", "WS"},
		{"--", "WS"},
	}

	for _, tc := range cases {
		if got := defaultIssuePrefixFromSlug(tc.slug); got != tc.want {
			t.Errorf("defaultIssuePrefixFromSlug(%q) = %q, want %q", tc.slug, got, tc.want)
		}
	}
}

// TestLegacyIssuePrefixFromName_Frozen guards the read-time fallback for
// workspaces whose stored prefix is empty. Issue identifiers are computed
// from the current prefix on every read, so changing what this returns would
// silently rewrite the identifier of every historical issue in those
// workspaces. The product decision on MUL-6050 was explicit: no backfill,
// existing workspaces are left exactly as they are — which means this
// function must keep returning what it always did, including "WS" for
// non-ASCII names.
func TestLegacyIssuePrefixFromName_Frozen(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"Jiayuan's Workspace", "JIA"},
		{"My Team", "MYT"},
		{"AB", "AB"},
		{"Team 2", "TEA"},
		{"前端团队", "WS"},
		{"", "WS"},
	}

	for _, tc := range cases {
		if got := legacyIssuePrefixFromName(tc.name); got != tc.want {
			t.Errorf("legacyIssuePrefixFromName(%q) = %q, want %q — this function is frozen; changing it rewrites existing issue identifiers", tc.name, got, tc.want)
		}
	}
}

// TestIssuePrefixForWorkspace_LegacyFallbackFrozen guards the resolution rule
// itself, at the seam every read-time caller goes through (getIssuePrefix and
// the GitHub close-intent scan, which holds the row already).
//
// A stored prefix always wins; only an empty one falls back, and that fallback
// must stay on the old name-based derivation. Pointing it at
// defaultIssuePrefixFromSlug would rewrite the identifier of every issue in
// those legacy workspaces on the next read — the exact outcome the "no
// backfill, leave existing workspaces alone" decision on MUL-6050 rules out.
func TestIssuePrefixForWorkspace_LegacyFallbackFrozen(t *testing.T) {
	cases := []struct {
		label string
		ws    db.Workspace
		want  string
	}{
		{"stored prefix wins", db.Workspace{Name: "前端团队", Slug: "frontend", IssuePrefix: "FE"}, "FE"},
		{"empty prefix, CJK name", db.Workspace{Name: "前端团队", Slug: "frontend"}, "WS"},
		{"empty prefix, ASCII name", db.Workspace{Name: "My Team", Slug: "my-team"}, "MYT"},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			if got := issuePrefixForWorkspace(tc.ws); got != tc.want {
				t.Fatalf("issuePrefixForWorkspace(%+v) = %q, want %q — legacy workspaces must keep the identifiers they already have", tc.ws, got, tc.want)
			}
		})
	}
}

func TestNormalizeIssuePrefix(t *testing.T) {
	cases := []struct {
		raw   string
		want  string
		valid bool
	}{
		{"acme", "ACME", true},
		{"  acme  ", "ACME", true},
		{"AB12", "AB12", true},
		{"ABCDEFGHIJ", "ABCDEFGHIJ", true},
		// Absent / blank means "use the default", not "invalid".
		{"", "", true},
		{"   ", "", true},
		// Rejections the API accepted before MUL-6050.
		{"ABCDEFGHIJK", "", false},
		{"前端", "", false},
		{"AB-CD", "", false},
		{"AB CD", "", false},
		{"AB_CD", "", false},
	}

	for _, tc := range cases {
		got, ok := normalizeIssuePrefix(tc.raw)
		if ok != tc.valid {
			t.Errorf("normalizeIssuePrefix(%q) valid = %v, want %v", tc.raw, ok, tc.valid)
			continue
		}
		if got != tc.want {
			t.Errorf("normalizeIssuePrefix(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// TestCreateWorkspace_ChineseNameDerivesPrefixFromSlug is the end-to-end
// regression for MUL-6050: a workspace whose name has no ASCII letters used
// to be created with prefix "WS" — deterministically, for every Chinese team
// on the instance. It must now take its prefix from the slug, which the same
// form already forced the user to choose in ASCII.
func TestCreateWorkspace_ChineseNameDerivesPrefixFromSlug(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	const slug = "handler-tests-frontend-team"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	dbfx.Cleanup(t, `DELETE FROM workspace WHERE slug = $1`, slug)

	req := newRequest("POST", "/api/workspaces", map[string]any{
		"name": "前端团队",
		"slug": slug,
	})
	w := testutil.Call(t, testHandler.CreateWorkspace, req).Want(http.StatusCreated)

	var resp WorkspaceResponse
	w.JSON(&resp)
	if resp.IssuePrefix != "HAND" {
		t.Fatalf("issue_prefix = %q, want %q (derived from the slug, not the name)", resp.IssuePrefix, "HAND")
	}
	if resp.IssuePrefix == "WS" {
		t.Fatal("issue_prefix fell back to WS — the MUL-6050 regression is back")
	}
}

// TestCreateWorkspace_HonorsExplicitIssuePrefix covers the create screen's new
// editable prefix field: whatever the user typed is what gets persisted.
func TestCreateWorkspace_HonorsExplicitIssuePrefix(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	const slug = "handler-tests-explicit-prefix"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	dbfx.Cleanup(t, `DELETE FROM workspace WHERE slug = $1`, slug)

	req := newRequest("POST", "/api/workspaces", map[string]any{
		"name":         "前端团队",
		"slug":         slug,
		"issue_prefix": "fe",
	})
	w := testutil.Call(t, testHandler.CreateWorkspace, req).Want(http.StatusCreated)

	var resp WorkspaceResponse
	w.JSON(&resp)
	if resp.IssuePrefix != "FE" {
		t.Fatalf("issue_prefix = %q, want %q", resp.IssuePrefix, "FE")
	}
}

// TestCreateWorkspace_RejectsInvalidIssuePrefix closes the API-side hole the
// settings UI already guarded: before MUL-6050 the create and update handlers
// only trimmed and uppercased the caller-supplied prefix, so a direct API call
// could persist CJK text or a 100-character string as an issue prefix.
func TestCreateWorkspace_RejectsInvalidIssuePrefix(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	invalid := map[string]string{
		"non-ascii":  "前端",
		"too-long":   "ABCDEFGHIJK",
		"hyphenated": "AB-CD",
		"spaced":     "AB CD",
	}

	for label, prefix := range invalid {
		t.Run(label, func(t *testing.T) {
			slug := "handler-tests-bad-prefix-" + label
			ctx := context.Background()
			_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
			dbfx.Cleanup(t, `DELETE FROM workspace WHERE slug = $1`, slug)

			req := newRequest("POST", "/api/workspaces", map[string]any{
				"name":         "Prefix Validation Probe",
				"slug":         slug,
				"issue_prefix": prefix,
			})
			w := testutil.Call(t, testHandler.CreateWorkspace, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("issue_prefix %q: expected 400, got %d: %s", prefix, w.Code, w.Body.String())
			}

			var count int
			dbfx.QueryRow(t, `SELECT count(*) FROM workspace WHERE slug = $1`, slug).Scan(&count)
			if count != 0 {
				t.Fatalf("expected no workspace row for a rejected prefix, found %d", count)
			}
		})
	}
}

// TestUpdateWorkspace_RejectsInvalidIssuePrefix is the PATCH half of the same
// hole: settings normalizes client-side, but the endpoint took anything.
func TestUpdateWorkspace_RejectsInvalidIssuePrefix(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	var before string
	dbfx.QueryRow(t, `SELECT issue_prefix FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&before)

	req := withURLParam(
		newRequest("PATCH", "/api/workspaces/"+testWorkspaceID, map[string]any{"issue_prefix": "前端团队前端团队前端"}),
		"id", testWorkspaceID,
	)
	testutil.Call(t, testHandler.UpdateWorkspace, req).Want(http.StatusBadRequest)

	var after string
	dbfx.QueryRow(t, `SELECT issue_prefix FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&after)
	if after != before {
		t.Fatalf("issue_prefix changed on a rejected update: %q → %q", before, after)
	}
}
