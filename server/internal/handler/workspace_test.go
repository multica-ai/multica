package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

func TestWorkspaceSettingsSanitizesFirtalGatewayKey(t *testing.T) {
	settings := sanitizeWorkspaceSettingsForResponse(map[string]any{
		"firtal_gateway": map[string]any{
			"enabled":     true,
			"gateway_url": "https://registry.example",
			"api_key":     "rk_secret",
			"model":       "gpt-5.5",
		},
	}).(map[string]any)

	gateway := settings["firtal_gateway"].(map[string]any)
	if _, ok := gateway["api_key"]; ok {
		t.Fatal("api_key leaked in sanitized workspace settings")
	}
	if gateway["api_key_configured"] != true {
		t.Fatalf("api_key_configured = %#v, want true", gateway["api_key_configured"])
	}
}

func TestPrepareWorkspaceSettingsUpdatePreservesGatewaySecretFromSanitizedInput(t *testing.T) {
	current := []byte(`{"firtal_gateway":{"enabled":true,"gateway_url":"https://registry.example","api_key":"rk_secret","model":"gpt-5.5"}}`)
	raw, changed, err := prepareWorkspaceSettingsUpdate(current, map[string]any{
		"co_authored_by_enabled": true,
		"firtal_gateway": map[string]any{
			"enabled":            true,
			"gateway_url":        "https://registry.example",
			"api_key_configured": true,
			"model":              "gpt-5.5",
		},
	})
	if err != nil {
		t.Fatalf("prepareWorkspaceSettingsUpdate() error = %v", err)
	}
	if changed {
		t.Fatal("gateway settings should not be marked changed when sanitized input matches stored config")
	}

	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("unmarshal stored settings: %v", err)
	}
	gateway := stored["firtal_gateway"].(map[string]any)
	if gateway["api_key"] != "rk_secret" {
		t.Fatalf("api_key = %#v, want preserved secret", gateway["api_key"])
	}
	if _, ok := gateway["api_key_configured"]; ok {
		t.Fatal("api_key_configured should not be persisted")
	}
}

func TestPrepareWorkspaceSettingsUpdateRequiresCompleteEnabledGateway(t *testing.T) {
	_, _, err := prepareWorkspaceSettingsUpdate(nil, map[string]any{
		"firtal_gateway": map[string]any{
			"enabled":     true,
			"gateway_url": "https://registry.example",
		},
	})
	if err == nil {
		t.Fatal("expected enabled gateway without API key to fail")
	}
}

func TestPrepareWorkspaceSettingsUpdateRejectsUnsafeGatewayURL(t *testing.T) {
	for _, rawURL := range []string{
		"http://registry.example",
		"https://localhost",
		"https://127.0.0.1",
		"https://169.254.169.254",
		"https://registry.internal",
	} {
		t.Run(rawURL, func(t *testing.T) {
			_, _, err := prepareWorkspaceSettingsUpdate(nil, map[string]any{
				"firtal_gateway": map[string]any{
					"enabled":     true,
					"gateway_url": rawURL,
					"api_key":     "rk_secret",
				},
			})
			if err == nil {
				t.Fatal("expected unsafe gateway URL to fail")
			}
		})
	}
}

func TestPrepareWorkspaceSettingsUpdateNormalizesGatewayURL(t *testing.T) {
	raw, _, err := prepareWorkspaceSettingsUpdate(nil, map[string]any{
		"firtal_gateway": map[string]any{
			"enabled":     true,
			"gateway_url": " https://Registry.Example.Com/ ",
			"api_key":     "rk_secret",
		},
	})
	if err != nil {
		t.Fatalf("prepareWorkspaceSettingsUpdate() error = %v", err)
	}

	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("unmarshal stored settings: %v", err)
	}
	gateway := stored["firtal_gateway"].(map[string]any)
	if gateway["gateway_url"] != "https://registry.example.com" {
		t.Fatalf("gateway_url = %#v", gateway["gateway_url"])
	}
}

func TestPrepareWorkspaceSettingsUpdateRequiresFreshKeyWhenGatewayHostChanges(t *testing.T) {
	current := []byte(`{"firtal_gateway":{"enabled":true,"gateway_url":"https://registry.example.com","api_key":"rk_secret","model":"gpt-5.5"}}`)

	_, _, err := prepareWorkspaceSettingsUpdate(current, map[string]any{
		"firtal_gateway": map[string]any{
			"enabled":            true,
			"gateway_url":        "https://other-registry.example.com",
			"api_key_configured": true,
			"model":              "gpt-5.5",
		},
	})
	if err == nil {
		t.Fatal("expected host change without fresh API key to fail")
	}
}

