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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/analytics"
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
var testRuntimeID string

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
	testHandler = New(queries, pool, hub, bus, emailSvc, nil, nil, analytics.NoopClient{}, Config{AllowSignup: true})
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
	testRuntimeID = runtimeID

	if _, err := pool.Exec(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
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

func handlerTestRuntimeID(t *testing.T) string {
	t.Helper()

	var runtimeID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent_runtime WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&runtimeID); err != nil {
		t.Fatalf("failed to load handler test runtime: %v", err)
	}

	return runtimeID
}

func createHandlerTestAgent(t *testing.T, name string, mcpConfig []byte) string {
	t.Helper()

	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, name, handlerTestRuntimeID(t), testUserID, mcpConfig).Scan(&agentID); err != nil {
		t.Fatalf("failed to create handler test agent: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	return agentID
}

// createHandlerTestTaskForAgent seeds a queued agent_task_queue row for the
// given agent and returns the task UUID. Used by tests that need to set
// X-Task-ID alongside X-Agent-ID — resolveActor now requires the pair to be
// present and consistent before granting "agent" actor identity.
func createHandlerTestTaskForAgent(t *testing.T, agentID string) string {
	t.Helper()

	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
		VALUES ($1, $2, 'queued', 0)
		RETURNING id
	`, agentID, handlerTestRuntimeID(t)).Scan(&taskID); err != nil {
		t.Fatalf("failed to create handler test task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

func fetchAgentMcpConfig(t *testing.T, agentID string) []byte {
	t.Helper()

	var mcpConfig []byte
	if err := testPool.QueryRow(context.Background(), `SELECT mcp_config FROM agent WHERE id = $1`, agentID).Scan(&mcpConfig); err != nil {
		t.Fatalf("failed to load agent mcp_config: %v", err)
	}

	return mcpConfig
}

func assertJSONEqual(t *testing.T, got []byte, want string) {
	t.Helper()

	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("failed to unmarshal got JSON %q: %v", string(got), err)
	}

	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("failed to unmarshal want JSON %q: %v", want, err)
	}

	gotJSON, err := json.Marshal(gotValue)
	if err != nil {
		t.Fatalf("failed to marshal normalized got JSON: %v", err)
	}
	wantJSON, err := json.Marshal(wantValue)
	if err != nil {
		t.Fatalf("failed to marshal normalized want JSON: %v", err)
	}

	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("expected JSON %s, got %s", string(wantJSON), string(gotJSON))
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

// TestDeleteIssueByIdentifier guards against #1661 — DELETE /api/issues/{id}
// must actually delete the row when the path segment is a human-readable
// identifier ("HAN-42") rather than a UUID. Before the PR #1680 + MUL-1410
// refactor, parseUUID(rawString) silently produced a zero UUID, the SQL
// DELETE matched nothing, and the handler still returned 204.
//
// Also asserts the issue:deleted WS event payload carries the resolved UUID,
// not the raw identifier — frontend caches key by UUID and would otherwise
// leave stale entries on other clients after an identifier-path delete.
func TestDeleteIssueByIdentifier(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "Issue to delete by identifier",
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	if created.Identifier == "" {
		t.Fatalf("CreateIssue: expected identifier to be populated, got empty")
	}

	// Capture the issue:deleted event payload via the bus.
	gotPayload := make(chan map[string]any, 1)
	testHandler.Bus.Subscribe(protocol.EventIssueDeleted, func(e events.Event) {
		if payload, ok := e.Payload.(map[string]any); ok {
			select {
			case gotPayload <- payload:
			default:
			}
		}
	})

	// Delete using the human-readable identifier (e.g. "HAN-1") rather than the UUID.
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/issues/"+created.Identifier, nil)
	req = withURLParam(req, "id", created.Identifier)
	testHandler.DeleteIssue(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteIssue by identifier: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the row is actually gone — the silent-data-loss bug would have
	// returned 204 here too, but the row would still exist.
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM issue WHERE id = $1`, created.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 0 {
		t.Fatalf("DeleteIssue by identifier returned 204 but row still exists (count=%d) — silent-data-loss regression", count)
	}

	// Event payload must carry the resolved UUID, not the identifier string.
	select {
	case payload := <-gotPayload:
		issueID, _ := payload["issue_id"].(string)
		if issueID != created.ID {
			t.Fatalf("issue:deleted event payload issue_id = %q; want resolved UUID %q (must not leak identifier %q)", issueID, created.ID, created.Identifier)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive issue:deleted event within timeout")
	}
}

// TestDeleteIssueRejectsInvalidUUID verifies that a path segment that is
// neither a valid UUID nor a valid identifier returns 404 (not 204) — the
// handler must never silently succeed on malformed input.
func TestDeleteIssueRejectsInvalidUUID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/issues/not-a-uuid-or-identifier", nil)
	req = withURLParam(req, "id", "not-a-uuid-or-identifier")
	testHandler.DeleteIssue(w, req)
	if w.Code == http.StatusNoContent {
		t.Fatalf("DeleteIssue with invalid id: must not return 204; got %d", w.Code)
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("DeleteIssue with invalid id: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateIssueDefaultStatusIsTodo verifies that issues created without an
// explicit status default to "todo" so the daemon picks them up immediately.
// Before this fix the default was "backlog", which daemons ignore.
func TestCreateIssueDefaultStatusIsTodo(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Issue with no explicit status",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	if created.Status != "todo" {
		t.Fatalf("CreateIssue: expected default status 'todo', got '%s'", created.Status)
	}

	// Cleanup
	cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
	cleanupReq = withURLParam(cleanupReq, "id", created.ID)
	testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
}

// TestCreateIssueExplicitBacklogPreserved verifies that explicitly requesting
// "backlog" status is still respected — only the implicit default changed.
func TestCreateIssueExplicitBacklogPreserved(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Explicit backlog issue",
		"status": "backlog",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	if created.Status != "backlog" {
		t.Fatalf("CreateIssue: expected explicit 'backlog' to be preserved, got '%s'", created.Status)
	}

	// Cleanup
	cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
	cleanupReq = withURLParam(cleanupReq, "id", created.ID)
	testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
}

func TestCreateSubIssueInheritsParentProject(t *testing.T) {
	var projectID, parentID, childID string
	defer func() {
		for _, issueID := range []string{childID, parentID} {
			if issueID == "" {
				continue
			}
			req := newRequest("DELETE", "/api/issues/"+issueID, nil)
			req = withURLParam(req, "id", issueID)
			testHandler.DeleteIssue(httptest.NewRecorder(), req)
		}
		if projectID != "" {
			req := newRequest("DELETE", "/api/projects/"+projectID, nil)
			req = withURLParam(req, "id", projectID)
			testHandler.DeleteProject(httptest.NewRecorder(), req)
		}
	}()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Sub-issue inheritance project",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)
	projectID = project.ID

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "Parent with project",
		"project_id": projectID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parent IssueResponse
	json.NewDecoder(w.Body).Decode(&parent)
	parentID = parent.ID
	if parent.ProjectID == nil || *parent.ProjectID != projectID {
		t.Fatalf("CreateIssue parent: expected project_id %q, got %v", projectID, parent.ProjectID)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "Child without explicit project",
		"parent_issue_id": parentID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue child: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var child IssueResponse
	json.NewDecoder(w.Body).Decode(&child)
	childID = child.ID

	if child.ParentIssueID == nil || *child.ParentIssueID != parentID {
		t.Fatalf("CreateIssue child: expected parent_issue_id %q, got %v", parentID, child.ParentIssueID)
	}
	if child.ProjectID == nil || *child.ProjectID != projectID {
		t.Fatalf("CreateIssue child: expected inherited project_id %q, got %v", projectID, child.ProjectID)
	}
}

func TestCreateSubIssueUsesExplicitProjectOverParentProject(t *testing.T) {
	var parentProjectID, childProjectID, parentID, childID string
	defer func() {
		for _, issueID := range []string{childID, parentID} {
			if issueID == "" {
				continue
			}
			req := newRequest("DELETE", "/api/issues/"+issueID, nil)
			req = withURLParam(req, "id", issueID)
			testHandler.DeleteIssue(httptest.NewRecorder(), req)
		}
		for _, projectID := range []string{childProjectID, parentProjectID} {
			if projectID == "" {
				continue
			}
			req := newRequest("DELETE", "/api/projects/"+projectID, nil)
			req = withURLParam(req, "id", projectID)
			testHandler.DeleteProject(httptest.NewRecorder(), req)
		}
	}()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Parent project",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parentProject ProjectResponse
	json.NewDecoder(w.Body).Decode(&parentProject)
	parentProjectID = parentProject.ID

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Child explicit project",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject child: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var childProject ProjectResponse
	json.NewDecoder(w.Body).Decode(&childProject)
	childProjectID = childProject.ID

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "Parent with project",
		"project_id": parentProjectID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parent IssueResponse
	json.NewDecoder(w.Body).Decode(&parent)
	parentID = parent.ID
	if parent.ProjectID == nil || *parent.ProjectID != parentProjectID {
		t.Fatalf("CreateIssue parent: expected project_id %q, got %v", parentProjectID, parent.ProjectID)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "Child with explicit project",
		"parent_issue_id": parentID,
		"project_id":      childProjectID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue child: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var child IssueResponse
	json.NewDecoder(w.Body).Decode(&child)
	childID = child.ID

	if child.ParentIssueID == nil || *child.ParentIssueID != parentID {
		t.Fatalf("CreateIssue child: expected parent_issue_id %q, got %v", parentID, child.ParentIssueID)
	}
	if child.ProjectID == nil || *child.ProjectID != childProjectID {
		t.Fatalf("CreateIssue child: expected explicit project_id %q, got %v", childProjectID, child.ProjectID)
	}
}

func TestCreateIssueRejectsActiveDuplicate(t *testing.T) {
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var projectID, parentID, issueID, duplicateID string
	defer func() {
		for _, id := range []string{duplicateID, issueID, parentID} {
			if id != "" {
				testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id)
			}
		}
		if projectID != "" {
			testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID)
		}
	}()

	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, testWorkspaceID, "Duplicate guard project "+suffix).Scan(&projectID); err != nil {
		t.Fatalf("create project fixture: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Duplicate guard parent " + suffix,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parent IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&parent); err != nil {
		t.Fatalf("decode parent: %v", err)
	}
	parentID = parent.ID

	title := "SH-PM-SYNTH-01 Synthesize recommendation-to-shortlist planning outputs " + suffix
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           title,
		"status":          "in_progress",
		"parent_issue_id": parentID,
		"project_id":      projectID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue original: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var original IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&original); err != nil {
		t.Fatalf("decode original: %v", err)
	}
	issueID = original.ID

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "  sh-pm-synth-01   synthesize recommendation-to-shortlist planning outputs " + suffix + "  ",
		"parent_issue_id": parentID,
		"project_id":      projectID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("CreateIssue duplicate: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var conflict struct {
		Code  string        `json:"code"`
		Error string        `json:"error"`
		Issue IssueResponse `json:"issue"`
	}
	if err := json.NewDecoder(w.Body).Decode(&conflict); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if conflict.Code != "active_duplicate_issue" {
		t.Fatalf("code = %q, want active_duplicate_issue", conflict.Code)
	}
	if conflict.Issue.ID != issueID || conflict.Issue.Status != "in_progress" {
		t.Fatalf("conflict issue = %#v, want original %s in_progress", conflict.Issue, issueID)
	}
	if !strings.Contains(conflict.Error, original.Identifier+" "+title) || !strings.Contains(conflict.Error, "allow_duplicate=true") || !strings.Contains(conflict.Error, "--allow-duplicate") {
		t.Fatalf("unexpected duplicate message: %q", conflict.Error)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           title,
		"parent_issue_id": parentID,
		"project_id":      projectID,
		"allow_duplicate": true,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue allow duplicate: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var duplicate IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&duplicate); err != nil {
		t.Fatalf("decode duplicate: %v", err)
	}
	duplicateID = duplicate.ID
	if duplicateID == issueID {
		t.Fatalf("allow duplicate returned original issue id %s", duplicateID)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           title,
		"parent_issue_id": parentID,
		"project_id":      projectID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("CreateIssue duplicate after allow-duplicate: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&conflict); err != nil {
		t.Fatalf("decode second conflict: %v", err)
	}
	if conflict.Issue.ID != issueID {
		t.Fatalf("conflict issue = %s, want oldest active issue %s", conflict.Issue.ID, issueID)
	}
}

func TestCreateIssueAllowsDuplicateAfterCancelled(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Cancelled duplicate guard %d", time.Now().UnixNano())
	var firstID, secondID string
	defer func() {
		for _, id := range []string{secondID, firstID} {
			if id != "" {
				testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id)
			}
		}
	}()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "cancelled",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue cancelled: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var first IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&first); err != nil {
		t.Fatalf("decode cancelled: %v", err)
	}
	firstID = first.ID

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": title,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue after cancelled: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var second IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&second); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	secondID = second.ID
	if secondID == firstID {
		t.Fatalf("new issue reused cancelled issue id %s", secondID)
	}
}

func TestCreateIssueAllowsDuplicateAfterDone(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Done duplicate guard %d", time.Now().UnixNano())
	var firstID, secondID string
	defer func() {
		for _, id := range []string{secondID, firstID} {
			if id != "" {
				testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id)
			}
		}
	}()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "done",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue done: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var first IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&first); err != nil {
		t.Fatalf("decode done: %v", err)
	}
	firstID = first.ID

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": title,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue after done: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var second IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&second); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	secondID = second.ID
	if secondID == firstID {
		t.Fatalf("new issue reused done issue id %s", secondID)
	}
}

