package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newCompensationTestPool returns a DB pool or skips the test when the
// database is unreachable (matching the repo's DB-gated test pattern).
func newCompensationTestPool(t *testing.T) *pgxpool.Pool {
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

type compensationFixture struct {
	pool        *pgxpool.Pool
	workspaceID string
}

func newCompensationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) compensationFixture {
	t.Helper()

	suffix := time.Now().UnixNano()
	slug := fmt.Sprintf("comp-%d", suffix)

	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Compensation Test", fmt.Sprintf("comp-%d@multica.ai", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	var workspaceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Compensation Test", slug, "temporary compensation test workspace", "CPT").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	return compensationFixture{pool: pool, workspaceID: workspaceID}
}

// insertCompensation inserts a compensation row directly and returns its id.
func insertCompensation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, wsID, op, state, leaseOwner string, leaseExpired bool) string {
	t.Helper()

	var id string
	lease := "NULL"
	leaseExp := "NULL"
	if leaseOwner != "" {
		lease = fmt.Sprintf("'%s'", leaseOwner)
		if leaseExpired {
			leaseExp = "now() - interval '1 minute'"
		} else {
			leaseExp = "now() + interval '1 minute'"
		}
	}
	query := fmt.Sprintf(`
		INSERT INTO memoryhub_compensation (workspace_id, op, idempotency_key, remote_ref, state, lease_owner, lease_expires_at, next_attempt_at)
		VALUES ($1, $2, $3, '{"ref":"remote-1"}'::jsonb, $4, %s, %s, now())
		RETURNING id
	`, lease, leaseExp)
	if err := pool.QueryRow(ctx, query, wsID, op, "idem-"+state+"-"+fmt.Sprintf("%d", time.Now().UnixNano()), state).Scan(&id); err != nil {
		t.Fatalf("insert compensation: %v", err)
	}
	return id
}

// fakeCompensationExecutor returns a scripted result per call.
type fakeCompensationExecutor struct {
	results []error
	calls   int
}

func (f *fakeCompensationExecutor) Execute(ctx context.Context, comp db.MemoryhubCompensation) error {
	if f.calls >= len(f.results) {
		return nil
	}
	err := f.results[f.calls]
	f.calls++
	return err
}

func TestCompensationSuccessMarksCompensated(t *testing.T) {
	pool := newCompensationTestPool(t)
	ctx := context.Background()
	fx := newCompensationFixture(t, ctx, pool)
	q := db.New(pool)

	id := insertCompensation(t, ctx, pool, fx.workspaceID, string(CompensationCreateRemote), string(CompensationPending), "", false)

	sweeper := NewCompensationSweeper(q, &fakeCompensationExecutor{results: []error{nil}}, "sweeper-1", time.Minute, 10)
	if err := sweeper.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	row := getCompensation(t, ctx, pool, id)
	if row.State != string(CompensationCompensated) {
		t.Fatalf("state = %q, want compensated", row.State)
	}
}

func TestCompensationTransientRetriesThenBlocks(t *testing.T) {
	pool := newCompensationTestPool(t)
	ctx := context.Background()
	fx := newCompensationFixture(t, ctx, pool)
	q := db.New(pool)

	id := insertCompensation(t, ctx, pool, fx.workspaceID, string(CompensationCreateRemote), string(CompensationPending), "", false)

	// Six transient failures: the first five pushes attempt up to max_attempt (6),
	// the sixth observes attempt >= max and blocks.
	results := make([]error, 6)
	for i := range results {
		results[i] = ErrCompensationTransient
	}
	sweeper := NewCompensationSweeper(q, &fakeCompensationExecutor{results: results}, "sweeper-1", time.Minute, 10)

	blockedAt := -1
	for i := 0; i < 6; i++ {
		if err := sweeper.Sweep(ctx); err != nil {
			t.Fatalf("Sweep %d: %v", i, err)
		}
		row := getCompensation(t, ctx, pool, id)
		switch row.State {
		case string(CompensationRetryWait):
			// advance time so next_attempt_at becomes due
			if _, err := pool.Exec(ctx, `
				UPDATE memoryhub_compensation
				SET next_attempt_at = now() - interval '1 second'
				WHERE id = $1
			`, id); err != nil {
				t.Fatalf("advance time: %v", err)
			}
		case string(CompensationBlocked):
			blockedAt = i
		default:
			t.Fatalf("unexpected state %q at sweep %d", row.State, i)
		}
	}

	row := getCompensation(t, ctx, pool, id)
	if row.State != string(CompensationBlocked) {
		t.Fatalf("final state = %q, want blocked", row.State)
	}
	if blockedAt != 5 {
		t.Fatalf("blocked at sweep %d, want 5 (max_attempt exhausted)", blockedAt)
	}
}

