package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type delegatedFailureFixture struct {
	pool          *pgxpool.Pool
	workspaceID   string
	userID        string
	issueID       string
	workerIssue   string
	runtimeID     string
	coordinator   string
	worker        string
	sourceTrigger string
	sourceTask    string
}

func seedDelegatedFailureFixture(t *testing.T) (*delegatedFailureFixture, *TaskService) {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	workspaceID, userID, coordinatorID, issueID := seedAttributionFixture(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE issue SET status = 'in_progress' WHERE id = $1`, issueID); err != nil {
		t.Fatalf("activate source issue: %v", err)
	}

	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, coordinatorID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	var workerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility,
			max_concurrent_tasks, owner_id, instructions, custom_env, custom_args
		)
		VALUES ($1, 'delegated-worker', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id`, workspaceID, runtimeID, userID).Scan(&workerID); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	var workerIssueID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, creator_type, creator_id, assignee_type, assignee_id,
			priority, parent_issue_id, number
		)
		VALUES ($1, 'delegated worker issue', 'member', $2, 'agent', $3, 'medium', $4, 2)
		RETURNING id`, workspaceID, userID, workerID, issueID).Scan(&workerIssueID); err != nil {
		t.Fatalf("seed worker issue: %v", err)
	}

	var sourceTriggerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'coordinate delegated work')
		RETURNING id`, workspaceID, issueID, userID).Scan(&sourceTriggerID); err != nil {
		t.Fatalf("seed source trigger comment: %v", err)
	}

	var sourceTaskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			trigger_comment_id, originator_user_id, accountable_user_id, originator_source
		)
		VALUES ($1, $2, $3, 'completed', 0, $4, $5, $5, 'direct_human')
		RETURNING id`, coordinatorID, runtimeID, issueID, sourceTriggerID, userID).Scan(&sourceTaskID); err != nil {
		t.Fatalf("seed source task: %v", err)
	}

	return &delegatedFailureFixture{
		pool:          pool,
		workspaceID:   workspaceID,
		userID:        userID,
		issueID:       issueID,
		workerIssue:   workerIssueID,
		runtimeID:     runtimeID,
		coordinator:   coordinatorID,
		worker:        workerID,
		sourceTrigger: sourceTriggerID,
		sourceTask:    sourceTaskID,
	}, NewTaskService(db.New(pool), pool, nil, events.New())
}

func (f *delegatedFailureFixture) insertWorkerTask(t *testing.T, status, evidenceKind string, attempt, maxAttempts int32) pgtype.UUID {
	t.Helper()
	var taskID pgtype.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, attempt, max_attempts,
			originator_user_id, accountable_user_id, originator_source,
			delegated_from_task_id, trigger_evidence_kind
		)
		VALUES ($1, $2, $3, $4, 0, $5, $6, $7, $7, 'delegation', $8, NULLIF($9, ''))
		RETURNING id`, f.worker, f.runtimeID, f.workerIssue, status, attempt, maxAttempts, f.userID, f.sourceTask, evidenceKind).Scan(&taskID); err != nil {
		t.Fatalf("seed worker task: %v", err)
	}
	return taskID
}

