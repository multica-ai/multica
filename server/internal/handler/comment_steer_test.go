package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type commentSteerFixture struct {
	agentID, runtimeID, issueID, rootID, taskID string
}

func newCommentSteerFixture(t *testing.T, suffix string) commentSteerFixture {
	t.Helper()
	runtimeID := dbfx.Runtime(t, "comment-steer-"+suffix, testutil.Cols{
		"provider": "codex",
		"metadata": testutil.Raw(`jsonb_build_object('capabilities', jsonb_build_array('task-steer-v1'))`),
	})
	agentID := dbfx.Agent(t, "comment-steer-"+suffix, runtimeID, testutil.Cols{"max_concurrent_tasks": 2})
	issueID := dbfx.Issue(t, "comment steer "+suffix, testutil.Cols{
		"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID,
	})
	rootID := dbfx.Comment(t, issueID, "initial instruction", testutil.Cols{
		"created_at": testutil.Raw("now() - interval '10 minutes'"),
	})
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "trigger_comment_id": rootID,
		"comment_thread_id": rootID, "status": "running",
		"created_at":            testutil.Raw("now() - interval '9 minutes'"),
		"started_at":            testutil.Raw("now() - interval '8 minutes'"),
		"delivered_comment_ids": testutil.Raw("ARRAY['" + rootID + "'::uuid]"),
	})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment_agent_delivery WHERE agent_id = $1`, agentID)
	})
	return commentSteerFixture{agentID: agentID, runtimeID: runtimeID, issueID: issueID, rootID: rootID, taskID: taskID}
}

func (f commentSteerFixture) reply(t *testing.T, content, age string) string {
	t.Helper()
	return dbfx.Comment(t, f.issueID, content, testutil.Cols{
		"parent_id": f.rootID, "created_at": testutil.Raw("now() - interval '" + age + "'"),
	})
}

func registerCommentSteer(t *testing.T, f commentSteerFixture, commentID string) db.RegisterCommentSteerRow {
	t.Helper()
	row, err := testHandler.Queries.RegisterCommentSteer(context.Background(), db.RegisterCommentSteerParams{
		CommentID: util.MustParseUUID(commentID), AgentID: util.MustParseUUID(f.agentID), IssueID: util.MustParseUUID(f.issueID),
	})
	if err != nil {
		t.Fatalf("register steer: %v", err)
	}
	return row
}

func TestCommentSteerRegistrationIsUniqueCapabilityGatedAndIdempotent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	t.Run("unique supported task", func(t *testing.T) {
		f := newCommentSteerFixture(t, "unique")
		commentID := f.reply(t, "apply this constraint", "2 minutes")
		row := registerCommentSteer(t, f, commentID)
		if got := uuidToString(row.TaskID); got != f.taskID {
			t.Fatalf("registered task = %s, want %s", got, f.taskID)
		}
		var planned, delivered bool
		dbfx.QueryRow(t, `
			SELECT $2::uuid = ANY(coalesced_comment_ids), $2::uuid = ANY(delivered_comment_ids)
			FROM agent_task_queue WHERE id = $1
		`, f.taskID, commentID).Scan(&planned, &delivered)
		if !planned || delivered {
			t.Fatalf("planned=%v delivered=%v, want true/false (option A)", planned, delivered)
		}

		// A transport retry cannot create a second receipt or mutate the task.
		_, err := testHandler.Queries.RegisterCommentSteer(context.Background(), db.RegisterCommentSteerParams{
			CommentID: util.MustParseUUID(commentID), AgentID: util.MustParseUUID(f.agentID), IssueID: util.MustParseUUID(f.issueID),
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("duplicate registration error = %v, want pgx.ErrNoRows", err)
		}
		var receiptCount, plannedCount int
		dbfx.QueryRow(t, `SELECT count(*) FROM comment_agent_delivery WHERE comment_id=$1 AND agent_id=$2`, commentID, f.agentID).Scan(&receiptCount)
		dbfx.QueryRow(t, `SELECT count(*) FROM unnest((SELECT coalesced_comment_ids FROM agent_task_queue WHERE id=$1)) id WHERE id=$2`, f.taskID, commentID).Scan(&plannedCount)
		if receiptCount != 1 || plannedCount != 1 {
			t.Fatalf("receipt count=%d planned count=%d, want 1/1", receiptCount, plannedCount)
		}
	})

	t.Run("unsupported runtime", func(t *testing.T) {
		f := newCommentSteerFixture(t, "unsupported")
		commentID := f.reply(t, "must follow up", "2 minutes")
		dbfx.Exec(t, `UPDATE agent_runtime SET metadata='{}'::jsonb WHERE id=$1`, f.runtimeID)
		_, err := testHandler.Queries.RegisterCommentSteer(context.Background(), db.RegisterCommentSteerParams{
			CommentID: util.MustParseUUID(commentID), AgentID: util.MustParseUUID(f.agentID), IssueID: util.MustParseUUID(f.issueID),
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("unsupported registration error = %v, want pgx.ErrNoRows", err)
		}
		if err := testHandler.Queries.RecordCommentFollowUpDelivery(context.Background(), db.RecordCommentFollowUpDeliveryParams{
			CommentID: util.MustParseUUID(commentID), AgentID: util.MustParseUUID(f.agentID),
			FailureReason: pgtype.Text{String: "runtime_unsupported", Valid: true},
		}); err != nil {
			t.Fatalf("record follow-up receipt: %v", err)
		}
		var status string
		dbfx.QueryRow(t, `SELECT status FROM comment_agent_delivery WHERE comment_id=$1 AND agent_id=$2`, commentID, f.agentID).Scan(&status)
		if status != "follow_up" {
			t.Fatalf("unsupported receipt status = %s, want follow_up", status)
		}
	})

	t.Run("OpenCode 2.x remains deny by default", func(t *testing.T) {
		f := newCommentSteerFixture(t, "opencode-v2")
		commentID := f.reply(t, "must follow up", "2 minutes")
		// OpenCode 2.x is still provider=opencode. Even a new daemon advertising
		// task-steer-v1 globally must not make this non-steerable Session eligible.
		dbfx.Exec(t, `UPDATE agent_runtime SET provider='opencode' WHERE id=$1`, f.runtimeID)
		_, err := testHandler.Queries.RegisterCommentSteer(context.Background(), db.RegisterCommentSteerParams{
			CommentID: util.MustParseUUID(commentID), AgentID: util.MustParseUUID(f.agentID), IssueID: util.MustParseUUID(f.issueID),
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("OpenCode registration error = %v, want pgx.ErrNoRows", err)
		}
	})

	t.Run("ambiguous active tasks", func(t *testing.T) {
		f := newCommentSteerFixture(t, "ambiguous")
		commentID := f.reply(t, "route only when unique", "2 minutes")
		dbfx.Task(t, f.agentID, testutil.Cols{
			"runtime_id": f.runtimeID, "issue_id": f.issueID, "trigger_comment_id": f.rootID,
			"comment_thread_id": f.rootID, "status": "running", "started_at": testutil.Raw("now()"),
		})
		_, err := testHandler.Queries.RegisterCommentSteer(context.Background(), db.RegisterCommentSteerParams{
			CommentID: util.MustParseUUID(commentID), AgentID: util.MustParseUUID(f.agentID), IssueID: util.MustParseUUID(f.issueID),
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("ambiguous registration error = %v, want pgx.ErrNoRows", err)
		}
		var receipts int
		dbfx.QueryRow(t, `SELECT count(*) FROM comment_agent_delivery WHERE comment_id=$1`, commentID).Scan(&receipts)
		if receipts != 0 {
			t.Fatalf("ambiguous route created %d receipts, want 0", receipts)
		}
	})
}

func TestCommentSteerClaimsChronologicallyAndFallsBackAcrossTerminalRace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newCommentSteerFixture(t, "ordering")
	olderID := f.reply(t, "older constraint", "4 minutes")
	newerID := f.reply(t, "newer constraint", "2 minutes")
	// Register out of order; claim order is the comments' durable chronology.
	registerCommentSteer(t, f, newerID)
	registerCommentSteer(t, f, olderID)

	first, err := testHandler.Queries.ClaimNextCommentSteer(context.Background(), util.MustParseUUID(f.taskID))
	if err != nil {
		t.Fatalf("claim first: %v", err)
	}
	if got := uuidToString(first.CommentID); got != olderID {
		t.Fatalf("first claim = %s, want older %s", got, olderID)
	}
	if _, err := testHandler.Queries.AckCommentSteerDelivered(context.Background(), db.AckCommentSteerDeliveredParams{
		TaskID: util.MustParseUUID(f.taskID), CommentID: util.MustParseUUID(olderID),
	}); err != nil {
		t.Fatalf("ack first: %v", err)
	}

	second, err := testHandler.Queries.ClaimNextCommentSteer(context.Background(), util.MustParseUUID(f.taskID))
	if err != nil {
		t.Fatalf("claim second: %v", err)
	}
	if got := uuidToString(second.CommentID); got != newerID {
		t.Fatalf("second claim = %s, want newer %s", got, newerID)
	}
	// Completion wins before the daemon's acknowledgement. The ack cannot claim
	// success, and terminal reconciliation turns the instruction into follow-up.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed', completed_at=now() WHERE id=$1`, f.taskID)
	if _, err := testHandler.Queries.FinalizeUndeliveredCommentSteers(context.Background(), util.MustParseUUID(f.taskID)); err != nil {
		t.Fatalf("finalize completed task: %v", err)
	}
	_, err = testHandler.Queries.AckCommentSteerDelivered(context.Background(), db.AckCommentSteerDeliveredParams{
		TaskID: util.MustParseUUID(f.taskID), CommentID: util.MustParseUUID(newerID),
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("late ack error = %v, want pgx.ErrNoRows", err)
	}
	var olderStatus, newerStatus string
	dbfx.QueryRow(t, `SELECT status FROM comment_agent_delivery WHERE comment_id=$1 AND agent_id=$2`, olderID, f.agentID).Scan(&olderStatus)
	dbfx.QueryRow(t, `SELECT status FROM comment_agent_delivery WHERE comment_id=$1 AND agent_id=$2`, newerID, f.agentID).Scan(&newerStatus)
	if olderStatus != "delivered" || newerStatus != "follow_up" {
		t.Fatalf("receipt statuses = %s/%s, want delivered/follow_up", olderStatus, newerStatus)
	}

}

