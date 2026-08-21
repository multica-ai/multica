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

// A move WITHIN the backlog category must not start a run (MUL-6463).
//
// Before custom statuses, leaving the `backlog` key was always leaving the
// backlog category, so the trigger could key on the key change alone. Once a
// workspace can define its own parking-lot statuses, `backlog` → `later` is a
// key change whose category never moves — and starting an agent on it breaks
// the one promise backlog makes. The UI does not confirm such a move either,
// so a run here would be a silent start.
func TestBacklogToCustomBacklogStatusDoesNotTrigger(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "Backlog Category Move Agent", nil)
	parkedKey := fmt.Sprintf("later_%d", time.Now().UnixNano())
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_status (workspace_id, key, name, description, category, color, position)
		VALUES ($1, $2, 'Later', '', 'backlog', '#ff0000', 1)
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

	// backlog → custom backlog-category status: still parked.
	req = newRequest("PUT", "/api/issues/"+created.ID, map[string]any{"status": parkedKey})
	req = withURLParam(req, "id", created.ID)
	testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusOK)

	var tasks int
	dbfx.QueryRow(t,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
		created.ID, agentID,
	).Scan(&tasks)
	if tasks != 0 {
		t.Fatalf("moving inside the backlog category must not enqueue a run, got %d queued tasks", tasks)
	}

	// Leaving the category still does, from the custom parked status too — the
	// fix must not cost the promotion it exists to allow.
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
