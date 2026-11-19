package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCommentDeliveryObligationRespectsInputLifecycle(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, tc := range []struct {
		name   string
		settle bool
		run    bool
	}{
		{"deleted", true, false}, {"tombstone", true, false},
		{"edited", true, false}, {"suppressed", true, false},
		{"explicit cancellation", true, false}, {"automatic cancellation", false, true},
		{"delivered", true, false}, {"planned", false, false},
		{"dispatched receipt", false, false}, {"running receipt", false, false},
		{"cancel agent inputs", true, false}, {"cancel issue inputs", true, false}, {"committed comment deletion", true, false},
		{"permission revoked", true, false}, {"agent archived", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var otherOwner string
			if tc.name == "permission revoked" {
				otherOwner = dbfx.User(t, "Other obligation owner", "other-obligation-owner-"+testWorkspaceID+"@example.test")
			}
			runtimeID := dbfx.Runtime(t, "lifecycle obligation runtime")
			agentID := dbfx.Agent(t, "lifecycle obligation target", runtimeID)
			issueID := dbfx.Issue(t, "lifecycle obligation", testutil.Cols{
				"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID,
			})
			commentID := dbfx.Comment(t, issueID, "Accepted instruction")
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM comment_delivery_obligation WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
				testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
			})
			_, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), commentDeliveryAdmissionForTest(t, db.UpsertCommentDeliveryObligationParams{
				WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(commentID), AgentID: parseUUID(agentID),
			}))
			if err != nil {
				t.Fatal(err)
			}
			dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at='-infinity' WHERE comment_id=$1 AND agent_id=$2`, commentID, agentID)
			var existingTask string
			switch tc.name {
			case "deleted":
				dbfx.Exec(t, `DELETE FROM comment WHERE id=$1`, commentID)
			case "tombstone":
				dbfx.Exec(t, `UPDATE comment SET deleted_at=now(), content='' WHERE id=$1`, commentID)
			case "edited":
				dbfx.Exec(t, `UPDATE comment SET revision=revision+1, content='Withdrawn' WHERE id=$1`, commentID)
			case "suppressed":
				dbfx.Exec(t, `UPDATE comment SET suppressed_agent_ids=ARRAY[$2::uuid] WHERE id=$1`, commentID, agentID)
			case "permission revoked":
				dbfx.Exec(t, `UPDATE agent SET owner_id=$2, permission_mode='private' WHERE id=$1`, agentID, otherOwner)
			case "cancel agent inputs":
				if _, err := testHandler.TaskService.CancelTasksForAgent(t.Context(), parseUUID(agentID)); err != nil {
					t.Fatal(err)
				}
			case "cancel issue inputs":
				if err := testHandler.TaskService.CancelTasksForIssue(t.Context(), parseUUID(issueID)); err != nil {
					t.Fatal(err)
				}
			case "committed comment deletion":
				if _, err := testHandler.deleteComment(t.Context(), parseUUID(commentID), parseUUID(testWorkspaceID)); err != nil {
					t.Fatal(err)
				}
			case "agent archived":
				dbfx.Exec(t, `UPDATE agent SET archived_at=now() WHERE id=$1`, agentID)
			default:
				cols := testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "trigger_comment_id": commentID}
				switch tc.name {
				case "explicit cancellation", "automatic cancellation":
					cols["status"] = "cancelled"
					cols["completed_at"] = testutil.Raw("now()")
					if tc.name == "explicit cancellation" {
						cols["cancelled_by_type"] = "member"
						cols["cancelled_by_id"] = testUserID
						cols["context"] = map[string]any{"cancelled_comment_revisions": map[string]int64{commentID: 1}}
					}
				case "delivered":
					cols["status"] = "completed"
					cols["started_at"] = testutil.Raw("now()")
					cols["completed_at"] = testutil.Raw("now()")
					cols["delivered_comment_ids"] = []string{commentID}
					cols["context"] = map[string]any{"delivered_comment_revisions": map[string]int64{commentID: 1}}
				case "planned":
					cols["status"] = "running"
					cols["started_at"] = testutil.Raw("now()")
				case "dispatched receipt", "running receipt":
					cols["status"] = "dispatched"
					if tc.name == "running receipt" {
						cols["status"] = "running"
					}
					cols["dispatched_at"] = testutil.Raw("now()")
					cols["started_at"] = testutil.Raw("now()")
					cols["delivered_comment_ids"] = []string{commentID}
					cols["context"] = map[string]any{"delivered_comment_revisions": map[string]int64{commentID: 1}}
				}
				existingTask = dbfx.Task(t, agentID, cols)
			}
			if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
				t.Fatal(err)
			}
			var replacements int
			dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2 AND id::text<>$3 AND status IN ('queued','dispatched','running','waiting_local_directory')`, issueID, agentID, existingTask).Scan(&replacements)
			if (replacements > 0) != tc.run {
				t.Fatalf("replacement tasks=%d, want run=%v", replacements, tc.run)
			}
			_, err = testHandler.Queries.GetCommentDeliveryObligation(t.Context(), db.GetCommentDeliveryObligationParams{
				WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(commentID), AgentID: parseUUID(agentID),
			})
			if tc.settle && !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("withdrawn or acknowledged obligation remains: %v", err)
			}
			if !tc.settle && err != nil {
				t.Fatalf("undelivered obligation was lost: %v", err)
			}
			if tc.name == "dispatched receipt" || tc.name == "running receipt" {
				// Reclaim can replace a provisional receipt before an automatic
				// cancellation. That receipt must not permanently acknowledge input.
				dbfx.Exec(t, `UPDATE agent_task_queue SET delivered_comment_ids='{}', status='cancelled', completed_at=now() WHERE id=$1`, existingTask)
				dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at=now() WHERE comment_id=$1 AND agent_id=$2`, commentID, agentID)
				if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
					t.Fatal(err)
				}
				if !commentCoveredByAnyActiveTask(t, issueID, agentID, commentID) {
					t.Fatal("replaced receipt lost the accepted instruction")
				}
			}
		})
	}
}

func TestCommentDeliveryObligationBlockedInputDoesNotStarveReadyInput(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	other := dbfx.User(t, "Other fairness owner", "other-fairness-owner-"+testWorkspaceID+"@example.test")
	var comments, agents, issues []string
	for _, name := range []string{"blocked", "ready"} {
		runtimeID := dbfx.Runtime(t, name+" fairness runtime")
		agentID := dbfx.Agent(t, name+" fairness agent", runtimeID)
		issueID := dbfx.Issue(t, name+" fairness issue", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID})
		commentID := dbfx.Comment(t, issueID, "Accepted input")
		comments, agents, issues = append(comments, commentID), append(agents, agentID), append(issues, issueID)
		if _, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), commentDeliveryAdmissionForTest(t, db.UpsertCommentDeliveryObligationParams{
			WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(commentID), AgentID: parseUUID(agentID),
		})); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM comment_delivery_obligation WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
			testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
		})
	}
	dbfx.Exec(t, `UPDATE agent SET owner_id=$2 WHERE id=$1`, agents[0], other)
	dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at='-infinity' WHERE comment_id=$1 AND agent_id=$2`, comments[0], agents[0])
	dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at=now()-interval '1 year' WHERE comment_id=$1 AND agent_id=$2`, comments[1], agents[1])
	for range 2 {
		if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
	}
	if commentCoveredByAnyActiveTask(t, issues[0], agents[0], comments[0]) {
		t.Fatal("permission-blocked input started a run")
	}
	if !commentCoveredByAnyActiveTask(t, issues[1], agents[1], comments[1]) {
		t.Fatal("blocked input starved the ready input")
	}
}
