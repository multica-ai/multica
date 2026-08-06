package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/auth"
)

const crossWorkspaceTokenPath = "/api/tokens/cross-workspace"

// crossWorkspaceTokenRequest builds a request shaped like the real
// task_token-authenticated caller: X-Actor-Source/X-Agent-ID/X-Task-ID/
// X-Workspace-ID/X-User-ID are exactly what the Auth middleware stamps for a
// mat_ token, and MintCrossWorkspaceToken trusts them as-is (it never reads
// the request through resolveActor).
func crossWorkspaceTokenRequest(agentID, taskID, originWorkspaceID, userID, targetWorkspaceID string) *http.Request {
	var body strings.Reader
	if targetWorkspaceID != "" {
		body = *strings.NewReader(`{"workspace_id":"` + targetWorkspaceID + `"}`)
	} else {
		body = *strings.NewReader(`{}`)
	}
	req := httptest.NewRequest(http.MethodPost, crossWorkspaceTokenPath, &body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	req.Header.Set("X-Workspace-ID", originWorkspaceID)
	req.Header.Set("X-User-ID", userID)
	return req
}

// createHandlerTestTask inserts a minimal queued task for agentID in
// testWorkspaceID's runtime, for tests that need a real agent_task_queue row
// to satisfy task_token's task_id foreign key.
func createHandlerTestTask(t *testing.T, agentID string) string {
	t.Helper()
	ctx := context.Background()

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
		VALUES ($1, $2, 'running', 0)
		RETURNING id
	`, agentID, handlerTestRuntimeID(t)).Scan(&taskID); err != nil {
		t.Fatalf("create handler test task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

// TestMintCrossWorkspaceToken_RequiresTaskTokenActor pins the strictest
// layer of the three-layer gate (BUS-171): a caller without the
// server-stamped X-Actor-Source: task_token header — a human PAT/JWT
// session, or the resolveActor legacy-header fallback a compromised agent
// process might try to forge — is rejected before any DB lookup runs.
func TestMintCrossWorkspaceToken_RequiresTaskTokenActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	req := httptest.NewRequest(http.MethodPost, crossWorkspaceTokenPath, strings.NewReader(`{"workspace_id":"`+uuid.NewString()+`"}`))
	req.Header.Set("Content-Type", "application/json")
	// Forged legacy-fallback headers, but deliberately no X-Actor-Source.
	req.Header.Set("X-Agent-ID", uuid.NewString())
	req.Header.Set("X-Task-ID", uuid.NewString())
	req.Header.Set("X-User-ID", testUserID)

	w := httptest.NewRecorder()
	testHandler.MintCrossWorkspaceToken(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

// TestMintCrossWorkspaceToken_RejectsSameWorkspace pins that the target
// workspace_id must differ from the task's own origin workspace — minting a
// "cross-workspace" token into the workspace the task is already scoped to
// would just be a confusing no-op path around the normal token.
func TestMintCrossWorkspaceToken_RejectsSameWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	req := crossWorkspaceTokenRequest(uuid.NewString(), uuid.NewString(), testWorkspaceID, testUserID, testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.MintCrossWorkspaceToken(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

// TestMintCrossWorkspaceToken_RejectsUngrantedWorkspace pins gate #2: even a
// real agent with an empty (default) cross_workspace_ids allow-list cannot
// mint a token into an arbitrary other workspace. No real task row is needed
// here — the grant check runs, and fails, before task_token is ever created.
func TestMintCrossWorkspaceToken_RejectsUngrantedWorkspace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "cwt-ungranted-agent", []byte("{}"))
	targetWS := createForeignWorkspaceForCrossWorkspaceTest(t, "cwt-ungranted-"+uuid.NewString()[:8])

	req := crossWorkspaceTokenRequest(agentID, uuid.NewString(), testWorkspaceID, testUserID, targetWS)
	w := httptest.NewRecorder()
	testHandler.MintCrossWorkspaceToken(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

// TestMintCrossWorkspaceToken_RejectsNonMemberTaskOwner pins gate #3
// (defense-in-depth): even when the agent IS granted access to the target
// workspace, the task's owning user must actually be a member of it. A
// stale or misconfigured grant must never mint a usable token.
func TestMintCrossWorkspaceToken_RejectsNonMemberTaskOwner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "cwt-nonmember-agent", []byte("{}"))
	targetWS := createForeignWorkspaceForCrossWorkspaceTest(t, "cwt-nonmember-"+uuid.NewString()[:8])
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent SET cross_workspace_ids = $1::uuid[] WHERE id = $2
	`, []string{targetWS}, agentID); err != nil {
		t.Fatalf("grant cross-workspace access: %v", err)
	}

	// testUserID is not a member of targetWS.
	req := crossWorkspaceTokenRequest(agentID, uuid.NewString(), testWorkspaceID, testUserID, targetWS)
	w := httptest.NewRecorder()
	testHandler.MintCrossWorkspaceToken(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

// TestMintCrossWorkspaceToken_Success pins the full happy path: a granted
// agent, whose task owner is a member of the target workspace, mints a
// mat_-prefixed token scoped to (task_id, agent_id, target_workspace_id,
// user_id) — the same shape a normal claim-time token has, just pointed at a
// different workspace — and the mint is recorded in the target workspace's
// activity_log.
func TestMintCrossWorkspaceToken_Success(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "cwt-success-agent", []byte("{}"))
	taskID := createHandlerTestTask(t, agentID)
	targetWS := createForeignWorkspaceForCrossWorkspaceTest(t, "cwt-success-"+uuid.NewString()[:8])
	if _, err := testPool.Exec(ctx, `
		UPDATE agent SET cross_workspace_ids = $1::uuid[] WHERE id = $2
	`, []string{targetWS}, agentID); err != nil {
		t.Fatalf("grant cross-workspace access: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, targetWS, testUserID); err != nil {
		t.Fatalf("add task owner as member of target workspace: %v", err)
	}

	req := crossWorkspaceTokenRequest(agentID, taskID, testWorkspaceID, testUserID, targetWS)
	w := httptest.NewRecorder()
	testHandler.MintCrossWorkspaceToken(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var resp MintCrossWorkspaceTokenResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.HasPrefix(resp.Token, "mat_") {
		t.Fatalf("token = %q, want mat_ prefix", resp.Token)
	}
	if resp.WorkspaceID != targetWS {
		t.Fatalf("response workspace_id = %q, want %q", resp.WorkspaceID, targetWS)
	}

	var dbAgentID, dbTaskID, dbWorkspaceID, dbUserID string
	if err := testPool.QueryRow(ctx, `
		SELECT agent_id, task_id, workspace_id, user_id FROM task_token WHERE token_hash = $1
	`, auth.HashToken(resp.Token)).Scan(&dbAgentID, &dbTaskID, &dbWorkspaceID, &dbUserID); err != nil {
		t.Fatalf("query minted task_token row: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM task_token WHERE token_hash = $1`, auth.HashToken(resp.Token)) })
	if dbAgentID != agentID || dbTaskID != taskID || dbWorkspaceID != targetWS || dbUserID != testUserID {
		t.Fatalf("minted task_token row = (agent=%s task=%s ws=%s user=%s), want (agent=%s task=%s ws=%s user=%s)",
			dbAgentID, dbTaskID, dbWorkspaceID, dbUserID, agentID, taskID, targetWS, testUserID)
	}

	var actionCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM activity_log
		WHERE workspace_id = $1 AND action = 'agent_cross_workspace_token_minted'
	`, targetWS).Scan(&actionCount); err != nil {
		t.Fatalf("query activity_log: %v", err)
	}
	if actionCount == 0 {
		t.Fatal("expected an audit activity_log row in the target workspace, found none")
	}
}
