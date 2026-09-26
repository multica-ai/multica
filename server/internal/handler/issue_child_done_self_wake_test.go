package handler

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// A running coordinator that closes its own stage has already observed that
// completion. A running coordinator alone is not evidence that it has observed
// a human or worker's independent completion, including another run by itself.
func TestChildDoneSourceTaskWake(t *testing.T) {
	for _, route := range []string{"agent", "squad"} {
		for _, batch := range []bool{false, true} {
			for _, source := range []string{"parent", "child", "worker", "human", "missing", "completed"} {
				t.Run(fmt.Sprintf("%s/batch=%t/%s", route, batch, source), func(t *testing.T) {
					runtimeID := dbfx.Runtime(t, "Stage coordinator runtime")
					leaderID := dbfx.Agent(t, "Stage coordinator", runtimeID)
					assigneeID := leaderID
					if route == "squad" {
						assigneeID = dbfx.Squad(t, "Stage squad", leaderID)
					}
					parentID := dbfx.Issue(t, "Coordinate stages", testutil.Cols{
						"status": "in_progress", "assignee_type": route, "assignee_id": assigneeID,
					})
					childID := dbfx.Issue(t, "Review completed work", testutil.Cols{
						"parent_issue_id": parentID, "stage": 1, "status": "in_review",
						"assignee_type": "agent", "assignee_id": leaderID,
					})
					childIDs := []string{childID}
					if batch {
						childIDs = append(childIDs, dbfx.Issue(t, "Other completed work", testutil.Cols{
							"parent_issue_id": parentID, "stage": 1, "status": "in_review",
						}))
					}
					nextID := dbfx.Issue(t, "Next stage", testutil.Cols{
						"parent_issue_id": parentID, "stage": 2, "status": "backlog",
					})
					parentTaskCols := testutil.Cols{
						"issue_id": parentID, "runtime_id": runtimeID, "status": "running",
					}
					if route == "squad" {
						parentTaskCols["is_leader_task"] = true
						parentTaskCols["squad_id"] = assigneeID
					}
					parentTaskID := dbfx.Task(t, leaderID, parentTaskCols)
					actorID, sourceID := leaderID, parentTaskID
					switch source {
					case "child":
						sourceID = dbfx.Task(t, leaderID, testutil.Cols{
							"issue_id": childID, "runtime_id": runtimeID, "status": "running",
						})
					case "worker":
						actorID = dbfx.Agent(t, "Independent worker", runtimeID)
						sourceID = dbfx.Task(t, actorID, testutil.Cols{
							"issue_id": childID, "runtime_id": runtimeID, "status": "running",
						})
					case "human":
						actorID, sourceID = "", ""
					case "missing":
						sourceID = ""
					case "completed":
						dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, parentTaskID)
					}
					dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, parentID)
					dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id = $1`, parentID)
					var reqBody map[string]any
					var path string
					if batch {
						path = "/api/issues/batch"
						reqBody = map[string]any{"issue_ids": childIDs, "updates": map[string]any{"status": "done"}}
					} else {
						path = "/api/issues/" + childID
						reqBody = map[string]any{"status": "done"}
					}
					req := withURLParam(newRequest(http.MethodPut, path, reqBody), "id", childID)
					req.Header.Set("X-Agent-ID", actorID)
					req.Header.Set("X-Task-ID", sourceID)
					if batch {
						testutil.Call(t, testHandler.BatchUpdateIssues, req).Want(http.StatusOK)
					} else {
						testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusOK)
					}
					want := 1
					if source == "parent" {
						want = 0
					}
					if got := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued'`, parentID); got != want {
						t.Fatalf("queued successor tasks = %d, want %d", got, want)
					}
					parentSystemCommentContent(t, parentID)
					if source == "parent" {
						// A later independent completion must still wake this leader,
						// even while the original parent task remains running.
						updateChildStatus(t, nextID, "todo")
						updateChildStatus(t, nextID, "done")
						if got := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued'`, parentID); got != 1 {
							t.Fatalf("next stage queued tasks = %d, want 1", got)
						}
					}
				})
			}
		}
	}
}

// Both status writes can commit before either barrier notification reads the
// sibling snapshot. Suppressing the coordinator's own event must not consume
// the independent event racing with it.
func TestChildDoneConcurrentSourceAndIndependentCompletion(t *testing.T) {
	for _, route := range []string{"agent", "squad"} {
		t.Run(route, func(t *testing.T) {
			ctx := context.Background()
			runtimeID := dbfx.Runtime(t, "Concurrent coordinator runtime")
			leaderID := dbfx.Agent(t, "Concurrent coordinator", runtimeID)
			assigneeID := leaderID
			if route == "squad" {
				assigneeID = dbfx.Squad(t, "Concurrent squad", leaderID)
			}
			parentID := dbfx.Issue(t, "Concurrent stage completion", testutil.Cols{
				"status": "in_progress", "assignee_type": route, "assignee_id": assigneeID,
			})
			taskID := dbfx.Task(t, leaderID, testutil.Cols{
				"issue_id": parentID, "runtime_id": runtimeID, "status": "running",
			})
			ownID := dbfx.Issue(t, "Coordinator closes review", testutil.Cols{
				"parent_issue_id": parentID, "stage": 1, "status": "done",
			})
			independentID := dbfx.Issue(t, "Independent completion", testutil.Cols{
				"parent_issue_id": parentID, "stage": 1, "status": "done",
			})
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, parentID)
			dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id = $1`, parentID)
			sourceReq := newRequest(http.MethodPut, "/api/issues/"+ownID, nil)
			sourceReq.Header.Set("X-Agent-ID", leaderID)
			sourceReq.Header.Set("X-Task-ID", taskID)
			sourceCtx := testHandler.withWakeupActor(sourceReq).Context()
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i, id := range []string{ownID, independentID} {
				child, err := testHandler.Queries.GetIssue(ctx, parseUUID(id))
				if err != nil {
					t.Fatal(err)
				}
				prev := child
				prev.Status = "in_review"
				eventCtx := ctx
				if i == 0 {
					eventCtx = sourceCtx
				}
				wg.Go(func() {
					<-start
					testHandler.notifyParentOfChildDone(eventCtx, prev, child)
				})
			}
			close(start)
			wg.Wait()
			if got := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued'`, parentID); got != 1 {
				t.Fatalf("independent completion queued tasks = %d, want 1", got)
			}
		})
	}
}
