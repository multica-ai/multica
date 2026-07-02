package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

// createPermissionTestMember inserts a fresh workspace member and returns its
// user id, registering cleanup.
func createPermissionTestMember(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, email, email).Scan(&userID); err != nil {
		t.Fatalf("create member user %s: %v", email, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("add member %s: %v", email, err)
	}
	return userID
}

// TestCreateAgent_LegacyVisibilityMapsToPermission verifies the lossless
// legacy-visibility mapping (MUL-3963) at the API layer — the same mapping the
// migration backfill applies to existing rows:
//   - visibility "workspace" -> permission_mode public_to + a workspace target
//   - visibility "private"   -> permission_mode private + no targets
func TestCreateAgent_LegacyVisibilityMapsToPermission(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	runtimeID := handlerTestRuntimeID(t)

	create := func(name, visibility string) AgentResponse {
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
			"name":       name,
			"runtime_id": runtimeID,
			"visibility": visibility,
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("create %q (visibility=%s): expected 201, got %d: %s", name, visibility, w.Code, w.Body.String())
		}
		var resp AgentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, resp.ID) })
		return resp
	}

	ws := create("legacy-visibility-workspace", "workspace")
	if ws.PermissionMode != "public_to" {
		t.Errorf("workspace agent permission_mode = %q, want public_to", ws.PermissionMode)
	}
	if ws.Visibility != "workspace" {
		t.Errorf("workspace agent derived visibility = %q, want workspace", ws.Visibility)
	}
	foundWorkspaceTarget := false
	for _, tgt := range ws.InvocationTargets {
		if tgt.TargetType == "workspace" {
			foundWorkspaceTarget = true
		}
	}
	if !foundWorkspaceTarget {
		t.Errorf("workspace agent invocation_targets = %+v, want a workspace target", ws.InvocationTargets)
	}

	priv := create("legacy-visibility-private", "private")
	if priv.PermissionMode != "private" {
		t.Errorf("private agent permission_mode = %q, want private", priv.PermissionMode)
	}
	if priv.Visibility != "private" {
		t.Errorf("private agent derived visibility = %q, want private", priv.Visibility)
	}
	if len(priv.InvocationTargets) != 0 {
		t.Errorf("private agent invocation_targets = %+v, want none", priv.InvocationTargets)
	}
}

// TestMigrationBackfill_VisibilityToPermission exercises the exact backfill
// statements from migration 130 against a synthetic pre-migration row
// (visibility='workspace' but permission_mode='private', no target). The
// statements are idempotent, so re-running them on the live (already-migrated)
// DB only affects the synthetic row.
func TestMigrationBackfill_VisibilityToPermission(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'backfill-legacy-workspace-agent', '', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 'private', 1, $3)
		RETURNING id
	`, testWorkspaceID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert pre-migration agent: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID) })

	// Exact backfill statements from migration 130 (idempotent).
	if _, err := testPool.Exec(ctx, `UPDATE agent SET permission_mode = 'public_to' WHERE visibility = 'workspace'`); err != nil {
		t.Fatalf("backfill update: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id, created_by)
		SELECT id, 'workspace', workspace_id, NULL FROM agent WHERE visibility = 'workspace'
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`); err != nil {
		t.Fatalf("backfill insert targets: %v", err)
	}

	var mode string
	if err := testPool.QueryRow(ctx, `SELECT permission_mode FROM agent WHERE id = $1`, agentID).Scan(&mode); err != nil {
		t.Fatalf("read permission_mode: %v", err)
	}
	if mode != "public_to" {
		t.Errorf("after backfill permission_mode = %q, want public_to", mode)
	}
	var targetCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_invocation_target
		WHERE agent_id = $1 AND target_type = 'workspace' AND target_id = $2
	`, agentID, testWorkspaceID).Scan(&targetCount); err != nil {
		t.Fatalf("count targets: %v", err)
	}
	if targetCount != 1 {
		t.Errorf("workspace target count = %d, want 1", targetCount)
	}
}

// TestCanInvokeAgent_PublicToMemberWhitelist verifies that a public_to agent
// restricted to a specific member is invocable (assignable) only by that
// member — not by other plain members, and not by workspace admins who are not
// on the list (MUL-3963).
func TestCanInvokeAgent_PublicToMemberWhitelist(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	runtimeID := handlerTestRuntimeID(t)

	allowedMember := createPermissionTestMember(t, "perm-allowed-member@multica.test")
	otherMember := createPermissionTestMember(t, "perm-other-member@multica.test")

	// Owner (testUserID) creates an agent public_to the allowed member only.
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":            "public-to-specific-member-agent",
		"runtime_id":      runtimeID,
		"permission_mode": "public_to",
		"invocation_targets": []map[string]any{
			{"target_type": "member", "target_id": allowedMember},
		},
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create agent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var agent AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&agent); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agent.ID) })

	// Derived legacy visibility for a member-only public_to agent must be
	// "private" so old clients never treat it as workspace-wide.
	if agent.Visibility != "private" {
		t.Errorf("member-only public_to derived visibility = %q, want private", agent.Visibility)
	}

	assignAs := func(actorID string) int {
		rec := httptest.NewRecorder()
		testHandler.CreateIssue(rec, newRequestAs(actorID, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":         "assign to member-scoped agent",
			"status":        "todo",
			"assignee_type": "agent",
			"assignee_id":   agent.ID,
		}))
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agent.ID)
			testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1 AND title = 'assign to member-scoped agent'`, testWorkspaceID)
		})
		return rec.Code
	}

	if code := assignAs(allowedMember); code != http.StatusCreated {
		t.Errorf("allow-listed member assign: expected 201, got %d", code)
	}
	if code := assignAs(otherMember); code != http.StatusForbidden {
		t.Errorf("non-allow-listed member assign: expected 403, got %d", code)
	}
}

