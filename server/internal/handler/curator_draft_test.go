package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// setupCuratorDraftServices creates a CuratorDraftTaskService (with nil Curator)
// and sets it on the shared testHandler. Returns a cleanup function.
func setupCuratorDraftServices(t *testing.T) func() {
	t.Helper()

	prev := testHandler.CuratorDraftService
	testHandler.CuratorDraftService = service.NewCuratorDraftTaskService(testHandler.Queries, nil)
	return func() {
		testHandler.CuratorDraftService = prev
	}
}

// createCuratorDraftTaskFixture inserts a curator_draft_task row for testing.
func createCuratorDraftTaskFixture(t *testing.T, workspaceID, runtimeID string, draftKind, status string, inputData []byte) string {
	t.Helper()

	// Look up the member ID for the test user in this workspace.
	var memberID string
	err := testPool.QueryRow(context.Background(),
		`SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`,
		workspaceID, testUserID,
	).Scan(&memberID)
	if err != nil {
		t.Fatalf("lookup test member: %v", err)
	}

	var taskID string
	err = testPool.QueryRow(context.Background(), `
		INSERT INTO curator_draft_task (workspace_id, runtime_id, draft_kind, status, input_data, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, workspaceID, runtimeID, draftKind, status, inputData, memberID).Scan(&taskID)
	if err != nil {
		t.Fatalf("create curator draft task fixture: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM curator_draft_task WHERE id = $1`, taskID)
	})
	return taskID
}

