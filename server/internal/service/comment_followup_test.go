package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/pooltestdb"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCommentFollowupLocksCommentBeforeObligation(t *testing.T) {
	raw, err := os.ReadFile("../../pkg/db/queries/comment_followup.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, fragment := range []string{
		"ON CONFLICT (agent_id, comment_id)",
		"-- name: ListCommentFollowupObligations",
		"-- name: LockCommentForFollowup",
		"-- name: LockCommentFollowupObligation",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("query missing %q", fragment)
		}
	}
	listStart := strings.Index(sql, "-- name: ListCommentFollowupObligations")
	lockStart := strings.Index(sql, "-- name: LockCommentForFollowup")
	if strings.Contains(sql[listStart:lockStart], "FOR UPDATE") {
		t.Fatal("scanner must not lock obligation before Comment")
	}
}

func TestCommentFollowupInitialUpsertLocksComment(t *testing.T) {
	ctx := context.Background()
	pool := pooltestdb.Open(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	suffix := time.Now().UnixNano()
	var workspaceID, userID, agentID, issueID, commentID pgtype.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ('followup', $1, '') RETURNING id`, fmt.Sprintf("followup-%d", suffix)).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('followup', $1) RETURNING id`, fmt.Sprintf("followup-%d@multica.test", suffix)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, workspaceID, userID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, owner_id)
		VALUES ($1, 'followup-agent', 'cloud', $2) RETURNING id`, workspaceID, userID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'followup issue', 'member', $2) RETURNING id`, workspaceID, userID).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'please follow up') RETURNING id`, issueID, workspaceID, userID).Scan(&commentID); err != nil {
		t.Fatal(err)
	}

	svc := NewTaskService(db.New(tx), tx, nil, events.New())
	if err := svc.PersistCommentFollowup(ctx, issueID, agentID, commentID, "head-1"); err != nil {
		t.Fatalf("PersistCommentFollowup first: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE comment SET content='edited', updated_at=clock_timestamp() WHERE id=$1`, commentID); err != nil {
		t.Fatal(err)
	}
	if err := svc.PersistCommentFollowup(ctx, issueID, agentID, commentID, "head-2"); err != nil {
		t.Fatalf("PersistCommentFollowup edit: %v", err)
	}

	var count int
	var gotHead string
	var gotCommentUpdatedAt, commentUpdatedAt pgtype.Timestamptz
	if err := tx.QueryRow(ctx, `
		SELECT count(*), max(head_sha), max(comment_updated_at)
		FROM agent_comment_followup_obligation
		WHERE agent_id=$1 AND comment_id=$2`, agentID, commentID).Scan(&count, &gotHead, &gotCommentUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `SELECT updated_at FROM comment WHERE id=$1`, commentID).Scan(&commentUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if count != 1 || gotHead != "head-2" || !gotCommentUpdatedAt.Time.Equal(commentUpdatedAt.Time) {
		t.Fatalf("obligation count/head/version = %d/%q/%v, want 1/head-2/%v", count, gotHead, gotCommentUpdatedAt, commentUpdatedAt)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE agent
		SET runtime_binding_mode='pool', runtime_mode='pool', runtime_id=NULL,
		    runtime_requirements='{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["multica.extension.execute/v1"]}'::jsonb
		WHERE id=$1`, agentID); err != nil {
		t.Fatal(err)
	}
	scheduler := &runtimePoolSeamScheduler{}
	svc.RuntimePool = scheduler
	if err := svc.ProcessCommentFollowups(ctx, 8); err != nil {
		t.Fatalf("ProcessCommentFollowups: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_comment_followup_obligation
		WHERE agent_id=$1 AND comment_id=$2`, agentID, commentID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("obligations after successful replay = %d, want 0", count)
	}
	var taskStatus string
	var taskRuntimeID, taskTriggerCommentID pgtype.UUID
	if err := tx.QueryRow(ctx, `
		SELECT status, runtime_id, trigger_comment_id
		FROM agent_task_queue
		WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID).Scan(&taskStatus, &taskRuntimeID, &taskTriggerCommentID); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "waiting_runtime" || taskRuntimeID.Valid || taskTriggerCommentID != commentID {
		t.Fatalf("follow-up task = status:%q runtime:%v trigger:%v", taskStatus, taskRuntimeID, taskTriggerCommentID)
	}
	if len(scheduler.assignCalls) != 1 {
		t.Fatalf("allocator calls = %d, want 1", len(scheduler.assignCalls))
	}
}
