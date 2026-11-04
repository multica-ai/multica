package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueWakeupManualActorSelfContinuation(t *testing.T) {
	for _, actor := range []string{"member", "agent"} {
		t.Run(actor, func(t *testing.T) {
			ctx := context.Background()
			issue := dbfx.Issue(t, "manual continuation")
			agent := dbfx.Agent(t, "manual continuation", testRuntimeID)
			dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
			dbfx.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN(SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
			dbfx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id=$1", issue)
			dbfx.Cleanup(t, "DELETE FROM activity_log WHERE issue_id=$1", issue)
			source := parseUUID(dbfx.Task(t, agent, testutil.Cols{
				"runtime_id": testRuntimeID, "issue_id": issue, "status": "running", "started_at": testutil.Raw("now()"),
				"originator_user_id": testUserID, "accountable_user_id": testUserID,
			}))
			svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
			in := service.WakeupInput{AgentID: agent, Kind: "at", AfterSeconds: 600, Instruction: "Check again"}
			for link := 0; link < 8; link++ {
				w, err := svc.Create(ctx, parseUUID(issue), parseUUID(testUserID), source, in)
				if err != nil {
					t.Fatalf("link %d: %v", link+1, err)
				}
				req := withURLParams(newRequest(http.MethodPost, "/", nil), "id", issue, "wakeupID", uuidToString(w.ID))
				if actor == "agent" {
					req.Header.Set("X-Actor-Source", "task_token")
					req.Header.Set("X-Agent-ID", agent)
					req.Header.Set("X-Task-ID", uuidToString(source))
				}
				testutil.Call(t, testHandler.TriggerIssueWakeup, req).Want(http.StatusNoContent)
				fired, err := testHandler.Queries.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
				if err != nil {
					t.Fatal(err)
				}
				if !fired.LastTaskID.Valid || fired.FireCount != 1 {
					t.Fatalf("manual wakeup did not start exactly one run: %+v", fired)
				}
				dbfx.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1", source)
				source = fired.LastTaskID
				dbfx.Exec(t, "UPDATE agent_task_queue SET status='running',started_at=now() WHERE id=$1", source)
				task, err := testHandler.Queries.GetAgentTask(ctx, source)
				if err != nil {
					t.Fatal(err)
				}
				if uuidToString(task.OriginatorUserID) != testUserID {
					t.Fatal("manual trigger changed the permission originator")
				}
			}
			req := withURLParam(newRequest(http.MethodPost, "/", in), "id", issue)
			req.Header.Set("X-Actor-Source", "task_token")
			req.Header.Set("X-Agent-ID", agent)
			req.Header.Set("X-Task-ID", uuidToString(source))
			want := http.StatusCreated
			if actor == "agent" {
				want = http.StatusBadRequest
			}
			testutil.Call(t, testHandler.CreateIssueWakeup, req).Want(want)
		})
	}
}
