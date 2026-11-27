package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestIssueDispatchAfterConsecutiveFailures(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := t.Context()
	runtimeID := dbfx.Runtime(t, "Failure streak runtime", testutil.Cols{
		"daemon_id": "failure-streak-" + uuid.NewString(), "runtime_mode": "local",
		"provider": "opencode", "status": "online", "owner_id": testUserID,
	})
	agentID := dbfx.Agent(t, "Failure streak agent "+uuid.NewString(), runtimeID, testutil.Cols{"runtime_mode": "local"})
	otherAgentID := dbfx.Agent(t, "Other agent "+uuid.NewString(), runtimeID, testutil.Cols{"runtime_mode": "local"})

	for _, tc := range []struct {
		name             string
		failedRuns       int
		success          bool
		successByOther   bool
		automaticSuccess bool
		legacySuccess    bool
		failureAfter     bool
		manual           bool
		wantQueued       bool
	}{
		{name: "two failures still allow automatic dispatch", failedRuns: 2, wantQueued: true},
		{name: "three failures stop automatic dispatch", failedRuns: 3},
		{name: "successful manual run reopens automatic dispatch", failedRuns: 3, success: true, wantQueued: true},
		{name: "another agent's success leaves the streak intact", failedRuns: 3, success: true, successByOther: true},
		{name: "an already running automatic task cannot reopen dispatch", failedRuns: 3, automaticSuccess: true},
		{name: "automatic success before the pause breaks the streak", failedRuns: 2, automaticSuccess: true, failureAfter: true, wantQueued: true},
		{name: "legacy success keeps recovered issues runnable", failedRuns: 3, legacySuccess: true, wantQueued: true},
		{name: "human can explicitly dispatch after three failures", failedRuns: 3, manual: true, wantQueued: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issueID := dbfx.Issue(t, "Consecutive execution failures", testutil.Cols{
				"assignee_type": "agent", "assignee_id": agentID,
			})
			issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			for minutesAgo := tc.failedRuns + 1; minutesAgo > 1; minutesAgo-- {
				at := now.Add(-time.Duration(minutesAgo) * time.Minute)
				dbfx.Task(t, agentID, testutil.Cols{
					"issue_id": issue.ID, "runtime_id": runtimeID,
					"status": "failed", "failure_reason": "agent_error.process_failure",
					"created_at": at, "started_at": at, "completed_at": at,
				})
			}
			if tc.successByOther {
				at := now.Add(-time.Minute)
				dbfx.Task(t, otherAgentID, testutil.Cols{
					"issue_id": issue.ID, "runtime_id": runtimeID,
					"status": "completed", "created_at": at, "started_at": at, "completed_at": at,
				})
			}

			if tc.success && !tc.successByOther {
				_, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue)
				if !errors.Is(err, service.ErrIssueAgentConsecutiveFailures) {
					t.Fatalf("dispatch before manual recovery: %v", err)
				}
				manual, err := testHandler.TaskService.EnqueueTaskForIssueByActor(ctx, issue, issue.CreatorID)
				if err != nil {
					t.Fatalf("start manual recovery: %v", err)
				}
				var manualDispatch bool
				dbfx.QueryRow(t, "SELECT COALESCE(context->>'manual_issue_dispatch' = 'true', false) FROM agent_task_queue WHERE id=$1", manual.ID).Scan(&manualDispatch)
				if !manualDispatch {
					t.Fatal("manual recovery run was not identified on its stored task")
				}
				dbfx.Exec(t, "UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1", manual.ID)
				if _, err := testHandler.TaskService.CompleteTask(ctx, manual.ID, []byte("{}"), "", "", "", false, "", ""); err != nil {
					t.Fatalf("complete manual recovery: %v", err)
				}
			}
			if tc.automaticSuccess {
				at := now.Add(-10 * time.Minute)
				automaticID := parseUUID(dbfx.Task(t, agentID, testutil.Cols{
					"issue_id": issue.ID, "runtime_id": runtimeID, "status": "running",
					"created_at": at, "started_at": at,
				}))
				if _, err := testHandler.TaskService.CompleteTask(ctx, automaticID, []byte("{}"), "", "", "", false, "", ""); err != nil {
					t.Fatalf("complete already running automatic task: %v", err)
				}
			}
			if tc.legacySuccess {
				at := now.Add(-time.Minute)
				dbfx.Task(t, agentID, testutil.Cols{
					"issue_id": issue.ID, "runtime_id": runtimeID,
					"status": "completed", "created_at": at, "started_at": at, "completed_at": at,
				})
			}
			if tc.failureAfter {
				at := time.Now()
				dbfx.Task(t, agentID, testutil.Cols{
					"issue_id": issue.ID, "runtime_id": runtimeID,
					"status": "failed", "created_at": at, "started_at": at, "completed_at": at,
				})
			}
			if tc.manual {
				_, err = testHandler.TaskService.EnqueueTaskForIssueByActor(ctx, issue, issue.CreatorID)
			} else {
				_, err = testHandler.TaskService.EnqueueTaskForIssue(ctx, issue)
			}
			if tc.wantQueued && err != nil || !tc.wantQueued && !errors.Is(err, service.ErrIssueAgentConsecutiveFailures) {
				t.Fatalf("unexpected dispatch result for want queued = %v: %v", tc.wantQueued, err)
			}
			queued := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'", issueID, agentID)
			if got := queued == 1; queued > 1 || got != tc.wantQueued {
				t.Fatalf("queued runs = %d, want queued = %v, dispatch error = %v", queued, tc.wantQueued, err)
			}
		})
	}
}

