package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var testHandler *Handler
var testPool *pgxpool.Pool
var testUserID string
var testWorkspaceID string

const (
	handlerTestEmail         = "handler-test@multica.ai"
	handlerTestName          = "Handler Test User"
	handlerTestWorkspaceSlug = "handler-tests"
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping tests: could not connect to database: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping tests: database not reachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}

	queries := db.New(pool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	emailSvc := service.NewEmailService()
	testHandler = New(queries, pool, hub, bus, emailSvc, nil, nil)
	testPool = pool

	testUserID, testWorkspaceID, err = setupHandlerTestFixture(ctx, pool)
	if err != nil {
		fmt.Printf("Failed to set up handler test fixture: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	code := m.Run()
	if err := cleanupHandlerTestFixture(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean up handler test fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func setupHandlerTestFixture(ctx context.Context, pool *pgxpool.Pool) (string, string, error) {
	if err := cleanupHandlerTestFixture(ctx, pool); err != nil {
		return "", "", err
	}

	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, handlerTestName, handlerTestEmail).Scan(&userID); err != nil {
		return "", "", err
	}

	var workspaceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Handler Tests", handlerTestWorkspaceSlug, "Temporary workspace for handler tests", "HAN").Scan(&workspaceID); err != nil {
		return "", "", err
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		return "", "", err
	}

	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, now())
		RETURNING id
	`, workspaceID, "Handler Test Runtime", "handler_test_runtime", "Handler test runtime").Scan(&runtimeID); err != nil {
		return "", "", err
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id, tools, triggers
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4, '[]'::jsonb, '[]'::jsonb)
	`, workspaceID, "Handler Test Agent", runtimeID, userID); err != nil {
		return "", "", err
	}

	return userID, workspaceID, nil
}

func cleanupHandlerTestFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, handlerTestWorkspaceSlug); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, handlerTestEmail); err != nil {
		return err
	}
	return nil
}

func newRequest(method, path string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	return req
}

func withURLParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func assertTimestampPtrEqual(t *testing.T, actual *string, expected string, field string) {
	t.Helper()
	if actual == nil {
		t.Fatalf("%s: expected %q, got nil", field, expected)
	}

	expectedTime, err := time.Parse(time.RFC3339Nano, expected)
	if err != nil {
		t.Fatalf("%s: failed to parse expected timestamp %q: %v", field, expected, err)
	}
	actualTime, err := time.Parse(time.RFC3339Nano, *actual)
	if err != nil {
		t.Fatalf("%s: failed to parse actual timestamp %q: %v", field, *actual, err)
	}
	if !actualTime.Equal(expectedTime) {
		t.Fatalf("%s: expected %q, got %q", field, expected, *actual)
	}
}

func TestIssueCRUD(t *testing.T) {
	// Create
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "Test issue from Go test",
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	if created.Title != "Test issue from Go test" {
		t.Fatalf("CreateIssue: expected title 'Test issue from Go test', got '%s'", created.Title)
	}
	if created.Status != "todo" {
		t.Fatalf("CreateIssue: expected status 'todo', got '%s'", created.Status)
	}
	issueID := created.ID

	// Get
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID, nil)
	req = withURLParam(req, "id", issueID)
	testHandler.GetIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var fetched IssueResponse
	json.NewDecoder(w.Body).Decode(&fetched)
	if fetched.ID != issueID {
		t.Fatalf("GetIssue: expected id '%s', got '%s'", issueID, fetched.ID)
	}

	// Update - partial (only status)
	w = httptest.NewRecorder()
	status := "in_progress"
	req = newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"status": status,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated IssueResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Status != "in_progress" {
		t.Fatalf("UpdateIssue: expected status 'in_progress', got '%s'", updated.Status)
	}
	if updated.Title != "Test issue from Go test" {
		t.Fatalf("UpdateIssue: title should be preserved, got '%s'", updated.Title)
	}
	if updated.Priority != "medium" {
		t.Fatalf("UpdateIssue: priority should be preserved, got '%s'", updated.Priority)
	}

	// List
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID, nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var listResp map[string]any
	json.NewDecoder(w.Body).Decode(&listResp)
	issues := listResp["issues"].([]any)
	if len(issues) == 0 {
		t.Fatal("ListIssues: expected at least 1 issue")
	}

	// Delete
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/issues/"+issueID, nil)
	req = withURLParam(req, "id", issueID)
	testHandler.DeleteIssue(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteIssue: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify deleted
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID, nil)
	req = withURLParam(req, "id", issueID)
	testHandler.GetIssue(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetIssue after delete: expected 404, got %d", w.Code)
	}
}

