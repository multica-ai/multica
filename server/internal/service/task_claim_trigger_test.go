package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func taskClaimTriggerPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	// Trigger DDL takes a table lock even when its WHEN clause targets one
	// agent. Keep it off the queue used by other packages' interleaving tests.
	return testutil.IsolatedDB(t, sharedTestPool(t).Config().ConnString())
}

// A sibling package can hold this lock while waiting for a unique-index
// conflict. Trigger DDL must not queue behind it and block that package's writer.
func TestClaimCapacityTriggerDoesNotBlockSharedQueue(t *testing.T) {
	shared := sharedTestPool(t)
	pool := taskClaimTriggerPool(t)
	agentID := createClaimCapacityFixture(t, context.Background(), pool)
	name := fmt.Sprintf("claim_capacity_isolation_%d", time.Now().UnixNano())

	// Finish database setup before holding a lock that can delay shared DDL.
	holder, err := shared.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.Background())
	if _, err := holder.Exec(context.Background(), `LOCK TABLE agent_task_queue IN ROW EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	createSleepTrigger(t, ctx, pool, name, name+"_fn", agentID)
	if _, err := pool.Exec(ctx, fmt.Sprintf("DROP TRIGGER %s ON agent_task_queue", quoteIdent(name))); err != nil {
		t.Fatalf("drop trigger while shared queue is locked: %v", err)
	}
}
