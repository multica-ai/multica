package runtimepool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/pooltestdb"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestSchedulerCandidateOrderPrecedesLimit(t *testing.T) {
	raw, err := os.ReadFile("../../pkg/db/queries/runtime.sql")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(raw), "-- name: ListPoolRuntimeCandidates")
	if start < 0 {
		t.Fatal("ListPoolRuntimeCandidates query missing")
	}
	query := string(raw[start:])
	order := strings.Index(query, "ORDER BY")
	limit := strings.Index(query, "LIMIT sqlc.arg(runtime_limit)")
	if order < 0 || limit <= order {
		t.Fatalf("ORDER/LIMIT positions %d/%d", order, limit)
	}
	for _, fragment := range []string{
		"ar.runtime_mode = 'local'",
		"ar.last_seen_at DESC NULLS LAST",
		"fixed_binding_count ASC",
		"ar.created_at ASC",
		"ar.id ASC",
	} {
		if !strings.Contains(query[order:limit], fragment) {
			t.Fatalf("candidate order missing %q", fragment)
		}
	}
	if !strings.Contains(query[:limit], "NOT EXISTS") {
		t.Fatal("candidate strict-idle predicate must precede LIMIT")
	}
	if strings.Contains(query[:limit], "platform-agent-cli") {
		t.Fatal("generic Runtime Pool query contains a Provider predicate")
	}
	for _, fragment := range []string{
		"ar.runtime_mode = 'cloud'",
		"ar.runtime_mode = 'local'",
		"ar.owner_id = sqlc.narg(trigger_user_id)::uuid",
	} {
		if !strings.Contains(query[:limit], fragment) {
			t.Fatalf("candidate trigger policy missing %q", fragment)
		}
	}
}

func TestSchedulerLivenessRedisAndFallback(t *testing.T) {
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	stale := schedulerRuntimeCandidate(1, now.Add(-151*time.Second))
	fresh := schedulerRuntimeCandidate(2, now.Add(-149*time.Second))
	candidates := []db.ListPoolRuntimeCandidatesRow{stale, fresh}

	redisAlive := filterAliveInOrder(candidates, map[string]bool{
		"00000000-0000-0000-0000-000000000001": true,
	}, true, now)
	if len(redisAlive) != 1 || redisAlive[0].AgentRuntime.ID != stale.AgentRuntime.ID {
		t.Fatalf("authoritative Redis result = %+v, want stale-DB Runtime selected by Redis", redisAlive)
	}

	redisEmpty := filterAliveInOrder(candidates, map[string]bool{}, true, now)
	if len(redisEmpty) != 0 {
		t.Fatalf("authoritative empty Redis result = %+v, want none", redisEmpty)
	}

	fallback := filterAliveInOrder(candidates, nil, false, now)
	if len(fallback) != 1 || fallback[0].AgentRuntime.ID != fresh.AgentRuntime.ID {
		t.Fatalf("DB fallback result = %+v, want only fresh Runtime", fallback)
	}
}

func TestSchedulerPinnedReasonCASCoversRoutingSnapshot(t *testing.T) {
	raw, err := os.ReadFile("../../pkg/db/queries/runtime.sql")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(raw), "-- name: UpdatePinnedPoolTaskWaitReasonCAS")
	if start < 0 {
		t.Fatal("UpdatePinnedPoolTaskWaitReasonCAS query missing")
	}
	query := string(raw[start:])
	for _, fragment := range []string{
		"status = sqlc.arg(expected_status)",
		"runtime_id IS NOT DISTINCT FROM sqlc.narg('expected_runtime_id')::uuid",
		"agent_id = sqlc.arg(expected_agent_id)::uuid",
		"chat_session_id IS NOT DISTINCT FROM sqlc.narg('expected_chat_session_id')::uuid",
		"runtime_binding_mode = sqlc.arg(expected_runtime_binding_mode)",
		"placement_workspace_id = sqlc.arg(expected_placement_workspace_id)::uuid",
		"runtime_requester_user_id = sqlc.arg(expected_runtime_requester_user_id)::uuid",
		"runtime_trigger_user_id IS NOT DISTINCT FROM sqlc.narg('expected_runtime_trigger_user_id')::uuid",
		"runtime_requirements = sqlc.arg(expected_runtime_requirements)::jsonb",
		"session_affinity_state = sqlc.arg(expected_session_affinity_state)",
		"session_affinity_runtime_id IS NOT DISTINCT FROM sqlc.narg('expected_session_affinity_runtime_id')::uuid",
		"explicit_fresh_session = sqlc.arg(expected_explicit_fresh_session)",
		"wait_reason IS NOT DISTINCT FROM sqlc.narg('expected_wait_reason')::text",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("pinned reason CAS missing routing predicate %q", fragment)
		}
	}
}

func schedulerRuntimeCandidate(last byte, seen time.Time) db.ListPoolRuntimeCandidatesRow {
	var id [16]byte
	id[15] = last
	return db.ListPoolRuntimeCandidatesRow{AgentRuntime: db.AgentRuntime{
		ID:         pgtype.UUID{Bytes: id, Valid: true},
		LastSeenAt: pgtype.Timestamptz{Time: seen, Valid: true},
	}}
}

type schedulerTestLiveness struct {
	alive map[string]bool
	ok    bool
	hook  func()
	calls atomic.Int32
}

func (l *schedulerTestLiveness) Available() bool { return l.ok }

