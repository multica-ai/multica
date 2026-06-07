package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestMcpConnectorListReturnsSeed asserts the directory list seeds the global
// curated catalog on first access and returns it (200). Every seed slug must
// appear, and global rows must come back flagged is_custom=false.
func TestMcpConnectorListReturnsSeed(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/mcp-connectors", nil)
	testHandler.ListMcpConnectors(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListMcpConnectors: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []McpConnectorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(resp) == 0 {
		t.Fatal("expected seeded connectors, got empty list")
	}

	bySlug := make(map[string]McpConnectorResponse, len(resp))
	for _, c := range resp {
		bySlug[c.Slug] = c
	}
	for _, slug := range []string{"github", "slack", "notion"} {
		c, ok := bySlug[slug]
		if !ok {
			t.Fatalf("expected seeded connector %q in directory", slug)
		}
		if c.IsCustom {
			t.Fatalf("seeded connector %q must be is_custom=false (global row)", slug)
		}
		if c.WorkspaceID != nil {
			t.Fatalf("seeded connector %q must have null workspace_id, got %v", slug, *c.WorkspaceID)
		}
		// input_schema / mcp_template must be well-formed JSON objects so the
		// frontend form renderer never receives a bare null.
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(c.McpTemplate, &obj); err != nil {
			t.Fatalf("connector %q mcp_template not a JSON object: %v", slug, err)
		}
	}
}

// TestMcpConnectorListSeedIsIdempotent asserts a second list does not
// duplicate the global catalog.
func TestMcpConnectorListSeedIsIdempotent(t *testing.T) {
	listCount := func() int {
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/mcp-connectors", nil)
		testHandler.ListMcpConnectors(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ListMcpConnectors: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp []McpConnectorResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return len(resp)
	}

	first := listCount()
	second := listCount()
	if first != second {
		t.Fatalf("seed not idempotent: first list %d connectors, second list %d", first, second)
	}
}

// TestMcpConnectorCreateAndListIncludesCustom asserts an admin can author a
// workspace-custom connector and it appears in the directory flagged
// is_custom=true alongside the global catalog (global ∪ workspace).
func TestMcpConnectorCreateAndListIncludesCustom(t *testing.T) {
	ctx := context.Background()
	slug := "custom-connector-test"
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM mcp_connector WHERE workspace_id = $1 AND slug = $2`, testWorkspaceID, slug)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/mcp-connectors", map[string]any{
		"slug":         slug,
		"name":         "Custom Connector",
		"description":  "A workspace-authored connector",
		"popularity":   5,
		"input_schema": map[string]any{"fields": []any{}},
		"mcp_template": map[string]any{"mcpServers": map[string]any{"custom": map[string]any{"command": "echo"}}},
	})
	testHandler.CreateMcpConnector(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateMcpConnector: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created McpConnectorResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if !created.IsCustom {
		t.Fatal("created connector must be is_custom=true")
	}
	if created.WorkspaceID == nil || *created.WorkspaceID != testWorkspaceID {
		t.Fatalf("created connector workspace_id = %v, want %s", created.WorkspaceID, testWorkspaceID)
	}

	// The custom row appears in the directory together with global rows.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/mcp-connectors", nil)
	testHandler.ListMcpConnectors(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListMcpConnectors: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp []McpConnectorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	var sawCustom, sawGlobal bool
	for _, c := range resp {
		if c.Slug == slug && c.IsCustom {
			sawCustom = true
		}
		if c.Slug == "github" && !c.IsCustom {
			sawGlobal = true
		}
	}
	if !sawCustom {
		t.Fatal("directory list did not include the workspace-custom connector")
	}
	if !sawGlobal {
		t.Fatal("directory list did not include the global catalog (global ∪ workspace expected)")
	}
}

// TestMcpConnectorUpdateAndDelete asserts admin update + delete of a custom
// connector, and that deleting again 404s (no silent success — #1661).
func TestMcpConnectorUpdateAndDelete(t *testing.T) {
	ctx := context.Background()
	slug := "custom-update-delete-test"
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM mcp_connector WHERE workspace_id = $1 AND slug = $2`, testWorkspaceID, slug)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/mcp-connectors", map[string]any{
		"slug":         slug,
		"name":         "Before",
		"mcp_template": map[string]any{"mcpServers": map[string]any{"x": map[string]any{"command": "echo"}}},
	})
	testHandler.CreateMcpConnector(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateMcpConnector: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created McpConnectorResponse
	json.NewDecoder(w.Body).Decode(&created)

	// Update name.
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/mcp-connectors/"+created.ID, map[string]any{"name": "After"})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateMcpConnector(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateMcpConnector: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated McpConnectorResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Name != "After" {
		t.Fatalf("update name = %q, want After", updated.Name)
	}

	// Delete.
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/mcp-connectors/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.DeleteMcpConnector(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteMcpConnector: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Second delete must 404 — the row is gone, never a silent 204.
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/mcp-connectors/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.DeleteMcpConnector(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("DeleteMcpConnector second time: expected 404, got %d", w.Code)
	}
}

// TestMcpConnectorDeleteGlobalRejected asserts a global seed row cannot be
// deleted through the API — the workspace_id IS NOT NULL guard means the
// DELETE matches no row and the handler returns 404.
func TestMcpConnectorDeleteGlobalRejected(t *testing.T) {
	// Seed + list to get a global connector id.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/mcp-connectors", nil)
	testHandler.ListMcpConnectors(w, req)
	var resp []McpConnectorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	var globalID string
	for _, c := range resp {
		if !c.IsCustom {
			globalID = c.ID
			break
		}
	}
	if globalID == "" {
		t.Fatal("expected at least one global connector to test deletion guard")
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/mcp-connectors/"+globalID, nil)
	req = withURLParam(req, "id", globalID)
	testHandler.DeleteMcpConnector(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("DeleteMcpConnector on global row: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// The row must still exist.
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM mcp_connector WHERE id = $1`, globalID).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Fatalf("global connector deleted through API (count=%d) — guard failed", count)
	}
}

// TestMcpConnectorCreateRequiresAdmin asserts a non-admin member is denied
// (403) when authoring a custom connector.
func TestMcpConnectorCreateRequiresAdmin(t *testing.T) {
	ctx := context.Background()

	// Create a second user who is only a plain member of the test workspace.
	var memberUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "MCP Plain Member", "mcp-plain-member@multica.ai").Scan(&memberUserID); err != nil {
		t.Fatalf("insert member user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, memberUserID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, memberUserID); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/mcp-connectors", map[string]any{
		"slug":         "should-be-denied",
		"name":         "Denied",
		"mcp_template": map[string]any{"mcpServers": map[string]any{"x": map[string]any{"command": "echo"}}},
	})
	// Override the actor to the non-admin member.
	req.Header.Set("X-User-ID", memberUserID)
	testHandler.CreateMcpConnector(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("CreateMcpConnector as non-admin: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM mcp_connector WHERE workspace_id = $1 AND slug = $2`,
		testWorkspaceID, "should-be-denied").Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 0 {
		t.Fatalf("non-admin create wrote a row (count=%d) — admin gate failed", count)
	}
}

// TestMcpConnectorDeleteInvalidUUID asserts a malformed connector id returns
// 400, not a silent success.
func TestMcpConnectorDeleteInvalidUUID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/mcp-connectors/not-a-uuid", nil)
	req = withURLParam(req, "id", "not-a-uuid")
	testHandler.DeleteMcpConnector(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("DeleteMcpConnector with invalid id: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