func TestTriggerAutopilotRejectsActiveDuplicateIssue(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Autopilot duplicate guard %d", time.Now().UnixNano())
	var issueID, autopilotID string
	defer func() {
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
		if issueID != "" {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue existing: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var existing IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&existing); err != nil {
		t.Fatalf("decode existing issue: %v", err)
	}
	issueID = existing.ID

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":                "Duplicate guard autopilot",
		"assignee_id":          agentID,
		"execution_mode":       "create_issue",
		"issue_title_template": title,
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var autopilot AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&autopilot); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = autopilot.ID

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/autopilots/"+autopilotID+"/trigger?workspace_id="+testWorkspaceID, nil)
	req = withURLParam(req, "id", autopilotID)
	testHandler.TriggerAutopilot(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("TriggerAutopilot duplicate: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var conflict struct {
		Code  string `json:"code"`
		Error string `json:"error"`
		Issue struct {
			ID         string `json:"id"`
			Identifier string `json:"identifier"`
			Title      string `json:"title"`
			Status     string `json:"status"`
		} `json:"issue"`
	}
	if err := json.NewDecoder(w.Body).Decode(&conflict); err != nil {
		t.Fatalf("decode duplicate conflict: %v", err)
	}
	if conflict.Code != "active_duplicate_issue" {
		t.Fatalf("code = %q, want active_duplicate_issue", conflict.Code)
	}
	if conflict.Issue.ID != issueID || conflict.Issue.Identifier != existing.Identifier || conflict.Issue.Status != "todo" {
		t.Fatalf("conflict issue = %#v, want existing %s %s todo", conflict.Issue, issueID, existing.Identifier)
	}
	if !strings.Contains(conflict.Error, "Active duplicate issue exists: "+existing.Identifier+" "+title) {
		t.Fatalf("duplicate error did not mention existing issue: %s", conflict.Error)
	}

	assertAutopilotDuplicateRunSkipped(t, ctx, autopilotID, issueID, existing.Identifier, title)
	assertAutopilotNotFailureMonitorCandidate(t, ctx, autopilotID)

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title).Scan(&count); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	if count != 1 {
		t.Fatalf("autopilot duplicate guard should leave one matching issue, got %d", count)
	}
}

