package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestPendingDelegatedFailureSweepDoesNotStarveOtherWorkspaces(t *testing.T) {
	ctx := context.Background()
	var paused []pgtype.UUID
	for _, status := range []string{issuestatus.Cancelled, issuestatus.Backlog} {
		f, svc := seedDelegatedFailureFixture(t)
		fx := dbfx.New(f.pool, f.workspaceID, f.userID)
		if err := issuestatus.Ensure(ctx, svc.Queries, util.MustParseUUID(f.workspaceID)); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 50; i++ {
			failedID := f.insertWorkerTask(t, "failed", "comment", 1, 2)
			target, created, err := svc.ensureDelegatedFailureRecoveryComment(ctx, failedID)
			if err != nil || target == nil || !created {
				t.Fatalf("seed interrupted recovery: created=%v err=%v", created, err)
			}
			paused = append(paused, target.comment.ID)
		}
		fx.Exec(t, `UPDATE issue SET status = $2 WHERE id = $1`, f.issueID, status)
	}
	// The 101st signal is newer and belongs to a different, executable workspace.
	f, svc := seedDelegatedFailureFixture(t)
	failedID := f.insertWorkerTask(t, "failed", "comment", 1, 2)
	target, created, err := svc.ensureDelegatedFailureRecoveryComment(ctx, failedID)
	if err != nil || target == nil || !created {
		t.Fatalf("seed executable recovery: created=%v err=%v", created, err)
	}
	result, err := svc.RecoverPendingDelegatedFailures(ctx, 100)
	if err != nil || result != (DelegatedFailureRecoverySweepResult{Scanned: 1, Replayed: 1}) {
		t.Fatalf("recovery behind 100 paused signals = %+v, %v; want one scan and replay", result, err)
	}
	if covered, err := svc.Queries.HasTaskCoveringDelegatedFailureComment(ctx, db.HasTaskCoveringDelegatedFailureCommentParams{
		IssueID: target.issue.ID, AgentID: target.agent.ID, CommentID: target.comment.ID,
	}); err != nil || !covered {
		t.Fatalf("executable recovery has a task = %v, %v; want true", covered, err)
	}
	if result, err := svc.RecoverPendingDelegatedFailures(ctx, 100); err != nil || result != (DelegatedFailureRecoverySweepResult{}) {
		t.Fatalf("repeat sweep = %+v, %v; want no duplicate dispatch", result, err)
	}
	for _, id := range paused {
		if f.settled(t, id) {
			t.Fatal("pausing an issue must not settle its undelivered recovery")
		}
	}
}

func TestDelegatedFailureRecoveryStatusEligibility(t *testing.T) {
	for _, seeded := range []bool{true, false} {
		name := "seeded_catalog"
		if !seeded {
			name = "missing_builtin_catalog"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f, svc := seedDelegatedFailureFixture(t)
			fx := dbfx.New(f.pool, f.workspaceID, f.userID)
			if seeded {
				if err := issuestatus.Ensure(ctx, svc.Queries, util.MustParseUUID(f.workspaceID)); err != nil {
					t.Fatal(err)
				}
			}
			failedID := f.insertWorkerTask(t, "failed", "comment", 1, 2)
			target, created, err := svc.ensureDelegatedFailureRecoveryComment(ctx, failedID)
			if err != nil || target == nil || !created {
				t.Fatalf("seed recovery: created=%v err=%v", created, err)
			}
			cases := []struct {
				status, category              string
				allowed, archived, unresolved bool
			}{
				{status: "backlog"},
				{status: "todo", allowed: true},
				{status: "in_progress", allowed: true},
				{status: "in_review", allowed: true},
				{status: "blocked", allowed: true},
				{status: "done"},
				{status: "cancelled"},
				{status: "triage"},
				{status: "missing_recovery_status", unresolved: true},
				{status: "custom_unstarted", category: "unstarted", allowed: true},
				{status: "custom_started", category: "started", allowed: true},
				{status: "custom_done", category: "done"},
				{status: "custom_closed", category: "closed"},
				{status: "archived_done", category: "done", archived: true},
				{status: "archived_open", category: "started", allowed: true, archived: true},
			}
			for _, tc := range cases {
				t.Run(tc.status, func(t *testing.T) {
					if tc.category != "" {
						cols := dbfx.Cols{"workspace_id": f.workspaceID, "key": tc.status, "name": tc.status,
							"category": tc.category, "color": "#22c55e", "position": 1}
						if tc.archived {
							cols["archived_at"] = dbfx.Raw("now()")
						}
						fx.Insert(t, "issue_status", cols)
					}
					fx.Exec(t, `UPDATE issue SET status = $2 WHERE id = $1`, f.issueID, tc.status)
					pending, err := svc.Queries.ListPendingDelegatedFailureRecoveries(ctx, 100)
					if err != nil {
						t.Fatal(err)
					}
					included := false
					for _, comment := range pending {
						included = included || comment.ID == target.comment.ID
					}
					if included != tc.allowed {
						t.Errorf("SQL selected recovery = %v, want %v", included, tc.allowed)
					}
					loaded, err := loadDelegatedFailureRecoveryTarget(ctx, svc.Queries, target.failed)
					if err != nil || loaded == nil {
						t.Fatalf("lifecycle must not prevent loading the signal target: %v", err)
					}
					allowed, err := canDispatchDelegatedFailureRecovery(ctx, svc.Queries, loaded.issue)
					if (err != nil) != tc.unresolved {
						t.Errorf("Go eligibility error = %v, want unresolved=%v", err, tc.unresolved)
					}
					if allowed != tc.allowed {
						t.Errorf("Go allowed recovery = %v, want %v", allowed, tc.allowed)
					}
				})
			}
		})
	}
}

