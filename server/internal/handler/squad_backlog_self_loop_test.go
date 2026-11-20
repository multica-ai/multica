package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// A promotion must compare the acting task with the resolved squad leader,
// including when that task is bound to the issue being promoted.
func TestBacklogToTodoSquadSameIssueMatchesPreview(t *testing.T) {
	for _, batch := range []bool{false, true} {
		path := "single"
		if batch {
			path = "batch"
		}
		for _, tc := range []struct {
			name       string
			actorLeads bool
			wantQueued int
		}{
			{name: "leader", actorLeads: true, wantQueued: 0},
			{name: "other_agent", wantQueued: 1},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				leaderID := createHandlerTestAgent(t, "Backlog Self-Loop Leader", nil)
				actorID := leaderID
				if !tc.actorLeads {
					actorID = createHandlerTestAgent(t, "Backlog Self-Loop Coordinator", nil)
				}
				squadID := dbfx.Squad(t, "Backlog Self-Loop Squad", leaderID)
				issueID := dbfx.Issue(t, "Squad backlog promotion", testutil.Cols{
					"status":        "backlog",
					"assignee_type": "squad",
					"assignee_id":   squadID,
				})
				dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
				actorTaskID := createHandlerTestTaskForAgentOnIssue(t, actorID, issueID)

				previewReq := newRequest("POST", "/api/issues/preview-trigger?workspace_id="+testWorkspaceID, map[string]any{
					"issue_ids": []string{issueID},
					"status":    "todo",
				})
				previewReq.Header.Set("X-Agent-ID", actorID)
				previewReq.Header.Set("X-Task-ID", actorTaskID)
				var preview IssueTriggerPreviewResponse
				testutil.Call(t, testHandler.PreviewIssueTrigger, previewReq).Want(http.StatusOK).JSON(&preview)
				if preview.TotalCount != tc.wantQueued || len(preview.Triggers) != tc.wantQueued {
					t.Errorf("expected %d preview triggers, got %+v", tc.wantQueued, preview)
				}
				if len(preview.Triggers) == 1 {
					trigger := preview.Triggers[0]
					if trigger.IssueID != issueID || trigger.AgentID != leaderID || trigger.Source != "status" {
						t.Errorf("expected status trigger for squad leader on %s, got %+v", issueID, trigger)
					}
				}
				if got := queuedTaskCountFor(t, issueID, leaderID); got != 0 {
					t.Fatalf("preview enqueued %d tasks", got)
				}

				req := withURLParam(newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "todo"}), "id", issueID)
				update := testHandler.UpdateIssue
				if batch {
					req = newRequest("PATCH", "/api/issues/batch?workspace_id="+testWorkspaceID, map[string]any{
						"issue_ids": []string{issueID},
						"updates":   map[string]any{"status": "todo"},
					})
					update = testHandler.BatchUpdateIssues
				}
				req.Header.Set("X-Agent-ID", actorID)
				req.Header.Set("X-Task-ID", actorTaskID)
				testutil.Call(t, update, req).Want(http.StatusOK)
				if got := queuedTaskCountFor(t, issueID, leaderID); got != tc.wantQueued {
					t.Errorf("expected %d queued squad leader tasks, got %d", tc.wantQueued, got)
				}
				var status string
				dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status)
				if status != "todo" {
					t.Errorf("expected promotion to todo, got %q", status)
				}
				if got := taskStatus(t, actorTaskID); got != "running" {
					t.Errorf("acting task must remain running, got %q", got)
				}
			})
		}
	}
}
