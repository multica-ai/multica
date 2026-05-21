package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// createTestTask inserts a minimal task for interaction tests and returns its ID.
func createTestTask(t *testing.T) string {
	t.Helper()

	var projectID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'interaction-test-proj')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, project_id, title, status, priority, creator_type, creator_id, number, position)
		VALUES ($1, $2, 'interaction test issue', 'todo', 'medium', 'member', $3, 
			COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1, 0)
		RETURNING id
	`, testWorkspaceID, projectID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	var agentID, runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("get agent: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (issue_id, agent_id, runtime_id, status)
		VALUES ($1, $2, $3, 'running')
		RETURNING id
	`, issueID, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
		// Reset workspace issue counter.
		testPool.Exec(context.Background(), `
			UPDATE workspace SET issue_counter = COALESCE(
				(SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0)
			WHERE id = $1`, testWorkspaceID)
	})

	return taskID
}

func createTestLocalRun(t *testing.T) string {
	t.Helper()

	var projectID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'interaction-local-run-proj')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, project_id, title, status, priority, creator_type, creator_id, number, position)
		VALUES ($1, $2, 'interaction local run issue', 'todo', 'medium', 'member', $3,
			COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1, 0)
		RETURNING id
	`, testWorkspaceID, projectID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	var runID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO local_cli_run (workspace_id, issue_id, owner_id, cli_name, status)
		VALUES ($1, $2, $3, 'codex', 'running')
		RETURNING id
	`, testWorkspaceID, issueID, testUserID).Scan(&runID); err != nil {
		t.Fatalf("create local run: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM local_cli_run WHERE id = $1`, runID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
		testPool.Exec(context.Background(), `
			UPDATE workspace SET issue_counter = COALESCE(
				(SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0)
			WHERE id = $1`, testWorkspaceID)
	})

	return runID
}

func cleanInteraction(t *testing.T, id string) {
	t.Helper()
	t.Cleanup(func() {
		testHandler.InteractionStore.mu.Lock()
		delete(testHandler.InteractionStore.items, id)
		testHandler.InteractionStore.mu.Unlock()
	})
}

func withUserTaskContext(req *http.Request, taskID, workspaceID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	ctx := middleware.SetMemberContext(req.Context(), workspaceID, db.Member{})
	return req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
}

