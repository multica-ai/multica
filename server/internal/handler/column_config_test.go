package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListColumnConfigs_Empty(t *testing.T) {
	clearColumnConfigs(t)

	w := httptest.NewRecorder()
	req := withURLParams(newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/column-configs", nil), "id", testWorkspaceID)

	testHandler.ListColumnConfigs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListColumnConfigs: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []ColumnConfigResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("ListColumnConfigs: decode response: %v", err)
	}
	if len(resp) != 0 {
		t.Fatalf("ListColumnConfigs: expected empty array, got %d items", len(resp))
	}
}

func TestUpsertColumnConfig_CreateAndUpdate(t *testing.T) {
	clearColumnConfigs(t)

	createReq := withURLParams(newRequest(http.MethodPut, "/api/workspaces/"+testWorkspaceID+"/column-configs/in_progress", map[string]any{
		"instructions":        "## Entry\nReady to execute",
		"allowed_transitions": []string{"in_review", "done"},
	}), "id", testWorkspaceID, "status", "in_progress")

	createW := httptest.NewRecorder()
	testHandler.UpsertColumnConfig(createW, createReq)
	if createW.Code != http.StatusOK {
		t.Fatalf("UpsertColumnConfig(create): expected 200, got %d: %s", createW.Code, createW.Body.String())
	}

	var created ColumnConfigResponse
	if err := json.NewDecoder(createW.Body).Decode(&created); err != nil {
		t.Fatalf("UpsertColumnConfig(create): decode response: %v", err)
	}
	if created.Status != "in_progress" {
		t.Fatalf("UpsertColumnConfig(create): expected status in_progress, got %q", created.Status)
	}
	if len(created.AllowedTransitions) != 2 {
		t.Fatalf("UpsertColumnConfig(create): expected 2 transitions, got %d", len(created.AllowedTransitions))
	}

	updateReq := withURLParams(newRequest(http.MethodPut, "/api/workspaces/"+testWorkspaceID+"/column-configs/in_progress", map[string]any{
		"instructions":        "## Exit\nShip it",
		"allowed_transitions": []string{"done"},
	}), "id", testWorkspaceID, "status", "in_progress")

	updateW := httptest.NewRecorder()
	testHandler.UpsertColumnConfig(updateW, updateReq)
	if updateW.Code != http.StatusOK {
		t.Fatalf("UpsertColumnConfig(update): expected 200, got %d: %s", updateW.Code, updateW.Body.String())
	}

	var updated ColumnConfigResponse
	if err := json.NewDecoder(updateW.Body).Decode(&updated); err != nil {
		t.Fatalf("UpsertColumnConfig(update): decode response: %v", err)
	}
	if updated.ID != created.ID {
		t.Fatalf("UpsertColumnConfig(update): expected same row id, got %q vs %q", updated.ID, created.ID)
	}
	if updated.Instructions != "## Exit\nShip it" {
		t.Fatalf("UpsertColumnConfig(update): unexpected instructions %q", updated.Instructions)
	}
	if len(updated.AllowedTransitions) != 1 || updated.AllowedTransitions[0] != "done" {
		t.Fatalf("UpsertColumnConfig(update): unexpected transitions %#v", updated.AllowedTransitions)
	}
}

func TestUpsertColumnConfig_RejectsInvalidTransition(t *testing.T) {
	clearColumnConfigs(t)

	req := withURLParams(newRequest(http.MethodPut, "/api/workspaces/"+testWorkspaceID+"/column-configs/todo", map[string]any{
		"instructions":        "anything",
		"allowed_transitions": []string{"nope"},
	}), "id", testWorkspaceID, "status", "todo")

	w := httptest.NewRecorder()
	testHandler.UpsertColumnConfig(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpsertColumnConfig: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func clearColumnConfigs(t *testing.T) {
	t.Helper()

	if _, err := testPool.Exec(context.Background(), `DELETE FROM workspace_column_config WHERE workspace_id = $1`, testWorkspaceID); err != nil {
		t.Fatalf("failed to clear column configs: %v", err)
	}
}