func (l *schedulerTestLiveness) IsAliveBatch(_ context.Context, ids []string) (map[string]bool, bool) {
	l.calls.Add(1)
	if l.hook != nil {
		l.hook()
	}
	if l.alive != nil {
		return l.alive, l.ok
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, l.ok
}

type schedulerTrackingTxStarter struct {
	pool   *pgxpool.Pool
	begins atomic.Int32
}

func (s *schedulerTrackingTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	s.begins.Add(1)
	return s.pool.Begin(ctx)
}

type schedulerPIDTxStarter struct {
	pool *pgxpool.Pool
	pids chan uint32
}

type schedulerFailingTxStarter struct {
	err error
}

func (s schedulerFailingTxStarter) Begin(context.Context) (pgx.Tx, error) {
	return nil, s.err
}

func (s *schedulerPIDTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err == nil {
		s.pids <- tx.Conn().PgConn().PID()
	}
	return tx, err
}

func TestSchedulerLivenessModesCompleteBeforeBegin(t *testing.T) {
	tests := []struct {
		name          string
		seenAgo       time.Duration
		livenessKind  string
		wantAssigned  int
		wantLiveCalls int32
	}{
		{name: "Redis authoritative empty", seenAgo: 10 * time.Second, livenessKind: "redis-empty", wantAssigned: 0, wantLiveCalls: 1},
		{name: "Redis error uses fresh DB heartbeat", seenAgo: 149 * time.Second, livenessKind: "redis-error", wantAssigned: 1, wantLiveCalls: 1},
		{name: "Noop uses fresh DB heartbeat", seenAgo: 149 * time.Second, livenessKind: "noop", wantAssigned: 1, wantLiveCalls: 0},
		{name: "Noop rejects stale DB heartbeat", seenAgo: 151 * time.Second, livenessKind: "noop", wantAssigned: 0, wantLiveCalls: 0},
		{name: "Redis alive is sampled before Begin", seenAgo: 10 * time.Minute, livenessKind: "redis-alive", wantAssigned: 1, wantLiveCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newSchedulerDBFixture(t)
			fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now().Add(-tt.seenAgo))
			fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})
			txStarter := &schedulerTrackingTxStarter{pool: fixture.pool}
			var liveness LivenessReader
			var livenessProbe *schedulerTestLiveness
			switch tt.livenessKind {
			case "redis-empty":
				livenessProbe = &schedulerTestLiveness{alive: map[string]bool{}, ok: true}
			case "redis-error":
				livenessProbe = &schedulerTestLiveness{alive: map[string]bool{}, ok: false}
			case "redis-alive":
				livenessProbe = &schedulerTestLiveness{ok: true}
			}
			if livenessProbe != nil {
				livenessProbe.hook = func() {
					if txStarter.begins.Load() != 0 {
						t.Errorf("IsAliveBatch ran after Begin")
					}
				}
				liveness = livenessProbe
			}
			scheduler := NewScheduler(fixture.q, txStarter, liveness)
			result, err := scheduler.AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Assigned) != tt.wantAssigned {
				t.Fatalf("assigned = %d, want %d", len(result.Assigned), tt.wantAssigned)
			}
			if livenessProbe != nil && livenessProbe.calls.Load() != tt.wantLiveCalls {
				t.Fatalf("IsAliveBatch calls = %d, want %d", livenessProbe.calls.Load(), tt.wantLiveCalls)
			}
			if tt.wantAssigned == 0 && txStarter.begins.Load() != 0 {
				t.Fatalf("Begin calls = %d with no live candidate, want 0", txStarter.begins.Load())
			}
		})
	}
}

func TestSchedulerStrictIdleCapacityStatesAndFixedBindingControl(t *testing.T) {
	for _, status := range []string{"queued", "deferred", "dispatched", "running", "waiting_local_directory"} {
		t.Run(status, func(t *testing.T) {
			fixture := newSchedulerDBFixture(t)
			busyRuntimeID := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
			fallbackRuntimeID := fixture.addRuntime(t, fixture.userID, "cloud", "public", []string{CapabilityExtensionExecuteV1}, "online", time.Now().Add(-time.Minute))
			fixture.addOccupancyStatus(t, busyRuntimeID, status)
			fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})

			result, err := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true}).AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Assigned) != 1 || result.Assigned[0].RuntimeID != fallbackRuntimeID {
				t.Fatalf("capacity state %s assigned %+v, want fallback Runtime %v", status, result.Assigned, fallbackRuntimeID)
			}
		})
	}

	t.Run("fixed Agent binding is ranking metadata, not occupancy", func(t *testing.T) {
		fixture := newSchedulerDBFixture(t)
		runtimeID := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
		fixture.addFixedAgentBinding(t, runtimeID)
		fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})

		result, err := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true}).AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Assigned) != 1 || result.Assigned[0].RuntimeID != runtimeID {
			t.Fatalf("fixed binding control assigned %+v, want Runtime %v", result.Assigned, runtimeID)
		}
	})
}