func TestFailTaskFinalDelegatedFailureWakesCoordinatorOnce(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	failedID := f.insertWorkerTask(t, "running", "comment", 1, 2)
	secret := "sk-" + strings.Repeat("a", 24)

	failed, err := svc.FailTask(ctx, failedID, "upstream capacity exhausted "+secret, "", "", "", "agent_error.process_failure", false, "", "")
	if err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	var recoveryCount int
	var recoveryAgent, recoveryIssue, evidenceRef, delegatedFrom string
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*), COALESCE(max(agent_id::text), ''), COALESCE(max(issue_id::text), ''),
		       COALESCE(max(trigger_evidence_ref_id::text), ''), COALESCE(max(delegated_from_task_id::text), '')
		FROM agent_task_queue
		WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID).
		Scan(&recoveryCount, &recoveryAgent, &recoveryIssue, &evidenceRef, &delegatedFrom); err != nil {
		t.Fatalf("read recovery task: %v", err)
	}
	if recoveryCount != 1 {
		t.Fatalf("recovery task count = %d, want 1", recoveryCount)
	}
	if recoveryAgent != f.coordinator || recoveryIssue != f.issueID {
		t.Fatalf("recovery target = agent %s issue %s, want %s/%s", recoveryAgent, recoveryIssue, f.coordinator, f.issueID)
	}
	if evidenceRef != util.UUIDToString(failedID) || delegatedFrom != util.UUIDToString(failedID) {
		t.Fatalf("recovery lineage = evidence %s delegated_from %s, want failed task %s", evidenceRef, delegatedFrom, util.UUIDToString(failedID))
	}
	var retryCount int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1`, failedID).Scan(&retryCount); err != nil {
		t.Fatalf("count process-failure retries: %v", err)
	}
	if retryCount != 0 {
		t.Fatalf("process-failure retry count = %d, want 0", retryCount)
	}

	var commentCount int
	var content, parentID string
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*), COALESCE(max(content), ''), COALESCE(max(parent_id::text), '') FROM comment
		WHERE issue_id = $1 AND author_type = 'system' AND type = 'progress_update' AND source_task_id = $2`, f.issueID, failedID).
		Scan(&commentCount, &content, &parentID); err != nil {
		t.Fatalf("read recovery comment: %v", err)
	}
	if commentCount != 1 {
		t.Fatalf("recovery comment count = %d, want 1", commentCount)
	}
	if parentID != f.sourceTrigger {
		t.Fatalf("recovery comment parent_id = %q, want source trigger %s", parentID, f.sourceTrigger)
	}
	if strings.Contains(content, secret) || !strings.Contains(content, "[REDACTED API KEY]") {
		t.Fatalf("recovery comment did not redact error: %q", content)
	}
	var failedIssueComments int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM comment
		WHERE issue_id = $1 AND type = 'system' AND source_task_id = $2`, f.workerIssue, failedID).Scan(&failedIssueComments); err != nil {
		t.Fatalf("count failed-issue comments: %v", err)
	}
	if failedIssueComments != 1 {
		t.Fatalf("failed-issue comments = %d, want legacy failure comment preserved", failedIssueComments)
	}

	// Replaying the sweeper/direct-failure side effect for the same terminal row
	// must not create another comment or another coordinator run.
	if handled, err := svc.recoverDelegatedTaskFailure(ctx, *failed); err != nil || !handled {
		t.Fatalf("repeat recovery = handled %v err %v, want handled without error", handled, err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1 AND type = 'progress_update' AND source_task_id = $2`, f.issueID, failedID).Scan(&commentCount); err != nil {
		t.Fatalf("recount recovery comments: %v", err)
	}
	if commentCount != 1 {
		t.Fatalf("replayed recovery comment count = %d, want 1", commentCount)
	}
	if _, err := f.pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'failed', completed_at = now(), failure_reason = 'agent_error.process_failure',
		    delivered_comment_ids = ARRAY[(
		        SELECT id FROM comment
		        WHERE type = 'progress_update' AND source_task_id = $1
		        LIMIT 1
		    )]::uuid[]
		WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID); err != nil {
		t.Fatalf("fail recovery task: %v", err)
	}
	if handled, err := svc.recoverDelegatedTaskFailure(ctx, *failed); err != nil || !handled {
		t.Fatalf("recovery after terminal recovery task = handled %v err %v", handled, err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID).Scan(&recoveryCount); err != nil {
		t.Fatalf("recount recovery tasks: %v", err)
	}
	if recoveryCount != 1 {
		t.Fatalf("delivered terminal recovery was recreated: task count = %d, want 1", recoveryCount)
	}
}

