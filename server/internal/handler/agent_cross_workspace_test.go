package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// createForeignWorkspaceForCrossWorkspaceTest inserts a bare workspace (no
// agent/runtime needed) to use as a grant target, and registers cleanup.
func createForeignWorkspaceForCrossWorkspaceTest(t *testing.T, slug string) string {
	t.Helper()
	ctx := context.Background()

	var workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, 'cross-workspace grant test', $3)
		RETURNING id
	`, "Cross Workspace Grant Test "+slug, slug, "CWG").Scan(&workspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID) })
	return workspaceID
}

func agentCrossWorkspaceGrantsPath(agentID string) string {
	return "/api/agents/" + agentID + "/cross-workspace-grants"
}

// TestAgentCrossWorkspaceGrants_SetAndGetRoundTrip pins the owner/admin happy
// path: an owner sets a grant list, and a subsequent GET returns exactly what
// was written (BUS-171).
func TestAgentCrossWorkspaceGrants_SetAndGetRoundTrip(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "cwg-roundtrip-agent", []byte("{}"))
	foreignWS := createForeignWorkspaceForCrossWorkspaceTest(t, "cwg-roundtrip-"+uuid.NewString()[:8])

	setReq := newRequest(http.MethodPut, agentCrossWorkspaceGrantsPath(agentID), map[string]any{
		"cross_workspace_ids": []string{foreignWS},
	})
	setReq = withURLParam(setReq, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgentCrossWorkspaceGrants(w, setReq)
	if w.Code != http.StatusOK {
		t.Fatalf("set grants: status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var setResp AgentCrossWorkspaceGrantsResponse
	if err := json.NewDecoder(w.Body).Decode(&setResp); err != nil {
		t.Fatalf("decode set response: %v", err)
	}
	if len(setResp.CrossWorkspaceIDs) != 1 || setResp.CrossWorkspaceIDs[0] != foreignWS {
		t.Fatalf("set response cross_workspace_ids = %v, want [%s]", setResp.CrossWorkspaceIDs, foreignWS)
	}

	getReq := withURLParam(newRequest(http.MethodGet, agentCrossWorkspaceGrantsPath(agentID), nil), "id", agentID)
	w = httptest.NewRecorder()
	testHandler.GetAgentCrossWorkspaceGrants(w, getReq)
	if w.Code != http.StatusOK {
		t.Fatalf("get grants: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var getResp AgentCrossWorkspaceGrantsResponse
	if err := json.NewDecoder(w.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if len(getResp.CrossWorkspaceIDs) != 1 || getResp.CrossWorkspaceIDs[0] != foreignWS {
		t.Fatalf("get response cross_workspace_ids = %v, want [%s]", getResp.CrossWorkspaceIDs, foreignWS)
	}

	var actionCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM activity_log
		WHERE workspace_id = $1 AND action = 'agent_cross_workspace_grants_updated'
	`, testWorkspaceID).Scan(&actionCount); err != nil {
		t.Fatalf("query activity_log: %v", err)
	}
	if actionCount == 0 {
		t.Fatal("expected an audit activity_log row for the grant update, found none")
	}
}

// TestAgentCrossWorkspaceGrants_RejectsOwnWorkspace pins the guard that an
// agent can never be granted access to the workspace it already runs in —
// that access is implicit and always-on, so listing it here would be
// meaningless allow-list noise.
func TestAgentCrossWorkspaceGrants_RejectsOwnWorkspace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "cwg-ownws-agent", []byte("{}"))

	req := withURLParam(newRequest(http.MethodPut, agentCrossWorkspaceGrantsPath(agentID), map[string]any{
		"cross_workspace_ids": []string{testWorkspaceID},
	}), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgentCrossWorkspaceGrants(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

// TestAgentCrossWorkspaceGrants_DedupesInput pins that duplicate workspace
// IDs in the request collapse to one entry rather than being stored (and
// later iterated/minted against) redundantly.
func TestAgentCrossWorkspaceGrants_DedupesInput(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "cwg-dedupe-agent", []byte("{}"))
	foreignWS := createForeignWorkspaceForCrossWorkspaceTest(t, "cwg-dedupe-"+uuid.NewString()[:8])

	req := withURLParam(newRequest(http.MethodPut, agentCrossWorkspaceGrantsPath(agentID), map[string]any{
		"cross_workspace_ids": []string{foreignWS, foreignWS},
	}), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgentCrossWorkspaceGrants(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp AgentCrossWorkspaceGrantsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.CrossWorkspaceIDs) != 1 {
		t.Fatalf("cross_workspace_ids = %v, want exactly one deduped entry", resp.CrossWorkspaceIDs)
	}
}

// TestAgentCrossWorkspaceGrants_NonOwnerRejected pins that a plain workspace
// member (not owner/admin) cannot widen an agent's cross-workspace grants —
// the same privilege boundary as custom_env.
func TestAgentCrossWorkspaceGrants_NonOwnerRejected(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "cwg-nonowner-agent", []byte("{}"))
	foreignWS := createForeignWorkspaceForCrossWorkspaceTest(t, "cwg-nonowner-"+uuid.NewString()[:8])
	memberUserID := createWorkspaceMemberUser(t, "CWG Non Owner", "cwg-nonowner-"+uuid.NewString()[:8]+"@multica.ai")

	req := withURLParam(newRequestAs(memberUserID, http.MethodPut, agentCrossWorkspaceGrantsPath(agentID), map[string]any{
		"cross_workspace_ids": []string{foreignWS},
	}), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgentCrossWorkspaceGrants(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

// TestAgentCrossWorkspaceGrants_RequireHumanActorBlocksTaskToken pins the
// router-level gate (RequireHumanActor, wired in router.go): a request
// carrying the task_token actor-source marker must never reach either
// grant-management handler, even indirectly via a chi route group.
func TestAgentCrossWorkspaceGrants_RequireHumanActorBlocksTaskToken(t *testing.T) {
	called := false
	guarded := RequireHumanActor(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, agentCrossWorkspaceGrantsPath(uuid.NewString()), nil)
	req.Header.Set("X-Actor-Source", "task_token")
	w := httptest.NewRecorder()
	guarded.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if called {
		t.Fatal("grant-management handler must not run for a task_token actor")
	}
}
