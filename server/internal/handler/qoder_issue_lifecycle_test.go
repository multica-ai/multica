package handler

import (
	"context"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestQoderIssueLifecycle(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	for _, scenario := range []string{"success", "claim replay", "edit", "status roundtrip", "reassign", "newer task", "already reviewing", "custom status", "local runtime", "failed", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			provider := "qoder_cloud"
			initial := "todo"
			if scenario == "local runtime" {
				provider = "claude"
			}
			if scenario == "already reviewing" {
				initial = "in_review"
			}
			if scenario == "custom status" {
				initial = createTestCustomStatus(t, "qoder_custom_active", "started").Key
			}
			runtimeID := dbfx.Runtime(t, "lifecycle runtime", testutil.Cols{"provider": provider})
			agentID := dbfx.Agent(t, "lifecycle agent", runtimeID)
			issueID := dbfx.Issue(t, "Qoder lifecycle", testutil.Cols{"status": initial, "assignee_type": "agent", "assignee_id": agentID})
			taskID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "dispatched", "dispatched_at": time.Now().UTC()})
			if scenario == "claim replay" {
				task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
				if err != nil {
					t.Fatal(err)
				}
				for range 2 {
					if _, err := testHandler.TaskService.StartTaskForClaim(ctx, db.LockAgentTaskStartClaimParams{ID: task.ID, RuntimeID: task.RuntimeID, DispatchedAt: task.DispatchedAt}); err != nil {
						t.Fatal(err)
					}
				}
			} else if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(taskID)); err != nil {
				t.Fatal(err)
			}
			issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
			if err != nil {
				t.Fatal(err)
			}
			want := "in_progress"
			if scenario == "local runtime" || scenario == "already reviewing" || scenario == "custom status" {
				want = initial
			}
			if issue.Status != want {
				t.Fatalf("start status = %s, want %s", issue.Status, want)
			}
			switch scenario {
			case "edit":
				dbfx.Exec(t, `UPDATE issue SET title='Human edit', revision=revision+1 WHERE id=$1`, issueID)
			case "status roundtrip":
				dbfx.Exec(t, `UPDATE issue SET status='blocked', revision=revision+1 WHERE id=$1`, issueID)
				dbfx.Exec(t, `UPDATE issue SET status='in_progress', revision=revision+1 WHERE id=$1`, issueID)
			case "reassign":
				dbfx.Exec(t, `UPDATE issue SET assignee_type='member', assignee_id=$2, revision=revision+1 WHERE id=$1`, issueID, testUserID)
			case "newer task":
				dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})
			case "failed":
				if _, err := testHandler.TaskService.FailTask(ctx, parseUUID(taskID), "Qoder failed", "", "", "", "qoder_cloud_execution", false, "", ""); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				if _, err := testHandler.TaskService.CancelTask(ctx, parseUUID(taskID)); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if _, _, err := testHandler.TaskService.CompleteTaskWithTransition(ctx, parseUUID(taskID), []byte(`{"output":"Ready for review"}`), "", "", "", false, "", ""); err != nil {
					t.Fatal(err)
				}
			}
			issue, err = testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "success" || scenario == "claim replay" {
				want = "in_review"
			}
			if issue.Status != want {
				t.Fatalf("completion status = %s, want %s", issue.Status, want)
			}
			var changes int
			dbfx.QueryRow(t, `SELECT count(*) FROM activity_log WHERE issue_id=$1 AND action='status_changed' AND details->>'source'='qoder_cloud'`, issueID).Scan(&changes)
			expected := 1
			if scenario == "success" || scenario == "claim replay" {
				expected = 2
			}
			if scenario == "local runtime" || scenario == "already reviewing" || scenario == "custom status" {
				expected = 0
			}
			if changes != expected {
				t.Fatalf("activity count = %d, want %d", changes, expected)
			}
		})
	}
}
