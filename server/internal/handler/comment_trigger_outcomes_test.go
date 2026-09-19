package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

func TestCommentTriggerDeliveryRetriesOnlyInternalFailures(t *testing.T) {
	outcomes := []CommentTriggerOutcome{
		{TargetType: "agent", TargetID: "ok", Status: DispatchQueued, ReasonCode: ReasonQueued},
		{TargetType: "agent", TargetID: "retry", Status: DispatchBlocked, ReasonCode: ReasonInternalError},
	}
	results := map[string]commentEnqueueResult{
		"ok":    {status: DispatchQueued, reason: ReasonQueued},
		"retry": {status: DispatchBlocked, reason: ReasonInternalError},
	}
	if !errors.Is(commentTriggerDeliveryError(outcomes, results), errCommentTriggerDeliveryRetryable) {
		t.Fatal("internal enqueue failure must keep the outbox retryable")
	}
	results["retry"] = commentEnqueueResult{status: DispatchBlocked, reason: ReasonAttributionBlocked}
	outcomes[1].ReasonCode = ReasonAttributionBlocked
	if err := commentTriggerDeliveryError(outcomes, results); err != nil {
		t.Fatalf("permanent refusal must not retry forever: %v", err)
	}
}

func findCommentOutcome(t *testing.T, outcomes []CommentTriggerOutcome, targetID string) CommentTriggerOutcome {
	t.Helper()
	for _, o := range outcomes {
		if o.TargetID == targetID {
			return o
		}
	}
	t.Fatalf("no trigger outcome for target %s in %+v", targetID, outcomes)
	return CommentTriggerOutcome{}
}

// TestCommentTriggerOutboxRepairsCommitBeforeDispatchCrash models the exact
// crash window the durable outbox closes: the comment and trigger intent have
// committed, but the HTTP process dies before routing the comment to an agent.
// The sweeper must enqueue the work once and a second pass must be a no-op.
func TestCommentTriggerOutboxRepairsCommitBeforeDispatchCrash(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "Durable Comment Trigger Target", nil)
	issueID := createCommentTriggerPreviewIssue(t, "durable comment trigger", "", "")
	commentID := dbid.NewV7()
	content := fmt.Sprintf("[@Target](mention://agent/%s) please continue", agentID)

	if _, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID: commentID, IssueID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
		AuthorType: "member", AuthorID: parseUUID(testUserID), Content: content, Type: "comment",
	}); err != nil {
		t.Fatalf("persist crash-window comment: %v", err)
	}
	if err := testHandler.Queries.CreateCommentTriggerOutbox(ctx, db.CreateCommentTriggerOutboxParams{
		CommentID: commentID, WorkspaceID: parseUUID(testWorkspaceID), IssueID: parseUUID(issueID),
		ActorType: "member", ActorID: parseUUID(testUserID),
		OriginatorUserID: pgtype.UUID{Bytes: parseUUID(testUserID).Bytes, Valid: true},
		SuppressAgentIds: []pgtype.UUID{},
	}); err != nil {
		t.Fatalf("persist crash-window trigger intent: %v", err)
	}

	result, err := testHandler.DeliverCommentTriggerOutbox(ctx, 10)
	if err != nil || result.Claimed != 1 || result.Done != 1 || result.Failed != 0 {
		t.Fatalf("first outbox delivery = %+v, %v", result, err)
	}
	if got := countQueuedCommentTriggerTasks(t, issueID, agentID); got != 1 {
		t.Fatalf("queued tasks after recovery = %d, want 1", got)
	}

	result, err = testHandler.DeliverCommentTriggerOutbox(ctx, 10)
	if err != nil || result.Claimed != 0 {
		t.Fatalf("second outbox delivery = %+v, %v, want no claim", result, err)
	}
	if got := countQueuedCommentTriggerTasks(t, issueID, agentID); got != 1 {
		t.Fatalf("queued tasks after replay = %d, want 1", got)
	}
}

