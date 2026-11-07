package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestSearchIndexChangePruneJobRemovesExpiredRowsAndRaisesMark(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()

	var previousMark *string
	_ = pool.QueryRow(ctx, `SELECT pruned_through_xid::text FROM search_index_prune_mark`).Scan(&previousMark)
	t.Cleanup(func() {
		if previousMark == nil {
			pool.Exec(context.Background(), `DELETE FROM search_index_prune_mark`)
			return
		}
		pool.Exec(context.Background(), `UPDATE search_index_prune_mark SET pruned_through_xid = $1::xid8`, *previousMark)
	})

	workspaceID := uuid.New()
	expiredID, freshID := uuid.New(), uuid.New()
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM search_index_change WHERE workspace_id = $1`, workspaceID)
	})
	planTime := time.Now().UTC().Truncate(time.Hour)
	var expiredXid string
	if err := pool.QueryRow(ctx, `
		INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_xid, changed_at)
		VALUES ('comment', $1, $2, pg_current_xact_id(), $3)
		RETURNING change_xid::text
	`, expiredID, workspaceID, planTime.Add(-SearchIndexChangeRetention-time.Hour)).Scan(&expiredXid); err != nil {
		t.Fatalf("insert expired change: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_xid, changed_at)
		VALUES ('comment', $1, $2, pg_current_xact_id(), $3)
	`, freshID, workspaceID, planTime.Add(-time.Hour)); err != nil {
		t.Fatalf("insert fresh change: %v", err)
	}

	job := SearchIndexChangePruneJob(db.New(pool))
	result, err := job.Handler(ctx, HandlerInput{Job: &job, Scope: ScopeGlobal, PlanTime: planTime})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if result.RowsAffected < 1 {
		t.Fatalf("prune removed %d rows, want at least the expired one", result.RowsAffected)
	}

	var remaining []uuid.UUID
	rows, err := pool.Query(ctx, `SELECT entity_id FROM search_index_change WHERE workspace_id = $1`, workspaceID)
	if err != nil {
		t.Fatalf("list remaining: %v", err)
	}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan remaining: %v", err)
		}
		remaining = append(remaining, id)
	}
	if len(remaining) != 1 || remaining[0] != freshID {
		t.Fatalf("remaining = %v, want only the fresh row %s", remaining, freshID)
	}

	var covered bool
	if err := pool.QueryRow(ctx, `
		SELECT pruned_through_xid >= $1::xid8 FROM search_index_prune_mark WHERE singleton
	`, expiredXid).Scan(&covered); err != nil {
		t.Fatalf("read prune mark: %v", err)
	}
	if !covered {
		t.Fatalf("prune mark must cover the pruned xid %s", expiredXid)
	}
}
