package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCommentDeliveryObligationDrainsAfterQueuedBlocker(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "obligation runtime")
	agentID := dbfx.Agent(t, "obligation replay target", runtimeID, testutil.Cols{
		"visibility": "workspace",
	})
	issueID := dbfx.Issue(t, "queued cross-head obligation", testutil.Cols{
		"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID,
	})
	seedDupRacePR(t, issueID, 9995985)
	aTrigger := dbfx.Comment(t, issueID, "First instruction", testutil.Cols{
		"created_at": testutil.Raw("now()-interval '11 minutes'"),
	})
	commentID := dbfx.Comment(t, issueID, "Follow up at the current head", testutil.Cols{
		"parent_id": aTrigger, "created_at": testutil.Raw("now()-interval '5 minutes'"),
	})
	bTrigger := dbfx.Comment(t, issueID, "Queued old-head instruction", testutil.Cols{
		"parent_id": aTrigger, "created_at": testutil.Raw("now()-interval '2 minutes'"),
	})
	taskA := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "trigger_comment_id": aTrigger,
		"delivered_comment_ids": []string{aTrigger}, "status": "running",
		"created_at": testutil.Raw("now()-interval '10 minutes'"),
		"started_at": testutil.Raw("now()-interval '9 minutes'"),
		"context":    map[string]any{"head_sha": dupRaceHeadA},
	})
	taskB := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "trigger_comment_id": bTrigger,
		"status": "queued", "created_at": testutil.Raw("now()-interval '1 minute'"),
		"context": map[string]any{"head_sha": dupRaceHeadA},
	})
	t.Cleanup(func() {
		_, err := testPool.Exec(context.Background(), `DELETE FROM comment_delivery_obligation WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
		if err != nil {
			t.Errorf("clean up replay obligations: %v", err)
		}
	})
	if w := completeTaskViaHandler(t, taskA, "done"); w.Code != 200 {
		t.Fatalf("complete A: %d: %s", w.Code, w.Body.String())
	}
	var folded bool
	var blockerHead string
	dbfx.QueryRow(t, `SELECT $2::uuid=trigger_comment_id OR $2::uuid=ANY(coalesced_comment_ids), context->>'head_sha' FROM agent_task_queue WHERE id=$1`, taskB, commentID).Scan(&folded, &blockerHead)
	if folded || blockerHead != dupRaceHeadA {
		t.Fatalf("old-head blocker changed: folded=%v head=%q", folded, blockerHead)
	}
	row, err := testHandler.Queries.GetCommentDeliveryObligation(t.Context(), db.GetCommentDeliveryObligationParams{
		WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(commentID), AgentID: parseUUID(agentID),
	})
	if err != nil {
		t.Fatalf("blocked replay has no durable obligation: %v", err)
	}
	if row.IssueID != parseUUID(issueID) {
		t.Fatalf("obligation points at issue %v, want %s", row.IssueID, issueID)
	}
	for blocker := 0; blocker < 3; blocker++ {
		dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at='-infinity' WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
		if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
			t.Fatal(err)
		}
		if commentCovered(t, issueID, agentID, commentID, "queued") {
			t.Fatal("input was folded into an old-head blocker")
		}
		dbfx.Exec(t, `UPDATE agent_task_queue SET status='running', started_at=now(), delivered_comment_ids=ARRAY[trigger_comment_id] WHERE id=$1`, taskB)
		if w := completeTaskViaHandler(t, taskB, "done"); w.Code != 200 {
			t.Fatalf("complete blocker %d: %d: %s", blocker, w.Code, w.Body.String())
		}
		if blocker < 2 {
			taskB = dbfx.Task(t, agentID, testutil.Cols{
				"runtime_id": runtimeID, "issue_id": issueID, "trigger_comment_id": bTrigger,
				"context": map[string]any{"head_sha": dupRaceHeadA},
			})
		}
	}
	dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at='-infinity' WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
	// A fresh handler has no process-local knowledge of the accepted input.
	fresh := New(db.New(testPool), testPool, testHandler.Hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, Config{AllowSignup: true})
	if err := fresh.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
		t.Fatal(err)
	}
	if !commentCoveredByAnyActiveTask(t, issueID, agentID, commentID) {
		t.Fatal("durable input was not replayed after the old-head blocker drained")
	}
	if head := coveringTaskHead(t, issueID, agentID, commentID); head != dupRaceHeadB {
		t.Fatalf("follow-up head=%q, want current head %q", head, dupRaceHeadB)
	}
	dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at=now() WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
	if err := fresh.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
		t.Fatal(err)
	}
	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("repeated drain created %d pending tasks, want one", n)
	}

}

func TestCommentDeliveryObligationAcceptsAndReplaysEveryRoutedSource(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, source := range []commentAgentTriggerSource{commentTriggerSourceIssueAssignee, commentTriggerSourceMentionAgent, commentTriggerSourceMentionSquadLeader, commentTriggerSourceThreadParent, commentTriggerSourceConversation} {
		t.Run(string(source), func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "routed input runtime")
			agentID := dbfx.Agent(t, "routed input recipient", runtimeID)
			issueID := dbfx.Issue(t, "real routed deferred input", testutil.Cols{"status": "in_progress"})
			seedDupRacePR(t, issueID, 9995992)
			content := "Continue at the current reviewed commit"
			var squadID, parentID, commentID string
			if source == commentTriggerSourceIssueAssignee {
				dbfx.Exec(t, `UPDATE issue SET assignee_type='agent',assignee_id=$2 WHERE id=$1`, issueID, agentID)
				commentID = dbfx.Comment(t, issueID, "Previous root instruction")
			} else {
				cols := testutil.Cols{}
				rootContent := "Earlier discussion"
				if source == commentTriggerSourceThreadParent {
					cols["author_type"] = "agent"
					cols["author_id"] = agentID
				}
				if source == commentTriggerSourceConversation {
					rootContent = "[@Target](mention://agent/" + agentID + ") Start this discussion"
				}
				parentID = dbfx.Comment(t, issueID, rootContent, cols)
				commentID = dbfx.Comment(t, issueID, "Previous reply", testutil.Cols{"parent_id": parentID})
			}
			if source == commentTriggerSourceMentionAgent {
				content = "[@Target](mention://agent/" + agentID + ") " + content
			}
			if source == commentTriggerSourceMentionSquadLeader {
				squadID = dbfx.Squad(t, "Routed input squad", agentID)
				content = "[@Squad](mention://squad/" + squadID + ") " + content
			}
			root := parentID
			if root == "" {
				root = commentID
			}
			otherInput := dbfx.Comment(t, issueID, "Another queued instruction", testutil.Cols{"parent_id": root})
			blocker := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "trigger_comment_id": otherInput, "status": "queued", "context": map[string]any{"head_sha": dupRaceHeadA}})
			dbfx.Cleanup(t, `DELETE FROM comment_delivery_obligation WHERE issue_id=$1`, issueID)
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, issueID)
			previewBody := map[string]any{"content": content, "editing_comment_id": commentID}
			if parentID != "" {
				previewBody["parent_id"] = parentID
			}
			var preview CommentTriggerPreviewResponse
			testutil.Call(t, testHandler.PreviewCommentTriggers, withURLParam(newRequest("POST", "/api/issues/"+issueID+"/comments/preview", previewBody), "id", issueID)).Want(200).JSON(&preview)
			if len(preview.Agents) != 1 || preview.Agents[0].ID != agentID || preview.Agents[0].Source != string(source) {
				t.Fatalf("real routing did not select %s: %+v", source, preview.Agents)
			}
			var accepted CommentResponse
			testutil.Call(t, testHandler.UpdateComment, withURLParam(newRequest("PATCH", "/api/comments/"+commentID, map[string]any{"content": content, "expected_revision": 1}), "commentId", commentID)).Want(200).JSON(&accepted)
			if len(accepted.TriggerOutcomes) > 0 && (len(accepted.TriggerOutcomes) != 1 || accepted.TriggerOutcomes[0].Status != DispatchDeferred) {
				t.Fatalf("routing did not defer: %+v", accepted.TriggerOutcomes)
			}
			if _, err := testHandler.Queries.GetCommentDeliveryObligation(t.Context(), db.GetCommentDeliveryObligationParams{WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(commentID), AgentID: parseUUID(agentID)}); err != nil {
				t.Fatalf("accepted %s has no durable input: %v", source, err)
			}
			if commentCovered(t, issueID, agentID, commentID, "queued") {
				t.Fatal("old-head blocker consumed new input")
			}
			dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed',completed_at=now(),delivered_comment_ids=ARRAY[trigger_comment_id] WHERE id=$1`, blocker)
			dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at='-infinity' WHERE comment_id=$1 AND agent_id=$2`, commentID, agentID)
			if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
				t.Fatal(err)
			}
			if !commentCoveredByAnyActiveTask(t, issueID, agentID, commentID) {
				t.Fatalf("real %s input was not recovered", source)
			}
			if head := coveringTaskHead(t, issueID, agentID, commentID); head != dupRaceHeadB {
				t.Fatalf("follow-up uses head %q instead of current commit", head)
			}
			var leader bool
			var storedSquad *string
			dbfx.QueryRow(t, `SELECT is_leader_task,squad_id::text FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2 AND status='queued'`, issueID, agentID).Scan(&leader, &storedSquad)
			if leader != (source == commentTriggerSourceMentionSquadLeader) {
				t.Fatalf("routed leader role=%v for source %s", leader, source)
			}
			if squadID != "" && (storedSquad == nil || *storedSquad != squadID) {
				t.Fatal("squad leader execution lost its squad")
			}
		})
	}
}

func TestCommentDeliveryObligationPreservesSquadLeaderContinuation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, source := range []string{"thread parent", "conversation"} {
		t.Run(source, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "leader continuation runtime")
			leaderID := dbfx.Agent(t, "continuing leader", runtimeID)
			squadID := dbfx.Squad(t, "continuing squad", leaderID)
			issueID := dbfx.Issue(t, "leader continuation", testutil.Cols{"status": "in_progress"})
			root := dbfx.Comment(t, issueID, "Original squad instruction")
			sourceTask := dbfx.Task(t, leaderID, testutil.Cols{
				"runtime_id": runtimeID, "issue_id": issueID, "trigger_comment_id": root,
				"squad_id": squadID, "is_leader_task": true, "status": "completed",
				"delivered_comment_ids": []string{root},
			})
			reply := dbfx.Comment(t, issueID, "Leader reply", testutil.Cols{
				"author_type": "agent", "author_id": leaderID, "source_task_id": sourceTask, "parent_id": root,
			})
			parent := reply
			if source == "conversation" {
				parent = root
			}
			commentID := dbfx.Comment(t, issueID, "Continue this discussion", testutil.Cols{"parent_id": parent})
			if _, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), commentDeliveryAdmissionForTest(t, db.UpsertCommentDeliveryObligationParams{
				WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(commentID), AgentID: parseUUID(leaderID),
			})); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM comment_delivery_obligation WHERE issue_id=$1 AND agent_id=$2`, issueID, leaderID)
				testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, issueID, leaderID)
			})
			dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at='-infinity' WHERE comment_id=$1 AND agent_id=$2`, commentID, leaderID)
			if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
				t.Fatal(err)
			}
			var queuedSquad string
			var isLeader bool
			dbfx.QueryRow(t, `SELECT squad_id::text, COALESCE(is_leader_task,false) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2 AND trigger_comment_id=$3 AND status='queued'`, issueID, leaderID, commentID).Scan(&queuedSquad, &isLeader)
			if queuedSquad != squadID || !isLeader {
				t.Fatalf("continuation lost leader role: squad=%s leader=%v", queuedSquad, isLeader)
			}
		})
	}
}