func TestSchedulerRuntimeLockCompetition(t *testing.T) {
	t.Run("fresh tries the next ranked Runtime", func(t *testing.T) {
		fixture := newSchedulerDBFixture(t)
		lockedRuntimeID := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
		nextRuntimeID := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now().Add(-time.Minute))
		fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})
		lockTx := fixture.lockRuntime(t, lockedRuntimeID)
		defer lockTx.Rollback(context.Background())

		result, err := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true}).AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Assigned) != 1 || result.Assigned[0].RuntimeID != nextRuntimeID {
			t.Fatalf("fresh lock race assigned %+v, want next Runtime %v", result.Assigned, nextRuntimeID)
		}
	})

	t.Run("pinned lock conflict preserves reason", func(t *testing.T) {
		fixture := newSchedulerDBFixture(t)
		runtimeID := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
		taskID := fixture.addWaiting(t, SessionAffinityPinned, runtimeID, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})
		if _, err := fixture.pool.Exec(context.Background(), `UPDATE agent_task_queue SET wait_reason='session_runtime_capability_mismatch' WHERE id=$1`, taskID); err != nil {
			t.Fatal(err)
		}
		lockTx := fixture.lockRuntime(t, runtimeID)
		defer lockTx.Rollback(context.Background())

		result, err := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true}).AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Assigned) != 0 {
			t.Fatalf("pinned lock conflict assigned %+v", result.Assigned)
		}
		var status, reason string
		if err := fixture.pool.QueryRow(context.Background(), `SELECT status,wait_reason FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status, &reason); err != nil {
			t.Fatal(err)
		}
		if status != StatusWaitingRuntime || reason != "session_runtime_capability_mismatch" {
			t.Fatalf("pinned Task after lock conflict = %s/%s", status, reason)
		}
	})
}

func TestSchedulerResolvedChatHeadBlocksTail(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	headRuntimeID := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	fixture.addRuntime(t, fixture.userID, "cloud", "public", []string{CapabilityExtensionExecuteV1}, "online", time.Now().Add(-time.Minute))
	chatID := fixture.addChat(t)
	headTaskID := fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, chatID, "waiting_runtime", 3, time.Time{})
	if _, err := fixture.pool.Exec(context.Background(), `
		UPDATE agent_task_queue SET status='queued',runtime_id=$1,wait_reason=NULL WHERE id=$2
	`, headRuntimeID, headTaskID); err != nil {
		t.Fatal(err)
	}
	tailTaskID := fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, chatID, "waiting_runtime", 2, time.Time{})

	result, err := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true}).AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assigned) != 0 {
		t.Fatalf("resolved Chat tail assigned behind active head: %+v", result.Assigned)
	}
	var status string
	var runtimeID pgtype.UUID
	if err := fixture.pool.QueryRow(context.Background(), `SELECT status,runtime_id FROM agent_task_queue WHERE id=$1`, tailTaskID).Scan(&status, &runtimeID); err != nil {
		t.Fatal(err)
	}
	if status != StatusWaitingRuntime || runtimeID.Valid {
		t.Fatalf("Chat tail = %s/%v, want waiting_runtime/unassigned", status, runtimeID)
	}
}

func TestSchedulerMemberRevokeSerializesBeforeRuntimeLock(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	taskID := fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})

	revokeTx, err := fixture.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer revokeTx.Rollback(context.Background())
	var memberID pgtype.UUID
	if err := revokeTx.QueryRow(context.Background(), `
		SELECT id FROM member WHERE workspace_id=$1 AND user_id=$2 FOR UPDATE
	`, fixture.workspaceID, fixture.userID).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	starter := &schedulerPIDTxStarter{pool: fixture.pool, pids: make(chan uint32, 1)}
	type assignmentOutcome struct {
		result AssignResult
		err    error
	}
	done := make(chan assignmentOutcome, 1)
	go func() {
		result, err := NewScheduler(fixture.q, starter, &schedulerTestLiveness{ok: true}).AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
		done <- assignmentOutcome{result: result, err: err}
	}()
	pid := <-starter.pids
	waitForBlockedPID(t, fixture.pool, pid)
	if _, err := revokeTx.Exec(context.Background(), `DELETE FROM member WHERE id=$1`, memberID); err != nil {
		t.Fatal(err)
	}
	if err := revokeTx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	outcome := <-done
	if outcome.err != nil {
		t.Fatal(outcome.err)
	}
	if len(outcome.result.Assigned) != 0 {
		t.Fatalf("assignment crossed committed Member revoke: %+v", outcome.result.Assigned)
	}
	var status string
	if err := fixture.pool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != StatusWaitingRuntime {
		t.Fatalf("Task after Member revoke = %s, want waiting_runtime", status)
	}
}

func TestSchedulerMemberRoleDowngradeFailsClosed(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	fixture.addRuntime(t, pgtype.UUID{}, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	if _, err := fixture.pool.Exec(context.Background(), `
		UPDATE member SET role='admin' WHERE workspace_id=$1 AND user_id=$2
	`, fixture.workspaceID, fixture.userID); err != nil {
		t.Fatal(err)
	}
	taskID := fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})

	downgradeTx, err := fixture.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer downgradeTx.Rollback(context.Background())
	var memberID pgtype.UUID
	if err := downgradeTx.QueryRow(context.Background(), `
		SELECT id FROM member WHERE workspace_id=$1 AND user_id=$2 FOR UPDATE
	`, fixture.workspaceID, fixture.userID).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	starter := &schedulerPIDTxStarter{pool: fixture.pool, pids: make(chan uint32, 1)}
	type assignmentOutcome struct {
		result AssignResult
		err    error
	}
	done := make(chan assignmentOutcome, 1)
	go func() {
		result, err := NewScheduler(fixture.q, starter, &schedulerTestLiveness{ok: true}).AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
		done <- assignmentOutcome{result: result, err: err}
	}()
	pid := <-starter.pids
	waitForBlockedPID(t, fixture.pool, pid)
	if _, err := downgradeTx.Exec(context.Background(), `UPDATE member SET role='member' WHERE id=$1`, memberID); err != nil {
		t.Fatal(err)
	}
	if err := downgradeTx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	outcome := <-done
	if outcome.err != nil {
		t.Fatal(outcome.err)
	}
	if len(outcome.result.Assigned) != 0 {
		t.Fatalf("assignment crossed committed Member role downgrade: %+v", outcome.result.Assigned)
	}
	var status string
	if err := fixture.pool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != StatusWaitingRuntime {
		t.Fatalf("Task after Member role downgrade = %s, want waiting_runtime", status)
	}
}

func TestSchedulerFixedEnqueueFollowsCommittedPoolAssignment(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	runtimeID := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	fixedAgentID := fixture.addFixedAgentBinding(t, runtimeID)
	chatID := fixture.addChat(t)
	fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, chatID, "waiting_runtime", 2, time.Time{})

	chatBlocker, err := fixture.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer chatBlocker.Rollback(context.Background())
	var lockedChatID pgtype.UUID
	if err := chatBlocker.QueryRow(context.Background(), `SELECT id FROM chat_session WHERE id=$1 FOR UPDATE`, chatID).Scan(&lockedChatID); err != nil {
		t.Fatal(err)
	}
	starter := &schedulerPIDTxStarter{pool: fixture.pool, pids: make(chan uint32, 1)}
	type assignmentOutcome struct {
		result AssignResult
		err    error
	}
	assignmentDone := make(chan assignmentOutcome, 1)
	go func() {
		result, err := NewScheduler(fixture.q, starter, &schedulerTestLiveness{ok: true}).AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
		assignmentDone <- assignmentOutcome{result: result, err: err}
	}()
	schedulerPID := <-starter.pids
	waitForBlockedPID(t, fixture.pool, schedulerPID)

	type fixedOutcome struct {
		id  pgtype.UUID
		err error
	}
	fixedPID := make(chan uint32, 1)
	fixedDone := make(chan fixedOutcome, 1)
	go func() {
		tx, err := fixture.pool.Begin(context.Background())
		if err != nil {
			fixedDone <- fixedOutcome{err: err}
			return
		}
		defer tx.Rollback(context.Background())
		fixedPID <- tx.Conn().PgConn().PID()
		var taskID pgtype.UUID
		if err := tx.QueryRow(context.Background(), `
			INSERT INTO agent_task_queue (agent_id,runtime_id,status,runtime_binding_mode)
			VALUES ($1,$2,'queued','fixed') RETURNING id
		`, fixedAgentID, runtimeID).Scan(&taskID); err != nil {
			fixedDone <- fixedOutcome{err: err}
			return
		}
		if err := tx.Commit(context.Background()); err != nil {
			fixedDone <- fixedOutcome{err: err}
			return
		}
		fixedDone <- fixedOutcome{id: taskID}
	}()
	waitForBlockedPID(t, fixture.pool, <-fixedPID)
	if err := chatBlocker.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}

	assignment := <-assignmentDone
	if assignment.err != nil {
		t.Fatal(assignment.err)
	}
	if len(assignment.result.Assigned) != 1 || assignment.result.Assigned[0].RuntimeID != runtimeID {
		t.Fatalf("Pool assignment = %+v, want Runtime %v", assignment.result.Assigned, runtimeID)
	}
	fixed := <-fixedDone
	if fixed.err != nil {
		t.Fatal(fixed.err)
	}
	if !fixed.id.Valid {
		t.Fatal("fixed enqueue did not commit after Pool assignment")
	}
	var queued int
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue WHERE runtime_id=$1 AND status='queued'
	`, runtimeID).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 2 {
		t.Fatalf("queued Tasks on Runtime = %d, want committed Pool + fixed", queued)
	}
}

