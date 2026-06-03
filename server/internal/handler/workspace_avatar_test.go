package handler

// CEREBRO-PATCH(workspace-avatar-test): FIR-2580 — net-new test for workspace avatar_url set + partial-update preservation. Lives in the upstream handler dir (not the cerebro zone) so it can exercise the unexported handler internals.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// FIR-2580: workspace logo (avatar_url). These tests exercise the cerebro
// extension to UpdateWorkspace — that avatar_url is persisted and returned,
// that a partial update touching only avatar_url leaves name/description
// intact, and that a partial update of other fields leaves a previously set
// avatar_url intact (the COALESCE(sqlc.narg, …) partial-update contract).

type workspaceAvatarResp struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	AvatarURL   *string `json:"avatar_url"`
}

func updateWorkspaceForTest(t *testing.T, wsID string, body map[string]any) workspaceAvatarResp {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/workspaces/"+wsID, body)
	req = withURLParam(req, "id", wsID)
	testHandler.UpdateWorkspace(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateWorkspace: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp workspaceAvatarResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, w.Body.String())
	}
	return resp
}

func TestUpdateWorkspace_AvatarSetAndPartialPreserve(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	const slug = "handler-tests-avatar"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Avatar Test WS", slug, "original description").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	const logo = "/api/files/workspace-logo.png"

	// 1. Set avatar_url only — name/description must stay untouched.
	resp := updateWorkspaceForTest(t, wsID, map[string]any{"avatar_url": logo})
	if resp.AvatarURL == nil || *resp.AvatarURL != logo {
		t.Fatalf("avatar_url not persisted: %+v", resp.AvatarURL)
	}
	if resp.Name != "Avatar Test WS" {
		t.Fatalf("name changed by avatar-only update: %q", resp.Name)
	}
	if resp.Description == nil || *resp.Description != "original description" {
		t.Fatalf("description changed by avatar-only update: %+v", resp.Description)
	}

	// 2. Update name only — the previously set avatar_url must be preserved
	//    (partial-update contract: omitted fields are not overwritten).
	resp = updateWorkspaceForTest(t, wsID, map[string]any{"name": "Renamed WS"})
	if resp.Name != "Renamed WS" {
		t.Fatalf("name not updated: %q", resp.Name)
	}
	if resp.AvatarURL == nil || *resp.AvatarURL != logo {
		t.Fatalf("avatar_url lost on name-only update: %+v", resp.AvatarURL)
	}

	// 3. Empty string clears the logo (back to the letter fallback).
	resp = updateWorkspaceForTest(t, wsID, map[string]any{"avatar_url": ""})
	if resp.AvatarURL == nil || *resp.AvatarURL != "" {
		t.Fatalf("avatar_url not cleared: %+v", resp.AvatarURL)
	}
	if resp.Name != "Renamed WS" {
		t.Fatalf("name changed by avatar-clear update: %q", resp.Name)
	}
}
