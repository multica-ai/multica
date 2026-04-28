package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdateProject_DoesNotResetLocalPathWhenFieldAbsent(t *testing.T) {
	ctx := context.Background()
	const originalPath = "/tmp/project-preserve"

	var projectID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO project (workspace_id, title, status, priority, local_path)
VALUES ($1, $2, 'planned', 'none', $3)
RETURNING id
`, testWorkspaceID, "Project local_path preserve", originalPath).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPatch, "/api/projects/"+projectID, map[string]any{
		"title": "Project local_path preserve updated",
	})
	req = withURLParam(req, "id", projectID)
	testHandler.UpdateProject(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("UpdateProject expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var localPath *string
	if err := testPool.QueryRow(ctx, `SELECT local_path FROM project WHERE id = $1`, projectID).Scan(&localPath); err != nil {
		t.Fatalf("load project local_path: %v", err)
	}
	if localPath == nil || *localPath != originalPath {
		t.Fatalf("project local_path = %v, want %q", localPath, originalPath)
	}
}

func TestUpdateProject_ClearsLocalPathWhenNullPassed(t *testing.T) {
	ctx := context.Background()

	var projectID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO project (workspace_id, title, status, priority, local_path)
VALUES ($1, $2, 'planned', 'none', $3)
RETURNING id
`, testWorkspaceID, "Project local_path clear", "/tmp/project-clear").Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPatch, "/api/projects/"+projectID, map[string]any{
		"local_path": nil,
	})
	req = withURLParam(req, "id", projectID)
	testHandler.UpdateProject(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("UpdateProject expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var localPath *string
	if err := testPool.QueryRow(ctx, `SELECT local_path FROM project WHERE id = $1`, projectID).Scan(&localPath); err != nil {
		t.Fatalf("load project local_path: %v", err)
	}
	if localPath != nil {
		t.Fatalf("project local_path = %q, want NULL", *localPath)
	}
}