func TestScheduledAutopilotDuplicateIssueSkipsRun(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Scheduled autopilot duplicate guard %d", time.Now().UnixNano())
	var issueID, autopilotID string
	defer func() {
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
		if issueID != "" {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue existing: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var existing IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&existing); err != nil {
		t.Fatalf("decode existing issue: %v", err)
	}
	issueID = existing.ID

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":                "Scheduled duplicate guard autopilot",
		"assignee_id":          agentID,
		"execution_mode":       "create_issue",
		"issue_title_template": title,
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var autopilot AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&autopilot); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = autopilot.ID

	queries := db.New(testPool)
	ap, err := queries.GetAutopilot(ctx, parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}
	run, err := testHandler.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "schedule", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot schedule duplicate: %v", err)
	}
	if run == nil {
		t.Fatal("expected skipped run, got nil")
	}
	if run.Status != "skipped" {
		t.Fatalf("run status = %q, want skipped", run.Status)
	}
	assertAutopilotDuplicateRunSkipped(t, ctx, autopilotID, issueID, existing.Identifier, title)
	assertAutopilotNotFailureMonitorCandidate(t, ctx, autopilotID)
}

func assertAutopilotDuplicateRunSkipped(t *testing.T, ctx context.Context, autopilotID, issueID, identifier, title string) {
	t.Helper()
	var status, failureReason string
	var result []byte
	if err := testPool.QueryRow(ctx, `
		SELECT status, failure_reason, result
		FROM autopilot_run
		WHERE autopilot_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, autopilotID).Scan(&status, &failureReason, &result); err != nil {
		t.Fatalf("load autopilot run: %v", err)
	}
	if status != "skipped" {
		t.Fatalf("autopilot duplicate run status = %q, want skipped", status)
	}
	if !strings.Contains(failureReason, identifier+" "+title) {
		t.Fatalf("duplicate run failure_reason = %q, want existing issue details", failureReason)
	}
	var payload struct {
		Code  string `json:"code"`
		Issue struct {
			ID         string `json:"id"`
			Identifier string `json:"identifier"`
			Title      string `json:"title"`
			Status     string `json:"status"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("decode duplicate run result: %v", err)
	}
	if payload.Code != "active_duplicate_issue" || payload.Issue.ID != issueID || payload.Issue.Identifier != identifier || payload.Issue.Title != title {
		t.Fatalf("duplicate run result = %#v, want issue %s %s %q", payload, issueID, identifier, title)
	}
}