func TestCommentTriggerOutboxDoesNotRepeatACompletedDeliveredTarget(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "Crash After Trigger Target", nil)
	issueID := createCommentTriggerPreviewIssue(t, "crash after trigger", "", "")
	commentID := dbid.NewV7()
	content := fmt.Sprintf("[@Target](mention://agent/%s) please continue", agentID)
	if _, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID: commentID, IssueID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
		AuthorType: "member", AuthorID: parseUUID(testUserID), Content: content, Type: "comment",
	}); err != nil {
		t.Fatalf("create comment: %v", err)
	}
	comment, err := testHandler.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID: commentID, WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load comment: %v", err)
	}
	if err := testHandler.Queries.CreateCommentTriggerOutbox(ctx, db.CreateCommentTriggerOutboxParams{
		CommentID: commentID, WorkspaceID: parseUUID(testWorkspaceID), IssueID: parseUUID(issueID),
		ActorType: "member", ActorID: parseUUID(testUserID),
		OriginatorUserID: parseOptionalUUID(testUserID), SuppressAgentIds: []pgtype.UUID{},
	}); err != nil {
		t.Fatalf("create trigger outbox: %v", err)
	}
	issue, err := testHandler.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	if _, err := testHandler.triggerTasksForCommentDelivery(
		ctx, issue, comment, nil, "member", testUserID, testUserID, nil,
	); err != nil {
		t.Fatalf("first trigger: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		WITH delivered_task AS (
			UPDATE agent_task_queue
			SET status='completed', completed_at=now(), delivered_comment_ids=ARRAY[$3::uuid]
			WHERE issue_id=$1 AND agent_id=$2
			RETURNING id, agent_id
		)
		INSERT INTO comment_trigger_delivery_receipt (comment_id, comment_trigger_revision, agent_id, task_id)
		SELECT source.id, source.trigger_revision, delivered_task.agent_id, delivered_task.id
		FROM delivered_task JOIN comment source ON source.id=$3::uuid
	`, issueID, agentID, uuidToString(commentID)); err != nil {
		t.Fatalf("complete delivered task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE comment_trigger_outbox
		SET state='processing', attempts=1, lease_owner='crashed-worker',
		    lease_expires_at=now()-interval '1 second'
		WHERE comment_id=$1
	`, uuidToString(commentID)); err != nil {
		t.Fatalf("stage expired outbox lease: %v", err)
	}

	result, err := testHandler.DeliverCommentTriggerOutbox(ctx, 10)
	if err != nil || result.Done != 1 || result.Failed != 0 {
		t.Fatalf("replay delivery = %+v, %v", result, err)
	}
	var taskCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2
	`, issueID, agentID).Scan(&taskCount); err != nil {
		t.Fatalf("count target tasks: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("tasks after post-trigger crash replay = %d, want 1", taskCount)
	}
}

func TestPresentationOnlyCommentRevisionDoesNotInvalidateTriggerReceipt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "Stable Trigger Revision Target", nil)
	issueID := createCommentTriggerPreviewIssue(t, "stable trigger revision", "", "")
	commentID := dbid.NewV7()
	created, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID: commentID, IssueID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
		AuthorType: "member", AuthorID: parseUUID(testUserID),
		Content: fmt.Sprintf("[@Target](mention://agent/%s) keep this instruction stable", agentID), Type: "comment",
	})
	if err != nil {
		t.Fatalf("create stable trigger comment: %v", err)
	}
	if err := testHandler.Queries.CreateCommentTriggerOutbox(ctx, db.CreateCommentTriggerOutboxParams{
		CommentID: commentID, WorkspaceID: parseUUID(testWorkspaceID), IssueID: parseUUID(issueID),
		ActorType: "member", ActorID: parseUUID(testUserID), OriginatorUserID: parseOptionalUUID(testUserID),
		SuppressAgentIds: []pgtype.UUID{}, CommentTriggerRevision: created.TriggerRevision,
	}); err != nil {
		t.Fatalf("create stable trigger outbox: %v", err)
	}
	if _, err := testHandler.processCommentTriggerOutbox(ctx, commentID); err != nil {
		t.Fatalf("deliver stable trigger: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		WITH delivered_task AS (
			UPDATE agent_task_queue
			SET status='completed', completed_at=now(), delivered_comment_ids=ARRAY[$3::uuid]
			WHERE issue_id=$1 AND agent_id=$2
			RETURNING id, agent_id
		)
		INSERT INTO comment_trigger_delivery_receipt (comment_id, comment_trigger_revision, agent_id, task_id)
		SELECT source.id, source.trigger_revision, delivered_task.agent_id, delivered_task.id
		FROM delivered_task JOIN comment source ON source.id=$3::uuid
	`, issueID, agentID, uuidToString(commentID)); err != nil {
		t.Fatalf("record stable trigger delivery: %v", err)
	}
	if _, err := testHandler.Queries.AddReaction(ctx, db.AddReactionParams{
		CommentID: commentID, WorkspaceID: parseUUID(testWorkspaceID),
		ActorType: "member", ActorID: parseUUID(testUserID), Emoji: "👍",
	}); err != nil {
		t.Fatalf("add presentation-only reaction: %v", err)
	}
	var revision, triggerRevision int64
	if err := testPool.QueryRow(ctx, `
		SELECT revision, trigger_revision FROM comment WHERE id=$1
	`, uuidToString(commentID)).Scan(&revision, &triggerRevision); err != nil {
		t.Fatalf("load split revisions: %v", err)
	}
	if revision != 2 || triggerRevision != 1 {
		t.Fatalf("split revisions = ui:%d trigger:%d, want 2/1", revision, triggerRevision)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE comment_trigger_outbox
		SET state='processing', attempts=1, lease_owner='presentation-replay',
		    lease_expires_at=now()-interval '1 second'
		WHERE comment_id=$1
	`, uuidToString(commentID)); err != nil {
		t.Fatalf("stage presentation replay: %v", err)
	}
	if result, err := testHandler.DeliverCommentTriggerOutbox(ctx, 10); err != nil || result.Done != 1 || result.Failed != 0 {
		t.Fatalf("presentation replay = %+v, %v", result, err)
	}
	var taskCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2
	`, issueID, agentID).Scan(&taskCount); err != nil {
		t.Fatalf("count stable trigger tasks: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("presentation-only change created %d tasks, want 1 total", taskCount)
	}
}

