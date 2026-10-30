package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Closing an issue ends its wakeups in the status write's own transaction, not
// through a database trigger, on every writer that can close an issue.
func TestIssueCloseStopsWakeupsInApplication(t *testing.T) {
	if n := dbfx.Count(t, "SELECT count(*) FROM pg_trigger WHERE tgname='stop_issue_wakeups'"); n != 0 {
		t.Fatal("issue close still cascades through a database trigger")
	}
	ctx := context.Background()
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	agent := dbfx.Agent(t, "wake close", testRuntimeID)
	for _, tc := range []struct {
		name  string
		close func(t *testing.T, issue string)
	}{
		{"update", func(t *testing.T, issue string) {
			req := withURLParam(newRequest(http.MethodPut, "/api/issues/"+issue, map[string]any{"status": "done"}), "id", issue)
			testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusOK)
		}},
		{"batch", func(t *testing.T, issue string) {
			req := newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{"issue_ids": []string{issue}, "updates": map[string]any{"status": "cancelled"}})
			testutil.Call(t, testHandler.BatchUpdateIssues, req).Want(http.StatusOK)
		}},
		{"merged pull request", func(t *testing.T, issue string) {
			linkMergedGitHubPRForTest(t, issue, "wake-close")
			testHandler.maybeAutoCompleteIssue(ctx, parseUUID(testWorkspaceID), parseUUID(issue), nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issue := dbfx.Issue(t, "wake close "+tc.name)
			dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
			dbfx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id=$1", issue)
			w, err := svc.Create(ctx, parseUUID(issue), parseUUID(testUserID), pgtype.UUID{}, service.WakeupInput{AgentID: agent, Kind: "at", AfterSeconds: 600, Instruction: "check"})
			if err != nil {
				t.Fatal(err)
			}
			pending := dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": testRuntimeID, "context": `{"wakeup_id":"` + uuidToString(w.ID) + `"}`})
			tc.close(t, issue)
			if n := dbfx.Count(t, "SELECT count(*) FROM issue_wakeup WHERE id=$1 AND NOT enabled AND disabled_at IS NOT NULL", w.ID); n != 1 {
				t.Fatal("closed issue kept its wakeup")
			}
			var status, reason string
			dbfx.QueryRow(t, "SELECT status,error FROM agent_task_queue WHERE id=$1", pending).Scan(&status, &reason)
			if status != "cancelled" || reason != "Issue closed; wakeup disabled" {
				t.Fatalf("unstarted wakeup run = %s (%s)", status, reason)
			}
		})
	}
}
