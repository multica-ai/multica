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
	testPool.Exec(context.Background(),
		`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
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

func TestUpdatePersonalAgentDefaults_WritesInstructionsHistoryOnlyForInstructions(t *testing.T) {
	testPool.Exec(context.Background(),
		`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
	testPool.Exec(context.Background(),
		`DELETE FROM member_agent_config WHERE workspace_id = $1`, testWorkspaceID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(),
			`DELETE FROM member_agent_config WHERE workspace_id = $1`, testWorkspaceID)
	})

	save := func(config map[string]any) {
		w := httptest.NewRecorder()
		req := newRequestWithMemberCtx(http.MethodPut, "/api/workspaces/"+testWorkspaceID+"/agent-defaults/me", map[string]any{
			"config": config,
		})
		testHandler.UpdatePersonalAgentDefaults(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("save expected 200, got %d: %s", w.Code, w.Body.String())
		}
	}

	save(map[string]any{
		"instructions": "first version",
		"custom_env":   map[string]string{"TOKEN": "secret"},
	})
	save(map[string]any{
		"instructions": "first version",
		"custom_env":   map[string]string{"TOKEN": "changed"},
	})
	save(map[string]any{
		"instructions": "second version",
		"custom_env":   map[string]string{"TOKEN": "changed"},
	})

	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(
		context.Background(),
		db.GetMemberByUserAndWorkspaceParams{
			UserID:      parseUUID(testUserID),
			WorkspaceID: parseUUID(testWorkspaceID),
		},
	)
	if err != nil {
		t.Fatalf("load member: %v", err)
	}

	rows, err := testHandler.Queries.ListInstructionsHistory(context.Background(), db.ListInstructionsHistoryParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Scope:       "personal",
		MemberID:    member.ID,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 history rows, got %d", len(rows))
	}
	if rows[0].Content != "second version" || rows[1].Content != "first version" {
		t.Fatalf("unexpected history contents: %#v", []string{rows[0].Content, rows[1].Content})
	}
}

func TestUpdatePersonalAgentDefaults_NoHistoryWhenInstructionsOmitted(t *testing.T) {
	testPool.Exec(context.Background(),
		`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
	testPool.Exec(context.Background(),
		`DELETE FROM member_agent_config WHERE workspace_id = $1`, testWorkspaceID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(),
			`DELETE FROM member_agent_config WHERE workspace_id = $1`, testWorkspaceID)
	})

	w := httptest.NewRecorder()
	req := newRequestWithMemberCtx(http.MethodPut, "/api/workspaces/"+testWorkspaceID+"/agent-defaults/me", map[string]any{
		"config": map[string]any{
			"custom_env": map[string]string{"TOKEN": "secret"},
		},
	})
	testHandler.UpdatePersonalAgentDefaults(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("save expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM instructions_history WHERE workspace_id = $1
	`, testWorkspaceID).Scan(&count); err != nil {
		t.Fatalf("count history: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no history rows, got %d", count)
	}
}

