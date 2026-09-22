package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// TestCreateRunOnlyTaskHalfOpenProbeIsExactlyOnce pins the F4 invariant against a
// real database: when a runtime's provider circuit is open but its reset window
// has already elapsed, the runtime is eligible again ONLY as a half-open probe,
// and two schedulers racing the same elapsed circuit must create EXACTLY ONE
// probe task — the second must be held, not pile a duplicate probe onto a
// provider that may still be refusing.
//
// It exercises the transactional lease (AcquireRuntimeProviderHalfOpenProbe +
// CreateAutopilotTask in one tx keyed to the new task id) that the pure selector
// unit tests cannot: the exactly-one row lock, the WHERE re-evaluation against
// the just-updated row, and the errProbeLeaseLost defer.
func TestCreateRunOnlyTaskHalfOpenProbeIsExactlyOnce(t *testing.T) {
	pool := sharedTestPool(t)
	ctx := context.Background()
	svc := &AutopilotService{Queries: db.New(pool), TxStarter: pool}

	suffix := time.Now().UnixNano()
	userID := insertBindingTeardownRow(t, pool, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Probe Race", fmt.Sprintf("probe-race-%d@multica.ai", suffix))
	workspaceID := insertBindingTeardownRow(t, pool, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, '', 'PRB') RETURNING id`,
		"Probe Race", fmt.Sprintf("probe-race-%d", suffix))
	execBindingTeardown(t, pool, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, workspaceID, userID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		_, _ = pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})

	runtimeID := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "probe runtime")
	agentID := insertAgentForBindingTeardown(t, pool, workspaceID, userID, "probe agent", runtimeID)

	// Circuit is open with a reset that already elapsed: not held, but every
	// dispatch onto it is a half-open probe and must win the lease first.
	past := time.Now().UTC().Add(-2 * time.Hour)
	execBindingTeardown(t, pool, `
		INSERT INTO runtime_provider_circuit (workspace_id, runtime_id, provider, state, reason, reset_at, opened_at)
		VALUES ($1, $2, 'binding_teardown_test', 'open', $3, $4, now())`,
		workspaceID, runtimeID, circuitClassQuota, past)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM runtime_provider_circuit WHERE runtime_id = $1`, runtimeID)
	})
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
	})

	rtUUID := util.MustParseUUID(runtimeID)
	agentUUID := util.MustParseUUID(agentID)
	chosen := runtimeCandidate{
		RuntimeID:   rtUUID,
		Provider:    "binding_teardown_test",
		ProbeWindow: true, // selectPoolRuntime sets this for an elapsed-open circuit
	}

	newParams := func() db.CreateAutopilotTaskParams {
		return db.CreateAutopilotTaskParams{
			ID:        dbid.NewV7(),
			AgentID:   agentUUID,
			RuntimeID: rtUUID,
			Priority:  0,
		}
	}

	// Two schedulers race the same elapsed circuit concurrently.
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		created  []db.AgentTaskQueue
		deferred int
		other    []error
	)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			params := newParams()
			<-start
			task, err := svc.createRunOnlyTask(ctx, chosen, params)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				created = append(created, task)
			case errors.Is(err, errProbeLeaseLost):
				deferred++
			default:
				other = append(other, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if len(other) > 0 {
		t.Fatalf("unexpected errors from concurrent probe dispatch: %v", other)
	}
	if len(created) != 1 || deferred != 1 {
		t.Fatalf("want exactly 1 task created and 1 deferred, got created=%d deferred=%d", len(created), deferred)
	}

	// Exactly one probe task landed in the queue.
	var taskCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE agent_id = $1`, agentID).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("want exactly 1 probe task row, got %d", taskCount)
	}

	// The circuit is half_open and its probe lease points at the winning task.
	var (
		state     string
		probeTask pgtype.UUID
		probeExp  pgtype.Timestamptz
	)
	if err := pool.QueryRow(ctx, `
		SELECT state, probe_task_id, probe_expires_at
		FROM runtime_provider_circuit WHERE runtime_id = $1`, runtimeID).Scan(&state, &probeTask, &probeExp); err != nil {
		t.Fatalf("read circuit: %v", err)
	}
	if state != "half_open" {
		t.Fatalf("circuit state=%q, want half_open after probe lease", state)
	}
	if util.UUIDToString(probeTask) != util.UUIDToString(created[0].ID) {
		t.Fatalf("probe_task_id=%s, want the created task %s", util.UUIDToString(probeTask), util.UUIDToString(created[0].ID))
	}
	if !probeExp.Valid || !probeExp.Time.After(time.Now().UTC()) {
		t.Fatalf("probe_expires_at=%v, want a future lease deadline", probeExp)
	}

	// A third dispatch while the lease is live is still held (no reclaim before expiry).
	if _, err := svc.createRunOnlyTask(ctx, chosen, newParams()); !errors.Is(err, errProbeLeaseLost) {
		t.Fatalf("third dispatch err=%v, want errProbeLeaseLost while lease is live", err)
	}
}