// --- MUL-3963 follow-up: stackable / mixed / batch-replaced targets -------

// createPublicToAgentWithTargets creates a public_to agent (owned by
// testUserID) with the given invocation targets via the CreateAgent handler
// and returns its id.
func createPublicToAgentWithTargets(t *testing.T, name string, targets []map[string]any) string {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":               name,
		"runtime_id":         handlerTestRuntimeID(t),
		"permission_mode":    "public_to",
		"invocation_targets": targets,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create %q: expected 201, got %d: %s", name, w.Code, w.Body.String())
	}
	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, resp.ID) })
	return resp.ID
}

// canMemberInvoke loads the agent fresh and asks canInvokeAgent whether the
// given member may invoke it (member is their own originator).
func canMemberInvoke(t *testing.T, agentID, userID string) bool {
	t.Helper()
	agent, err := testHandler.Queries.GetAgent(context.Background(), util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	return testHandler.canInvokeAgent(context.Background(), agent, "member", userID, userID, testWorkspaceID)
}

func invocationTargetCount(t *testing.T, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_invocation_target WHERE agent_id = $1`, agentID,
	).Scan(&n); err != nil {
		t.Fatalf("count targets: %v", err)
	}
	return n
}

// TestCanInvokeAgent_MixedMemberAndTeamTargets verifies a public_to agent can
// carry a MIX of target types on one agent, and canInvokeAgent OR-matches: a
// member target admits that member; a team target is inert in v1 so it admits
// nobody, but co-existing with the member target does not break the member
// grant.
func TestCanInvokeAgent_MixedMemberAndTeamTargets(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberA := createPermissionTestMember(t, "perm-mix-a@multica.test")
	memberB := createPermissionTestMember(t, "perm-mix-b@multica.test")
	teamID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" // no team table FK in v1

	agentID := createPublicToAgentWithTargets(t, "mixed-member-team-agent", []map[string]any{
		{"target_type": "member", "target_id": memberA},
		{"target_type": "team", "target_id": teamID},
	})

	if n := invocationTargetCount(t, agentID); n != 2 {
		t.Errorf("expected 2 mixed targets persisted, got %d", n)
	}
	if !canMemberInvoke(t, agentID, memberA) {
		t.Errorf("member A (on member target) should be able to invoke")
	}
	if canMemberInvoke(t, agentID, memberB) {
		t.Errorf("member B should NOT invoke — only a (inert) team target applies to them")
	}
}

// TestUpdateAgent_BatchReplaceOverlappingMembers verifies the create/update
// path replaces the WHOLE allow-list (not one row) and that overlapping
// members survive the replace while removed ones lose access.
func TestUpdateAgent_BatchReplaceOverlappingMembers(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberA := createPermissionTestMember(t, "perm-batch-a@multica.test")
	memberB := createPermissionTestMember(t, "perm-batch-b@multica.test")
	memberC := createPermissionTestMember(t, "perm-batch-c@multica.test")

	agentID := createPublicToAgentWithTargets(t, "batch-replace-agent", []map[string]any{
		{"target_type": "member", "target_id": memberA},
		{"target_type": "member", "target_id": memberB},
	})
	if !canMemberInvoke(t, agentID, memberA) || !canMemberInvoke(t, agentID, memberB) {
		t.Fatalf("initial: A and B should both invoke")
	}
	if canMemberInvoke(t, agentID, memberC) {
		t.Fatalf("initial: C should not invoke")
	}

	// Batch-replace the allow-list with an overlapping set: drop A, keep B, add C.
	w := httptest.NewRecorder()
	r := newRequest("PUT", "/api/agents/"+agentID, map[string]any{
		"permission_mode": "public_to",
		"invocation_targets": []map[string]any{
			{"target_type": "member", "target_id": memberB},
			{"target_type": "member", "target_id": memberC},
		},
	})
	r = withURLParam(r, "id", agentID)
	testHandler.UpdateAgent(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := invocationTargetCount(t, agentID); n != 2 {
		t.Errorf("after batch replace expected exactly 2 targets, got %d (stale rows not cleared?)", n)
	}
	if canMemberInvoke(t, agentID, memberA) {
		t.Errorf("A was removed and must no longer invoke")
	}
	if !canMemberInvoke(t, agentID, memberB) {
		t.Errorf("B overlapped both sets and must still invoke")
	}
	if !canMemberInvoke(t, agentID, memberC) {
		t.Errorf("C was added and must now invoke")
	}
}

// TestUpdateAgent_WorkspaceStacksWithMembersThenNarrowed verifies workspace and
// member targets stack (workspace admits any member via OR), and that a
// subsequent batch replace to a member-only set narrows access correctly.
func TestUpdateAgent_WorkspaceStacksWithMembersThenNarrowed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberA := createPermissionTestMember(t, "perm-stack-a@multica.test")
	memberB := createPermissionTestMember(t, "perm-stack-b@multica.test")

	agentID := createPublicToAgentWithTargets(t, "workspace-plus-member-agent", []map[string]any{
		{"target_type": "workspace"},
		{"target_type": "member", "target_id": memberA},
	})
	// Workspace target admits ANY workspace member via OR, including B who is
	// not on an explicit member target.
	if !canMemberInvoke(t, agentID, memberA) || !canMemberInvoke(t, agentID, memberB) {
		t.Fatalf("workspace target should admit any member (A and B)")
	}

	// Narrow to member C only — workspace grant is dropped by the replace.
	memberC := createPermissionTestMember(t, "perm-stack-c@multica.test")
	w := httptest.NewRecorder()
	r := newRequest("PUT", "/api/agents/"+agentID, map[string]any{
		"permission_mode": "public_to",
		"invocation_targets": []map[string]any{
			{"target_type": "member", "target_id": memberC},
		},
	})
	r = withURLParam(r, "id", agentID)
	testHandler.UpdateAgent(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("narrow update: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if canMemberInvoke(t, agentID, memberB) {
		t.Errorf("after dropping workspace target, non-listed member B must lose access")
	}
	if !canMemberInvoke(t, agentID, memberC) {
		t.Errorf("member C must have access after the replace")
	}
}

// TestCreateAgent_EmptyPublicToNormalizesToWorkspace locks the MUL-3963 review
// ruling: a public_to agent with no invocation targets is a phantom, so the
// backend normalises it to a single workspace target (and therefore derived
// visibility "workspace"). This also covers `--permission-mode public_to`
// alone from the CLI, which sends permission_mode without targets.
func TestCreateAgent_EmptyPublicToNormalizesToWorkspace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":            "empty-public-to-agent",
		"runtime_id":      handlerTestRuntimeID(t),
		"permission_mode": "public_to",
		// no invocation_targets on purpose
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, resp.ID) })

	if resp.PermissionMode != "public_to" {
		t.Errorf("permission_mode = %q, want public_to", resp.PermissionMode)
	}
	if resp.Visibility != "workspace" {
		t.Errorf("empty public_to should derive visibility=workspace, got %q", resp.Visibility)
	}
	foundWorkspace := false
	for _, tgt := range resp.InvocationTargets {
		if tgt.TargetType == "workspace" {
			foundWorkspace = true
		}
	}
	if !foundWorkspace {
		t.Errorf("empty public_to must normalise to a workspace target, got %+v", resp.InvocationTargets)
	}
	// And any workspace member can then invoke it.
	someMember := createPermissionTestMember(t, "perm-emptypublic-m@multica.test")
	if !canMemberInvoke(t, resp.ID, someMember) {
		t.Errorf("a workspace member should be able to invoke the normalised public_to-workspace agent")
	}
}

// TestCanInvokeAgent_SystemWorkspaceExceptionAndMemberFailClosed locks the
// product-approved exception (MUL-3963): a system / no-human-originator trigger
// MAY hit a workspace target (webhook / workspace-wide automation), but MUST
// fail closed against a member/team target when no originator resolves.
func TestCanInvokeAgent_SystemWorkspaceExceptionAndMemberFailClosed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// public_to workspace agent → system trigger (no originator) is admitted.
	wsAgentID := createPublicToAgentWithTargets(t, "sys-exception-workspace-agent", []map[string]any{
		{"target_type": "workspace"},
	})
	wsAgent, err := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(wsAgentID))
	if err != nil {
		t.Fatalf("load ws agent: %v", err)
	}
	if !testHandler.canInvokeAgent(ctx, wsAgent, "system", "", "", testWorkspaceID) {
		t.Errorf("system trigger should hit a workspace target (product-approved exception)")
	}
	if !testHandler.canInvokeAgent(ctx, wsAgent, "agent", "", "", testWorkspaceID) {
		t.Errorf("agent trigger with no originator should still hit a workspace target")
	}

	// public_to member-only agent → system / unattributed agent trigger must
	// fail closed (member target requires a resolved human originator).
	memberX := createPermissionTestMember(t, "perm-sys-failclosed@multica.test")
	memAgentID := createPublicToAgentWithTargets(t, "sys-exception-member-agent", []map[string]any{
		{"target_type": "member", "target_id": memberX},
	})
	memAgent, err := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(memAgentID))
	if err != nil {
		t.Fatalf("load member agent: %v", err)
	}
	if testHandler.canInvokeAgent(ctx, memAgent, "system", "", "", testWorkspaceID) {
		t.Errorf("system trigger must FAIL CLOSED against a member target with no originator")
	}
	if testHandler.canInvokeAgent(ctx, memAgent, "agent", "", "", testWorkspaceID) {
		t.Errorf("agent trigger with no originator must FAIL CLOSED against a member target")
	}
	// But the actual member (as originator) is admitted.
	if !testHandler.canInvokeAgent(ctx, memAgent, "agent", "", memberX, testWorkspaceID) {
		t.Errorf("agent trigger whose originator IS the targeted member should be admitted")
	}
}

// TestRevokeMember_ClearsInvocationTargets is the MUL-3963 review regression:
// removing a member must prune their member-target invocation grants (there is
// no DB FK), and a re-invited user must NOT silently reclaim the old grant.
func TestRevokeMember_ClearsInvocationTargets(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	memberX := createPermissionTestMember(t, "perm-revoke-x@multica.test")
	agentID := createPublicToAgentWithTargets(t, "revoke-member-target-agent", []map[string]any{
		{"target_type": "member", "target_id": memberX},
	})
	if !canMemberInvoke(t, agentID, memberX) {
		t.Fatalf("member X should be able to invoke before removal")
	}

	// Resolve the member row id for the revoke call.
	var memberRowID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`,
		testWorkspaceID, memberX,
	).Scan(&memberRowID); err != nil {
		t.Fatalf("load member row: %v", err)
	}

	if _, err := testHandler.revokeAndRemoveMember(ctx,
		util.MustParseUUID(testWorkspaceID),
		util.MustParseUUID(memberX),
		util.MustParseUUID(memberRowID),
		util.MustParseUUID(testUserID),
	); err != nil {
		t.Fatalf("revokeAndRemoveMember: %v", err)
	}

	// The member-target grant must be gone.
	if n := invocationTargetCount(t, agentID); n != 0 {
		t.Errorf("member target should be pruned on removal, still have %d", n)
	}
	if canMemberInvoke(t, agentID, memberX) {
		t.Errorf("removed member must no longer invoke the agent")
	}

	// Re-invite the same user: they must NOT reclaim the old grant.
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, memberX,
	); err != nil {
		t.Fatalf("re-invite member: %v", err)
	}
	if canMemberInvoke(t, agentID, memberX) {
		t.Errorf("re-invited member must NOT reclaim the stale invocation grant")
	}
}