func TestCommentTriggerOutboxRetriesOnlyTheMissingTarget(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	firstAgentID := createHandlerTestAgent(t, "Partial Trigger Delivered", nil)
	secondAgentID := createHandlerTestAgent(t, "Partial Trigger Retry", nil)
	issueID := createCommentTriggerPreviewIssue(t, "partial trigger retry", "", "")
	commentID := dbid.NewV7()
	content := fmt.Sprintf(
		"[@First](mention://agent/%s) [@Second](mention://agent/%s) continue",
		firstAgentID, secondAgentID,
	)
	if _, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID: commentID, IssueID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
		AuthorType: "member", AuthorID: parseUUID(testUserID), Content: content, Type: "comment",
	}); err != nil {
		t.Fatalf("create comment: %v", err)
	}
	comment, err := testHandler.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID: commentID, WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load comment: %v", err)
	}
	if err := testHandler.Queries.CreateCommentTriggerOutbox(ctx, db.CreateCommentTriggerOutboxParams{
		CommentID: commentID, WorkspaceID: parseUUID(testWorkspaceID), IssueID: parseUUID(issueID),
		ActorType: "member", ActorID: parseUUID(testUserID),
		OriginatorUserID: parseOptionalUUID(testUserID), SuppressAgentIds: []pgtype.UUID{},
	}); err != nil {
		t.Fatalf("create trigger outbox: %v", err)
	}
	issue, err := testHandler.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	if _, err := testHandler.triggerTasksForCommentDelivery(
		ctx, issue, comment, nil, "member", testUserID, testUserID, nil,
	); err != nil {
		t.Fatalf("initial trigger: %v", err)
	}
	var deliveredTaskID, missingTaskID string
	if err := testPool.QueryRow(ctx, `
		SELECT id::text FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2
	`, issueID, firstAgentID).Scan(&deliveredTaskID); err != nil {
		t.Fatalf("read delivered target task: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT id::text FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2
	`, issueID, secondAgentID).Scan(&missingTaskID); err != nil {
		t.Fatalf("read missing target task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		WITH delivered_task AS (
			UPDATE agent_task_queue
			SET status='completed', completed_at=now(), delivered_comment_ids=ARRAY[$2::uuid]
			WHERE id=$1
			RETURNING id, agent_id
		)
		INSERT INTO comment_trigger_delivery_receipt (comment_id, comment_trigger_revision, agent_id, task_id)
		SELECT source.id, source.trigger_revision, delivered_task.agent_id, delivered_task.id
		FROM delivered_task JOIN comment source ON source.id=$2::uuid
	`, deliveredTaskID, uuidToString(commentID)); err != nil {
		t.Fatalf("complete delivered target: %v", err)
	}
	if _, err := testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id=$1`, missingTaskID); err != nil {
		t.Fatalf("remove failed target attempt: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE comment_trigger_outbox
		SET state='processing', attempts=1, lease_owner='partial-worker',
		    lease_expires_at=now()-interval '1 second'
		WHERE comment_id=$1
	`, uuidToString(commentID)); err != nil {
		t.Fatalf("stage partial retry: %v", err)
	}

	result, err := testHandler.DeliverCommentTriggerOutbox(ctx, 10)
	if err != nil || result.Done != 1 || result.Failed != 0 {
		t.Fatalf("partial retry delivery = %+v, %v", result, err)
	}
	for _, agentID := range []string{firstAgentID, secondAgentID} {
		var count int
		if err := testPool.QueryRow(ctx, `
			SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2
		`, issueID, agentID).Scan(&count); err != nil {
			t.Fatalf("count target %s tasks: %v", agentID, err)
		}
		if count != 1 {
			t.Fatalf("target %s tasks = %d, want exactly 1", agentID, count)
		}
	}
}

