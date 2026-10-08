package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/events"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Exercise the same service entry point as the daemon /fail callback, including
// its terminal-transition guard, retry decision and failure comment.
func TestFailTaskReconcilesIssueStatus(t *testing.T) {
	for _, tc := range []struct {
		name, status, sibling, reason, want string
		retry                               bool
	}{
		{name: "terminal capacity failure", status: "in_progress", want: "todo"},
		{name: "running sibling", status: "in_progress", sibling: "running", want: "in_progress"},
		{name: "queued sibling", status: "in_progress", sibling: "queued", want: "in_progress"},
		{name: "dispatched sibling", status: "in_progress", sibling: "dispatched", want: "in_progress"},
		{name: "directory wait", status: "in_progress", sibling: "waiting_local_directory", want: "in_progress"},
		{name: "deferred sibling", status: "in_progress", sibling: "deferred", want: "in_progress"},
		{name: "immediate retry", status: "in_progress", reason: "timeout", retry: true, want: "in_progress"},
		{name: "deferred retry", status: "in_progress", reason: "runtime_offline", retry: true, want: "in_progress"},
		{name: "human review", status: "in_review", want: "in_review"},
		{name: "human blocked", status: "blocked", want: "blocked"},
		{name: "custom started", status: "custom_started", want: "custom_started"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc, pool, userID, agentID, issueID, runtimeID := rerunQueueFixture(t)
			issue, err := svc.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
			if err != nil {
				t.Fatal(err)
			}
			fx := dbfx.New(pool, util.UUIDToString(issue.WorkspaceID), userID)
			if tc.status == "custom_started" {
				fx.Insert(t, "issue_status", dbfx.Cols{"workspace_id": fx.WorkspaceID, "key": tc.status, "name": "Custom", "category": "started", "color": "#22c55e", "position": 1})
			}
			fx.Exec(t, `UPDATE issue SET status = $2 WHERE id = $1`, issueID, tc.status)
			taskID := util.MustParseUUID(fx.Task(t, agentID, dbfx.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "running", "attempt": 1, "max_attempts": 2}))
			if tc.sibling != "" {
				fx.Task(t, agentID, dbfx.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": tc.sibling})
			}
			fx.Cleanup(t, `DELETE FROM comment WHERE issue_id = $1`, issueID)
			fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE parent_task_id = $1`, taskID)
			updates := 0
			svc.Bus.Subscribe("issue:updated", func(e events.Event) {
				updates++
				payload := e.Payload.(map[string]any)
				if payload["prev_status"] != "in_progress" || payload["status_changed"] != true {
					t.Errorf("unexpected issue update: %#v", payload)
				}
			})
			reason := tc.reason
			if reason == "" {
				reason = "agent_error.provider_capacity_or_rate_limit"
			}
			for call := 0; call < 2; call++ {
				task, transitioned, err := svc.FailTaskWithTransition(ctx, taskID, "HTTP 429 provider capacity", "", "", "", reason, false, "", "")
				if err != nil {
					t.Fatal(err)
				}
				if task.Status != "failed" || transitioned != (call == 0) {
					t.Fatalf("call %d: status=%s transitioned=%v", call, task.Status, transitioned)
				}
			}
			got, err := svc.Queries.GetIssue(ctx, issue.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want {
				t.Errorf("issue status = %q, want %q", got.Status, tc.want)
			}
			wantUpdates := 0
			if tc.want != tc.status {
				wantUpdates = 1
			}
			if updates != wantUpdates {
				t.Errorf("issue updates = %d, want %d", updates, wantUpdates)
			}
			var comments, children int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1 AND source_task_id = $2`, issueID, taskID).Scan(&comments); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1`, taskID).Scan(&children); err != nil {
				t.Fatal(err)
			}
			wantComments, wantChildren := 1, 0
			if tc.retry {
				wantComments, wantChildren = 0, 1
			}
			if comments != wantComments || children != wantChildren {
				t.Errorf("comments=%d children=%d, want %d/%d", comments, children, wantComments, wantChildren)
			}
		})
	}
}

func TestFailureReconciliationPreservesNewerIssueRevision(t *testing.T) {
	ctx := context.Background()
	svc, pool, userID, agentID, issueID, runtimeID := rerunQueueFixture(t)
	issue, err := svc.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	fx := dbfx.New(pool, util.UUIDToString(issue.WorkspaceID), userID)
	taskID := util.MustParseUUID(fx.Task(t, agentID, dbfx.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "failed"}))
	for _, status := range []string{"blocked", "in_review", "in_progress"} {
		// A human write lands after the reconciler read, including a decision that
		// leaves the issue active. A stale revision must never overwrite it.
		fx.Exec(t, `UPDATE issue SET status = $2, revision = revision + 1 WHERE id = $1`, issueID, status)
		_, err := svc.Queries.ReconcileIssueAfterTaskFailure(ctx, db.ReconcileIssueAfterTaskFailureParams{
			ID: issue.ID, WorkspaceID: issue.WorkspaceID, SourceTaskID: taskID, ExpectedRevision: issue.Revision,
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("status %s: stale update returned %v", status, err)
		}
		got, err := svc.Queries.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != status {
			t.Fatalf("overwrote newer status %s with %s", status, got.Status)
		}
	}
}

func TestHandleFailedTasksSharesIssueReconciliation(t *testing.T) {
	ctx := context.Background()
	svc, pool, userID, agentID, issueID, runtimeID := rerunQueueFixture(t)
	issue, err := svc.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	fx := dbfx.New(pool, util.UUIDToString(issue.WorkspaceID), userID)
	fx.Exec(t, `UPDATE issue SET status = 'in_progress' WHERE id = $1`, issueID)
	id := util.MustParseUUID(fx.Task(t, agentID, dbfx.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "failed", "failure_reason": "agent_error.provider_capacity_or_rate_limit"}))
	failed, err := svc.Queries.GetAgentTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	updates := 0
	svc.Bus.Subscribe("issue:updated", func(events.Event) { updates++ })
	for i := 0; i < 2; i++ {
		if retries := svc.HandleFailedTasks(ctx, []db.AgentTaskQueue{failed}); retries != 0 {
			t.Fatalf("unexpected retries: %d", retries)
		}
	}
	got, err := svc.Queries.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "todo" || updates != 1 {
		t.Fatalf("status=%s updates=%d, want todo/1", got.Status, updates)
	}
}
