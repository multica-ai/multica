package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// TestUpsertRuntimePauseCard verifies the FIR-2611 aggregation contract against
// a real Postgres: the first auth/quota pause of the day inserts one card; later
// pauses for the same runtime that day bump the same card (failed-run counter +
// reset time) instead of stacking new rows, and the circuit-open path raises the
// severity. Uses the shared runtime fixture (a runtime owned by the test user).
func TestUpsertRuntimePauseCard(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not reachable; skipping inbox card integration test")
	}
	ctx := context.Background()
	pool := runtimeAccountTestPool
	svc := &Service{Cerebro: cerebrodb.New(pool)} // Bus nil → publish no-ops

	recipient := runtimeAccountTestUserID
	wipe := func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM inbox_item WHERE recipient_id = $1 AND type = 'runtime_auto_paused'`, recipient)
	}
	// A sibling test (TestAutoPauseCircuitBreaker) shares the runtime fixture and
	// can leave aggregated cards behind if its own cleanup ever regresses; start
	// from a known-empty inbox so the counter assertions below are stable.
	wipe()
	t.Cleanup(wipe)

	now := time.Now().UTC()
	reset := now.Add(5 * time.Minute)

	// First pause → exactly one card, failed_runs = 1, attention severity.
	svc.upsertRuntimePauseCard(ctx, runtimeAccountTestRuntimeID, 1, reset, false)
	count, failed, severity, read := readCard(t, pool, recipient)
	if count != 1 {
		t.Fatalf("after first pause: want 1 card, got %d", count)
	}
	if failed != "1" {
		t.Fatalf("after first pause: want failed_runs=1, got %q", failed)
	}
	if severity != "attention" {
		t.Fatalf("after first pause: want severity=attention, got %q", severity)
	}

	// Mark read, then a second pause must reuse the row, bump to 2, resurface unread.
	if _, err := pool.Exec(ctx,
		`UPDATE inbox_item SET read = true WHERE recipient_id = $1 AND type = 'runtime_auto_paused'`, recipient); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	svc.upsertRuntimePauseCard(ctx, runtimeAccountTestRuntimeID, 2, reset.Add(time.Minute), false)
	count, failed, _, read = readCard(t, pool, recipient)
	if count != 1 {
		t.Fatalf("after second pause: want still 1 card (deduped), got %d", count)
	}
	if failed != "2" {
		t.Fatalf("after second pause: want failed_runs=2, got %q", failed)
	}
	if read {
		t.Fatalf("after second pause: card should be forced unread")
	}

	// Circuit-open pause raises severity to action_required.
	svc.upsertRuntimePauseCard(ctx, runtimeAccountTestRuntimeID, 6, time.Time{}, true)
	count, failed, severity, _ = readCard(t, pool, recipient)
	if count != 1 {
		t.Fatalf("after circuit-open pause: want still 1 card, got %d", count)
	}
	if failed != "3" {
		t.Fatalf("after circuit-open pause: want failed_runs=3, got %q", failed)
	}
	if severity != "action_required" {
		t.Fatalf("after circuit-open pause: want severity=action_required, got %q", severity)
	}
}

// readCard returns the count of runtime_auto_paused cards for the recipient plus
// the latest card's failed_runs, severity, and read flag (zero values when none).
func readCard(t *testing.T, pool *pgxpool.Pool, recipient pgtype.UUID) (int, string, string, bool) {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM inbox_item WHERE recipient_id = $1 AND type = 'runtime_auto_paused'`,
		recipient).Scan(&n); err != nil {
		t.Fatalf("count cards: %v", err)
	}
	if n == 0 {
		return 0, "", "", false
	}
	var failed, severity string
	var read bool
	if err := pool.QueryRow(context.Background(), `
		SELECT COALESCE(details->>'failed_runs',''), severity, read
		FROM inbox_item
		WHERE recipient_id = $1 AND type = 'runtime_auto_paused'
		ORDER BY created_at DESC
		LIMIT 1
	`, recipient).Scan(&failed, &severity, &read); err != nil {
		t.Fatalf("read card: %v", err)
	}
	return n, failed, severity, read
}
