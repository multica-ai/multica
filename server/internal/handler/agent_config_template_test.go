package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// cleanPersonalTemplates wipes this workspace's personal templates + the
// personal/system instructions history so each test starts from a known state.
func cleanPersonalTemplates(t *testing.T) {
	t.Helper()
	testPool.Exec(context.Background(),
		`DELETE FROM agent_config_template WHERE workspace_id = $1`, testWorkspaceID)
	testPool.Exec(context.Background(),
		`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_config_template WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(),
			`DELETE FROM instructions_history WHERE workspace_id = $1`, testWorkspaceID)
	})
}

func TestUpdateAgentConfigTemplate_RecordsHistoryPerTemplate(t *testing.T) {
	cleanPersonalTemplates(t)

	create := func(name string, isDefault bool, instructions string) string {
		w := httptest.NewRecorder()
		req := newRequestWithMemberCtx(http.MethodPost, "/api/agent-config-templates", map[string]any{
			"scope":     "personal",
			"name":      name,
			"config":    map[string]any{"instructions": instructions},
			"is_default": isDefault,
		})
		testHandler.CreateAgentConfigTemplate(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %q expected 201, got %d: %s", name, w.Code, w.Body.String())
		}
		var resp map[string]any
		json.Unmarshal(w.Body.Bytes(), &resp)
		return resp["id"].(string)
	}
	update := func(id string, config map[string]any) {
		w := httptest.NewRecorder()
		req := newRequestWithMemberCtx(http.MethodPut, "/api/agent-config-templates/"+id, map[string]any{
			"config": config,
		})
		req = withURLParam(req, "templateId", id)
		testHandler.UpdateAgentConfigTemplate(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("update expected 200, got %d: %s", w.Code, w.Body.String())
		}
	}

	// A default personal template: editing its instructions records a version.
	defaultID := create("默认", true, "v1")
	update(defaultID, map[string]any{"instructions": "v2"})
	// Editing only env (instructions unchanged) records nothing.
	update(defaultID, map[string]any{"instructions": "v2", "custom_env": map[string]string{"K": "v"}})
	// A non-default template: editing its instructions ALSO records a version
	// — every template carries its own instructions history.
	plainID := create("other", false, "p1")
	update(plainID, map[string]any{"instructions": "p2"})

	listHistory := func(templateID string) []db.ListInstructionsHistoryRow {
		rows, err := testHandler.Queries.ListInstructionsHistory(context.Background(), db.ListInstructionsHistoryParams{
			WorkspaceID: parseUUID(testWorkspaceID),
			TemplateID:  parseUUID(templateID),
			Limit:       10,
		})
		if err != nil {
			t.Fatalf("list history for %s: %v", templateID, err)
		}
		return rows
	}

	defaultRows := listHistory(defaultID)
	if len(defaultRows) != 1 || defaultRows[0].Content != "v2" {
		t.Fatalf("default template: expected 1 history row content v2, got %#v", defaultRows)
	}
	plainRows := listHistory(plainID)
	if len(plainRows) != 1 || plainRows[0].Content != "p2" {
		t.Fatalf("plain template: expected 1 history row content p2, got %#v", plainRows)
	}
}

func TestListAllAgentDefaultTemplates_MasksEnv(t *testing.T) {
	cleanPersonalTemplates(t)

	// Seed a default personal template with a secret env value.
	testPool.Exec(context.Background(),
		`INSERT INTO agent_config_template (workspace_id, scope, name, config, is_default, created_by)
		 VALUES ($1, 'personal', '默认', '{"instructions":"hi","custom_env":{"SECRET":"shh"}}'::jsonb, true, $2)`,
		testWorkspaceID, testMemberID(t))

	w := httptest.NewRecorder()
	req := newRequestWithMemberCtx(http.MethodGet, "/api/agent-config-templates/defaults", nil)
	testHandler.ListAllAgentDefaultTemplates(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var items []map[string]any
	json.Unmarshal(w.Body.Bytes(), &items)
	if len(items) != 1 {
		t.Fatalf("expected 1 default template, got %d", len(items))
	}
	cfg := items[0]["config"].(map[string]any)
	env := cfg["custom_env"].(map[string]any)
	if env["SECRET"] != "***" {
		t.Fatalf("expected masked env, got %v", env["SECRET"])
	}
	if cfg["instructions"] != "hi" {
		t.Fatalf("expected instructions preserved, got %v", cfg["instructions"])
	}
}

// testMemberID resolves the test member row id for seeding created_by.
func testMemberID(t *testing.T) string {
	t.Helper()
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
	return uuidToString(member.ID)
}
