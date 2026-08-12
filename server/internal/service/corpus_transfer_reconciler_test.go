package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCorpusTransferReconcilerPersistsRetryAndTombstonePasses(t *testing.T) {
	t.Run("delete failure releases lease with retry", func(t *testing.T) {
		queries := &fakeCorpusCleanupQueries{claims: []db.CorpusTransfer{cleanupTransferRow(0)}}
		reconciler := &CorpusTransferReconciler{Queries: queries, Storage: &fakeCorpusCleanupDeleter{err: fmt.Errorf("store unavailable")}}
		reconciler.RunOnce(context.Background())
		if queries.retryCalls != 1 || queries.scheduleCalls != 0 || queries.completeCalls != 0 {
			t.Fatalf("retry/schedule/complete = %d/%d/%d", queries.retryCalls, queries.scheduleCalls, queries.completeCalls)
		}
		if got := time.Duration(queries.lastRetry.RetryAfter.Microseconds) * time.Microsecond; got != corpusTransferCleanupBackoffBase {
			t.Fatalf("retry backoff = %v, want %v", got, corpusTransferCleanupBackoffBase)
		}
		if queries.lastRetry.CleanupLastError != "store unavailable" || !queries.lastRetry.CleanupLeaseToken.Valid {
			t.Fatalf("retry persistence = %#v", queries.lastRetry)
		}
	})

	t.Run("successful delete schedules widening re-delete", func(t *testing.T) {
		queries := &fakeCorpusCleanupQueries{claims: []db.CorpusTransfer{cleanupTransferRow(0)}}
		reconciler := &CorpusTransferReconciler{Queries: queries, Storage: &fakeCorpusCleanupDeleter{}}
		reconciler.RunOnce(context.Background())
		if queries.scheduleCalls != 1 || queries.completeCalls != 0 {
			t.Fatalf("schedule/complete = %d/%d", queries.scheduleCalls, queries.completeCalls)
		}
		if got := time.Duration(queries.lastSchedule.RetryAfter.Microseconds) * time.Microsecond; got != corpusTransferCleanupRedelete[0] {
			t.Fatalf("first re-delete delay = %v, want %v", got, corpusTransferCleanupRedelete[0])
		}
	})

	t.Run("last successful pass completes durable cleanup", func(t *testing.T) {
		queries := &fakeCorpusCleanupQueries{claims: []db.CorpusTransfer{cleanupTransferRow(int32(len(corpusTransferCleanupRedelete)))}}
		reconciler := &CorpusTransferReconciler{Queries: queries, Storage: &fakeCorpusCleanupDeleter{}}
		reconciler.RunOnce(context.Background())
		if queries.completeCalls != 1 || queries.scheduleCalls != 0 {
			t.Fatalf("complete/schedule = %d/%d", queries.completeCalls, queries.scheduleCalls)
		}
		if !queries.lastComplete.CleanupLeaseToken.Valid {
			t.Fatal("completion was not fenced by a cleanup lease token")
		}
	})

	t.Run("last successful pass removes a deleted workspace ledger", func(t *testing.T) {
		queries := &fakeCorpusCleanupQueries{
			claims:       []db.CorpusTransfer{cleanupTransferRow(int32(len(corpusTransferCleanupRedelete)))},
			deleteOrphan: true,
		}
		reconciler := &CorpusTransferReconciler{Queries: queries, Storage: &fakeCorpusCleanupDeleter{}}
		reconciler.RunOnce(context.Background())
		if queries.deleteOrphanCalls != 1 || queries.completeCalls != 0 {
			t.Fatalf("delete-orphan/complete = %d/%d, want 1/0", queries.deleteOrphanCalls, queries.completeCalls)
		}
	})
}

