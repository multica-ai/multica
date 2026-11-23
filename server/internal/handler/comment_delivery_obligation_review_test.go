package handler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func reviewObligationFixture(t *testing.T) (string, string, string, string) {
	t.Helper()
	if testHandler == nil {
		t.Fatal("review regressions require a database")
	}
	runtime := dbfx.Runtime(t, "review obligation runtime")
	agent := dbfx.Agent(t, "review obligation agent", runtime)
	issue := dbfx.Issue(t, "review obligation", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})
	comment := dbfx.Comment(t, issue, "Accepted instruction")
	dbfx.Cleanup(t, `DELETE FROM comment_delivery_obligation WHERE issue_id=$1`, issue)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, issue)
	if _, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), commentDeliveryAdmissionForTest(t, db.UpsertCommentDeliveryObligationParams{
		WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(comment), AgentID: parseUUID(agent),
	})); err != nil {
		t.Fatal(err)
	}
	return runtime, agent, issue, comment
}

func TestCommentDeliveryObligationInvalidRouteDoesNotRevive(t *testing.T) {
	_, agent, issue, comment := reviewObligationFixture(t)
	dbfx.Exec(t, `UPDATE issue SET assignee_id=NULL, assignee_type=NULL WHERE id=$1`, issue)
	if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
		t.Fatal(err)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM comment_delivery_obligation WHERE comment_id=$1`, comment); n != 0 {
		t.Fatal("invalid route retained obsolete input")
	}
	dbfx.Exec(t, `UPDATE issue SET assignee_id=$2, assignee_type='agent' WHERE id=$1`, issue, agent)
	if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
		t.Fatal(err)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, issue); n != 0 {
		t.Fatal("restoring assignment replayed obsolete input")
	}
}

func TestCommentDeliveryObligationInvalidAuthorityDoesNotRevive(t *testing.T) {
	for _, reason := range []string{"member left", "actor archived", "lineage changed"} {
		t.Run(reason, func(t *testing.T) {
			runtime, agent, issue, comment := reviewObligationFixture(t)
			actor := dbfx.Agent(t, "review authoring agent", runtime)
			parent := dbfx.Task(t, actor, testutil.Cols{"runtime_id": runtime, "issue_id": issue,
				"status": "completed", "completed_at": testutil.Raw("now()"), "originator_user_id": testUserID, "accountable_user_id": testUserID})
			var departed string
			content := "[@Target](mention://agent/" + agent + ") Accepted instruction"
			dbfx.Exec(t, `UPDATE comment SET content=$2 WHERE id=$1`, comment, content)
			dbfx.Exec(t, `UPDATE comment_delivery_obligation SET comment_content=$2, actor_type='agent', actor_id=$3, originator_user_id=$4, authoring_task_id=$5 WHERE comment_id=$1`, comment, content, actor, testUserID, parent)
			switch reason {
			case "member left":
				departed = dbfx.User(t, "Departed review actor", "departed-"+agent+"@example.test")
				dbfx.Exec(t, `UPDATE comment_delivery_obligation SET actor_type='member', actor_id=$2, originator_user_id=$2, authoring_task_id=NULL WHERE comment_id=$1`, comment, departed)
			case "actor archived":
				dbfx.Exec(t, `UPDATE agent SET archived_at=now() WHERE id=$1`, actor)
			case "lineage changed":
				dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=NULL WHERE id=$1`, parent)
			}
			if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
				t.Fatal(err)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM comment_delivery_obligation WHERE comment_id=$1`, comment); n != 0 {
				t.Fatal("invalid authority retained input")
			}
			dbfx.Exec(t, `UPDATE agent SET archived_at=NULL WHERE id=$1`, actor)
			dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=$2 WHERE id=$1`, parent, testUserID)
			if departed != "" {
				dbfx.Member(t, testWorkspaceID, departed, "member")
			}
			if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
				t.Fatal(err)
			}
			if commentCoveredByAnyActiveTask(t, issue, agent, comment) {
				t.Fatal("restoring authority replayed obsolete input")
			}
		})
	}
}

type obligationRoutingFailureDB struct {
	dbExecutor
	query string
}

func (d obligationRoutingFailureDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "-- name: "+d.query+" ") {
		return commentDeliveryErrorRow{}
	}
	return d.dbExecutor.QueryRow(ctx, sql, args...)
}

func (d obligationRoutingFailureDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.HasPrefix(sql, "-- name: "+d.query+" ") {
		return nil, errors.New("fixture routing storage unavailable")
	}
	return d.dbExecutor.Query(ctx, sql, args...)
}