func TestDelegatedFailureRecoveryRechecksStatusAndResumes(t *testing.T) {
	for _, status := range []string{"backlog", "cancelled", "done", "recovery_closed"} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			f, svc := seedDelegatedFailureFixture(t)
			fx := dbfx.New(f.pool, f.workspaceID, f.userID)
			if status == "recovery_closed" {
				fx.Insert(t, "issue_status", dbfx.Cols{"workspace_id": f.workspaceID, "key": status,
					"name": "Closed", "category": "closed", "color": "#22c55e", "position": 1})
			}
			failedID := f.insertWorkerTask(t, "failed", "comment", 1, 2)
			target, created, err := svc.ensureDelegatedFailureRecoveryComment(ctx, failedID)
			if err != nil || target == nil || !created {
				t.Fatalf("seed recovery: created=%v err=%v", created, err)
			}
			pending, err := svc.Queries.ListPendingDelegatedFailureRecoveries(ctx, 100)
			if err != nil || len(pending) != 1 || pending[0].ID != target.comment.ID {
				t.Fatalf("initial candidates = %d, %v; want this recovery", len(pending), err)
			}
			// Change the issue after selection, before the same dispatch entry
			// point used by the sweeper and completion reconciliation runs.
			fx.Exec(t, `UPDATE issue SET status = $2 WHERE id = $1`, f.issueID, status)
			if err := svc.DispatchDelegatedFailureRecoveryComment(ctx, pending[0], pgtype.UUID{}); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue
				WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 || f.settled(t, target.comment.ID) {
				t.Fatalf("paused recovery: tasks=%d; want no task or permanent receipt", count)
			}
			fx.Exec(t, `UPDATE issue SET status = 'todo' WHERE id = $1`, f.issueID)
			if result, err := svc.RecoverPendingDelegatedFailures(ctx, 100); err != nil || result != (DelegatedFailureRecoverySweepResult{Scanned: 1, Replayed: 1}) {
				t.Fatalf("reopened recovery = %+v, %v; want one replay", result, err)
			}
			if result, err := svc.RecoverPendingDelegatedFailures(ctx, 100); err != nil || result != (DelegatedFailureRecoverySweepResult{}) {
				t.Fatalf("repeat sweep = %+v, %v; want no duplicate", result, err)
			}
		})
	}
}

type recoveryCatalogErrorDB struct {
	db.DBTX
	err error
}

func (d recoveryCatalogErrorDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "FROM issue_status") {
		return recoveryCatalogErrorRow{d.err}
	}
	return d.DBTX.QueryRow(ctx, sql, args...)
}

type recoveryCatalogErrorRow struct{ err error }

func (r recoveryCatalogErrorRow) Scan(...any) error { return r.err }

