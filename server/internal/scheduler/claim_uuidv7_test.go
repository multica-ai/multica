package scheduler

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestClaimIDIsUUIDv7 covers the one converted INSERT that does not go through
// sqlc: tryClaim's hand-written statement in db_ops.go. Execution rows are
// append-only and written on every tick of every job, so their primary key is
// minted as a v7 for index locality — while lease_token stays random, because a
// claim token must not leak a guessable timestamp.
func TestClaimIDIsUUIDv7(t *testing.T) {
	pool := integrationPool(t)
	job := newTestJobSpec(uniqueJobName(t, "claim_uuidv7"))
	t.Cleanup(func() { cleanupExecutions(t, pool, job.Name) })

	ctx := context.Background()
	now, err := dbNow(ctx, pool)
	if err != nil {
		t.Fatalf("dbNow: %v", err)
	}
	planTime := FloorPlan(now, job.Cadence)

	c, err := tryClaim(ctx, pool, job, ScopeGlobal, planTime, now, "runner-uuidv7")
	if err != nil {
		t.Fatalf("tryClaim: %v", err)
	}
	if !c.Won {
		t.Fatal("expected a fresh insert to win the claim")
	}
	if v := c.ID.Version(); v != 7 {
		t.Errorf("sys_cron_executions.id is v%d (%s), want v7", v, c.ID)
	}
	if v := c.LeaseToken.Version(); v != 4 {
		t.Errorf("lease_token is v%d (%s), want a random v4 — a lease token must not embed its creation time", v, c.LeaseToken)
	}

	// The value the row holds, not just the value RETURNING echoed back.
	var stored uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM sys_cron_executions WHERE job_name = $1 AND plan_time = $2`,
		job.Name, planTime).Scan(&stored); err != nil {
		t.Fatalf("read back execution row: %v", err)
	}
	if stored != c.ID {
		t.Errorf("stored id %s != claimed id %s", stored, c.ID)
	}
}
