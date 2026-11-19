package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestCommentDeliveryObligationPreservesSurvivorsAcrossBatchChanges(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, action := range []string{"three edits", "delete after edit"} {
		t.Run(action, func(t *testing.T) {
			fixture := createCommentDeliveryFixture(t, "Repeated batch changes")
			makeCommentDeliverySingleThread(t, fixture)
			dbfx.Cleanup(t, `DELETE FROM comment_delivery_obligation WHERE issue_id=$1`, fixture.issueID)
			dbfx.Exec(t, `UPDATE issue SET assignee_type='agent', assignee_id=$2 WHERE id=$1`, fixture.issueID, fixture.agentID)
			for edit := 1; edit <= 3; edit++ {
				testutil.Call(t, testHandler.UpdateComment, withURLParam(newRequest(http.MethodPatch, "/api/comments/"+fixture.commentID[0], map[string]any{
					"content": fmt.Sprintf("Edited instruction %d", edit),
				}), "commentId", fixture.commentID[0])).Want(http.StatusOK)
				if action == "delete after edit" {
					testutil.Call(t, testHandler.DeleteComment, withURLParam(newRequest(http.MethodDelete, "/api/comments/"+fixture.commentID[0], nil), "commentId", fixture.commentID[0])).Want(http.StatusNoContent)
					break
				}
			}
			for _, survivor := range fixture.commentID[1:] {
				if !commentCoveredByAnyActiveTask(t, fixture.issueID, fixture.agentID, survivor) {
					t.Errorf("%s lost surviving comment %s", action, survivor)
				}
			}
			claimed := claimCommentDeliveryFixture(t, fixture, protocol.DaemonCapabilityCoalescedCommentsV1)
			for _, survivor := range fixture.commentID[1:] {
				if !slices.Contains(claimed.DeliveredCommentIDs, survivor) {
					t.Errorf("surviving comment %s was absent from the claim receipt", survivor)
				}
			}
			if action == "delete after edit" && slices.Contains(claimed.DeliveredCommentIDs, fixture.commentID[0]) {
				t.Fatal("deleted input was delivered with the survivors")
			}
		})
	}
}

type commentDeliveryPolicyFailureDB struct {
	dbExecutor
	failure error
	reads   int
}

func (d *commentDeliveryPolicyFailureDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "-- name: GetWorkspaceAttributionFailClosed ") {
		d.reads++
		if d.failure != nil {
			return errorRow{err: d.failure}
		}
	}
	return d.dbExecutor.QueryRow(ctx, sql, args...)
}

func TestCommentDeliveryObligationRetriesAttributionPolicyReadFailure(t *testing.T) {
	for _, path := range []string{"enqueue", "merge"} {
		t.Run(path, func(t *testing.T) {
			runtime, agent, issue, comment := reviewObligationFixture(t)
			content := "[@Target](mention://agent/" + agent + ") Accepted system instruction"
			dbfx.Exec(t, `UPDATE agent SET permission_mode='public_to' WHERE id=$1`, agent)
			dbfx.Insert(t, "agent_invocation_target", testutil.Cols{"agent_id": agent, "target_type": "workspace", "target_id": testWorkspaceID})
			dbfx.Exec(t, `UPDATE comment SET author_type='system', content=$2 WHERE id=$1`, comment, content)
			dbfx.Exec(t, `UPDATE comment_delivery_obligation SET actor_type='system', actor_id=NULL, originator_user_id=NULL, comment_content=$2 WHERE comment_id=$1`, comment, content)
			var original, blocker string
			if path == "merge" {
				original = dbfx.Comment(t, issue, "Previously accepted instruction")
				dbfx.Exec(t, `UPDATE comment SET parent_id=$2 WHERE id=$1`, comment, original)
				blocker = dbfx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "issue_id": issue, "trigger_comment_id": original,
					"originator_user_id": testUserID, "accountable_user_id": testUserID, "originator_source": "comment_source"})
			}
			storageErr := errors.New("fixture attribution policy unavailable")
			storage := &commentDeliveryPolicyFailureDB{dbExecutor: testPool, failure: storageErr}
			h := New(db.New(storage), testPool, testHandler.Hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, Config{})
			if err := h.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
				t.Fatal(err)
			}
			if storage.reads == 0 {
				t.Fatal("fixture did not exercise the attribution policy read")
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM comment_delivery_obligation WHERE comment_id=$1`, comment); n != 1 {
				t.Error("policy read failure deleted accepted input")
			}
			if commentCoveredByAnyActiveTask(t, issue, agent, comment) {
				t.Fatal("policy read failure did not fail closed")
			}
			if blocker != "" {
				before, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(blocker))
				if err != nil {
					t.Fatal(err)
				}
				if before.TriggerCommentID != parseUUID(original) || before.OriginatorUserID != parseUUID(testUserID) || before.AccountableUserID != parseUUID(testUserID) || before.OriginatorSource.String != "comment_source" {
					t.Fatal("policy read failure changed the existing task's attribution")
				}
			}
			storage.failure = nil
			dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at='-infinity' WHERE comment_id=$1`, comment)
			if err := h.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
				t.Fatal(err)
			}
			if !commentCoveredByAnyActiveTask(t, issue, agent, comment) {
				t.Fatal("policy recovery failed to retry retained input")
			}
		})
	}
}
