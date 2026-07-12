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

// FIR-3114 — run lifecycle: a new Start supersedes the previous 'ready' run,
// and DismissRun (the UI's Pause) completes it so the round collapses back to
// its planned state. Uses a nil TaskService: with no held triggers Start never
// enqueues, and publishProgress no-ops without a bus.
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
	if first.Status != RunReady {
		t.Fatalf("first run status = %q, want ready (0 held triggers complete instantly)", first.Status)
	}

	second, err := svc.Start(ctx, wsID, ownerID, roundID)
	if err != nil {
		t.Fatal(err)
	}
	firstAfter, err := svc.GetRun(ctx, mustUUID(t, first.ID))
	if err != nil {
		t.Fatal(err)
	}
	if firstAfter.Status != RunCompleted || firstAfter.CompletedAt == nil {
		t.Fatalf("superseded run = %q (completed_at %v), want completed with timestamp", firstAfter.Status, firstAfter.CompletedAt)
	}
	active, err := svc.ActiveRun(ctx, roundID)
	if err != nil {
		t.Fatal(err)
	}
	if active == nil || active.ID != second.ID {
		t.Fatalf("active run = %+v, want the second run %s", active, second.ID)
	}

	if err := svc.DismissRun(ctx, wsID, ownerID, roundID); err != nil {
		t.Fatal(err)
	}
	active, err = svc.ActiveRun(ctx, roundID)
	if err != nil {
		t.Fatal(err)
	}
	if active != nil {
		t.Fatalf("active run after dismiss = %+v, want none", active)
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
