package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
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
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/workspaces", map[string]any{
				"name": fmt.Sprintf("Test %s", slug),
				"slug": slug,
			})
			testHandler.CreateWorkspace(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("slug %q: expected 400, got %d: %s", slug, w.Code, w.Body.String())
			}
		})
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

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Handler Test Delete 403", slug, "DeleteWorkspace handler permission test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'admin')
`, wsID, testUserID); err != nil {
		t.Fatalf("create admin member: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testHandler.DeleteWorkspace(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 from DeleteWorkspace handler for admin (non-owner), got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
		t.Fatalf("verify workspace: %v", err)
	}
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

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Handler Test Delete OK", slug, "DeleteWorkspace handler owner test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testHandler.DeleteWorkspace(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from DeleteWorkspace handler for owner, got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
		t.Fatalf("verify workspace: %v", err)
	}
	if exists {
		t.Fatal("workspace still exists after owner DELETE")
	}
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

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description, issue_prefix)
VALUES ($1, $2, $3, $4)
RETURNING id
`, "Revocation "+slug, slug, "revocation test", "REV").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	// Requester (= testUserID) is always an owner so DeleteMember authorization
	// passes. Two owners total so LeaveWorkspace doesn't trip the "must keep
	// at least one owner" guard.
	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create requester member: %v", err)
	}

	targetEmail := fmt.Sprintf("revocation-%s@multica.ai", slug)
	var targetUserID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
`, "Revocation Target "+slug, targetEmail).Scan(&targetUserID); err != nil {
		t.Fatalf("create target user: %v", err)
	}

	// Cleanup ordering: workspace first (cascade clears agent_runtime,
	// agent, member, daemon_token), then user (whose deletion would
	// otherwise be blocked by agent.owner_id / agent_runtime.owner_id FKs).
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, targetUserID)
	})

	var memberID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner') RETURNING id
`, wsID, targetUserID).Scan(&memberID); err != nil {
		t.Fatalf("create target member: %v", err)
	}

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_runtime (
    workspace_id, daemon_id, name, runtime_mode, provider, status,
    device_info, metadata, owner_id, last_seen_at
)
VALUES ($1, $2, 'Target Runtime', 'local', 'multica_daemon', 'online', '', '{}'::jsonb, $3, now())
RETURNING id
`, wsID, daemonID, targetUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent (
    workspace_id, name, description, runtime_mode, runtime_config,
    runtime_id, visibility, max_concurrent_tasks, owner_id
)
VALUES ($1, 'Target Agent', '', 'local', '{}'::jsonb, $2, 'workspace', 1, $3)
RETURNING id
`, wsID, runtimeID, targetUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
VALUES ($1, $2, 'queued', 0)
RETURNING id
`, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	// daemon_token row — paired with the runtime's daemon_id so the
	// revocation should sweep its hash up via DeleteDaemonTokensByWorkspaceAndDaemons.
	rawToken := "mdt_test_" + slug
	sum := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(sum[:])
	if _, err := testPool.Exec(ctx, `
INSERT INTO daemon_token (token_hash, workspace_id, daemon_id, expires_at)
VALUES ($1, $2, $3, now() + interval '1 day')
`, tokenHash, wsID, daemonID); err != nil {
		t.Fatalf("insert daemon_token: %v", err)
	}

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
	ctx := context.Background()

	var memberExists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM member WHERE id = $1)`, fx.MemberID).Scan(&memberExists); err != nil {
		t.Fatalf("query member: %v", err)
	}
	if memberExists {
		t.Fatal("member row was not deleted")
	}

	var runtimeStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_runtime WHERE id = $1`, fx.RuntimeID).Scan(&runtimeStatus); err != nil {
		t.Fatalf("query runtime: %v", err)
	}
	if runtimeStatus != "offline" {
		t.Fatalf("expected runtime offline, got %q", runtimeStatus)
	}

	var archivedAt *string
	if err := testPool.QueryRow(ctx, `SELECT archived_at::text FROM agent WHERE id = $1`, fx.AgentID).Scan(&archivedAt); err != nil {
		t.Fatalf("query agent: %v", err)
	}
	if archivedAt == nil {
		t.Fatal("agent was not archived")
	}

	var taskStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, fx.TaskID).Scan(&taskStatus); err != nil {
		t.Fatalf("query task: %v", err)
	}
	if taskStatus != "cancelled" {
		t.Fatalf("expected task cancelled, got %q", taskStatus)
	}

	var tokenExists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM daemon_token WHERE token_hash = $1)`, fx.TokenHash).Scan(&tokenExists); err != nil {
		t.Fatalf("query daemon_token: %v", err)
	}
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

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/members/"+fx.MemberID, nil)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParams(req, "id", fx.WorkspaceID, "memberId", fx.MemberID)
	testHandler.DeleteMember(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteMember: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	assertRevoked(t, fx)
}

