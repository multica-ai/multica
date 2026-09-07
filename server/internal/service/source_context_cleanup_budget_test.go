package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// sourceContextCleanupTestLock serializes every cleanup-oracle test against the
// other packages that sweep the same integration database.
const sourceContextCleanupTestLock = int64(0x53434f4e54455854)

// failingSourceContextObjectStore records the context each delete received and
// always fails, standing in for an object store that is down or throttling.
type failingSourceContextObjectStore struct {
	mu              sync.Mutex
	attempts        []string
	deadlineBounded []bool
	onDelete        func()
}

func (s *failingSourceContextObjectStore) KeyFromURL(rawURL string) string { return rawURL }

func (s *failingSourceContextObjectStore) DeleteObject(ctx context.Context, key string) error {
	s.mu.Lock()
	s.attempts = append(s.attempts, key)
	deadline, ok := ctx.Deadline()
	s.deadlineBounded = append(s.deadlineBounded, ok && time.Until(deadline) <= sourceContextObjectDeleteTimeout)
	onDelete := s.onDelete
	s.mu.Unlock()
	if onDelete != nil {
		onDelete()
	}
	return errors.New("object store unavailable")
}

func (s *failingSourceContextObjectStore) attemptCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.attempts)
}

// TestCleanupSourceContextObjectIntentsBoundsAttemptsNotSuccesses locks the
// batch semantics that made this sweeper dangerous: the loop used to stop after
// `limit` SUCCESSES, so a failing object store — the case that produces a
// backlog of due intents in the first place — let one round keep claiming rows
// for as long as due intents existed. The bound is on attempts.
func TestCleanupSourceContextObjectIntentsBoundsAttemptsNotSuccesses(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire cleanup test lock: %v", err)
	}
	if _, err := lockConn.Exec(ctx, `SELECT pg_advisory_lock($1)`, sourceContextCleanupTestLock); err != nil {
		lockConn.Release()
		t.Fatalf("lock source-context cleanup tests: %v", err)
	}
	t.Cleanup(func() {
		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, sourceContextCleanupTestLock)
		lockConn.Release()
	})

	workspaceID, _, _, _ := seedAttributionFixture(t, pool)
	workspaceUUID := util.MustParseUUID(workspaceID)

	// The whole fixture lives in a rolled-back transaction: the sweeper is
	// global, so seeding due rows outside one would let a developer server (or
	// the next test) claim them mid-assertion.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin isolated intent batch: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `LOCK TABLE issue_source_context_object_intent IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		t.Fatalf("lock object intent table: %v", err)
	}
	// Park any pre-existing due row so the claim order below is exactly the
	// three intents this test seeds.
	if _, err := tx.Exec(ctx, `
		UPDATE issue_source_context_object_intent
		SET next_attempt_at = now() + interval '1 hour'
		WHERE next_attempt_at <= now()
	`); err != nil {
		t.Fatalf("park pre-existing due intents: %v", err)
	}

	qtx := db.New(pool).WithTx(tx)
	keys := make([]string, 0, 3)
	for _, name := range []string{"a", "b", "c"} {
		attachmentID := dbid.NewV7()
		key := "source-context-budget/" + name + "-" + util.UUIDToString(attachmentID)
		keys = append(keys, key)
		if _, err := qtx.RecordSourceContextObjectIntent(ctx, db.RecordSourceContextObjectIntentParams{
			StorageKey: key, WorkspaceID: workspaceUUID, SourceContextID: dbid.NewV7(),
			AttachmentID: attachmentID, ObjectUrl: "https://objects.example/" + key,
		}); err != nil {
			t.Fatalf("record intent %s: %v", name, err)
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE issue_source_context_object_intent
		SET created_at = now() - interval '2 hours', next_attempt_at = '-infinity'::timestamptz
		WHERE storage_key = ANY($1::text[])
	`, keys); err != nil {
		t.Fatalf("age seeded intents: %v", err)
	}

	store := &failingSourceContextObjectStore{}
	svc := &TaskService{Queries: qtx, SourceContextStorage: store}
	cleaned, err := svc.CleanupSourceContextObjectIntents(ctx, 2)
	if err != nil {
		t.Fatalf("cleanup intents: %v", err)
	}
	if cleaned != 0 {
		t.Fatalf("cleaned intents = %d, want 0 while every delete fails", cleaned)
	}
	if got := store.attemptCount(); got != 2 {
		t.Fatalf("delete attempts = %d, want the batch size of 2 (a failing store must not extend the round)", got)
	}
	for i, bounded := range store.deadlineBounded {
		if !bounded {
			t.Fatalf("delete attempt %d ran without a bounded deadline", i)
		}
	}

	var due int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM issue_source_context_object_intent
		WHERE storage_key = ANY($1::text[]) AND next_attempt_at <= now()
	`, keys).Scan(&due); err != nil {
		t.Fatalf("count remaining due intents: %v", err)
	}
	if due != 1 {
		t.Fatalf("still-due seeded intents = %d, want 1 (the two attempted rows carry a retry backoff)", due)
	}
}

