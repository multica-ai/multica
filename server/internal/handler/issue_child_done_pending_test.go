package handler

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Drive status update -> claim -> start -> complete -> next claim. A visible
// system comment alone is not evidence that the parent agent received it.
func TestChildDoneHandoffSurvivesUnrelatedPendingTask(t *testing.T) {
	for _, owner := range []string{"agent", "squad"} {
		for _, state := range []string{"queued", "dispatched"} {
			for _, source := range []string{"assignment", "other thread"} {
				t.Run(owner+"/"+state+"/"+source, func(t *testing.T) {
					ctx := context.Background()
					runtimeID := dbfx.Runtime(t, "Parent handoff runtime")
					agentID := dbfx.Agent(t, "Parent coordinator", runtimeID, testutil.Cols{"max_concurrent_tasks": 3})
					assigneeID := agentID
					if owner == "squad" {
						assigneeID = dbfx.Squad(t, "Parent squad", agentID)
					}
					parentID := dbfx.Issue(t, "Parent awaiting stage completion", testutil.Cols{
						"status": "in_progress", "assignee_type": owner, "assignee_id": assigneeID,
					})
					childID := dbfx.Issue(t, "Last stage 1 child", testutil.Cols{
						"status": "in_progress", "parent_issue_id": parentID, "stage": 1,
					})
					nextChildID := dbfx.Issue(t, "Parked stage 2 child", testutil.Cols{
						"status": "backlog", "parent_issue_id": parentID, "stage": 2,
					})
					cols := testutil.Cols{"runtime_id": runtimeID, "issue_id": parentID, "priority": 5}
					if source == "other thread" {
						cols["trigger_comment_id"] = dbfx.Comment(t, parentID, "An independent request")
					}
					pendingID := dbfx.Task(t, agentID, cols)
					claim := func() *AgentTaskResponse {
						req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "parent-handoff-test")
						req = withURLParam(req, "runtimeId", runtimeID)
						var response struct {
							Task *AgentTaskResponse `json:"task"`
						}
						testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&response)
						return response.Task
					}
					var initial *AgentTaskResponse
					if state == "dispatched" {
						initial = claim()
					}

					// The child status handler must record AND deliver the new stage
					// signal even though a different run already occupies the parent.
					req := withURLParam(newRequest(http.MethodPut, "/api/issues/"+childID, map[string]any{"status": "done"}), "id", childID)
					testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusOK)
					var signalID, signalContent string
					dbfx.QueryRow(t, `SELECT id, content FROM comment WHERE issue_id = $1 AND type = 'system'`, parentID).Scan(&signalID, &signalContent)
					if !strings.Contains(signalContent, "Stage 2 is next") {
						t.Fatalf("missing next-stage instruction: %s", signalContent)
					}
					parent, err := testHandler.Queries.GetIssue(ctx, parseUUID(parentID))
					if err != nil {
						t.Fatal(err)
					}
					// A repeated dispatch while the signal's own run is queued must
					// not duplicate it. Repeat again after claim below.
					testHandler.dispatchParentAssigneeTrigger(ctx, parent, db.Comment{ID: parseUUID(signalID)})
					if state == "queued" {
						initial = claim()
					}
					if initial == nil || initial.ID != pendingID {
						t.Fatal("expected to claim the pre-existing higher-priority run")
					}
					if slices.Contains(initial.DeliveredCommentIDs, signalID) {
						t.Fatal("unrelated run unexpectedly acknowledged the new system thread")
					}

					if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(pendingID)); err != nil {
						t.Fatal(err)
					}
					if parallel := claim(); parallel != nil {
						t.Fatal("parent handoff must remain serialized even with spare agent capacity")
					}
					complete := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+pendingID+"/complete", map[string]any{"output": "Prior request finished"}, testWorkspaceID, "parent-handoff-test")
					complete = withURLParam(complete, "taskId", pendingID)
					testutil.Call(t, testHandler.CompleteTask, complete).Want(http.StatusOK)

					followup := claim()
					if followup == nil {
						t.Fatal("stage completion comment persisted, but no parent run received it after the pending run finished")
					}
					if followup.ID == pendingID || followup.TriggerCommentContent != signalContent || !slices.Contains(followup.DeliveredCommentIDs, signalID) {
						t.Fatal("parent follow-up did not deliver the stage completion instruction")
					}
					if followup.IsLeaderTask != (owner == "squad") {
						t.Fatalf("leader role = %t for %s parent", followup.IsLeaderTask, owner)
					}
					stored, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(followup.ID))
					if err != nil {
						t.Fatal(err)
					}
					if owner == "squad" && stored.SquadID != parseUUID(assigneeID) {
						t.Fatal("handoff lost squad provenance")
					}
					var nextStatus string
					dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, nextChildID).Scan(&nextStatus)
					if nextStatus != "backlog" {
						t.Fatal("notification must not automatically promote the next stage")
					}

					// Re-dispatching the SAME signal must still deduplicate while its
					// run is pending, including after the daemon has claimed it.
					testHandler.dispatchParentAssigneeTrigger(ctx, parent, db.Comment{ID: parseUUID(signalID)})
					var signalTasks int
					dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND trigger_comment_id = $2`, parentID, signalID).Scan(&signalTasks)
					if signalTasks != 1 {
						t.Fatalf("same stage signal created %d runs", signalTasks)
					}
				})
			}
		}
	}
}

func TestBatchChildDoneQueuesLatestHandoffAlongsideEarlierStage(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "Batch parent handoff runtime")
	agentID := dbfx.Agent(t, "Batch parent coordinator", runtimeID)
	parentID := dbfx.Issue(t, "Parent with an earlier stage wake", testutil.Cols{
		"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID,
	})
	children := make([]string, 3)
	for i := range children {
		children[i] = dbfx.Issue(t, "Stage child", testutil.Cols{
			"status": "in_progress", "parent_issue_id": parentID, "stage": i + 1,
		})
	}

	req := withURLParam(newRequest(http.MethodPut, "/api/issues/"+children[0], map[string]any{"status": "done"}), "id", children[0])
	testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusOK)
	var earlierSignal string
	dbfx.QueryRow(t, `SELECT trigger_comment_id FROM agent_task_queue WHERE issue_id = $1`, parentID).Scan(&earlierSignal)

	// A batch closing later stages still emits one final-state notification.
	// Its instruction must not be swallowed by the earlier stage's queued run.
	req = newRequest(http.MethodPatch, "/api/issues/batch", map[string]any{
		"issue_ids": []string{children[2], children[1]}, "updates": map[string]any{"status": "done"},
	})
	var result struct {
		Updated int `json:"updated"`
	}
	testutil.Call(t, testHandler.BatchUpdateIssues, req).Want(http.StatusOK).JSON(&result)
	if result.Updated != 2 {
		t.Fatalf("batch updated %d children, want 2", result.Updated)
	}
	var latestSignal, latestContent string
	dbfx.QueryRow(t, `SELECT id, content FROM comment WHERE issue_id = $1 AND type = 'system' AND id <> $2`, parentID, earlierSignal).Scan(&latestSignal, &latestContent)
	if !strings.Contains(latestContent, "Stage 3 of this issue is complete") || !strings.Contains(latestContent, "together in a batch update") {
		t.Fatalf("batch handoff did not describe the final stage: %s", latestContent)
	}
	var queuedSignals []string
	dbfx.QueryRow(t, `SELECT array_agg(trigger_comment_id::text) FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued'`, parentID).Scan(&queuedSignals)
	slices.Sort(queuedSignals)
	want := []string{earlierSignal, latestSignal}
	slices.Sort(want)
	if !slices.Equal(queuedSignals, want) {
		t.Fatalf("queued signal ids = %v, want both stage handoffs %v", queuedSignals, want)
	}
}
