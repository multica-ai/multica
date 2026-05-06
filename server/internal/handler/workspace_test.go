package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/multica-ai/multica/server/internal/localmode"
)

// newLocalWorkspaceHandler returns a clone of the package-level testHandler
// with its LocalMode configured for the test. We never mutate testHandler
// itself because it is shared across the entire handler test suite.
func newLocalWorkspaceHandler(localEnabled bool) *Handler {
	h := *testHandler
	if localEnabled {
		h.LocalMode = localmode.Config{ProductMode: "local"}
	} else {
		h.LocalMode = localmode.Config{}
	}
	return &h
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

// TestLocalGuardWorkspace_LeaveRejectedInLocalMode verifies that LeaveWorkspace
// returns 403 with the expected body in local mode and does NOT delete the
// caller's membership. Local mode is single-user; "leave" is meaningless.
func TestLocalGuardWorkspace_LeaveRejectedInLocalMode(t *testing.T) {
	ctx := context.Background()

	h := newLocalWorkspaceHandler(true)

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+testWorkspaceID+"/members/me", nil)
	req = withURLParam(req, "id", testWorkspaceID)
	h.LeaveWorkspace(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if got, want := w.Body.String(), "{\"error\":\"leaving a workspace is unavailable in local mode\"}\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}

	// Membership row must still exist — the guard runs BEFORE the DB delete.
	var exists bool
	if err := testPool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM member WHERE workspace_id = $1 AND user_id = $2)
`, testWorkspaceID, testUserID).Scan(&exists); err != nil {
		t.Fatalf("verify membership: %v", err)
	}
	if !exists {
		t.Fatal("membership row was deleted in local mode despite 403 — guard ran AFTER the delete")
	}
}

// TestLocalGuardWorkspace_LeaveAllowedOutsideLocalMode is the negative
// counterpart: outside local mode LeaveWorkspace must continue to function.
// We intentionally do NOT use the fixture user's owner membership of
// testWorkspaceID here — deleting it would break every other test in the
// suite. Instead we seed a throwaway workspace + non-owner member and remove
// only that membership.
func TestLocalGuardWorkspace_LeaveAllowedOutsideLocalMode(t *testing.T) {
	ctx := context.Background()

	const slug = "local-guard-leave-ok"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Local Guard Leave OK", slug, "LeaveWorkspace allowed outside local mode").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	// We seed our test user as 'admin' (non-owner) so the "must keep at least
	// one owner" branch in LeaveWorkspace never triggers. The workspace has
	// no owner at all, but the guard only fires when the leaver IS an owner.
	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'admin')
`, wsID, testUserID); err != nil {
		t.Fatalf("create admin member: %v", err)
	}

	// newRequest() sets X-Workspace-ID to testWorkspaceID by default; override
	// it via the URL param + a fresh header so workspaceMember() resolves the
	// throwaway workspace, not the fixture one.
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID+"/members/me", nil)
	req.Header.Set("X-Workspace-ID", wsID)
	req = withURLParam(req, "id", wsID)
	testHandler.LeaveWorkspace(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM member WHERE workspace_id = $1 AND user_id = $2)
`, wsID, testUserID).Scan(&exists); err != nil {
		t.Fatalf("verify membership: %v", err)
	}
	if exists {
		t.Fatal("throwaway membership row still exists after successful leave")
	}
}

// TestLocalGuardWorkspace_DeleteLocalSlugRejectedInLocalMode verifies that the
// special "local" workspace cannot be deleted via the ordinary delete path
// when running in local mode. Spec: 400 + the exact error body.
func TestLocalGuardWorkspace_DeleteLocalSlugRejectedInLocalMode(t *testing.T) {
	ctx := context.Background()

	// Cleanup any residue from local_session_test.go fixtures or previous runs.
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, localmode.LocalWorkspaceSlug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Local Workspace", localmode.LocalWorkspaceSlug, "guard test for ordinary delete").Scan(&wsID); err != nil {
		t.Fatalf("create local workspace: %v", err)
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

	h := newLocalWorkspaceHandler(true)

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req.Header.Set("X-Workspace-ID", wsID)
	req = withURLParam(req, "id", wsID)
	h.DeleteWorkspace(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if got, want := w.Body.String(), "{\"error\":\"the local workspace cannot be deleted\"}\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
		t.Fatalf("verify workspace: %v", err)
	}
	if !exists {
		t.Fatal("local workspace was deleted in local mode despite 400")
	}
}

// TestLocalGuardWorkspace_DeleteCustomSlugAllowedInLocalMode documents that the
// guard ONLY protects the default slug. A user-created workspace with a
// different slug, even in local mode, can still be deleted normally.
func TestLocalGuardWorkspace_DeleteCustomSlugAllowedInLocalMode(t *testing.T) {
	ctx := context.Background()

	const slug = "local-guard-other"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Local Guard Other", slug, "non-default slug deletion in local mode").Scan(&wsID); err != nil {
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

	h := newLocalWorkspaceHandler(true)

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req.Header.Set("X-Workspace-ID", wsID)
	req = withURLParam(req, "id", wsID)
	h.DeleteWorkspace(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
		t.Fatalf("verify workspace: %v", err)
	}
	if exists {
		t.Fatal("custom-slug workspace still exists after owner DELETE in local mode")
	}
}

// TestLocalGuardWorkspace_DeleteLocalSlugAllowedOutsideLocalMode verifies that
// the "local" slug is NOT magic outside local mode — a hosted deployment that
// happens to have a workspace called "local" can still delete it normally.
func TestLocalGuardWorkspace_DeleteLocalSlugAllowedOutsideLocalMode(t *testing.T) {
	ctx := context.Background()

	// Cleanup any residue.
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, localmode.LocalWorkspaceSlug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Local Workspace (hosted)", localmode.LocalWorkspaceSlug, "guard inactive outside local mode").Scan(&wsID); err != nil {
		t.Fatalf("create local-slug workspace: %v", err)
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
	req.Header.Set("X-Workspace-ID", wsID)
	req = withURLParam(req, "id", wsID)
	testHandler.DeleteWorkspace(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
		t.Fatalf("verify workspace: %v", err)
	}
	if exists {
		t.Fatal("local-slug workspace still exists after owner DELETE outside local mode")
	}
}