func TestCommentTriggerOutboxExpiresTheLastCrashedLeaseToDead(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID := createCommentTriggerPreviewIssue(t, "exhausted trigger lease", "", "")
	commentID := dbid.NewV7()
	if _, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID: commentID, IssueID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
		AuthorType: "member", AuthorID: parseUUID(testUserID), Content: "/note exhausted", Type: "comment",
	}); err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if err := testHandler.Queries.CreateCommentTriggerOutbox(ctx, db.CreateCommentTriggerOutboxParams{
		CommentID: commentID, WorkspaceID: parseUUID(testWorkspaceID), IssueID: parseUUID(issueID),
		ActorType: "member", ActorID: parseUUID(testUserID),
		OriginatorUserID: parseOptionalUUID(testUserID), SuppressAgentIds: []pgtype.UUID{},
	}); err != nil {
		t.Fatalf("create trigger outbox: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE comment_trigger_outbox
		SET state='processing', attempts=$2, lease_owner='last-worker',
		    lease_expires_at=now()-interval '1 second'
		WHERE comment_id=$1
	`, uuidToString(commentID), commentTriggerOutboxMaxAttempts); err != nil {
		t.Fatalf("stage exhausted outbox: %v", err)
	}

	result, err := testHandler.DeliverCommentTriggerOutbox(ctx, 10)
	if err != nil || result.Claimed != 0 {
		t.Fatalf("expired delivery = %+v, %v", result, err)
	}
	var state, lastError string
	if err := testPool.QueryRow(ctx, `
		SELECT state, last_error FROM comment_trigger_outbox WHERE comment_id=$1
	`, uuidToString(commentID)).Scan(&state, &lastError); err != nil {
		t.Fatalf("read exhausted outbox: %v", err)
	}
	if state != "dead" || lastError == "" {
		t.Fatalf("exhausted outbox = (%q, %q), want visible dead letter", state, lastError)
	}
}

func TestEditedCommentRevisionTriggersExactlyOnce(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, priorState := range []string{"completed", "running"} {
		t.Run(priorState, func(t *testing.T) {
			ctx := context.Background()
			agentID := createHandlerTestAgent(t, "Edited Revision "+priorState, nil)
			issueID := createCommentTriggerPreviewIssue(t, "edited revision "+priorState, "", "")
			commentID := dbid.NewV7()
			contentV1 := fmt.Sprintf("[@Target](mention://agent/%s) version one", agentID)
			created, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
				ID: commentID, IssueID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
				AuthorType: "member", AuthorID: parseUUID(testUserID), Content: contentV1, Type: "comment",
			})
			if err != nil {
				t.Fatalf("create version one comment: %v", err)
			}
			if err := testHandler.Queries.CreateCommentTriggerOutbox(ctx, db.CreateCommentTriggerOutboxParams{
				CommentID: commentID, WorkspaceID: parseUUID(testWorkspaceID), IssueID: parseUUID(issueID),
				ActorType: "member", ActorID: parseUUID(testUserID), OriginatorUserID: parseOptionalUUID(testUserID),
				SuppressAgentIds: []pgtype.UUID{}, CommentTriggerRevision: created.TriggerRevision,
			}); err != nil {
				t.Fatalf("create version one outbox: %v", err)
			}
			if _, err := testHandler.processCommentTriggerOutbox(ctx, commentID); err != nil {
				t.Fatalf("deliver version one: %v", err)
			}

			var firstTaskID string
			if err := testPool.QueryRow(ctx, `
				SELECT id::text FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2
			`, issueID, agentID).Scan(&firstTaskID); err != nil {
				t.Fatalf("load version one task: %v", err)
			}
			switch priorState {
			case "completed":
				if _, err := testPool.Exec(ctx, `
					WITH delivered_task AS (
						UPDATE agent_task_queue
						SET status='completed', dispatched_at=now()-interval '2 seconds',
						    started_at=now()-interval '1 second', completed_at=now(),
						    delivered_comment_ids=ARRAY[$2::uuid]
						WHERE id=$1
						RETURNING id, agent_id
					)
					INSERT INTO comment_trigger_delivery_receipt (comment_id, comment_trigger_revision, agent_id, task_id)
					SELECT source.id, source.trigger_revision, delivered_task.agent_id, delivered_task.id
					FROM delivered_task JOIN comment source ON source.id=$2::uuid
				`, firstTaskID, uuidToString(commentID)); err != nil {
					t.Fatalf("complete version one task: %v", err)
				}
			case "running":
				if _, err := testPool.Exec(ctx, `
					UPDATE agent_task_queue
					SET status='running', dispatched_at=now(), started_at=now()
					WHERE id=$1
				`, firstTaskID); err != nil {
					t.Fatalf("run version one task: %v", err)
				}
			}

			contentV2 := fmt.Sprintf("[@Target](mention://agent/%s) version two", agentID)
			update := func() *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				req := withURLParam(newRequest(http.MethodPut, "/api/comments/"+uuidToString(commentID), map[string]any{
					"content": contentV2, "expected_revision": int64(1),
				}), "commentId", uuidToString(commentID))
				testHandler.UpdateComment(w, req)
				return w
			}
			if w := update(); w.Code != http.StatusOK {
				t.Fatalf("edit to version two = %d: %s", w.Code, w.Body.String())
			}
			if w := update(); w.Code != http.StatusConflict {
				t.Fatalf("network replay = %d, want 409: %s", w.Code, w.Body.String())
			}

			var total, active, cancelled int
			if err := testPool.QueryRow(ctx, `
				SELECT count(*),
				       count(*) FILTER (WHERE status IN ('queued','dispatched','running','waiting_local_directory','deferred')),
				       count(*) FILTER (WHERE status='cancelled')
				FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2
			`, issueID, agentID).Scan(&total, &active, &cancelled); err != nil {
				t.Fatalf("count revision tasks: %v", err)
			}
			if total != 2 || active != 1 {
				t.Fatalf("revision tasks = total:%d active:%d, want 2/1", total, active)
			}
			if priorState == "running" && cancelled != 1 {
				t.Fatalf("running version one cancellations = %d, want 1", cancelled)
			}
			var outboxRevision int64
			var outboxState string
			if err := testPool.QueryRow(ctx, `
				SELECT comment_trigger_revision, state FROM comment_trigger_outbox WHERE comment_id=$1
			`, uuidToString(commentID)).Scan(&outboxRevision, &outboxState); err != nil {
				t.Fatalf("load version two outbox: %v", err)
			}
			if outboxRevision != 2 || outboxState != "done" {
				t.Fatalf("version two outbox = revision:%d state:%s, want 2/done", outboxRevision, outboxState)
			}
		})
	}
}

func TestCommentTriggerOutboxFencePreventsExpiredConsumerTakeover(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "Lease Fence Target", nil)
	issueID := createCommentTriggerPreviewIssue(t, "lease fence", "", "")
	commentID := dbid.NewV7()
	created, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID: commentID, IssueID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
		AuthorType: "member", AuthorID: parseUUID(testUserID),
		Content: fmt.Sprintf("[@Target](mention://agent/%s) fenced", agentID), Type: "comment",
	})
	if err != nil {
		t.Fatalf("create fenced comment: %v", err)
	}
	if err := testHandler.Queries.CreateCommentTriggerOutbox(ctx, db.CreateCommentTriggerOutboxParams{
		CommentID: commentID, WorkspaceID: parseUUID(testWorkspaceID), IssueID: parseUUID(issueID),
		ActorType: "member", ActorID: parseUUID(testUserID), OriginatorUserID: parseOptionalUUID(testUserID),
		SuppressAgentIds: []pgtype.UUID{}, CommentTriggerRevision: created.TriggerRevision,
	}); err != nil {
		t.Fatalf("create fenced outbox: %v", err)
	}

	functionName := "test_pause_comment_trigger_insert"
	triggerName := "test_pause_comment_trigger_insert"
	if _, err := testPool.Exec(ctx, fmt.Sprintf(`
		CREATE OR REPLACE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM pg_sleep(4);
			RETURN NEW;
		END $$;
		DROP TRIGGER IF EXISTS %s ON agent_task_queue;
		CREATE TRIGGER %s BEFORE INSERT ON agent_task_queue
		FOR EACH ROW WHEN (NEW.trigger_comment_id = '%s'::uuid)
		EXECUTE FUNCTION %s();
	`, functionName, triggerName, triggerName, uuidToString(commentID), functionName)); err != nil {
		t.Fatalf("install pause trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON agent_task_queue", triggerName))
		_, _ = testPool.Exec(context.Background(), fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName))
	})

	ownerA := "lease-owner-a"
	row, err := testHandler.Queries.ClaimCommentTriggerOutboxByComment(ctx, db.ClaimCommentTriggerOutboxByCommentParams{
		LeaseOwner: pgtype.Text{String: ownerA, Valid: true}, LeaseSeconds: 2,
		MaxAttempts: commentTriggerOutboxMaxAttempts, CommentID: commentID,
	})
	if err != nil {
		t.Fatalf("claim consumer A: %v", err)
	}
	aDone := make(chan error, 1)
	go func() {
		_, deliverErr := testHandler.deliverClaimedCommentTriggerOutbox(ctx, row, ownerA)
		aDone <- deliverErr
	}()

	// A is sleeping inside the task INSERT after its coverage check while its
	// transaction still owns the outbox row lock. Wait beyond the nominal lease,
	// then prove B cannot take over using the expired timestamp.
	time.Sleep(2500 * time.Millisecond)
	bCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, bErr := testHandler.Queries.ClaimCommentTriggerOutboxByComment(bCtx, db.ClaimCommentTriggerOutboxByCommentParams{
		LeaseOwner: pgtype.Text{String: "lease-owner-b", Valid: true}, LeaseSeconds: 60,
		MaxAttempts: commentTriggerOutboxMaxAttempts, CommentID: commentID,
	})
	if !errors.Is(bErr, pgx.ErrNoRows) {
		t.Fatalf("consumer B claim after A commit = %v, want pgx.ErrNoRows", bErr)
	}
	if err := <-aDone; err != nil {
		t.Fatalf("consumer A delivery: %v", err)
	}
	var taskCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2
	`, issueID, agentID).Scan(&taskCount); err != nil {
		t.Fatalf("count fenced tasks: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("fenced task count = %d, want 1", taskCount)
	}
}

type lockTimeoutTxStarter struct {
	base txStarter
}

func (s lockTimeoutTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.base.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '100ms'`); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func TestLifeOSRouteQueryFailureKeepsCommentTriggerRetryable(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	enableLifeOSLocalModeForTest(t)
	ceoID := createHandlerTestAgent(t, localLifeOSCEOAgentName, nil)
	issueID := createCommentTriggerPreviewIssue(t, "LifeOS retryable route query", "member", testUserID)
	commentID := dbid.NewV7()
	created, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID: commentID, IssueID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
		AuthorType: "member", AuthorID: parseUUID(testUserID), Content: "请继续处理", Type: "comment",
	})
	if err != nil {
		t.Fatalf("create LifeOS route comment: %v", err)
	}
	if err := testHandler.Queries.CreateCommentTriggerOutbox(ctx, db.CreateCommentTriggerOutboxParams{
		CommentID: commentID, WorkspaceID: parseUUID(testWorkspaceID), IssueID: parseUUID(issueID),
		ActorType: "member", ActorID: parseUUID(testUserID), OriginatorUserID: parseOptionalUUID(testUserID),
		SuppressAgentIds: []pgtype.UUID{}, CommentTriggerRevision: created.TriggerRevision,
	}); err != nil {
		t.Fatalf("create LifeOS route outbox: %v", err)
	}

	blocker, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin route blocker: %v", err)
	}
	if _, err := blocker.Exec(ctx, `LOCK TABLE agent_task_queue IN ACCESS EXCLUSIVE MODE`); err != nil {
		_ = blocker.Rollback(ctx)
		t.Fatalf("lock task queue: %v", err)
	}
	faulting := *testHandler
	faulting.TxStarter = lockTimeoutTxStarter{base: testHandler.TxStarter}
	if _, err := faulting.processCommentTriggerOutbox(ctx, commentID); err == nil {
		_ = blocker.Rollback(ctx)
		t.Fatal("route query failure was swallowed")
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatalf("release route blocker: %v", err)
	}

	var state string
	if err := testPool.QueryRow(ctx, `
		SELECT state FROM comment_trigger_outbox WHERE comment_id=$1
	`, uuidToString(commentID)).Scan(&state); err != nil {
		t.Fatalf("read retryable route outbox: %v", err)
	}
	if state != "pending" {
		t.Fatalf("route failure outbox state = %s, want pending", state)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE comment_trigger_outbox SET next_attempt_at=now() WHERE comment_id=$1
	`, uuidToString(commentID)); err != nil {
		t.Fatalf("make route retry due: %v", err)
	}
	if _, err := testHandler.processCommentTriggerOutbox(ctx, commentID); err != nil {
		t.Fatalf("recover route delivery: %v", err)
	}
	if got := countQueuedCommentTriggerTasks(t, issueID, ceoID); got != 1 {
		t.Fatalf("recovered AI 星耀 tasks = %d, want 1", got)
	}
}

