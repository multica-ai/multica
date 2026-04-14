package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newDaemonTokenRequest creates an HTTP request with daemon token context set
// (simulating DaemonAuth middleware for mdt_ tokens).
func newDaemonTokenRequest(method, path string, body any, workspaceID, daemonID string) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	// No X-User-ID — daemon tokens don't set it.
	ctx := middleware.WithDaemonContext(req.Context(), workspaceID, daemonID)
	return req.WithContext(ctx)
}

func TestDaemonRegister_WithDaemonToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    "test-daemon-mdt",
		"device_name":  "test-device",
		"runtimes": []map[string]any{
			{"name": "test-runtime", "type": "claude", "version": "1.0.0", "status": "online"},
		},
	}, testWorkspaceID, "test-daemon-mdt")

	testHandler.DaemonRegister(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DaemonRegister with daemon token: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	runtimes, ok := resp["runtimes"].([]any)
	if !ok || len(runtimes) == 0 {
		t.Fatalf("DaemonRegister: expected runtimes in response, got %v", resp)
	}

	// Clean up: deregister the runtime.
	rt := runtimes[0].(map[string]any)
	runtimeID := rt["id"].(string)
	testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
}

func TestDaemonRegister_WithDaemonToken_WorkspaceMismatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	w := httptest.NewRecorder()
	// Daemon token is for a different workspace than the request body.
	req := newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    "test-daemon-mdt",
		"device_name":  "test-device",
		"runtimes": []map[string]any{
			{"name": "test-runtime", "type": "claude", "version": "1.0.0", "status": "online"},
		},
	}, "00000000-0000-0000-0000-000000000000", "test-daemon-mdt")

	testHandler.DaemonRegister(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("DaemonRegister with mismatched workspace: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDaemonHeartbeat_WithDaemonToken_CrossWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// First, register a runtime using PAT (existing flow).
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    "test-daemon-heartbeat",
		"device_name":  "test-device",
		"runtimes": []map[string]any{
			{"name": "test-runtime-hb", "type": "claude", "version": "1.0.0", "status": "online"},
		},
	})
	testHandler.DaemonRegister(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Setup: DaemonRegister failed: %d: %s", w.Code, w.Body.String())
	}
	var regResp map[string]any
	json.NewDecoder(w.Body).Decode(&regResp)
	runtimes := regResp["runtimes"].([]any)
	runtimeID := runtimes[0].(map[string]any)["id"].(string)
	defer testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)

	// Try heartbeat with a daemon token from a DIFFERENT workspace — should fail.
	w = httptest.NewRecorder()
	req = newDaemonTokenRequest("POST", "/api/daemon/heartbeat", map[string]any{
		"runtime_id": runtimeID,
	}, "00000000-0000-0000-0000-000000000000", "attacker-daemon")

	testHandler.DaemonHeartbeat(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("DaemonHeartbeat with cross-workspace token: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetTaskStatus_WithDaemonToken_CrossWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Create a task in the test workspace.
	var issueID, taskID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type)
		VALUES ($1, 'daemon-auth-test-issue', 'todo', 'medium', $2, 'member')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID)
	if err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	defer testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)

	// Get an agent and runtime from the test workspace.
	var agentID, runtimeID string
	err = testPool.QueryRow(context.Background(), `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	err = testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, issue_id, status, runtime_id)
		VALUES ($1, $2, 'queued', $3)
		RETURNING id
	`, agentID, issueID, runtimeID).Scan(&taskID)
	if err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	defer testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)

	// Try GetTaskStatus with a daemon token from a DIFFERENT workspace — should fail.
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("GET", "/api/daemon/tasks/"+taskID+"/status", nil,
		"00000000-0000-0000-0000-000000000000", "attacker-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.GetTaskStatus(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetTaskStatus with cross-workspace token: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Same request with the CORRECT workspace should succeed.
	w = httptest.NewRecorder()
	req = newDaemonTokenRequest("GET", "/api/daemon/tasks/"+taskID+"/status", nil,
		testWorkspaceID, "legit-daemon")
	req = req.WithContext(context.WithValue(
		middleware.WithDaemonContext(req.Context(), testWorkspaceID, "legit-daemon"),
		chi.RouteCtxKey, rctx))

	testHandler.GetTaskStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetTaskStatus with correct workspace token: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDaemonCompletionWriteback_ImplementationMovesToReview(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	issueID, taskID := createCompletionTask(t, "Fix daemon writeback", "todo")
	completeDaemonTask(t, taskID, map[string]any{
		"output":      "Implemented daemon writeback.\nPR: https://github.com/acme/repo/pull/12",
		"work_dir":    "/tmp/multica-workdir",
		"branch_name": "agent/fix/writeback",
	})

	if got := issueStatus(t, issueID); got != "in_review" {
		t.Fatalf("issue status = %q, want in_review", got)
	}
	comments := completionComments(t, issueID, taskID)
	if len(comments) != 1 {
		t.Fatalf("completion comments = %d, want 1: %#v", len(comments), comments)
	}
	for _, want := range []string{
		"Implemented daemon writeback.",
		"Run ID: `" + taskID + "`",
		"Workdir: `/tmp/multica-workdir`",
		"Branch: `agent/fix/writeback`",
		"PR: https://github.com/acme/repo/pull/12",
	} {
		if !strings.Contains(comments[0], want) {
			t.Fatalf("completion comment missing %q:\n%s", want, comments[0])
		}
	}
}

func TestDaemonCompletionWriteback_ReviewNoBlockingFindingsMovesToDone(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	issueID, taskID := createCompletionTask(t, "Review daemon writeback", "in_review")
	completeDaemonTask(t, taskID, map[string]any{
		"output":   "No blocking findings. The implementation is ready.",
		"work_dir": "/tmp/review-workdir",
	})

	if got := issueStatus(t, issueID); got != "done" {
		t.Fatalf("issue status = %q, want done", got)
	}
	if got := len(completionComments(t, issueID, taskID)); got != 1 {
		t.Fatalf("completion comments = %d, want 1", got)
	}
}

func TestDaemonCompletionWriteback_ReviewBlockingFindingsDoesNotMoveToDone(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	issueID, taskID := createCompletionTask(t, "Review daemon writeback", "in_review")
	completeDaemonTask(t, taskID, map[string]any{
		"output": "Blocking findings:\n- The writeback can duplicate comments.",
	})

	if got := issueStatus(t, issueID); got == "done" {
		t.Fatalf("issue status = %q, want not done", got)
	} else if got != "blocked" {
		t.Fatalf("issue status = %q, want blocked", got)
	}
	if got := len(completionComments(t, issueID, taskID)); got != 1 {
		t.Fatalf("completion comments = %d, want 1", got)
	}
}

func TestDaemonCompletionWriteback_UnknownTaskLeavesStatusUnchanged(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	issueID, taskID := createCompletionTask(t, "Update daemon writeback", "in_progress")
	completeDaemonTask(t, taskID, map[string]any{
		"output": "Completed an unknown task type.",
	})

	if got := issueStatus(t, issueID); got != "in_progress" {
		t.Fatalf("issue status = %q, want unchanged in_progress", got)
	}
	if got := len(completionComments(t, issueID, taskID)); got != 1 {
		t.Fatalf("completion comments = %d, want 1", got)
	}
}

func TestDaemonCompletionWriteback_RetryDoesNotDuplicateCompletionComment(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	issueID, taskID := createCompletionTask(t, "Fix daemon writeback retry", "in_progress")
	body := map[string]any{
		"output":      "Implemented once.",
		"work_dir":    "/tmp/retry-workdir",
		"branch_name": "agent/fix/retry",
	}
	completeDaemonTask(t, taskID, body)
	completeDaemonTask(t, taskID, body)

	if got := issueStatus(t, issueID); got != "in_review" {
		t.Fatalf("issue status = %q, want in_review", got)
	}
	if got := len(completionComments(t, issueID, taskID)); got != 1 {
		t.Fatalf("completion comments = %d, want 1", got)
	}
}

func TestDaemonCompletionWriteback_RetryDoesNotClobberManualStatus(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	issueID, taskID := createCompletionTask(t, "Fix daemon writeback manual status", "in_progress")
	body := map[string]any{
		"output": "Implemented once.",
	}
	completeDaemonTask(t, taskID, body)
	if got := issueStatus(t, issueID); got != "in_review" {
		t.Fatalf("issue status after first completion = %q, want in_review", got)
	}

	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET status = 'done' WHERE id = $1`, issueID); err != nil {
		t.Fatalf("manual status update: %v", err)
	}

	completeDaemonTask(t, taskID, body)
	if got := issueStatus(t, issueID); got != "done" {
		t.Fatalf("issue status after retry = %q, want manual done preserved", got)
	}
	if got := len(completionComments(t, issueID, taskID)); got != 1 {
		t.Fatalf("completion comments = %d, want 1", got)
	}
}

func TestDaemonCompletionWriteback_StatusCASPreservesConcurrentManualStatus(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	issueID, _ := createCompletionTask(t, "Fix daemon writeback stale status", "in_progress")
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issueID))
	if err != nil {
		t.Fatalf("load stale issue status: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET status = 'done' WHERE id = $1`, issueID); err != nil {
		t.Fatalf("manual status update: %v", err)
	}

	_, err = testHandler.Queries.UpdateIssueStatusIfCurrent(context.Background(), db.UpdateIssueStatusIfCurrentParams{
		ID:             issue.ID,
		NextStatus:     "in_review",
		ExpectedStatus: issue.Status,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("UpdateIssueStatusIfCurrent error = %v, want pgx.ErrNoRows", err)
	}
	if got := issueStatus(t, issueID); got != "done" {
		t.Fatalf("issue status after stale CAS = %q, want manual done preserved", got)
	}
}

func TestDaemonCompletionWriteback_RetryAfterCommentOnlyAppliesStatus(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	issueID, taskID := createCompletionTask(t, "Fix daemon writeback partial retry", "in_progress")
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, $4, 'comment')
	`, issueID, testWorkspaceID, testUserID, "<!-- multica-completion-run:"+taskID+" -->"); err != nil {
		t.Fatalf("insert completion marker comment: %v", err)
	}

	completeDaemonTask(t, taskID, map[string]any{
		"output": "Implemented after partial retry.",
	})

	if got := issueStatus(t, issueID); got != "in_review" {
		t.Fatalf("issue status = %q, want in_review", got)
	}
	if got := len(completionComments(t, issueID, taskID)); got != 1 {
		t.Fatalf("completion comments = %d, want 1 existing marker comment", got)
	}
}

func TestDaemonCompletionWritebackFailureLeavesRunCompleted(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	oldWriteback := testHandler.TaskService.CompletionWriteback
	testHandler.TaskService.CompletionWriteback = func(context.Context, db.AgentTaskQueue, []byte) error {
		return errors.New("forced writeback failure")
	}
	t.Cleanup(func() {
		testHandler.TaskService.CompletionWriteback = oldWriteback
	})

	issueID, taskID := createCompletionTask(t, "Fix writeback failure handling", "in_progress")
	completeDaemonTask(t, taskID, map[string]any{
		"output": "The final result should still be stored.",
	})

	var taskStatus, output string
	if err := testPool.QueryRow(context.Background(), `SELECT status, result->>'output' FROM agent_task_queue WHERE id = $1`, taskID).Scan(&taskStatus, &output); err != nil {
		t.Fatalf("query task result: %v", err)
	}
	if taskStatus != "completed" {
		t.Fatalf("task status = %q, want completed", taskStatus)
	}
	if output != "The final result should still be stored." {
		t.Fatalf("stored output = %q, want final result preserved", output)
	}
	if got := issueStatus(t, issueID); got != "in_progress" {
		t.Fatalf("issue status = %q, want unchanged in_progress", got)
	}
	if got := len(completionComments(t, issueID, taskID)); got != 0 {
		t.Fatalf("completion comments = %d, want 0 after forced failure", got)
	}
}

func TestDaemonCompletionWriteback_RedactsWorkdirRef(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		t.Skip("home directory unavailable")
	}

	issueID, taskID := createCompletionTask(t, "Fix daemon writeback redaction", "in_progress")
	completeDaemonTask(t, taskID, map[string]any{
		"output":   "Implemented with a local workdir.",
		"work_dir": homeDir + "/multica-secret-workdir",
	})

	comments := completionComments(t, issueID, taskID)
	if len(comments) != 1 {
		t.Fatalf("completion comments = %d, want 1", len(comments))
	}
	if strings.Contains(comments[0], homeDir) {
		t.Fatalf("completion comment leaked home dir %q:\n%s", homeDir, comments[0])
	}
	if !strings.Contains(comments[0], "****") {
		t.Fatalf("completion comment missing redacted home dir marker:\n%s", comments[0])
	}
}

func TestDaemonCompletionWriteback_TruncatesLargeCommentButStoresFullResult(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	largeOutput := "start-" + strings.Repeat("x", 13000) + "-end"
	issueID, taskID := createCompletionTask(t, "Fix daemon writeback large output", "in_progress")
	completeDaemonTask(t, taskID, map[string]any{
		"output": largeOutput,
	})

	comments := completionComments(t, issueID, taskID)
	if len(comments) != 1 {
		t.Fatalf("completion comments = %d, want 1", len(comments))
	}
	if strings.Contains(comments[0], "-end") {
		t.Fatal("completion comment should be truncated before the end of the large output")
	}
	if !strings.Contains(comments[0], "Completion output truncated for issue comment") {
		t.Fatalf("completion comment missing truncation marker:\n%s", comments[0])
	}

	var storedOutput string
	if err := testPool.QueryRow(context.Background(), `SELECT result->>'output' FROM agent_task_queue WHERE id = $1`, taskID).Scan(&storedOutput); err != nil {
		t.Fatalf("query stored output: %v", err)
	}
	if storedOutput != largeOutput {
		t.Fatal("stored task result should preserve full output")
	}
}

func createCompletionTask(t *testing.T, title, status string) (issueID string, taskID string) {
	t.Helper()
	ctx := context.Background()

	var number int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace SET issue_counter = issue_counter + 1
		WHERE id = $1
		RETURNING issue_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("setup: increment issue counter: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, $2, $3, 'medium', $4, 'member', $5, 0)
		RETURNING id
	`, testWorkspaceID, title, status, testUserID, number).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT id, runtime_id FROM agent
		WHERE workspace_id = $1 AND name = 'Handler Test Agent'
		LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent/runtime: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, status, runtime_id, started_at, priority)
		VALUES ($1, $2, 'running', $3, now(), 0)
		RETURNING id
	`, agentID, issueID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create running task: %v", err)
	}

	return issueID, taskID
}

func completeDaemonTask(t *testing.T, taskID string, body map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete", body, testWorkspaceID, "completion-test-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.CompleteTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func issueStatus(t *testing.T, issueID string) string {
	t.Helper()
	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("query issue status: %v", err)
	}
	return status
}

func completionComments(t *testing.T, issueID, taskID string) []string {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `
		SELECT content FROM comment
		WHERE issue_id = $1 AND content LIKE '%' || $2 || '%'
		ORDER BY created_at
	`, issueID, "multica-completion-run:"+taskID)
	if err != nil {
		t.Fatalf("query comments: %v", err)
	}
	defer rows.Close()

	var comments []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			t.Fatalf("scan comment: %v", err)
		}
		comments = append(comments, content)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate comments: %v", err)
	}
	return comments
}