func TestUpdateCommentDuringSteeringDoesNotQueueDuplicateOrRewriteReceipt(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newCommentSteerFixture(t, "edit-in-flight")
	commentID := f.reply(t, "old constraint", "2 minutes")
	registerCommentSteer(t, f, commentID)
	claimed, err := testHandler.Queries.ClaimNextCommentSteer(context.Background(), util.MustParseUUID(f.taskID))
	if err != nil {
		t.Fatalf("claim steer: %v", err)
	}
	if claimed.Content != "old constraint" {
		t.Fatalf("claimed content = %q, want old constraint", claimed.Content)
	}

	updateCommentForTriggerPreviewTest(t, commentID, map[string]any{
		"content":           "edited constraint",
		"expected_revision": 1,
	})

	var taskCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, f.issueID, f.agentID).Scan(&taskCount)
	if taskCount != 1 {
		t.Fatalf("task count after in-flight edit = %d, want the original task only", taskCount)
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM comment_agent_delivery WHERE comment_id=$1 AND agent_id=$2`, commentID, f.agentID).Scan(&status)
	if status != "steering" {
		t.Fatalf("receipt after in-flight edit = %s, want steering", status)
	}
	if err := testHandler.Queries.RecordCommentFollowUpDelivery(context.Background(), db.RecordCommentFollowUpDeliveryParams{
		CommentID: util.MustParseUUID(commentID), AgentID: util.MustParseUUID(f.agentID),
		FailureReason: pgtype.Text{String: "stale_edit_fallback", Valid: true},
	}); err != nil {
		t.Fatalf("record stale follow-up: %v", err)
	}
	dbfx.QueryRow(t, `SELECT status FROM comment_agent_delivery WHERE comment_id=$1 AND agent_id=$2`, commentID, f.agentID).Scan(&status)
	if status != "steering" {
		t.Fatalf("stale fallback rewrote in-flight receipt to %s", status)
	}
	if _, err := testHandler.Queries.AckCommentSteerDelivered(context.Background(), db.AckCommentSteerDeliveredParams{
		TaskID: util.MustParseUUID(f.taskID), CommentID: util.MustParseUUID(commentID),
	}); err != nil {
		t.Fatalf("ack historical delivery: %v", err)
	}
	dbfx.QueryRow(t, `SELECT status FROM comment_agent_delivery WHERE comment_id=$1 AND agent_id=$2`, commentID, f.agentID).Scan(&status)
	if status != "delivered" {
		t.Fatalf("historical receipt after ack = %s, want delivered", status)
	}
}

func TestCancelTaskSettlesUndeliveredSteersWithoutStartingRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newCommentSteerFixture(t, "stop")
	deliveredID := f.reply(t, "already injected", "4 minutes")
	pendingID := f.reply(t, "claimed but not injected", "2 minutes")
	registerCommentSteer(t, f, deliveredID)
	registerCommentSteer(t, f, pendingID)
	if _, err := testHandler.Queries.ClaimNextCommentSteer(context.Background(), util.MustParseUUID(f.taskID)); err != nil {
		t.Fatalf("claim delivered steer: %v", err)
	}
	if _, err := testHandler.Queries.AckCommentSteerDelivered(context.Background(), db.AckCommentSteerDeliveredParams{
		TaskID: util.MustParseUUID(f.taskID), CommentID: util.MustParseUUID(deliveredID),
	}); err != nil {
		t.Fatalf("ack delivered steer: %v", err)
	}
	if _, err := testHandler.Queries.ClaimNextCommentSteer(context.Background(), util.MustParseUUID(f.taskID)); err != nil {
		t.Fatalf("claim pending steer: %v", err)
	}

	req := newRequest(http.MethodPost, "/api/issues/"+f.issueID+"/tasks/"+f.taskID+"/cancel", nil)
	req = withURLParams(req, "id", f.issueID, "taskId", f.taskID)
	testutil.Call(t, testHandler.CancelTask, req).Want(http.StatusOK)

	var deliveredStatus, pendingStatus string
	var deliveredTimestampPreserved bool
	dbfx.QueryRow(t, `SELECT status, delivered_at IS NOT NULL FROM comment_agent_delivery WHERE comment_id=$1 AND agent_id=$2`, deliveredID, f.agentID).Scan(&deliveredStatus, &deliveredTimestampPreserved)
	dbfx.QueryRow(t, `SELECT status FROM comment_agent_delivery WHERE comment_id=$1 AND agent_id=$2`, pendingID, f.agentID).Scan(&pendingStatus)
	if deliveredStatus != "delivered" || !deliveredTimestampPreserved || pendingStatus != "follow_up" {
		t.Fatalf("receipts after Stop = delivered %s (timestamp=%v), pending %s; want delivered/true/follow_up",
			deliveredStatus, deliveredTimestampPreserved, pendingStatus)
	}
	var taskCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, f.issueID, f.agentID).Scan(&taskCount)
	if taskCount != 1 {
		t.Fatalf("task count after Stop = %d, want no replacement run", taskCount)
	}
}
