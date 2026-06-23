package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An unknown project status must fail fast with a 400 and the valid list, not
// surface the DB CHECK violation as a 500 (#3925: `--status active`).
func TestCreateProjectInvalidStatusReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "invalid status project",
		"status": "active",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status, got %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "planned") {
		t.Errorf("expected error to list valid statuses, got: %s", body)
	}
}

func TestCreateProjectInvalidPriorityReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "invalid priority project",
		"priority": "critical",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid priority, got %d: %s", w.Code, w.Body.String())
	}
}

// A valid status still creates the project (the validation does not over-reject).
func TestCreateProjectValidStatusReturns201(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "valid status project",
		"status": "in_progress",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for valid status, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	t.Cleanup(func() {
		req := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	})
	if project.Status != "in_progress" {
		t.Errorf("expected status in_progress, got %q", project.Status)
	}
}

// Updating to an unknown status is a 400, not a 500.
func TestUpdateProjectInvalidStatusReturns400(t *testing.T) {
	// Seed a project to update.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "update validation project",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed CreateProject: %d %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	t.Cleanup(func() {
		req := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	})

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+project.ID, map[string]any{"status": "active"})
	req = withURLParam(req, "id", project.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid update status, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteProjectRequiresAdminOrOwner(t *testing.T) {
	memberUserID := createProjectPermissionTestMember(t, "member")
	project := createProjectPermissionTestProject(t, "delete permission denied project")

	w := httptest.NewRecorder()
	req := newRequestAs(memberUserID, "DELETE", "/api/projects/"+project.ID, nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.DeleteProject(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for plain member project delete, got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM project WHERE id = $1)`, project.ID).Scan(&exists); err != nil {
		t.Fatalf("verify project exists: %v", err)
	}
	if !exists {
		t.Fatal("project was deleted despite plain member request")
	}
}

func TestDeleteProjectAllowsAdmin(t *testing.T) {
	adminUserID := createProjectPermissionTestMember(t, "admin")
	project := createProjectPermissionTestProject(t, "delete permission admin project")

	w := httptest.NewRecorder()
	req := newRequestAs(adminUserID, "DELETE", "/api/projects/"+project.ID, nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.DeleteProject(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin project delete, got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM project WHERE id = $1)`, project.ID).Scan(&exists); err != nil {
		t.Fatalf("verify project deleted: %v", err)
	}
	if exists {
		t.Fatal("project still exists after admin delete")
	}
}

func TestGetProjectAictxStatusDefaultOffReturnsMetadataOnly(t *testing.T) {
	project := createProjectPermissionTestProject(t, "aictx default-off project")

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects/"+project.ID+"/aictx/status?multica_project_id=client-forged", nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.GetProjectAictxStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for default-off AICTX status, got %d: %s", w.Code, w.Body.String())
	}

	var status ProjectAictxStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode AICTX status: %v", err)
	}

	if status.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2", status.SchemaVersion)
	}
	if status.ContractName != "D-MULTICA010-ProjectAictxStatusDTO" {
		t.Errorf("contract_name = %q", status.ContractName)
	}
	if status.WorkspaceID != testWorkspaceID {
		t.Errorf("workspace_id = %q, want route workspace %q", status.WorkspaceID, testWorkspaceID)
	}
	if status.MulticaProjectID != project.ID {
		t.Errorf("multica_project_id = %q, want route project %q", status.MulticaProjectID, project.ID)
	}
	if status.ProjectBindingID != nil {
		t.Errorf("project_binding_id = %v, want nil when disabled", *status.ProjectBindingID)
	}
	if status.State != "unconfigured" {
		t.Errorf("state = %q, want unconfigured", status.State)
	}
	if status.ContextIndexStatus != "unknown" {
		t.Errorf("context_index_status = %q, want unknown", status.ContextIndexStatus)
	}
	if status.RedactionStatus != "not_needed" {
		t.Errorf("redaction_status = %q, want not_needed", status.RedactionStatus)
	}
	if status.Binding.Status != "missing" {
		t.Errorf("binding.status = %q, want missing", status.Binding.Status)
	}
	if status.Binding.BindingID != nil || status.Binding.RepoRootRefRedacted != nil || status.Binding.RepoRootSHA256 != nil {
		t.Fatalf("disabled binding leaked resolved root metadata: %+v", status.Binding)
	}
	if len(status.ReasonCodes) != 1 || status.ReasonCodes[0] != "feature_disabled" {
		t.Errorf("reason_codes = %#v, want [feature_disabled]", status.ReasonCodes)
	}
	if strings.Contains(w.Body.String(), "/Users/") || strings.Contains(w.Body.String(), "client-forged") {
		t.Fatalf("AICTX status leaked raw path or client-supplied authority: %s", w.Body.String())
	}
}

func TestGetProjectAictxStatusEnabledWithoutLocalDirectoryStaysUnconfigured(t *testing.T) {
	project := createProjectPermissionTestProject(t, "aictx enabled missing binding project")
	setWorkspaceAictxProjectBindingFlagForTest(t, true)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects/"+project.ID+"/aictx/status", nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.GetProjectAictxStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for missing AICTX binding, got %d: %s", w.Code, w.Body.String())
	}

	var status ProjectAictxStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode AICTX status: %v", err)
	}
	if status.State != "unconfigured" {
		t.Errorf("state = %q, want unconfigured", status.State)
	}
	if status.Binding.Status != "missing" {
		t.Errorf("binding.status = %q, want missing", status.Binding.Status)
	}
	if len(status.ReasonCodes) != 1 || status.ReasonCodes[0] != "no_project_local_directory" {
		t.Errorf("reason_codes = %#v, want [no_project_local_directory]", status.ReasonCodes)
	}
}

