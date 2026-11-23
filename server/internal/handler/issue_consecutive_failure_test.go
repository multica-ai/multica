package handler

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

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
		name           string
		failedRuns     int
		success        bool
		successByOther bool
		manual         bool
		wantQueued     bool
	}{
		{name: "two failures still allow automatic dispatch", failedRuns: 2, wantQueued: true},
		{name: "three failures stop automatic dispatch", failedRuns: 3},
		{name: "successful manual run reopens automatic dispatch", failedRuns: 3, success: true, wantQueued: true},
		{name: "another agent's success leaves the streak intact", failedRuns: 3, success: true, successByOther: true},
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
				dbfx.Exec(t, "UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1", manual.ID)
				if _, err := testHandler.TaskService.CompleteTask(ctx, manual.ID, []byte("{}"), "", "", "", false, "", ""); err != nil {
					t.Fatalf("complete manual recovery: %v", err)
				}
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