func TestSchedulerProviderIsCapabilityDriven(t *testing.T) {
	t.Run("capable non-Platform Runtime is selectable", func(t *testing.T) {
		fixture := newSchedulerDBFixture(t)
		runtimeID := fixture.addRuntimeWithProvider(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now(), "custom-runtime")
		fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})
		result, err := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true}).AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Assigned) != 1 || result.Assigned[0].RuntimeID != runtimeID {
			t.Fatalf("non-Platform assignment = %+v, want Runtime %v", result.Assigned, runtimeID)
		}
	})

	t.Run("incapable Platform Runtime is rejected", func(t *testing.T) {
		fixture := newSchedulerDBFixture(t)
		fixture.addRuntimeWithProvider(t, fixture.userID, "local", "private", []string{}, "online", time.Now(), "platform-agent-cli")
		taskID := fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})
		result, err := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true}).AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Assigned) != 0 {
			t.Fatalf("incapable Platform Runtime assigned %+v", result.Assigned)
		}
		var status, reason string
		if err := fixture.pool.QueryRow(context.Background(), `SELECT status,wait_reason FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status, &reason); err != nil {
			t.Fatal(err)
		}
		if status != StatusWaitingRuntime || reason != "no_eligible_runtime" {
			t.Fatalf("Task with incapable Platform Runtime = %s/%s", status, reason)
		}
	})
}

func TestSchedulerFreshCandidateExclusionMatrix(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*testing.T, *schedulerDBFixture)
		liveness func() LivenessReader
	}{
		{
			name: "private non-owner",
			setup: func(t *testing.T, fixture *schedulerDBFixture) {
				fixture.addRuntime(t, pgtype.UUID{}, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
			},
			liveness: func() LivenessReader { return &schedulerTestLiveness{ok: true} },
		},
		{
			name: "cross Workspace",
			setup: func(t *testing.T, fixture *schedulerDBFixture) {
				fixture.addCrossWorkspaceRuntime(t, []string{CapabilityExtensionExecuteV1}, "online", time.Now())
			},
			liveness: func() LivenessReader { return &schedulerTestLiveness{ok: true} },
		},
		{
			name: "capability mismatch",
			setup: func(t *testing.T, fixture *schedulerDBFixture) {
				fixture.addRuntime(t, fixture.userID, "local", "private", []string{}, "online", time.Now())
			},
			liveness: func() LivenessReader { return &schedulerTestLiveness{ok: true} },
		},
		{
			name: "offline",
			setup: func(t *testing.T, fixture *schedulerDBFixture) {
				fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "offline", time.Now())
			},
			liveness: func() LivenessReader { return &schedulerTestLiveness{ok: true} },
		},
		{
			name: "stale DB fallback",
			setup: func(t *testing.T, fixture *schedulerDBFixture) {
				fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now().Add(-151*time.Second))
			},
			liveness: func() LivenessReader { return nil },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newSchedulerDBFixture(t)
			tt.setup(t, fixture)
			taskID := fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})
			result, err := NewScheduler(fixture.q, fixture.pool, tt.liveness()).AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Assigned) != 0 {
				t.Fatalf("excluded Runtime assigned %+v", result.Assigned)
			}
			var status string
			var runtimeID pgtype.UUID
			if err := fixture.pool.QueryRow(context.Background(), `SELECT status,runtime_id FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status, &runtimeID); err != nil {
				t.Fatal(err)
			}
			if status != StatusWaitingRuntime || runtimeID.Valid {
				t.Fatalf("Task after excluded Runtime = %s/%v", status, runtimeID)
			}
		})
	}
}

func TestSchedulerExcludesUnresolvedWaitingTask(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	chatID := fixture.addChat(t)
	taskID := fixture.addWaiting(t, SessionAffinityUnresolved, pgtype.UUID{}, chatID, "waiting_runtime", 2, time.Time{})
	result, err := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true}).AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assigned) != 0 {
		t.Fatalf("unresolved waiting Task assigned %+v", result.Assigned)
	}
	var status, reason string
	if err := fixture.pool.QueryRow(context.Background(), `SELECT status,wait_reason FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != StatusWaitingRuntime || reason != "chat_predecessor_pending" {
		t.Fatalf("unresolved waiting Task = %s/%s", status, reason)
	}
}

func TestSchedulerTwoTasksRaceForOneStrictIdleRuntime(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	firstTaskID := fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})
	secondTaskID := fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 1, time.Time{})

	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	results := make(chan AssignResult, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			liveness := &schedulerTestLiveness{ok: true, hook: func() {
				arrived <- struct{}{}
				<-release
			}}
			result, err := NewScheduler(fixture.q, fixture.pool, liveness).AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
			results <- result
			errs <- err
		}()
	}
	<-arrived
	<-arrived
	close(release)
	assigned := 0
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		assigned += len((<-results).Assigned)
	}
	if assigned != 1 {
		t.Fatalf("strict-idle concurrent assignments = %d, want 1", assigned)
	}
	var queued int
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue WHERE id IN ($1,$2) AND status='queued'
	`, firstTaskID, secondTaskID).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("queued competing Tasks = %d, want 1", queued)
	}
}

