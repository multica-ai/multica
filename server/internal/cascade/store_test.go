package cascade

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// withTestPool connects to DATABASE_URL (or the multica dev default) and
// returns a pool. Tests skip when the DB is unreachable — same pattern
// internal/handler tests use. CI provides a real Postgres so the
// integration coverage runs there.
func withTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Skipf("no database: %v", err)
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
		return nil
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestStore_InsertRetrigger_FreshInsert(t *testing.T) {
	pool := withTestPool(t)
	if pool == nil {
		return
	}
	store := NewStore(pool)
	ctx := context.Background()

	p := RetriggerInsert{
		EventID:   uuid.New(),
		PRURL:     "https://github.com/owner/repo/pull/9001",
		PRNumber:  9001,
		HeadSHA:   "fresh-sha-1",
		EventType: "ci_failure",
	}
	id, err := store.InsertRetrigger(ctx, p)
	if err != nil {
		t.Fatalf("InsertRetrigger: %v", err)
	}
	if id == 0 {
		t.Fatal("returned id is 0")
	}

	// Cleanup so the row doesn't leak between test runs.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM cascade_retrigger WHERE id = $1`, id)
	})
}

func TestStore_InsertRetrigger_Idempotent(t *testing.T) {
	pool := withTestPool(t)
	if pool == nil {
		return
	}
	store := NewStore(pool)
	ctx := context.Background()

	eventID := uuid.New()
	p := RetriggerInsert{
		EventID:   eventID,
		PRURL:     "https://github.com/owner/repo/pull/9002",
		PRNumber:  9002,
		HeadSHA:   "idempotent-sha",
		EventType: "pr_merged",
	}

	id, err := store.InsertRetrigger(ctx, p)
	if err != nil {
		t.Fatalf("first InsertRetrigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM cascade_retrigger WHERE id = $1`, id)
	})

	// GitHub re-delivers the same event_id (which the adapter derives
	// deterministically from X-GitHub-Delivery). Second insert must
	// return ErrRetriggerAlreadyExists, NOT a unique-violation pgError,
	// so the router can treat it as a benign 200.
	if _, err := store.InsertRetrigger(ctx, p); !errors.Is(err, ErrRetriggerAlreadyExists) {
		t.Fatalf("second InsertRetrigger: err = %v, want ErrRetriggerAlreadyExists", err)
	}
}

func TestStore_InsertRetrigger_StampsFiredAt(t *testing.T) {
	// Callers may supply their own fired_at (e.g. backfill from
	// X-GitHub-Delivery header timestamp); when zero, the DB default
	// (now()) fills it in.
	pool := withTestPool(t)
	if pool == nil {
		return
	}
	store := NewStore(pool)
	ctx := context.Background()

	want := time.Date(2026, 5, 13, 12, 34, 56, 0, time.UTC)
	p := RetriggerInsert{
		EventID:   uuid.New(),
		PRURL:     "https://github.com/owner/repo/pull/9003",
		PRNumber:  9003,
		HeadSHA:   "ts-sha",
		EventType: "pr_review_change",
		FiredAt:   want,
	}
	id, err := store.InsertRetrigger(ctx, p)
	if err != nil {
		t.Fatalf("InsertRetrigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM cascade_retrigger WHERE id = $1`, id)
	})

	var got time.Time
	if err := pool.QueryRow(ctx, `SELECT fired_at FROM cascade_retrigger WHERE id = $1`, id).Scan(&got); err != nil {
		t.Fatalf("read back fired_at: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("fired_at not preserved: got %v, want %v", got, want)
	}
}