func TestDelegatedFailureRecoveryPreservesSignalWhenStatusUnavailable(t *testing.T) {
	for _, readError := range []bool{false, true} {
		name := "missing_catalog_entry"
		if readError {
			name = "catalog_read_error"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f, svc := seedDelegatedFailureFixture(t)
			fx := dbfx.New(f.pool, f.workspaceID, f.userID)
			const status = "recovery_custom_status"
			createStatus := func() {
				fx.Insert(t, "issue_status", dbfx.Cols{"workspace_id": f.workspaceID, "key": status,
					"name": "Active", "category": "started", "color": "#22c55e", "position": 1})
			}
			fx.Exec(t, `UPDATE issue SET status = $2 WHERE id = $1`, f.issueID, status)
			failedID := f.insertWorkerTask(t, "failed", "comment", 1, 2)
			queries := svc.Queries
			failed, err := queries.GetAgentTask(ctx, failedID)
			if err != nil {
				t.Fatal(err)
			}
			wantErr := error(pgx.ErrNoRows)
			if readError {
				createStatus()
				wantErr = errors.New("catalog temporarily unavailable")
				svc.Queries = db.New(recoveryCatalogErrorDB{DBTX: f.pool, err: wantErr})
			}
			// This post-terminal hook runs only once. A failure to read lifecycle
			// must pause dispatch without losing the durable obligation to retry.
			if handled, err := svc.recoverDelegatedTaskFailure(ctx, failed); !handled || !errors.Is(err, wantErr) {
				t.Errorf("initial recovery: handled=%v err=%v; want handled and %v", handled, err, wantErr)
			}
			comment, err := queries.GetDelegatedFailureRecoveryComment(ctx, db.GetDelegatedFailureRecoveryCommentParams{
				IssueID: util.MustParseUUID(f.issueID), WorkspaceID: util.MustParseUUID(f.workspaceID), SourceTaskID: failedID,
			})
			if err != nil {
				t.Fatalf("recovery signal must survive status resolution failure: %v", err)
			}
			var count int
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue
				WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 || f.settled(t, comment.ID) {
				t.Fatalf("unresolved recovery: tasks=%d; want no dispatch or permanent receipt", count)
			}
			if !readError {
				if result, err := svc.RecoverPendingDelegatedFailures(ctx, 100); err != nil || result != (DelegatedFailureRecoverySweepResult{}) {
					t.Fatalf("unresolved signal occupies the batch: %+v, %v", result, err)
				}
				createStatus()
			}
			svc.Queries = queries
			if result, err := svc.RecoverPendingDelegatedFailures(ctx, 100); err != nil || result != (DelegatedFailureRecoverySweepResult{Scanned: 1, Replayed: 1}) {
				t.Fatalf("catalog repaired: %+v, %v; want one replay without repeating the failure hook", result, err)
			}
			if result, err := svc.RecoverPendingDelegatedFailures(ctx, 100); err != nil || result != (DelegatedFailureRecoverySweepResult{}) {
				t.Fatalf("repeat sweep: %+v, %v; want no duplicate", result, err)
			}
		})
	}
}

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