func TestProjectCRUDAndIssueLinking(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Project Alpha",
		"description": "Initial project description",
		"icon":        "🚀",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var createdProject ProjectResponse
	json.NewDecoder(w.Body).Decode(&createdProject)
	if createdProject.Title != "Project Alpha" {
		t.Fatalf("CreateProject: expected title 'Project Alpha', got %q", createdProject.Title)
	}
	if createdProject.Status != "planned" {
		t.Fatalf("CreateProject: expected default status 'planned', got %q", createdProject.Status)
	}

	projectID := createdProject.ID
	projectDeleted := false
	issueID := ""
	t.Cleanup(func() {
		if issueID != "" {
			w := httptest.NewRecorder()
			req := newRequest("DELETE", "/api/issues/"+issueID, nil)
			req = withURLParam(req, "id", issueID)
			testHandler.DeleteIssue(w, req)
		}
		if !projectDeleted {
			w := httptest.NewRecorder()
			req := newRequest("DELETE", "/api/projects/"+projectID, nil)
			req = withURLParam(req, "id", projectID)
			testHandler.DeleteProject(w, req)
		}
	})

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+projectID, nil)
	req = withURLParam(req, "id", projectID)
	testHandler.GetProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetProject: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var fetchedProject ProjectResponse
	json.NewDecoder(w.Body).Decode(&fetchedProject)
	if fetchedProject.ID != projectID {
		t.Fatalf("GetProject: expected id %q, got %q", projectID, fetchedProject.ID)
	}

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+projectID, map[string]any{
		"status":      "in_progress",
		"description": "Updated project description",
	})
	req = withURLParam(req, "id", projectID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateProject: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updatedProject ProjectResponse
	json.NewDecoder(w.Body).Decode(&updatedProject)
	if updatedProject.Status != "in_progress" {
		t.Fatalf("UpdateProject: expected status 'in_progress', got %q", updatedProject.Status)
	}
	if updatedProject.Description == nil || *updatedProject.Description != "Updated project description" {
		t.Fatalf("UpdateProject: expected updated description, got %#v", updatedProject.Description)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects?workspace_id="+testWorkspaceID, nil)
	testHandler.ListProjects(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjects: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var projectListResp struct {
		Projects []ProjectResponse `json:"projects"`
		Total    int               `json:"total"`
	}
	json.NewDecoder(w.Body).Decode(&projectListResp)
	foundProject := false
	for _, project := range projectListResp.Projects {
		if project.ID == projectID {
			foundProject = true
			break
		}
	}
	if !foundProject {
		t.Fatalf("ListProjects: expected project %q in response", projectID)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "Project-linked issue",
		"project_id": projectID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue with project: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var createdIssue IssueResponse
	json.NewDecoder(w.Body).Decode(&createdIssue)
	issueID = createdIssue.ID
	if createdIssue.ProjectID == nil || *createdIssue.ProjectID != projectID {
		t.Fatalf("CreateIssue with project: expected project_id %q, got %#v", projectID, createdIssue.ProjectID)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&project_id="+projectID, nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues by project_id: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var issueListResp struct {
		Issues []IssueResponse `json:"issues"`
	}
	json.NewDecoder(w.Body).Decode(&issueListResp)
	foundIssue := false
	for _, issue := range issueListResp.Issues {
		if issue.ID == issueID {
			foundIssue = true
			break
		}
	}
	if !foundIssue {
		t.Fatalf("ListIssues by project_id: expected issue %q in response", issueID)
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/projects/"+projectID, nil)
	req = withURLParam(req, "id", projectID)
	testHandler.DeleteProject(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteProject: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	projectDeleted = true

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID, nil)
	req = withURLParam(req, "id", issueID)
	testHandler.GetIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetIssue after project delete: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var fetchedIssue IssueResponse
	json.NewDecoder(w.Body).Decode(&fetchedIssue)
	if fetchedIssue.ProjectID != nil {
		t.Fatalf("GetIssue after project delete: expected cleared project_id, got %#v", fetchedIssue.ProjectID)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+projectID, nil)
	req = withURLParam(req, "id", projectID)
	testHandler.GetProject(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetProject after delete: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateIssueRejectsProjectFromDifferentWorkspace(t *testing.T) {
	ctx := context.Background()
	otherSlug := fmt.Sprintf("handler-project-foreign-%d", time.Now().UnixNano())

	var otherWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Foreign Project Workspace", otherSlug, "Foreign workspace for project validation", "FPW").Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("failed to create foreign workspace: %v", err)
	}

	var foreignProjectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, status)
		VALUES ($1, $2, 'planned')
		RETURNING id
	`, otherWorkspaceID, "Foreign project").Scan(&foreignProjectID); err != nil {
		t.Fatalf("failed to create foreign project: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "Invalid foreign project issue",
		"project_id": foreignProjectID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateIssue with foreign project: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "project not found") {
		t.Fatalf("CreateIssue with foreign project: unexpected body %s", w.Body.String())
	}
}

func TestIssueScheduleDates(t *testing.T) {
	createStartDate := "2026-04-10T00:00:00Z"
	createEndDate := "2026-04-15T00:00:00Z"

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "Scheduled issue",
		"status":     "todo",
		"start_date": createStartDate,
		"end_date":   createEndDate,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	assertTimestampPtrEqual(t, created.StartDate, createStartDate, "CreateIssue start_date")
	assertTimestampPtrEqual(t, created.EndDate, createEndDate, "CreateIssue end_date")

	issueID := created.ID
	t.Cleanup(func() {
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/issues/"+issueID, nil)
		req = withURLParam(req, "id", issueID)
		testHandler.DeleteIssue(w, req)
	})

	// Get the issue to verify single-issue responses include schedule dates.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID, nil)
	req = withURLParam(req, "id", issueID)
	testHandler.GetIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var fetched IssueResponse
	json.NewDecoder(w.Body).Decode(&fetched)
	assertTimestampPtrEqual(t, fetched.StartDate, createStartDate, "GetIssue start_date")
	assertTimestampPtrEqual(t, fetched.EndDate, createEndDate, "GetIssue end_date")

	// List responses should also include the stored schedule dates.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID, nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var listResp struct {
		Issues []IssueResponse `json:"issues"`
	}
	json.NewDecoder(w.Body).Decode(&listResp)
	var listed *IssueResponse
	for i := range listResp.Issues {
		if listResp.Issues[i].ID == issueID {
			listed = &listResp.Issues[i]
			break
		}
	}
	if listed == nil {
		t.Fatalf("ListIssues: expected issue %s in response", issueID)
	}
	assertTimestampPtrEqual(t, listed.StartDate, createStartDate, "ListIssues start_date")
	assertTimestampPtrEqual(t, listed.EndDate, createEndDate, "ListIssues end_date")

	// Update only the start date and keep the previous end date.
	updatedStartDate := "2026-04-11T00:00:00Z"
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"start_date": updatedStartDate,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated IssueResponse
	json.NewDecoder(w.Body).Decode(&updated)
	assertTimestampPtrEqual(t, updated.StartDate, updatedStartDate, "UpdateIssue start_date")
	assertTimestampPtrEqual(t, updated.EndDate, createEndDate, "UpdateIssue end_date")

	// Clear only the end date.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"end_date": nil,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue clear end_date: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cleared IssueResponse
	json.NewDecoder(w.Body).Decode(&cleared)
	if cleared.EndDate != nil {
		t.Fatalf("UpdateIssue clear end_date: expected nil end_date, got %#v", cleared.EndDate)
	}
	assertTimestampPtrEqual(t, cleared.StartDate, updatedStartDate, "UpdateIssue clear end_date start_date")
}

func TestIssueScheduleDatesRejectInvalidRange(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "Invalid schedule",
		"start_date": "2026-04-20T00:00:00Z",
		"end_date":   "2026-04-10T00:00:00Z",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateIssue invalid range: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "start_date must be on or before end_date") {
		t.Fatalf("CreateIssue invalid range: unexpected body %s", w.Body.String())
	}

	createStartDate := "2026-04-10T00:00:00Z"
	createEndDate := "2026-04-15T00:00:00Z"
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "Valid schedule",
		"start_date": createStartDate,
		"end_date":   createEndDate,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue valid range: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	issueID := created.ID
	t.Cleanup(func() {
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/issues/"+issueID, nil)
		req = withURLParam(req, "id", issueID)
		testHandler.DeleteIssue(w, req)
	})

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"start_date": "2026-04-18T00:00:00Z",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateIssue invalid range: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "start_date must be on or before end_date") {
		t.Fatalf("UpdateIssue invalid range: unexpected body %s", w.Body.String())
	}
}

func TestListIssuesViewSemanticsAndCompatibility(t *testing.T) {
	ctx := context.Background()

	var agentID string
	err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	otherEmail := fmt.Sprintf("handler-list-%d@multica.ai", time.Now().UnixNano())
	var otherUserID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Handler List Other User", otherEmail).Scan(&otherUserID)
	if err != nil {
		t.Fatalf("failed to create secondary user: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, otherUserID); err != nil {
		t.Fatalf("failed to add secondary member: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, otherUserID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, otherUserID)
	})

	todayStart := time.Now().UTC().Truncate(24 * time.Hour)
	todayDue := todayStart.Add(12 * time.Hour).Format(time.RFC3339)
	rangeStart := todayStart.Add(-24 * time.Hour).Format(time.RFC3339)
	rangeEnd := todayStart.Add(24 * time.Hour).Format(time.RFC3339)
	upcomingStart := todayStart.Add(48 * time.Hour).Format(time.RFC3339)

	type seededIssue struct {
		id    string
		title string
	}

	seeded := map[string]seededIssue{}
	nextNumber := int32(20000)

	insertIssue := func(title, status string, assigneeType, assigneeID *string, creatorID string, dueDate, startDate, endDate *string) string {
		nextNumber++
		var issueID string
		err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, status, priority, assignee_type, assignee_id,
				creator_type, creator_id, number, position, due_date, start_date, end_date
			)
			VALUES ($1, $2, $3, 'none', $4, $5, 'member', $6, $7, $8, $9, $10, $11)
			RETURNING id
		`, testWorkspaceID, title, status, assigneeType, assigneeID, creatorID, nextNumber, float64(nextNumber), dueDate, startDate, endDate).Scan(&issueID)
		if err != nil {
			t.Fatalf("failed to insert issue %q: %v", title, err)
		}
		seeded[title] = seededIssue{id: issueID, title: title}
		return issueID
	}

	cleanupIDs := []string{
		insertIssue("List Backlog", "backlog", nil, nil, otherUserID, nil, nil, nil),
		insertIssue("List Today Due", "todo", nil, nil, otherUserID, &todayDue, nil, nil),
		insertIssue("List Today Range", "in_progress", nil, nil, otherUserID, nil, &rangeStart, &rangeEnd),
		insertIssue("List Upcoming", "todo", nil, nil, otherUserID, nil, &upcomingStart, nil),
		insertIssue("List Done Today", "done", nil, nil, otherUserID, &todayDue, nil, nil),
	}

	memberType := "member"
	agentType := "agent"
	cleanupIDs = append(cleanupIDs,
		insertIssue("List Assigned Member", "todo", &memberType, &testUserID, otherUserID, nil, nil, nil),
		insertIssue("List Assigned Agent", "todo", &agentType, &agentID, otherUserID, nil, nil, nil),
		insertIssue("List Created By Me", "todo", nil, nil, testUserID, nil, nil, nil),
	)

	t.Cleanup(func() {
		for _, issueID := range cleanupIDs {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
		}
	})

	decodeList := func(t *testing.T, recorder *httptest.ResponseRecorder) []IssueResponse {
		t.Helper()
		var resp struct {
			Issues []IssueResponse `json:"issues"`
			Total  int             `json:"total"`
		}
		if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode list response: %v", err)
		}
		return resp.Issues
	}

	assertContainsTitle := func(t *testing.T, issues []IssueResponse, title string) {
		t.Helper()
		for _, issue := range issues {
			if issue.Title == title {
				return
			}
		}
		t.Fatalf("expected response to contain title %q", title)
	}

	assertNotContainsTitle := func(t *testing.T, issues []IssueResponse, title string) {
		t.Helper()
		for _, issue := range issues {
			if issue.Title == title {
				t.Fatalf("expected response to exclude title %q", title)
			}
		}
	}

	// Compatibility: old issue listing with no new params still returns seeded issues.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID, nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues default: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	issues := decodeList(t, w)
	assertContainsTitle(t, issues, "List Backlog")
	assertContainsTitle(t, issues, "List Today Due")
	assertContainsTitle(t, issues, "List Upcoming")

	// Backlog view only returns backlog issues.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&view=backlog", nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues backlog view: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	issues = decodeList(t, w)
	assertContainsTitle(t, issues, "List Backlog")
	assertNotContainsTitle(t, issues, "List Today Due")
	assertNotContainsTitle(t, issues, "List Upcoming")

	// Today view includes due-today and overlapping scheduled issues, but not done or upcoming issues.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&view=today", nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues today view: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	issues = decodeList(t, w)
	assertContainsTitle(t, issues, "List Today Due")
	assertContainsTitle(t, issues, "List Today Range")
	assertNotContainsTitle(t, issues, "List Upcoming")
	assertNotContainsTitle(t, issues, "List Done Today")

	// Upcoming view includes future scheduled issues only.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&view=upcoming", nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues upcoming view: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	issues = decodeList(t, w)
	assertContainsTitle(t, issues, "List Upcoming")
	assertNotContainsTitle(t, issues, "List Today Due")
	assertNotContainsTitle(t, issues, "List Backlog")

	// My-work-compatible filters: creator filters and assignee type filters remain additive.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&creator_id="+testUserID+"&creator_type=member", nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues creator filters: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	issues = decodeList(t, w)
	assertContainsTitle(t, issues, "List Created By Me")
	assertNotContainsTitle(t, issues, "List Backlog")

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&assignee_id="+testUserID+"&assignee_type=member", nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues member assignee filters: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	issues = decodeList(t, w)
	assertContainsTitle(t, issues, "List Assigned Member")
	assertNotContainsTitle(t, issues, "List Assigned Agent")

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&assignee_id="+agentID+"&assignee_type=agent", nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues agent assignee filters: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	issues = decodeList(t, w)
	assertContainsTitle(t, issues, "List Assigned Agent")
	assertNotContainsTitle(t, issues, "List Assigned Member")

	// Invalid view values fail fast instead of silently changing list behavior.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&view=invalid", nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("ListIssues invalid view: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid view") {
		t.Fatalf("ListIssues invalid view: unexpected body %s", w.Body.String())
	}
}

