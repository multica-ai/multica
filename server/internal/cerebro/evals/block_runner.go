package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/cerebro/loops"
)

// BlockRunner is the trusted Chain v2 adapter: it resolves the eval by the
// workflow binding and key, executes it server-side, and returns its verdict.
type BlockRunner struct {
	pool  *pgxpool.Pool
	store *Store
}

func NewBlockRunner(pool *pgxpool.Pool, executor RunExecutor) *BlockRunner {
	return &BlockRunner{pool: pool, store: NewStore(pool).WithRunExecutor(executor)}
}

func (r *BlockRunner) RunEvalBlock(ctx context.Context, d loops.BlockDispatch) (loops.StepStatus, json.RawMessage, error) {
	workflowID := uuid.UUID(d.Run.WorkflowID.Bytes)
	issueID := uuid.UUID(d.Run.IssueID.Bytes)
	var workspaceID, evalID uuid.UUID
	var actor pgtype.UUID
	err := r.pool.QueryRow(ctx, `SELECT i.workspace_id, b.eval_id, i.creator_id FROM issue i JOIN cerebro_workflow_eval_binding b ON b.workspace_id=i.workspace_id AND b.workflow_id=$2 JOIN cerebro_eval e ON e.id=b.eval_id WHERE i.id=$1 AND e.eval_key=$3 AND b.blocking=true ORDER BY b.created_at DESC LIMIT 1`, issueID, workflowID, d.Block.EvalKey).Scan(&workspaceID, &evalID, &actor)
	if err != nil {
		return loops.StepFailed, nil, fmt.Errorf("resolve eval block %q: %w", d.Block.EvalKey, err)
	}
	now := time.Now()
	if !actor.Valid {
		return loops.StepFailed, nil, fmt.Errorf("eval block issue has no creator")
	}
	actorID := uuid.UUID(actor.Bytes)
	run, err := r.store.CreateRun(ctx, workspaceID, actorID, evalID, "system", EvalRunInput{WorkflowID: &workflowID, IssueID: &issueID, StartedAt: &now})
	if err != nil {
		return loops.StepFailed, nil, err
	}
	outcome, _ := json.Marshal(map[string]any{"eval_run_id": run.ID, "status": run.Status})
	if run.Status == RunStatusPassed {
		return loops.StepCompleted, outcome, nil
	}
	return loops.StepFailed, outcome, nil
}