// TestCreateComment_MixedMentionSurfacesPartialTriggerOutcomes is the MUL-4525 §2
// acceptance test for Bohan's exact scenario: a comment @mentions an agent the
// author can invoke AND a squad whose private leader they cannot. The comment is
// still saved (one blocked mention must not reject it), and the response carries
// per-target outcomes — queued for the allowed agent, blocked +
// invocation_not_allowed for the squad — so the client can show partial success
// instead of a silent no-op. The preview surfaces the same split before sending.
func TestCreateComment_MixedMentionSurfacesPartialTriggerOutcomes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	allowedAgentID := createHandlerTestAgent(t, "Outcome Allowed Agent", nil)
	// A private leader owned by someone other than testUserID: the workspace
	// owner can VIEW it but cannot INVOKE it (no admin bypass).
	privateLeaderID, _, _ := privateAgentTestFixture(t)
	squadID := createCommentTriggerPreviewSquad(t, "Outcome Private Squad", privateLeaderID)
	issueID := createCommentTriggerPreviewIssue(t, "mixed mention partial outcomes", "", "")

	content := fmt.Sprintf(
		"[@Allowed](mention://agent/%s) [@Squad](mention://squad/%s) please take a look",
		allowedAgentID, squadID,
	)

	// Preview surfaces both the allowed agent and the blocked squad.
	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": content})
	requirePreviewAgents(t, preview, allowedAgentID)
	if len(preview.Blocked) != 1 {
		t.Fatalf("preview blocked = %+v, want 1 entry", preview.Blocked)
	}
	if b := preview.Blocked[0]; b.TargetType != "squad" || b.TargetID != squadID ||
		b.Status != DispatchBlocked || b.ReasonCode != ReasonInvocationNotAllowed {
		t.Fatalf("preview blocked[0] = %+v, want squad %s blocked/invocation_not_allowed", b, squadID)
	}

	// Create the comment: it must save (201) and report partial outcomes.
	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": content}), "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201 (comment must save despite blocked mention), got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	if resp.ID == "" {
		t.Fatal("comment was not saved")
	}
	if len(resp.TriggerOutcomes) != 2 {
		t.Fatalf("trigger_outcomes = %+v, want 2 (one queued, one blocked)", resp.TriggerOutcomes)
	}

	allowed := findCommentOutcome(t, resp.TriggerOutcomes, allowedAgentID)
	if allowed.TargetType != "agent" || allowed.Status != DispatchQueued {
		t.Errorf("allowed outcome = %+v, want agent/queued", allowed)
	}
	blocked := findCommentOutcome(t, resp.TriggerOutcomes, squadID)
	if blocked.TargetType != "squad" || blocked.Status != DispatchBlocked || blocked.ReasonCode != ReasonInvocationNotAllowed {
		t.Errorf("blocked outcome = %+v, want squad/blocked/invocation_not_allowed", blocked)
	}

	// The allowed agent ran; the private leader was never enqueued.
	if got := countQueuedCommentTriggerTasks(t, issueID, allowedAgentID); got != 1 {
		t.Errorf("allowed agent queued tasks = %d, want 1", got)
	}
	var leaderTasks int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, issueID, privateLeaderID).Scan(&leaderTasks); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if leaderTasks != 0 {
		t.Errorf("blocked private leader tasks = %d, want 0", leaderTasks)
	}
}

