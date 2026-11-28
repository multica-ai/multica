package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestDeleteCommentAuditSurvivesRemoval(t *testing.T) {
	for _, withTask := range []bool{false, true} {
		name := "member_without_task"
		if withTask {
			name = "agent_with_task"
		}
		t.Run(name, func(t *testing.T) {
			issueID := dbfx.Issue(t, "durable deletion audit")
			parent := dbfx.Comment(t, issueID, "parent")
			authorType, authorID := "member", testUserID
			var sourceTask any
			if withTask {
				authorType = "agent"
				authorID = dbfx.Agent(t, "audit author", testRuntimeID)
				sourceTask = dbfx.Task(t, authorID, testutil.Cols{"issue_id": issueID, "runtime_id": testRuntimeID, "status": "completed"})
			}
			commentID := dbfx.Comment(t, issueID, "private deleted body", testutil.Cols{
				"parent_id": parent, "author_type": authorType, "author_id": authorID, "source_task_id": sourceTask,
			})
			rec := newCommentEventRecorder()
			rec.delete(t, commentID).Want(http.StatusNoContent)
			if state := loadCommentRowState(t, commentID); state.exists {
				t.Fatal("deleted leaf still exists")
			}
			assertCommentDeletionAudit(t, issueID, commentID, "member", testUserID, map[string]any{
				"comment_id": commentID, "parent_id": parent, "author_type": authorType, "author_id": authorID,
				"source_task_id": sourceTask, "tombstoned": false,
			})
			rec.delete(t, commentID).Want(http.StatusNotFound)
			if count := dbfx.Count(t, `SELECT count(*) FROM activity_log WHERE issue_id=$1 AND action='comment_deleted'`, issueID); count != 1 {
				t.Fatalf("audit count=%d, want 1", count)
			}
		})
	}
}

func TestDeleteCommentAuditTombstonePruningDoesNotDuplicate(t *testing.T) {
	issueID := dbfx.Issue(t, "tombstone audit")
	root := dbfx.Comment(t, issueID, "private root body")
	reply := dbfx.Comment(t, issueID, "private reply body", testutil.Cols{"parent_id": root})
	rec := newCommentEventRecorder()
	rec.delete(t, root).Want(http.StatusNoContent)
	assertCommentDeletionAudit(t, issueID, root, "member", testUserID, map[string]any{
		"comment_id": root, "parent_id": nil, "author_type": "member", "author_id": testUserID, "source_task_id": nil, "tombstoned": true,
	})
	rec.delete(t, reply).Want(http.StatusNoContent)
	if state := loadCommentRowState(t, root); state.exists {
		t.Fatal("replyless tombstone not pruned")
	}
	assertCommentDeletionAudit(t, issueID, root, "member", testUserID, map[string]any{
		"comment_id": root, "parent_id": nil, "author_type": "member", "author_id": testUserID, "source_task_id": nil, "tombstoned": true,
	})
	assertCommentDeletionAudit(t, issueID, reply, "member", testUserID, map[string]any{
		"comment_id": reply, "parent_id": root, "author_type": "member", "author_id": testUserID, "source_task_id": nil, "tombstoned": false,
	})
}

func TestDeleteCommentAuditSeparatesDeletingRunFromPostingRun(t *testing.T) {
	issueID := dbfx.Issue(t, "agent deletion audit")
	agentID := dbfx.Agent(t, "audit deleting agent", testRuntimeID)
	postingTask := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": testRuntimeID, "status": "completed"})
	deletingTask := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": testRuntimeID, "status": "running"})
	commentID := dbfx.Comment(t, issueID, "private answer", testutil.Cols{"author_type": "agent", "author_id": agentID, "source_task_id": postingTask})
	req := withURLParam(newRequest(http.MethodDelete, "/api/comments/"+commentID, nil), "commentId", commentID)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", deletingTask)
	testutil.Call(t, testHandler.DeleteComment, req).Want(http.StatusNoContent)
	assertCommentDeletionAudit(t, issueID, commentID, "agent", agentID, map[string]any{
		"comment_id": commentID, "parent_id": nil, "author_type": "agent", "author_id": agentID, "source_task_id": postingTask, "tombstoned": false,
	})
}