// TestCleanupAbandonedSourceContextsStopsWhenTheRoundBudgetEnds proves the
// second stage is both per-object bounded and budget-aware: it stops between
// captures instead of walking the rest of the batch with a dead context.
func TestCleanupAbandonedSourceContextsStopsWhenTheRoundBudgetEnds(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire cleanup test lock: %v", err)
	}
	if _, err := lockConn.Exec(ctx, `SELECT pg_advisory_lock($1)`, sourceContextCleanupTestLock); err != nil {
		lockConn.Release()
		t.Fatalf("lock source-context cleanup tests: %v", err)
	}
	t.Cleanup(func() {
		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, sourceContextCleanupTestLock)
		lockConn.Release()
	})

	workspaceID, userID, _, sourceIssueID := seedAttributionFixture(t, pool)
	workspaceUUID := util.MustParseUUID(workspaceID)
	userUUID := util.MustParseUUID(userID)
	sourceIssueUUID := util.MustParseUUID(sourceIssueID)

	contextIDs := make([]pgtype.UUID, 0, 2)
	for i, age := range []string{"120 days", "119 days"} {
		contextID := dbid.NewV7()
		contextIDs = append(contextIDs, contextID)
		if _, err := pool.Exec(ctx, `
			INSERT INTO issue_source_context (
				id, workspace_id, source_issue_id, anchor_comment_id, captured_by_user_id,
				snapshot_version, snapshot, capture_digest, state, captured_at
			) VALUES ($1, $2, $3, gen_random_uuid(), $4, 1, '{}'::jsonb, 'digest', 'abandoned',
				now() - $5::interval)
		`, contextID, workspaceUUID, sourceIssueUUID, userUUID, age); err != nil {
			t.Fatalf("insert abandoned context %d: %v", i, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO attachment (
				id, workspace_id, source_context_id, uploader_type, uploader_id,
				filename, url, content_type, size_bytes
			) VALUES (gen_random_uuid(), $1, $2, 'member', $3, 'clone.txt', $4, 'text/plain', 7)
		`, workspaceUUID, contextID, userUUID,
			"source-context-budget/abandoned-"+util.UUIDToString(contextID)); err != nil {
			t.Fatalf("insert abandoned clone %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM attachment WHERE source_context_id = ANY($1::uuid[])`, contextIDs)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_source_context WHERE id = ANY($1::uuid[])`, contextIDs)
	})

	roundCtx, cancelRound := context.WithCancel(ctx)
	defer cancelRound()
	// Model the round budget expiring inside the first object delete.
	store := &failingSourceContextObjectStore{onDelete: cancelRound}
	svc := &TaskService{Queries: db.New(pool), TxStarter: pool, SourceContextStorage: store}

	removed, err := svc.CleanupAbandonedSourceContexts(roundCtx, 10)
	if err != nil {
		t.Fatalf("abandoned cleanup after budget exhaustion = %v, want a clean stop", err)
	}
	if removed != 0 {
		t.Fatalf("removed captures = %d, want 0 when no object could be deleted", removed)
	}
	if got := store.attemptCount(); got != 1 {
		t.Fatalf("delete attempts = %d, want 1: the round must stop instead of walking the rest of the batch", got)
	}
	if len(store.deadlineBounded) != 1 || !store.deadlineBounded[0] {
		t.Fatalf("object delete ran without a per-object deadline: %#v", store.deadlineBounded)
	}

	var surviving int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM issue_source_context WHERE id = ANY($1::uuid[]) AND state = 'abandoned'
	`, contextIDs).Scan(&surviving); err != nil {
		t.Fatalf("count surviving abandoned captures: %v", err)
	}
	if surviving != 2 {
		t.Fatalf("surviving abandoned captures = %d, want 2 retained for the next round", surviving)
	}
}