func TestPrepareWorkspaceSettingsUpdateClearsStoredKeyWhenDisabledHostChanges(t *testing.T) {
	current := []byte(`{"firtal_gateway":{"enabled":true,"gateway_url":"https://registry.example.com","api_key":"rk_secret","model":"gpt-5.5"}}`)

	raw, _, err := prepareWorkspaceSettingsUpdate(current, map[string]any{
		"firtal_gateway": map[string]any{
			"enabled":            false,
			"gateway_url":        "https://other-registry.example.com",
			"api_key_configured": true,
			"model":              "gpt-5.5",
		},
	})
	if err != nil {
		t.Fatalf("prepareWorkspaceSettingsUpdate() error = %v", err)
	}

	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("unmarshal stored settings: %v", err)
	}
	gateway := stored["firtal_gateway"].(map[string]any)
	if _, ok := gateway["api_key"]; ok {
		t.Fatal("api_key should be cleared when host changes without a fresh key")
	}
}

func TestPrepareWorkspaceSettingsUpdateDetectsGatewayRotation(t *testing.T) {
	current := []byte(`{"firtal_gateway":{"enabled":true,"gateway_url":"https://registry.example","api_key":"rk_old","model":"gpt-5.5"}}`)
	raw, changed, err := prepareWorkspaceSettingsUpdate(current, map[string]any{
		"firtal_gateway": map[string]any{
			"enabled":     true,
			"gateway_url": "https://registry.example",
			"api_key":     "rk_new",
			"model":       "gpt-5.5",
		},
	})
	if err != nil {
		t.Fatalf("prepareWorkspaceSettingsUpdate() error = %v", err)
	}
	if !changed {
		t.Fatal("gateway rotation should be marked changed")
	}

	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("unmarshal stored settings: %v", err)
	}
	gateway := stored["firtal_gateway"].(map[string]any)
	if gateway["api_key"] != "rk_new" {
		t.Fatalf("api_key = %#v, want rotated secret", gateway["api_key"])
	}
}

func TestCreateWorkspace_RejectsReservedSlug(t *testing.T) {
	// Drive the test off the actual reservedSlugs map so the test can never
	// drift from the source of truth. New entries are covered automatically.
	reserved := make([]string, 0, len(reservedSlugs))
	for slug := range reservedSlugs {
		reserved = append(reserved, slug)
	}
	sort.Strings(reserved) // deterministic test order

	for _, slug := range reserved {
		t.Run(slug, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/workspaces", map[string]any{
				"name": fmt.Sprintf("Test %s", slug),
				"slug": slug,
			})
			testHandler.CreateWorkspace(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("slug %q: expected 400, got %d: %s", slug, w.Code, w.Body.String())
			}
		})
	}
}

// TestDeleteWorkspace_RequiresOwner exercises the in-handler authorization
// added to DeleteWorkspace by calling the handler directly (bypassing the
// router-level RequireWorkspaceRoleFromURL middleware). Without the handler
// check, a non-owner member request would reach DeleteWorkspace and erase the
// workspace; with it, the handler must return 403 and leave the workspace
// intact.
func TestDeleteWorkspace_RequiresOwner(t *testing.T) {
	ctx := context.Background()

	const slug = "handler-tests-delete-403"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Handler Test Delete 403", slug, "DeleteWorkspace handler permission test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'admin')
`, wsID, testUserID); err != nil {
		t.Fatalf("create admin member: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testHandler.DeleteWorkspace(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 from DeleteWorkspace handler for admin (non-owner), got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
		t.Fatalf("verify workspace: %v", err)
	}
	if !exists {
		t.Fatal("workspace was deleted despite non-owner request — handler-level check did not fire")
	}
}

// TestDeleteWorkspace_OwnerSucceeds is the positive counterpart: an owner
// calling DeleteWorkspace directly must succeed (204) and the workspace must
// be gone. This guards the handler check against being too strict.
func TestDeleteWorkspace_OwnerSucceeds(t *testing.T) {
	ctx := context.Background()

	const slug = "handler-tests-delete-ok"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Handler Test Delete OK", slug, "DeleteWorkspace handler owner test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testHandler.DeleteWorkspace(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from DeleteWorkspace handler for owner, got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
		t.Fatalf("verify workspace: %v", err)
	}
	if exists {
		t.Fatal("workspace still exists after owner DELETE")
	}
}