// TestLeaveWorkspace_RevokesOwnRuntimes is the self-removal counterpart: when
// a member leaves a workspace voluntarily, their own runtimes are revoked
// with the same atomic write set as DeleteMember.
func TestLeaveWorkspace_RevokesOwnRuntimes(t *testing.T) {
	fx := setupRevocationFixture(t, "handler-tests-revoke-leave", "daemon-revoke-leave")

	// Re-target the request from the leaving member's perspective: the
	// leaver is the request actor, not the workspace owner.
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/leave", nil)
	req.Header.Set("X-User-ID", fx.TargetUserID)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParam(req, "id", fx.WorkspaceID)
	testHandler.LeaveWorkspace(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("LeaveWorkspace: expected 204, got %d: %s", w.Code, w.Body.String())
	}

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
	ctx := context.Background()

	// Create a SECOND runtime in the workspace owned by the requester
	// (not the leaving member). The agent originally lived here.
	var otherRuntimeID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_runtime (
    workspace_id, daemon_id, name, runtime_mode, provider, status,
    device_info, metadata, owner_id, last_seen_at
)
VALUES ($1, $2, 'Other Runtime', 'local', 'multica_daemon', 'online', '', '{}'::jsonb, $3, now())
RETURNING id
`, fx.WorkspaceID, "daemon-revoke-reassign-other", testUserID).Scan(&otherRuntimeID); err != nil {
		t.Fatalf("insert other runtime: %v", err)
	}

	// Queue a task on the agent while it was still pinned to the OTHER
	// runtime (simulating a task created before the agent was reassigned
	// to the leaving member's runtime).
	var orphanTaskID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
VALUES ($1, $2, 'queued', 0)
RETURNING id
`, fx.AgentID, otherRuntimeID).Scan(&orphanTaskID); err != nil {
		t.Fatalf("insert orphan task: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/members/"+fx.MemberID, nil)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParams(req, "id", fx.WorkspaceID, "memberId", fx.MemberID)
	testHandler.DeleteMember(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteMember: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	assertRevoked(t, fx)

	// The orphan task — same agent, different runtime — must also be
	// cancelled. Without the by-agent leg in CancelAgentTasksByRuntimeOrAgent
	// this stays 'queued' and would be picked up by the other runtime.
	var orphanStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, orphanTaskID).Scan(&orphanStatus); err != nil {
		t.Fatalf("query orphan task: %v", err)
	}
	if orphanStatus != "cancelled" {
		t.Fatalf("expected orphan task cancelled (archived agent leftover on other runtime), got %q", orphanStatus)
	}

	// And the OTHER runtime — owned by an active member — must still be
	// online: revocation is scoped to the leaving member's owned runtimes.
	var otherStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_runtime WHERE id = $1`, otherRuntimeID).Scan(&otherStatus); err != nil {
		t.Fatalf("query other runtime: %v", err)
	}
	if otherStatus != "online" {
		t.Fatalf("expected other-member runtime to stay online, got %q", otherStatus)
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

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description, issue_prefix)
VALUES ($1, $2, $3, $4)
RETURNING id
`, "Revocation no runtimes", slug, "revocation no-runtimes test", "REV").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create requester member: %v", err)
	}

	var targetUserID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
`, "Revocation No Runtimes Target", "revocation-no-runtimes@multica.ai").Scan(&targetUserID); err != nil {
		t.Fatalf("create target user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, targetUserID)
	})

	var memberID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'admin') RETURNING id
`, wsID, targetUserID).Scan(&memberID); err != nil {
		t.Fatalf("create target member: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID+"/members/"+memberID, nil)
	req.Header.Set("X-Workspace-ID", wsID)
	req = withURLParams(req, "id", wsID, "memberId", memberID)
	testHandler.DeleteMember(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteMember: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var memberExists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM member WHERE id = $1)`, memberID).Scan(&memberExists); err != nil {
		t.Fatalf("query member: %v", err)
	}
	if memberExists {
		t.Fatal("member row was not deleted")
	}
}

// handlerTestAgentID returns the agent created by the shared fixture in
// setupHandlerTestFixture (handler_test.go). Tests that need an agent ID
// without creating their own should reach for this — duplicate fixtures
// have caused races against the shared workspace before.
func handlerTestAgentID(t *testing.T) string {
	t.Helper()
	var agentID string
	err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}
	return agentID
}

// resetWorkspaceSettings empties the test workspace's settings JSONB so a
// test can start from a known state. The default-unassigned-to feature is
// stateful per workspace, so leakage between tests would mask bugs.
func resetWorkspaceSettings(t *testing.T) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE workspace SET settings = '{}'::jsonb WHERE id = $1`,
		testWorkspaceID,
	); err != nil {
		t.Fatalf("reset workspace settings: %v", err)
	}
}

