package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ERI-204 replaces the historical per-agent partial-commit batch protocol with
// one fail-closed ownership transaction. The regression contract is therefore
// that a successful multi-agent batch commits every returned task together.
func TestClaimTasksForRuntimes_CommitsMultiAgentBatchAtomically(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	svc := NewTaskService(db.New(pool), pool, nil, events.New())

	rt1, rt2 := batchClaimFixture(t, ctx, pool)
	ids := []pgtype.UUID{util.MustParseUUID(rt1), util.MustParseUUID(rt2)}
	result, err := svc.ClaimTasksForRuntimes(ctx, ids, 5)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(result.Tasks) != 2 {
		t.Fatalf("claimed %d tasks, want one per agent", len(result.Tasks))
	}
	for _, task := range result.Tasks {
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&status); err != nil {
			t.Fatalf("read task status: %v", err)
		}
		if status != "dispatched" {
			t.Fatalf("returned task %s status = %s, want dispatched", util.UUIDToString(task.ID), status)
		}
	}
}
