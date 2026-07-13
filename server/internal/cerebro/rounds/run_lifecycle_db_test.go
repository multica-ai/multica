package rounds

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// FIR-3114 — run lifecycle (review round 3): a Start with nothing waiting
// never leaves the round pinned open (the cycle closes again immediately and
// the empty run completes), a new Start supersedes any lingering active run,
// and DismissRun (the UI's Pause) stays idempotent. Uses a nil TaskService:
// with no held triggers Start never enqueues, and publishProgress no-ops
// without a bus.
func TestStartSupersedesReadyRunAndDismissCompletesIt(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var wsID, ownerID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ('Rounds Lifecycle', 'rounds-lifecycle-'||substr(gen_random_uuid()::text,1,8), '', 'RLC') RETURNING id`).Scan(&wsID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM workspace WHERE id=$1`, wsID)
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Rounds Owner', 'rounds-lifecycle-'||substr(gen_random_uuid()::text,1,8)||'@test.local') RETURNING id`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM "user" WHERE id=$1`, ownerID)

	svc := New(pool, db.New(pool), nil)
	round, err := svc.Create(ctx, wsID, ownerID, "Lifecycle", "batch", "", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	roundID := mustUUID(t, round.ID)

	first, err := svc.Start(ctx, wsID, ownerID, roundID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != RunCompleted {
		t.Fatalf("first run status = %q, want completed (0 held triggers complete instantly)", first.Status)
	}
	var cycleOpen bool
	if err := pool.QueryRow(ctx, `SELECT cycle_opened_at IS NOT NULL FROM cerebro_round WHERE id=$1`, roundID).Scan(&cycleOpen); err != nil {
		t.Fatal(err)
	}
	if cycleOpen {
		t.Fatal("cycle open after an empty start, want closed again immediately")
	}

	// A lingering active run (e.g. agents still working) is superseded by the
	// next Start instead of violating the one-active-run index.
	var lingering pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO cerebro_round_run (round_id, total_count) VALUES ($1, 2) RETURNING id`, roundID).Scan(&lingering); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(ctx, wsID, ownerID, roundID); err != nil {
		t.Fatal(err)
	}
	superseded, err := svc.GetRun(ctx, lingering)
	if err != nil {
		t.Fatal(err)
	}
	if superseded.Status != RunCompleted || superseded.CompletedAt == nil {
		t.Fatalf("superseded run = %q (completed_at %v), want completed with timestamp", superseded.Status, superseded.CompletedAt)
	}
	active, err := svc.ActiveRun(ctx, roundID)
	if err != nil {
		t.Fatal(err)
	}
	if active != nil {
		t.Fatalf("active run after empty starts = %+v, want none", active)
	}

	if err := svc.DismissRun(ctx, wsID, ownerID, roundID); err != nil {
		t.Fatal(err)
	}
	// Idempotent: dismissing again is a no-op, not an error.
	if err := svc.DismissRun(ctx, wsID, ownerID, roundID); err != nil {
		t.Fatalf("second dismiss = %v, want nil", err)
	}
}

// FIR-3179 — a failed job is retried only when the owner starts the next
// round. Three failed retries exhaust the trigger; a fourth retry is never
// created and the durable trigger state becomes failed.
func TestStartRetriesFailedJobsThreeTimesThenStops(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	issueID := f.newIssue(t, "retry failed round job")
	commentID := f.newComment(t, issueID, "member", f.ownerID, "retry this")
	held, err := f.svc.HoldComment(ctx, f.wsID, issueID, commentID, "member", util.UUIDToString(f.ownerID), "retry this")
	if err != nil || !held {
		t.Fatalf("HoldComment = held %v, err %v; want held", held, err)
	}

	failRun := func(run Run) {
		t.Helper()
		runID := mustUUID(t, run.ID)
		if _, err := f.pool.Exec(ctx, `UPDATE agent_task_queue q SET status='failed', failure_reason='test_failure' FROM cerebro_round_run_item i WHERE i.run_id=$1 AND i.task_id=q.id`, runID); err != nil {
			t.Fatal(err)
		}
		f.svc.RefreshRun(ctx, runID)
	}

	initial, err := f.svc.Start(ctx, f.wsID, f.ownerID, f.roundID)
	if err != nil {
		t.Fatal(err)
	}
	failRun(initial)

	for retry := 1; retry <= 3; retry++ {
		run, err := f.svc.Start(ctx, f.wsID, f.ownerID, f.roundID)
		if err != nil {
			t.Fatalf("retry %d Start: %v", retry, err)
		}
		if run.TotalCount != 1 {
			t.Fatalf("retry %d total_count = %d, want 1", retry, run.TotalCount)
		}
		var retryCount int
		if err := f.pool.QueryRow(ctx, `SELECT retry_count FROM cerebro_round_held_trigger WHERE round_id=$1 AND issue_id=$2`, f.roundID, issueID).Scan(&retryCount); err != nil {
			t.Fatal(err)
		}
		if retryCount != retry {
			t.Fatalf("retry_count = %d after retry %d, want %d", retryCount, retry, retry)
		}
		failRun(run)
	}

	var triggerState string
	if err := f.pool.QueryRow(ctx, `SELECT state FROM cerebro_round_held_trigger WHERE round_id=$1 AND issue_id=$2`, f.roundID, issueID).Scan(&triggerState); err != nil {
		t.Fatal(err)
	}
	if triggerState != "failed" {
		t.Fatalf("trigger state = %q, want failed after three retries", triggerState)
	}
	members, err := f.svc.Members(ctx, f.roundID)
	if err != nil {
		t.Fatal(err)
	}
	member := f.memberByIssue(t, members, issueID)
	if member.State != MemberFailed || member.RetryCount != 3 {
		t.Fatalf("member state = %q retry_count=%d, want failed/3", member.State, member.RetryCount)
	}
	var tasksBefore int
	if err := f.pool.QueryRow(ctx, `SELECT count(*)::int FROM agent_task_queue WHERE issue_id=$1`, issueID).Scan(&tasksBefore); err != nil {
		t.Fatal(err)
	}
	finalRun, err := f.svc.Start(ctx, f.wsID, f.ownerID, f.roundID)
	if err != nil {
		t.Fatal(err)
	}
	if finalRun.TotalCount != 0 {
		t.Fatalf("run after exhausted retries has %d jobs, want 0", finalRun.TotalCount)
	}
	var tasksAfter int
	if err := f.pool.QueryRow(ctx, `SELECT count(*)::int FROM agent_task_queue WHERE issue_id=$1`, issueID).Scan(&tasksAfter); err != nil {
		t.Fatal(err)
	}
	if tasksAfter != tasksBefore {
		t.Fatalf("tasks after exhausted Start = %d, want unchanged %d", tasksAfter, tasksBefore)
	}
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		t.Fatal(fmt.Errorf("parse uuid %q: %w", s, err))
	}
	return id
}