// TestCreateComment_BlockedMentionReasonDoesNotEnumeratePrivateAgent pins the
// enumeration-safety rule (MUL-4525 §2): a mention the author cannot invoke and a
// mention of a truly nonexistent agent both return the same generic
// invocation_not_allowed, so a blocked reason can never confirm a private
// agent's existence.
func TestCreateComment_BlockedMentionReasonDoesNotEnumeratePrivateAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	privateAgentID, _, _ := privateAgentTestFixture(t)
	issueID := createCommentTriggerPreviewIssue(t, "blocked mention enumeration safety", "", "")
	nonexistentID := "00000000-0000-0000-0000-0000000000ff"

	content := fmt.Sprintf(
		"[@Private](mention://agent/%s) [@Ghost](mention://agent/%s) ping",
		privateAgentID, nonexistentID,
	)
	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": content})
	if len(preview.Agents) != 0 {
		t.Fatalf("preview agents = %+v, want none", preview.Agents)
	}
	if len(preview.Blocked) != 2 {
		t.Fatalf("preview blocked = %+v, want 2", preview.Blocked)
	}
	for _, b := range preview.Blocked {
		if b.ReasonCode != ReasonInvocationNotAllowed {
			t.Errorf("blocked %s reason = %q, want invocation_not_allowed (must not distinguish private-exists from not-found)", b.TargetID, b.ReasonCode)
		}
	}
}

