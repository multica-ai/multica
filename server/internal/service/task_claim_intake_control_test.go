package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type claimIntakeFixture struct {
	workspaceID string
	userID      string
	runtimeID   string
	agentID     string
	taskID      string
}

func newClaimIntakeFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) claimIntakeFixture {
	t.Helper()
	stamp := time.Now().UnixNano()
	f := claimIntakeFixture{}
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Claim Intake Test", fmt.Sprintf("claim-intake-%s-%d@multica.ai", suffix, stamp)).Scan(&f.userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, 'claim intake control test', 'CIC')
		RETURNING id
	`, "Claim Intake Test", fmt.Sprintf("claim-intake-%s-%d", suffix, stamp)).Scan(&f.workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, f.workspaceID, f.userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at, visibility, owner_id
		)
		VALUES ($1, $2, $3, 'cloud', 'claim_intake_test', 'online', 'test runtime', '{}'::jsonb, now(), 'private', $4)
		RETURNING id
	`, f.workspaceID, "daemon-"+suffix, "Claim Intake Runtime", f.userID).Scan(&f.runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 5, $4)
		RETURNING id
	`, f.workspaceID, "Claim Intake Agent", f.runtimeID, f.userID).Scan(&f.agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	var issueID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, $2, 'in_progress', 'none', $3, 'member', $4, 0)
		RETURNING id
	`, f.workspaceID, "claim intake queued task", f.userID, 970000+int(stamp%10000)).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, status, priority, context, runtime_id)
		VALUES ($1, $2, 'queued', 0, '{}'::jsonb, $3)
		RETURNING id
	`, f.agentID, issueID, f.runtimeID).Scan(&f.taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}

	t.Cleanup(func() {
		cleanup := context.Background()
		pool.Exec(cleanup, `DELETE FROM workspace_claim_intake_action WHERE workspace_id = $1`, f.workspaceID)
		pool.Exec(cleanup, `DELETE FROM workspace_claim_intake_control WHERE workspace_id = $1`, f.workspaceID)
		pool.Exec(cleanup, `DELETE FROM agent_task_queue WHERE agent_id = $1`, f.agentID)
		pool.Exec(cleanup, `DELETE FROM issue WHERE workspace_id = $1`, f.workspaceID)
		pool.Exec(cleanup, `DELETE FROM agent WHERE id = $1`, f.agentID)
		pool.Exec(cleanup, `DELETE FROM agent_runtime WHERE id = $1`, f.runtimeID)
		pool.Exec(cleanup, `DELETE FROM member WHERE workspace_id = $1`, f.workspaceID)
		pool.Exec(cleanup, `DELETE FROM workspace WHERE id = $1`, f.workspaceID)
		pool.Exec(cleanup, `DELETE FROM "user" WHERE id = $1`, f.userID)
	})
	return f
}

func seedClaimIntakeControl(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID, state string, generation int64) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspace_claim_intake_control (workspace_id, state, generation, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (workspace_id) DO UPDATE
		SET state = EXCLUDED.state,
		    generation = EXCLUDED.generation,
		    updated_at = EXCLUDED.updated_at
	`, workspaceID, state, generation); err != nil {
		t.Fatalf("seed claim intake control: %v", err)
	}
}

func assertTaskStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID, want string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&got); err != nil {
		t.Fatalf("load task status: %v", err)
	}
	if got != want {
		t.Fatalf("task status = %q, want %q", got, want)
	}
}

// claimIntakeLockBarrier pauses a claim transaction immediately after it has
// acquired the authoritative Workspace control row's shared lock. Tests can
// then start a pause mutation and prove acknowledgement waits for the claim's
// ownership transaction to commit.
type claimIntakeLockBarrier struct {
	pool    *pgxpool.Pool
	locked  chan struct{}
	release chan struct{}
	once    sync.Once
}

