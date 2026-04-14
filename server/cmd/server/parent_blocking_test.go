package main

import (
	"context"
	"encoding/json"
	"testing"
)

// createTestIssueViaAPI creates an issue through the HTTP API (which handles number assignment)
// and returns its ID.
func createTestIssueViaAPI(t *testing.T, title, status string, parentID *string) string {
	t.Helper()
	body := map[string]any{
		"title":    title,
		"status":   status,
		"priority": "high",
	}
	if parentID != nil {
		body["parent_issue_id"] = *parentID
	}
	resp := authRequest(t, "POST", "/api/issues?workspace_id="+testWorkspaceID, body)
	if resp.StatusCode != 201 {
		t.Fatalf("CreateIssue %q: expected 201, got %d", title, resp.StatusCode)
	}
	var created map[string]any
	readJSON(t, resp, &created)
	return created["id"].(string)
}

func deleteTestIssue(t *testing.T, id string) {
	t.Helper()
	resp := authRequest(t, "DELETE", "/api/issues/"+id, nil)
	resp.Body.Close()
}

// enqueueTaskDirect inserts a task into agent_task_queue using direct SQL.
func enqueueTaskDirect(t *testing.T, agentID, runtimeID, issueID string) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 1)
	`, agentID, runtimeID, issueID)
	if err != nil {
		t.Fatalf("failed to enqueue task for issue %s: %v", issueID, err)
	}
}

func cleanupTasks(t *testing.T, issueIDs ...string) {
	t.Helper()
	for _, id := range issueIDs {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, id)
	}
}

// countSchedulableTasks returns how many tasks for the given runtime pass the
// parent-blocking filter (mirrors the ListPendingTasksByRuntime query).
func countSchedulableTasks(t *testing.T, runtimeID string) int {
	t.Helper()
	var count int
	err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue atq
		LEFT JOIN issue i ON atq.issue_id = i.id
		LEFT JOIN issue parent ON i.parent_issue_id = parent.id
		WHERE atq.runtime_id = $1
		  AND atq.status IN ('queued', 'dispatched')
		  AND (
			atq.issue_id IS NULL
			OR i.parent_issue_id IS NULL
			OR parent.status IN ('done', 'cancelled')
		  )
	`, runtimeID).Scan(&count)
	if err != nil {
		t.Fatalf("countSchedulableTasks query failed: %v", err)
	}
	return count
}

func getTestAgentAndRuntime(t *testing.T) (string, string) {
	t.Helper()
	var agentID, runtimeID string
	err := testPool.QueryRow(context.Background(), `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN workspace w ON a.workspace_id = w.id
		WHERE w.slug = $1 LIMIT 1
	`, integrationTestWorkspaceSlug).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}
	return agentID, runtimeID
}

// TestParentIssueBlocksChildTaskScheduling verifies that tasks for child issues
// are not returned by ListPendingTasksByRuntime when the parent issue is incomplete.
func TestParentIssueBlocksChildTaskScheduling(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	agentID, runtimeID := getTestAgentAndRuntime(t)

	parentID := createTestIssueViaAPI(t, "Parent: infra setup", "todo", nil)
	childID := createTestIssueViaAPI(t, "Child: build feature", "todo", &parentID)
	defer func() {
		cleanupTasks(t, parentID, childID)
		deleteTestIssue(t, childID)
		deleteTestIssue(t, parentID)
	}()

	enqueueTaskDirect(t, agentID, runtimeID, parentID)
	enqueueTaskDirect(t, agentID, runtimeID, childID)

	// Parent in todo → only parent task should be schedulable
	count := countSchedulableTasks(t, runtimeID)
	if count != 1 {
		t.Fatalf("expected 1 schedulable task (parent only), got %d", count)
	}

	// Mark parent as done
	resp := authRequest(t, "PUT", "/api/issues/"+parentID, map[string]any{"status": "done"})
	resp.Body.Close()

	// Now both tasks should be schedulable
	count = countSchedulableTasks(t, runtimeID)
	if count != 2 {
		t.Fatalf("expected 2 schedulable tasks after parent done, got %d", count)
	}
}

// TestCancelledParentUnblocksChildTask verifies that a cancelled parent
// also unblocks its child tasks.
func TestCancelledParentUnblocksChildTask(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	agentID, runtimeID := getTestAgentAndRuntime(t)

	parentID := createTestIssueViaAPI(t, "Cancelled parent", "todo", nil)
	childID := createTestIssueViaAPI(t, "Child of cancelled", "todo", &parentID)
	defer func() {
		cleanupTasks(t, parentID, childID)
		deleteTestIssue(t, childID)
		deleteTestIssue(t, parentID)
	}()

	enqueueTaskDirect(t, agentID, runtimeID, childID)

	// Cancel the parent
	resp := authRequest(t, "PUT", "/api/issues/"+parentID, map[string]any{"status": "cancelled"})
	resp.Body.Close()

	// Child should be schedulable
	count := countSchedulableTasks(t, runtimeID)
	if count != 1 {
		t.Fatalf("expected 1 schedulable task (child unblocked by cancelled parent), got %d", count)
	}
}

// TestTopLevelIssueAlwaysSchedulable verifies that issues without a parent
// are never blocked by the parent-check logic.
func TestTopLevelIssueAlwaysSchedulable(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	agentID, runtimeID := getTestAgentAndRuntime(t)

	issueID := createTestIssueViaAPI(t, "Top-level issue", "todo", nil)
	defer func() {
		cleanupTasks(t, issueID)
		deleteTestIssue(t, issueID)
	}()

	enqueueTaskDirect(t, agentID, runtimeID, issueID)

	count := countSchedulableTasks(t, runtimeID)
	if count != 1 {
		t.Fatalf("expected 1 schedulable task (top-level always schedulable), got %d", count)
	}
}

// TestInProgressParentBlocksChild verifies blocking for various incomplete statuses.
func TestInProgressParentBlocksChild(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	agentID, runtimeID := getTestAgentAndRuntime(t)

	for _, parentStatus := range []string{"in_progress", "in_review", "blocked"} {
		t.Run("parent_"+parentStatus, func(t *testing.T) {
			parentID := createTestIssueViaAPI(t, "Parent "+parentStatus, "todo", nil)
			childID := createTestIssueViaAPI(t, "Child of "+parentStatus, "todo", &parentID)
			defer func() {
				cleanupTasks(t, parentID, childID)
				deleteTestIssue(t, childID)
				deleteTestIssue(t, parentID)
			}()

			// Move parent to the target status
			resp := authRequest(t, "PUT", "/api/issues/"+parentID, map[string]any{"status": parentStatus})
			var updated map[string]any
			json.NewDecoder(resp.Body).Decode(&updated)
			resp.Body.Close()

			enqueueTaskDirect(t, agentID, runtimeID, childID)

			count := countSchedulableTasks(t, runtimeID)
			if count != 0 {
				t.Fatalf("expected 0 schedulable tasks (parent %s should block child), got %d", parentStatus, count)
			}
		})
	}
}
