package service

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

func TestAutopilotDispatchSettlementFreezesIssueStatus(t *testing.T) {
	for _, status := range []string{"blocked", "in_review", "done"} {
		for _, outcome := range []string{"completed", "failed"} {
			t.Run(status+"/"+outcome, func(t *testing.T) {
				f := v1ReviewScanSetup(t, "create_issue")
				ctx := t.Context()
				taskID := f.fx.Task(t, f.agentID, testutil.Cols{"runtime_id": f.runtime, "issue_id": f.issueID, "status": "running"})
				f.fx.Exec(t, "UPDATE autopilot_run SET task_id=$1 WHERE id=$2", taskID, f.run.ID)
				f.fx.Exec(t, "UPDATE issue SET status=$1 WHERE id=$2", status, f.issueID)
				issue, err := f.q.GetIssue(ctx, util.MustParseUUID(f.issueID))
				if err != nil {
					t.Fatal(err)
				}
				svc := f.syncer()
				svc.SyncRunFromIssue(ctx, issue)
				run, err := f.q.GetAutopilotRun(ctx, f.run.ID)
				if err != nil || run.Status != "issue_created" {
					t.Fatalf("issue status settled active dispatch: %s %v", run.Status, err)
				}
				f.fx.Exec(t, "UPDATE agent_task_queue SET status=$1,completed_at=now() WHERE id=$2", outcome, taskID)
				task, err := f.q.GetAgentTask(ctx, util.MustParseUUID(taskID))
				if err != nil {
					t.Fatal(err)
				}
				svc.SyncRunFromLinkedIssueTask(ctx, task)
				assertFrozen := func() {
					t.Helper()
					run, err := f.q.GetAutopilotRun(ctx, f.run.ID)
					var result map[string]string
					if err != nil || json.Unmarshal(run.Result, &result) != nil || run.Status != outcome || result["first_terminal_status"] != status {
						t.Fatalf("settlement lost outcome or frozen status: status=%s result=%s err=%v", run.Status, run.Result, err)
					}
				}
				assertFrozen()
				f.fx.Exec(t, "UPDATE issue SET status='cancelled' WHERE id=$1", f.issueID)
				issue.Status = "cancelled"
				svc.SyncRunFromIssue(ctx, issue)
				svc.SyncRunFromLinkedIssueTask(ctx, task)
				assertFrozen()
			})
		}
	}
}
