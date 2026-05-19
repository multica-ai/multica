package outbound

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Skipf("outbox: cannot parse DATABASE_URL: %v", err)
	}
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Skipf("outbox: could not create pool: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("outbox: database not reachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestDBNotificationStore_ReclaimStaleProcessing(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t)
	ctx := context.Background()
	store := NewDBNotificationStore(pool)
	suffix := time.Now().UnixNano()

	// Create a user so the FK on target_user_id is satisfied.
	var userID pgtype.UUID
	err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Outbox Test", fmt.Sprintf("outbox-test-%d@multica.ai", suffix)).Scan(&userID)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)

	connID := fmt.Sprintf("conn-outbox-%d", suffix)
	if _, err := pool.Exec(ctx, `
		INSERT INTO channel_connection (
			id, provider, display_name, enabled, is_default, config, secret_config, status
		) VALUES (
			$1, 'feishu', 'Outbox Test', true, false, '{}', '{}', 'connected'
		)
	`, connID); err != nil {
		t.Fatalf("create connection: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM channel_connection WHERE id = $1`, connID)

	// Insert a stale processing row.
	var id pgtype.UUID
	err = pool.QueryRow(ctx, `
		INSERT INTO channel_outbound_notification (
			provider, connection_id, event_kind, target_user_id, target_external_user_id,
			title, body, status, updated_at, next_attempt_at, aggregation_due_at
		) VALUES (
			'feishu', $2, 'test_event', $1, 'ext_1',
			'Title', 'Body', 'processing', now() - interval '10 minutes', now(), now()
		)
		RETURNING id
	`, userID, connID).Scan(&id)
	if err != nil {
		t.Fatalf("insert notification: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM channel_outbound_notification WHERE id = $1`, id)

	// Reclaim should succeed and return the row.
	reclaimed, err := store.ReclaimStaleProcessing(ctx, 10, 5*time.Minute, nil)
	if err != nil {
		t.Fatalf("ReclaimStaleProcessing: %v", err)
	}
	if len(reclaimed) != 1 {
		t.Fatalf("reclaimed count = %d, want 1", len(reclaimed))
	}

	// Row must still be 'processing' (not downgraded to pending).
	var status string
	var nextAttemptAt time.Time
	err = pool.QueryRow(ctx, `
		SELECT status, next_attempt_at
		FROM channel_outbound_notification
		WHERE id = $1
	`, id).Scan(&status, &nextAttemptAt)
	if err != nil {
		t.Fatalf("select status: %v", err)
	}
	if status != "processing" {
		t.Errorf("status = %q, want processing", status)
	}
	if !nextAttemptAt.After(time.Now()) {
		t.Errorf("next_attempt_at = %v, should be in the future", nextAttemptAt)
	}

	// ClaimDue should NOT see the row because it is still processing.
	claimed, err := store.ClaimDue(ctx, 10, nil)
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	for _, c := range claimed {
		if c.ID == id {
			t.Fatal("ClaimDue returned a row that was just reclaimed — duplicate processing risk")
		}
	}
}
