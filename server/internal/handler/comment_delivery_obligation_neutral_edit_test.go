package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCommentDeliveryObligationSurvivesRoutingNeutralMutation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, action := range []string{"attachment", "resolution"} {
		for _, delivered := range []bool{false, true} {
			name := action + "/pending"
			if delivered {
				name = action + "/delivered"
			}
			t.Run(name, func(t *testing.T) {
				runtimeID := dbfx.Runtime(t, "neutral mutation runtime")
				agentID := dbfx.Agent(t, "neutral mutation recipient", runtimeID)
				issueID := dbfx.Issue(t, "neutral mutation issue", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID})
				seedDupRacePR(t, issueID, 9995987)
				root := dbfx.Comment(t, issueID, "Old-head input")
				blocker := dbfx.Task(t, agentID, testutil.Cols{
					"runtime_id": runtimeID, "issue_id": issueID, "trigger_comment_id": root,
					"status": "queued", "context": map[string]any{"head_sha": dupRaceHeadA},
				})
				var accepted CommentResponse
				content := "[@Target](mention://agent/" + agentID + ") Review this head"
				testutil.Call(t, testHandler.CreateComment, withURLParam(newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
					"content": content, "parent_id": root,
				}), "id", issueID)).Want(201).JSON(&accepted)
				if len(accepted.TriggerOutcomes) != 1 || accepted.TriggerOutcomes[0].Status != DispatchDeferred {
					t.Fatalf("input was not accepted for deferred delivery: %+v", accepted.TriggerOutcomes)
				}
				dbfx.Cleanup(t, `DELETE FROM comment_delivery_obligation WHERE issue_id=$1`, issueID)
				dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, issueID)
				if delivered {
					dbfx.Task(t, agentID, testutil.Cols{
						"runtime_id": runtimeID, "issue_id": issueID, "trigger_comment_id": accepted.ID,
						"status": "completed", "started_at": testutil.Raw("now()"), "completed_at": testutil.Raw("now()"),
						"delivered_comment_ids": []string{accepted.ID}, "context": map[string]any{"head_sha": dupRaceHeadB, "delivered_comment_revisions": map[string]int64{accepted.ID: accepted.Revision}},
					})
				}
				if action == "attachment" {
					attachment := unlinkedIssueAttachment(t, issueID)
					testutil.Call(t, testHandler.UpdateComment, withURLParam(newRequest("PATCH", "/api/comments/"+accepted.ID, map[string]any{
						"content": content, "attachment_ids": []string{attachment}, "expected_revision": accepted.Revision,
					}), "commentId", accepted.ID)).Want(200)
				} else {
					testutil.Call(t, testHandler.ResolveComment, withURLParam(newRequest("POST", "/api/comments/"+accepted.ID+"/resolve", nil), "commentId", accepted.ID)).Want(200)
				}
				var neutralRevision int64
				dbfx.QueryRow(t, `SELECT revision FROM comment WHERE id=$1`, accepted.ID).Scan(&neutralRevision)
				if neutralRevision <= accepted.Revision {
					t.Fatal("fixture did not advance the general comment revision")
				}
				dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed', completed_at=now(), delivered_comment_ids=ARRAY[trigger_comment_id] WHERE id=$1`, blocker)
				dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at='-infinity' WHERE comment_id=$1 AND agent_id=$2`, accepted.ID, agentID)
				if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
					t.Fatal(err)
				}
				active := commentCoveredByAnyActiveTask(t, issueID, agentID, accepted.ID)
				if active == delivered {
					t.Fatalf("after neutral mutation active=%v, delivered=%v", active, delivered)
				}
				if !delivered {
					if head := coveringTaskHead(t, issueID, agentID, accepted.ID); head != dupRaceHeadB {
						t.Fatalf("follow-up head=%q, want current head", head)
					}
					if _, err := testHandler.Queries.GetCommentDeliveryObligation(t.Context(), db.GetCommentDeliveryObligationParams{WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(accepted.ID), AgentID: parseUUID(agentID)}); err != nil {
						t.Fatalf("neutral mutation lost accepted input: %v", err)
					}
				}
			})
		}
	}
}
