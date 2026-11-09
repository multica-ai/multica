package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestRerunTaskByUserContinuity(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, tc := range []struct {
		name              string
		fresh, reassigned bool
	}{
		{name: "assignee reuse"},
		{name: "assignee fresh", fresh: true},
		{name: "reassigned reuse", reassigned: true},
		{name: "reassigned fresh", fresh: true, reassigned: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh := tc.fresh
			agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, context.Background())
			currentAgentID := agentID
			if tc.reassigned {
				currentAgentID = createHandlerTestAgent(t, "New assignee", []byte("[]"))
			}
			issueID := dbfx.Issue(t, "rerun a displaced agent", testutil.Cols{
				"assignee_type": "agent", "assignee_id": currentAgentID,
			})
			commentID := dbfx.Comment(t, issueID, "continue this work")
			sourceID := dbfx.Task(t, agentID, testutil.Cols{
				"runtime_id": runtimeID, "issue_id": issueID, "status": "completed",
				"session_id": "source-session", "work_dir": "/tmp/source-workdir",
				"trigger_comment_id": commentID,
			})
			req := withURLParam(newRequest(http.MethodPost, "/api/tasks/"+sourceID+"/rerun", map[string]any{"fresh": fresh}), "taskId", sourceID)
			var result struct {
				ID string `json:"id"`
			}
			testutil.Call(t, testHandler.RerunTaskByUser, withChatTestWorkspaceCtx(t, req)).Want(http.StatusAccepted).JSON(&result)
			row, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(result.ID))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, result.ID) })
			if uuidToString(row.AgentID) != agentID || uuidToString(row.RerunOfTaskID) != sourceID || uuidToString(row.TriggerCommentID) != commentID {
				t.Fatalf("rerun lost source agent, lineage, or comment: %+v", row)
			}
			if !row.ForceFreshSession || service.IsFreshRerun(row) != fresh {
				t.Fatalf("fresh=%v: unexpected stored continuity flags", fresh)
			}
			claimed := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
			if fresh {
				if claimed.PriorSessionID != "" || claimed.PriorWorkDir != "" {
					t.Fatalf("fresh rerun resumed %q / %q", claimed.PriorSessionID, claimed.PriorWorkDir)
				}
			} else if claimed.PriorSessionID != "source-session" || claimed.PriorWorkDir != "/tmp/source-workdir" {
				t.Fatalf("default rerun lost continuity: %q / %q", claimed.PriorSessionID, claimed.PriorWorkDir)
			}
		})
	}
}

func TestRerunTaskByUserRejectsInvalidSource(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "RerunSource", []byte("[]"))
	foreignWorkspaceID := dbfx.Workspace(t, "foreign rerun workspace", "foreign-rerun-workspace")
	foreignIssueID := dbfx.Issue(t, "foreign issue", testutil.Cols{"workspace_id": foreignWorkspaceID})
	foreignAgentID := dbfx.Agent(t, "foreign agent", testRuntimeID, testutil.Cols{"workspace_id": foreignWorkspaceID})
	for _, tc := range []struct {
		name, agent string
		cols        testutil.Cols
		code        int
	}{
		{name: "issue-less", agent: agentID, cols: testutil.Cols{}, code: http.StatusBadRequest},
		{name: "foreign issue", agent: agentID, cols: testutil.Cols{"issue_id": foreignIssueID}, code: http.StatusNotFound},
		{name: "foreign task", agent: foreignAgentID, cols: testutil.Cols{"issue_id": foreignIssueID}, code: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.cols["runtime_id"] = testRuntimeID
			sourceID := dbfx.Task(t, tc.agent, tc.cols)
			req := withURLParam(newRequest(http.MethodPost, "/api/tasks/"+sourceID+"/rerun", nil), "taskId", sourceID)
			testutil.Call(t, testHandler.RerunTaskByUser, withChatTestWorkspaceCtx(t, req)).Want(tc.code)
			row, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(sourceID))
			if err != nil || row.Status != "queued" {
				t.Fatalf("rejected source mutated: %s, %v", row.Status, err)
			}
		})
	}
}

func TestRerunTaskByUserPrivateHistoricalAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, ownerID, _ := privateAgentTestFixture(t)
	currentAgentID := createHandlerTestAgent(t, "Current public assignee", []byte("[]"))
	issueID := dbfx.Issue(t, "private historical run", testutil.Cols{
		"assignee_type": "agent", "assignee_id": currentAgentID,
	})
	sourceID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": testRuntimeID})
	for _, fresh := range []bool{false, true} {
		req := withURLParam(newRequest(http.MethodPost, "/api/tasks/"+sourceID+"/rerun", map[string]any{"fresh": fresh}), "taskId", sourceID)
		testutil.Call(t, testHandler.RerunTaskByUser, withChatTestWorkspaceCtx(t, req)).Want(http.StatusForbidden)
		if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID); n != 1 {
			t.Fatalf("denied rerun created tasks: count=%d", n)
		}
		row, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(sourceID))
		if err != nil || row.Status != "queued" {
			t.Fatalf("denied rerun cancelled source: %s, %v", row.Status, err)
		}
	}
	req := withURLParam(newRequestAs(ownerID, http.MethodPost, "/api/tasks/"+sourceID+"/rerun", nil), "taskId", sourceID)
	var result struct {
		ID string `json:"id"`
	}
	testutil.Call(t, testHandler.RerunTaskByUser, withChatTestWorkspaceCtx(t, req)).Want(http.StatusAccepted).JSON(&result)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, result.ID) })
}
