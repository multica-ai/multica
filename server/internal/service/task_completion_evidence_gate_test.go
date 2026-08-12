package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

// newEvidenceGateTestPool returns a DB pool or skips the test when the
// database is unreachable.
func newEvidenceGateTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return newReviewSchedulerTestPool(t)
}

// seedEvidenceGateTask provisions a running task carrying an execution snapshot
// (execution_id + memoryhub_run_id + memory_policy) plus its execution ledger
// row, so the evidence gate has a real MemoryHub execution to evaluate.
func seedEvidenceGateTask(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (db.AgentTaskQueue, string) {
	t.Helper()
	fx := newReviewSchedulerFixture(t, ctx, pool)

	execID := uuid.New().String()
	runID := "run-" + uuid.New().String()
	taskID := uuid.New().String()

	var taskID2 string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			execution_id, memoryhub_run_id, memory_policy, review_policy
		)
		VALUES ($1, $2, NULL, 'running', 0, $3, $4, 'required', 'none')
		RETURNING id
	`, fx.agentID, fx.runtimeID, execID, runID).Scan(&taskID2); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	_ = taskID

	if _, err := pool.Exec(ctx, `
		INSERT INTO execution_ledger (
			execution_id, attempt, task_id, task_version, workspace_id,
			scope_kind, run_id, agent_id, runtime_id, model, state, origin,
			idempotency_key, review_policy
		)
		VALUES ($1, 1, $2, 1, $3, 'workspace', $4, $5, $6, 'test-model', 'running',
		        'enqueue', $7, 'none')
	`, execID, taskID2, fx.workspaceID, runID, fx.agentID, fx.runtimeID, "key-"+taskID2); err != nil {
		t.Fatalf("seed execution ledger: %v", err)
	}

	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM execution_evidence_record WHERE execution_id = $1`, execID)
		pool.Exec(context.Background(), `DELETE FROM execution_ledger WHERE execution_id = $1`, execID)
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID2)
	})

	task, err := db.New(pool).GetAgentTask(ctx, util.MustParseUUID(taskID2))
	if err != nil {
		t.Fatalf("load seeded task: %v", err)
	}
	return task, fx.workspaceID
}

// TestEvidenceGateBlocksMissingOutput is the B10 negative: completing a running
// MemoryHub execution with no output must NOT complete the task. It follows the
// failure path with the empty_or_unparseable_output stop_reason.
func TestEvidenceGateBlocksMissingOutput(t *testing.T) {
	pool := newEvidenceGateTestPool(t)
	ctx := context.Background()
	task, _ := seedEvidenceGateTask(t, ctx, pool)
	svc := &TaskService{Queries: db.New(pool), TxStarter: pool, Bus: events.New()}

	input := CompletionInput{
		OutputPresent: false,
		MessageCount:  1,
		UsagePresent:  true,
		Artifacts:     map[string]ArtifactEvidence{},
		Tests:         map[string]bool{},
	}
	result, _ := json.Marshal(map[string]string{"output": ""})
	res, err := svc.CompleteTaskWithRuntimeEvidenceGate(ctx, task.ID, result, "", "", false, "", input)
	if err != nil {
		t.Fatalf("CompleteTaskWithRuntimeEvidenceGate: %v", err)
	}
	if res.Status != "failed" {
		t.Fatalf("task status = %s, want failed (missing evidence must never transiently complete)", res.Status)
	}
	if res.FailureReason.String != string(MissingOutput) {
		t.Fatalf("failure_reason = %q, want %q", res.FailureReason.String, MissingOutput)
	}
}

// TestEvidenceGateCompletesWithFullEvidence is the B10 positive: all five
// categories present -> the task completes and the evidence record is persisted
// in 'complete' state with review state 'not_required' (policy none).
func TestEvidenceGateCompletesWithFullEvidence(t *testing.T) {
	pool := newEvidenceGateTestPool(t)
	ctx := context.Background()
	task, _ := seedEvidenceGateTask(t, ctx, pool)
	svc := &TaskService{Queries: db.New(pool), TxStarter: pool, Bus: events.New()}

	input := CompletionInput{
		OutputPresent: true,
		Output:        "done",
		MessageCount:  1,
		UsagePresent:  true,
		Artifacts:     map[string]ArtifactEvidence{},
		Tests:         map[string]bool{},
	}
	result, _ := json.Marshal(map[string]string{"output": "done"})
	res, err := svc.CompleteTaskWithRuntimeEvidenceGate(ctx, task.ID, result, "", "", false, "", input)
	if err != nil {
		t.Fatalf("CompleteTaskWithRuntimeEvidenceGate: %v", err)
	}
	if res.Status != "completed" {
		t.Fatalf("task status = %s, want completed", res.Status)
	}

	record, err := db.New(pool).GetExecutionEvidenceRecord(ctx, task.ExecutionID)
	if err != nil {
		t.Fatalf("evidence record not persisted: %v", err)
	}
	if record.RuntimeEvidenceState != "complete" {
		t.Fatalf("runtime_evidence_state = %s, want complete", record.RuntimeEvidenceState)
	}
	if record.ReviewState != "not_required" {
		t.Fatalf("review_state = %s, want not_required (policy none)", record.ReviewState)
	}
}

// TestEvidenceGateMissingReviewerIsNotFailure locks V4-5.1: an independent
// policy with a missing reviewer blocks the review workflow (blocked, no
// wakeup) but must NOT convert a valid runtime completion into a failure.
func TestEvidenceGateMissingReviewerIsNotFailure(t *testing.T) {
	pool := newEvidenceGateTestPool(t)
	ctx := context.Background()
	task, _ := seedEvidenceGateTask(t, ctx, pool)

	// Flip the review policy to independent with no reviewer: initial review
	// state must be blocked memoryhub_reviewer_unavailable, wakeup null.
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET review_policy = 'independent' WHERE id = $1`, task.ID); err != nil {
		t.Fatalf("set independent review policy: %v", err)
	}

	svc := &TaskService{Queries: db.New(pool), TxStarter: pool, Bus: events.New()}
	input := CompletionInput{
		OutputPresent: true,
		Output:        "done",
		MessageCount:  1,
		UsagePresent:  true,
		Artifacts:     map[string]ArtifactEvidence{},
		Tests:         map[string]bool{},
	}
	result, _ := json.Marshal(map[string]string{"output": "done"})
	res, err := svc.CompleteTaskWithRuntimeEvidenceGate(ctx, task.ID, result, "", "", false, "", input)
	if err != nil {
		t.Fatalf("CompleteTaskWithRuntimeEvidenceGate: %v", err)
	}
	if res.Status != "completed" {
		t.Fatalf("task status = %s, want completed (missing reviewer must not fail a valid run)", res.Status)
	}

	record, err := db.New(pool).GetExecutionEvidenceRecord(ctx, task.ExecutionID)
	if err != nil {
		t.Fatalf("evidence record not persisted: %v", err)
	}
	if record.ReviewState != "blocked" {
		t.Fatalf("review_state = %s, want blocked", record.ReviewState)
	}
	if record.ReviewFailureCode.String != "memoryhub_reviewer_unavailable" {
		t.Fatalf("review_failure_code = %q, want memoryhub_reviewer_unavailable", record.ReviewFailureCode.String)
	}
	if record.ReviewNextWakeup.Valid {
		t.Fatalf("blocked review must not carry a scheduler wakeup (V5-7)")
	}
}

var _ = pgtype.UUID{}