func TestGetProjectAictxStatusLocalDirectoryUsesRedactedBindingRef(t *testing.T) {
	project := createProjectPermissionTestProject(t, "aictx local directory binding project")
	setWorkspaceAictxProjectBindingFlagForTest(t, true)

	const rawPath = "/Users/example/private/source"
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "local_directory",
		"resource_ref": map[string]any{
			"local_path": rawPath,
			"daemon_id":  "daemon-aictx-redacted",
		},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProjectResource: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+project.ID+"/aictx/status", nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.GetProjectAictxStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for bound AICTX status, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), rawPath) || strings.Contains(w.Body.String(), "/Users/") {
		t.Fatalf("AICTX status leaked raw local directory: %s", w.Body.String())
	}

	var status ProjectAictxStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode AICTX status: %v", err)
	}
	if status.State != "provider_unavailable" {
		t.Errorf("state = %q, want provider_unavailable until provider integration exists", status.State)
	}
	if status.Binding.Status != "bound" {
		t.Errorf("binding.status = %q, want bound", status.Binding.Status)
	}
	if status.Binding.RepoRootRefRedacted == nil || !strings.HasPrefix(*status.Binding.RepoRootRefRedacted, "project_resource://") {
		t.Fatalf("repo_root_ref_redacted = %v, want project_resource:// ref", status.Binding.RepoRootRefRedacted)
	}
	if status.Binding.RepoRootSHA256 == nil || !strings.HasPrefix(*status.Binding.RepoRootSHA256, "sha256:") {
		t.Fatalf("repo_root_sha256 = %v, want sha256 ref", status.Binding.RepoRootSHA256)
	}
}

func TestGetProjectAictxStatusUsesWorkspaceProjectTenantGuard(t *testing.T) {
	project := createProjectPermissionTestProject(t, "aictx tenant guarded project")

	foreignWorkspaceID := createProjectAictxForeignWorkspace(t)
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects/"+project.ID+"/aictx/status", nil)
	req.Header.Set("X-Workspace-ID", foreignWorkspaceID)
	req = withURLParam(req, "id", project.ID)
	testHandler.GetProjectAictxStatus(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for project outside resolved workspace, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), project.ID) {
		t.Fatalf("foreign workspace response leaked project id: %s", w.Body.String())
	}
}

func createProjectPermissionTestMember(t *testing.T, role string) string {
	t.Helper()

	ctx := context.Background()
	email := "project-delete-" + role + "@multica.test"
	// The schema uses no foreign keys or cascades, so a leftover member from a
	// prior run won't disappear when its user is deleted. Drop the member first.
	_, _ = testPool.Exec(ctx, `DELETE FROM member WHERE user_id IN (SELECT id FROM "user" WHERE email = $1)`, email)
	_, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)

	var userID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO "user" (name, email)
VALUES ($1, $2)
RETURNING id
`, "Project Delete "+role, email).Scan(&userID); err != nil {
		t.Fatalf("create %s user: %v", role, err)
	}
	t.Cleanup(func() {
		// No cascade in the schema: remove the member row before its user so the
		// shared test workspace isn't left with an orphaned member record.
		_, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, userID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, $3)
`, testWorkspaceID, userID, role); err != nil {
		t.Fatalf("create %s member: %v", role, err)
	}

	return userID
}

func createProjectPermissionTestProject(t *testing.T, title string) ProjectResponse {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": title,
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, project.ID)
	})
	return project
}

func setWorkspaceAictxProjectBindingFlagForTest(t *testing.T, enabled bool) {
	t.Helper()
	ctx := context.Background()

	var previous []byte
	if err := testPool.QueryRow(ctx, `SELECT settings FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previous); err != nil {
		t.Fatalf("load workspace settings: %v", err)
	}

	next := map[string]any{}
	if len(previous) > 0 {
		if err := json.Unmarshal(previous, &next); err != nil {
			t.Fatalf("decode workspace settings: %v", err)
		}
	}
	next["aictx_project_binding_enabled"] = enabled
	nextBytes, err := json.Marshal(next)
	if err != nil {
		t.Fatalf("encode workspace settings: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE workspace SET settings = $1 WHERE id = $2`, nextBytes, testWorkspaceID); err != nil {
		t.Fatalf("update workspace settings: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `UPDATE workspace SET settings = $1 WHERE id = $2`, previous, testWorkspaceID)
	})
}

func createProjectAictxForeignWorkspace(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	suffix := strings.ReplaceAll(t.Name(), "/", "-")
	slug := "aictx-foreign-" + suffix

	var workspaceID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description, issue_prefix)
VALUES ($1, $2, $3, $4)
RETURNING id
`, "AICTX Foreign Workspace", slug, "Foreign workspace for AICTX tenant guard test", "AFT").Scan(&workspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, workspaceID, testUserID); err != nil {
		t.Fatalf("create foreign workspace member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})
	return workspaceID
}
