package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The recover-orphans drain must step PAST a page made entirely of poison
// (unresolvable-workspace) rows to reach the healthy rows behind them (MUL-4332
// review round 3, point 1). A poison row is skipped rather than failed, so it stays
// selectable; without the keyset cursor a plain "re-select the oldest N" drain would
// keep re-reading that same poison page forever and never fail the healthy rows
// behind it. The cursor advances over every candidate the page locked — poison
// included — so the next page moves on.
//
// A real agent_task_queue row always resolves its workspace via its agent, so (as in
// TestFailBulkTasksIsolatesPoisonRow) we present the two oldest rows to the fail path
// as poison by stripping their resolvable links in-memory; their id/created_at stay
// intact so the cursor still advances over them, and the DB rows are left untouched
// and un-failed, exactly as a genuinely unresolvable row would be.
func TestRecoverOrphansCursorStepsPastPoisonPage(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	_, _, agentID, issueID := seedAttributionFixture(t, pool)

	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime id: %v", err)
	}

	// Three orphans on the runtime with DISTINCT, increasing created_at so ordering
	// is by created_at (exercising the keyset created_at branch, complementing the
	// id-tiebreaker path). The two OLDEST are the poison page; the newest is healthy.
	seed := func(ageInterval string) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at)
			VALUES ($1, $2, $3, 'running', 0, now() - $4::interval)
			RETURNING id`, agentID, runtimeID, issueID, ageInterval).Scan(&id); err != nil {
			t.Fatalf("seed task: %v", err)
		}
		return id
	}
	poison1 := seed("3 minutes")
	poison2 := seed("2 minutes")
	healthy := seed("1 minute")
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = ANY($1::uuid[])`, []string{poison1, poison2, healthy})
		pool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id = ANY($1::uuid[])`, []string{poison1, poison2, healthy})
	})
	isPoison := map[string]bool{poison1: true, poison2: true}

	// Drain exactly as the handler does, but with a page size of 2 so the two poison
	// rows fill page 1 completely. If the cursor did not advance past them, page 2
	// would re-select the same poison page and the healthy row would never fail.
	const pageSize = 2
	var afterCreatedAt pgtype.Timestamptz
	var afterID pgtype.UUID
	drained := 0
	for page := 0; page < 5; page++ {
		var candidates []db.AgentTaskQueue
		failed, err := svc.FailBulkTasksWithEvents(ctx,
			func(qtx *db.Queries) ([]db.AgentTaskQueue, error) {
				c, e := qtx.SelectOrphanedTasksForRuntime(ctx, db.SelectOrphanedTasksForRuntimeParams{
					RuntimeID:      util.MustParseUUID(runtimeID),
					AfterCreatedAt: afterCreatedAt,
					AfterID:        afterID,
					MaxPerTick:     pageSize,
				})
				if e != nil {
					return nil, e
				}
				// Strip links on the poison rows so the fail path cannot resolve a
				// workspace and skips them (leaving them 'running'). id/created_at are
				// untouched, so the cursor still advances over them.
				for i := range c {
					if isPoison[util.UUIDToString(c[i].ID)] {
						c[i].AgentID = pgtype.UUID{}
						c[i].IssueID = pgtype.UUID{}
						c[i].ChatSessionID = pgtype.UUID{}
						c[i].AutopilotRunID = pgtype.UUID{}
					}
				}
				candidates = c
				return c, e
			},
			func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
				return qtx.FailAgentTasksByIDs(ctx, db.FailAgentTasksByIDsParams{
					Ids:           ids,
					Error:         pgtype.Text{String: "daemon restarted while task was in flight", Valid: true},
					FailureReason: pgtype.Text{String: "runtime_recovery", Valid: true},
				})
			})
		if err != nil {
			t.Fatalf("page %d: FailBulkTasksWithEvents: %v", page, err)
		}
		drained += len(failed)
		if len(candidates) < pageSize {
			break // short page → drained
		}
		last := candidates[len(candidates)-1]
		afterCreatedAt = last.CreatedAt
		afterID = last.ID
	}

	// The healthy row behind the poison page got failed with its event...
	if s := taskStatusForTest(t, pool, healthy); s != "failed" {
		t.Errorf("healthy row status = %q, want failed (cursor must step past the poison page)", s)
	}
	if n := subjectEventCount(t, pool, healthy); n != 1 {
		t.Errorf("healthy row events = %d, want 1", n)
	}
	// ...and the poison rows were left untouched — skipped, never failed, no event.
	for _, pid := range []string{poison1, poison2} {
		if s := taskStatusForTest(t, pool, pid); s != "running" {
			t.Errorf("poison row %s status = %q, want running (skipped, not failed)", pid, s)
		}
		if n := subjectEventCount(t, pool, pid); n != 0 {
			t.Errorf("poison row %s events = %d, want 0", pid, n)
		}
	}
	if drained != 1 {
		t.Errorf("drained = %d, want 1 (only the healthy row behind the poison page)", drained)
	}
}
