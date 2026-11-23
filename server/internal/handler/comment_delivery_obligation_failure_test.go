package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type commentDeliveryFailingDB struct {
	*pgxpool.Pool
	admission bool
	head      bool
}

type commentDeliveryErrorRow struct{}

func (commentDeliveryErrorRow) Scan(...any) error { return errors.New("fixture storage unavailable") }

func (d commentDeliveryFailingDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if d.head && strings.HasPrefix(sql, "-- name: GetIssueReviewHeadSha") {
		return commentDeliveryErrorRow{}
	}
	if d.admission && strings.HasPrefix(sql, "-- name: UpsertCommentDeliveryObligation") {
		return commentDeliveryErrorRow{}
	}
	return d.Pool.QueryRow(ctx, sql, args...)
}

func (d commentDeliveryFailingDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if !d.admission && strings.HasPrefix(sql, "-- name: ClaimCommentDeliveryObligations") {
		return nil, errors.New("fixture storage unavailable")
	}
	return d.Pool.Query(ctx, sql, args...)
}

func TestCommentDeliveryObligationStorageFailureDoesNotDispatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, mode := range []string{"admission", "drain"} {
		t.Run(mode, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "failed obligation runtime")
			agentID := dbfx.Agent(t, "failed obligation target", runtimeID)
			issueID := dbfx.Issue(t, "failed obligation storage", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID})
			commentID := dbfx.Comment(t, issueID, "Requires a durable admission")
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM comment_delivery_obligation WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
				testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
			})
			q := db.New(commentDeliveryFailingDB{Pool: testPool, admission: mode == "admission"})
			h := New(q, testPool, testHandler.Hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, Config{})
			if mode == "admission" {
				issue, err := testHandler.Queries.GetIssue(t.Context(), parseUUID(issueID))
				if err != nil {
					t.Fatal(err)
				}
				agent, err := testHandler.Queries.GetAgent(t.Context(), parseUUID(agentID))
				if err != nil {
					t.Fatal(err)
				}
				status, reason := h.resolveCommentTriggerEnqueue(t.Context(), issue, commentAgentTrigger{Agent: agent, Source: commentTriggerSourceIssueAssignee}, commentDeliverySourceForTest(t, parseUUID(commentID)))
				if status != DispatchBlocked || reason != ReasonInternalError {
					t.Fatalf("unpersisted input acknowledged as %s/%s", status, reason)
				}
			} else {
				if _, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), commentDeliveryAdmissionForTest(t, db.UpsertCommentDeliveryObligationParams{WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(commentID), AgentID: parseUUID(agentID)})); err != nil {
					t.Fatal(err)
				}
				if err := h.DrainCommentDeliveryObligations(t.Context(), 50); err == nil {
					t.Fatal("drain storage failure was hidden")
				}
				if _, err := testHandler.Queries.GetCommentDeliveryObligation(t.Context(), db.GetCommentDeliveryObligationParams{WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(commentID), AgentID: parseUUID(agentID)}); err != nil {
					t.Fatalf("storage failure lost accepted input: %v", err)
				}
			}
			var tasks int
			dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID).Scan(&tasks)
			if tasks != 0 {
				t.Fatalf("storage failure started %d tasks", tasks)
			}
		})
	}
}

