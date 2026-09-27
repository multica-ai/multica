package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// GH #8849: closing the last child from a parent turn must not queue a second
// turn for that same assignee. Other sources still need the barrier handoff.
func TestChildDoneSourceTask(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, assignee := range []string{"agent", "squad"} {
			for _, tc := range []struct {
				name        string
				source      string
				status      string
				terminal    string
				unstaged    bool
				worker      bool
				otherSquad  bool
				wantPending int
			}{
				{name: "own_parent_turn", source: "parent", status: "running"},
				{name: "own_parent_cancels_child", source: "parent", status: "running", terminal: "cancelled"},
				{name: "own_parent_unstaged", source: "parent", status: "running", unstaged: true},
				{name: "same_agent_child_turn", source: "child", status: "running", wantPending: 1},
				{name: "other_agent_parent_turn", source: "other_agent", status: "running", wantPending: 1},
				{name: "human_while_assignee_running", source: "human", status: "running", wantPending: 1},
				{name: "completed_parent_turn", source: "parent", status: "completed", wantPending: 1},
				{name: "mismatched_agent_header", source: "mismatch", status: "running", wantPending: 1},
				{name: "leader_in_worker_role", source: "parent", status: "running", worker: true, wantPending: 1},
				{name: "leader_for_other_squad", source: "parent", status: "running", otherSquad: true, wantPending: 1},
			} {
				if assignee == "agent" && (tc.worker || tc.otherSquad) {
					continue
				}
				t.Run(fmt.Sprintf("batch=%t/%s/%s", batch, assignee, tc.name), func(t *testing.T) {
					agentID := createHandlerTestAgent(t, "Barrier assignee", nil)
					assigneeID := agentID
					var squadID string
					if assignee == "squad" {
						squadID = dbfx.Squad(t, "Barrier squad", agentID)
						assigneeID = squadID
					}
					parentID := dbfx.Issue(t, "Barrier parent", testutil.Cols{
						"status": "in_progress", "assignee_type": assignee, "assignee_id": assigneeID,
					})
					childCount := 1
					if batch {
						childCount = 2
					}
					var children []string
					for range childCount {
						cols := testutil.Cols{"parent_issue_id": parentID, "status": "in_review"}
						if !tc.unstaged {
							cols["stage"] = 1
						}
						children = append(children, dbfx.Issue(t, "Barrier child", cols))
					}
					if !tc.unstaged {
						dbfx.Issue(t, "Parked next stage", testutil.Cols{
							"parent_issue_id": parentID, "status": "backlog", "stage": 2,
						})
					}
					sourceIssue, sourceAgent := parentID, agentID
					if tc.source == "child" {
						sourceIssue = children[0]
					}
					if tc.source == "other_agent" || tc.source == "mismatch" {
						sourceAgent = createHandlerTestAgent(t, "Barrier other agent", nil)
					}
					taskCols := testutil.Cols{
						"issue_id": sourceIssue, "runtime_id": handlerTestRuntimeID(t),
						"status": tc.status, "started_at": testutil.Raw("now()"),
						"is_leader_task": assignee == "squad" && !tc.worker,
					}
					if tc.status == "completed" {
						taskCols["completed_at"] = testutil.Raw("now()")
					}
					if squadID != "" {
						taskCols["squad_id"] = squadID
					}
					if tc.otherSquad {
						taskCols["squad_id"] = dbfx.Squad(t, "Other squad", agentID)
					}
					taskID := dbfx.Task(t, sourceAgent, taskCols)
					dbfx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id = $1", parentID)
					dbfx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", parentID)
					terminal := tc.terminal
					if terminal == "" {
						terminal = "done"
					}
					req := testutil.WithURLParams(newRequest(http.MethodPut, "/api/issues/"+children[0],
						map[string]any{"status": terminal}), "id", children[0])
					handler := testHandler.UpdateIssue
					if batch {
						req = newRequest(http.MethodPatch, "/api/issues/batch", map[string]any{
							"issue_ids": children, "updates": map[string]any{"status": terminal},
						})
						handler = testHandler.BatchUpdateIssues
					}
					if tc.source != "human" {
						claimedAgent := sourceAgent
						if tc.source == "mismatch" {
							claimedAgent = agentID
						}
						req = testutil.WithHeaders(req, "X-Agent-ID", claimedAgent, "X-Task-ID", taskID)
					}
					testutil.Call(t, handler, req).Want(http.StatusOK)
					if got := countSystemCommentsOn(t, parentID); got != 1 {
						t.Fatalf("barrier comment must be preserved: got %d", got)
					}
					content := parentSystemCommentContent(t, parentID)
					if !tc.unstaged && !strings.Contains(content, "Stage 1 of this issue is") {
						t.Fatalf("missing stage completion in comment: %s", content)
					}
					if got := countQueuedOrDispatched(t, agentID, parentID); got != tc.wantPending {
						t.Fatalf("pending parent turns: want %d, got %d", tc.wantPending, got)
					}
				})
			}
		}
	}
}

func TestBatchChildDoneSourceOnlySuppressesItsOwnParent(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Batch barrier assignee", nil)
	var parents, children []string
	for range 2 {
		parentID := dbfx.Issue(t, "Batch barrier parent", testutil.Cols{
			"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID,
		})
		parents = append(parents, parentID)
		children = append(children, dbfx.Issue(t, "Batch barrier child", testutil.Cols{
			"parent_issue_id": parentID, "status": "in_review", "stage": 1,
		}))
		dbfx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id = $1", parentID)
		dbfx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", parentID)
	}
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": parents[0], "status": "running", "started_at": testutil.Raw("now()"),
		"runtime_id": handlerTestRuntimeID(t),
	})
	req := testutil.WithHeaders(newRequest(http.MethodPatch, "/api/issues/batch", map[string]any{
		"issue_ids": children, "updates": map[string]any{"status": "done"},
	}), "X-Agent-ID", agentID, "X-Task-ID", taskID)
	testutil.Call(t, testHandler.BatchUpdateIssues, req).Want(http.StatusOK)
	for i, parentID := range parents {
		if got := countSystemCommentsOn(t, parentID); got != 1 {
			t.Fatalf("parent %d barrier comments: want 1, got %d", i, got)
		}
		if got := countQueuedOrDispatched(t, agentID, parentID); got != i {
			t.Fatalf("parent %d pending turns: want %d, got %d", i, i, got)
		}
	}
}
