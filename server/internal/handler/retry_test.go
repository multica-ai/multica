package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRetryTask uses a data-driven approach to validate the retry endpoint
// across happy-path and all edge cases.
func TestRetryTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// --- fixture setup ---
	// Get agent + runtime from the shared test fixture.
	var agentID, runtimeID string
	err := testPool.QueryRow(ctx,
		`SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	// Helper: create an issue in the test workspace.
	issueCounter := 9900
	createIssue := func(t *testing.T, title, status string) string {
		t.Helper()
		issueCounter++
		var id string
		err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, position)
			VALUES ($1, $2, $3, 'medium', 'member', $4, $5, 0)
			RETURNING id
		`, testWorkspaceID, title, status, testUserID, issueCounter).Scan(&id)
		if err != nil {
			t.Fatalf("setup: create issue: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id) })
		return id
	}

	// Helper: create a task in a specific status.
	createTask := func(t *testing.T, issueID, status string) string {
		t.Helper()
		var id string
		err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
			VALUES ($1, $2, $3, $4, 0)
			RETURNING id
		`, agentID, runtimeID, issueID, status).Scan(&id)
		if err != nil {
			t.Fatalf("setup: create task: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1 OR retried_from_id = $1`, id) })
		return id
	}

	// Helper: create a retry task pointing to originalID.
	createRetryOf := func(t *testing.T, originalID, issueID, status string) string {
		t.Helper()
		var id string
		err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, retried_from_id)
			VALUES ($1, $2, $3, $4, 0, $5)
			RETURNING id
		`, agentID, runtimeID, issueID, status, originalID).Scan(&id)
		if err != nil {
			t.Fatalf("setup: create retry task: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, id) })
		return id
	}

	// --- data-driven test cases ---
	tests := []struct {
		name       string
		setup      func(t *testing.T) string // returns taskID to retry
		wantStatus int
		wantError  string // substring in error response, empty for success
	}{
		{
			name: "retry failed task succeeds",
			setup: func(t *testing.T) string {
				issueID := createIssue(t, "retry-failed-test", "todo")
				return createTask(t, issueID, "failed")
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "retry cancelled task succeeds",
			setup: func(t *testing.T) string {
				issueID := createIssue(t, "retry-cancelled-test", "todo")
				return createTask(t, issueID, "cancelled")
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "retry running task rejected",
			setup: func(t *testing.T) string {
				issueID := createIssue(t, "retry-running-test", "todo")
				return createTask(t, issueID, "running")
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "not retryable",
		},
		{
			name: "retry completed task rejected",
			setup: func(t *testing.T) string {
				issueID := createIssue(t, "retry-completed-test", "todo")
				return createTask(t, issueID, "completed")
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "not retryable",
		},
		{
			name: "retry queued task rejected",
			setup: func(t *testing.T) string {
				issueID := createIssue(t, "retry-queued-test", "todo")
				return createTask(t, issueID, "queued")
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "not retryable",
		},
		{
			name: "double retry rejected",
			setup: func(t *testing.T) string {
				issueID := createIssue(t, "double-retry-test", "todo")
				taskID := createTask(t, issueID, "failed")
				// Create existing retry task
				createRetryOf(t, taskID, issueID, "queued")
				return taskID
			},
			wantStatus: http.StatusConflict,
			wantError:  "already been retried",
		},
		{
			name: "retry with active task on issue rejected",
			setup: func(t *testing.T) string {
				issueID := createIssue(t, "active-task-test", "todo")
				taskID := createTask(t, issueID, "failed")
				// Create an active (running) task on the same issue
				activeID := createTask(t, issueID, "running")
				t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, activeID) })
				return taskID
			},
			wantStatus: http.StatusConflict,
			wantError:  "active task",
		},
		{
			name: "retry nonexistent task returns 404",
			setup: func(t *testing.T) string {
				return "00000000-0000-0000-0000-000000000099"
			},
			wantStatus: http.StatusNotFound,
			wantError:  "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskID := tt.setup(t)

			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/tasks/"+taskID+"/retry", nil)
			req = withURLParam(req, "taskId", taskID)
			testHandler.RetryTask(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", tt.wantStatus, w.Code, w.Body.String())
			}

			if tt.wantError != "" {
				body := w.Body.String()
				if !strings.Contains(body, tt.wantError) {
					t.Fatalf("expected error containing %q, got: %s", tt.wantError, body)
				}
				return
			}

			// Success case: verify the response has a new task with correct lineage.
			var resp AgentTaskResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp.ID == "" || resp.ID == taskID {
				t.Fatal("expected new task ID, got same or empty")
			}
			if resp.Status != "queued" {
				t.Fatalf("expected status 'queued', got %q", resp.Status)
			}
			if resp.RetriedFromID == nil || *resp.RetriedFromID != taskID {
				t.Fatalf("expected retried_from_id = %q, got %v", taskID, resp.RetriedFromID)
			}

			// Cleanup: the retry task itself
			t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, resp.ID) })
		})
	}
}