func TestCommentDeliveryObligationHeadReadFailureDoesNotMerge(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "head failure runtime")
	agentID := dbfx.Agent(t, "head failure recipient", runtimeID)
	issueID := dbfx.Issue(t, "head failure issue", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID})
	original := dbfx.Comment(t, issueID, "Review the previous head")
	followup := dbfx.Comment(t, issueID, "Review the new head")
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "queued", "trigger_comment_id": original})
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM comment_delivery_obligation WHERE comment_id=$1`, followup)
	})
	q := db.New(commentDeliveryFailingDB{Pool: testPool, head: true})
	h := New(q, testPool, testHandler.Hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, Config{})
	issue, err := testHandler.Queries.GetIssue(t.Context(), parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := testHandler.Queries.GetAgent(t.Context(), parseUUID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	status, reason := h.resolveCommentTriggerEnqueue(t.Context(), issue, commentAgentTrigger{Agent: agent, Source: commentTriggerSourceIssueAssignee, AlreadyPending: true}, commentDeliverySourceForTest(t, parseUUID(followup)))
	if status != DispatchDeferred || reason != ReasonDeferred {
		t.Errorf("head read failure returned %s/%s, want durable deferral", status, reason)
	}
	var changed bool
	dbfx.QueryRow(t, `SELECT trigger_comment_id=$2 OR $2=ANY(coalesced_comment_ids) FROM agent_task_queue WHERE id=$1`, taskID, followup).Scan(&changed)
	if changed {
		t.Error("head read failure merged input using a wildcard head")
	}
	if _, err := testHandler.Queries.GetCommentDeliveryObligation(t.Context(), db.GetCommentDeliveryObligationParams{WorkspaceID: issue.WorkspaceID, CommentID: parseUUID(followup), AgentID: agent.ID}); err != nil {
		t.Fatalf("head read failure lost accepted input: %v", err)
	}
}

type commentDeliveryBeginFailure struct{}

func (commentDeliveryBeginFailure) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("fixture mutation transaction unavailable")
}

func TestCommentDeliveryObligationSurvivesFailedCommentMutation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, action := range []string{"edit", "delete", "edit rollback", "delete rollback"} {
		t.Run(action, func(t *testing.T) {
			issueID := dbfx.Issue(t, "failed comment mutation")
			commentID := dbfx.Comment(t, issueID, "Accepted instruction must survive a failed mutation")
			agentID := dbfx.Agent(t, "failed mutation recipient", "")
			_, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), commentDeliveryAdmissionForTest(t, db.UpsertCommentDeliveryObligationParams{
				WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(commentID), AgentID: parseUUID(agentID),
			}))
			if err != nil {
				t.Fatal(err)
			}
			dbfx.Cleanup(t, `DELETE FROM comment_delivery_obligation WHERE comment_id=$1`, commentID)
			h := New(db.New(testPool), testPool, testHandler.Hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, Config{})
			h.TxStarter = commentDeliveryBeginFailure{}
			if strings.HasSuffix(action, "rollback") {
				h.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
			}
			var call http.HandlerFunc
			var req *http.Request
			if strings.HasPrefix(action, "edit") {
				call = h.UpdateComment
				req = newRequest("PATCH", "/api/comments/"+commentID, map[string]any{"content": "/note Withdrawn"})
			} else {
				call = h.DeleteComment
				req = newRequest("DELETE", "/api/comments/"+commentID, nil)
			}
			testutil.Call(t, call, withURLParam(req, "commentId", commentID)).Want(500)
			if _, err := testHandler.Queries.GetCommentDeliveryObligation(t.Context(), db.GetCommentDeliveryObligationParams{
				WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(commentID), AgentID: parseUUID(agentID),
			}); err != nil {
				t.Fatalf("failed %s lost an accepted instruction with no task plan to restore: %v", action, err)
			}
			var unchanged bool
			dbfx.QueryRow(t, `SELECT deleted_at IS NULL AND content=$2 FROM comment WHERE id=$1`, commentID, "Accepted instruction must survive a failed mutation").Scan(&unchanged)
			if !unchanged {
				t.Fatal("failed mutation changed the comment")
			}
			h.TxStarter = testPool
			want := http.StatusNoContent
			if strings.HasPrefix(action, "edit") {
				req = newRequest("PATCH", "/api/comments/"+commentID, map[string]any{"content": "/note Withdrawn"})
				want = http.StatusOK
			}
			testutil.Call(t, call, withURLParam(req, "commentId", commentID)).Want(want)
			if _, err := testHandler.Queries.GetCommentDeliveryObligation(t.Context(), db.GetCommentDeliveryObligationParams{
				WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(commentID), AgentID: parseUUID(agentID),
			}); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("committed %s did not withdraw the accepted instruction: %v", action, err)
			}
		})
	}
}

func TestCommentDeliveryObligationFreshHeadReadFailureDoesNotEnqueue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, triggerSource := range []commentAgentTriggerSource{commentTriggerSourceIssueAssignee, commentTriggerSourceMentionAgent} {
		t.Run(string(triggerSource), func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "fresh head failure runtime")
			agentID := dbfx.Agent(t, "fresh head failure recipient", runtimeID)
			issueID := dbfx.Issue(t, "fresh head failure issue", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID})
			commentID := dbfx.Comment(t, issueID, "Review the current head")
			dbfx.Cleanup(t, `DELETE FROM comment_delivery_obligation WHERE issue_id=$1`, issueID)
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, issueID)
			q := db.New(commentDeliveryFailingDB{Pool: testPool, head: true})
			h := New(q, testPool, testHandler.Hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, Config{})
			issue, err := testHandler.Queries.GetIssue(t.Context(), parseUUID(issueID))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := testHandler.Queries.GetAgent(t.Context(), parseUUID(agentID))
			if err != nil {
				t.Fatal(err)
			}
			status, reason := h.resolveCommentTriggerEnqueue(t.Context(), issue, commentAgentTrigger{Agent: agent, Source: triggerSource}, commentDeliverySourceForTest(t, parseUUID(commentID)))
			if status != DispatchDeferred || reason != ReasonDeferred {
				t.Errorf("fresh head read failure returned %s/%s, want durable deferral", status, reason)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID); n != 0 {
				t.Fatalf("fresh head read failure created %d tasks with an unknown review head", n)
			}
			if _, err := testHandler.Queries.GetCommentDeliveryObligation(t.Context(), db.GetCommentDeliveryObligationParams{WorkspaceID: issue.WorkspaceID, CommentID: parseUUID(commentID), AgentID: agent.ID}); err != nil {
				t.Fatalf("fresh head read failure lost accepted input: %v", err)
			}
		})
	}
}