func TestPendingDelegatedFailureSweepRepairsCommittedCommentWithoutTask(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	failedID := f.insertWorkerTask(t, "failed", "comment", 1, 2)
	if _, err := f.pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET failure_reason = 'agent_error.process_failure', error = 'worker exited', completed_at = now()
		WHERE id = $1`, failedID); err != nil {
		t.Fatalf("stamp failed task: %v", err)
	}
	// The durable outbox starts at the explicit platform comment. Do not
	// retroactively wake arbitrary historical delegated failures that predate
	// this recovery mechanism.
	if result, err := svc.RecoverPendingDelegatedFailures(ctx, 100); err != nil || result != (DelegatedFailureRecoverySweepResult{}) {
		t.Fatalf("pre-comment recovery sweep = %+v, %v; want zero result, nil", result, err)
	}

	// This is the durable intermediate state left when comment creation commits
	// but the first coordinator dispatch fails or the server exits immediately.
	target, created, err := svc.ensureDelegatedFailureRecoveryComment(ctx, failedID)
	if err != nil || target == nil || !created {
		t.Fatalf("ensure recovery comment = target %v created %v err %v", target != nil, created, err)
	}
	var tasksBefore int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID).Scan(&tasksBefore); err != nil {
		t.Fatalf("count recovery tasks before sweep: %v", err)
	}
	if tasksBefore != 0 {
		t.Fatalf("recovery tasks before sweep = %d, want 0", tasksBefore)
	}

	result, err := svc.RecoverPendingDelegatedFailures(ctx, 100)
	if err != nil || result.Replayed != 1 || result.Exhausted != 0 {
		t.Fatalf("RecoverPendingDelegatedFailures = %+v, %v; want one replay, nil", result, err)
	}
	var comments, tasks int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE type = 'progress_update' AND source_task_id = $1`, failedID).Scan(&comments); err != nil {
		t.Fatalf("count recovery comments: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID).Scan(&tasks); err != nil {
		t.Fatalf("count recovery tasks after sweep: %v", err)
	}
	if comments != 1 || tasks != 1 {
		t.Fatalf("comments/tasks after sweep = %d/%d, want 1/1", comments, tasks)
	}
	if result, err := svc.RecoverPendingDelegatedFailures(ctx, 100); err != nil || result != (DelegatedFailureRecoverySweepResult{}) {
		t.Fatalf("second recovery sweep = %+v, %v; want zero result, nil", result, err)
	}
}