func TestListIssuesSearchAndDateFilters(t *testing.T) {
	ctx := context.Background()
	today := time.Now().UTC().Truncate(24 * time.Hour)
	tomorrow := today.Add(24 * time.Hour)
	twoDaysLater := today.Add(48 * time.Hour)

	nextNumber := int32(26000)
	insertIssue := func(title, description string, dueDate, startDate, endDate *string) (string, int32) {
		nextNumber++
		var issueID string
		err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, description, status, priority, creator_type, creator_id,
				number, position, due_date, start_date, end_date
			)
			VALUES ($1, $2, $3, 'todo', 'none', 'member', $4, $5, $6, $7, $8, $9)
			RETURNING id
		`, testWorkspaceID, title, description, testUserID, nextNumber, float64(nextNumber), dueDate, startDate, endDate).Scan(&issueID)
		if err != nil {
			t.Fatalf("failed to insert issue %q: %v", title, err)
		}
		return issueID, nextNumber
	}

	searchTitleID, searchTitleNumber := insertIssue(
		"search-title-token",
		"plain body",
		nil,
		nil,
		nil,
	)
	searchDescriptionID, _ := insertIssue(
		"search-description-title",
		"search-description-token",
		nil,
		nil,
		nil,
	)
	dueDate := today.Add(12 * time.Hour).Format(time.RFC3339)
	startDate := tomorrow.Format(time.RFC3339)
	endDate := twoDaysLater.Format(time.RFC3339)
	dateIssueID, _ := insertIssue(
		"search-date-token",
		"date body",
		&dueDate,
		&startDate,
		&endDate,
	)

	t.Cleanup(func() {
		for _, issueID := range []string{searchTitleID, searchDescriptionID, dateIssueID} {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
		}
	})

	type issueListResponse struct {
		Issues []IssueResponse `json:"issues"`
		Total  int             `json:"total"`
	}

	decodeList := func(t *testing.T, recorder *httptest.ResponseRecorder) issueListResponse {
		t.Helper()
		var resp issueListResponse
		if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode issue list response: %v", err)
		}
		return resp
	}

	assertSingleTitle := func(t *testing.T, resp issueListResponse, title string) {
		t.Helper()
		if resp.Total != 1 {
			t.Fatalf("expected total=1, got %d", resp.Total)
		}
		if len(resp.Issues) != 1 || resp.Issues[0].Title != title {
			t.Fatalf("expected single issue %q, got %+v", title, resp.Issues)
		}
	}

	prefix := testHandler.getIssuePrefix(ctx, parseUUID(testWorkspaceID))
	identifier := fmt.Sprintf("%s-%d", prefix, searchTitleNumber)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&search=search-title-token", nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("title search: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertSingleTitle(t, decodeList(t, w), "search-title-token")

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&search=search-description-token", nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("description search: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertSingleTitle(t, decodeList(t, w), "search-description-title")

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&search="+identifier, nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("identifier search: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertSingleTitle(t, decodeList(t, w), "search-title-token")

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&due_from="+today.Format(time.DateOnly)+"&due_to="+today.Format(time.DateOnly), nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("due date search: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertSingleTitle(t, decodeList(t, w), "search-date-token")

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&start_from="+tomorrow.Format(time.DateOnly)+"&start_to="+tomorrow.Format(time.DateOnly), nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("start date search: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertSingleTitle(t, decodeList(t, w), "search-date-token")

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&due_from=2026-99-99", nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid due_from: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "due_from") {
		t.Fatalf("invalid due_from: unexpected body %s", w.Body.String())
	}
}

func TestIssueUpdatePublishesScheduleDateMetadata(t *testing.T) {
	createStartDate := "2026-04-10T00:00:00Z"
	createEndDate := "2026-04-15T00:00:00Z"

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "Metadata schedule issue",
		"start_date": createStartDate,
		"end_date":   createEndDate,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	issueID := created.ID
	t.Cleanup(func() {
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/issues/"+issueID, nil)
		req = withURLParam(req, "id", issueID)
		testHandler.DeleteIssue(w, req)
	})

	eventsCh := make(chan events.Event, 1)
	testHandler.Bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		if len(eventsCh) == 0 {
			eventsCh <- e
		}
	})

	updatedStartDate := "2026-04-12T00:00:00Z"
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"start_date": updatedStartDate,
		"end_date":   nil,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case evt := <-eventsCh:
		payload, ok := evt.Payload.(map[string]any)
		if !ok {
			t.Fatalf("expected map payload, got %T", evt.Payload)
		}
		if changed, _ := payload["start_date_changed"].(bool); !changed {
			t.Fatalf("expected start_date_changed to be true")
		}
		if changed, _ := payload["end_date_changed"].(bool); !changed {
			t.Fatalf("expected end_date_changed to be true")
		}
		prevStartDate, _ := payload["prev_start_date"].(*string)
		assertTimestampPtrEqual(t, prevStartDate, createStartDate, "prev_start_date")
		prevEndDate, _ := payload["prev_end_date"].(*string)
		assertTimestampPtrEqual(t, prevEndDate, createEndDate, "prev_end_date")
		issue, ok := payload["issue"].(IssueResponse)
		if !ok {
			t.Fatalf("expected IssueResponse payload, got %T", payload["issue"])
		}
		assertTimestampPtrEqual(t, issue.StartDate, updatedStartDate, "updated issue start_date")
		if issue.EndDate != nil {
			t.Fatalf("expected cleared end_date, got %#v", issue.EndDate)
		}
	default:
		t.Fatal("expected issue update event")
	}
}

func TestCommentCRUD(t *testing.T) {
	// Create an issue first
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Comment test issue",
	})
	testHandler.CreateIssue(w, req)
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	issueID := issue.ID

	// Create comment
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "Test comment from Go test",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// List comments
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID+"/comments", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListComments(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListComments: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var comments []CommentResponse
	json.NewDecoder(w.Body).Decode(&comments)
	if len(comments) != 1 {
		t.Fatalf("ListComments: expected 1 comment, got %d", len(comments))
	}
	if comments[0].Content != "Test comment from Go test" {
		t.Fatalf("ListComments: expected content 'Test comment from Go test', got '%s'", comments[0].Content)
	}

	// Cleanup
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/issues/"+issueID, nil)
	req = withURLParam(req, "id", issueID)
	testHandler.DeleteIssue(w, req)
}

func TestAgentCRUD(t *testing.T) {
	// List agents
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/agents?workspace_id="+testWorkspaceID, nil)
	testHandler.ListAgents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var agents []AgentResponse
	json.NewDecoder(w.Body).Decode(&agents)
	if len(agents) == 0 {
		t.Fatal("ListAgents: expected at least 1 agent")
	}

	// Update agent status
	agentID := agents[0].ID
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/agents/"+agentID, map[string]any{
		"status": "idle",
	})
	req = withURLParam(req, "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated AgentResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Status != "idle" {
		t.Fatalf("UpdateAgent: expected status 'idle', got '%s'", updated.Status)
	}
	if updated.Name != agents[0].Name {
		t.Fatalf("UpdateAgent: name should be preserved, got '%s'", updated.Name)
	}
}

func TestWorkspaceCRUD(t *testing.T) {
	// List workspaces
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/workspaces", nil)
	testHandler.ListWorkspaces(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListWorkspaces: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var workspaces []WorkspaceResponse
	json.NewDecoder(w.Body).Decode(&workspaces)
	if len(workspaces) == 0 {
		t.Fatal("ListWorkspaces: expected at least 1 workspace")
	}

	// Get workspace
	wsID := workspaces[0].ID
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testHandler.GetWorkspace(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetWorkspace: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSendCode(t *testing.T) {
	w := httptest.NewRecorder()
	body := map[string]string{"email": "sendcode-test@multica.ai"}
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["message"] == "" {
		t.Fatal("SendCode: expected non-empty message")
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM verification_code WHERE email = $1`, "sendcode-test@multica.ai")
	})
}