func TestCorpusTransferReconcilerExpiresAbandonedUploadedTransfer(t *testing.T) {
	pool := newCancelFinalizePool(t)
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('corpus_transfer') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		t.Skip("corpus transfer migration is not installed")
	}
	workspaceID, transferID, actorID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	objectKey := "workspaces/" + workspaceID + "/corpus-transfers/" + transferID + "/archive.zip"
	digest := strings.Repeat("a", 64)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO corpus_transfer (
			id, workspace_id, actor_id, idempotency_key, object_key, manifest,
			manifest_sha256, expected_size_bytes, expected_sha256, state,
			expires_at, upload_started_at, uploaded_at
		) VALUES ($1, $2, $3, $4, $5, '{}'::jsonb, $6, 1, $6, 'uploaded', now() - interval '1 minute', now() - interval '2 minutes', now() - interval '90 seconds')
	`, transferID, workspaceID, actorID, uuid.NewString(), objectKey, digest); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM corpus_transfer WHERE workspace_id = $1 AND id = $2`, workspaceID, transferID)
	})

	deleter := &fakeCorpusCleanupDeleter{}
	reconciler := &CorpusTransferReconciler{Queries: db.New(pool), Storage: deleter}
	reconciler.RunOnce(context.Background())

	var state string
	var cleanupPending bool
	if err := pool.QueryRow(context.Background(), `
		SELECT state, cleanup_pending FROM corpus_transfer WHERE workspace_id = $1 AND id = $2
	`, workspaceID, transferID).Scan(&state, &cleanupPending); err != nil {
		t.Fatal(err)
	}
	if state != "expired" || !cleanupPending || !deleter.wasDeleted(objectKey) {
		t.Fatalf("state/pending/deleted = %s/%v/%v, want expired/true/true", state, cleanupPending, deleter.wasDeleted(objectKey))
	}
}

func TestCorpusTransferReconcilerPurgesExpiredConfirmedAndACKedContent(t *testing.T) {
	pool := newCancelFinalizePool(t)
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('corpus_transfer') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		t.Skip("corpus transfer migration is not installed")
	}
	workspaceID, actorID := uuid.NewString(), uuid.NewString()
	digest := strings.Repeat("b", 64)
	type seeded struct{ id, state, key string }
	rows := []seeded{
		{id: uuid.NewString(), state: "confirmed", key: "workspaces/" + workspaceID + "/corpus-transfers/confirmed/archive.zip"},
		{id: uuid.NewString(), state: "acked", key: "workspaces/" + workspaceID + "/corpus-transfers/acked/archive.zip"},
	}
	for _, row := range rows {
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO corpus_transfer (
				id, workspace_id, actor_id, idempotency_key, object_key, manifest,
				manifest_sha256, expected_size_bytes, expected_sha256, state,
				verified_size_bytes, verified_sha256, expires_at, confirmed_at
			) VALUES ($1, $2, $3, $4, $5, '{}'::jsonb, $6, 1, $6, $7, 1, $6, now() - interval '1 minute', now() - interval '2 minutes')
		`, row.id, workspaceID, actorID, uuid.NewString(), row.key, digest, row.state); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO corpus_transfer_ack (workspace_id, transfer_id, sink_id, confirmed_sha256, acknowledged_by)
		VALUES ($1, $2, 'retained-sink', $3, $4)
	`, workspaceID, rows[1].id, digest, actorID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM corpus_transfer_ack WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM corpus_transfer WHERE workspace_id = $1`, workspaceID)
	})

	queries := db.New(pool)
	for _, row := range rows {
		if _, err := queries.GetConfirmedCorpusTransferContent(context.Background(), db.GetConfirmedCorpusTransferContentParams{WorkspaceID: parseTestUUID(t, workspaceID), ID: parseTestUUID(t, row.id)}); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expired %s content lookup error before sweep = %v, want no rows", row.state, err)
		}
	}

	deleter := &fakeCorpusCleanupDeleter{}
	reconciler := &CorpusTransferReconciler{Queries: queries, Storage: deleter}
	reconciler.RunOnce(context.Background())

	for _, row := range rows {
		var state string
		var pending bool
		var verified string
		if err := pool.QueryRow(context.Background(), `
			SELECT state, cleanup_pending, verified_sha256 FROM corpus_transfer WHERE workspace_id = $1 AND id = $2
		`, workspaceID, row.id).Scan(&state, &pending, &verified); err != nil {
			t.Fatal(err)
		}
		if state != "purged" || !pending || verified != digest || !deleter.wasDeleted(row.key) {
			t.Fatalf("%s row = state %s, pending %v, verified %q, deleted %v", row.state, state, pending, verified, deleter.wasDeleted(row.key))
		}
		if _, err := queries.GetConfirmedCorpusTransferContent(context.Background(), db.GetConfirmedCorpusTransferContentParams{WorkspaceID: parseTestUUID(t, workspaceID), ID: parseTestUUID(t, row.id)}); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("purged content lookup error = %v, want no rows", err)
		}
	}
	var ackCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM corpus_transfer_ack WHERE workspace_id = $1`, workspaceID).Scan(&ackCount); err != nil || ackCount != 1 {
		t.Fatalf("retained ACK count/error = %d/%v", ackCount, err)
	}
}