func newClaimIntakeLockBarrier(pool *pgxpool.Pool) *claimIntakeLockBarrier {
	return &claimIntakeLockBarrier{
		pool:    pool,
		locked:  make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (s *claimIntakeLockBarrier) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &claimIntakeBarrierTx{
		Tx:      tx,
		locked:  s.locked,
		release: s.release,
		once:    &s.once,
	}, nil
}

type claimIntakeBarrierTx struct {
	pgx.Tx
	locked  chan struct{}
	release chan struct{}
	once    *sync.Once
}

var errClaimIntakeControlUnreadable = errors.New("claim intake control unreadable")

type claimIntakeControlReadFailureStarter struct {
	pool *pgxpool.Pool
}

func (s claimIntakeControlReadFailureStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &claimIntakeControlReadFailureTx{Tx: tx}, nil
}

type claimIntakeControlReadFailureTx struct {
	pgx.Tx
}

func (t *claimIntakeControlReadFailureTx) Query(
	ctx context.Context,
	sql string,
	args ...any,
) (pgx.Rows, error) {
	if strings.Contains(sql, "LockWorkspaceClaimIntakeControlsForClaim") {
		return nil, errClaimIntakeControlUnreadable
	}
	return t.Tx.Query(ctx, sql, args...)
}

func (t *claimIntakeBarrierTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := t.Tx.Query(ctx, sql, args...)
	if err != nil || !strings.Contains(sql, "LockWorkspaceClaimIntakeControlsForClaim") {
		return rows, err
	}
	t.once.Do(func() { close(t.locked) })
	select {
	case <-t.release:
		return rows, nil
	case <-ctx.Done():
		rows.Close()
		return nil, ctx.Err()
	}
}

func TestClaimTaskForRuntime_TwoConsumersOwnQueuedTaskExactlyOnce(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	f := newClaimIntakeFixture(t, ctx, pool, "two-consumers")
	seedClaimIntakeControl(t, ctx, pool, f.workspaceID, "resumed", 3)

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	start := make(chan struct{})
	type claimResult struct {
		consumerID string
		taskID     string
		err        error
	}
	results := make(chan claimResult, 2)
	for _, consumerID := range []string{"consumer-a", "consumer-b"} {
		go func(consumerID string) {
			<-start
			result, err := svc.ClaimTaskForRuntimeAsConsumer(
				ctx,
				util.MustParseUUID(f.runtimeID),
				consumerID,
			)
			claimedTaskID := ""
			if err == nil && result.Task != nil {
				claimedTaskID = util.UUIDToString(result.Task.ID)
			}
			results <- claimResult{
				consumerID: consumerID,
				taskID:     claimedTaskID,
				err:        err,
			}
		}(consumerID)
	}
	close(start)

	var claimedBy string
	claimCount := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("claim for %s: %v", result.consumerID, result.err)
		}
		if result.taskID == "" {
			continue
		}
		if result.taskID != f.taskID {
			t.Fatalf(
				"consumer %s claimed task %s, want %s",
				result.consumerID,
				result.taskID,
				f.taskID,
			)
		}
		claimedBy = result.consumerID
		claimCount++
	}
	if claimCount != 1 {
		t.Fatalf("claim count = %d, want exactly 1", claimCount)
	}

	var status, consumerID string
	var generation *int64
	if err := pool.QueryRow(ctx, `
		SELECT status, claim_consumer_id, claim_intake_generation
		FROM agent_task_queue
		WHERE id = $1
	`, f.taskID).Scan(&status, &consumerID, &generation); err != nil {
		t.Fatalf("load claimed task ownership: %v", err)
	}
	if status != "dispatched" || consumerID != claimedBy {
		t.Fatalf(
			"persisted ownership = %s/%s, want dispatched/%s",
			status,
			consumerID,
			claimedBy,
		)
	}
	if generation == nil || *generation != 3 {
		t.Fatalf("claim generation = %v, want 3", generation)
	}
}

