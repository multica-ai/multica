package service

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestClaimTasksForRuntimesAttempt_ReplayReturnsSameTaskSet(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	rt1, rt2 := batchClaimFixture(t, ctx, pool)
	attemptID := util.MustParseUUID(uuid.NewString())
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM daemon_claim_attempt WHERE id = $1`, attemptID)
	})

	req := ClaimAttemptRequest{
		ID:                 attemptID,
		DaemonID:           "daemon-batch",
		PrincipalKey:       "daemon:test:daemon-batch",
		RequestFingerprint: "same-request",
		RuntimeIDs:         []pgtype.UUID{util.MustParseUUID(rt1), util.MustParseUUID(rt2)},
		MaxTasks:           2,
	}
	first, err := svc.ClaimTasksForRuntimesAttempt(ctx, req)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	second, err := svc.ClaimTasksForRuntimesAttempt(ctx, req)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if first.Replayed || !second.Replayed {
		t.Fatalf("replay flags = first:%v second:%v", first.Replayed, second.Replayed)
	}
	if len(first.Tasks) != 2 || len(second.Tasks) != 2 {
		t.Fatalf("task counts = first:%d second:%d, want 2/2", len(first.Tasks), len(second.Tasks))
	}
	firstIDs := []string{util.UUIDToString(first.Tasks[0].ID), util.UUIDToString(first.Tasks[1].ID)}
	secondIDs := []string{util.UUIDToString(second.Tasks[0].ID), util.UUIDToString(second.Tasks[1].ID)}
	if !slices.Equal(firstIDs, secondIDs) {
		t.Fatalf("replay task ids = %v, want %v", secondIDs, firstIDs)
	}

	var mapped int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE claim_attempt_id = $1`, attemptID).Scan(&mapped); err != nil {
		t.Fatalf("count mapped tasks: %v", err)
	}
	if mapped != 2 {
		t.Fatalf("mapped tasks = %d, want 2", mapped)
	}
	if _, err := svc.StartTask(ctx, first.Tasks[0].ID); err != nil {
		t.Fatalf("start mapped task: %v", err)
	}
	var attemptStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM daemon_claim_attempt WHERE id = $1`, attemptID).Scan(&attemptStatus); err != nil {
		t.Fatalf("load attempt status: %v", err)
	}
	if attemptStatus != "acknowledged" {
		t.Fatalf("StartTask implicit ack status = %s, want acknowledged", attemptStatus)
	}
}

func TestClaimTasksForRuntimesAttempt_ConcurrentReplayClaimsOnce(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	rt1, rt2 := batchClaimFixture(t, ctx, pool)
	attemptID := util.MustParseUUID(uuid.NewString())
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM daemon_claim_attempt WHERE id = $1`, attemptID)
	})
	req := ClaimAttemptRequest{
		ID:                 attemptID,
		DaemonID:           "daemon-batch",
		PrincipalKey:       "daemon:test:daemon-batch",
		RequestFingerprint: "concurrent-request",
		RuntimeIDs:         []pgtype.UUID{util.MustParseUUID(rt1), util.MustParseUUID(rt2)},
		MaxTasks:           2,
	}

	results := make([]ClaimAttemptResult, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = svc.ClaimTasksForRuntimesAttempt(ctx, req)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if len(results[0].Tasks) != 2 || len(results[1].Tasks) != 2 {
		t.Fatalf("concurrent task counts = %d/%d, want 2/2", len(results[0].Tasks), len(results[1].Tasks))
	}
	if results[0].Replayed == results[1].Replayed {
		t.Fatalf("exactly one call should execute and one replay: flags=%v/%v", results[0].Replayed, results[1].Replayed)
	}

	var mapped int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE claim_attempt_id = $1`, attemptID).Scan(&mapped); err != nil {
		t.Fatalf("count mapped tasks: %v", err)
	}
	if mapped != 2 {
		t.Fatalf("concurrent calls mapped %d tasks, want max_tasks=2", mapped)
	}
}

func TestClaimTasksForRuntimesAttempt_RejectsKeyReuseAndOtherPrincipal(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	rt1, _ := batchClaimFixture(t, ctx, pool)
	attemptID := util.MustParseUUID(uuid.NewString())
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM daemon_claim_attempt WHERE id = $1`, attemptID)
	})
	req := ClaimAttemptRequest{
		ID:                 attemptID,
		DaemonID:           "daemon-batch",
		PrincipalKey:       "daemon:test:daemon-batch",
		RequestFingerprint: "original",
		RuntimeIDs:         []pgtype.UUID{util.MustParseUUID(rt1)},
		MaxTasks:           1,
	}
	if _, err := svc.ClaimTasksForRuntimesAttempt(ctx, req); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	mismatch := req
	mismatch.RequestFingerprint = "changed"
	if _, err := svc.ClaimTasksForRuntimesAttempt(ctx, mismatch); !errors.Is(err, ErrClaimAttemptMismatch) {
		t.Fatalf("fingerprint reuse error = %v, want mismatch", err)
	}
	foreign := req
	foreign.PrincipalKey = "daemon:other:daemon-batch"
	if _, err := svc.ClaimTasksForRuntimesAttempt(ctx, foreign); !errors.Is(err, ErrClaimAttemptNotFound) {
		t.Fatalf("cross-principal replay error = %v, want not found", err)
	}
}

