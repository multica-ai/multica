package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// failNamedQueryTxStarter wraps the pool so a single named statement fails when
// it runs inside the transaction, while every other statement executes for
// real. OpenRuntimeProviderCircuit is a :many query (pgx Query), so the Exec-only
// injectors already in the package cannot reach it — this one intercepts Query.
type failNamedQueryTxStarter struct {
	pool      *pgxpool.Pool
	queryName string
	err       error
}

func (s *failNamedQueryTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &failNamedQueryTx{Tx: tx, queryName: s.queryName, err: s.err}, nil
}

type failNamedQueryTx struct {
	pgx.Tx
	queryName string
	err       error
}

func (t *failNamedQueryTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, t.queryName) {
		return nil, t.err
	}
	return t.Tx.Query(ctx, sql, args...)
}

// TestFailTaskCircuitWriteFailureIsFailClosed is the F7 regression (SE-37711 /
// SE-37664): the provider circuit for a terminal quota/auth refusal is opened in
// the SAME transaction as the terminal status, so a lost circuit write can never
// commit a failed task while leaving the runtime readable as "closed" — which
// would let the autopilot selector dispatch straight back into the exhausted
// provider and storm.
//
//   - fail-closed: when the OpenRuntimeProviderCircuit write is injected to fail,
//     FailTask returns an error, the task stays `running` (the whole transaction
//     rolled back), and no circuit row exists;
//   - recovery: re-driving the exact same failure on a healthy handle commits
//     both the terminal `failed` status AND the open circuit atomically.
func TestFailTaskCircuitWriteFailureIsFailClosed(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load agent runtime: %v", err)
	}
	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue
			(agent_id, runtime_id, chat_session_id, status, priority, attempt, max_attempts)
		VALUES ($1, $2, $3, 'running', 0, 1, 1)
		RETURNING id`, agentID, runtimeID, chatSessionID).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM runtime_provider_circuit WHERE runtime_id = $1`, runtimeID)
		_, _ = pool.Exec(bg, `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
		_, _ = pool.Exec(bg, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		_, _ = pool.Exec(bg, `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})

	quotaReason := string(taskfailure.ReasonAgentProviderQuotaLimit)
	const quotaErr = "You've hit your org's monthly usage limit"

	// 1. Fail-closed: the injected circuit-write failure aborts the whole fail.
	injected := errors.New("injected circuit write failure")
	failing := &TaskService{
		Queries:   q,
		TxStarter: &failNamedQueryTxStarter{pool: pool, queryName: "OpenRuntimeProviderCircuit", err: injected},
		Bus:       events.New(),
	}
	if _, err := failing.FailTask(ctx, util.MustParseUUID(taskID), quotaErr, "", "", "", quotaReason, false, "", ""); err == nil {
		t.Fatal("FailTask returned nil, want an error when the circuit write fails (fail-closed)")
	}

	if st := scanTaskStatus(t, pool, taskID); st != "running" {
		t.Fatalf("task status = %q after fail-closed abort, want running (transaction must roll back)", st)
	}
	if n := scanCircuitRowCount(t, pool, runtimeID); n != 0 {
		t.Fatalf("found %d circuit rows after a rolled-back fail, want 0", n)
	}

	// 2. Recovery: the same failure on a healthy handle commits both writes.
	healthy := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	if _, err := healthy.FailTask(ctx, util.MustParseUUID(taskID), quotaErr, "", "", "", quotaReason, false, "", ""); err != nil {
		t.Fatalf("FailTask on healthy handle: %v", err)
	}
	if st := scanTaskStatus(t, pool, taskID); st != "failed" {
		t.Fatalf("task status = %q after recovery, want failed", st)
	}
	state, reason := scanCircuitStateReason(t, pool, runtimeID)
	if state != "open" {
		t.Fatalf("circuit state = %q after recovery, want open", state)
	}
	if reason != circuitClassQuota {
		t.Fatalf("circuit reason = %q, want %q", reason, circuitClassQuota)
	}
}

func scanTaskStatus(t *testing.T, pool *pgxpool.Pool, taskID string) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	return status
}

func scanCircuitRowCount(t *testing.T, pool *pgxpool.Pool, runtimeID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM runtime_provider_circuit WHERE runtime_id = $1`, runtimeID).Scan(&n); err != nil {
		t.Fatalf("count circuit rows: %v", err)
	}
	return n
}

func scanCircuitStateReason(t *testing.T, pool *pgxpool.Pool, runtimeID string) (state, reason string) {
	t.Helper()
	if err := pool.QueryRow(context.Background(),
		`SELECT state, coalesce(reason, '') FROM runtime_provider_circuit WHERE runtime_id = $1`, runtimeID).Scan(&state, &reason); err != nil {
		t.Fatalf("read circuit state/reason: %v", err)
	}
	return state, reason
}
