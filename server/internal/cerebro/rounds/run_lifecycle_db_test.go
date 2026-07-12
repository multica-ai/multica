package rounds

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
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

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		t.Fatal(fmt.Errorf("parse uuid %q: %w", s, err))
	}
	return id
}
