package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Only the fixed backlog key parks work. A custom unstarted status does not.
func TestBacklogToCustomUnstartedStatusTriggers(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "Backlog Category Move Agent", nil)
	parkedKey := fmt.Sprintf("later_%d", time.Now().UnixNano())
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_status (workspace_id, key, name, description, category, color, position)
		VALUES ($1, $2, 'Later', '', 'unstarted', '#ff0000', 1)
	`, testWorkspaceID, parkedKey); err != nil {
		t.Fatalf("create custom backlog status: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_status WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, parkedKey)
	})

	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Parked issue",
		"status":        "backlog",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	w := testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusCreated)
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	// Leaving the built-in backlog promotes the issue, even within unstarted.
	req = newRequest("PUT", "/api/issues/"+created.ID, map[string]any{"status": parkedKey})
	req = withURLParam(req, "id", created.ID)
	testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusOK)

	var tasks int
	dbfx.QueryRow(t,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
		created.ID, agentID,
	).Scan(&tasks)
	if tasks != 1 {
		t.Fatalf("leaving built-in backlog must enqueue one run, got %d queued tasks", tasks)
	}

	// Leaving a custom unstarted status is not a second backlog promotion.
	req = newRequest("PUT", "/api/issues/"+created.ID, map[string]any{"status": "todo"})
	req = withURLParam(req, "id", created.ID)
	testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusOK)

	dbfx.QueryRow(t,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
		created.ID, agentID,
	).Scan(&tasks)
	if tasks != 1 {
		t.Fatalf("promotion out of the backlog category must enqueue exactly 1 run, got %d", tasks)
	}
}