func TestClaimTasksForRuntimesAttempt_ExpiryAllowsStaleTaskRecovery(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	rt1, _ := batchClaimFixture(t, ctx, pool)
	runtimeIDs := []pgtype.UUID{util.MustParseUUID(rt1)}
	oldID := util.MustParseUUID(uuid.NewString())
	newID := util.MustParseUUID(uuid.NewString())
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM daemon_claim_attempt WHERE id IN ($1, $2)`, oldID, newID)
	})

	oldResult, err := svc.ClaimTasksForRuntimesAttempt(ctx, ClaimAttemptRequest{
		ID: oldID, DaemonID: "daemon-batch", PrincipalKey: "daemon:test:daemon-batch",
		RequestFingerprint: "old", RuntimeIDs: runtimeIDs, MaxTasks: 1,
	})
	if err != nil || len(oldResult.Tasks) != 1 {
		t.Fatalf("old claim: tasks=%d err=%v", len(oldResult.Tasks), err)
	}
	oldTaskID := oldResult.Tasks[0].ID
	if _, err := pool.Exec(ctx, `UPDATE daemon_claim_attempt SET expires_at = now() - interval '1 second' WHERE id = $1`, oldID); err != nil {
		t.Fatalf("age old attempt: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET dispatched_at = now() - interval '2 minutes', prepare_lease_expires_at = NULL
		WHERE id = $1
	`, oldTaskID); err != nil {
		t.Fatalf("age old task: %v", err)
	}

	newResult, err := svc.ClaimTasksForRuntimesAttempt(ctx, ClaimAttemptRequest{
		ID: newID, DaemonID: "daemon-batch", PrincipalKey: "daemon:test:daemon-batch",
		RequestFingerprint: "new", RuntimeIDs: runtimeIDs, MaxTasks: 1,
	})
	if err != nil || len(newResult.Tasks) != 1 {
		t.Fatalf("recovery claim: tasks=%d err=%v", len(newResult.Tasks), err)
	}
	if newResult.Tasks[0].ID != oldTaskID {
		t.Fatalf("recovered task = %s, want %s", util.UUIDToString(newResult.Tasks[0].ID), util.UUIDToString(oldTaskID))
	}
	if newResult.Tasks[0].ClaimAttemptID != newID {
		t.Fatalf("recovered task attempt = %s, want %s", util.UUIDToString(newResult.Tasks[0].ClaimAttemptID), util.UUIDToString(newID))
	}
}