func TestUpdateWorkspaceSetting_RejectsUnknownKey(t *testing.T) {
	t.Cleanup(func() { resetWorkspaceSettings(t) })

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/workspaces/"+testWorkspaceID+"/settings", map[string]any{
		"key":   "totally_made_up",
		"value": "x",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.UpdateWorkspaceSetting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown key, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateWorkspaceSetting_RejectsInvalidAgentUUID(t *testing.T) {
	t.Cleanup(func() { resetWorkspaceSettings(t) })

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/workspaces/"+testWorkspaceID+"/settings", map[string]any{
		"key":   "default_unassigned_to",
		"value": "not-a-uuid",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.UpdateWorkspaceSetting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed uuid, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateWorkspaceSetting_RejectsNonexistentAgent(t *testing.T) {
	t.Cleanup(func() { resetWorkspaceSettings(t) })

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/workspaces/"+testWorkspaceID+"/settings", map[string]any{
		"key":   "default_unassigned_to",
		"value": "00000000-0000-0000-0000-000000000000",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.UpdateWorkspaceSetting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing agent, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateWorkspaceSetting_PersistsAndClearsAgent(t *testing.T) {
	t.Cleanup(func() { resetWorkspaceSettings(t) })
	agentID := handlerTestAgentID(t)

	// Set the value.
	wSet := httptest.NewRecorder()
	reqSet := newRequest("PATCH", "/api/workspaces/"+testWorkspaceID+"/settings", map[string]any{
		"key":   "default_unassigned_to",
		"value": agentID,
	})
	reqSet = withURLParam(reqSet, "id", testWorkspaceID)
	testHandler.UpdateWorkspaceSetting(wSet, reqSet)
	if wSet.Code != http.StatusOK {
		t.Fatalf("PATCH settings: expected 200, got %d: %s", wSet.Code, wSet.Body.String())
	}

	// Verify persisted in DB.
	var raw []byte
	if err := testPool.QueryRow(context.Background(),
		`SELECT settings FROM workspace WHERE id = $1`,
		testWorkspaceID,
	).Scan(&raw); err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	if stored["default_unassigned_to"] != agentID {
		t.Fatalf("expected default_unassigned_to=%q, got %v", agentID, stored)
	}

	// Clear with explicit null.
	wClear := httptest.NewRecorder()
	reqClear := newRequest("PATCH", "/api/workspaces/"+testWorkspaceID+"/settings", map[string]any{
		"key":   "default_unassigned_to",
		"value": nil,
	})
	reqClear = withURLParam(reqClear, "id", testWorkspaceID)
	testHandler.UpdateWorkspaceSetting(wClear, reqClear)
	if wClear.Code != http.StatusOK {
		t.Fatalf("PATCH settings (clear): expected 200, got %d: %s", wClear.Code, wClear.Body.String())
	}

	if err := testPool.QueryRow(context.Background(),
		`SELECT settings FROM workspace WHERE id = $1`,
		testWorkspaceID,
	).Scan(&raw); err != nil {
		t.Fatalf("read settings post-clear: %v", err)
	}
	var stored2 map[string]any
	if err := json.Unmarshal(raw, &stored2); err != nil {
		t.Fatalf("unmarshal settings post-clear: %v", err)
	}
	if _, exists := stored2["default_unassigned_to"]; exists {
		t.Fatalf("expected key removed, still present: %v", stored2)
	}
}

func TestUpdateWorkspaceSetting_RejectsArchivedAgent(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() { resetWorkspaceSettings(t) })

	// Spin up a dedicated archived agent so we don't poison the shared
	// fixture's "Handler Test Agent" used by every other test.
	var archivedAgentID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id, archived_at)
VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4, now())
RETURNING id
`, testWorkspaceID, "Handler Archived Default Assignee", testRuntimeID, testUserID).Scan(&archivedAgentID); err != nil {
		t.Fatalf("create archived agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, archivedAgentID)
	})

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/workspaces/"+testWorkspaceID+"/settings", map[string]any{
		"key":   "default_unassigned_to",
		"value": archivedAgentID,
	})
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.UpdateWorkspaceSetting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for archived agent, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateIssue_DefaultAssigneeAppliedWhenMissing covers acceptance #3:
// when the workspace setting is configured and the request itself omits an
// assignee, the issue is created with assignee = configured agent.
func TestCreateIssue_DefaultAssigneeAppliedWhenMissing(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() { resetWorkspaceSettings(t) })
	agentID := handlerTestAgentID(t)

	if _, err := testPool.Exec(ctx,
		`UPDATE workspace SET settings = jsonb_build_object('default_unassigned_to', $2::text) WHERE id = $1`,
		testWorkspaceID, agentID,
	); err != nil {
		t.Fatalf("seed default_unassigned_to: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Default-assignee feature: no assignee in request",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
	})

	if created.AssigneeType == nil || *created.AssigneeType != "agent" {
		t.Fatalf("expected assignee_type=agent, got %v", created.AssigneeType)
	}
	if created.AssigneeID == nil || *created.AssigneeID != agentID {
		t.Fatalf("expected assignee_id=%s, got %v", agentID, created.AssigneeID)
	}
}

// TestCreateIssue_ExplicitAssigneeBeatsDefault covers the contract that an
// explicit assignee in the request must always win. Without this guard, the
// auto-fill could silently rewrite a member assignment to an agent.
func TestCreateIssue_ExplicitAssigneeBeatsDefault(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() { resetWorkspaceSettings(t) })
	agentID := handlerTestAgentID(t)

	if _, err := testPool.Exec(ctx,
		`UPDATE workspace SET settings = jsonb_build_object('default_unassigned_to', $2::text) WHERE id = $1`,
		testWorkspaceID, agentID,
	); err != nil {
		t.Fatalf("seed default_unassigned_to: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Default-assignee feature: explicit assignee wins",
		"assignee_type": "member",
		"assignee_id":   testUserID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
	})

	if created.AssigneeType == nil || *created.AssigneeType != "member" {
		t.Fatalf("expected assignee_type=member, got %v", created.AssigneeType)
	}
	if created.AssigneeID == nil || *created.AssigneeID != testUserID {
		t.Fatalf("expected assignee_id=%s, got %v", testUserID, created.AssigneeID)
	}
}

// TestCreateIssue_StaleDefaultAssigneeFallsBackUnassigned covers the soft
// fallback property: if the configured agent is archived (or otherwise
// invisible) by the time an issue is created, the issue is created
// unassigned rather than 4xx-failing the caller. Acceptance #5.
func TestCreateIssue_StaleDefaultAssigneeFallsBackUnassigned(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() { resetWorkspaceSettings(t) })

	// Create an agent that exists at config-time, then archive it before the
	// issue create — simulating the "admin set it, agent later went away"
	// scenario. The PATCH validator would have rejected an archived agent at
	// write time; this test exercises only the read-path soft fallback.
	var staleAgentID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
RETURNING id
`, testWorkspaceID, "Handler Stale Default Assignee", testRuntimeID, testUserID).Scan(&staleAgentID); err != nil {
		t.Fatalf("create stale agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, staleAgentID)
	})

	if _, err := testPool.Exec(ctx,
		`UPDATE workspace SET settings = jsonb_build_object('default_unassigned_to', $2::text) WHERE id = $1`,
		testWorkspaceID, staleAgentID,
	); err != nil {
		t.Fatalf("seed default_unassigned_to: %v", err)
	}

	if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, staleAgentID); err != nil {
		t.Fatalf("archive agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Default-assignee feature: stale config falls back",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201 (soft fallback, not 4xx), got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
	})

	if created.AssigneeType != nil || created.AssigneeID != nil {
		t.Fatalf("expected unassigned issue from stale config, got type=%v id=%v", created.AssigneeType, created.AssigneeID)
	}
}

// TestCreateIssue_NoSettingLeavesUnassigned is the regression guard for
// acceptance #5 ("留空时退回未指派"): the legacy unassigned path must keep
// working when no setting is configured. Without this, an over-eager
// auto-fill could surface a default that the user never asked for.
func TestCreateIssue_NoSettingLeavesUnassigned(t *testing.T) {
	t.Cleanup(func() { resetWorkspaceSettings(t) })
	resetWorkspaceSettings(t) // make doubly sure we start from {}.

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Default-assignee feature: no setting, no assignee",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
	})

	if created.AssigneeType != nil || created.AssigneeID != nil {
		t.Fatalf("expected unassigned, got type=%v id=%v", created.AssigneeType, created.AssigneeID)
	}
}