func TestPendingDelegatedFailureSweepRequeuesTerminalUndeliveredTask(t *testing.T) {
	for _, terminalStatus := range []string{"failed", "cancelled"} {
		t.Run(terminalStatus, func(t *testing.T) {
			f, svc := seedDelegatedFailureFixture(t)
			ctx := context.Background()
			failedID := f.insertWorkerTask(t, "failed", "comment", 1, 2)
			if _, err := f.pool.Exec(ctx, `
				UPDATE agent_task_queue
				SET failure_reason = 'agent_error.process_failure', error = 'worker exited', completed_at = now()
				WHERE id = $1`, failedID); err != nil {
				t.Fatalf("stamp failed task: %v", err)
			}
			failed, err := svc.Queries.GetAgentTask(ctx, failedID)
			if err != nil {
				t.Fatalf("load failed task: %v", err)
			}
			if handled, err := svc.recoverDelegatedTaskFailure(ctx, failed); err != nil || !handled {
				t.Fatalf("initial recovery = handled %v err %v", handled, err)
			}

			var firstRecoveryID pgtype.UUID
			if err := f.pool.QueryRow(ctx, `
				SELECT id FROM agent_task_queue
				WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID).Scan(&firstRecoveryID); err != nil {
				t.Fatalf("load first recovery task: %v", err)
			}
			if terminalStatus == "cancelled" {
				if _, err := svc.CancelTask(ctx, firstRecoveryID); err != nil {
					t.Fatalf("server-cancel recovery before delivery: %v", err)
				}
			} else if _, err := f.pool.Exec(ctx, `
				UPDATE agent_task_queue
				SET status = $2, completed_at = now(), delivered_comment_ids = '{}'
				WHERE id = $1`, firstRecoveryID, terminalStatus); err != nil {
				t.Fatalf("terminalize recovery before delivery: %v", err)
			}

			result, err := svc.RecoverPendingDelegatedFailures(ctx, 100)
			if err != nil || result.Replayed != 1 || result.Exhausted != 0 {
				t.Fatalf("RecoverPendingDelegatedFailures = %+v, %v; want one replay, nil", result, err)
			}
			var active, total int
			if err := f.pool.QueryRow(ctx, `
				SELECT count(*) FILTER (WHERE status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')),
				       count(*)
				FROM agent_task_queue
				WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID).Scan(&active, &total); err != nil {
				t.Fatalf("count recovery tasks: %v", err)
			}
			if active != 1 || total != 2 {
				t.Fatalf("active/total recovery tasks = %d/%d, want 1/2", active, total)
			}
			if result, err := svc.RecoverPendingDelegatedFailures(ctx, 100); err != nil || result != (DelegatedFailureRecoverySweepResult{}) {
				t.Fatalf("second recovery sweep = %+v, %v; want zero result, nil", result, err)
			}
		})
	}
}

func TestUserCancelledDelegatedFailureRecoveryStaysCancelled(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	failedID := f.insertWorkerTask(t, "failed", "comment", 1, 2)
	if _, err := f.pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET failure_reason = 'agent_error.process_failure', error = 'worker exited', completed_at = now()
		WHERE id = $1`, failedID); err != nil {
		t.Fatalf("stamp failed task: %v", err)
	}
	failed, err := svc.Queries.GetAgentTask(ctx, failedID)
	if err != nil {
		t.Fatalf("load failed task: %v", err)
	}
	if handled, err := svc.recoverDelegatedTaskFailure(ctx, failed); err != nil || !handled {
		t.Fatalf("initial recovery = handled %v err %v", handled, err)
	}

	var recoveryTaskID, recoveryCommentID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `
		SELECT task.id, recovery.id
		FROM agent_task_queue task
		JOIN comment recovery ON recovery.id = task.trigger_comment_id
		WHERE task.trigger_evidence_kind = 'delegated_failure'
		  AND task.trigger_evidence_ref_id = $1`, failedID).Scan(&recoveryTaskID, &recoveryCommentID); err != nil {
		t.Fatalf("load recovery task/comment: %v", err)
	}
	cancelled, err := svc.CancelTaskByUser(ctx, recoveryTaskID)
	if err != nil {
		t.Fatalf("CancelTaskByUser: %v", err)
	}
	if cancelled.Status != "cancelled" {
		t.Fatalf("cancelled status = %q, want cancelled", cancelled.Status)
	}

	var acknowledged bool
	if err := f.pool.QueryRow(ctx, `
		SELECT $2::uuid = ANY(delivered_comment_ids)
		FROM agent_task_queue WHERE id = $1`, recoveryTaskID, recoveryCommentID).Scan(&acknowledged); err != nil {
		t.Fatalf("read user-cancel acknowledgement: %v", err)
	}
	if !acknowledged {
		t.Fatal("user cancellation did not terminally acknowledge the recovery signal")
	}
	if result, err := svc.RecoverPendingDelegatedFailures(ctx, 100); err != nil || result != (DelegatedFailureRecoverySweepResult{}) {
		t.Fatalf("recovery sweep after user cancel = %+v, %v; want zero result, nil", result, err)
	}
	var taskCount int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID).Scan(&taskCount); err != nil {
		t.Fatalf("count recovery tasks: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("recovery tasks after user cancel = %d, want 1", taskCount)
	}
}

func TestManualRerunKeepsDelegatedFailureRecoveryReplayable(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	failedID := f.insertWorkerTask(t, "failed", "comment", 1, 2)
	if _, err := f.pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET failure_reason = 'agent_error.process_failure', error = 'worker exited', completed_at = now()
		WHERE id = $1`, failedID); err != nil {
		t.Fatalf("stamp failed task: %v", err)
	}
	failed, err := svc.Queries.GetAgentTask(ctx, failedID)
	if err != nil {
		t.Fatalf("load failed task: %v", err)
	}
	if handled, err := svc.recoverDelegatedTaskFailure(ctx, failed); err != nil || !handled {
		t.Fatalf("initial recovery = handled %v err %v", handled, err)
	}

	var firstRecoveryTaskID, recoveryCommentID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `
		SELECT task.id, recovery.id
		FROM agent_task_queue task
		JOIN comment recovery ON recovery.id = task.trigger_comment_id
		WHERE task.trigger_evidence_kind = 'delegated_failure'
		  AND task.trigger_evidence_ref_id = $1`, failedID).Scan(&firstRecoveryTaskID, &recoveryCommentID); err != nil {
		t.Fatalf("load initial recovery task/comment: %v", err)
	}

	rerun, err := svc.RerunIssue(
		ctx,
		util.MustParseUUID(f.issueID),
		util.MustParseUUID(f.sourceTask),
		pgtype.UUID{},
		util.MustParseUUID(f.userID),
		nil,
	)
	if err != nil {
		t.Fatalf("RerunIssue: %v", err)
	}
	var firstStatus string
	var acknowledged bool
	if err := f.pool.QueryRow(ctx, `
		SELECT status, $2::uuid = ANY(delivered_comment_ids)
		FROM agent_task_queue WHERE id = $1`, firstRecoveryTaskID, recoveryCommentID).Scan(&firstStatus, &acknowledged); err != nil {
		t.Fatalf("read rerun-cancelled recovery task: %v", err)
	}
	if firstStatus != "cancelled" || acknowledged {
		t.Fatalf("rerun-cancelled recovery = status %q acknowledged %v, want cancelled and replayable", firstStatus, acknowledged)
	}

	result, err := svc.RecoverPendingDelegatedFailures(ctx, 100)
	if err != nil || result.Replayed != 1 || result.Exhausted != 0 {
		t.Fatalf("recovery sweep after rerun = %+v, %v; want one replay", result, err)
	}
	var rerunTrigger string
	if err := f.pool.QueryRow(ctx, `SELECT COALESCE(trigger_comment_id::text, '') FROM agent_task_queue WHERE id = $1`, rerun.ID).Scan(&rerunTrigger); err != nil {
		t.Fatalf("read rerun trigger: %v", err)
	}
	if rerunTrigger != util.UUIDToString(recoveryCommentID) {
		t.Fatalf("rerun trigger = %q, want recovery comment %s", rerunTrigger, util.UUIDToString(recoveryCommentID))
	}
	var dedicatedRecoveryTasks int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID).Scan(&dedicatedRecoveryTasks); err != nil {
		t.Fatalf("count dedicated recovery tasks: %v", err)
	}
	if dedicatedRecoveryTasks != 1 {
		t.Fatalf("dedicated recovery task count after rerun = %d, want original cancelled task only", dedicatedRecoveryTasks)
	}
	if result, err := svc.RecoverPendingDelegatedFailures(ctx, 100); err != nil || result != (DelegatedFailureRecoverySweepResult{}) {
		t.Fatalf("second recovery sweep after rerun = %+v, %v; want zero result", result, err)
	}
}