func TestSchedulerHardBounds(t *testing.T) {
	t.Run("assignment commits at most eight", func(t *testing.T) {
		fixture := newSchedulerDBFixture(t)
		for i := range 9 {
			fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now().Add(-time.Duration(i)*time.Second))
			fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", int32(20-i), time.Time{})
		}
		result, err := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true}).AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: 99})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Assigned) != AssignmentBatchLimit {
			t.Fatalf("assignments = %d, want hard limit %d", len(result.Assigned), AssignmentBatchLimit)
		}
		var queued, waiting int
		if err := fixture.pool.QueryRow(context.Background(), `
			SELECT count(*) FILTER (WHERE status='queued'),
			       count(*) FILTER (WHERE status='waiting_runtime')
			FROM agent_task_queue
			WHERE agent_id=$1 AND runtime_binding_mode='pool'
		`, fixture.agentID).Scan(&queued, &waiting); err != nil {
			t.Fatal(err)
		}
		if queued != AssignmentBatchLimit || waiting != 1 {
			t.Fatalf("persisted bound = queued %d/waiting %d, want 8/1", queued, waiting)
		}
	})

	t.Run("waiting scan is one Workspace and at most sixty-four", func(t *testing.T) {
		fixture := newSchedulerDBFixture(t)
		other := newSchedulerDBFixture(t)
		for i := range WaitingTaskScanLimit + 1 {
			fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", int32(i), time.Time{})
		}
		other.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 1_000, time.Time{})
		tasks, err := fixture.q.ListWaitingPoolTasks(context.Background(), db.ListWaitingPoolTasksParams{
			PlacementWorkspaceID: fixture.workspaceID,
			ScanLimit:            WaitingTaskScanLimit,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != WaitingTaskScanLimit {
			t.Fatalf("waiting scan = %d, want %d", len(tasks), WaitingTaskScanLimit)
		}
		for _, task := range tasks {
			if task.PlacementWorkspaceID != fixture.workspaceID {
				t.Fatalf("cross-Workspace Task entered scan: %+v", task)
			}
		}
	})

	t.Run("Runtime candidate page is at most one hundred twenty-eight", func(t *testing.T) {
		fixture := newSchedulerDBFixture(t)
		for i := range RuntimeScanLimit + 1 {
			fixture.addRuntime(t, fixture.userID, "cloud", "public", []string{CapabilityExtensionExecuteV1}, "online", time.Now().Add(-time.Duration(i)*time.Millisecond))
		}
		candidates, err := fixture.q.ListPoolRuntimeCandidates(context.Background(), db.ListPoolRuntimeCandidatesParams{
			RequesterUserID: fixture.userID,
			WorkspaceID:     fixture.workspaceID,
			RequirementsAll: []string{CapabilityExtensionExecuteV1},
			RuntimeLimit:    RuntimeScanLimit,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(candidates) != RuntimeScanLimit {
			t.Fatalf("Runtime candidate page = %d, want %d", len(candidates), RuntimeScanLimit)
		}
	})

	t.Run("deferred promotion updates at most sixty-four", func(t *testing.T) {
		fixture := newSchedulerDBFixture(t)
		for i := range DeferredPromotionLimit + 1 {
			fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "deferred", int32(i), time.Now().Add(-time.Minute))
		}
		promoted, err := fixture.q.PromoteDuePoolDeferredTasksForWorkspace(context.Background(), db.PromoteDuePoolDeferredTasksForWorkspaceParams{
			PlacementWorkspaceID: fixture.workspaceID,
			Now:                  pgtype.Timestamptz{Time: time.Now(), Valid: true},
			PromoteLimit:         DeferredPromotionLimit,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(promoted) != DeferredPromotionLimit {
			t.Fatalf("promoted = %d, want %d", len(promoted), DeferredPromotionLimit)
		}
		var waiting, deferred int
		if err := fixture.pool.QueryRow(context.Background(), `
			SELECT count(*) FILTER (WHERE status='waiting_runtime'),
			       count(*) FILTER (WHERE status='deferred')
			FROM agent_task_queue WHERE agent_id=$1 AND runtime_binding_mode='pool'
		`, fixture.agentID).Scan(&waiting, &deferred); err != nil {
			t.Fatal(err)
		}
		if waiting != DeferredPromotionLimit || deferred != 1 {
			t.Fatalf("promotion bound = waiting %d/deferred %d, want 64/1", waiting, deferred)
		}
	})

	t.Run("Workspace page is at most thirty-two", func(t *testing.T) {
		pool := pooltestdb.Open(t)
		tx, err := pool.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		for i := range WorkspaceSweepLimit + 1 {
			suffix := fmt.Sprintf("%d-%d-%d", time.Now().UnixNano(), schedulerFixtureSequence.Add(1), i)
			if _, err := tx.Exec(context.Background(), `INSERT INTO workspace (name,slug) VALUES ('Pool Sweep Bound',$1)`, "pool-sweep-bound-"+suffix); err != nil {
				t.Fatal(err)
			}
		}
		workspaces, err := db.New(pool).WithTx(tx).ListRuntimePoolSweepWorkspaces(context.Background(), db.ListRuntimePoolSweepWorkspacesParams{
			AfterWorkspaceID: pgtype.UUID{},
			WorkspaceLimit:   WorkspaceSweepLimit,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(workspaces) != WorkspaceSweepLimit {
			t.Fatalf("Workspace page = %d, want %d", len(workspaces), WorkspaceSweepLimit)
		}
	})
}

func TestSchedulerOwnerLocalPrecedesShared(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	local := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now().Add(-30*time.Second))
	fixture.addRuntime(t, fixture.userID, "cloud", "public", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})

	scheduler := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true})
	result, err := scheduler.AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assigned) != 1 || result.Assigned[0].RuntimeID != local {
		t.Fatalf("assigned = %+v, want owner-local Runtime %v", result.Assigned, local)
	}
}

func TestSchedulerBusyLocalSelectsShared(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	local := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	shared := fixture.addRuntime(t, fixture.userID, "cloud", "public", []string{CapabilityExtensionExecuteV1}, "online", time.Now().Add(-time.Minute))
	fixture.addOccupancy(t, local)
	fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})

	scheduler := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true})
	result, err := scheduler.AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assigned) != 1 || result.Assigned[0].RuntimeID != shared {
		t.Fatalf("assigned = %+v, want shared Runtime %v", result.Assigned, shared)
	}
}

func TestSchedulerAnonymousUsesCloudOnly(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	cloud := fixture.addRuntime(t, pgtype.UUID{}, "cloud", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now().Add(-time.Minute))
	taskID := fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})
	if _, err := fixture.pool.Exec(context.Background(), `UPDATE agent_task_queue SET runtime_trigger_user_id=NULL WHERE id=$1`, taskID); err != nil {
		t.Fatal(err)
	}

	result, err := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true}).AssignWaiting(
		context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assigned) != 1 || result.Assigned[0].RuntimeID != cloud {
		t.Fatalf("assigned = %+v, want cloud Runtime %v", result.Assigned, cloud)
	}
}

func TestSchedulerPinnedBusyRuntime(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	runtimeID := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	fixture.addOccupancy(t, runtimeID)
	fixture.addWaiting(t, SessionAffinityPinned, runtimeID, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})

	scheduler := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true})
	result, err := scheduler.AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assigned) != 1 || result.Assigned[0].RuntimeID != runtimeID {
		t.Fatalf("assigned = %+v, want pinned busy Runtime %v", result.Assigned, runtimeID)
	}
}

func TestSchedulerPinnedUnavailableReasons(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	offlineRuntime := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "offline", time.Now())
	unauthorizedRuntime := fixture.addRuntime(t, pgtype.UUID{}, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	capabilityRuntime := fixture.addRuntime(t, fixture.userID, "local", "public", []string{}, "online", time.Now())
	offlineTask := fixture.addWaiting(t, SessionAffinityPinned, offlineRuntime, pgtype.UUID{}, "waiting_runtime", 3, time.Time{})
	unauthorizedTask := fixture.addWaiting(t, SessionAffinityPinned, unauthorizedRuntime, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})
	capabilityTask := fixture.addWaiting(t, SessionAffinityPinned, capabilityRuntime, pgtype.UUID{}, "waiting_runtime", 1, time.Time{})

	scheduler := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true})
	if _, err := scheduler.AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit}); err != nil {
		t.Fatal(err)
	}
	wants := map[pgtype.UUID]string{
		offlineTask:      "session_runtime_offline",
		unauthorizedTask: "session_runtime_unauthorized",
		capabilityTask:   "session_runtime_capability_mismatch",
	}
	for taskID, want := range wants {
		var status, reason string
		var runtimeID pgtype.UUID
		if err := fixture.pool.QueryRow(context.Background(), `SELECT status,wait_reason,runtime_id FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status, &reason, &runtimeID); err != nil {
			t.Fatal(err)
		}
		if status != StatusWaitingRuntime || reason != want || runtimeID.Valid {
			t.Fatalf("Task %v = %s/%s/%+v, want waiting/%s/unassigned", taskID, status, reason, runtimeID, want)
		}
	}
}

func TestSchedulerPinnedReasonCASRejectsAffinityABA(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	offlineRuntimeID := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "offline", time.Now())
	newRuntimeID := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	pinnedTaskID := fixture.addWaiting(t, SessionAffinityPinned, offlineRuntimeID, pgtype.UUID{}, "waiting_runtime", 3, time.Time{})
	fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 1, time.Time{})

	var once sync.Once
	liveness := &schedulerTestLiveness{ok: true, hook: func() {
		once.Do(func() {
			if _, err := fixture.pool.Exec(context.Background(), `
				UPDATE agent_task_queue
				SET session_affinity_state='none', session_affinity_runtime_id=NULL
				WHERE id=$1
			`, pinnedTaskID); err != nil {
				t.Errorf("move pinned Task through none state: %v", err)
				return
			}
			if _, err := fixture.pool.Exec(context.Background(), `
				UPDATE agent_task_queue
				SET session_affinity_state='pinned', session_affinity_runtime_id=$1
				WHERE id=$2
			`, newRuntimeID, pinnedTaskID); err != nil {
				t.Errorf("repin Task to a new Runtime: %v", err)
			}
		})
	}}

	scheduler := NewScheduler(fixture.q, fixture.pool, liveness)
	if _, err := scheduler.AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit}); err != nil {
		t.Fatal(err)
	}
	var state, reason string
	var persistedRuntimeID pgtype.UUID
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT session_affinity_state,session_affinity_runtime_id,wait_reason
		FROM agent_task_queue WHERE id=$1
	`, pinnedTaskID).Scan(&state, &persistedRuntimeID, &reason); err != nil {
		t.Fatal(err)
	}
	if state != SessionAffinityPinned || persistedRuntimeID != newRuntimeID || reason != "no_eligible_runtime" {
		t.Fatalf("ABA-drifted Task = %s/%v/%s, want pinned/%v/no_eligible_runtime", state, persistedRuntimeID, reason, newRuntimeID)
	}
}