func TestIssueFailureStreakStopsAutomaticRetries(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := t.Context()
	runtimeID := dbfx.Runtime(t, "Failure streak retry runtime")
	agentID := dbfx.Agent(t, "Failure streak retry agent "+uuid.NewString(), runtimeID)

	for _, path := range []string{"failure report", "orphan recovery"} {
		t.Run(path, func(t *testing.T) {
			issueID := dbfx.Issue(t, "Repeated infrastructure failure")
			now := time.Now()
			for _, minutesAgo := range []int{3, 2} {
				at := now.Add(-time.Duration(minutesAgo) * time.Minute)
				dbfx.Task(t, agentID, testutil.Cols{
					"issue_id": issueID, "runtime_id": runtimeID,
					"status": "failed", "failure_reason": "timeout",
					"created_at": at, "started_at": at, "completed_at": at,
				})
			}
			parent := testutil.Cols{
				"issue_id": issueID, "runtime_id": runtimeID, "status": "running",
				"started_at": now.Add(-time.Minute), "attempt": 1, "max_attempts": 2,
			}
			if path == "orphan recovery" {
				parent["status"] = "failed"
				parent["failure_reason"] = "timeout"
				parent["completed_at"] = now
			}
			parentID := parseUUID(dbfx.Task(t, agentID, parent))
			if path == "failure report" {
				if _, err := testHandler.TaskService.FailTask(ctx, parentID, "execution timed out", "", "", "", "timeout", false, "", ""); err != nil {
					t.Fatalf("report failure: %v", err)
				}
			} else {
				task, err := testHandler.Queries.GetAgentTask(ctx, parentID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := testHandler.TaskService.MaybeRetryFailedTask(ctx, task); err != nil {
					t.Fatalf("recover failed task: %v", err)
				}
			}
			var status string
			dbfx.QueryRow(t, "SELECT status FROM agent_task_queue WHERE id = $1", parentID).Scan(&status)
			if status != "failed" {
				t.Fatalf("failed parent status = %q", status)
			}
			if children := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1", parentID); children != 0 {
				t.Fatalf("automatic retry created %d child runs after three failures", children)
			}
		})
	}
}