func TestDelegatedFailureRecoveryStopsAfterBoundedUndeliveredAttempts(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	failedID := f.insertWorkerTask(t, "failed", "comment", 1, 2)
	if _, err := f.pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET failure_reason = 'agent_error.process_failure', error = 'worker exited', completed_at = now()
		WHERE id = $1`, failedID); err != nil {
		t.Fatalf("stamp failed task: %v", err)
	}
	failed, err := svc.Queries.GetAgentTask(ctx, failedID)
	if err != nil {
		t.Fatalf("load failed task: %v", err)
	}
	if handled, err := svc.recoverDelegatedTaskFailure(ctx, failed); err != nil || !handled {
		t.Fatalf("initial recovery = handled %v err %v", handled, err)
	}

	for attempt := 1; attempt <= delegatedFailureRecoveryMaxTaskAttempts; attempt++ {
		var currentTaskID pgtype.UUID
		if err := f.pool.QueryRow(ctx, `
			SELECT id FROM agent_task_queue
			WHERE trigger_evidence_kind = 'delegated_failure'
			  AND trigger_evidence_ref_id = $1
			  AND status = 'queued'
			ORDER BY created_at DESC, id DESC
			LIMIT 1`, failedID).Scan(&currentTaskID); err != nil {
			t.Fatalf("load recovery attempt %d: %v", attempt, err)
		}
		if _, err := f.pool.Exec(ctx, `
			UPDATE agent_task_queue
			SET status = 'failed', completed_at = now(), failure_reason = 'queued_expired',
			    error = 'task expired in queue', delivered_comment_ids = '{}'
			WHERE id = $1`, currentTaskID); err != nil {
			t.Fatalf("fail recovery attempt %d: %v", attempt, err)
		}

		result, err := svc.RecoverPendingDelegatedFailures(ctx, 100)
		if err != nil {
			t.Fatalf("recovery sweep after attempt %d = %+v, %v", attempt, result, err)
		}
		if attempt < delegatedFailureRecoveryMaxTaskAttempts {
			if result.Replayed != 1 || result.Exhausted != 0 {
				t.Fatalf("recovery sweep after attempt %d = %+v, want one replay", attempt, result)
			}
		} else if result.Replayed != 0 || result.Exhausted != 1 {
			t.Fatalf("recovery sweep after final attempt = %+v, want one exhaustion", result)
		}
		var active, total int
		if err := f.pool.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')),
			       count(*)
			FROM agent_task_queue
			WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID).Scan(&active, &total); err != nil {
			t.Fatalf("count attempt %d tasks: %v", attempt, err)
		}
		wantActive := 1
		wantTotal := attempt + 1
		if attempt == delegatedFailureRecoveryMaxTaskAttempts {
			wantActive = 0
			wantTotal = delegatedFailureRecoveryMaxTaskAttempts
		}
		if active != wantActive || total != wantTotal {
			t.Fatalf("attempt %d active/total = %d/%d, want %d/%d", attempt, active, total, wantActive, wantTotal)
		}
	}

	var exhaustionComments int
	var exhaustionContent, exhaustionParentID string
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*), COALESCE(max(content), ''), COALESCE(max(parent_id::text), '')
		FROM comment
		WHERE issue_id = $1 AND author_type = 'system' AND type = 'system' AND source_task_id = $2`, f.issueID, failedID).
		Scan(&exhaustionComments, &exhaustionContent, &exhaustionParentID); err != nil {
		t.Fatalf("read exhaustion comment: %v", err)
	}
	if exhaustionComments != 1 || !strings.Contains(exhaustionContent, "stopped after 3") {
		t.Fatalf("exhaustion comment = count %d content %q, want one visible bounded-stop explanation", exhaustionComments, exhaustionContent)
	}
	if exhaustionParentID != f.sourceTrigger {
		t.Fatalf("exhaustion comment parent_id = %q, want source trigger %s", exhaustionParentID, f.sourceTrigger)
	}
	var inboxItems int
	var inboxSeverity, inboxBody string
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*), COALESCE(max(severity), ''), COALESCE(max(body), '')
		FROM inbox_item
		WHERE workspace_id = $1
		  AND recipient_type = 'member'
		  AND recipient_id = $2
		  AND issue_id = $3
		  AND type = 'task_failed'`, f.workspaceID, f.userID, f.issueID).
		Scan(&inboxItems, &inboxSeverity, &inboxBody); err != nil {
		t.Fatalf("read exhaustion inbox item: %v", err)
	}
	if inboxItems != 1 || inboxSeverity != "action_required" || inboxBody != exhaustionContent {
		t.Fatalf("exhaustion inbox = count %d severity %q body %q, want one action-required copy of explanation", inboxItems, inboxSeverity, inboxBody)
	}
	if result, err := svc.RecoverPendingDelegatedFailures(ctx, 100); err != nil || result != (DelegatedFailureRecoverySweepResult{}) {
		t.Fatalf("post-exhaustion recovery sweep = %+v, %v; want zero result, nil", result, err)
	}
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM inbox_item
		WHERE workspace_id = $1 AND recipient_id = $2 AND issue_id = $3
		  AND type = 'task_failed'`, f.workspaceID, f.userID, f.issueID).Scan(&inboxItems); err != nil {
		t.Fatalf("recount exhaustion inbox items: %v", err)
	}
	if inboxItems != 1 {
		t.Fatalf("post-exhaustion inbox count = %d, want 1", inboxItems)
	}
	var finalTasks int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID).Scan(&finalTasks); err != nil {
		t.Fatalf("count final recovery tasks: %v", err)
	}
	if finalTasks != delegatedFailureRecoveryMaxTaskAttempts {
		t.Fatalf("final recovery task count = %d, want %d", finalTasks, delegatedFailureRecoveryMaxTaskAttempts)
	}
}

