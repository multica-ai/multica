package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// patchRuntimeCustomName is a small helper that PATCHes /api/runtimes/:id with
// a custom_name body as the given actor and returns the recorder.
func patchRuntimeCustomName(actorID, runtimeID string, body map[string]any) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := newRequestAs(actorID, http.MethodPatch, "/api/runtimes/"+runtimeID, body)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	return w
}

// TestUpdateAgentRuntime_CustomNamePatchApplies covers the single-runtime
// rename path (MUL-4217): a PATCH carrying custom_name sets it, an empty
// string clears it back to NULL, and an over-long value is rejected with 400.
func TestUpdateAgentRuntime_CustomNamePatchApplies(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, runtimeOwnerID, plainMemberID := runtimeVisibilityFixture(t)

	// Owner sets a custom name.
	w := patchRuntimeCustomName(runtimeOwnerID, runtimeID, map[string]any{"custom_name": "  Prod Box  "})
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH custom_name: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.CustomName == nil || *resp.CustomName != "Prod Box" {
		t.Fatalf("custom_name: got %v, want trimmed \"Prod Box\"", resp.CustomName)
	}
	// The raw daemon name is preserved alongside the override.
	if resp.Name != "Visibility Test Runtime" {
		t.Fatalf("name should be untouched by rename: got %q", resp.Name)
	}

	// Empty string clears the override back to NULL.
	w = patchRuntimeCustomName(runtimeOwnerID, runtimeID, map[string]any{"custom_name": "   "})
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH clear custom_name: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp = AgentRuntimeResponse{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.CustomName != nil {
		t.Fatalf("custom_name should be cleared to null, got %q", *resp.CustomName)
	}

	// Over-long name is rejected before any mutation.
	w = patchRuntimeCustomName(runtimeOwnerID, runtimeID, map[string]any{"custom_name": strings.Repeat("x", maxRuntimeCustomNameLen+1)})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PATCH over-long custom_name: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Plain member cannot rename someone else's runtime.
	w = patchRuntimeCustomName(plainMemberID, runtimeID, map[string]any{"custom_name": "hijack"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("PATCH custom_name as plain member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateAgentRuntime_CustomNameMachineFanout verifies that
// apply_to_machine renames every runtime sharing a daemon_id, so a machine
// hosting several provider runtimes can be labelled in one action.
func TestUpdateAgentRuntime_CustomNameMachineFanout(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	_, runtimeOwnerID, _ := runtimeVisibilityFixture(t)
	ctx := context.Background()

	const daemonID = "custom-name-test-daemon"
	makeRuntime := func(provider string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_runtime (
				workspace_id, daemon_id, name, runtime_mode, provider, status,
				device_info, metadata, owner_id, visibility, last_seen_at
			)
			VALUES ($1, $2, $3, 'local', $4, 'online', 'host', '{}'::jsonb, $5, 'private', now())
			RETURNING id
		`, testWorkspaceID, daemonID, provider+" (host)", provider, runtimeOwnerID).Scan(&id); err != nil {
			t.Fatalf("create runtime %s: %v", provider, err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, id)
		})
		return id
	}

	idA := makeRuntime("ccn_a")
	idB := makeRuntime("ccn_b")

	w := patchRuntimeCustomName(runtimeOwnerID, idA, map[string]any{
		"custom_name":      "Bohan's MacBook",
		"apply_to_machine": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH machine rename: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Both runtimes on the daemon must now carry the name.
	for _, id := range []string{idA, idB} {
		var name *string
		if err := testPool.QueryRow(ctx, `SELECT custom_name FROM agent_runtime WHERE id = $1`, id).Scan(&name); err != nil {
			t.Fatalf("read custom_name for %s: %v", id, err)
		}
		if name == nil || *name != "Bohan's MacBook" {
			t.Fatalf("runtime %s custom_name = %v, want machine name applied", id, name)
		}
	}
}