func TestReportInteraction_DaemonSuccess(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	taskID := createTestTask(t)

	body := ReportInteractionRequest{
		Type:     protocol.InteractionCommandApproval,
		Title:    "Run: rm -rf /tmp/test",
		Provider: "claude",
		Options: []protocol.InteractionOption{
			{ID: "allow", Label: "Allow"},
			{ID: "deny", Label: "Deny"},
		},
	}

	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/interactions", body, testWorkspaceID, "test-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	testHandler.ReportInteraction(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var dto InteractionDTO
	json.NewDecoder(w.Body).Decode(&dto)
	if dto.ID == "" {
		t.Fatal("expected non-empty interaction ID")
	}
	if dto.Status != protocol.InteractionStatusPending {
		t.Errorf("status = %q, want %q", dto.Status, protocol.InteractionStatusPending)
	}
	if dto.TaskID != taskID {
		t.Errorf("task_id = %q, want %q", dto.TaskID, taskID)
	}
	cleanInteraction(t, dto.ID)
}

func TestReportInteraction_PlanApprovalDoesNotExpire(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	taskID := createTestTask(t)

	body := ReportInteractionRequest{
		Type:      protocol.InteractionPlanApproval,
		Title:     "Plan ready",
		Provider:  "claude",
		ExpiresIn: -1,
	}

	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/interactions", body, testWorkspaceID, "test-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	testHandler.ReportInteraction(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var dto InteractionDTO
	json.NewDecoder(w.Body).Decode(&dto)
	defer cleanInteraction(t, dto.ID)

	if dto.ExpiresAt != "" {
		t.Fatalf("plan approval should not expire, got expires_at=%q", dto.ExpiresAt)
	}
}

func TestRespondInteraction_UserSuccess(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	taskID := createTestTask(t)

	interactionID := testHandler.InteractionStore.Create(protocol.InteractionRequest{
		TaskID:   taskID,
		Provider: "claude",
		Type:     protocol.InteractionCommandApproval,
		Title:    "Run: echo hello",
		Options: []protocol.InteractionOption{
			{ID: "allow", Label: "Allow"},
			{ID: "deny", Label: "Deny"},
		},
	})
	cleanInteraction(t, interactionID)

	body := RespondInteractionRequest{ChosenOption: "allow"}
	req := newRequest("POST", "/api/tasks/"+taskID+"/interactions/"+interactionID+"/respond", body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	rctx.URLParams.Add("interactionId", interactionID)
	ctx := middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{})
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	testHandler.RespondInteraction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var dto InteractionDTO
	json.NewDecoder(w.Body).Decode(&dto)
	if dto.Status != protocol.InteractionStatusApproved {
		t.Errorf("status = %q, want %q", dto.Status, protocol.InteractionStatusApproved)
	}
	if dto.ChosenOption != "allow" {
		t.Errorf("chosen_option = %q, want %q", dto.ChosenOption, "allow")
	}
}

func TestRespondInteraction_NoPermission(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	taskID := createTestTask(t)

	interactionID := testHandler.InteractionStore.Create(protocol.InteractionRequest{
		TaskID: taskID,
		Type:   protocol.InteractionCommandApproval,
		Title:  "test",
	})
	cleanInteraction(t, interactionID)

	body := RespondInteractionRequest{ChosenOption: "allow"}
	req := newRequest("POST", "/api/tasks/"+taskID+"/interactions/"+interactionID+"/respond", body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	rctx.URLParams.Add("interactionId", interactionID)
	// Wrong workspace → no access.
	ctx := middleware.SetMemberContext(req.Context(), "00000000-0000-0000-0000-000000000000", db.Member{})
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	testHandler.RespondInteraction(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListTaskInteractions_UserSuccess(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	taskID := createTestTask(t)

	id1 := testHandler.InteractionStore.Create(protocol.InteractionRequest{
		TaskID: taskID, Type: protocol.InteractionCommandApproval, Title: "cmd1",
	})
	id2 := testHandler.InteractionStore.Create(protocol.InteractionRequest{
		TaskID: taskID, Type: protocol.InteractionFileChangeApproval, Title: "file1",
	})
	cleanInteraction(t, id1)
	cleanInteraction(t, id2)

	req := newRequest("GET", "/api/tasks/"+taskID+"/interactions", nil)
	req = withUserTaskContext(req, taskID, testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.ListTaskInteractions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var dtos []InteractionDTO
	json.NewDecoder(w.Body).Decode(&dtos)
	if len(dtos) != 2 {
		t.Errorf("expected 2 interactions, got %d", len(dtos))
	}
}

func TestListTaskInteractions_LocalRunReturnsEmpty(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runID := createTestLocalRun(t)
	req := newRequest("GET", "/api/tasks/"+runID+"/interactions?status=pending", nil)
	req = withUserTaskContext(req, runID, testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.ListTaskInteractions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var dtos []InteractionDTO
	if err := json.NewDecoder(w.Body).Decode(&dtos); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(dtos) != 0 {
		t.Fatalf("expected no local run interactions, got %+v", dtos)
	}
}

func TestAutoPolicy_DoesNotCreatePendingInteraction(t *testing.T) {
	// Default policy is auto — no pending interaction should be created.
	policy := protocol.ResolveApprovalPolicy(nil)
	if policy != protocol.ApprovalPolicyAuto {
		t.Fatalf("default policy = %q, want %q", policy, protocol.ApprovalPolicyAuto)
	}

	policy = protocol.ResolveApprovalPolicy([]byte(`{}`))
	if policy != protocol.ApprovalPolicyAuto {
		t.Fatalf("empty config policy = %q, want %q", policy, protocol.ApprovalPolicyAuto)
	}

	policy = protocol.ResolveApprovalPolicy([]byte(`{"approval_policy":"prompt"}`))
	if policy != protocol.ApprovalPolicyPrompt {
		t.Fatalf("prompt config policy = %q, want %q", policy, protocol.ApprovalPolicyPrompt)
	}

	policy = protocol.ResolveApprovalPolicy([]byte(`{"approval_policy":"deny"}`))
	if policy != protocol.ApprovalPolicyDeny {
		t.Fatalf("deny config policy = %q, want %q", policy, protocol.ApprovalPolicyDeny)
	}

	policy = protocol.ResolveApprovalPolicy([]byte(`{"approval_policy":"unknown"}`))
	if policy != protocol.ApprovalPolicyAuto {
		t.Fatalf("unknown policy = %q, want %q (should fallback to auto)", policy, protocol.ApprovalPolicyAuto)
	}

	// AutoApprovalHandler approves immediately.
	h := protocol.AutoApprovalHandler{}
	option, approved := h.Handle(protocol.InteractionRequest{
		Type: protocol.InteractionCommandApproval, Title: "test",
	})
	if !approved || option != "allow" {
		t.Errorf("auto: option=%q approved=%v, want allow/true", option, approved)
	}

	// DenyApprovalHandler denies immediately.
	dh := protocol.DenyApprovalHandler{}
	option, approved = dh.Handle(protocol.InteractionRequest{})
	if approved || option != "deny" {
		t.Errorf("deny: option=%q approved=%v, want deny/false", option, approved)
	}
}

func TestGetInteractionResult_DaemonSuccess(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	taskID := createTestTask(t)

	interactionID := testHandler.InteractionStore.Create(protocol.InteractionRequest{
		TaskID:   taskID,
		Provider: "codex",
		Type:     protocol.InteractionCommandApproval,
		Title:    "exec: ls",
	})
	cleanInteraction(t, interactionID)

	req := newDaemonTokenRequest("GET", "/api/daemon/tasks/"+taskID+"/interactions/"+interactionID, nil, testWorkspaceID, "test-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	rctx.URLParams.Add("interactionId", interactionID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	testHandler.GetInteractionResult(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var dto InteractionDTO
	json.NewDecoder(w.Body).Decode(&dto)
	if dto.ID != interactionID {
		t.Errorf("id = %q, want %q", dto.ID, interactionID)
	}
	if dto.Status != protocol.InteractionStatusPending {
		t.Errorf("status = %q, want %q", dto.Status, protocol.InteractionStatusPending)
	}
}