func TestHandleFailedTasksFinalDelegatedFailureWakesCoordinator(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	failedID := f.insertWorkerTask(t, "failed", "comment", 1, 2)
	if _, err := f.pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET failure_reason = 'agent_error.process_failure', error = 'worker process exited', completed_at = now()
		WHERE id = $1`, failedID); err != nil {
		t.Fatalf("stamp failed task: %v", err)
	}
	failed, err := svc.Queries.GetAgentTask(ctx, failedID)
	if err != nil {
		t.Fatalf("load failed task: %v", err)
	}

	if retried := svc.HandleFailedTasks(ctx, []db.AgentTaskQueue{failed}); retried != 0 {
		t.Fatalf("HandleFailedTasks retried = %d, want 0", retried)
	}

	var recoveryTasks, recoveryComments int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID).Scan(&recoveryTasks); err != nil {
		t.Fatalf("count recovery tasks: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1 AND type = 'progress_update' AND source_task_id = $2`, f.issueID, failedID).Scan(&recoveryComments); err != nil {
		t.Fatalf("count recovery comments: %v", err)
	}
	if recoveryTasks != 1 || recoveryComments != 1 {
		t.Fatalf("sweeper recovery tasks/comments = %d/%d, want 1/1", recoveryTasks, recoveryComments)
	}
}

