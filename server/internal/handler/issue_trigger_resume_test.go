package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func updateResumeIssue(t *testing.T, issueID, status string, suppress bool) {
	t.Helper()
	body := map[string]any{"status": status}
	if suppress {
		body["suppress_run"] = true
	}
	req := withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, body), "id", issueID)
	testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusOK)
}

// A completed first run can be handed back to the same assignee by returning
// the issue to todo. Preview and the write must agree on the new run.
func TestResumeAssignedIssueFromBlockedReviewOrDone(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := seededReadyAgentID(t)
	for _, previous := range []string{"blocked", "done", "in_review"} {
		t.Run(previous, func(t *testing.T) {
			issue := createIssueForTest(t, map[string]any{
				"title": "resume " + previous, "status": "todo",
				"assignee_type": "agent", "assignee_id": agentID,
			})
			if got := taskCountFor(t, issue.ID, agentID); got != 1 {
				t.Fatalf("initial run count = %d, want 1", got)
			}
			if _, err := testPool.Exec(context.Background(),
				`UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE issue_id = $1 AND agent_id = $2`,
				issue.ID, agentID); err != nil {
				t.Fatal(err)
			}
			updateResumeIssue(t, issue.ID, previous, true)
			preview := previewIssueTrigger(t, map[string]any{"issue_ids": []string{issue.ID}, "status": "todo"})
			if preview.TotalCount != 1 || preview.Triggers[0].Source != "status" {
				t.Fatalf("preview %s -> todo = %+v, want one status run", previous, preview)
			}
			updateResumeIssue(t, issue.ID, "todo", false)
			if got := taskCountFor(t, issue.ID, agentID); got != 2 {
				t.Fatalf("%s -> todo run count = %d, want 2", previous, got)
			}
			claimed, err := testHandler.TaskService.ClaimTask(context.Background(), parseUUID(agentID))
			if err != nil || claimed == nil || uuidToString(claimed.IssueID) != issue.ID {
				t.Fatalf("resume task was not claimable: task=%+v, err=%v", claimed, err)
			}
			started, err := testHandler.TaskService.StartTask(context.Background(), claimed.ID)
			if err != nil || started == nil || started.Status != "running" {
				t.Fatalf("resume task did not start: task=%+v, err=%v", started, err)
			}
			// Saving todo again is not a transition and cannot create another run.
			updateResumeIssue(t, issue.ID, "todo", false)
			if got := taskCountFor(t, issue.ID, agentID); got != 2 {
				t.Fatalf("repeated todo save created a run: count = %d", got)
			}
		})
	}
}

func TestResumeAssignedIssueSuppressesActiveRunAndNoStart(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := seededReadyAgentID(t)
	issue := createIssueForTest(t, map[string]any{
		"title": "resume active", "status": "todo",
		"assignee_type": "agent", "assignee_id": agentID,
	})
	// The first task is still queued, so a return to todo must not duplicate it.
	updateResumeIssue(t, issue.ID, "blocked", true)
	if got := previewIssueTrigger(t, map[string]any{"issue_ids": []string{issue.ID}, "status": "todo"}); got.TotalCount != 0 {
		t.Fatalf("preview promised duplicate queued run: %+v", got)
	}
	updateResumeIssue(t, issue.ID, "todo", false)
	if got := taskCountFor(t, issue.ID, agentID); got != 1 {
		t.Fatalf("queued task duplicated: %d", got)
	}

	// A running task likewise owns the pair; changing board status is allowed.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE issue_id = $1 AND agent_id = $2`,
		issue.ID, agentID); err != nil {
		t.Fatal(err)
	}
	updateResumeIssue(t, issue.ID, "done", true)
	if got := previewIssueTrigger(t, map[string]any{"issue_ids": []string{issue.ID}, "status": "todo"}); got.TotalCount != 0 {
		t.Fatalf("preview promised duplicate running run: %+v", got)
	}
	updateResumeIssue(t, issue.ID, "todo", false)
	if got := taskCountFor(t, issue.ID, agentID); got != 1 {
		t.Fatalf("running task duplicated: %d", got)
	}

	// Once it finishes, --no-start updates the status without a new task.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE issue_id = $1 AND agent_id = $2`,
		issue.ID, agentID); err != nil {
		t.Fatal(err)
	}
	updateResumeIssue(t, issue.ID, "blocked", true)
	updateResumeIssue(t, issue.ID, "todo", true)
	if got := taskCountFor(t, issue.ID, agentID); got != 1 {
		t.Fatalf("suppress_run created a task: %d", got)
	}
}

