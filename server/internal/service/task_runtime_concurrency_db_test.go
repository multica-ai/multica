package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DB-backed coverage for the runtime-wide concurrency cap (GitHub #9626).
// Reuses the claim-decision fixture (one runtime, one agent) and adds more
// agents on the same runtime so the per-agent cap is never what blocks.

// addRuntimeAgentWithTask binds another agent to the fixture runtime and
// queues one task for it on its own issue, so per-(issue, agent)
// serialization cannot be the fence under test.
func (f *claimDecisionFixture) addRuntimeAgentWithTask(t *testing.T, name string) string {
	t.Helper()
	agentID := f.fx.Agent(t, name, util.UUIDToString(f.runtimeID), testutil.Cols{
		"owner_id":             f.fx.UserID,
		"max_concurrent_tasks": 5,
	})
	issueID := f.fx.Issue(t, name+" issue")
	return f.fx.Task(t, agentID, testutil.Cols{
		"runtime_id": util.UUIDToString(f.runtimeID),
		"issue_id":   issueID,
		"status":     "queued",
	})
}

func (f *claimDecisionFixture) setRuntimeCap(t *testing.T, limit *int32) {
	t.Helper()
	var v pgtype.Int4
	if limit != nil {
		v = pgtype.Int4{Int32: *limit, Valid: true}
	}
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE agent_runtime SET max_concurrent_tasks = $2 WHERE id = $1`, f.runtimeID, v,
	); err != nil {
		t.Fatalf("set runtime cap: %v", err)
	}
}

func int32Ptr(v int32) *int32 { return &v }

// With a cap of 1, two agents that each have spare per-agent capacity still
// get only one task between them; the second attempt is attributed to the
// runtime cap. Finishing the first task frees the slot.
func TestRuntimeConcurrencyCapLimitsAcrossAgents(t *testing.T) {
	f := newClaimDecisionFixture(t, 5)
	f.insertTask(t, "queued")
	f.addRuntimeAgentWithTask(t, "runtime-cap-agent-b")
	f.setRuntimeCap(t, int32Ptr(1))
	ctx := context.Background()

	claimed, err := f.svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{f.runtimeID}, 8)
	if err != nil {
		t.Fatalf("ClaimTasksForRuntimes: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed %d tasks, want 1 under runtime cap 1", len(claimed))
	}
	if got := claimDecisionCount(t, f.metrics, claimPathBatch, claimOutcomeRuntimeNoCapacity, claimReasonRuntimeConcurrencyCap); got != 1 {
		t.Fatalf("claim_decision_total{batch,runtime_no_capacity,runtime_concurrency_cap} = %v, want 1", got)
	}

	again, err := f.svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{f.runtimeID}, 8)
	if err != nil {
		t.Fatalf("second ClaimTasksForRuntimes: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second poll claimed %d tasks while the runtime is full, want 0", len(again))
	}

	if _, err := f.pool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, claimed[0].ID,
	); err != nil {
		t.Fatalf("complete first task: %v", err)
	}
	next, err := f.svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{f.runtimeID}, 8)
	if err != nil {
		t.Fatalf("third ClaimTasksForRuntimes: %v", err)
	}
	if len(next) != 1 {
		t.Fatalf("after the slot freed, claimed %d tasks, want 1", len(next))
	}
	if next[0].AgentID == claimed[0].AgentID {
		t.Fatalf("expected the other agent's task to be claimed next")
	}
}

// No cap (NULL, the default for every existing runtime) keeps today's
// behavior: each agent claims within its own limit.
func TestRuntimeConcurrencyCapNullIsUnlimited(t *testing.T) {
	f := newClaimDecisionFixture(t, 5)
	f.insertTask(t, "queued")
	f.addRuntimeAgentWithTask(t, "runtime-nocap-agent-b")

	claimed, err := f.svc.ClaimTasksForRuntimes(context.Background(), []pgtype.UUID{f.runtimeID}, 8)
	if err != nil {
		t.Fatalf("ClaimTasksForRuntimes: %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed %d tasks, want 2 with no runtime cap", len(claimed))
	}
}

// The count-then-claim window is serialized per runtime: while another
// transaction holds the runtime claim lock and has an uncommitted task start,
// a claim must wait for it and then see the slot as taken. Without the lock
// the claim would not see the uncommitted row and would overshoot the cap.
func TestRuntimeConcurrencyCapClaimWaitsForLockHolder(t *testing.T) {
	f := newClaimDecisionFixture(t, 5)
	f.insertTask(t, "queued")
	otherTaskID := f.addRuntimeAgentWithTask(t, "runtime-lock-agent-b")
	f.setRuntimeCap(t, int32Ptr(1))
	ctx := context.Background()

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := db.New(tx).LockRuntimeClaimCapacity(ctx, f.runtimeID); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'running', dispatched_at = now(), started_at = now() WHERE id = $1`,
		util.MustParseUUID(otherTaskID),
	); err != nil {
		t.Fatalf("start other task in tx: %v", err)
	}

	type result struct {
		task *db.AgentTaskQueue
		err  error
	}
	done := make(chan result, 1)
	go func() {
		task, err := f.svc.ClaimTask(ctx, f.agentID)
		done <- result{task, err}
	}()

	select {
	case r := <-done:
		t.Fatalf("claim returned (claimed=%v, err=%v) while another transaction held the runtime claim lock", r.task != nil, r.err)
	case <-time.After(300 * time.Millisecond):
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("ClaimTask: %v", r.err)
		}
		if r.task != nil {
			t.Fatalf("claimed %s although the runtime was full", util.UUIDToString(r.task.ID))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("claim did not finish after the lock holder committed")
	}
}

// Concurrent claims from different agents must not overshoot the cap.
func TestRuntimeConcurrencyCapConcurrentClaims(t *testing.T) {
	f := newClaimDecisionFixture(t, 5)
	f.insertTask(t, "queued")
	const extraAgents = 5
	for i := 0; i < extraAgents; i++ {
		f.addRuntimeAgentWithTask(t, "runtime-race-agent-"+string(rune('a'+i)))
	}
	f.setRuntimeCap(t, int32Ptr(2))

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		total int
		errs  []error
	)
	for i := 0; i < extraAgents+1; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, err := f.svc.ClaimTaskForRuntime(context.Background(), f.runtimeID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			if task != nil {
				total++
			}
		}()
	}
	wg.Wait()
	if len(errs) > 0 {
		t.Fatalf("concurrent claims returned errors: %v", errs)
	}

	var active int64
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_task_queue WHERE runtime_id = $1 AND status IN ('dispatched','running','waiting_local_directory')`,
		f.runtimeID,
	).Scan(&active); err != nil {
		t.Fatalf("count active: %v", err)
	}
	if active > 2 {
		t.Fatalf("runtime has %d active tasks, cap is 2", active)
	}
	if total > 2 {
		t.Fatalf("claimed %d tasks concurrently, cap is 2", total)
	}
}