func TestCorpusTransferReconcilerRunsImmediateStartupSweep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	queries := &fakeCorpusCleanupQueries{onClaim: cancel}
	done := make(chan struct{})
	go func() {
		(&CorpusTransferReconciler{Queries: queries, Storage: &fakeCorpusCleanupDeleter{}}).Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		cancel()
		t.Fatal("reconciler did not sweep immediately on startup")
	}
}

func TestCorpusTransferCleanupClaimsAreLeaseFenced(t *testing.T) {
	pool := newCancelFinalizePool(t)
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('corpus_transfer') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		t.Skip("corpus transfer migration is not installed")
	}
	workspaceID, transferID, actorID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	digest := strings.Repeat("c", 64)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO corpus_transfer (
			id, workspace_id, actor_id, idempotency_key, object_key, manifest,
			manifest_sha256, expected_size_bytes, expected_sha256, state,
			failure_code, cleanup_pending, cleanup_next_attempt_at, expires_at, failed_at
		) VALUES ($1, $2, $3, $4, $5, '{}'::jsonb, $6, 1, $6, 'failed', 'test_failure', true, now() - interval '1 minute', now() + interval '1 hour', now())
	`, transferID, workspaceID, actorID, uuid.NewString(), "workspaces/"+workspaceID+"/corpus-transfers/lease/archive.zip", digest); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM corpus_transfer WHERE workspace_id = $1 AND id = $2`, workspaceID, transferID)
	})

	queries := db.New(pool)
	lease := parseTestUUID(t, uuid.NewString())
	claimed, err := queries.ClaimNextCorpusTransferForCleanup(context.Background(), db.ClaimNextCorpusTransferForCleanupParams{
		CleanupLeaseToken: lease,
		CleanupLease:      pgInterval(time.Minute),
	})
	if err != nil || claimed.ID != parseTestUUID(t, transferID) {
		t.Fatalf("claim row/error = %#v/%v", claimed, err)
	}
	if _, err := queries.ClaimNextCorpusTransferForCleanup(context.Background(), db.ClaimNextCorpusTransferForCleanupParams{
		CleanupLeaseToken: parseTestUUID(t, uuid.NewString()),
		CleanupLease:      pgInterval(time.Minute),
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("second claim error = %v, want no rows", err)
	}
	wrongLease := parseTestUUID(t, uuid.NewString())
	if _, err := queries.CompleteCorpusTransferCleanup(context.Background(), db.CompleteCorpusTransferCleanupParams{
		WorkspaceID: parseTestUUID(t, workspaceID), ID: parseTestUUID(t, transferID), CleanupLeaseToken: wrongLease,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale completion error = %v, want no rows", err)
	}
	if _, err := queries.DeleteOrphanedCorpusTransferAfterCleanup(context.Background(), db.DeleteOrphanedCorpusTransferAfterCleanupParams{
		WorkspaceID: parseTestUUID(t, workspaceID), ID: parseTestUUID(t, transferID), CleanupLeaseToken: wrongLease,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale orphan deletion error = %v, want no rows", err)
	}
	if _, err := queries.DeleteOrphanedCorpusTransferAfterCleanup(context.Background(), db.DeleteOrphanedCorpusTransferAfterCleanupParams{
		WorkspaceID: parseTestUUID(t, workspaceID), ID: parseTestUUID(t, transferID), CleanupLeaseToken: lease,
	}); err != nil {
		t.Fatalf("lease owner orphan deletion: %v", err)
	}
}

func parseTestUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	parsed, err := uuid.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}
}