// TestRevokeMember_InvocationTargetCleanupIsWorkspaceScoped locks the 3rd-review
// fix (MUL-3963): removing a user from ONE workspace must only prune their
// member-target invocation grants in THAT workspace. A user who belongs to
// multiple workspaces must keep their grants in the workspaces they remain in.
func TestRevokeMember_InvocationTargetCleanupIsWorkspaceScoped(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	userX := createPermissionTestMember(t, "perm-xws@multica.test")

	// Workspace A (the shared test workspace): an agent allow-lists userX.
	agentA := createPublicToAgentWithTargets(t, "xws-agent-a", []map[string]any{
		{"target_type": "member", "target_id": userX},
	})

	// Workspace B: fresh workspace with its own runtime + agent that also
	// allow-lists the same userX.
	testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = 'xws-b-perm-test'`)
	var wsB string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('XWS B', 'xws-b-perm-test', '', 'XWB')
		RETURNING id
	`).Scan(&wsB); err != nil {
		t.Fatalf("create workspace B: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsB) })

	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, wsB, testUserID); err != nil {
		t.Fatalf("add owner to B: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, wsB, userX); err != nil {
		t.Fatalf("add userX to B: %v", err)
	}
	var rtB string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, 'xws-b-rt', 'cloud', 'handler_test_runtime', 'online', 'dev', '{}'::jsonb, $2, now())
		RETURNING id
	`, wsB, testUserID).Scan(&rtB); err != nil {
		t.Fatalf("create runtime B: %v", err)
	}
	var agentB string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'xws-agent-b', '', 'cloud', '{}'::jsonb, $2, 'private', 'public_to', 1, $3)
		RETURNING id
	`, wsB, rtB, testUserID).Scan(&agentB); err != nil {
		t.Fatalf("create agent B: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id) VALUES ($1, 'member', $2)
	`, agentB, userX); err != nil {
		t.Fatalf("seed B member target: %v", err)
	}

	if invocationTargetCount(t, agentA) != 1 || invocationTargetCount(t, agentB) != 1 {
		t.Fatalf("setup: expected one member target on each of A and B")
	}

	// Remove userX from workspace A only.
	var memberRowA string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userX,
	).Scan(&memberRowA); err != nil {
		t.Fatalf("load member row A: %v", err)
	}
	if _, err := testHandler.revokeAndRemoveMember(ctx,
		util.MustParseUUID(testWorkspaceID),
		util.MustParseUUID(userX),
		util.MustParseUUID(memberRowA),
		util.MustParseUUID(testUserID),
	); err != nil {
		t.Fatalf("revokeAndRemoveMember(A): %v", err)
	}

	if n := invocationTargetCount(t, agentA); n != 0 {
		t.Errorf("workspace A target should be pruned on removal, got %d", n)
	}
	if n := invocationTargetCount(t, agentB); n != 1 {
		t.Errorf("workspace B target MUST survive removal from A (cross-workspace collateral), got %d", n)
	}
}

// createPermissionTestAdmin inserts a fresh workspace member with the admin
// role and returns its user id.
func createPermissionTestAdmin(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`, email, email).Scan(&userID); err != nil {
		t.Fatalf("create admin user %s: %v", email, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'admin')`, testWorkspaceID, userID); err != nil {
		t.Fatalf("add admin %s: %v", email, err)
	}
	return userID
}

// TestUpdateAgent_AccessChangeIsOwnerOnly locks the interaction-bug fix
// (separate PR): a workspace ADMIN who is NOT the agent owner may edit other
// agent fields but must NOT change access — a real permission change returns an
// explicit 403 (no more silent "bounce back"), while a no-op resubmit and
// edits to other fields still succeed. The agent owner can change access.
func TestUpdateAgent_AccessChangeIsOwnerOnly(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Agent owned by testUserID, public_to workspace (createHandlerTestAgent).
	agentID := createHandlerTestAgent(t, "owner-only-access-agent", nil)
	adminID := createPermissionTestAdmin(t, "perm-access-admin@multica.test")

	put := func(actorID string, body map[string]any) int {
		rec := httptest.NewRecorder()
		r := newRequestAs(actorID, "PUT", "/api/agents/"+agentID, body)
		r = withURLParam(r, "id", agentID)
		testHandler.UpdateAgent(rec, r)
		return rec.Code
	}

	// Admin (non-owner) attempts a REAL access change → 403.
	rec := httptest.NewRecorder()
	r := newRequestAs(adminID, "PUT", "/api/agents/"+agentID, map[string]any{"permission_mode": "private"})
	r = withURLParam(r, "id", agentID)
	testHandler.UpdateAgent(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin access change: expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	// Access must be unchanged (still public_to workspace).
	if a, _ := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(agentID)); a.PermissionMode != "public_to" {
		t.Errorf("access must be unchanged after rejected admin write, got %q", a.PermissionMode)
	}

	// Admin no-op resubmit of the CURRENT permission (PATCH-as-PUT) → tolerated.
	if code := put(adminID, map[string]any{
		"permission_mode":    "public_to",
		"invocation_targets": []map[string]any{{"target_type": "workspace"}},
	}); code != http.StatusOK {
		t.Errorf("admin no-op permission resubmit: expected 200, got %d", code)
	}

	// Admin editing a NON-permission field still works.
	if code := put(adminID, map[string]any{"description": "renamed by admin"}); code != http.StatusOK {
		t.Errorf("admin editing other fields: expected 200, got %d", code)
	}

	// The owner CAN change access.
	if code := put(testUserID, map[string]any{"permission_mode": "private"}); code != http.StatusOK {
		t.Errorf("owner access change: expected 200, got %d", code)
	}
	if n := invocationTargetCount(t, agentID); n != 0 {
		t.Errorf("owner set private: expected 0 targets, got %d", n)
	}
}

// TestUpdateAgent_LegacyVisibilityNoOpForMemberOnlyPublicTo locks the PR #4853
// compatibility fix: a member-only public_to agent DERIVES legacy visibility
// "private", so an admin (non-owner) echoing visibility:"private" via an old
// client / PATCH-as-PUT while editing another field must be treated as a NO-OP
// (200, targets unchanged) — not misread as a public_to→private downgrade
// (403). Submitting visibility:"workspace" is a real change and still 403.
func TestUpdateAgent_LegacyVisibilityNoOpForMemberOnlyPublicTo(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	memberX := createPermissionTestMember(t, "perm-legacyvis-x@multica.test")
	agentID := createPublicToAgentWithTargets(t, "legacy-vis-member-only-agent", []map[string]any{
		{"target_type": "member", "target_id": memberX},
	})
	adminID := createPermissionTestAdmin(t, "perm-legacyvis-admin@multica.test")

	put := func(actorID string, body map[string]any) int {
		rec := httptest.NewRecorder()
		r := newRequestAs(actorID, "PUT", "/api/agents/"+agentID, body)
		r = withURLParam(r, "id", agentID)
		testHandler.UpdateAgent(rec, r)
		return rec.Code
	}

	// Derived legacy visibility of a member-only public_to agent is "private".
	// Admin echoing that back while editing description → 200 no-op.
	if code := put(adminID, map[string]any{"visibility": "private", "description": "admin note"}); code != http.StatusOK {
		t.Fatalf("admin legacy visibility=private no-op: expected 200, got %d", code)
	}
	// Access must be untouched: still public_to with the one member target.
	if a, _ := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(agentID)); a.PermissionMode != "public_to" {
		t.Errorf("permission_mode must stay public_to after legacy no-op, got %q", a.PermissionMode)
	}
	if n := invocationTargetCount(t, agentID); n != 1 {
		t.Errorf("member target must be intact after legacy no-op, got %d targets", n)
	}

	// Admin submitting a REAL legacy change (workspace) is still rejected.
	if code := put(adminID, map[string]any{"visibility": "workspace"}); code != http.StatusForbidden {
		t.Errorf("admin legacy visibility=workspace (real change): expected 403, got %d", code)
	}
}