func TestPendingDelegatedFailureSweepSkipsCustomTerminalSourceIssue(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	failedID := f.insertWorkerTask(t, "failed", "comment", 1, 2)
	if _, err := f.pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET failure_reason = 'agent_error.process_failure', error = 'worker exited', completed_at = now()
		WHERE id = $1`, failedID); err != nil {
		t.Fatalf("stamp failed task: %v", err)
	}
	if target, created, err := svc.ensureDelegatedFailureRecoveryComment(ctx, failedID); err != nil || target == nil || !created {
		t.Fatalf("ensure recovery comment = target %v created %v err %v", target != nil, created, err)
	}

	const customDone = "recovery_verified"
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO issue_status (
			workspace_id, key, name, description, category, color, is_system, position
		) VALUES ($1, $2, 'Recovery verified', '', 'done', '#22c55e', false, 1)`,
		f.workspaceID, customDone); err != nil {
		t.Fatalf("insert custom done status: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE issue SET status = $2 WHERE id = $1`, f.issueID, customDone); err != nil {
		t.Fatalf("move source issue to custom done status: %v", err)
	}

	if result, err := svc.RecoverPendingDelegatedFailures(ctx, 100); err != nil || result != (DelegatedFailureRecoverySweepResult{}) {
		t.Fatalf("terminal-source recovery sweep = %+v, %v; want zero result, nil", result, err)
	}
	var recoveryTasks int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1`, failedID).Scan(&recoveryTasks); err != nil {
		t.Fatalf("count recovery tasks: %v", err)
	}
	if recoveryTasks != 0 {
		t.Fatalf("recovery tasks = %d, want none for a custom terminal source issue", recoveryTasks)
	}
}

// The SQL prefilter is what keeps the recovery outbox scan from growing with
// history. A signal whose source issue can never resume has to be dropped
// THERE: the Go gate downstream discards it without stamping
// recovery_settled_at, so anything that reaches Go stays in the scan forever
// and spends part of maxPerTick on every tick.
//
// MUL-7240 disabled most of it silently. The predicate read
// issue_status.category but compared it against the old seven-value key
// vocabulary, and once a workspace's catalog is seeded — every production
// workspace, per migration 469 — COALESCE returns that category: 'cancelled'
// arrives as 'closed' and 'backlog' as 'unstarted', leaving only 'done' still
// excluded. An unseeded workspace falls back to the raw key, so both are
// covered here: the two vocabularies are each only reachable in one of them.
// (MUL-7364)
func TestPendingDelegatedFailureSweepPrefiltersUnresumableSourceIssues(t *testing.T) {
	ctx := context.Background()
	for _, catalog := range []struct {
		name   string
		seeded bool
	}{
		{"seeded catalog", true},
		{"unseeded catalog", false},
	} {
		t.Run(catalog.name, func(t *testing.T) {
			for _, tc := range []struct {
				name string
				// status the source issue ends up on. customCategory, when set,
				// makes it a custom key created with that lifecycle first.
				status         string
				customCategory string
				wantScanned    bool
			}{
				{name: "built-in cancelled", status: "cancelled"},
				{name: "built-in backlog", status: "backlog"},
				{name: "built-in done", status: "done"},
				{name: "custom closed-category status", status: "shelved", customCategory: "closed"},
				{name: "pre-backfill cancelled category", status: "legacy_shelved", customCategory: "cancelled"},
				{name: "pre-backfill custom backlog stays live", status: "legacy_later", customCategory: "backlog", wantScanned: true},
				{name: "source issue still live", status: "in_progress", wantScanned: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					f, svc := seedDelegatedFailureFixture(t)
					workspaceUUID, err := util.ParseUUID(f.workspaceID)
					if err != nil {
						t.Fatalf("parse workspace id: %v", err)
					}
					if catalog.seeded {
						if err := svc.Queries.SeedIssueStatusEntries(ctx, workspaceUUID); err != nil {
							t.Fatalf("seed status catalog: %v", err)
						}
					}
					if tc.customCategory != "" {
						if _, err := f.pool.Exec(ctx, `
							INSERT INTO issue_status (
								workspace_id, key, name, description, category, color, is_system, position
							) VALUES ($1, $2, 'Shelved', '', $3, '#6b7280', false, 1)`,
							f.workspaceID, tc.status, tc.customCategory); err != nil {
							t.Fatalf("insert custom status: %v", err)
						}
					}

					failedID := f.insertWorkerTask(t, "failed", "comment", 1, 2)
					if _, err := f.pool.Exec(ctx, `
						UPDATE agent_task_queue
						SET failure_reason = 'agent_error.process_failure', error = 'worker exited', completed_at = now()
						WHERE id = $1`, failedID); err != nil {
						t.Fatalf("stamp failed task: %v", err)
					}
					// The signal has to exist before the source issue moves:
					// creating it runs the same Go gate being bypassed here.
					target, created, err := svc.ensureDelegatedFailureRecoveryComment(ctx, failedID)
					if err != nil || target == nil || !created {
						t.Fatalf("ensure recovery comment = target %v created %v err %v", target != nil, created, err)
					}
					if _, err := f.pool.Exec(ctx, `UPDATE issue SET status = $2 WHERE id = $1`, f.issueID, tc.status); err != nil {
						t.Fatalf("move source issue to %q: %v", tc.status, err)
					}

					// Asserted against this fixture's own signal rather than the
					// sweep's totals: the scan is not workspace-scoped.
					pending, err := svc.Queries.ListPendingDelegatedFailureRecoveries(ctx, 1000)
					if err != nil {
						t.Fatalf("list pending recoveries: %v", err)
					}
					scanned := false
					for _, comment := range pending {
						if comment.ID == target.comment.ID {
							scanned = true
							break
						}
					}
					if scanned != tc.wantScanned {
						t.Fatalf("recovery signal scanned = %v, want %v — a source issue on %q %s",
							scanned, tc.wantScanned, tc.status,
							map[bool]string{true: "must stay in the outbox scan", false: "can never resume, so the row is rescanned and re-discarded forever"}[tc.wantScanned])
					}
				})
			}
		})
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
	cancelled, err := svc.CancelTaskByUser(ctx, recoveryTaskID, TaskCancellationActor{
		Type: "member",
		ID:   util.MustParseUUID(f.userID),
		Name: "Recovery owner",
	})
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
			trigger_evidence_kind, trigger_evidence_ref_id, trigger_comment_id
		)
		VALUES ($1, $2, $3, 'queued', 0, $4, $4, 'direct_human', 'issue_assignment', $3, $5)
		RETURNING id`, f.coordinator, f.runtimeID, f.issueID, f.userID, f.sourceTrigger).Scan(&pendingID); err != nil {
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
			originator_user_id, accountable_user_id, originator_source, trigger_comment_id
		)
		VALUES ($1, $2, $3, 'dispatched', 0, now(), $4, $4, 'direct_human', $5)
		RETURNING id`, f.coordinator, f.runtimeID, f.issueID, f.userID, f.sourceTrigger).Scan(&activeID); err != nil {
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