func TestSchedulerPinnedReasonCASDoesNotOverwriteNewerReason(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	runtimeID := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "offline", time.Now())
	taskID := fixture.addWaiting(t, SessionAffinityPinned, runtimeID, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})
	stale, err := fixture.q.GetAgentTask(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(context.Background(), `
		UPDATE agent_task_queue SET wait_reason='session_runtime_unauthorized' WHERE id=$1
	`, taskID); err != nil {
		t.Fatal(err)
	}

	scheduler := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true})
	if err := scheduler.updatePinnedReason(context.Background(), stale, "session_runtime_offline"); err != nil {
		t.Fatal(err)
	}
	var reason string
	if err := fixture.pool.QueryRow(context.Background(), `SELECT wait_reason FROM agent_task_queue WHERE id=$1`, taskID).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason != "session_runtime_unauthorized" {
		t.Fatalf("stale diagnosis overwrote newer reason: %s", reason)
	}
}

func TestSchedulerRoutingSnapshotDriftRollsBack(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	runtimeID := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	taskID := fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})
	liveness := &schedulerTestLiveness{ok: true, hook: func() {
		if _, err := fixture.pool.Exec(context.Background(), `
			UPDATE agent_task_queue
			SET session_affinity_state='pinned', session_affinity_runtime_id=$1
			WHERE id=$2
		`, runtimeID, taskID); err != nil {
			t.Errorf("mutate routing snapshot: %v", err)
		}
	}}

	scheduler := NewScheduler(fixture.q, fixture.pool, liveness)
	result, err := scheduler.AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assigned) != 0 {
		t.Fatalf("assigned drifted Task: %+v", result.Assigned)
	}
	var status string
	var assignedRuntime pgtype.UUID
	if err := fixture.pool.QueryRow(context.Background(), `SELECT status,runtime_id FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status, &assignedRuntime); err != nil {
		t.Fatal(err)
	}
	if status != StatusWaitingRuntime || assignedRuntime.Valid {
		t.Fatalf("Task after drift = status %s Runtime %+v", status, assignedRuntime)
	}
}

func TestSchedulerConcurrentAssignmentCAS(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 2, time.Time{})

	start := make(chan struct{})
	results := make(chan AssignResult, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			scheduler := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true})
			result, err := scheduler.AssignWaiting(context.Background(), AssignRequest{WorkspaceID: fixture.workspaceID, Limit: AssignmentBatchLimit})
			results <- result
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assigned := 0
	for result := range results {
		assigned += len(result.Assigned)
	}
	if assigned != 1 {
		t.Fatalf("concurrent assignments = %d, want 1", assigned)
	}
}

func TestSchedulerDoesNotPromoteUnresolvedChatTail(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	chatID := fixture.addChat(t)
	taskID := fixture.addWaiting(t, SessionAffinityUnresolved, pgtype.UUID{}, chatID, "deferred", 2, time.Now().Add(-time.Minute))
	promoted, err := fixture.q.PromoteDuePoolDeferredTasksForWorkspace(context.Background(), db.PromoteDuePoolDeferredTasksForWorkspaceParams{
		PlacementWorkspaceID: fixture.workspaceID,
		Now:                  pgtype.Timestamptz{Time: time.Now(), Valid: true},
		PromoteLimit:         DeferredPromotionLimit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(promoted) != 0 {
		t.Fatalf("promoted unresolved Chat tail: %+v", promoted)
	}
	var status, reason string
	if err := fixture.pool.QueryRow(context.Background(), `SELECT status,wait_reason FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "deferred" || reason != "chat_predecessor_pending" {
		t.Fatalf("unresolved tail = %s/%s", status, reason)
	}
}

func TestSchedulerSweepDoesNotReportPromotionAssignedBeforeResult(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	runtimeID := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	taskID := fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "deferred", 2, time.Now().Add(-time.Minute))

	var once sync.Once
	liveness := &schedulerTestLiveness{ok: true, hook: func() {
		once.Do(func() {
			if _, err := fixture.q.AssignWaitingPoolTask(context.Background(), db.AssignWaitingPoolTaskParams{
				RuntimeID:            runtimeID,
				TaskID:               taskID,
				PlacementWorkspaceID: fixture.workspaceID,
			}); err != nil {
				t.Errorf("concurrent assignment after promotion: %v", err)
			}
		})
	}}
	scheduler := NewScheduler(fixture.q, fixture.pool, liveness)
	scheduler.sweepCursor = uuidPredecessor(fixture.workspaceID)

	results, err := scheduler.SweepWaiting(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		for _, promoted := range result.PromotedWaiting {
			if promoted.ID == taskID {
				t.Fatalf("Sweep reported stale promoted Task after concurrent assignment: %+v", promoted)
			}
		}
	}
	var status string
	var persistedRuntimeID pgtype.UUID
	if err := fixture.pool.QueryRow(context.Background(), `SELECT status,runtime_id FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status, &persistedRuntimeID); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || persistedRuntimeID != runtimeID {
		t.Fatalf("persisted Task = %s/%v, want queued/%v", status, persistedRuntimeID, runtimeID)
	}
}

func TestSchedulerPromotedPinnedReasonIsNeutralUntilDiagnosed(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	runtimeID := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "offline", time.Now())
	preservedTaskID := fixture.addWaiting(t, SessionAffinityPinned, runtimeID, pgtype.UUID{}, "deferred", 2, time.Now().Add(-time.Minute))
	defaultedTaskID := fixture.addWaiting(t, SessionAffinityPinned, runtimeID, pgtype.UUID{}, "deferred", 1, time.Now().Add(-time.Minute))
	if _, err := fixture.pool.Exec(context.Background(), `
		UPDATE agent_task_queue SET wait_reason='session_runtime_unauthorized' WHERE id=$1
	`, preservedTaskID); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.q.PromoteDuePoolDeferredTasksForWorkspace(context.Background(), db.PromoteDuePoolDeferredTasksForWorkspaceParams{
		PlacementWorkspaceID: fixture.workspaceID,
		Now:                  pgtype.Timestamptz{Time: time.Now(), Valid: true},
		PromoteLimit:         DeferredPromotionLimit,
	}); err != nil {
		t.Fatal(err)
	}
	wants := map[pgtype.UUID]string{
		preservedTaskID: "no_eligible_runtime",
		defaultedTaskID: "no_eligible_runtime",
	}
	for taskID, wantReason := range wants {
		var status, reason string
		if err := fixture.pool.QueryRow(context.Background(), `SELECT status,wait_reason FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status, &reason); err != nil {
			t.Fatal(err)
		}
		if status != StatusWaitingRuntime || reason != wantReason {
			t.Fatalf("promoted pinned Task %v = %s/%s, want waiting_runtime/%s", taskID, status, reason, wantReason)
		}
	}
}

