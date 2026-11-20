package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

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

func TestProjectIssueRunCeilingsUpdate(t *testing.T) {
	projectID := dbfx.Project(t, "Run ceiling settings", nil)
	url := "/api/projects/" + projectID
	update := func(body map[string]any) *testutil.Response {
		req := withURLParam(newRequest(http.MethodPut, url, body), "id", projectID)
		return testutil.Call(t, testHandler.UpdateProject, req)
	}
	var project ProjectResponse
	update(map[string]any{"max_runs_per_issue": 5, "max_runs_per_agent_per_issue": 3}).Want(http.StatusOK).JSON(&project)
	if project.MaxRunsPerIssue == nil || *project.MaxRunsPerIssue != 5 ||
		project.MaxRunsPerAgentPerIssue == nil || *project.MaxRunsPerAgentPerIssue != 3 {
		t.Fatalf("configured run limits = %+v", project)
	}
	update(map[string]any{"max_runs_per_issue": 0}).Want(http.StatusBadRequest)
	update(map[string]any{"max_runs_per_agent_per_issue": nil}).Want(http.StatusOK)
	req := withURLParam(newRequest(http.MethodGet, url, nil), "id", projectID)
	testutil.Call(t, testHandler.GetProject, req).Want(http.StatusOK).JSON(&project)
	if project.MaxRunsPerIssue == nil || *project.MaxRunsPerIssue != 5 || project.MaxRunsPerAgentPerIssue != nil {
		t.Fatalf("persisted run limits after clear = %+v", project)
	}
}

func TestProjectIssueRunCeilingSerializesAgents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	agentID, runtimeID, _ := createRuntimeGuardAgent(t, t.Context())
	secondAgentID := dbfx.Agent(t, "Second agent", runtimeID)
	projectID := dbfx.Project(t, "One run project", testutil.Cols{"max_runs_per_issue": 1})
	issueID := parseUUID(dbfx.Issue(t, "One remaining slot", testutil.Cols{
		"assignee_type": "agent", "assignee_id": secondAgentID, "project_id": projectID,
	}))
	issue, err := testHandler.Queries.GetIssue(ctx, issueID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		dbfx.Exec(t, "DELETE FROM agent_task_queue WHERE issue_id = $1", issueID)
	})
	first, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback(context.Background())
	if _, err := first.Exec(ctx, `INSERT INTO agent_task_queue (issue_id, agent_id, runtime_id, status)
		VALUES ($1, $2, $3, 'queued')`, issueID, agentID, runtimeID); err != nil {
		t.Fatal(err)
	}

	second := make(chan error, 1)
	go func() {
		_, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue)
		second <- err
	}()
	if !waitForWaiterBlockedBy(t, holderBackendPID(t, ctx, first), 5*time.Second) {
		t.Fatal("second agent did not wait for the first agent's insertion")
	}
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-second:
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.ConstraintName != "issue_run_limit" {
			t.Fatalf("second enqueue: got %v, want issue_run_limit", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if count := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1", issueID); count != 1 {
		t.Fatalf("inserted %d runs, want 1", count)
	}
}

func TestIssueRunCeilingRerunKeepsPendingTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := t.Context()
	agentID, _, _ := createRuntimeGuardAgent(t, ctx)
	projectID := dbfx.Project(t, "Single run project", testutil.Cols{"max_runs_per_issue": 1})
	issueID := parseUUID(dbfx.Issue(t, "Keep the pending run", testutil.Cols{
		"assignee_type": "agent", "assignee_id": agentID, "project_id": projectID,
	}))
	t.Cleanup(func() {
		dbfx.Exec(t, "DELETE FROM agent_task_queue WHERE issue_id = $1", issueID)
	})
	issue, err := testHandler.Queries.GetIssue(ctx, issueID)
	if err != nil {
		t.Fatal(err)
	}
	task, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		t.Fatal(err)
	}
	id := uuidToString(issueID)
	req := withURLParam(newRequest(http.MethodPost, "/api/issues/"+id+"/rerun", map[string]any{
		"task_id": uuidToString(task.ID),
	}), "id", id)
	blocked := testutil.Call(t, testHandler.RerunIssue, req).Want(http.StatusConflict).Map()
	if blocked["reason_code"] != string(ReasonIssueRunLimit) {
		t.Fatalf("rerun reason = %v, want %s", blocked["reason_code"], ReasonIssueRunLimit)
	}
	stored, err := testHandler.Queries.GetAgentTask(ctx, task.ID)
	if err != nil || stored.Status != "queued" {
		t.Fatalf("original queued task was changed: status=%q err=%v", stored.Status, err)
	}
}
