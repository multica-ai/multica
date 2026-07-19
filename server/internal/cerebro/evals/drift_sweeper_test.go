package evals

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// insertVersionedRun inserts a run for an eval with an explicit target_version,
// status and created_at (issue_id left NULL) so drift ordering is deterministic.
func insertVersionedRun(t *testing.T, store *Store, f evalFixture, evalID uuid.UUID, targetVersion, status string, createdAt time.Time) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `INSERT INTO cerebro_eval_run
        (workspace_id, eval_id, eval_version, target_version, status, results, created_by_id, created_by_type, created_at)
        SELECT $1, e.id, e.version, $3, $4, '{}'::jsonb, $5, 'member', $6
        FROM cerebro_eval e WHERE e.workspace_id=$1 AND e.id=$2`,
		f.workspaceID, evalID, targetVersion, status, f.actorID, createdAt); err != nil {
		t.Fatalf("insert versioned run: %v", err)
	}
}

func newDriftSweeperForTest(store *Store, inbox *fakeInboxWriter, recipients []pgtype.UUID) *DriftSweeper {
	return NewDriftSweeper(store, &fakeOwnerAdminLister{recipients: recipients}, inbox, nil)
}

func TestDriftAssessFlagsFailingNewestRun(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	f := seedEvalFixture(t)
	store := NewStore(evalTestPool)
	ctx := context.Background()

	evalID := seedActiveEval(t, f, "drift-failing", 1)
	eval, err := store.Get(ctx, f.workspaceID, evalID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	base := time.Now().Add(-time.Hour)
	insertVersionedRun(t, store, f, evalID, "1.0.0", "passed", base)
	insertVersionedRun(t, store, f, evalID, "1.0.0", "failed", base.Add(30*time.Minute))

	sweeper := newDriftSweeperForTest(store, &fakeInboxWriter{}, []pgtype.UUID{recipient()})
	reason, drifted, err := sweeper.assess(ctx, eval)
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	if !drifted {
		t.Fatal("expected drift for an eval whose newest run failed")
	}
	if reason == "" {
		t.Fatal("expected a non-empty drift reason")
	}
}

func TestDriftAssessFlagsPassRateRegression(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	f := seedEvalFixture(t)
	store := NewStore(evalTestPool)
	ctx := context.Background()

	evalID := seedActiveEval(t, f, "drift-regression", 1)
	eval, err := store.Get(ctx, f.workspaceID, evalID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	recent := time.Now().Add(-time.Minute)
	// Previous target version: 2/2 passed (100%). Newest target version, most
	// recent runs: 1/2 passed (50%) — a regression, but newest run itself passed
	// so only the pass-rate signal fires.
	insertVersionedRun(t, store, f, evalID, "1.0.0", "passed", old)
	insertVersionedRun(t, store, f, evalID, "1.0.0", "passed", old.Add(time.Minute))
	insertVersionedRun(t, store, f, evalID, "2.0.0", "failed", recent.Add(-2*time.Minute))
	insertVersionedRun(t, store, f, evalID, "2.0.0", "passed", recent)

	sweeper := newDriftSweeperForTest(store, &fakeInboxWriter{}, []pgtype.UUID{recipient()})
	reason, drifted, err := sweeper.assess(ctx, eval)
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	if !drifted {
		t.Fatal("expected drift for a pass-rate regression across target versions")
	}
	if reason == "" {
		t.Fatal("expected a non-empty drift reason")
	}
}

func TestDriftAssessHealthyEvalDoesNotDrift(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	f := seedEvalFixture(t)
	store := NewStore(evalTestPool)
	ctx := context.Background()

	evalID := seedActiveEval(t, f, "drift-healthy", 1)
	eval, err := store.Get(ctx, f.workspaceID, evalID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	base := time.Now().Add(-time.Hour)
	insertVersionedRun(t, store, f, evalID, "1.0.0", "passed", base)
	insertVersionedRun(t, store, f, evalID, "1.0.0", "passed", base.Add(time.Minute))

	sweeper := newDriftSweeperForTest(store, &fakeInboxWriter{}, []pgtype.UUID{recipient()})
	_, drifted, err := sweeper.assess(ctx, eval)
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	if drifted {
		t.Fatal("a healthy eval with only passing runs should not drift")
	}
}

func TestDriftAlertWritesAttentionInboxPerRecipient(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	f := seedEvalFixture(t)
	store := NewStore(evalTestPool)
	ctx := context.Background()

	evalID := seedActiveEval(t, f, "drift-alert", 1)
	eval, err := store.Get(ctx, f.workspaceID, evalID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	inbox := &fakeInboxWriter{}
	sweeper := newDriftSweeperForTest(store, inbox, nil)
	sweeper.alert(ctx, eval, "its newest run failed", []pgtype.UUID{recipient(), recipient()})
	if len(inbox.calls) != 2 {
		t.Fatalf("expected one inbox item per recipient (2), got %d", len(inbox.calls))
	}
	for _, item := range inbox.calls {
		if item.Type != inboxTypeEvalDrift {
			t.Errorf("type = %q, want %q", item.Type, inboxTypeEvalDrift)
		}
		if item.Severity != "attention" {
			t.Errorf("severity = %q, want attention", item.Severity)
		}
		if !item.ActorType.Valid || item.ActorType.String != "system" {
			t.Errorf("actor_type = %+v, want system", item.ActorType)
		}
	}
}

func TestDriftSweeperDisabledReturnsWithoutWork(t *testing.T) {
	t.Setenv("CEREBRO_EVAL_DRIFT_ENABLED", "false")
	NewDriftSweeper(nil, nil, nil, nil).Run(context.Background(), time.Millisecond)
}