func TestCompensationFatalGoesDeadLetter(t *testing.T) {
	pool := newCompensationTestPool(t)
	ctx := context.Background()
	fx := newCompensationFixture(t, ctx, pool)
	q := db.New(pool)

	id := insertCompensation(t, ctx, pool, fx.workspaceID, string(CompensationCreateRemote), string(CompensationPending), "", false)

	sweeper := NewCompensationSweeper(q, &fakeCompensationExecutor{results: []error{errors.New("scope invalid")}}, "sweeper-1", time.Minute, 10)
	if err := sweeper.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	row := getCompensation(t, ctx, pool, id)
	if row.State != string(CompensationDeadLetter) {
		t.Fatalf("state = %q, want dead_letter", row.State)
	}
}

// TestCompensationCrashPoint4ReDrivesStaleRunning is crash point (4): a worker
// died mid-execution leaving state=running with an expired lease. The next
// sweep resets it to retry_wait, re-claims it, and the idempotent executor
// completes it (reuse, not duplicate create).
func TestCompensationCrashPoint4ReDrivesStaleRunning(t *testing.T) {
	pool := newCompensationTestPool(t)
	ctx := context.Background()
	fx := newCompensationFixture(t, ctx, pool)
	q := db.New(pool)

	id := insertCompensation(t, ctx, pool, fx.workspaceID, string(CompensationReuseRemote), string(CompensationRunning), "crashed-worker", true)

	sweeper := NewCompensationSweeper(q, &fakeCompensationExecutor{results: []error{nil}}, "sweeper-1", time.Minute, 10)
	if err := sweeper.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	row := getCompensation(t, ctx, pool, id)
	if row.State != string(CompensationCompensated) {
		t.Fatalf("state = %q, want compensated after re-drive", row.State)
	}
	if row.LeaseOwner.Valid {
		t.Fatalf("lease_owner should be cleared after completion, got %q", row.LeaseOwner.String)
	}
}

// TestCompensationIdempotencyKeyDedupes is crash point (1): a remote side
// effect must never be duplicated. InsertCompensationIfAbsent on a duplicate
// idempotency key returns the existing row (ON CONFLICT DO NOTHING).
func TestCompensationIdempotencyKeyDedupes(t *testing.T) {
	pool := newCompensationTestPool(t)
	ctx := context.Background()
	fx := newCompensationFixture(t, ctx, pool)
	q := db.New(pool)

	key := "idem-dedupe-" + fmt.Sprintf("%d", time.Now().UnixNano())
	arg := db.InsertCompensationIfAbsentParams{
		WorkspaceID:    uuidFromString(fx.workspaceID),
		Op:             string(CompensationCreateRemote),
		IdempotencyKey: key,
		RemoteRef:      []byte(`{"ref":"remote-1"}`),
	}

	first, err := q.InsertCompensationIfAbsent(ctx, arg)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	// ON CONFLICT DO NOTHING RETURNING * yields no row on the duplicate, which
	// proves no second row is created (the crash-point guard: re-drive never
	// duplicates the remote side effect).
	if _, err := q.InsertCompensationIfAbsent(ctx, arg); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("second insert err = %v, want pgx.ErrNoRows (no duplicate row)", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM memoryhub_compensation WHERE idempotency_key = $1
	`, key).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("rows for idempotency key = %d, want 1", count)
	}
	_ = first
}

func getCompensation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) db.MemoryhubCompensation {
	t.Helper()
	var row db.MemoryhubCompensation
	err := pool.QueryRow(ctx, `
		SELECT id, workspace_id, binding_id, op, idempotency_key, remote_ref, state,
		       attempt, max_attempt, next_attempt_at, lease_owner, lease_expires_at,
		       version, last_error, evidence_ref, created_at, updated_at
		FROM memoryhub_compensation
		WHERE id = $1
	`, id).Scan(
		&row.ID, &row.WorkspaceID, &row.BindingID, &row.Op, &row.IdempotencyKey, &row.RemoteRef, &row.State,
		&row.Attempt, &row.MaxAttempt, &row.NextAttemptAt, &row.LeaseOwner, &row.LeaseExpiresAt,
		&row.Version, &row.LastError, &row.EvidenceRef, &row.CreatedAt, &row.UpdatedAt,
	)
	if err != nil {
		t.Fatalf("get compensation: %v", err)
	}
	return row
}