func TestClaimTaskForRuntime_PauseAcknowledgementOrdersAfterInFlightClaim(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	f := newClaimIntakeFixture(t, ctx, pool, "pause-race-singular")
	seedClaimIntakeControl(t, ctx, pool, f.workspaceID, "resumed", 0)

	barrier := newClaimIntakeLockBarrier(pool)
	svc := NewTaskService(db.New(pool), barrier, nil, events.New())
	claimDone := make(chan error, 1)
	go func() {
		result, err := svc.ClaimTaskForRuntimeAsConsumer(
			ctx,
			util.MustParseUUID(f.runtimeID),
			"consumer-race",
		)
		if err == nil && (result.Task == nil || util.UUIDToString(result.Task.ID) != f.taskID) {
			err = fmt.Errorf("claimed task = %+v, want %s", result.Task, f.taskID)
		}
		claimDone <- err
	}()

	select {
	case <-barrier.locked:
	case <-time.After(5 * time.Second):
		t.Fatal("claim did not acquire Workspace control lock")
	}

	pauseAcquired := make(chan error, 1)
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			pauseAcquired <- err
			return
		}
		defer tx.Rollback(ctx)
		qtx := db.New(pool).WithTx(tx)
		control, err := qtx.LockWorkspaceClaimIntakeControlForMutation(
			ctx,
			util.MustParseUUID(f.workspaceID),
		)
		if err != nil {
			pauseAcquired <- err
			return
		}
		_, err = tx.Exec(ctx, `
			UPDATE workspace_claim_intake_control
			SET state = 'paused', generation = $2, updated_at = now()
			WHERE workspace_id = $1
		`, f.workspaceID, control.Generation+1)
		if err == nil {
			err = tx.Commit(ctx)
		}
		pauseAcquired <- err
	}()

	select {
	case err := <-pauseAcquired:
		t.Fatalf("pause acquired mutation lock before in-flight claim committed: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(barrier.release)
	if err := <-claimDone; err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := <-pauseAcquired; err != nil {
		t.Fatalf("pause: %v", err)
	}

	assertTaskStatus(t, ctx, pool, f.taskID, "dispatched")
	var state string
	var generation int64
	if err := pool.QueryRow(ctx, `
		SELECT state, generation
		FROM workspace_claim_intake_control
		WHERE workspace_id = $1
	`, f.workspaceID).Scan(&state, &generation); err != nil {
		t.Fatalf("load paused state: %v", err)
	}
	if state != "paused" || generation != 1 {
		t.Fatalf("control = %s/%d, want paused/1", state, generation)
	}

	result, err := svc.ClaimTaskForRuntimeAsConsumer(
		ctx,
		util.MustParseUUID(f.runtimeID),
		"consumer-after-pause",
	)
	if err != nil {
		t.Fatalf("post-pause claim: %v", err)
	}
	if result.Task != nil || !result.Paused || result.Generation != 1 {
		t.Fatalf("post-pause claim = %+v, want paused generation 1 with no task", result)
	}
}

func TestClaimTasksForRuntimes_PauseAcknowledgementOrdersAfterInFlightBatch(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	first := newClaimIntakeFixture(t, ctx, pool, "pause-race-batch-a")
	second := newClaimIntakeFixture(t, ctx, pool, "pause-race-batch-b")
	seedClaimIntakeControl(t, ctx, pool, first.workspaceID, "resumed", 0)
	seedClaimIntakeControl(t, ctx, pool, second.workspaceID, "resumed", 0)

	barrier := newClaimIntakeLockBarrier(pool)
	svc := NewTaskService(db.New(pool), barrier, nil, events.New())
	claimDone := make(chan error, 1)
	go func() {
		result, err := svc.ClaimTasksForRuntimesAsConsumer(
			ctx,
			[]pgtype.UUID{
				util.MustParseUUID(first.runtimeID),
				util.MustParseUUID(second.runtimeID),
			},
			2,
			"consumer-batch-race",
		)
		if err == nil && len(result.Tasks) != 2 {
			err = fmt.Errorf("claimed %d tasks, want 2", len(result.Tasks))
		}
		claimDone <- err
	}()

	select {
	case <-barrier.locked:
	case <-time.After(5 * time.Second):
		t.Fatal("batch claim did not acquire Workspace control locks")
	}

	pauseAcquired := make(chan error, 1)
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			pauseAcquired <- err
			return
		}
		defer tx.Rollback(ctx)
		qtx := db.New(pool).WithTx(tx)
		control, err := qtx.LockWorkspaceClaimIntakeControlForMutation(
			ctx,
			util.MustParseUUID(second.workspaceID),
		)
		if err != nil {
			pauseAcquired <- err
			return
		}
		_, err = tx.Exec(ctx, `
			UPDATE workspace_claim_intake_control
			SET state = 'paused', generation = $2, updated_at = now()
			WHERE workspace_id = $1
		`, second.workspaceID, control.Generation+1)
		if err == nil {
			err = tx.Commit(ctx)
		}
		pauseAcquired <- err
	}()

	select {
	case err := <-pauseAcquired:
		t.Fatalf("pause acquired mutation lock before in-flight batch committed: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(barrier.release)
	if err := <-claimDone; err != nil {
		t.Fatalf("batch claim: %v", err)
	}
	if err := <-pauseAcquired; err != nil {
		t.Fatalf("pause: %v", err)
	}
	assertTaskStatus(t, ctx, pool, first.taskID, "dispatched")
	assertTaskStatus(t, ctx, pool, second.taskID, "dispatched")

	result, err := svc.ClaimTasksForRuntimesAsConsumer(
		ctx,
		[]pgtype.UUID{
			util.MustParseUUID(first.runtimeID),
			util.MustParseUUID(second.runtimeID),
		},
		2,
		"consumer-batch-after-pause",
	)
	if err != nil {
		t.Fatalf("post-pause batch claim: %v", err)
	}
	if len(result.Tasks) != 0 {
		t.Fatalf("post-pause batch claimed tasks: %+v", result.Tasks)
	}
	if len(result.PausedWorkspaces) != 1 ||
		util.UUIDToString(result.PausedWorkspaces[0].WorkspaceID) != second.workspaceID ||
		result.PausedWorkspaces[0].Generation != 1 {
		t.Fatalf("paused workspaces = %+v, want %s generation 1", result.PausedWorkspaces, second.workspaceID)
	}
}

func TestClaimTaskForRuntime_PauseAcknowledgementOrdersAfterStaleReclaim(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	f := newClaimIntakeFixture(t, ctx, pool, "pause-race-stale")
	seedClaimIntakeControl(t, ctx, pool, f.workspaceID, "resumed", 6)
	if _, err := pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'dispatched',
		    dispatched_at = now() - interval '1 hour',
		    prepare_lease_expires_at = NULL,
		    claim_intake_generation = 5,
		    claim_consumer_id = 'consumer-before-race'
		WHERE id = $1
	`, f.taskID); err != nil {
		t.Fatalf("make task stale: %v", err)
	}

	barrier := newClaimIntakeLockBarrier(pool)
	svc := NewTaskService(db.New(pool), barrier, nil, events.New())
	reclaimDone := make(chan error, 1)
	go func() {
		result, err := svc.ClaimTaskForRuntimeAsConsumer(
			ctx,
			util.MustParseUUID(f.runtimeID),
			"consumer-stale-race",
		)
		if err == nil && (result.Task == nil || util.UUIDToString(result.Task.ID) != f.taskID) {
			err = fmt.Errorf("reclaimed task = %+v, want %s", result.Task, f.taskID)
		}
		reclaimDone <- err
	}()

	select {
	case <-barrier.locked:
	case <-time.After(5 * time.Second):
		t.Fatal("stale reclaim did not acquire Workspace control lock")
	}

	pauseAcquired := make(chan error, 1)
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			pauseAcquired <- err
			return
		}
		defer tx.Rollback(ctx)
		qtx := db.New(pool).WithTx(tx)
		control, err := qtx.LockWorkspaceClaimIntakeControlForMutation(
			ctx,
			util.MustParseUUID(f.workspaceID),
		)
		if err != nil {
			pauseAcquired <- err
			return
		}
		_, err = tx.Exec(ctx, `
			UPDATE workspace_claim_intake_control
			SET state = 'paused', generation = $2, updated_at = now()
			WHERE workspace_id = $1
		`, f.workspaceID, control.Generation+1)
		if err == nil {
			err = tx.Commit(ctx)
		}
		pauseAcquired <- err
	}()

	select {
	case err := <-pauseAcquired:
		t.Fatalf("pause acquired mutation lock before stale reclaim committed: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(barrier.release)
	if err := <-reclaimDone; err != nil {
		t.Fatalf("stale reclaim: %v", err)
	}
	if err := <-pauseAcquired; err != nil {
		t.Fatalf("pause: %v", err)
	}

	var consumerID string
	var generation int64
	if err := pool.QueryRow(ctx, `
		SELECT claim_consumer_id, claim_intake_generation
		FROM agent_task_queue
		WHERE id = $1
	`, f.taskID).Scan(&consumerID, &generation); err != nil {
		t.Fatalf("load reclaimed task: %v", err)
	}
	if consumerID != "consumer-stale-race" || generation != 6 {
		t.Fatalf("reclaimed ownership = %q/%d, want consumer-stale-race/6", consumerID, generation)
	}

	result, err := svc.ClaimTaskForRuntimeAsConsumer(
		ctx,
		util.MustParseUUID(f.runtimeID),
		"consumer-after-stale-pause",
	)
	if err != nil {
		t.Fatalf("post-pause stale claim: %v", err)
	}
	if result.Task != nil || !result.Paused || result.Generation != 7 {
		t.Fatalf("post-pause stale claim = %+v, want paused generation 7", result)
	}
}

func TestClaimTasksForRuntimes_PauseAcknowledgementOrdersAfterInFlightStaleBatch(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	first := newClaimIntakeFixture(t, ctx, pool, "pause-race-stale-batch-a")
	second := newClaimIntakeFixture(t, ctx, pool, "pause-race-stale-batch-b")
	seedClaimIntakeControl(t, ctx, pool, first.workspaceID, "resumed", 10)
	seedClaimIntakeControl(t, ctx, pool, second.workspaceID, "resumed", 20)

	for _, fixture := range []claimIntakeFixture{first, second} {
		if _, err := pool.Exec(ctx, `
			UPDATE agent_task_queue
			SET status = 'dispatched',
			    dispatched_at = now() - interval '1 hour',
			    prepare_lease_expires_at = NULL,
			    claim_intake_generation = $2,
			    claim_consumer_id = 'consumer-before-stale-batch'
			WHERE id = $1
		`, fixture.taskID, map[string]int64{
			first.taskID:  9,
			second.taskID: 19,
		}[fixture.taskID]); err != nil {
			t.Fatalf("make task %s stale: %v", fixture.taskID, err)
		}
	}

	barrier := newClaimIntakeLockBarrier(pool)
	svc := NewTaskService(db.New(pool), barrier, nil, events.New())
	reclaimDone := make(chan error, 1)
	go func() {
		result, err := svc.ClaimTasksForRuntimesAsConsumer(
			ctx,
			[]pgtype.UUID{
				util.MustParseUUID(first.runtimeID),
				util.MustParseUUID(second.runtimeID),
			},
			2,
			"consumer-stale-batch-race",
		)
		if err == nil && len(result.Tasks) != 2 {
			err = fmt.Errorf("reclaimed %d tasks, want 2", len(result.Tasks))
		}
		reclaimDone <- err
	}()

	select {
	case <-barrier.locked:
	case <-time.After(5 * time.Second):
		t.Fatal("stale batch reclaim did not acquire Workspace control locks")
	}

	pauseAcquired := make(chan error, 1)
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			pauseAcquired <- err
			return
		}
		defer tx.Rollback(ctx)
		qtx := db.New(pool).WithTx(tx)
		control, err := qtx.LockWorkspaceClaimIntakeControlForMutation(
			ctx,
			util.MustParseUUID(second.workspaceID),
		)
		if err != nil {
			pauseAcquired <- err
			return
		}
		_, err = tx.Exec(ctx, `
			UPDATE workspace_claim_intake_control
			SET state = 'paused', generation = $2, updated_at = now()
			WHERE workspace_id = $1
		`, second.workspaceID, control.Generation+1)
		if err == nil {
			err = tx.Commit(ctx)
		}
		pauseAcquired <- err
	}()

	select {
	case err := <-pauseAcquired:
		t.Fatalf("pause acquired mutation lock before stale batch committed: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(barrier.release)
	if err := <-reclaimDone; err != nil {
		t.Fatalf("stale batch reclaim: %v", err)
	}
	if err := <-pauseAcquired; err != nil {
		t.Fatalf("pause: %v", err)
	}

	for _, fixture := range []struct {
		claimIntakeFixture
		generation int64
	}{
		{claimIntakeFixture: first, generation: 10},
		{claimIntakeFixture: second, generation: 20},
	} {
		var consumerID string
		var generation int64
		if err := pool.QueryRow(ctx, `
			SELECT claim_consumer_id, claim_intake_generation
			FROM agent_task_queue
			WHERE id = $1
		`, fixture.taskID).Scan(&consumerID, &generation); err != nil {
			t.Fatalf("load reclaimed task %s: %v", fixture.taskID, err)
		}
		if consumerID != "consumer-stale-batch-race" || generation != fixture.generation {
			t.Fatalf(
				"reclaimed task %s ownership = %q/%d, want consumer-stale-batch-race/%d",
				fixture.taskID,
				consumerID,
				generation,
				fixture.generation,
			)
		}
	}

	result, err := svc.ClaimTasksForRuntimesAsConsumer(
		ctx,
		[]pgtype.UUID{
			util.MustParseUUID(first.runtimeID),
			util.MustParseUUID(second.runtimeID),
		},
		2,
		"consumer-after-stale-batch-pause",
	)
	if err != nil {
		t.Fatalf("post-pause stale batch claim: %v", err)
	}
	if len(result.Tasks) != 0 ||
		len(result.PausedWorkspaces) != 1 ||
		util.UUIDToString(result.PausedWorkspaces[0].WorkspaceID) != second.workspaceID ||
		result.PausedWorkspaces[0].Generation != 21 {
		t.Fatalf("post-pause stale batch result = %+v", result)
	}
}

func TestClaimTasksForRuntimes_MixedWorkspacePauseRaceKeepsActiveWorkspaceClaimable(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	active := newClaimIntakeFixture(t, ctx, pool, "pause-race-mixed-active")
	pausing := newClaimIntakeFixture(t, ctx, pool, "pause-race-mixed-pausing")
	seedClaimIntakeControl(t, ctx, pool, active.workspaceID, "resumed", 3)
	seedClaimIntakeControl(t, ctx, pool, pausing.workspaceID, "resumed", 4)

	barrier := newClaimIntakeLockBarrier(pool)
	svc := NewTaskService(db.New(pool), barrier, nil, events.New())
	claimDone := make(chan error, 1)
	go func() {
		result, err := svc.ClaimTasksForRuntimesAsConsumer(
			ctx,
			[]pgtype.UUID{
				util.MustParseUUID(active.runtimeID),
				util.MustParseUUID(pausing.runtimeID),
			},
			2,
			"consumer-mixed-before-pause",
		)
		if err == nil && len(result.Tasks) != 2 {
			err = fmt.Errorf("claimed %d tasks, want 2", len(result.Tasks))
		}
		claimDone <- err
	}()

	select {
	case <-barrier.locked:
	case <-time.After(5 * time.Second):
		t.Fatal("mixed claim did not acquire Workspace control locks")
	}

	pauseAcquired := make(chan error, 1)
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			pauseAcquired <- err
			return
		}
		defer tx.Rollback(ctx)
		qtx := db.New(pool).WithTx(tx)
		control, err := qtx.LockWorkspaceClaimIntakeControlForMutation(
			ctx,
			util.MustParseUUID(pausing.workspaceID),
		)
		if err != nil {
			pauseAcquired <- err
			return
		}
		_, err = tx.Exec(ctx, `
			UPDATE workspace_claim_intake_control
			SET state = 'paused', generation = $2, updated_at = now()
			WHERE workspace_id = $1
		`, pausing.workspaceID, control.Generation+1)
		if err == nil {
			err = tx.Commit(ctx)
		}
		pauseAcquired <- err
	}()

	select {
	case err := <-pauseAcquired:
		t.Fatalf("pause acquired mutation lock before mixed claim committed: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(barrier.release)
	if err := <-claimDone; err != nil {
		t.Fatalf("mixed pre-fence claim: %v", err)
	}
	if err := <-pauseAcquired; err != nil {
		t.Fatalf("pause: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, status, priority, context, runtime_id)
		VALUES ($1, 'queued', 0, '{}'::jsonb, $2)
	`, active.agentID, active.runtimeID); err != nil {
		t.Fatalf("queue post-pause active task: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, status, priority, context, runtime_id)
		VALUES ($1, 'queued', 0, '{}'::jsonb, $2)
	`, pausing.agentID, pausing.runtimeID); err != nil {
		t.Fatalf("queue post-pause fenced task: %v", err)
	}

	result, err := svc.ClaimTasksForRuntimesAsConsumer(
		ctx,
		[]pgtype.UUID{
			util.MustParseUUID(active.runtimeID),
			util.MustParseUUID(pausing.runtimeID),
		},
		2,
		"consumer-mixed-after-pause",
	)
	if err != nil {
		t.Fatalf("mixed post-pause claim: %v", err)
	}
	if len(result.Tasks) != 1 || util.UUIDToString(result.Tasks[0].RuntimeID) != active.runtimeID {
		t.Fatalf("mixed post-pause tasks = %+v, want one active Workspace task", result.Tasks)
	}
	if len(result.PausedWorkspaces) != 1 ||
		util.UUIDToString(result.PausedWorkspaces[0].WorkspaceID) != pausing.workspaceID ||
		result.PausedWorkspaces[0].Generation != 5 {
		t.Fatalf("mixed post-pause fences = %+v", result.PausedWorkspaces)
	}
}

func TestClaimTaskForRuntime_UnreadableControlFailsClosed(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	f := newClaimIntakeFixture(t, ctx, pool, "unreadable-control")
	seedClaimIntakeControl(t, ctx, pool, f.workspaceID, "resumed", 0)

	svc := NewTaskService(
		db.New(pool),
		claimIntakeControlReadFailureStarter{pool: pool},
		nil,
		events.New(),
	)
	_, err := svc.ClaimTaskForRuntimeAsConsumer(
		ctx,
		util.MustParseUUID(f.runtimeID),
		"consumer-unreadable-control",
	)
	if !errors.Is(err, errClaimIntakeControlUnreadable) {
		t.Fatalf(
			"claim error = %v, want unreadable-control cause %v",
			err,
			errClaimIntakeControlUnreadable,
		)
	}
	assertTaskStatus(t, ctx, pool, f.taskID, "queued")
}

func TestClaimTaskForRuntime_ControlLockFailureDispatchesNothing(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	f := newClaimIntakeFixture(t, ctx, pool, "lock-failure")
	seedClaimIntakeControl(t, ctx, pool, f.workspaceID, "resumed", 0)

	mutationTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin mutation lock: %v", err)
	}
	defer mutationTx.Rollback(ctx)
	if _, err := db.New(pool).WithTx(mutationTx).
		LockWorkspaceClaimIntakeControlForMutation(
			ctx,
			util.MustParseUUID(f.workspaceID),
		); err != nil {
		t.Fatalf("hold mutation lock: %v", err)
	}

	claimCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	if _, err := svc.ClaimTaskForRuntimeAsConsumer(
		claimCtx,
		util.MustParseUUID(f.runtimeID),
		"consumer-lock-failure",
	); err == nil {
		t.Fatal("claim succeeded while authoritative control lock was unavailable")
	}
	assertTaskStatus(t, ctx, pool, f.taskID, "queued")
}

func TestClaimTaskForRuntime_PausedStatePersistsAcrossServiceRestart(t *testing.T) {
	ctx := context.Background()
	fixturePool := newTaskClaimRacePool(t)
	f := newClaimIntakeFixture(t, ctx, fixturePool, "restart-persistence")
	seedClaimIntakeControl(t, ctx, fixturePool, f.workspaceID, "paused", 8)

	// A separate pool and service instance simulate a restarted server process;
	// no in-memory state is shared with the fixture creator.
	restartedPool := newTaskClaimRacePool(t)
	restarted := NewTaskService(db.New(restartedPool), restartedPool, nil, events.New())
	result, err := restarted.ClaimTaskForRuntimeAsConsumer(
		ctx,
		util.MustParseUUID(f.runtimeID),
		"consumer-after-restart",
	)
	if err != nil {
		t.Fatalf("claim after restart: %v", err)
	}
	if result.Task != nil || !result.Paused || result.Generation != 8 {
		t.Fatalf("claim after restart = %+v, want persisted paused generation 8", result)
	}
	assertTaskStatus(t, ctx, fixturePool, f.taskID, "queued")
}

func TestClaimTaskForRuntime_PauseKeepsPreFenceOwnershipActive(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	f := newClaimIntakeFixture(t, ctx, pool, "pre-fence-active")
	seedClaimIntakeControl(t, ctx, pool, f.workspaceID, "resumed", 0)

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	claimed, err := svc.ClaimTaskForRuntimeAsConsumer(
		ctx,
		util.MustParseUUID(f.runtimeID),
		"consumer-pre-fence",
	)
	if err != nil || claimed.Task == nil {
		t.Fatalf("pre-fence claim = %+v, error %v", claimed, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE workspace_claim_intake_control
		SET state = 'paused', generation = 1, updated_at = now()
		WHERE workspace_id = $1
	`, f.workspaceID); err != nil {
		t.Fatalf("pause workspace: %v", err)
	}

	var status, consumerID string
	var generation int64
	if err := pool.QueryRow(ctx, `
		SELECT status, claim_consumer_id, claim_intake_generation
		FROM agent_task_queue
		WHERE id = $1
	`, f.taskID).Scan(&status, &consumerID, &generation); err != nil {
		t.Fatalf("load pre-fence ownership: %v", err)
	}
	if status != "dispatched" || consumerID != "consumer-pre-fence" || generation != 0 {
		t.Fatalf(
			"pre-fence ownership = status %q consumer %q generation %d",
			status,
			consumerID,
			generation,
		)
	}

	postFence, err := svc.ClaimTaskForRuntimeAsConsumer(
		ctx,
		util.MustParseUUID(f.runtimeID),
		"consumer-post-fence",
	)
	if err != nil {
		t.Fatalf("post-fence claim: %v", err)
	}
	if postFence.Task != nil || !postFence.Paused || postFence.Generation != 1 {
		t.Fatalf("post-fence claim = %+v, want paused generation 1", postFence)
	}
}

func TestClaimTaskForRuntime_DeferredBlockedThenResumeRestoresClaiming(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	f := newClaimIntakeFixture(t, ctx, pool, "deferred-resume")
	seedClaimIntakeControl(t, ctx, pool, f.workspaceID, "paused", 2)
	if _, err := pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'deferred', fire_at = now() - interval '1 minute'
		WHERE id = $1
	`, f.taskID); err != nil {
		t.Fatalf("defer task: %v", err)
	}

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	paused, err := svc.ClaimTaskForRuntimeAsConsumer(
		ctx,
		util.MustParseUUID(f.runtimeID),
		"consumer-while-paused",
	)
	if err != nil {
		t.Fatalf("paused deferred claim: %v", err)
	}
	if paused.Task != nil || !paused.Paused || paused.Generation != 2 {
		t.Fatalf("paused deferred claim = %+v", paused)
	}
	assertTaskStatus(t, ctx, pool, f.taskID, "deferred")

	var resumeActionID string
	if err := pool.QueryRow(ctx, `
		UPDATE workspace_claim_intake_control
		SET state = 'resumed',
		    generation = 3,
		    authoritative_action_id = gen_random_uuid(),
		    effective_at = now(),
		    updated_at = now()
		WHERE workspace_id = $1
		RETURNING authoritative_action_id::text
	`, f.workspaceID).Scan(&resumeActionID); err != nil {
		t.Fatalf("resume workspace: %v", err)
	}

	resumed, err := svc.ClaimTaskForRuntimeAsConsumer(
		ctx,
		util.MustParseUUID(f.runtimeID),
		"consumer-after-resume",
	)
	if err != nil {
		t.Fatalf("resumed claim: %v", err)
	}
	if resumed.Task == nil || util.UUIDToString(resumed.Task.ID) != f.taskID {
		t.Fatalf("resumed claim = %+v, want task %s", resumed, f.taskID)
	}

	var status, consumerID, actionID string
	var generation int64
	if err := pool.QueryRow(ctx, `
		SELECT
			status,
			claim_consumer_id,
			claim_intake_generation,
			claim_intake_action_id::text
		FROM agent_task_queue
		WHERE id = $1
	`, f.taskID).Scan(&status, &consumerID, &generation, &actionID); err != nil {
		t.Fatalf("load resumed ownership: %v", err)
	}
	if status != "dispatched" ||
		consumerID != "consumer-after-resume" ||
		generation != 3 ||
		actionID != resumeActionID {
		t.Fatalf(
			"resumed ownership = status %q consumer %q generation %d action %q",
			status,
			consumerID,
			generation,
			actionID,
		)
	}
}

func TestClaimTaskForRuntime_PausedWorkspaceDispatchesNothing(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	f := newClaimIntakeFixture(t, ctx, pool, "paused-singular")
	seedClaimIntakeControl(t, ctx, pool, f.workspaceID, "paused", 1)

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	result, err := svc.ClaimTaskForRuntime(ctx, util.MustParseUUID(f.runtimeID))
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if result.Task != nil {
		t.Fatalf("paused claim returned task %s", util.UUIDToString(result.Task.ID))
	}
	if !result.Paused || result.Generation != 1 {
		t.Fatalf("claim control = %+v, want paused generation 1", result)
	}
	assertTaskStatus(t, ctx, pool, f.taskID, "queued")
}

func TestClaimTaskForRuntimeAsConsumer_StampsFreshOwnership(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	f := newClaimIntakeFixture(t, ctx, pool, "consumer-fresh")
	seedClaimIntakeControl(t, ctx, pool, f.workspaceID, "resumed", 0)

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	result, err := svc.ClaimTaskForRuntimeAsConsumer(
		ctx,
		util.MustParseUUID(f.runtimeID),
		"consumer-a",
	)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if result.Task == nil || util.UUIDToString(result.Task.ID) != f.taskID {
		t.Fatalf("claimed task = %+v, want %s", result.Task, f.taskID)
	}

	var consumerID string
	var generation int64
	if err := pool.QueryRow(ctx, `
		SELECT claim_consumer_id, claim_intake_generation
		FROM agent_task_queue
		WHERE id = $1
	`, f.taskID).Scan(&consumerID, &generation); err != nil {
		t.Fatalf("load ownership stamps: %v", err)
	}
	if consumerID != "consumer-a" || generation != 0 {
		t.Fatalf(
			"ownership stamps = consumer %q generation %d, want consumer-a/0",
			consumerID,
			generation,
		)
	}
}

func TestClaimTaskForRuntimeAsConsumer_StaleReclaimReplacesConsumer(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	f := newClaimIntakeFixture(t, ctx, pool, "consumer-stale")
	seedClaimIntakeControl(t, ctx, pool, f.workspaceID, "resumed", 4)
	if _, err := pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'dispatched',
		    dispatched_at = now() - interval '1 hour',
		    prepare_lease_expires_at = NULL,
		    claim_intake_generation = 3,
		    claim_consumer_id = 'consumer-old'
		WHERE id = $1
	`, f.taskID); err != nil {
		t.Fatalf("make task stale: %v", err)
	}

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	result, err := svc.ClaimTaskForRuntimeAsConsumer(
		ctx,
		util.MustParseUUID(f.runtimeID),
		"consumer-new",
	)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if result.Task == nil || util.UUIDToString(result.Task.ID) != f.taskID {
		t.Fatalf("reclaimed task = %+v, want %s", result.Task, f.taskID)
	}

	var consumerID string
	var generation int64
	var refreshed bool
	if err := pool.QueryRow(ctx, `
		SELECT
			claim_consumer_id,
			claim_intake_generation,
			dispatched_at > now() - interval '15 seconds'
		FROM agent_task_queue
		WHERE id = $1
	`, f.taskID).Scan(&consumerID, &generation, &refreshed); err != nil {
		t.Fatalf("load reclaimed ownership stamps: %v", err)
	}
	if consumerID != "consumer-new" || generation != 4 || !refreshed {
		t.Fatalf(
			"reclaimed ownership = consumer %q generation %d refreshed %v, want consumer-new/4/true",
			consumerID,
			generation,
			refreshed,
		)
	}
}