func TestResumeSquadIssueFromDone(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	leaderID := seededReadyAgentID(t)
	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'resume squad', '', $2, $3) RETURNING id
	`, testWorkspaceID, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, squadID) })
	issue := createIssueForTest(t, map[string]any{
		"title": "squad rework", "status": "todo",
		"assignee_type": "squad", "assignee_id": squadID,
	})
	if got := taskCountFor(t, issue.ID, leaderID); got != 1 {
		t.Fatalf("initial squad leader tasks = %d, want 1", got)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE issue_id = $1 AND agent_id = $2`,
		issue.ID, leaderID); err != nil {
		t.Fatal(err)
	}
	updateResumeIssue(t, issue.ID, "done", true)
	if got := previewIssueTrigger(t, map[string]any{"issue_ids": []string{issue.ID}, "status": "todo"}); got.TotalCount != 1 {
		t.Fatalf("squad resume preview = %+v, want one run", got)
	}
	updateResumeIssue(t, issue.ID, "todo", false)
	if got := taskCountFor(t, issue.ID, leaderID); got != 2 {
		t.Fatalf("resumed squad leader tasks = %d, want 2", got)
	}
}

func TestResumeUnavailableExecutorReportsDispatchOutcome(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "unavailable resume agent", nil)
	issue := createIssueForTest(t, map[string]any{
		"title": "resume unavailable", "status": "blocked",
		"assignee_type": "agent", "assignee_id": agentID,
	})
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE issue_id = $1 AND agent_id = $2`,
		issue.ID, agentID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET runtime_id = NULL WHERE id = $1`, agentID); err != nil {
		t.Fatal(err)
	}
	if got := previewIssueTrigger(t, map[string]any{"issue_ids": []string{issue.ID}, "status": "todo"}); got.TotalCount != 0 {
		t.Fatalf("unavailable executor preview = %+v, want no trigger", got)
	}
	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPut, "/api/issues/"+issue.ID, map[string]any{"status": "todo"}), "id", issue.ID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue = %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	outcome, ok := body["run_dispatch"].(map[string]any)
	if !ok || outcome["status"] != "not_started" || outcome["reason"] == "" {
		t.Fatalf("missing actionable dispatch outcome: %+v", body)
	}
	if got := taskCountFor(t, issue.ID, agentID); got != 1 {
		t.Fatalf("unavailable executor received a new task: count = %d", got)
	}
}

func TestPreviewResumeSuppressRunMatchesWrite(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := seededReadyAgentID(t)
	issue := createIssueForTest(t, map[string]any{
		"title": "preview no start", "status": "blocked",
		"assignee_type": "agent", "assignee_id": agentID,
	})
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE issue_id = $1 AND agent_id = $2`,
		issue.ID, agentID); err != nil {
		t.Fatal(err)
	}
	if got := previewIssueTrigger(t, map[string]any{"issue_ids": []string{issue.ID}, "status": "todo"}); got.TotalCount != 1 {
		t.Fatalf("ordinary preview = %+v, want a run", got)
	}
	if got := previewIssueTrigger(t, map[string]any{
		"issue_ids": []string{issue.ID}, "status": "todo", "suppress_run": true,
	}); got.TotalCount != 0 {
		t.Fatalf("suppressed preview promised run: %+v", got)
	}
	updateResumeIssue(t, issue.ID, "todo", true)
	if got := taskCountFor(t, issue.ID, agentID); got != 1 {
		t.Fatalf("suppressed update queued a new task: count = %d", got)
	}
}

func TestResumeFromCustomDoneStatus(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	key := fmt.Sprintf("done_rework_%d", time.Now().UnixNano()%1_000_000_000)
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_status (workspace_id, key, name, description, category, color, position)
		VALUES ($1, $2, 'Done for review', '', 'done', '#00aa00', 1)
	`, testWorkspaceID, key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM issue_status WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, key)
	})
	agentID := seededReadyAgentID(t)
	issue := createIssueForTest(t, map[string]any{
		"title": "custom done handback", "status": "todo",
		"assignee_type": "agent", "assignee_id": agentID,
	})
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE issue_id = $1 AND agent_id = $2`,
		issue.ID, agentID); err != nil {
		t.Fatal(err)
	}
	updateResumeIssue(t, issue.ID, key, true)
	if got := previewIssueTrigger(t, map[string]any{"issue_ids": []string{issue.ID}, "status": "todo"}); got.TotalCount != 1 {
		t.Fatalf("custom done preview = %+v, want one run", got)
	}
	updateResumeIssue(t, issue.ID, "todo", false)
	if got := taskCountFor(t, issue.ID, agentID); got != 2 {
		t.Fatalf("custom done resume tasks = %d, want 2", got)
	}
}

func TestResumePrivateAgentRequiresInvokePermission(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID, _, _ := privateAgentTestFixture(t)
	issueID := dbfx.Issue(t, "private resume gate", testutil.Cols{
		"workspace_id": testWorkspaceID, "status": "blocked",
		"assignee_type": "agent", "assignee_id": agentID,
	})
	if got := previewIssueTrigger(t, map[string]any{"issue_ids": []string{issueID}, "status": "todo"}); got.TotalCount != 0 {
		t.Fatalf("preview leaked private agent invocation: %+v", got)
	}
	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{"status": "todo"}), "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue = %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	outcome, ok := body["run_dispatch"].(map[string]any)
	if !ok || outcome["status"] != "not_started" {
		t.Fatalf("private agent denial lacks dispatch outcome: %+v", body)
	}
	if got := taskCountFor(t, issueID, agentID); got != 0 {
		t.Fatalf("unauthorized status write enqueued %d tasks", got)
	}
}