func TestDeleteCommentAuditRollsBackWithDelete(t *testing.T) {
	issueID := dbfx.Issue(t, "rollback deletion audit")
	commentID := dbfx.Comment(t, issueID, "keep on rollback")
	h := *testHandler
	h.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
	req := withURLParam(newRequest(http.MethodDelete, "/api/comments/"+commentID, nil), "commentId", commentID)
	testutil.Call(t, h.DeleteComment, req).Want(http.StatusInternalServerError)
	if state := loadCommentRowState(t, commentID); !state.exists || state.deleted || state.content != "keep on rollback" {
		t.Fatalf("comment changed on rollback: %+v", state)
	}
	if count := dbfx.Count(t, `SELECT count(*) FROM activity_log WHERE issue_id=$1 AND action='comment_deleted'`, issueID); count != 0 {
		t.Fatalf("rollback leaked %d activities", count)
	}
}

func assertCommentDeletionAudit(t *testing.T, issueID, commentID, actorType, actorID string, want map[string]any) {
	t.Helper()
	entries, status := fetchTimeline(t, issueID)
	if status != http.StatusOK {
		t.Fatalf("timeline status=%d", status)
	}
	count := 0
	for _, entry := range entries {
		if entry.Action == nil || *entry.Action != "comment_deleted" {
			continue
		}
		var details map[string]any
		if err := json.Unmarshal(entry.Details, &details); err != nil {
			t.Fatal(err)
		}
		if details["comment_id"] != commentID {
			continue
		}
		count++
		if entry.Type != "activity" || entry.ActorType != actorType || entry.ActorID != actorID || entry.CreatedAt == "" {
			t.Fatalf("incorrect deleting actor/timestamp: %+v", entry)
		}
		if len(details) != len(want) {
			t.Fatalf("audit fields=%v, want exactly %v (no body)", details, want)
		}
		for key, value := range want {
			if actual, exists := details[key]; !exists || actual != value {
				t.Errorf("%s=%v (present=%t), want %v", key, actual, exists, value)
			}
		}
	}
	if count != 1 {
		t.Fatalf("comment %s has %d deletion activities in timeline, want 1", commentID, count)
	}
}

// Fail only the audit insert; every comment mutation still uses PostgreSQL.
type failCommentDeletionAuditTx struct{ pgx.Tx }

func (tx failCommentDeletionAuditTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "-- name: CreateActivity") {
		return errorRow{err: errors.New("injected audit failure")}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func TestDeleteCommentAuditInsertFailureRollsBackDelete(t *testing.T) {
	for _, tombstone := range []bool{false, true} {
		name := "leaf"
		if tombstone {
			name = "tombstone"
		}
		t.Run(name, func(t *testing.T) {
			issueID := dbfx.Issue(t, "audit storage failure")
			commentID := dbfx.Comment(t, issueID, "preserve answer on audit failure")
			if tombstone {
				dbfx.Comment(t, issueID, "reply", testutil.Cols{"parent_id": commentID})
			}
			h := *testHandler
			h.TxStarter = privacyBeginHook(func(ctx context.Context) (pgx.Tx, error) {
				tx, err := testPool.Begin(ctx)
				if err != nil {
					return nil, err
				}
				return failCommentDeletionAuditTx{Tx: tx}, nil
			})
			req := withURLParam(newRequest(http.MethodDelete, "/api/comments/"+commentID, nil), "commentId", commentID)
			testutil.Call(t, h.DeleteComment, req).Want(http.StatusInternalServerError)
			if state := loadCommentRowState(t, commentID); !state.exists || state.deleted || state.content != "preserve answer on audit failure" {
				t.Fatalf("audit failure changed comment: %+v", state)
			}
			if count := dbfx.Count(t, `SELECT count(*) FROM activity_log WHERE issue_id=$1 AND action='comment_deleted'`, issueID); count != 0 {
				t.Fatalf("failed audit left %d activities", count)
			}
		})
	}
}