func TestFailTaskRetryPendingDoesNotWakeCoordinator(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	failedID := f.insertWorkerTask(t, "running", "comment", 1, 2)

	if _, err := svc.FailTask(ctx, failedID, "task timed out", "", "", "", "timeout", false, "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	var retries, recoveries, comments int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1`, failedID).Scan(&retries); err != nil {
		t.Fatalf("count retries: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID).Scan(&recoveries); err != nil {
		t.Fatalf("count recoveries: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE type = 'progress_update' AND source_task_id = $1`, failedID).Scan(&comments); err != nil {
		t.Fatalf("count recovery comments: %v", err)
	}
	if retries != 1 || recoveries != 0 || comments != 0 {
		t.Fatalf("retry/recovery/comments = %d/%d/%d, want 1/0/0", retries, recoveries, comments)
	}
}

func TestFinalDelegatedFailureMergesIntoPendingCoordinatorTask(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	failedID := f.insertWorkerTask(t, "running", "comment", 1, 2)

	var pendingID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			originator_user_id, accountable_user_id, originator_source,
			trigger_evidence_kind, trigger_evidence_ref_id
		)
		VALUES ($1, $2, $3, 'queued', 0, $4, $4, 'direct_human', 'issue_assignment', $3)
		RETURNING id`, f.coordinator, f.runtimeID, f.issueID, f.userID).Scan(&pendingID); err != nil {
		t.Fatalf("seed pending coordinator task: %v", err)
	}

	if _, err := svc.FailTask(ctx, failedID, "worker exited", "", "", "", "agent_error.process_failure", false, "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	var triggerID pgtype.UUID
	var evidenceKind string
	if err := f.pool.QueryRow(ctx, `SELECT trigger_comment_id, trigger_evidence_kind FROM agent_task_queue WHERE id = $1`, pendingID).
		Scan(&triggerID, &evidenceKind); err != nil {
		t.Fatalf("read pending coordinator task: %v", err)
	}
	if !triggerID.Valid {
		t.Fatal("pending coordinator task did not receive the recovery comment")
	}
	if evidenceKind != string(attribution.EvidenceIssueAssignment) {
		t.Fatalf("pending task attribution changed to %q, want issue_assignment", evidenceKind)
	}
	var newRecoveryTasks int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID).Scan(&newRecoveryTasks); err != nil {
		t.Fatalf("count standalone recovery tasks: %v", err)
	}
	if newRecoveryTasks != 0 {
		t.Fatalf("standalone recovery tasks = %d, want 0 after merge", newRecoveryTasks)
	}

	// A second delegated failure while the same coordinator task is still
	// queued must coalesce into that task instead of creating a parallel run.
	secondFailedID := f.insertWorkerTask(t, "running", "comment", 1, 2)
	if _, err := svc.FailTask(ctx, secondFailedID, "second worker exited", "", "", "", "agent_error.process_failure", false, "", ""); err != nil {
		t.Fatalf("FailTask(second): %v", err)
	}
	var secondCommentID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `SELECT id FROM comment WHERE type = 'progress_update' AND source_task_id = $1`, secondFailedID).Scan(&secondCommentID); err != nil {
		t.Fatalf("load second recovery comment: %v", err)
	}
	var newestTrigger, coversFirst bool
	if err := f.pool.QueryRow(ctx, `
		SELECT trigger_comment_id = $2::uuid, $3::uuid = ANY(coalesced_comment_ids)
		FROM agent_task_queue WHERE id = $1`, pendingID, secondCommentID, triggerID).Scan(&newestTrigger, &coversFirst); err != nil {
		t.Fatalf("read second merged signal: %v", err)
	}
	if !newestTrigger || !coversFirst {
		t.Fatalf("parallel recovery plan = newest trigger %v / prior coalesced %v, want true/true", newestTrigger, coversFirst)
	}
}

func TestDelegatedFailurePlannedBehindDispatchedCoordinatorGetsFollowUp(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	failedID := f.insertWorkerTask(t, "running", "comment", 1, 2)

	var activeID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, dispatched_at,
			originator_user_id, accountable_user_id, originator_source
		)
		VALUES ($1, $2, $3, 'dispatched', 0, now(), $4, $4, 'direct_human')
		RETURNING id`, f.coordinator, f.runtimeID, f.issueID, f.userID).Scan(&activeID); err != nil {
		t.Fatalf("seed active coordinator task: %v", err)
	}

	if _, err := svc.FailTask(ctx, failedID, "worker exited", "", "", "", "agent_error.process_failure", false, "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}
	comment, err := svc.Queries.GetDelegatedFailureRecoveryComment(ctx, db.GetDelegatedFailureRecoveryCommentParams{
		IssueID:      util.MustParseUUID(f.issueID),
		WorkspaceID:  util.MustParseUUID(f.workspaceID),
		SourceTaskID: failedID,
	})
	if err != nil {
		t.Fatalf("load recovery comment: %v", err)
	}
	var planned bool
	if err := f.pool.QueryRow(ctx, `SELECT $2::uuid = ANY(coalesced_comment_ids) FROM agent_task_queue WHERE id = $1`, activeID, comment.ID).Scan(&planned); err != nil {
		t.Fatalf("read active recovery plan: %v", err)
	}
	if !planned {
		t.Fatal("active coordinator task did not record planned recovery input")
	}
	reconcilable, err := svc.Queries.ListReconcilableCommentsForIssueSince(ctx, db.ListReconcilableCommentsForIssueSinceParams{
		IssueID:           util.MustParseUUID(f.issueID),
		Since:             pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		PlannedCommentIds: []pgtype.UUID{comment.ID},
	})
	if err != nil {
		t.Fatalf("list reconcilable recovery comments: %v", err)
	}
	if len(reconcilable) != 1 || reconcilable[0].ID != comment.ID {
		t.Fatalf("reconcilable recovery comments = %+v, want only %s", reconcilable, util.UUIDToString(comment.ID))
	}

	if _, err := f.pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, activeID); err != nil {
		t.Fatalf("complete coordinator task: %v", err)
	}
	if err := svc.DispatchDelegatedFailureRecoveryComment(ctx, comment, activeID); err != nil {
		t.Fatalf("replay recovery after completion: %v", err)
	}
	var followUps int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE agent_id = $1 AND issue_id = $2 AND trigger_evidence_kind = 'delegated_failure'
		  AND trigger_evidence_ref_id = $3`, f.coordinator, f.issueID, failedID).Scan(&followUps); err != nil {
		t.Fatalf("count recovery follow-ups: %v", err)
	}
	if followUps != 1 {
		t.Fatalf("recovery follow-ups = %d, want 1", followUps)
	}
}

func TestDelegatedFailureRecoveryTaskDoesNotRecursivelyWake(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	recoveryID := f.insertWorkerTask(t, "running", string(attribution.EvidenceDelegatedFailure), 1, 2)

	if _, err := svc.FailTask(ctx, recoveryID, "recovery failed", "", "", "", "agent_error.process_failure", false, "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	var comments int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE type = 'progress_update' AND source_task_id = $1`, recoveryID).Scan(&comments); err != nil {
		t.Fatalf("count recursive recovery comments: %v", err)
	}
	if comments != 0 {
		t.Fatalf("recursive recovery comments = %d, want 0", comments)
	}
}