func TestSendCodeRateLimit(t *testing.T) {
	const email = "ratelimit-test@multica.ai"
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM verification_code WHERE email = $1`, email)
	})

	// First request should succeed
	w := httptest.NewRecorder()
	body := map[string]string{"email": email}
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendCode (first): expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Second request within 60s should be rate limited
	w = httptest.NewRecorder()
	buf.Reset()
	json.NewEncoder(&buf).Encode(body)
	req = httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("SendCode (second): expected 429, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyCode(t *testing.T) {
	const email = "verify-test@multica.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
		user, err := testHandler.Queries.GetUserByEmail(ctx, email)
		if err == nil {
			workspaces, listErr := testHandler.Queries.ListWorkspaces(ctx, user.ID)
			if listErr == nil {
				for _, workspace := range workspaces {
					_ = testHandler.Queries.DeleteWorkspace(ctx, workspace.ID)
				}
			}
		}
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})

	// Send code first
	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Read code from DB
	dbCode, err := testHandler.Queries.GetLatestVerificationCode(ctx, email)
	if err != nil {
		t.Fatalf("GetLatestVerificationCode: %v", err)
	}

	// Verify with correct code
	w = httptest.NewRecorder()
	buf.Reset()
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": dbCode.Code})
	req = httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("VerifyCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LoginResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Token == "" {
		t.Fatal("VerifyCode: expected non-empty token")
	}
	if resp.User.Email != email {
		t.Fatalf("VerifyCode: expected email '%s', got '%s'", email, resp.User.Email)
	}
}

func TestVerifyCodeWrongCode(t *testing.T) {
	const email = "wrong-code-test@multica.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
	})

	// Send code
	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)

	// Verify with wrong code
	w = httptest.NewRecorder()
	buf.Reset()
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": "000000"})
	req = httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("VerifyCode (wrong code): expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyCodeBruteForceProtection(t *testing.T) {
	const email = "bruteforce-test@multica.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
	})

	// Send code
	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Read actual code so we can try it after lockout
	dbCode, err := testHandler.Queries.GetLatestVerificationCode(ctx, email)
	if err != nil {
		t.Fatalf("GetLatestVerificationCode: %v", err)
	}

	// Exhaust all 5 attempts with wrong codes
	for i := 0; i < 5; i++ {
		w = httptest.NewRecorder()
		buf.Reset()
		json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": "000000"})
		req = httptest.NewRequest("POST", "/auth/verify-code", &buf)
		req.Header.Set("Content-Type", "application/json")
		testHandler.VerifyCode(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d: expected 400, got %d", i+1, w.Code)
		}
	}

	// Now even the correct code should be rejected (code is locked out)
	w = httptest.NewRecorder()
	buf.Reset()
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": dbCode.Code})
	req = httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("after lockout: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyCodeCreatesWorkspace(t *testing.T) {
	const email = "workspace-verify-test@multica.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
		user, err := testHandler.Queries.GetUserByEmail(ctx, email)
		if err == nil {
			workspaces, listErr := testHandler.Queries.ListWorkspaces(ctx, user.ID)
			if listErr == nil {
				for _, workspace := range workspaces {
					_ = testHandler.Queries.DeleteWorkspace(ctx, workspace.ID)
				}
			}
		}
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})

	// Send code
	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)

	// Read code from DB
	dbCode, err := testHandler.Queries.GetLatestVerificationCode(ctx, email)
	if err != nil {
		t.Fatalf("GetLatestVerificationCode: %v", err)
	}

	// Verify
	w = httptest.NewRecorder()
	buf.Reset()
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": dbCode.Code})
	req = httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("VerifyCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	user, err := testHandler.Queries.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}

	workspaces, err := testHandler.Queries.ListWorkspaces(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("ListWorkspaces: expected 1 workspace, got %d", len(workspaces))
	}
	if !strings.Contains(workspaces[0].Name, "Workspace") {
		t.Fatalf("expected auto-created workspace name, got %q", workspaces[0].Name)
	}
}

func TestResolveActor(t *testing.T) {
	ctx := context.Background()

	// Look up the agent created by the test fixture.
	var agentID string
	err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	// Create a task for the agent so we can test X-Task-ID validation.
	var issueID string
	err = testPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, position)
		 VALUES ($1, 'resolveActor test', 'todo', 'none', 'member', $2, 9999, 0)
		 RETURNING id`, testWorkspaceID, testUserID,
	).Scan(&issueID)
	if err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}

	// Look up runtime_id for the agent.
	var runtimeID string
	err = testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("failed to get agent runtime_id: %v", err)
	}

	var taskID string
	err = testPool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		 VALUES ($1, $2, $3, 'queued', 0)
		 RETURNING id`, agentID, runtimeID, issueID,
	).Scan(&taskID)
	if err != nil {
		t.Fatalf("failed to create test task: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	tests := []struct {
		name          string
		agentIDHeader string
		taskIDHeader  string
		wantActorType string
		wantIsAgent   bool
	}{
		{
			name:          "no headers returns member",
			wantActorType: "member",
		},
		{
			name:          "valid agent ID returns agent",
			agentIDHeader: agentID,
			wantActorType: "agent",
			wantIsAgent:   true,
		},
		{
			name:          "non-existent agent ID returns member",
			agentIDHeader: "00000000-0000-0000-0000-000000000099",
			wantActorType: "member",
		},
		{
			name:          "valid agent + valid task returns agent",
			agentIDHeader: agentID,
			taskIDHeader:  taskID,
			wantActorType: "agent",
			wantIsAgent:   true,
		},
		{
			name:          "valid agent + wrong task returns member",
			agentIDHeader: agentID,
			taskIDHeader:  "00000000-0000-0000-0000-000000000099",
			wantActorType: "member",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newRequest("GET", "/test", nil)
			if tt.agentIDHeader != "" {
				req.Header.Set("X-Agent-ID", tt.agentIDHeader)
			}
			if tt.taskIDHeader != "" {
				req.Header.Set("X-Task-ID", tt.taskIDHeader)
			}

			actorType, actorID := testHandler.resolveActor(req, testUserID, testWorkspaceID)

			if actorType != tt.wantActorType {
				t.Errorf("actorType = %q, want %q", actorType, tt.wantActorType)
			}
			if tt.wantIsAgent {
				if actorID != tt.agentIDHeader {
					t.Errorf("actorID = %q, want agent %q", actorID, tt.agentIDHeader)
				}
			} else {
				if actorID != testUserID {
					t.Errorf("actorID = %q, want user %q", actorID, testUserID)
				}
			}
		})
	}
}

func TestDaemonRegisterMissingWorkspaceReturns404(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/daemon/register", bytes.NewBufferString(`{
		"workspace_id":"00000000-0000-0000-0000-000000000001",
		"daemon_id":"local-daemon",
		"device_name":"test-machine",
		"runtimes":[{"name":"Local Codex","type":"codex","version":"1.0.0","status":"online"}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)

	testHandler.DaemonRegister(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("DaemonRegister: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "workspace not found") {
		t.Fatalf("DaemonRegister: expected workspace not found error, got %s", w.Body.String())
	}
}