func TestInstructionsHistoryEndpoints_Personal(t *testing.T) {
	testPool.Exec(context.Background(),
		`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
	})

	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(
		context.Background(),
		db.GetMemberByUserAndWorkspaceParams{
			UserID:      parseUUID(testUserID),
			WorkspaceID: parseUUID(testWorkspaceID),
		},
	)
	if err != nil {
		t.Fatalf("load member: %v", err)
	}
	inserted, err := testHandler.Queries.InsertInstructionsHistory(context.Background(), db.InsertInstructionsHistoryParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Scope:       "personal",
		MemberID:    member.ID,
		Content:     "full personal content",
		ActorID:     member.ID,
	})
	if err != nil {
		t.Fatalf("insert history: %v", err)
	}

	listW := httptest.NewRecorder()
	listReq := newRequestWithMemberCtx(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/instructions-history?scope=personal", nil)
	listReq = withURLParam(listReq, "id", testWorkspaceID)
	testHandler.ListInstructionsHistory(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("list expected 200, got %d: %s", listW.Code, listW.Body.String())
	}

	var listResp struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(listW.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listResp.Total != 1 || len(listResp.Items) != 1 {
		t.Fatalf("expected one history item, got total=%d len=%d", listResp.Total, len(listResp.Items))
	}
	if _, ok := listResp.Items[0]["content"]; ok {
		t.Fatalf("list response should not include full content")
	}

	detailW := httptest.NewRecorder()
	detailReq := newRequestWithMemberCtx(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/instructions-history/"+uuidToString(inserted.ID)+"?scope=personal", nil)
	detailReq = withURLParam(detailReq, "id", testWorkspaceID)
	detailReq = withURLParam(detailReq, "versionId", uuidToString(inserted.ID))
	testHandler.GetInstructionsHistory(detailW, detailReq)
	if detailW.Code != http.StatusOK {
		t.Fatalf("detail expected 200, got %d: %s", detailW.Code, detailW.Body.String())
	}
	var detailResp map[string]any
	if err := json.Unmarshal(detailW.Body.Bytes(), &detailResp); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detailResp["content"] != "full personal content" {
		t.Fatalf("expected full content, got %v", detailResp["content"])
	}
}

func TestDuplicateAgentDefaults_WritesInstructionsHistory(t *testing.T) {
	testPool.Exec(context.Background(),
		`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
	testPool.Exec(context.Background(),
		`DELETE FROM member_agent_config WHERE workspace_id = $1`, testWorkspaceID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(),
			`DELETE FROM member_agent_config WHERE workspace_id = $1`, testWorkspaceID)
	})

	var sourceUserID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email) VALUES ('Defaults Source', 'defaults-source@multica.ai')
		RETURNING id
	`).Scan(&sourceUserID); err != nil {
		t.Fatalf("create source user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, sourceUserID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, sourceUserID)
	})

	var sourceMemberID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
		RETURNING id
	`, testWorkspaceID, sourceUserID).Scan(&sourceMemberID); err != nil {
		t.Fatalf("create source member: %v", err)
	}

	sourceConfig, _ := json.Marshal(map[string]any{
		"instructions": "source instructions",
		"custom_env":   map[string]string{"TOKEN": "secret"},
	})
	source, err := testHandler.Queries.UpsertMemberAgentConfig(context.Background(), db.UpsertMemberAgentConfigParams{
		MemberID:    parseUUID(sourceMemberID),
		WorkspaceID: parseUUID(testWorkspaceID),
		Config:      sourceConfig,
	})
	if err != nil {
		t.Fatalf("create source config: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequestWithMemberCtx(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/agent-defaults/duplicate/"+uuidToString(source.ID), nil)
	req = withURLParam(req, "configId", uuidToString(source.ID))
	testHandler.DuplicateAgentDefaults(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("duplicate expected 200, got %d: %s", w.Code, w.Body.String())
	}

	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(
		context.Background(),
		db.GetMemberByUserAndWorkspaceParams{
			UserID:      parseUUID(testUserID),
			WorkspaceID: parseUUID(testWorkspaceID),
		},
	)
	if err != nil {
		t.Fatalf("load current member: %v", err)
	}
	rows, err := testHandler.Queries.ListInstructionsHistory(context.Background(), db.ListInstructionsHistoryParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Scope:       "personal",
		MemberID:    member.ID,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(rows) != 1 || rows[0].Content != "source instructions" {
		t.Fatalf("expected duplicated instructions history, got %#v", rows)
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
	testPool.Exec(context.Background(),
		`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
	})

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

func TestUpdateWorkspace_KnowledgeCuratorRequiresAdminAndValidSchema(t *testing.T) {
	var memberUserID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email) VALUES ('Curator Settings Test', 'curator-settings-test@multica.ai')
		RETURNING id
	`).Scan(&memberUserID)
	if err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, memberUserID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberUserID)
	})

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, memberUserID); err != nil {
		t.Fatalf("failed to create test member: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID, map[string]any{
		"settings": map[string]any{
			"knowledge_curator": map[string]any{"enabled": true, "provider": "test"},
		},
	})
	req.Header.Set("X-User-ID", memberUserID)
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.UpdateWorkspace(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin setting knowledge_curator, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID, map[string]any{
		"settings": map[string]any{
			"knowledge_curator": map[string]any{"enabled": "yes"},
		},
	})
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.UpdateWorkspace(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid knowledge_curator schema, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID, map[string]any{
		"settings": map[string]any{
			"knowledge_curator": map[string]any{
				"enabled":         true,
				"provider":        "test",
				"model":           "curator-model",
				"embedding_model": "embedding-model",
				"runtime_mode":    "external",
				"base_url":        "https://api.example.com/v1",
				"secret_ref":      "secret://workspace/curator",
			},
			"other": "value",
		},
	})
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.UpdateWorkspace(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for owner setting knowledge_curator, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateWorkspace_AgentDefaultsInstructionsHistory(t *testing.T) {
	testPool.Exec(context.Background(),
		`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
	})

	update := func(settings map[string]any) {
		w := httptest.NewRecorder()
		req := newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID, map[string]any{
			"settings": settings,
		})
		req = withURLParam(req, "id", testWorkspaceID)
		testHandler.UpdateWorkspace(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("update workspace expected 200, got %d: %s", w.Code, w.Body.String())
		}
	}

	update(map[string]any{
		"agent_defaults": map[string]any{
			"instructions": "system first",
			"custom_env":   map[string]string{"TOKEN": "secret"},
		},
	})
	update(map[string]any{
		"agent_defaults": map[string]any{
			"instructions": "system first",
			"custom_env":   map[string]string{"TOKEN": "changed"},
		},
	})
	update(map[string]any{
		"agent_defaults": map[string]any{
			"instructions": "system second",
			"custom_env":   map[string]string{"TOKEN": "changed"},
		},
	})

	rows, err := testHandler.Queries.ListInstructionsHistory(context.Background(), db.ListInstructionsHistoryParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Scope:       "system",
		MemberID:    parseUUID(testUserID),
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("list system history: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 system history rows, got %d", len(rows))
	}
	if rows[0].Content != "system second" || rows[1].Content != "system first" {
		t.Fatalf("unexpected system history contents: %#v", []string{rows[0].Content, rows[1].Content})
	}
}

func TestUpdateWorkspace_AgentDefaultsNoHistoryWhenInstructionsOmitted(t *testing.T) {
	testPool.Exec(context.Background(),
		`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
	})

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID, map[string]any{
		"settings": map[string]any{
			"agent_defaults": map[string]any{
				"custom_env": map[string]string{"TOKEN": "secret"},
			},
		},
	})
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.UpdateWorkspace(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM instructions_history WHERE workspace_id = $1
	`, testWorkspaceID).Scan(&count); err != nil {
		t.Fatalf("count history: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no system history rows, got %d", count)
	}
}

func TestInstructionsHistoryEndpoints_SystemRequiresAdmin(t *testing.T) {
	testPool.Exec(context.Background(),
		`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
	})

	ownerMember, err := testHandler.Queries.GetMemberByUserAndWorkspace(
		context.Background(),
		db.GetMemberByUserAndWorkspaceParams{
			UserID:      parseUUID(testUserID),
			WorkspaceID: parseUUID(testWorkspaceID),
		},
	)
	if err != nil {
		t.Fatalf("load owner member: %v", err)
	}
	if _, err := testHandler.Queries.InsertInstructionsHistory(context.Background(), db.InsertInstructionsHistoryParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Scope:       "system",
		Content:     "system content",
		ActorID:     ownerMember.ID,
	}); err != nil {
		t.Fatalf("insert system history: %v", err)
	}

	var memberUserID string
	err = testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email) VALUES ('System History Member', 'system-history-member@multica.ai')
		RETURNING id
	`).Scan(&memberUserID)
	if err != nil {
		t.Fatalf("failed to create member user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, memberUserID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberUserID)
	})

	var regularMemberID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
		RETURNING id
	`, testWorkspaceID, memberUserID).Scan(&regularMemberID); err != nil {
		t.Fatalf("failed to create regular member: %v", err)
	}

	forbiddenReq := newRequestAs(memberUserID, http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/instructions-history?scope=system", nil)
	memberRow, err := testHandler.Queries.GetMember(context.Background(), parseUUID(regularMemberID))
	if err != nil {
		t.Fatalf("load regular member: %v", err)
	}
	forbiddenReq = forbiddenReq.WithContext(middleware.SetMemberContext(forbiddenReq.Context(), testWorkspaceID, memberRow))
	forbiddenReq = withURLParam(forbiddenReq, "id", testWorkspaceID)
	forbiddenW := httptest.NewRecorder()
	testHandler.ListInstructionsHistory(forbiddenW, forbiddenReq)
	if forbiddenW.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for regular member reading system history, got %d: %s", forbiddenW.Code, forbiddenW.Body.String())
	}

	allowedReq := newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/instructions-history?scope=system", nil)
	allowedReq = withURLParam(allowedReq, "id", testWorkspaceID)
	allowedW := httptest.NewRecorder()
	testHandler.ListInstructionsHistory(allowedW, allowedReq)
	if allowedW.Code != http.StatusOK {
		t.Fatalf("expected 200 for owner reading system history, got %d: %s", allowedW.Code, allowedW.Body.String())
	}

	var resp struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(allowedW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode allowed response: %v", err)
	}
	if resp.Total != 1 || len(resp.Items) != 1 {
		t.Fatalf("expected one system history row, got total=%d len=%d", resp.Total, len(resp.Items))
	}
}