// TestCreateComment_AgentAndSameLeaderSquad is Elon's round-3 must-fix 1
// acceptance test: when a comment names BOTH @Agent A and @Squad S whose leader
// is A, the run coalesces to ONE task that carries the LEADER role
// (is_leader_task + squad_id=S, so the daemon injects S's briefing) regardless
// of mention order, and each explicitly-named target still gets its own outcome
// (MUL-4525). The old first-mention-wins dedup could drop the leader role when
// @Agent A came first — this asserts the role independent of order.
func TestCreateComment_AgentAndSameLeaderSquad(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	cases := []struct {
		name    string
		content func(agentID, squadID string) string
	}{
		{"agent mention first", func(a, s string) string {
			return fmt.Sprintf("[@A](mention://agent/%s) [@S](mention://squad/%s) please", a, s)
		}},
		{"squad mention first", func(a, s string) string {
			return fmt.Sprintf("[@S](mention://squad/%s) [@A](mention://agent/%s) please", s, a)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agentID := createHandlerTestAgent(t, "Shared Leader Agent "+tc.name, nil)
			squadID := createCommentTriggerPreviewSquad(t, "Shared Leader Squad "+tc.name, agentID)
			issueID := createCommentTriggerPreviewIssue(t, "same-leader "+tc.name, "", "")

			w := httptest.NewRecorder()
			r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": tc.content(agentID, squadID)}), "id", issueID)
			testHandler.CreateComment(w, r)
			if w.Code != http.StatusCreated {
				t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
			}
			var resp CommentResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode comment: %v", err)
			}

			// One coalesced execution carrying the leader role for squad S.
			var taskCount int
			var isLeader bool
			var taskSquadID string
			if err := testPool.QueryRow(ctx, `
				SELECT count(*), COALESCE(bool_or(is_leader_task), false), COALESCE(max(squad_id::text), '')
				FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
			`, issueID, agentID).Scan(&taskCount, &isLeader, &taskSquadID); err != nil {
				t.Fatalf("read task role: %v", err)
			}
			if taskCount != 1 {
				t.Fatalf("queued tasks = %d, want 1 (coalesced execution)", taskCount)
			}
			if !isLeader {
				t.Errorf("execution is_leader_task = false, want true (leader role must win regardless of order)")
			}
			if taskSquadID != squadID {
				t.Errorf("execution squad_id = %q, want %q", taskSquadID, squadID)
			}

			// Two outcomes — one per explicitly-named target — both queued.
			if len(resp.TriggerOutcomes) != 2 {
				t.Fatalf("trigger_outcomes = %+v, want 2 (agent + squad)", resp.TriggerOutcomes)
			}
			if o := findCommentOutcome(t, resp.TriggerOutcomes, agentID); o.TargetType != "agent" || o.Status != DispatchQueued {
				t.Errorf("agent outcome = %+v, want agent/queued", o)
			}
			if o := findCommentOutcome(t, resp.TriggerOutcomes, squadID); o.TargetType != "squad" || o.Status != DispatchQueued {
				t.Errorf("squad outcome = %+v, want squad/queued", o)
			}
		})
	}
}