func cleanupTransferRow(pass int32) db.CorpusTransfer {
	return db.CorpusTransfer{
		ID:          pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		WorkspaceID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		ObjectKey:   "workspaces/test/corpus-transfers/test/archive.zip",
		State:       "failed", CleanupPending: true, CleanupPass: pass,
	}
}

type fakeCorpusCleanupQueries struct {
	mu                sync.Mutex
	claims            []db.CorpusTransfer
	retryCalls        int
	scheduleCalls     int
	completeCalls     int
	deleteOrphanCalls int
	deleteOrphan      bool
	onClaim           func()
	lastRetry         db.RetryCorpusTransferCleanupParams
	lastSchedule      db.ScheduleCorpusTransferCleanupPassParams
	lastComplete      db.CompleteCorpusTransferCleanupParams
}

func (f *fakeCorpusCleanupQueries) ClaimNextCorpusTransferForCleanup(_ context.Context, arg db.ClaimNextCorpusTransferForCleanupParams) (db.CorpusTransfer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.onClaim != nil {
		f.onClaim()
	}
	if len(f.claims) == 0 {
		return db.CorpusTransfer{}, pgx.ErrNoRows
	}
	row := f.claims[0]
	f.claims = f.claims[1:]
	row.CleanupLeaseToken = arg.CleanupLeaseToken
	return row, nil
}

func (f *fakeCorpusCleanupQueries) RetryCorpusTransferCleanup(_ context.Context, arg db.RetryCorpusTransferCleanupParams) (db.CorpusTransfer, error) {
	f.retryCalls++
	f.lastRetry = arg
	return db.CorpusTransfer{}, nil
}

func (f *fakeCorpusCleanupQueries) ScheduleCorpusTransferCleanupPass(_ context.Context, arg db.ScheduleCorpusTransferCleanupPassParams) (db.CorpusTransfer, error) {
	f.scheduleCalls++
	f.lastSchedule = arg
	return db.CorpusTransfer{}, nil
}

func (f *fakeCorpusCleanupQueries) CompleteCorpusTransferCleanup(_ context.Context, arg db.CompleteCorpusTransferCleanupParams) (db.CorpusTransfer, error) {
	f.completeCalls++
	f.lastComplete = arg
	return db.CorpusTransfer{}, nil
}

func (f *fakeCorpusCleanupQueries) DeleteOrphanedCorpusTransferAfterCleanup(_ context.Context, _ db.DeleteOrphanedCorpusTransferAfterCleanupParams) (db.CorpusTransfer, error) {
	f.deleteOrphanCalls++
	if f.deleteOrphan {
		return db.CorpusTransfer{}, nil
	}
	return db.CorpusTransfer{}, pgx.ErrNoRows
}

type fakeCorpusCleanupDeleter struct {
	mu          sync.Mutex
	err         error
	deletedKeys []string
}

func (f *fakeCorpusCleanupDeleter) DeleteObject(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedKeys = append(f.deletedKeys, key)
	return f.err
}

func (f *fakeCorpusCleanupDeleter) wasDeleted(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, deleted := range f.deletedKeys {
		if deleted == key {
			return true
		}
	}
	return false
}