func assertAutopilotNotFailureMonitorCandidate(t *testing.T, ctx context.Context, autopilotID string) {
	t.Helper()
	candidates, err := db.New(testPool).SelectAutopilotsExceedingFailureThreshold(ctx, db.SelectAutopilotsExceedingFailureThresholdParams{
		MinRuns:            1,
		FailRatioThreshold: 1,
		Since:              pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
	})
	if err != nil {
		t.Fatalf("SelectAutopilotsExceedingFailureThreshold: %v", err)
	}
	for _, candidate := range candidates {
		if uuidToString(candidate.ID) == autopilotID {
			t.Fatalf("duplicate skipped run should not be a failure monitor candidate: %+v", candidate)
		}
	}
}

// TestCreateIssueRejectsNonexistentMemberAssignee covers the bug where any
// well-formed UUID was accepted as assignee_id without checking workspace
// membership.
func TestCreateIssueRejectsNonexistentMemberAssignee(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Ghost member assignee",
		"assignee_type": "member",
		"assignee_id":   "00000000-0000-0000-0000-000000000000",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateIssue: expected 400 for nonexistent member, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateIssueRejectsNonexistentAgentAssignee verifies the same check on
// the agent branch — previously rejected with 403 "agent not found"; we want a
// consistent 400 from the new validator.
func TestCreateIssueRejectsNonexistentAgentAssignee(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Ghost agent assignee",
		"assignee_type": "agent",
		"assignee_id":   "00000000-0000-0000-0000-000000000000",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateIssue: expected 400 for nonexistent agent, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateIssueRejectsAssigneeTypeWithoutID rejects requests where only one
// of the two fields was supplied — historically this would create an issue
// with an inconsistent state.
func TestCreateIssueRejectsAssigneeTypeWithoutID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Lone assignee_type",
		"assignee_type": "member",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateIssue: expected 400 when only assignee_type is set, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateIssueRejectsAssigneeIDWithoutType is the symmetric case.
func TestCreateIssueRejectsAssigneeIDWithoutType(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Lone assignee_id",
		"assignee_id": testUserID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateIssue: expected 400 when only assignee_id is set, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateIssueRejectsUnknownAssigneeType guards against typos like
// "members" or "user" that previously sneaked through.
func TestCreateIssueRejectsUnknownAssigneeType(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Bogus assignee_type",
		"assignee_type": "user",
		"assignee_id":   testUserID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateIssue: expected 400 for unknown assignee_type, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateIssueAcceptsValidMemberAssignee is the positive control — the
// validator must not block legitimate workspace members.
func TestCreateIssueAcceptsValidMemberAssignee(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Valid member assignee",
		"assignee_type": "member",
		"assignee_id":   testUserID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201 for valid member assignee, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
	cleanupReq = withURLParam(cleanupReq, "id", created.ID)
	testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
}

// TestCreateIssueRejectsMalformedAssigneeID covers the case where parseUUID
// silently produces an invalid pgtype.UUID and the validator would otherwise
// treat (no type + unparseable id) as "no assignee" and accept the request.
func TestCreateIssueRejectsMalformedAssigneeID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Malformed assignee_id only",
		"assignee_id": "not-a-uuid",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateIssue: expected 400 for malformed assignee_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateIssueRejectsMalformedAttachmentIDBeforeWrite(t *testing.T) {
	var before int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM issue WHERE workspace_id = $1`, testWorkspaceID).Scan(&before); err != nil {
		t.Fatalf("count issues before: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Malformed attachment issue",
		"attachment_ids": []string{"not-a-uuid"},
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateIssue: expected 400 for malformed attachment_ids, got %d: %s", w.Code, w.Body.String())
	}

	var after int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM issue WHERE workspace_id = $1`, testWorkspaceID).Scan(&after); err != nil {
		t.Fatalf("count issues after: %v", err)
	}
	if after != before {
		t.Fatalf("CreateIssue: malformed attachment_ids should not create issue, count before=%d after=%d", before, after)
	}
}

// TestUpdateIssueRejectsMalformedAssigneeID is the equivalent for the update
// path, where the same parseUUID-shaped gap existed on a previously-unassigned
// issue.
func TestUpdateIssueRejectsMalformedAssigneeID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Update malformed assignee target",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	defer func() {
		cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
	}()

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+created.ID, map[string]any{
		"assignee_id": "not-a-uuid",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateIssue: expected 400 for malformed assignee_id, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateIssueRejectsNonexistentMemberAssignee verifies the same gap is
// closed on the update path — UpdateIssue previously only validated agents.
func TestUpdateIssueRejectsNonexistentMemberAssignee(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Update assignee target",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	defer func() {
		cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
	}()

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+created.ID, map[string]any{
		"assignee_type": "member",
		"assignee_id":   "00000000-0000-0000-0000-000000000000",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateIssue: expected 400 for nonexistent member, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateIssueAllowsExplicitUnassign verifies that sending null for both
// fields still works after the new validator landed — clearing the assignee
// must not be misclassified as a mismatched pair.
func TestUpdateIssueAllowsExplicitUnassign(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Issue to unassign",
		"assignee_type": "member",
		"assignee_id":   testUserID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	defer func() {
		cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
	}()

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+created.ID, map[string]any{
		"assignee_type": nil,
		"assignee_id":   nil,
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue: expected 200 for unassign, got %d: %s", w.Code, w.Body.String())
	}
	var updated IssueResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.AssigneeType != nil || updated.AssigneeID != nil {
		t.Fatalf("UpdateIssue: expected assignee cleared, got type=%v id=%v", updated.AssigneeType, updated.AssigneeID)
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

func TestCreateCommentRejectsMalformedParentID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Comment malformed parent issue",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issue.ID+"/comments", map[string]any{
		"content":   "bad parent",
		"parent_id": "not-a-uuid",
	})
	req = withURLParam(req, "id", issue.ID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateComment: expected 400 for malformed parent_id, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/issues/"+issue.ID, nil)
	req = withURLParam(req, "id", issue.ID)
	testHandler.DeleteIssue(w, req)
}

func TestGetChatSessionRejectsMalformedSessionID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/chat/sessions/not-a-uuid", nil)
	req = withURLParam(req, "sessionId", "not-a-uuid")
	testHandler.GetChatSession(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("GetChatSession: expected 400 for malformed sessionId, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAutopilotRejectsMalformedAssigneeID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots", map[string]any{
		"title":          "Malformed assignee autopilot",
		"assignee_id":    "not-a-uuid",
		"execution_mode": "run_only",
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateAutopilot: expected 400 for malformed assignee_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAutopilotRejectsMalformedID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/autopilots/not-a-uuid", map[string]any{
		"title": "Malformed autopilot id",
	})
	req = withURLParam(req, "id", "not-a-uuid")
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateAutopilot: expected 400 for malformed id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgentRejectsMalformedAgentID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/agents/not-a-uuid", map[string]any{
		"name": "Malformed agent id",
	})
	req = withURLParam(req, "id", "not-a-uuid")
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateAgent: expected 400 for malformed id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgentRejectsMalformedRuntimeID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/agents", map[string]any{
		"name":       "Malformed runtime agent",
		"runtime_id": "not-a-uuid",
	})
	testHandler.CreateAgent(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateAgent: expected 400 for malformed runtime_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgentRejectsMalformedRuntimeID(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Malformed Runtime Update", nil)

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/agents/"+agentID, map[string]any{
		"runtime_id": "not-a-uuid",
	})
	req = withURLParam(req, "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateAgent: expected 400 for malformed runtime_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreatePinRejectsMalformedItemID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/pins", map[string]any{
		"item_type": "issue",
		"item_id":   "not-a-uuid",
	})
	testHandler.CreatePin(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreatePin: expected 400 for malformed item_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateWorkspaceRejectsMalformedID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/workspaces/not-a-uuid", map[string]any{
		"name": "Malformed workspace id",
	})
	req = withURLParam(req, "id", "not-a-uuid")
	testHandler.UpdateWorkspace(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateWorkspace: expected 400 for malformed id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateMemberRejectsMalformedMemberID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/workspaces/"+testWorkspaceID+"/members/not-a-uuid", map[string]any{
		"role": "member",
	})
	req = withURLParams(req, "id", testWorkspaceID, "memberId", "not-a-uuid")
	testHandler.UpdateMember(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateMember: expected 400 for malformed memberId, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRevokeInvitationRejectsMalformedInvitationID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+testWorkspaceID+"/invitations/not-a-uuid", nil)
	req = withURLParams(req, "id", testWorkspaceID, "invitationId", "not-a-uuid")
	testHandler.RevokeInvitation(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("RevokeInvitation: expected 400 for malformed invitationId, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetMyInvitationRejectsMalformedID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/invitations/not-a-uuid", nil)
	req = withURLParam(req, "id", "not-a-uuid")
	testHandler.GetMyInvitation(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("GetMyInvitation: expected 400 for malformed id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddReactionRejectsMalformedCommentID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/comments/not-a-uuid/reactions", map[string]any{
		"emoji": "thumbs_up",
	})
	req = withURLParam(req, "commentId", "not-a-uuid")
	testHandler.AddReaction(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("AddReaction: expected 400 for malformed commentId, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateCommentRejectsMalformedCommentID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/comments/not-a-uuid", map[string]any{
		"content": "updated",
	})
	req = withURLParam(req, "commentId", "not-a-uuid")
	testHandler.UpdateComment(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateComment: expected 400 for malformed commentId, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMarkInboxReadRejectsMalformedItemID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/inbox/not-a-uuid/read", nil)
	req = withURLParam(req, "id", "not-a-uuid")
	testHandler.MarkInboxRead(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("MarkInboxRead: expected 400 for malformed id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRevokePersonalAccessTokenRejectsMalformedID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/tokens/not-a-uuid", nil)
	req = withURLParam(req, "id", "not-a-uuid")
	testHandler.RevokePersonalAccessToken(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("RevokePersonalAccessToken: expected 400 for malformed id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequestBodyUUIDFieldsRejectMalformed(t *testing.T) {
	tests := []struct {
		name   string
		req    *http.Request
		handle func(http.ResponseWriter, *http.Request)
	}{
		{
			name: "daemon register workspace_id",
			req: newRequest("POST", "/api/daemon/register", map[string]any{
				"workspace_id": "not-a-uuid",
				"daemon_id":    "daemon-malformed-workspace",
				"runtimes": []map[string]any{
					{"name": "codex", "type": "codex", "status": "online"},
				},
			}),
			handle: testHandler.DaemonRegister,
		},
		{
			name: "import starter content workspace_id",
			req: newRequest("POST", "/api/onboarding/starter-content/import", map[string]any{
				"workspace_id": "not-a-uuid",
				"project": map[string]any{
					"title": "Getting Started",
				},
			}),
			handle: testHandler.ImportStarterContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tt.handle(w, tt.req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%s: expected 400 for malformed body UUID, got %d: %s", tt.name, w.Code, w.Body.String())
			}
		})
	}
}

func TestDaemonDeregisterRejectsMalformedRuntimeID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/daemon/deregister", map[string]any{
		"runtime_ids": []string{"not-a-uuid"},
	})
	testHandler.DaemonDeregister(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("DaemonDeregister: expected 400 for malformed runtime_ids, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetIssueGCCheckRejectsMalformedIssueID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/daemon/issues/not-a-uuid/gc-check", nil)
	req = withURLParam(req, "issueId", "not-a-uuid")
	testHandler.GetIssueGCCheck(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("GetIssueGCCheck: expected 400 for malformed issueId, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSetAgentSkillsRejectsMalformedSkillID(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Malformed Skill Assignment", nil)

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/agents/"+agentID+"/skills", map[string]any{
		"skill_ids": []string{"not-a-uuid"},
	})
	req = withURLParam(req, "id", agentID)
	testHandler.SetAgentSkills(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("SetAgentSkills: expected 400 for malformed skill_ids, got %d: %s", w.Code, w.Body.String())
	}
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

func TestUpdateAgentMcpConfigAbsentPreservesValue(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Mcp Preserve", []byte(`{"preset":"keep"}`))

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/agents/"+agentID, map[string]any{
		"name": "Handler Mcp Preserve Updated",
	})
	req = withURLParam(req, "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("UpdateAgent: decode response: %v", err)
	}
	assertJSONEqual(t, updated.McpConfig, `{"preset":"keep"}`)
	assertJSONEqual(t, fetchAgentMcpConfig(t, agentID), `{"preset":"keep"}`)
}

func TestUpdateAgentMcpConfigNullClearsValue(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Mcp Clear", []byte(`{"preset":"clear"}`))

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/agents/"+agentID, map[string]any{
		"mcp_config": nil,
	})
	req = withURLParam(req, "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("UpdateAgent: decode response: %v", err)
	}
	assertJSONEqual(t, updated.McpConfig, `null`)
	if fetchAgentMcpConfig(t, agentID) != nil {
		t.Fatalf("UpdateAgent: expected DB mcp_config to be SQL NULL")
	}
}