// createTestRuntimeFixture creates a second runtime for cross-runtime tests.
func createTestRuntimeFixture(t *testing.T, workspaceID, name, runtimeMode string) string {
	t.Helper()

	var runtimeID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, $2, $3, 'test_provider', 'online', '{}'::jsonb, '{}'::jsonb, now())
		RETURNING id
	`, workspaceID, name, runtimeMode).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create test runtime fixture: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func validDraftBody() map[string]any {
	return map[string]any{
		"draft": map[string]any{
			"title":                "Test Draft",
			"type":                 "lesson",
			"domain_labels":        []string{"testing"},
			"problem_pattern":      "test pattern",
			"trigger_conditions":   "test conditions",
			"diagnostic_steps":     "test steps",
			"recommended_practice": "test practice",
			"anti_patterns":        "test anti patterns",
			"applicability":        "test applicability",
			"confidence_status":    "high",
		},
	}
}

// ---------------------------------------------------------------------------
// Cross-runtime rejection tests (Problem 3)
// ---------------------------------------------------------------------------

func TestCompleteCuratorDraftTask_WrongRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	cleanup := setupCuratorDraftServices(t)
	defer cleanup()

	runtimeA := testRuntimeID
	taskID := createCuratorDraftTaskFixture(t, testWorkspaceID, runtimeA, "issue", "running", []byte(`{}`))

	runtimeB := createTestRuntimeFixture(t, testWorkspaceID, "cross-runtime-test-complete", "cloud")

	req := newDaemonTokenRequest(http.MethodPost,
		fmt.Sprintf("/api/daemon/runtimes/%s/curator-drafts/%s/complete", runtimeB, taskID),
		validDraftBody(), testWorkspaceID, "test-daemon")
	req = withURLParam(req, "runtimeId", runtimeB)
	req = withURLParam(req, "taskId", taskID)

	rec := httptest.NewRecorder()
	testHandler.CompleteCuratorDraftTask(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for wrong runtime complete, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFailCuratorDraftTask_WrongRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	cleanup := setupCuratorDraftServices(t)
	defer cleanup()

	runtimeA := testRuntimeID
	taskID := createCuratorDraftTaskFixture(t, testWorkspaceID, runtimeA, "issue", "running", []byte(`{}`))

	runtimeB := createTestRuntimeFixture(t, testWorkspaceID, "cross-runtime-test-fail", "cloud")

	req := newDaemonTokenRequest(http.MethodPost,
		fmt.Sprintf("/api/daemon/runtimes/%s/curator-drafts/%s/fail", runtimeB, taskID),
		map[string]any{"error": "test failure"}, testWorkspaceID, "test-daemon")
	req = withURLParam(req, "runtimeId", runtimeB)
	req = withURLParam(req, "taskId", taskID)

	rec := httptest.NewRecorder()
	testHandler.FailCuratorDraftTask(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for wrong runtime fail, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetCuratorDraftStatus_Success(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	cleanup := setupCuratorDraftServices(t)
	defer cleanup()

	taskID := createCuratorDraftTaskFixture(t, testWorkspaceID, testRuntimeID, "issue", "completed", []byte(`{}`))

	// Simulate the RequireWorkspaceMember middleware injecting workspace context.
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/knowledge/curator-drafts/%s", taskID), nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	// Inject workspace context as the middleware would.
	userUUID := pgtype.UUID{}
	userUUID.Scan(testUserID)
	wsUUID := pgtype.UUID{}
	wsUUID.Scan(testWorkspaceID)
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      userUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		t.Fatalf("get test member: %v", err)
	}
	ctx := middleware.SetMemberContext(req.Context(), testWorkspaceID, member)
	req = req.WithContext(ctx)
	req = withURLParam(req, "taskId", taskID)

	rec := httptest.NewRecorder()
	testHandler.GetCuratorDraftStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetCuratorDraftStatus_MissingWorkspaceContext(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	cleanup := setupCuratorDraftServices(t)
	defer cleanup()

	taskID := createCuratorDraftTaskFixture(t, testWorkspaceID, testRuntimeID, "issue", "completed", []byte(`{}`))

	// Request without middleware-injected workspace context (no SetMemberContext).
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/knowledge/curator-drafts/%s", taskID), nil)
	req.Header.Set("Content-Type", "application/json")
	req = withURLParam(req, "taskId", taskID)

	rec := httptest.NewRecorder()
	testHandler.GetCuratorDraftStatus(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing workspace context, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Credential delivery tests (Problem 1)
// ---------------------------------------------------------------------------

func TestCuratorDraftTaskInputData_HasNoApiKey(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	cleanup := setupCuratorDraftServices(t)
	defer cleanup()

	// Create a task with only non-sensitive LLM config (no api_key, no secret_ref).
	input := service.CuratorDraftTaskInput{
		BaseURL:        "https://test.example/v1",
		Model:          "test-model",
		EmbeddingModel: "test-embedding",
		Provider:       "test",
		DraftInput: service.CuratorDraftInput{
			SourceSummary: "test summary",
		},
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	taskID := createCuratorDraftTaskFixture(t, testWorkspaceID, testRuntimeID, "issue", "queued", inputJSON)

	// Claim the task via daemon API.
	req := newDaemonTokenRequest(http.MethodPost,
		fmt.Sprintf("/api/daemon/runtimes/%s/curator-drafts/claim", testRuntimeID),
		map[string]any{}, testWorkspaceID, "test-daemon-claim")
	req = withURLParam(req, "runtimeId", testRuntimeID)

	rec := httptest.NewRecorder()
	testHandler.ClaimCuratorDraftTask(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for claim, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Task struct {
			ID         string          `json:"id"`
			Config     json.RawMessage `json:"config"`
			DraftInput json.RawMessage `json:"draft_input"`
		} `json:"task"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse claim response: %v", err)
	}
	if resp.Task.ID != taskID {
		t.Fatalf("task id = %s, want %s", resp.Task.ID, taskID)
	}

	// Verify config contains base_url, model, etc. but NOT api_key.
	var config map[string]any
	if err := json.Unmarshal(resp.Task.Config, &config); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if config["base_url"] != "https://test.example/v1" {
		t.Fatalf("config.base_url = %v, want https://test.example/v1", config["base_url"])
	}
	if config["model"] != "test-model" {
		t.Fatalf("config.model = %v, want test-model", config["model"])
	}
	if _, ok := config["api_key"]; ok {
		t.Fatal("config must not contain api_key")
	}

	// Verify response does NOT contain credentials or input_data.
	var rawResp map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &rawResp); err != nil {
		t.Fatalf("parse raw response: %v", err)
	}
	var taskObj map[string]json.RawMessage
	if err := json.Unmarshal(rawResp["task"], &taskObj); err != nil {
		t.Fatalf("parse task object: %v", err)
	}
	if _, ok := taskObj["input_data"]; ok {
		t.Fatal("response must not contain input_data key")
	}
	if _, ok := taskObj["credentials"]; ok {
		t.Fatal("response must not contain credentials key")
	}
	if _, ok := taskObj["config"]; !ok {
		t.Fatal("response must contain config key")
	}
	if _, ok := taskObj["draft_input"]; !ok {
		t.Fatal("response must contain draft_input key")
	}

	// Verify the DB input_data does NOT contain api_key or secret_ref.
	var storedInput []byte
	var dbInput map[string]any
	if err := testPool.QueryRow(context.Background(),
		`SELECT input_data FROM curator_draft_task WHERE id = $1`, taskID,
	).Scan(&storedInput); err != nil {
		t.Fatalf("read task input_data: %v", err)
	}
	if err := json.Unmarshal(storedInput, &dbInput); err != nil {
		t.Fatalf("parse db input_data: %v", err)
	}
	if _, ok := dbInput["api_key"]; ok {
		t.Fatal("DB input_data must not contain api_key")
	}
	if _, ok := dbInput["secret_ref"]; ok {
		t.Fatal("DB input_data must not contain secret_ref")
	}
	if dbInput["base_url"] != "https://test.example/v1" {
		t.Fatalf("DB input_data.base_url = %v, want https://test.example/v1", dbInput["base_url"])
	}
}
