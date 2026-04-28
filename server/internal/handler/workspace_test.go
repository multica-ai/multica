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

// TestUpdateWorkspace_PKMPath exercises the round-trip behavior of the
// pkm_path settings field that PR2/PR3 will consume: a normalized path is
// stored and read back, and obviously-bad inputs (".." traversal, empty
// string, non-string) are rejected with 400. Allowlist-root enforcement
// itself is covered by pkmpath unit tests; the handler test focuses on the
// PUT/GET wiring.
func TestUpdateWorkspace_PKMPath(t *testing.T) {
	ctx := context.Background()

	const slug = "handler-tests-pkm-path"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description, issue_prefix)
VALUES ($1, $2, $3, $4)
RETURNING id
`, "Handler PKM Path", slug, "PKM path test", "PKM").Scan(&wsID); err != nil {
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

	t.Run("rejects dot-dot traversal", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/workspaces/"+wsID, map[string]any{
			"settings": map[string]any{"pkm_path": "/PKM/../etc/passwd"},
		})
		req = withURLParam(req, "id", wsID)
		testHandler.UpdateWorkspace(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for '..' path, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects empty string", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/workspaces/"+wsID, map[string]any{
			"settings": map[string]any{"pkm_path": ""},
		})
		req = withURLParam(req, "id", wsID)
		testHandler.UpdateWorkspace(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for empty pkm_path, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects non-string", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/workspaces/"+wsID, map[string]any{
			"settings": map[string]any{"pkm_path": 42},
		})
		req = withURLParam(req, "id", wsID)
		testHandler.UpdateWorkspace(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for non-string pkm_path, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("stores cleaned path and round-trips through GET", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/workspaces/"+wsID, map[string]any{
			"settings": map[string]any{"pkm_path": "/PKM-CUONG//GROWTH/PROJECTS/"},
		})
		req = withURLParam(req, "id", wsID)
		testHandler.UpdateWorkspace(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT settings: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var put WorkspaceResponse
		if err := json.NewDecoder(w.Body).Decode(&put); err != nil {
			t.Fatalf("decode PUT response: %v", err)
		}
		settings, ok := put.Settings.(map[string]any)
		if !ok {
			t.Fatalf("PUT response settings is not an object: %#v", put.Settings)
		}
		if got := settings["pkm_path"]; got != "/PKM-CUONG/GROWTH/PROJECTS" {
			t.Fatalf("PUT response pkm_path = %#v, want %q", got, "/PKM-CUONG/GROWTH/PROJECTS")
		}

		w = httptest.NewRecorder()
		req = newRequest("GET", "/api/workspaces/"+wsID, nil)
		req = withURLParam(req, "id", wsID)
		testHandler.GetWorkspace(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET workspace: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var got WorkspaceResponse
		if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
			t.Fatalf("decode GET response: %v", err)
		}
		settings, ok = got.Settings.(map[string]any)
		if !ok {
			t.Fatalf("GET response settings is not an object: %#v", got.Settings)
		}
		if v := settings["pkm_path"]; v != "/PKM-CUONG/GROWTH/PROJECTS" {
			t.Fatalf("GET response pkm_path = %#v, want %q", v, "/PKM-CUONG/GROWTH/PROJECTS")
		}
	})
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