func TestUpdateAgentMcpConfigObjectUpdatesValue(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Mcp Update", []byte(`{"preset":"old"}`))

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/agents/"+agentID, map[string]any{
		"mcp_config": map[string]any{"preset": "new"},
	})
	req = withURLParam(req, "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("UpdateAgent: decode response: %v", err)
	}
	assertJSONEqual(t, updated.McpConfig, `{"preset":"new"}`)
	assertJSONEqual(t, fetchAgentMcpConfig(t, agentID), `{"preset":"new"}`)
}

func TestCreateAgentMcpConfigNullStoresSQLNull(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/agents", map[string]any{
		"name":        "Handler Mcp Create Null",
		"runtime_id":  handlerTestRuntimeID(t),
		"mcp_config":  nil,
		"custom_env":  map[string]string{},
		"custom_args": []string{},
	})
	testHandler.CreateAgent(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgent: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("CreateAgent: decode response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, created.ID)
	})

	assertJSONEqual(t, created.McpConfig, `null`)
	if fetchAgentMcpConfig(t, created.ID) != nil {
		t.Fatalf("CreateAgent: expected DB mcp_config to be SQL NULL")
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

func TestCreateWorkspaceUsesRequestedSlug(t *testing.T) {
	const slug = "handler-create-workspace-requested"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces", map[string]string{
		"name": "Handler Create Workspace Requested",
		"slug": slug,
	})
	testHandler.CreateWorkspace(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkspace: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created WorkspaceResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("CreateWorkspace: decode response: %v", err)
	}
	if created.Slug != slug {
		t.Fatalf("CreateWorkspace: expected slug %q, got %q", slug, created.Slug)
	}
}

