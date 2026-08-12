package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

// newStageGateTestPool returns a DB pool or skips when the database is
// unreachable.
func newStageGateTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return newReviewSchedulerTestPool(t)
}

// stageGateFixture provisions a user/workspace/runtime/agent/issue and returns
// their ids.
func stageGateFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (wsID, agentID, runtimeID, issueID string) {
	t.Helper()
	fx := newReviewSchedulerFixture(t, ctx, pool)

	var issue string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'stage gate fixture', 'in_progress', 'none', $2, 'member', 30001, 0)
		RETURNING id
	`, fx.workspaceID, fx.agentID).Scan(&issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue) })
	return fx.workspaceID, fx.agentID, fx.runtimeID, issue
}

// seedMemoryGateCandidate inserts a queued MemoryHub task (execution_id
// stamped, memory_policy required) for the agent. Returns its id.
func seedMemoryGateCandidate(t *testing.T, ctx context.Context, pool *pgxpool.Pool, agentID, runtimeID, issueID string) string {
	t.Helper()
	execID := uuid.New().String()
	runID := "run-" + uuid.New().String()
	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			execution_id, memoryhub_run_id, memory_policy, review_policy
		)
		VALUES ($1, $2, $3, 'queued', 0, $4, $5, 'required', 'none')
		RETURNING id
	`, agentID, runtimeID, issueID, execID, runID).Scan(&taskID); err != nil {
		t.Fatalf("seed memory candidate: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

// TestMemoryGateBlocksRequiredTaskWithoutBinding is the B6 stage gate test: a
// required MemoryHub task with no bound memoryhub_binding must NOT be claimed.
// The queue stays queued, the gate outcome is persisted as blocked_required,
// and no token/running/claim response is produced.
//
// Before the B6 wiring, ClaimTask skipped the gate entirely and the task was
// dispatched (fail). After the wiring the gate runs before dispatch and keeps
// the queue queued (pass).
func TestMemoryGateBlocksRequiredTaskWithoutBinding(t *testing.T) {
	pool := newStageGateTestPool(t)
	ctx := context.Background()
	wsID, agentID, runtimeID, issueID := stageGateFixture(t, ctx, pool)
	taskID := seedMemoryGateCandidate(t, ctx, pool, agentID, runtimeID, issueID)

	svc := &TaskService{Queries: db.New(pool), TxStarter: pool, Bus: events.New()}
	claimed, err := svc.ClaimTask(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if claimed != nil {
		t.Fatalf("task %s was claimed (status dispatched) despite missing binding; required gate failure must keep the queue queued", taskID)
	}

	var status, gateState, gateErr string
	if err := pool.QueryRow(ctx, `
		SELECT status, COALESCE(memory_gate_state, ''), COALESCE(memory_gate_error_code, '')
		FROM agent_task_queue WHERE id = $1
	`, taskID).Scan(&status, &gateState, &gateErr); err != nil {
		t.Fatalf("read task: %v", err)
	}
	if status != "queued" {
		t.Fatalf("task status = %s, want queued (gate must keep the queue queued)", status)
	}
	if gateState != string(GateBlockedRequired) {
		t.Fatalf("memory_gate_state = %q, want %q", gateState, GateBlockedRequired)
	}
	if gateErr == "" {
		t.Fatal("memory_gate_error_code must be persisted for a required failure")
	}
	_ = wsID
}

// TestMemoryGateAllowsReadyTask is the B6 positive path: once a bound binding
// exists, the same candidate IS claimed and the gate state becomes ready.
func TestMemoryGateAllowsReadyTask(t *testing.T) {
	pool := newStageGateTestPool(t)
	ctx := context.Background()
	wsID, agentID, runtimeID, issueID := stageGateFixture(t, ctx, pool)

	taskID := seedMemoryGateCandidate(t, ctx, pool, agentID, runtimeID, issueID)

	// A bound binding for the issue scope (projectless -> workspace scope).
	bindingID := uuid.New().String()
	if _, err := pool.Exec(ctx, `
		INSERT INTO memoryhub_binding (
			id, workspace_id, scope_kind, scope_id, subject_type, subject_id,
			status, version, idempotency_key, remote_name
		)
		VALUES ($1, $2, 'workspace', NULL, 'issue', $3, 'bound', 1, $4, 'remote')
	`, bindingID, wsID, issueID, "idem-"+bindingID); err != nil {
		t.Fatalf("seed binding: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM memoryhub_binding WHERE id = $1`, bindingID) })

	// An active secret (credential_ref == workspace id as resolved by the gate).
	secretID := uuid.New().String()
	credRef := wsID
	if _, err := pool.Exec(ctx, `
		INSERT INTO memoryhub_secret (
			id, workspace_id, credential_ref, kind, envelope_version, key_id,
			nonce, ciphertext, aad, user_key_hash, state, state_version
		)
		VALUES ($1, $2, $3, 'user_key', 1, 'k1', decode('00','hex'), decode('00','hex'), 'aad', NULL, 'active', 1)
	`, secretID, wsID, credRef); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM memoryhub_secret WHERE id = $1`, secretID) })

	// A docket for the issue subject, and an attachment ref on the task.
	docketID := uuid.New().String()
	if _, err := pool.Exec(ctx, `
		INSERT INTO memoryhub_memory_docket (
			id, workspace_id, scope_kind, scope_id, subject_type, subject_id, revision
		)
		VALUES ($1, $2, 'workspace', NULL, 'issue', $3, 1)
	`, docketID, wsID, issueID); err != nil {
		t.Fatalf("seed docket: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM memoryhub_memory_docket WHERE id = $1`, docketID) })
	if _, err := pool.Exec(ctx, `
		UPDATE agent_task_queue SET memory_attachment_ref = 'attachment://' || $2 WHERE id = $1
	`, taskID, uuid.New().String()); err != nil {
		t.Fatalf("stamp attachment ref: %v", err)
	}

	svc := &TaskService{Queries: db.New(pool), TxStarter: pool, Bus: events.New()}
	claimed, err := svc.ClaimTask(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if claimed == nil {
		t.Fatalf("task %s was not claimed despite a bound binding", taskID)
	}
	if claimed.Status != "dispatched" {
		t.Fatalf("claimed status = %s, want dispatched", claimed.Status)
	}
	if !claimed.ExecutionID.Valid {
		t.Fatal("claimed task must carry an execution_id after gate commit")
	}
}

var _ = fmt.Sprintf
var _ = time.Now
var _ = pgtype.UUID{}