func TestSchedulerPromotedPinnedOutsideWaitingScanStaysNeutral(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	runtimeID := fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	for range WaitingTaskScanLimit + 1 {
		fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "waiting_runtime", 100, time.Time{})
	}
	pinnedTaskID := fixture.addWaiting(t, SessionAffinityPinned, runtimeID, pgtype.UUID{}, "deferred", -100, time.Now().Add(-time.Minute))
	scheduler := NewScheduler(fixture.q, fixture.pool, &schedulerTestLiveness{ok: true})
	scheduler.sweepCursor = uuidPredecessor(fixture.workspaceID)
	results, err := scheduler.SweepWaiting(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, result := range results {
		for _, task := range result.PromotedWaiting {
			if task.ID == pinnedTaskID {
				found = true
				if task.Status != StatusWaitingRuntime || task.WaitReason.String != "no_eligible_runtime" {
					t.Fatalf("scan-outside pinned promotion = %s/%s", task.Status, task.WaitReason.String)
				}
			}
		}
	}
	if !found {
		t.Fatal("scan-outside pinned promotion missing from durable Sweep result")
	}
	var status, reason string
	if err := fixture.pool.QueryRow(context.Background(), `SELECT status,wait_reason FROM agent_task_queue WHERE id=$1`, pinnedTaskID).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != StatusWaitingRuntime || reason != "no_eligible_runtime" {
		t.Fatalf("persisted scan-outside pinned Task = %s/%s, want waiting_runtime/no_eligible_runtime", status, reason)
	}
}

func TestSchedulerSweepWrapsCursor(t *testing.T) {
	pool := pooltestdb.Open(t)
	zeroID := pgtype.UUID{Valid: true}
	suffix := fmt.Sprintf("%d-%d", time.Now().UnixNano(), schedulerFixtureSequence.Add(1))
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO workspace (id,name,slug) VALUES ($1,'Pool Sweep Cursor Zero',$2)
	`, zeroID, "pool-sweep-cursor-zero-"+suffix); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id=$1`, zeroID)
	})
	maxID := pgtype.UUID{Valid: true}
	for i := range maxID.Bytes {
		maxID.Bytes[i] = 0xff
	}
	scheduler := NewScheduler(db.New(pool), pool, nil)
	scheduler.sweepCursor = maxID
	results, err := scheduler.SweepWaiting(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("empty wrapped Workspace produced results: %+v", results)
	}
	if scheduler.sweepCursor != zeroID {
		t.Fatalf("wrapped cursor = %v, want first Workspace %v", scheduler.sweepCursor, zeroID)
	}
}

func TestSchedulerSweepKeepsAllCommittedPromotionsAndJoinsErrors(t *testing.T) {
	fixture := newSchedulerDBFixture(t)
	fixture.addRuntime(t, fixture.userID, "local", "private", []string{CapabilityExtensionExecuteV1}, "online", time.Now())
	fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "deferred", 2, time.Now().Add(-time.Minute))
	fixture.addWaiting(t, SessionAffinityNone, pgtype.UUID{}, pgtype.UUID{}, "deferred", 1, time.Now().Add(-time.Minute))
	firstWorkspaceID := uuidPredecessor(fixture.workspaceID)
	initialCursor := uuidPredecessor(firstWorkspaceID)
	suffix := fmt.Sprintf("%d-%d", time.Now().UnixNano(), schedulerFixtureSequence.Add(1))
	if _, err := fixture.pool.Exec(context.Background(), `
		INSERT INTO workspace (id,name,slug) VALUES ($1,'Pool Sweep Prefix',$2)
	`, firstWorkspaceID, "pool-sweep-prefix-"+suffix); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = fixture.pool.Exec(context.Background(), `DELETE FROM workspace WHERE id=$1`, firstWorkspaceID)
	})
	assignErr := errors.New("assignment sentinel")
	verifyErr := errors.New("verification sentinel")
	scheduler := NewScheduler(fixture.q, schedulerFailingTxStarter{err: assignErr}, &schedulerTestLiveness{ok: true})
	scheduler.sweepCursor = initialCursor
	batchReads := 0
	scheduler.getTasks = func(context.Context, []pgtype.UUID) ([]db.AgentTaskQueue, error) {
		batchReads++
		return nil, verifyErr
	}
	results, err := scheduler.SweepWaiting(context.Background(), 2)
	if !errors.Is(err, assignErr) || !errors.Is(err, verifyErr) {
		t.Fatalf("Sweep error = %v, want joined assignment and verification errors", err)
	}
	if batchReads != 1 {
		t.Fatalf("promotion verification batch reads = %d, want 1", batchReads)
	}
	if len(results) != 1 || len(results[0].PromotedWaiting) != 2 || len(results[0].Assigned) != 0 {
		t.Fatalf("partial Sweep results = %+v, want both committed promotions", results)
	}
	for _, promoted := range results[0].PromotedWaiting {
		if promoted.Status != StatusWaitingRuntime {
			t.Fatalf("committed promotion fallback = %+v", promoted)
		}
	}
	if scheduler.sweepCursor != firstWorkspaceID {
		t.Fatalf("cursor after second Workspace failure = %v, want prior success %v", scheduler.sweepCursor, firstWorkspaceID)
	}
}

func uuidPredecessor(id pgtype.UUID) pgtype.UUID {
	predecessor := id
	for i := len(predecessor.Bytes) - 1; i >= 0; i-- {
		if predecessor.Bytes[i] != 0 {
			predecessor.Bytes[i]--
			return predecessor
		}
		predecessor.Bytes[i] = 0xff
	}
	return pgtype.UUID{}
}

var schedulerFixtureSequence atomic.Uint64

type schedulerDBFixture struct {
	pool        *pgxpool.Pool
	q           *db.Queries
	workspaceID pgtype.UUID
	userID      pgtype.UUID
	agentID     pgtype.UUID
}