func TestClaimTasksForRuntimes_MissingRuntimeDoesNotBlockValidRuntime(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	valid := newClaimIntakeFixture(t, ctx, pool, "runtime-disappears-valid")
	seedClaimIntakeControl(t, ctx, pool, valid.workspaceID, "resumed", 0)

	// The handler may authorize a runtime that is deleted before the service
	// transaction resolves the runtime set. At transaction time that is
	// indistinguishable from this well-formed but absent runtime id.
	missingRuntimeID := util.MustParseUUID("00000000-0000-4000-8000-000000000204")

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	result, err := svc.ClaimTasksForRuntimesAsConsumer(
		ctx,
		[]pgtype.UUID{
			util.MustParseUUID(valid.runtimeID),
			missingRuntimeID,
		},
		2,
		"consumer-after-runtime-delete",
	)
	if err != nil {
		t.Fatalf("batch claim with disappeared runtime: %v", err)
	}
	if len(result.Tasks) != 1 || util.UUIDToString(result.Tasks[0].ID) != valid.taskID {
		t.Fatalf("claimed tasks = %+v, want valid task %s", result.Tasks, valid.taskID)
	}
	assertTaskStatus(t, ctx, pool, valid.taskID, "dispatched")
}

func TestClaimTaskForRuntime_MissingControlFailsClosed(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	f := newClaimIntakeFixture(t, ctx, pool, "missing-control")
	if _, err := pool.Exec(ctx, `DELETE FROM workspace_claim_intake_control WHERE workspace_id = $1`, f.workspaceID); err != nil {
		t.Fatalf("delete authoritative control: %v", err)
	}

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	if _, err := svc.ClaimTaskForRuntime(ctx, util.MustParseUUID(f.runtimeID)); err == nil {
		t.Fatal("claim without authoritative control row succeeded; want fail-closed error")
	}
	assertTaskStatus(t, ctx, pool, f.taskID, "queued")
}