// TestCreateComment_TwoSquadsSharingLeaderCoalescesNonWinner is Elon's round-3
// must-fix 1 (multi-squad case): two DIFFERENT squads share the same leader and
// both are @mentioned. The single leader agent runs ONCE carrying one squad's
// context; the other squad's mention folds into that run and is reported
// coalesced — never a second task, and never both reported queued (MUL-4525).
func TestCreateComment_TwoSquadsSharingLeaderCoalescesNonWinner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Two-Squad Shared Leader", nil)
	squad1 := createCommentTriggerPreviewSquad(t, "Two-Squad S1", leaderID)
	squad2 := createCommentTriggerPreviewSquad(t, "Two-Squad S2", leaderID)
	issueID := createCommentTriggerPreviewIssue(t, "two squads sharing leader", "", "")
	content := fmt.Sprintf("[@S1](mention://squad/%s) [@S2](mention://squad/%s) please", squad1, squad2)

	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": content}), "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode comment: %v", err)
	}

	// Exactly one queued task (a single leader agent can only run once).
	if got := countQueuedCommentTriggerTasks(t, issueID, leaderID); got != 1 {
		t.Fatalf("queued tasks = %d, want 1", got)
	}
	// Two squad outcomes; exactly one queued (the executed squad) and one
	// coalesced (the folded squad) — never both queued.
	if len(resp.TriggerOutcomes) != 2 {
		t.Fatalf("trigger_outcomes = %+v, want 2", resp.TriggerOutcomes)
	}
	statuses := map[DispatchStatus]int{}
	for _, o := range resp.TriggerOutcomes {
		if o.TargetType != "squad" {
			t.Errorf("outcome %+v: want squad target", o)
		}
		statuses[o.Status]++
	}
	if statuses[DispatchQueued] != 1 || statuses[DispatchCoalesced] != 1 {
		t.Errorf("outcome statuses = %v, want exactly 1 queued + 1 coalesced (not both queued)", statuses)
	}
}

// TestCreateComment_SquadLeaderSelfMentionCompletedTaskDoesNotFakeSuccess is
// Elon's round-3 must-fix 2: a squad leader's own @mention of its squad is
// suppressed by the self-trigger guard, but when its latest task is already
// TERMINAL (no active run), the outcome must NOT be a success-shaped `deferred`
// — it is a non-success `blocked/self_trigger_suppressed`, and no new task runs.
func TestCreateComment_SquadLeaderSelfMentionCompletedTaskDoesNotFakeSuccess(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	leaderID := createHandlerTestAgent(t, "Self-Mention Leader", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Self-Mention Squad", leaderID)
	issueID := createCommentTriggerPreviewIssue(t, "self-mention completed task", "", "")

	// The leader's latest task on this issue is a COMPLETED leader task: the
	// self-trigger guard suppresses (latest role is leader) AND there is no
	// active run to cover the comment.
	var completedTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, is_leader_task, squad_id, started_at, completed_at)
		VALUES ($1, $2, 'completed', 0, $3, true, $4, now(), now())
		RETURNING id
	`, leaderID, handlerTestRuntimeID(t), issueID, squadID).Scan(&completedTaskID); err != nil {
		t.Fatalf("seed completed leader task: %v", err)
	}

	content := fmt.Sprintf("[@S](mention://squad/%s) revisit please", squadID)
	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": content}), "id", issueID)
	// Author the comment AS the leader agent (A2A self-mention).
	r.Header.Set("X-Agent-ID", leaderID)
	r.Header.Set("X-Task-ID", completedTaskID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode comment: %v", err)
	}

	// The self-mention neither re-fired the leader nor is covered by an active
	// run: the outcome is non-success, never a fake `deferred`.
	if len(resp.TriggerOutcomes) != 1 {
		t.Fatalf("trigger_outcomes = %+v, want 1", resp.TriggerOutcomes)
	}
	o := resp.TriggerOutcomes[0]
	if o.TargetType != "squad" || o.TargetID != squadID {
		t.Fatalf("outcome target = %+v, want squad %s", o, squadID)
	}
	if o.Status != DispatchBlocked || o.ReasonCode != ReasonSelfTriggerSuppressed {
		t.Errorf("outcome = %+v, want blocked/self_trigger_suppressed (must not fake success)", o)
	}
	// No new task was enqueued (only the pre-seeded completed one exists).
	var total int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, issueID, leaderID).Scan(&total); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if total != 1 {
		t.Errorf("leader tasks = %d, want 1 (self-mention suppressed, no new task)", total)
	}
}
