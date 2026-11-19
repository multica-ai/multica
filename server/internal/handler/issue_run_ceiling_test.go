package handler

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestIssueRunCeilingsAtEnqueue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := t.Context()
	agentID, _, _ := createRuntimeGuardAgent(t, ctx)

	for _, tc := range []struct {
		name       string
		limits     testutil.Cols
		constraint string
	}{
		{name: "unlimited"},
		{name: "issue", limits: testutil.Cols{"max_runs_per_issue": 5}, constraint: "issue_run_limit"},
		{name: "agent", limits: testutil.Cols{"max_runs_per_agent_per_issue": 5}, constraint: "issue_agent_run_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectID := dbfx.Project(t, "Run ceilings", tc.limits)
			issueID := parseUUID(dbfx.Issue(t, "Repeated dispatch", testutil.Cols{
				"assignee_type": "agent", "assignee_id": agentID, "project_id": projectID,
			}))
			issue, err := testHandler.Queries.GetIssue(ctx, issueID)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				dbfx.Exec(t, "DELETE FROM agent_task_queue WHERE issue_id = $1", issueID)
			})

			for dispatch := 1; dispatch <= 6; dispatch++ {
				task, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue)
				if dispatch == 6 && tc.constraint != "" {
					var pgErr *pgconn.PgError
					if !errors.As(err, &pgErr) || pgErr.ConstraintName != tc.constraint {
						t.Fatalf("dispatch %d: got %v, want %s", dispatch, err, tc.constraint)
					}
					return
				}
				if err != nil {
					t.Fatalf("dispatch %d: %v", dispatch, err)
				}
				if dispatch == 5 && tc.constraint != "" {
					dbfx.Exec(t, "UPDATE agent_task_queue SET status = 'running', started_at = now(), max_attempts = 2 WHERE id = $1", task.ID)
					if _, err := testHandler.TaskService.FailTask(ctx, task.ID, "runtime went away", "", "", "", "timeout", false, "", ""); err != nil {
						t.Fatalf("fail dispatch at ceiling: %v", err)
					}
					var status string
					dbfx.QueryRow(t, "SELECT status FROM agent_task_queue WHERE id = $1", task.ID).Scan(&status)
					if status != "failed" {
						t.Fatalf("parent status = %q, want failed", status)
					}
					if children := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1", task.ID); children != 0 {
						t.Fatalf("retry children = %d at ceiling", children)
					}
					continue
				}
				dbfx.Exec(t, "UPDATE agent_task_queue SET status = 'completed', started_at = now(), completed_at = now() WHERE id = $1", task.ID)
			}
		})
	}
}