func TestClaimTaskForRuntime_PausedWorkspaceDoesNotReclaimStaleDispatch(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	f := newClaimIntakeFixture(t, ctx, pool, "paused-stale")
	seedClaimIntakeControl(t, ctx, pool, f.workspaceID, "paused", 2)
	if _, err := pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'dispatched', dispatched_at = now() - interval '1 hour', prepare_lease_expires_at = NULL
		WHERE id = $1
	`, f.taskID); err != nil {
		t.Fatalf("make task stale: %v", err)
	}

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	result, err := svc.ClaimTaskForRuntime(ctx, util.MustParseUUID(f.runtimeID))
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if result.Task != nil || !result.Paused {
		t.Fatalf("paused stale claim = %+v, want paused with no task", result)
	}
	var dispatchedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT dispatched_at FROM agent_task_queue WHERE id = $1`, f.taskID).Scan(&dispatchedAt); err != nil {
		t.Fatalf("load dispatched_at: %v", err)
	}
	if dispatchedAt.After(time.Now().Add(-30 * time.Minute)) {
		t.Fatalf("paused stale task was reclaimed at %s", dispatchedAt)
	}
}

func TestClaimTasksForRuntimes_MixedWorkspaceReturnsActiveAndPausedMetadata(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	active := newClaimIntakeFixture(t, ctx, pool, "mixed-active")
	paused := newClaimIntakeFixture(t, ctx, pool, "mixed-paused")
	seedClaimIntakeControl(t, ctx, pool, active.workspaceID, "resumed", 0)
	seedClaimIntakeControl(t, ctx, pool, paused.workspaceID, "paused", 3)

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	result, err := svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{
		util.MustParseUUID(active.runtimeID),
		util.MustParseUUID(paused.runtimeID),
	}, 5)
	if err != nil {
		t.Fatalf("batch claim: %v", err)
	}
	if len(result.Tasks) != 1 || util.UUIDToString(result.Tasks[0].ID) != active.taskID {
		t.Fatalf("claimed tasks = %+v, want only active workspace task %s", result.Tasks, active.taskID)
	}
	if len(result.PausedWorkspaces) != 1 || util.UUIDToString(result.PausedWorkspaces[0].WorkspaceID) != paused.workspaceID || result.PausedWorkspaces[0].Generation != 3 {
		t.Fatalf("paused workspaces = %+v, want workspace %s generation 3", result.PausedWorkspaces, paused.workspaceID)
	}
	assertTaskStatus(t, ctx, pool, active.taskID, "dispatched")
	assertTaskStatus(t, ctx, pool, paused.taskID, "queued")
}

func TestClaimTasksForRuntimes_MissingControlRollsBackEveryWorkspace(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	active := newClaimIntakeFixture(t, ctx, pool, "rollback-active")
	missing := newClaimIntakeFixture(t, ctx, pool, "rollback-missing")
	seedClaimIntakeControl(t, ctx, pool, active.workspaceID, "resumed", 0)
	if _, err := pool.Exec(ctx, `DELETE FROM workspace_claim_intake_control WHERE workspace_id = $1`, missing.workspaceID); err != nil {
		t.Fatalf("delete authoritative control: %v", err)
	}

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	if _, err := svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{
		util.MustParseUUID(active.runtimeID),
		util.MustParseUUID(missing.runtimeID),
	}, 5); err == nil {
		t.Fatal("mixed batch with missing control row succeeded; want fail-closed error")
	}
	assertTaskStatus(t, ctx, pool, active.taskID, "queued")
	assertTaskStatus(t, ctx, pool, missing.taskID, "queued")
}