func newSchedulerDBFixture(t *testing.T) *schedulerDBFixture {
	t.Helper()
	pool := pooltestdb.Open(t)
	ctx := context.Background()
	sequence := schedulerFixtureSequence.Add(1)
	suffix := fmt.Sprintf("%d-%d", time.Now().UnixNano(), sequence)
	fixture := &schedulerDBFixture{pool: pool, q: db.New(pool)}
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name,email) VALUES ('Pool Scheduler Test',$1) RETURNING id`, "pool-scheduler-"+suffix+"@example.test").Scan(&fixture.userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name,slug) VALUES ('Pool Scheduler Test',$1) RETURNING id`, "pool-scheduler-"+suffix).Scan(&fixture.workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id,user_id,role) VALUES ($1,$2,'member')`, fixture.workspaceID, fixture.userID); err != nil {
		t.Fatal(err)
	}
	requirements := `{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["multica.extension.execute/v1"]}`
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id,name,runtime_mode,runtime_config,visibility,status,
			max_concurrent_tasks,owner_id,instructions,runtime_binding_mode,runtime_requirements
		) VALUES ($1,'Pool Scheduler Agent','pool','{}','private','offline',1,$2,'','pool',$3::jsonb)
		RETURNING id
	`, fixture.workspaceID, fixture.userID, requirements).Scan(&fixture.agentID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id=$1`, fixture.workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, fixture.userID)
	})
	return fixture
}

func (f *schedulerDBFixture) addRuntime(t *testing.T, ownerID pgtype.UUID, mode, visibility string, capabilities []string, status string, seen time.Time) pgtype.UUID {
	return f.addRuntimeWithProvider(t, ownerID, mode, visibility, capabilities, status, seen, "test-runtime")
}

func (f *schedulerDBFixture) addRuntimeWithProvider(t *testing.T, ownerID pgtype.UUID, mode, visibility string, capabilities []string, status string, seen time.Time, provider string) pgtype.UUID {
	t.Helper()
	var runtimeID pgtype.UUID
	err := f.pool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id,daemon_id,name,runtime_mode,provider,status,owner_id,
			visibility,capabilities,last_seen_at
		) VALUES ($1,gen_random_uuid()::text,'Pool Scheduler Runtime',$2,$3,$4,$5,$6,$7,$8)
		RETURNING id
	`, f.workspaceID, mode, provider, status, ownerID, visibility, capabilities, seen).Scan(&runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	return runtimeID
}

func (f *schedulerDBFixture) addCrossWorkspaceRuntime(t *testing.T, capabilities []string, status string, seen time.Time) pgtype.UUID {
	t.Helper()
	var workspaceID, runtimeID pgtype.UUID
	suffix := fmt.Sprintf("%d-%d", time.Now().UnixNano(), schedulerFixtureSequence.Add(1))
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO workspace (name,slug) VALUES ('Pool Scheduler Foreign',$1) RETURNING id
	`, "pool-scheduler-foreign-"+suffix).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO member (workspace_id,user_id,role) VALUES ($1,$2,'member')
	`, workspaceID, f.userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM workspace WHERE id=$1`, workspaceID)
	})
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id,daemon_id,name,runtime_mode,provider,status,owner_id,
			visibility,capabilities,last_seen_at
		) VALUES ($1,gen_random_uuid()::text,'Foreign Pool Scheduler Runtime','local','test-runtime',$2,$3,'public',$4,$5)
		RETURNING id
	`, workspaceID, status, f.userID, capabilities, seen).Scan(&runtimeID); err != nil {
		t.Fatal(err)
	}
	return runtimeID
}

func (f *schedulerDBFixture) addWaiting(t *testing.T, affinity string, affinityRuntimeID, chatID pgtype.UUID, status string, priority int32, fireAt time.Time) pgtype.UUID {
	t.Helper()
	var taskID pgtype.UUID
	reason := "no_eligible_runtime"
	if affinity == SessionAffinityUnresolved {
		reason = "chat_predecessor_pending"
	}
	var fire pgtype.Timestamptz
	if !fireAt.IsZero() {
		fire = pgtype.Timestamptz{Time: fireAt, Valid: true}
	}
	err := f.pool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id,status,priority,chat_session_id,fire_at,runtime_binding_mode,
			runtime_requirements,placement_workspace_id,runtime_requester_user_id,
			runtime_trigger_user_id,session_affinity_state,session_affinity_runtime_id,wait_reason
		) VALUES ($1,$2,$3,$4,$5,'pool',
			'{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["multica.extension.execute/v1"]}'::jsonb,
			$6,$7,$7,$8,$9,$10)
		RETURNING id
	`, f.agentID, status, priority, chatID, fire, f.workspaceID, f.userID, affinity, affinityRuntimeID, reason).Scan(&taskID)
	if err != nil {
		t.Fatal(err)
	}
	return taskID
}

func (f *schedulerDBFixture) addOccupancy(t *testing.T, runtimeID pgtype.UUID) {
	f.addOccupancyStatus(t, runtimeID, "queued")
}

func (f *schedulerDBFixture) addOccupancyStatus(t *testing.T, runtimeID pgtype.UUID, status string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO agent_task_queue (agent_id,runtime_id,status,runtime_binding_mode)
		VALUES ($1,$2,$3,'fixed')
	`, f.agentID, runtimeID, status); err != nil {
		t.Fatal(err)
	}
}

func (f *schedulerDBFixture) addFixedAgentBinding(t *testing.T, runtimeID pgtype.UUID) pgtype.UUID {
	t.Helper()
	var agentID pgtype.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id,name,runtime_mode,runtime_config,runtime_id,visibility,status,
			max_concurrent_tasks,owner_id,instructions,runtime_binding_mode
		) VALUES ($1,'Fixed Pool Scheduler Control','local','{}',$2,'private','offline',1,$3,'','fixed')
		RETURNING id
	`, f.workspaceID, runtimeID, f.userID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	return agentID
}

func (f *schedulerDBFixture) lockRuntime(t *testing.T, runtimeID pgtype.UUID) pgx.Tx {
	t.Helper()
	tx, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var lockedID pgtype.UUID
	if err := tx.QueryRow(context.Background(), `SELECT id FROM agent_runtime WHERE id=$1 FOR UPDATE`, runtimeID).Scan(&lockedID); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	return tx
}

func waitForBlockedPID(t *testing.T, pool *pgxpool.Pool, pid uint32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		if err := pool.QueryRow(context.Background(), `SELECT cardinality(pg_blocking_pids($1)) > 0`, int32(pid)).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("backend %d did not reach a blocked row lock", pid)
}

func (f *schedulerDBFixture) addChat(t *testing.T) pgtype.UUID {
	t.Helper()
	var chatID pgtype.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO chat_session (workspace_id,agent_id,creator_id,title)
		VALUES ($1,$2,$3,'Pool Scheduler Chat') RETURNING id
	`, f.workspaceID, f.agentID, f.userID).Scan(&chatID); err != nil {
		t.Fatal(err)
	}
	return chatID
}

var _ TxStarter = (*pgxpool.Pool)(nil)
