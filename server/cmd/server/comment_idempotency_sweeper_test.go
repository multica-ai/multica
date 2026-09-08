package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestSweepCommentIdempotencyWithBudgetDeletesOnlyExpiredRowsInBoundedBatch(t *testing.T) {
	now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	retention := 7 * 24 * time.Hour
	var gotCutoff pgtype.Timestamptz
	var gotMaxRows int32

	deleted, err := sweepCommentIdempotencyWithBudget(
		context.Background(),
		func(_ context.Context, cutoff pgtype.Timestamptz, maxRows int32) (int64, error) {
			gotCutoff = cutoff
			gotMaxRows = maxRows
			return 17, nil
		},
		now,
		retention,
		500,
	)
	if err != nil {
		t.Fatalf("sweepCommentIdempotencyWithBudget() error = %v", err)
	}
	if deleted != 17 {
		t.Fatalf("deleted = %d, want 17", deleted)
	}
	if !gotCutoff.Valid || !gotCutoff.Time.Equal(now.Add(-retention)) {
		t.Fatalf("cutoff = %#v, want %s", gotCutoff, now.Add(-retention))
	}
	if gotMaxRows != 500 {
		t.Fatalf("max rows = %d, want 500", gotMaxRows)
	}
}

func TestSweepCommentIdempotencyWithBudgetPropagatesCleanupFailure(t *testing.T) {
	wantErr := errors.New("database unavailable")
	_, err := sweepCommentIdempotencyWithBudget(
		context.Background(),
		func(context.Context, pgtype.Timestamptz, int32) (int64, error) {
			return 0, wantErr
		},
		time.Now().UTC(),
		7*24*time.Hour,
		500,
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func TestDeleteExpiredCommentIdempotencyRemovesOnlyRowsOutsideReplayWindow(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	issueID := createIssue(t, "comment idempotency retention test")
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	oldCommentID := insertCommentAt(t, issueID, "member", testUserID, "old replay", time.Now().UTC().Add(-8*24*time.Hour))
	newCommentID := insertCommentAt(t, issueID, "member", testUserID, "new replay", time.Now().UTC().Add(-24*time.Hour))
	oldKey := "retention-old-" + uuid.NewString()
	newKey := "retention-new-" + uuid.NewString()
	hash := strings.Repeat("a", 64)
	ctx := context.Background()
	_, err := testPool.Exec(ctx, `
		INSERT INTO comment_idempotency (workspace_id, idempotency_key, request_hash, comment_id, created_at)
		VALUES ($1, $2, $3, $4, now() - interval '8 days'),
		       ($1, $5, $3, $6, now() - interval '1 day')
	`, testWorkspaceID, oldKey, hash, oldCommentID, newKey, newCommentID)
	if err != nil {
		t.Fatalf("insert idempotency fixtures: %v", err)
	}

	deleted, err := db.New(testPool).DeleteExpiredCommentIdempotency(ctx, db.DeleteExpiredCommentIdempotencyParams{
		CreatedAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(-7 * 24 * time.Hour), Valid: true},
		Limit:     500,
	})
	if err != nil {
		t.Fatalf("delete expired idempotency rows: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}

	var oldCount, newCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM comment_idempotency WHERE workspace_id = $1 AND idempotency_key = $2`, testWorkspaceID, oldKey).Scan(&oldCount); err != nil {
		t.Fatalf("count old idempotency row: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM comment_idempotency WHERE workspace_id = $1 AND idempotency_key = $2`, testWorkspaceID, newKey).Scan(&newCount); err != nil {
		t.Fatalf("count new idempotency row: %v", err)
	}
	if oldCount != 0 || newCount != 1 {
		t.Fatalf("retention rows after cleanup = old:%d new:%d, want old:0 new:1", oldCount, newCount)
	}
}