func TestConcurrentAutomaticSuccessesDoNotClearIssueFailurePause(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := t.Context()
	runtimeID := dbfx.Runtime(t, "Concurrent failure streak runtime")
	agentID := dbfx.Agent(t, "Concurrent failure streak agent "+uuid.NewString(), runtimeID)
	issueID := dbfx.Issue(t, "Concurrent outcomes after a failure streak", testutil.Cols{
		"assignee_type": "agent", "assignee_id": agentID,
	})
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, minutesAgo := range []int{3, 2, 1} {
		at := now.Add(-time.Duration(minutesAgo) * time.Minute)
		dbfx.Task(t, agentID, testutil.Cols{
			"issue_id": issue.ID, "runtime_id": runtimeID, "status": "failed",
			"created_at": at, "started_at": at, "completed_at": at,
		})
	}
	if _, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue); !errors.Is(err, service.ErrIssueAgentConsecutiveFailures) {
		t.Fatalf("automatic dispatch before concurrent completions: %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, minutesAgo := range []int{5, 4} {
		at := now.Add(-time.Duration(minutesAgo) * time.Minute)
		taskID := parseUUID(dbfx.Task(t, agentID, testutil.Cols{
			"issue_id": issue.ID, "runtime_id": runtimeID, "status": "running",
			"created_at": at, "started_at": at,
		}))
		go func() {
			<-start
			_, err := testHandler.TaskService.CompleteTask(ctx, taskID, []byte("{}"), "", "", "", false, "", "")
			results <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Errorf("complete automatic task: %v", err)
		}
	}
	if _, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue); !errors.Is(err, service.ErrIssueAgentConsecutiveFailures) {
		t.Fatalf("automatic dispatch after concurrent completions: %v", err)
	}
}

func TestAutomaticCompletionOverlappingThirdIssueFailure(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := t.Context()
	runtimeID := dbfx.Runtime(t, "Overlapping issue outcomes runtime")
	agentID := dbfx.Agent(t, "Overlapping issue outcomes agent "+uuid.NewString(), runtimeID)
	issueID := parseUUID(dbfx.Issue(t, "Third issue failure overlaps an automatic completion", testutil.Cols{
		"assignee_type": "agent", "assignee_id": agentID,
	}))
	issue, err := testHandler.Queries.GetIssue(ctx, issueID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, minutesAgo := range []int{2, 1} {
		at := now.Add(-time.Duration(minutesAgo) * time.Minute)
		dbfx.Task(t, agentID, testutil.Cols{
			"issue_id": issueID, "runtime_id": runtimeID, "status": "failed",
			"created_at": at, "started_at": at, "completed_at": at,
		})
	}
	running := testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "running", "started_at": now.Add(-3 * time.Minute)}
	failedID := parseUUID(dbfx.Task(t, agentID, running))
	completedID := parseUUID(dbfx.Task(t, agentID, running))

	failService := service.NewTaskService(testHandler.Queries, beforeIssueFailureCommitStarter{
		base: testPool,
		beforeCommit: func(ctx context.Context) error {
			// Keep the third failure invisible while the other run reports success.
			_, err := testHandler.TaskService.CompleteTask(ctx, completedID, []byte("{}"), "", "", "", false, "", "")
			return err
		},
	}, testHandler.TaskService.Hub, testHandler.TaskService.Bus)
	if _, err := failService.FailTask(ctx, failedID, "process failed", "", "", "", "agent_error.process_failure", false, "", ""); err != nil {
		t.Fatalf("fail third run: %v", err)
	}
	if _, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue); !errors.Is(err, service.ErrIssueAgentConsecutiveFailures) {
		t.Fatalf("automatic dispatch after overlapping terminal reports: %v", err)
	}
}

type beforeIssueFailureCommitStarter struct {
	base         service.TxStarter
	beforeCommit func(context.Context) error
}

func (s beforeIssueFailureCommitStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.base.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return beforeIssueFailureCommitTx{Tx: tx, beforeCommit: s.beforeCommit}, nil
}

type beforeIssueFailureCommitTx struct {
	pgx.Tx
	beforeCommit func(context.Context) error
}

func (tx beforeIssueFailureCommitTx) Commit(ctx context.Context) error {
	if err := tx.beforeCommit(ctx); err != nil {
		return err
	}
	return tx.Tx.Commit(ctx)
}
