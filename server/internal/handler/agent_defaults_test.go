package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newRequestWithMemberCtx creates a request with workspace middleware context
// (member + workspaceID) already injected, simulating what the middleware does.
func newRequestWithMemberCtx(method, path string, body any) *http.Request {
	req := newRequest(method, path, body)

	// Look up the member row for the test user + workspace.
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(
		context.Background(),
		db.GetMemberByUserAndWorkspaceParams{
			UserID:      parseUUID(testUserID),
			WorkspaceID: parseUUID(testWorkspaceID),
		},
	)
	if err != nil {
		panic("newRequestWithMemberCtx: could not load test member: " + err.Error())
	}

	ctx := middleware.SetMemberContext(req.Context(), testWorkspaceID, member)
	return req.WithContext(ctx)
}

func TestGetPersonalAgentDefaults_Empty(t *testing.T) {
	// Ensure no pre-existing config for this member.
	testPool.Exec(context.Background(),
		`DELETE FROM member_agent_config WHERE workspace_id = $1`, testWorkspaceID)

	w := httptest.NewRecorder()
	req := newRequestWithMemberCtx(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/agent-defaults/me", nil)
	testHandler.GetPersonalAgentDefaults(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	cfg, ok := resp["config"].(map[string]any)
	if !ok {
		t.Fatalf("expected config to be an object, got %T", resp["config"])
	}
	if len(cfg) != 0 {
		t.Fatalf("expected empty config, got %v", cfg)
	}
}

func TestUpdatePersonalAgentDefaults(t *testing.T) {
	// Clean up any previous config.
	testPool.Exec(context.Background(),
		`DELETE FROM member_agent_config WHERE workspace_id = $1`, testWorkspaceID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM member_agent_config WHERE workspace_id = $1`, testWorkspaceID)
	})

	config := map[string]any{
		"model":        "gpt-4",
		"instructions": "Be concise.",
	}

	// PUT
	w := httptest.NewRecorder()
	req := newRequestWithMemberCtx(http.MethodPut, "/api/workspaces/"+testWorkspaceID+"/agent-defaults/me", map[string]any{
		"config": config,
	})
	testHandler.UpdatePersonalAgentDefaults(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var putResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &putResp)
	if putResp["id"] == nil || putResp["id"] == "" {
		t.Fatal("expected id in PUT response")
	}
	putCfg, ok := putResp["config"].(map[string]any)
	if !ok {
		t.Fatalf("expected config object, got %T", putResp["config"])
	}
	if putCfg["model"] != "gpt-4" {
		t.Fatalf("expected model=gpt-4, got %v", putCfg["model"])
	}

	// GET should now return the saved config.
	w2 := httptest.NewRecorder()
	req2 := newRequestWithMemberCtx(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/agent-defaults/me", nil)
	testHandler.GetPersonalAgentDefaults(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var getResp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &getResp)
	getCfg, ok := getResp["config"].(map[string]any)
	if !ok {
		t.Fatalf("expected config object, got %T", getResp["config"])
	}
	if getCfg["instructions"] != "Be concise." {
		t.Fatalf("expected instructions preserved, got %v", getCfg["instructions"])
	}
}

func TestUpdatePersonalAgentDefaults_Upsert(t *testing.T) {
	testPool.Exec(context.Background(),
		`DELETE FROM member_agent_config WHERE workspace_id = $1`, testWorkspaceID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM member_agent_config WHERE workspace_id = $1`, testWorkspaceID)
	})

	// First PUT
	w := httptest.NewRecorder()
	req := newRequestWithMemberCtx(http.MethodPut, "/api/workspaces/"+testWorkspaceID+"/agent-defaults/me", map[string]any{
		"config": map[string]any{"model": "gpt-4"},
	})
	testHandler.UpdatePersonalAgentDefaults(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first PUT expected 200, got %d", w.Code)
	}

	// Second PUT (upsert)
	w2 := httptest.NewRecorder()
	req2 := newRequestWithMemberCtx(http.MethodPut, "/api/workspaces/"+testWorkspaceID+"/agent-defaults/me", map[string]any{
		"config": map[string]any{"model": "claude-4", "temperature": 0.7},
	})
	testHandler.UpdatePersonalAgentDefaults(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second PUT expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp)
	cfg := resp["config"].(map[string]any)
	if cfg["model"] != "claude-4" {
		t.Fatalf("expected model updated to claude-4, got %v", cfg["model"])
	}
}

func TestUpdatePersonalAgentDefaults_MissingConfig(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequestWithMemberCtx(http.MethodPut, "/api/workspaces/"+testWorkspaceID+"/agent-defaults/me", map[string]any{})
	testHandler.UpdatePersonalAgentDefaults(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateWorkspace_AgentDefaultsRequiresAdmin(t *testing.T) {
	// Create a non-admin member.
	var memberUserID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email) VALUES ('Agent Defaults Test', 'agentdefaults-test@multica.ai')
		RETURNING id
	`).Scan(&memberUserID)
	if err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, memberUserID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberUserID)
	})

	_, err = testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, memberUserID)
	if err != nil {
		t.Fatalf("failed to create test member: %v", err)
	}

	// Try to update workspace settings with agent_defaults as a regular member.
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID, map[string]any{
		"settings": map[string]any{
			"agent_defaults": map[string]any{"model": "gpt-4"},
		},
	})
	req.Header.Set("X-User-ID", memberUserID)
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.UpdateWorkspace(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin setting agent_defaults, got %d: %s", w.Code, w.Body.String())
	}

	// Owner should be allowed.
	w2 := httptest.NewRecorder()
	req2 := newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID, map[string]any{
		"settings": map[string]any{
			"agent_defaults": map[string]any{"model": "gpt-4"},
			"other":          "value",
		},
	})
	req2 = withURLParam(req2, "id", testWorkspaceID)
	testHandler.UpdateWorkspace(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for owner setting agent_defaults, got %d: %s", w2.Code, w2.Body.String())
	}
}