func TestCreateWorkspaceSlugConflictReturnsConflict(t *testing.T) {
	ctx := context.Background()
	retriedSlug := handlerTestWorkspaceSlug + "-2"

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, retriedSlug)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces", map[string]string{
		"name": "Duplicate Handler Workspace",
		"slug": handlerTestWorkspaceSlug,
	})
	testHandler.CreateWorkspace(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("CreateWorkspace: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workspace WHERE slug = $1`, retriedSlug).Scan(&count); err != nil {
		t.Fatalf("CreateWorkspace: check retried slug: %v", err)
	}
	if count != 0 {
		t.Fatalf("CreateWorkspace: expected no fallback slug %q, got %d rows", retriedSlug, count)
	}
}

func TestCreateWorkspaceInvalidSlugReturnsBadRequest(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces", map[string]string{
		"name": "Invalid Slug Workspace",
		"slug": "invalid slug",
	})
	testHandler.CreateWorkspace(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateWorkspace: expected 400, got %d: %s", w.Code, w.Body.String())
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

func TestSendCodeDbError(t *testing.T) {
	// We can't easily mock the DB here without changing architecture,
	// but we can simulate a DB error by closing the pool temporarily or
	// using a cancelled context if the query respects it.

	// Create a handler with a "broken" queries object is hard because it's a struct.
	// Instead, let's use a context that is already cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w := httptest.NewRecorder()
	body := map[string]string{"email": "dberror-test@multica.ai"}
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctx)

	testHandler.SendCode(w, req)

	// If the DB query respects the cancelled context, it should return an error.
	// pgx usually returns context.Canceled which is not what isNotFound checks for.
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("SendCode (db error): expected 500, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "failed to lookup user" {
		t.Fatalf("SendCode (db error): expected error message 'failed to lookup user', got '%s'", resp["error"])
	}
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

func createVerificationCodeForTest(t *testing.T, email, code string) {
	t.Helper()

	_, err := testPool.Exec(context.Background(), `
		INSERT INTO verification_code (email, code, expires_at)
		VALUES ($1, $2, now() + interval '10 minutes')
	`, email, code)
	if err != nil {
		t.Fatalf("create verification code: %v", err)
	}
}

func TestVerifyCodeRejectsDevCodeUnlessExplicitlyConfigured(t *testing.T) {
	t.Setenv(devVerificationCodeEnv, "")
	t.Setenv("APP_ENV", "")

	const email = "dev-code-disabled-test@multica.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
	})

	createVerificationCodeForTest(t, email, "123456")

	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": "888888"})
	req := httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("VerifyCode (disabled dev code): expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyCodeAcceptsConfiguredDevCodeOutsideProduction(t *testing.T) {
	t.Setenv(devVerificationCodeEnv, "888888")
	t.Setenv("APP_ENV", "development")

	const email = "dev-code-enabled-test@multica.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})

	createVerificationCodeForTest(t, email, "123456")

	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": "888888"})
	req := httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("VerifyCode (enabled dev code): expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyCodeRejectsConfiguredDevCodeInProduction(t *testing.T) {
	t.Setenv(devVerificationCodeEnv, "888888")
	t.Setenv("APP_ENV", "production")

	const email = "dev-code-production-test@multica.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
	})

	createVerificationCodeForTest(t, email, "123456")

	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": "888888"})
	req := httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("VerifyCode (production dev code): expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyCodeWrongCode(t *testing.T) {
	t.Setenv(devVerificationCodeEnv, "")

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
	t.Setenv(devVerificationCodeEnv, "")

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

func TestVerifyCodeNewUserHasNoWorkspace(t *testing.T) {
	const email = "workspace-verify-test@multica.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
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

	// New users should have no workspaces (/workspaces/new creates one)
	workspaces, err := testHandler.Queries.ListWorkspaces(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(workspaces) != 0 {
		t.Fatalf("ListWorkspaces: expected 0 workspaces for new user, got %d", len(workspaces))
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
			// X-Agent-ID without X-Task-ID is not trusted — otherwise a
			// workspace member who guesses an agent's UUID could impersonate
			// it and bypass the private-agent gate. See resolveActor for the
			// rationale.
			name:          "agent ID without task ID returns member",
			agentIDHeader: agentID,
			wantActorType: "member",
		},
		{
			name:          "non-existent agent ID with task returns member",
			agentIDHeader: "00000000-0000-0000-0000-000000000099",
			taskIDHeader:  taskID,
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

// TestBacklogNoTriggerOnCreate verifies that creating a backlog issue with an
// agent assignee does NOT enqueue a task — backlog is a parking lot.
func TestBacklogNoTriggerOnCreate(t *testing.T) {
	ctx := context.Background()

	var agentID string
	err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Backlog no-trigger test",
		"status":        "backlog",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	var taskCount int
	err = testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`,
		created.ID,
	).Scan(&taskCount)
	if err != nil {
		t.Fatalf("failed to count tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected no tasks for backlog issue on creation, got %d", taskCount)
	}

	// Cleanup
	cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
	cleanupReq = withURLParam(cleanupReq, "id", created.ID)
	testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
}

// TestBacklogToTodoTriggersAgent verifies that moving an agent-assigned issue
// from "backlog" to "todo" enqueues exactly one agent task (none on creation,
// one on status transition).
func TestBacklogToTodoTriggersAgent(t *testing.T) {
	ctx := context.Background()

	var agentID string
	err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	// Create a backlog issue assigned to the agent — should NOT trigger.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Backlog trigger test",
		"status":        "backlog",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	// Move the issue from backlog to todo — should trigger.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+created.ID, map[string]any{
		"status": "todo",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify exactly one task was enqueued (from the status transition, not creation).
	var taskCount int
	err = testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
		created.ID, agentID,
	).Scan(&taskCount)
	if err != nil {
		t.Fatalf("failed to count tasks: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("expected exactly 1 task after backlog->todo transition, got %d", taskCount)
	}

	// Cleanup
	testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, created.ID)
	cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
	cleanupReq = withURLParam(cleanupReq, "id", created.ID)
	testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
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

// TestAgentReplyDoesNotInheritParentMentions verifies that agent-authored
// replies do NOT inherit parent-comment mentions, preventing agent-to-agent
// re-trigger loops (e.g. "No reply needed" chains). Member-authored replies
// still inherit parent mentions as expected.
func TestAgentReplyDoesNotInheritParentMentions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Create two agents.
	agentA := createHandlerTestAgent(t, "Loop Agent A", nil)
	agentB := createHandlerTestAgent(t, "Loop Agent B", nil)

	// Create an unassigned issue so on_comment doesn't fire.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Agent mention inheritance test",
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	issueID := issue.ID

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Helper: count queued tasks for a given agent on this issue.
	countTasks := func(agentID string) int {
		var n int
		err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
			issueID, agentID,
		).Scan(&n)
		if err != nil {
			t.Fatalf("failed to count tasks: %v", err)
		}
		return n
	}

	// Helper: cancel all tasks for an agent on this issue.
	cancelTasks := func(agentID string) {
		_, err := testPool.Exec(ctx,
			`UPDATE agent_task_queue SET status = 'cancelled' WHERE issue_id = $1 AND agent_id = $2`,
			issueID, agentID,
		)
		if err != nil {
			t.Fatalf("failed to cancel tasks: %v", err)
		}
	}

	postComment := func(issueID string, body map[string]any, headers map[string]string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/issues/"+issueID+"/comments", body)
		r = withURLParam(r, "id", issueID)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		testHandler.CreateComment(w, r)
		return w
	}

	// 1. Member posts top-level comment mentioning Agent B.
	mentionB := fmt.Sprintf("[@Agent B](mention://agent/%s) please review", agentB)
	w = postComment(issueID, map[string]any{"content": mentionB}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("member mention comment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parentComment CommentResponse
	json.NewDecoder(w.Body).Decode(&parentComment)
	if countTasks(agentB) != 1 {
		t.Fatalf("expected 1 task for Agent B after member mention, got %d", countTasks(agentB))
	}

	// 2. Cancel Agent B's task so it's free to be re-triggered.
	cancelTasks(agentB)
	if countTasks(agentB) != 0 {
		t.Fatalf("expected 0 tasks for Agent B after cancel, got %d", countTasks(agentB))
	}

	// 3. Agent A posts a reply in the same thread with NO mentions.
	// With the fix, this must NOT inherit the parent mention of Agent B.
	// resolveActor requires X-Task-ID paired with X-Agent-ID to trust the
	// agent identity, so we seed a task that belongs to agent A.
	agentATask := createHandlerTestTaskForAgent(t, agentA)
	w = postComment(issueID, map[string]any{
		"content":   "No reply needed — just an acknowledgment.",
		"parent_id": parentComment.ID,
	}, map[string]string{"X-Agent-ID": agentA, "X-Task-ID": agentATask})
	if w.Code != http.StatusCreated {
		t.Fatalf("agent A reply: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if countTasks(agentB) != 0 {
		t.Fatalf("expected 0 tasks for Agent B after agent reply (no parent inheritance), got %d", countTasks(agentB))
	}

	// 4. Cancel any stray tasks.
	cancelTasks(agentB)

	// 5. Member posts a reply in the same thread with NO mentions.
	// This SHOULD inherit the parent mention and re-trigger Agent B.
	w = postComment(issueID, map[string]any{
		"content":   "Thanks for the review.",
		"parent_id": parentComment.ID,
	}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("member reply: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if countTasks(agentB) != 1 {
		t.Fatalf("expected 1 task for Agent B after member reply (parent inheritance allowed), got %d", countTasks(agentB))
	}
}

// TestMemberReplyToAgentRootDoesNotInheritParentMentions is the regression
// for MUL-1535. When an agent posts a comment that @mentions another agent
// (e.g. J posting a PR completion that @mentions a reviewer agent), a later
// member reply in the same thread with no explicit mentions must NOT inherit
// the @reviewer mention. The reviewer was a one-shot delegation; subsequent
// member follow-ups are directed at the assignee, not the reviewer.
func TestMemberReplyToAgentRootDoesNotInheritParentMentions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	jAgent := createHandlerTestAgent(t, "J", nil)
	reviewerAgent := createHandlerTestAgent(t, "Reviewer", nil)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "PR review delegation no-leak test",
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	issueID := issue.ID

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	countTasks := func(agentID string) int {
		var n int
		err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
			issueID, agentID,
		).Scan(&n)
		if err != nil {
			t.Fatalf("failed to count tasks: %v", err)
		}
		return n
	}

	// 1. Agent J posts a PR-completion comment that @mentions Reviewer for review.
	// This is a deliberate handoff and must enqueue a task for Reviewer.
	// X-Task-ID is required alongside X-Agent-ID for resolveActor to grant
	// the "agent" actor identity (defense against header forgery).
	jAgentTask := createHandlerTestTaskForAgent(t, jAgent)
	w = httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": fmt.Sprintf("PR ready. [@Reviewer](mention://agent/%s) please review this.", reviewerAgent),
	})
	r = withURLParam(r, "id", issueID)
	r.Header.Set("X-Agent-ID", jAgent)
	r.Header.Set("X-Task-ID", jAgentTask)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("J PR completion: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var rootComment CommentResponse
	json.NewDecoder(w.Body).Decode(&rootComment)
	if got := countTasks(reviewerAgent); got != 1 {
		t.Fatalf("expected 1 task for Reviewer after explicit mention, got %d", got)
	}

	// Cancel reviewer's task so it's free to be re-triggered if the bug returns.
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'cancelled' WHERE issue_id = $1 AND agent_id = $2`,
		issueID, reviewerAgent,
	); err != nil {
		t.Fatalf("cancel reviewer task: %v", err)
	}

	// 2. Member posts a plain follow-up reply under J's PR comment, with no
	// explicit mentions. The pre-fix code path inherited mentions from the
	// parent regardless of the parent author, which re-triggered Reviewer.
	// With the fix, the reply must NOT inherit because the parent was
	// authored by an agent.
	w = httptest.NewRecorder()
	r = newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content":   "How do I test this after merging?",
		"parent_id": rootComment.ID,
	})
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("member follow-up: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if got := countTasks(reviewerAgent); got != 0 {
		t.Fatalf("expected 0 tasks for Reviewer after plain member reply (no inheritance from agent root), got %d", got)
	}
}

// TestAgentExplicitMentionStillTriggers documents the boundary the structural
// fix preserves: suppressing implicit parent-mention inheritance for agent
// authors does NOT block deliberate handoffs. An agent that explicitly
// @mentions another agent in its own comment content still enqueues a task
// for that mentioned agent.
func TestAgentExplicitMentionStillTriggers(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentA := createHandlerTestAgent(t, "Handoff Agent A", nil)
	agentB := createHandlerTestAgent(t, "Handoff Agent B", nil)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Agent explicit handoff test",
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	issueID := issue.ID

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	countTasks := func(agentID string) int {
		var n int
		err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
			issueID, agentID,
		).Scan(&n)
		if err != nil {
			t.Fatalf("failed to count tasks: %v", err)
		}
		return n
	}

	// Agent A posts a top-level comment that explicitly @mentions Agent B —
	// a deliberate handoff. This must enqueue a task for Agent B, and must
	// not enqueue a self-trigger for Agent A. resolveActor requires
	// X-Task-ID to grant "agent" identity; without it the self-trigger
	// suppression (authorType=="agent") would not fire.
	agentATask := createHandlerTestTaskForAgent(t, agentA)
	explicitMention := fmt.Sprintf("[@Agent B](mention://agent/%s) please take it from here", agentB)
	w = httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": explicitMention,
	})
	r = withURLParam(r, "id", issueID)
	r.Header.Set("X-Agent-ID", agentA)
	r.Header.Set("X-Task-ID", agentATask)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("agent A handoff: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if got := countTasks(agentB); got != 1 {
		t.Fatalf("expected 1 task for Agent B after explicit mention by Agent A, got %d", got)
	}
	if got := countTasks(agentA); got != 0 {
		t.Fatalf("expected 0 tasks for Agent A (no self-trigger on own mention), got %d", got)
	}
}
