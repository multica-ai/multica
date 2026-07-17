package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// failNthBeginTxStarter fails the Nth Begin so a specific ClaimTask inside the
// batch loop errors AFTER earlier ones have committed.
type failNthBeginTxStarter struct {
	inner  TxStarter
	failOn int
	mu     sync.Mutex
	count  int
}

func (s *failNthBeginTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	s.mu.Lock()
	s.count++
	n := s.count
	s.mu.Unlock()
	if n == s.failOn {
		return nil, fmt.Errorf("injected begin failure #%d", n)
	}
	return s.inner.Begin(ctx)
}

// candidateFailDBTX passes every statement through EXCEPT the batch candidate
// SELECT, which it fails — simulating a candidate query error after step-2
// reclaims have already committed.
type candidateFailDBTX struct{ inner db.DBTX }

func (f candidateFailDBTX) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return f.inner.Exec(ctx, sql, args...)
}

func (f candidateFailDBTX) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	if strings.Contains(sql, "ListQueuedClaimCandidatesByRuntimes") {
		return nil, fmt.Errorf("injected candidate query failure")
	}
	return f.inner.Query(ctx, sql, args...)
}

func (f candidateFailDBTX) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return f.inner.QueryRow(ctx, sql, args...)
}

// TestClaimTasksForRuntimes_PartialSuccessOnSecondAgentClaimFailure is the
// MUL-4257 review regression: ClaimTask runs per agent in its own transaction,
// so when a later agent's claim fails the already-committed (dispatched) tasks
// must be RETURNED, not dropped with an error. Dropping them would 500 the
// handler and make the daemon HTTP-fall-back and double-claim the same slots.
func TestClaimTasksForRuntimes_PartialSuccessOnSecondAgentClaimFailure(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	tx := &failNthBeginTxStarter{inner: pool, failOn: 2} // 1st agent claims, 2nd errors
	svc := NewTaskService(db.New(pool), tx, nil, events.New())

	rt1, rt2 := batchClaimFixture(t, ctx, pool)
	ids := []pgtype.UUID{util.MustParseUUID(rt1), util.MustParseUUID(rt2)}

	claimed, err := svc.ClaimTasksForRuntimes(ctx, ids, 5)
	if err != nil {
		t.Fatalf("expected partial success (nil error), got %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed %d tasks, want 1 (the first agent's committed task, not dropped)", len(claimed))
	}

	// The one returned task must actually be dispatched in the DB (committed),
	// proving we returned a real claim rather than a phantom.
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, util.UUIDToString(claimed[0].ID)).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "dispatched" {
		t.Fatalf("returned task status = %s, want dispatched", status)
	}
}

// TestClaimTasksForRuntimes_PartialSuccessOnCandidateQueryFailureAfterReclaim
// covers the other partial-commit path: step-2 reclaims a stale dispatched task
// (committed), then the step-4 candidate SELECT errors. The reclaimed task must
// be returned, not dropped — same double-claim risk otherwise (MUL-4257).
func TestClaimTasksForRuntimes_PartialSuccessOnCandidateQueryFailureAfterReclaim(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	svc := NewTaskService(db.New(candidateFailDBTX{inner: pool}), pool, nil, events.New())

	rt1, rt2 := batchClaimFixture(t, ctx, pool)

	// Seed a stale dispatched task on rt1 (never started, dispatched long ago,
	// no live prepare lease) so step-2 reclaim picks it up.
	var workspaceID, ownerID, agentID string
	if err := pool.QueryRow(ctx, `SELECT workspace_id, owner_id FROM agent_runtime WHERE id = $1`, rt1).Scan(&workspaceID, &ownerID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM agent WHERE runtime_id = $1 LIMIT 1`, rt1).Scan(&agentID); err != nil {
		t.Fatalf("load agent: %v", err)
	}
	var issueID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'stale dispatched', 'in_progress', 'none', $2, 'member', 900123, 99)
		RETURNING id`, workspaceID, ownerID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	var staleTaskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, context, dispatched_at, started_at, prepare_lease_expires_at)
		VALUES ($1, $2, $3, 'dispatched', 0, '{}'::jsonb, now() - interval '1 hour', NULL, NULL)
		RETURNING id`, agentID, rt1, issueID).Scan(&staleTaskID); err != nil {
		t.Fatalf("create stale dispatched task: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM agent_task_queue WHERE id = $1`, staleTaskID)
		pool.Exec(c, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	ids := []pgtype.UUID{util.MustParseUUID(rt1), util.MustParseUUID(rt2)}
	claimed, err := svc.ClaimTasksForRuntimes(ctx, ids, 5)
	if err != nil {
		t.Fatalf("expected partial success (nil error) despite candidate query failure, got %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed %d tasks, want 1 (the reclaimed task, not dropped)", len(claimed))
	}
	if util.UUIDToString(claimed[0].ID) != staleTaskID {
		t.Fatalf("returned task = %s, want the reclaimed stale task %s", util.UUIDToString(claimed[0].ID), staleTaskID)
	}
}
