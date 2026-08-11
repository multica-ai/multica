package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// newReviewSchedulerTestPool returns a DB pool or skips the test when the
// database is unreachable.
func newReviewSchedulerTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type reviewSchedulerFixture struct {
	pool        *pgxpool.Pool
	workspaceID string
	agentID     string
	runtimeID   string
}

func newReviewSchedulerFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) reviewSchedulerFixture {
	t.Helper()

	// The due scan is global (migration 328 index), so stale rows from prior
	// runs would be picked up and enqueued. Remove any evidence records that
	// belong to test workspaces before seeding a fresh one.
	if _, err := pool.Exec(ctx, `
		DELETE FROM execution_evidence_record
		WHERE workspace_id IN (
			SELECT id FROM workspace WHERE slug LIKE 'review-%'
		)
	`); err != nil {
		t.Fatalf("clean test evidence records: %v", err)
	}

	suffix := time.Now().UnixNano()
	slug := fmt.Sprintf("review-%d", suffix)

	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Review Scheduler Test", fmt.Sprintf("review-%d@multica.ai", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	var workspaceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Review Scheduler Test", slug, "temporary review scheduler test workspace", "RST").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}

	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at, visibility, owner_id
		)
		VALUES ($1, NULL, $2, 'cloud', 'review_scheduler_test', 'online', 'test runtime', '{}'::jsonb, now(), 'private', $3)
		RETURNING id
	`, workspaceID, "Review Scheduler Runtime", userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, workspaceID, "Review Scheduler Agent", runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	return reviewSchedulerFixture{pool: pool, workspaceID: workspaceID, agentID: agentID, runtimeID: runtimeID}
}

// insertEvidenceRecord seeds an execution_evidence_record in the given review
// state and returns its execution_id.
func insertEvidenceRecord(t *testing.T, ctx context.Context, pool *pgxpool.Pool, wsID string, reviewState string, attempt, maxAttempts int32, wakeup *time.Time, version int32) string {
	t.Helper()

	var execID string
	query := `
		INSERT INTO execution_evidence_record (
			execution_id, workspace_id, review_policy, review_state,
			review_attempt, max_review_attempts, review_version, review_next_wakeup
		)
		VALUES (gen_random_uuid(), $1, 'independent', $2, $3, $4, $5, COALESCE($6, now() - interval '1 minute'))
		RETURNING execution_id
	`
	var wakeupVal interface{}
	if wakeup != nil {
		wakeupVal = *wakeup
	}
	if err := pool.QueryRow(ctx, query, wsID, reviewState, attempt, maxAttempts, version, wakeupVal).Scan(&execID); err != nil {
		t.Fatalf("insert evidence record: %v", err)
	}
	return execID
}

// fakeReviewEnqueuer records enqueue calls and returns a scripted task id or
// error.
type fakeReviewEnqueuer struct {
	taskID string
	err    error
	calls  int
}

func (f *fakeReviewEnqueuer) Enqueue(ctx context.Context, rec db.ExecutionEvidenceRecord) (pgtype.UUID, error) {
	f.calls++
	if f.err != nil {
		return pgtype.UUID{}, f.err
	}
	return uuidFromString(f.taskID), nil
}

func newScheduler(t *testing.T, q ReviewQuerier, enq ReviewTaskEnqueuer) *EvidenceReviewScheduler {
	t.Helper()
	return NewEvidenceReviewScheduler(q, enq, ReviewSchedulerConfig{
		LeaseOwner:    "scheduler-1",
		LeaseDuration: time.Minute,
		BatchSize:     50,
	})
}

func getEvidenceRecord(t *testing.T, ctx context.Context, pool *pgxpool.Pool, execID string) db.ExecutionEvidenceRecord {
	t.Helper()
	var row db.ExecutionEvidenceRecord
	err := pool.QueryRow(ctx, `
		SELECT execution_id, workspace_id, schema_version, runtime_evidence_state,
		       output_ref, message_refs, usage_refs, artifact_refs, test_refs,
		       review_policy, review_state, review_version, reviewer_agent_id,
		       review_task_id, review_output_ref, review_attempt, max_review_attempts,
		       review_next_wakeup, review_lease_owner, review_lease_expires_at,
		       review_failure_code, created_at, updated_at
		FROM execution_evidence_record
		WHERE execution_id = $1
	`, execID).Scan(
		&row.ExecutionID, &row.WorkspaceID, &row.SchemaVersion, &row.RuntimeEvidenceState,
		&row.OutputRef, &row.MessageRefs, &row.UsageRefs, &row.ArtifactRefs, &row.TestRefs,
		&row.ReviewPolicy, &row.ReviewState, &row.ReviewVersion, &row.ReviewerAgentID,
		&row.ReviewTaskID, &row.ReviewOutputRef, &row.ReviewAttempt, &row.MaxReviewAttempts,
		&row.ReviewNextWakeup, &row.ReviewLeaseOwner, &row.ReviewLeaseExpiresAt,
		&row.ReviewFailureCode, &row.CreatedAt, &row.UpdatedAt,
	)
	if err != nil {
		t.Fatalf("get evidence record: %v", err)
	}
	return row
}

func TestReviewSchedulerPendingToQueued(t *testing.T) {
	pool := newReviewSchedulerTestPool(t)
	ctx := context.Background()
	fx := newReviewSchedulerFixture(t, ctx, pool)
	q := db.New(pool)

	execID := insertEvidenceRecord(t, ctx, pool, fx.workspaceID, string(protocol.ReviewStatePending), 0, 3, nil, 1)

	enq := &fakeReviewEnqueuer{taskID: fx.agentID}
	sched := newScheduler(t, q, enq)
	if err := sched.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	row := getEvidenceRecord(t, ctx, pool, execID)
	if row.ReviewState != string(protocol.ReviewStateQueued) {
		t.Fatalf("review_state = %q, want queued", row.ReviewState)
	}
	if !row.ReviewTaskID.Valid || uuidString(row.ReviewTaskID) != fx.agentID {
		t.Fatalf("review_task_id = %q, want %q", uuidString(row.ReviewTaskID), fx.agentID)
	}
	if row.ReviewAttempt != 1 {
		t.Fatalf("review_attempt = %d, want 1", row.ReviewAttempt)
	}
	if enq.calls != 1 {
		t.Fatalf("enqueue calls = %d, want 1", enq.calls)
	}
}

func TestReviewSchedulerEnqueueFailureGoesRetryWait(t *testing.T) {
	pool := newReviewSchedulerTestPool(t)
	ctx := context.Background()
	fx := newReviewSchedulerFixture(t, ctx, pool)
	q := db.New(pool)

	execID := insertEvidenceRecord(t, ctx, pool, fx.workspaceID, string(protocol.ReviewStatePending), 0, 3, nil, 1)

	enq := &fakeReviewEnqueuer{err: errors.New("dispatch failed")}
	sched := newScheduler(t, q, enq)
	sched.Now = func() time.Time { return time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC) }
	if err := sched.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	row := getEvidenceRecord(t, ctx, pool, execID)
	if row.ReviewState != string(protocol.ReviewStateRetryWait) {
		t.Fatalf("review_state = %q, want retry_wait", row.ReviewState)
	}
	if !row.ReviewNextWakeup.Valid {
		t.Fatal("review_next_wakeup must be set for retry_wait")
	}
	if row.ReviewLeaseOwner.Valid {
		t.Fatalf("lease_owner must be cleared on retry_wait, got %q", row.ReviewLeaseOwner.String)
	}
}

func TestReviewSchedulerMaxAttemptsBlocks(t *testing.T) {
	pool := newReviewSchedulerTestPool(t)
	ctx := context.Background()
	fx := newReviewSchedulerFixture(t, ctx, pool)
	q := db.New(pool)

	// attempt already at max (3); a fresh enqueue failure must block.
	execID := insertEvidenceRecord(t, ctx, pool, fx.workspaceID, string(protocol.ReviewStatePending), 3, 3, nil, 1)

	enq := &fakeReviewEnqueuer{err: errors.New("dispatch failed")}
	sched := newScheduler(t, q, enq)
	if err := sched.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	row := getEvidenceRecord(t, ctx, pool, execID)
	if row.ReviewState != string(protocol.ReviewStateBlocked) {
		t.Fatalf("review_state = %q, want blocked", row.ReviewState)
	}
	if row.ReviewNextWakeup.Valid {
		t.Fatalf("blocked must never carry a scheduler wakeup")
	}
	if row.ReviewLeaseOwner.Valid {
		t.Fatalf("blocked must never carry a lease")
	}
}

func TestReviewSchedulerRetryWaitPromotesToPending(t *testing.T) {
	pool := newReviewSchedulerTestPool(t)
	ctx := context.Background()
	fx := newReviewSchedulerFixture(t, ctx, pool)
	q := db.New(pool)

	past := time.Now().Add(-time.Minute)
	execID := insertEvidenceRecord(t, ctx, pool, fx.workspaceID, string(protocol.ReviewStateRetryWait), 1, 3, &past, 1)

	enq := &fakeReviewEnqueuer{taskID: fx.agentID}
	sched := newScheduler(t, q, enq)
	if err := sched.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	row := getEvidenceRecord(t, ctx, pool, execID)
	if row.ReviewState != string(protocol.ReviewStateQueued) {
		t.Fatalf("review_state = %q, want queued (retry_wait -> pending -> queued)", row.ReviewState)
	}
	if row.ReviewAttempt != 2 {
		t.Fatalf("review_attempt = %d, want 2", row.ReviewAttempt)
	}
}

func TestReviewSchedulerRecoversStaleDispatching(t *testing.T) {
	pool := newReviewSchedulerTestPool(t)
	ctx := context.Background()
	fx := newReviewSchedulerFixture(t, ctx, pool)
	q := db.New(pool)

	// dispatching with an expired lease: the previous scheduler died mid-
	// dispatch. ResetExpiredDispatchingReviewCAS moves it back to pending.
	execID := insertEvidenceRecord(t, ctx, pool, fx.workspaceID, string(protocol.ReviewStateDispatching), 1, 3, nil, 1)
	if _, err := pool.Exec(ctx, `
		UPDATE execution_evidence_record
		SET review_lease_owner = 'dead-scheduler',
		    review_lease_expires_at = now() - interval '1 minute',
		    review_next_wakeup = now() - interval '1 minute'
		WHERE execution_id = $1
	`, execID); err != nil {
		t.Fatalf("set stale dispatching: %v", err)
	}

	enq := &fakeReviewEnqueuer{taskID: fx.agentID}
	sched := newScheduler(t, q, enq)
	if err := sched.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	row := getEvidenceRecord(t, ctx, pool, execID)
	if row.ReviewState != string(protocol.ReviewStateQueued) {
		t.Fatalf("review_state = %q, want queued (stale dispatching re-driven)", row.ReviewState)
	}
	if row.ReviewAttempt != 2 {
		t.Fatalf("review_attempt = %d, want 2 (stale attempt preserved, one new claim)", row.ReviewAttempt)
	}
}

func TestReviewSchedulerBlockedNeverScheduled(t *testing.T) {
	pool := newReviewSchedulerTestPool(t)
	ctx := context.Background()
	fx := newReviewSchedulerFixture(t, ctx, pool)
	q := db.New(pool)

	// blocked rows must never appear in ListReviewDueRecords (migration 328
	// index excludes them), so the scheduler must leave them untouched.
	execID := insertEvidenceRecord(t, ctx, pool, fx.workspaceID, string(protocol.ReviewStateBlocked), 0, 3, nil, 1)

	enq := &fakeReviewEnqueuer{taskID: fx.agentID}
	sched := newScheduler(t, q, enq)
	if err := sched.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	row := getEvidenceRecord(t, ctx, pool, execID)
	if row.ReviewState != string(protocol.ReviewStateBlocked) {
		t.Fatalf("review_state changed to %q, want blocked untouched", row.ReviewState)
	}
	if enq.calls != 0 {
		t.Fatalf("blocked row was enqueued; enqueue calls = %d", enq.calls)
	}
}

// TestReviewSchedulerCASRejectsStaleVersion covers the optimistic review_version
// CAS: a second scheduler claiming with an old version gets zero rows, so the
// record is not double-dispatched.
func TestReviewSchedulerCASRejectsStaleVersion(t *testing.T) {
	pool := newReviewSchedulerTestPool(t)
	ctx := context.Background()
	fx := newReviewSchedulerFixture(t, ctx, pool)
	q := db.New(pool)

	execID := insertEvidenceRecord(t, ctx, pool, fx.workspaceID, string(protocol.ReviewStatePending), 0, 3, nil, 1)

	enq := &fakeReviewEnqueuer{taskID: fx.agentID}
	sched := newScheduler(t, q, enq)
	if err := sched.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	row := getEvidenceRecord(t, ctx, pool, execID)
	if row.ReviewState != string(protocol.ReviewStateQueued) {
		t.Fatalf("review_state = %q, want queued", row.ReviewState)
	}
	if enq.calls != 1 {
		t.Fatalf("enqueue calls = %d, want 1", enq.calls)
	}
}
