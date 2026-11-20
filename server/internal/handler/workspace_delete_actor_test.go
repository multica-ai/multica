package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// An agent task token and a cloud node PAT both authenticate as their owner,
// so the owner-role check alone would let a running agent delete its owner's
// whole workspace. Deleting a workspace takes every issue, doc and agent with
// it, and doc deletion is already human-only.
func TestDeleteWorkspaceRefusesMachineCredentials(t *testing.T) {
	if testPool == nil {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()

	const slug = "handler-tests-delete-machine-actor"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Handler Test Delete Machine Actor", slug, "DeleteWorkspace machine actor test").Scan(&wsID); err != nil {
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

	for _, source := range []string{"task_token", "cloud_pat"} {
		t.Run(source, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := withURLParam(newRequest("DELETE", "/api/workspaces/"+wsID, nil), "id", wsID)
			req.Header.Set("X-Actor-Source", source)
			testHandler.DeleteWorkspace(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
			}
			var exists bool
			if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
				t.Fatalf("verify workspace: %v", err)
			}
			if !exists {
				t.Fatal("workspace was deleted by a machine credential")
			}
		})
	}
}