func TestCommentDeliveryObligationRoutingFailureRetainsInput(t *testing.T) {
	for _, query := range []string{"GetSquadInWorkspace", "HasPendingTaskForIssueAndAgentExcludingTriggerCommentInThread", "ListAgentInvocationTargets"} {
		t.Run(query, func(t *testing.T) {
			_, agent, issue, comment := reviewObligationFixture(t)
			squad := dbfx.Squad(t, "review routing squad", agent)
			dbfx.Exec(t, `UPDATE issue SET assignee_type='squad', assignee_id=$2 WHERE id=$1`, issue, squad)
			if query == "ListAgentInvocationTargets" {
				owner := dbfx.User(t, "Other review owner", "review-owner-"+agent+"@example.test")
				dbfx.Exec(t, `UPDATE agent SET owner_id=$2, permission_mode='public_to' WHERE id=$1`, agent, owner)
			}
			storage := obligationRoutingFailureDB{dbExecutor: testPool, query: query}
			h := &Handler{Queries: db.New(storage), DB: storage, TaskService: testHandler.TaskService}
			if err := h.DrainCommentDeliveryObligations(t.Context(), 50); err == nil {
				t.Error("routing storage error was hidden")
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM comment_delivery_obligation WHERE comment_id=$1`, comment); n != 1 {
				t.Fatal("routing storage error lost input")
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, issue); n != 0 {
				t.Fatal("routing storage error dispatched input")
			}
		})
	}
}

func TestCommentDeliveryObligationBoundsSystemReplay(t *testing.T) {
	for _, status := range []string{"cancelled", "queued_expired", "inherited change marker"} {
		t.Run(status, func(t *testing.T) {
			runtime, agent, issue, comment := reviewObligationFixture(t)
			cols := testutil.Cols{"runtime_id": runtime, "issue_id": issue, "trigger_comment_id": comment,
				"status": "cancelled", "completed_at": testutil.Raw("now()"), "cancelled_by_type": "system"}
			if status == "queued_expired" {
				cols["status"], cols["failure_reason"] = "failed", "queued_expired"
			}
			if status == "inherited change marker" {
				// A stale marker copied from a different task must not exempt a
				// later runtime cancellation from the replay limit.
				cols["context"] = map[string]any{"comment_change_cancelled_task_id": comment}
			}
			dbfx.Task(t, agent, cols)
			if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
				t.Fatal(err)
			}
			if !commentCoveredByAnyActiveTask(t, issue, agent, comment) {
				t.Fatal("first automatic replay was lost")
			}
			dbfx.Exec(t, `UPDATE agent_task_queue SET status='cancelled', cancelled_by_type='system', completed_at=now() WHERE issue_id=$1 AND status='queued'`, issue)
			dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at='-infinity' WHERE comment_id=$1`, comment)
			for range 3 {
				if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
					t.Fatal(err)
				}
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, issue); n != 2 {
				t.Fatalf("system failures produced %d tasks, want two attempts", n)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM comment_delivery_obligation WHERE comment_id=$1`, comment); n != 0 {
				t.Fatal("exhausted automatic replay remains pending")
			}
			// Earlier failed attempts cannot consume a new edit's replay budget.
			dbfx.Exec(t, `UPDATE comment SET revision=revision+1, content='New instruction', updated_at=now() WHERE id=$1`, comment)
			if _, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), commentDeliveryAdmissionForTest(t, db.UpsertCommentDeliveryObligationParams{
				WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(comment), AgentID: parseUUID(agent),
			})); err != nil {
				t.Fatal(err)
			}
			if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
				t.Fatal(err)
			}
			if !commentCoveredByAnyActiveTask(t, issue, agent, comment) {
				t.Fatal("prior system failures suppressed a new edit")
			}
		})
	}
}

func TestCommentDeliveryObligationLegacyReceipt(t *testing.T) {
	for _, tc := range []struct {
		name, dispatched string
		revision         int64
		settled          bool
	}{
		{"legacy current input", "now()", 0, true},
		{"legacy before edit", "now()-interval '1 hour'", 0, false},
		{"legacy without dispatch", "NULL", 0, false},
		{"explicit old revision", "now()", 1, false},
		{"explicit current revision", "now()-interval '1 hour'", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, agent, issue, comment := reviewObligationFixture(t)
			dbfx.Exec(t, `UPDATE comment SET revision=2, content='Updated instruction', updated_at=now() WHERE id=$1`, comment)
			if _, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), commentDeliveryAdmissionForTest(t, db.UpsertCommentDeliveryObligationParams{
				WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(comment), AgentID: parseUUID(agent),
			})); err != nil {
				t.Fatal(err)
			}
			cols := testutil.Cols{"runtime_id": runtime, "issue_id": issue, "status": "completed", "trigger_comment_id": comment,
				"dispatched_at": testutil.Raw(tc.dispatched), "completed_at": testutil.Raw("now()"), "delivered_comment_ids": []string{comment}}
			if tc.revision > 0 {
				cols["context"] = map[string]any{"delivered_comment_revisions": map[string]int64{comment: tc.revision}}
			}
			dbfx.Task(t, agent, cols)
			if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
				t.Fatal(err)
			}
			remaining := dbfx.Count(t, `SELECT count(*) FROM comment_delivery_obligation WHERE comment_id=$1`, comment)
			if (remaining == 0) != tc.settled {
				t.Fatalf("remaining=%d, want settled=%v", remaining, tc.settled)
			}
			if commentCoveredByAnyActiveTask(t, issue, agent, comment) == tc.settled {
				t.Fatalf("replay disagrees with receipt coverage: settled=%v", tc.settled)
			}
		})
	}
}
