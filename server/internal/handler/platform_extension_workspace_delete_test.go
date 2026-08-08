package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeleteWorkspaceRemovesPlatformExtensionReleases(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	const slug = "handler-tests-delete-platform-extension"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var workspaceID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug)
VALUES ('Platform extension delete test', $1)
RETURNING id
`, slug).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM platform_extension_release WHERE workspace_id = $1`, workspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, workspaceID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
INSERT INTO platform_extension_release (
	workspace_id, extension_key, name, version, digest, manifest, created_by
)
VALUES ($1, 'workspace-delete', 'Workspace Delete', '1.0.0', 'sha256:workspace-delete', '{}'::jsonb, $2)
`, workspaceID, testUserID); err != nil {
		t.Fatalf("create platform extension release: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := newRequest(http.MethodDelete, "/api/workspaces/"+workspaceID, nil)
	request = withURLParam(request, "id", workspaceID)
	testHandler.DeleteWorkspace(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("DeleteWorkspace returned %d: %s", recorder.Code, recorder.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx, `
SELECT COUNT(*)
FROM platform_extension_release
WHERE workspace_id = $1
`, workspaceID).Scan(&count); err != nil {
		t.Fatalf("count surviving platform extension releases: %v", err)
	}
	if count != 0 {
		t.Fatalf("platform extension releases survived workspace delete: %d", count)
	}
}
