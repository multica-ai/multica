package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/cerebro/loops"
)

// BlockRunner is the trusted Chain v2 adapter: it resolves the eval by the
// workflow binding and key, executes it server-side, and returns its verdict.
type BlockRunner struct {
	pool   *pgxpool.Pool
	store  *Store
	warner *AdvisoryWarner
}

func NewBlockRunner(pool *pgxpool.Pool, executor RunExecutor) *BlockRunner {
	return &BlockRunner{pool: pool, store: NewStore(pool).WithRunExecutor(executor)}
}

func (r *BlockRunner) WithAdvisoryWarner(warner *AdvisoryWarner) *BlockRunner {
	r.warner = warner
	return r
}

func (r *BlockRunner) RunEvalBlock(ctx context.Context, d loops.BlockDispatch) (loops.StepStatus, json.RawMessage, error) {
	workflowID := uuid.UUID(d.Run.WorkflowID.Bytes)
	issueID := uuid.UUID(d.Run.IssueID.Bytes)
	phase := d.Block.EvalPhase
	if phase == "" {
		phase = "delivery"
	}
	if !validEvalPhase(phase) {
		return loops.StepFailed, nil, fmt.Errorf("resolve eval block %q: invalid phase %q", d.Block.EvalKey, phase)
	}
	var binding Binding
	var actor pgtype.UUID
	var actorType string
	err := r.pool.QueryRow(ctx, `SELECT b.id, i.workspace_id, b.workflow_id, b.eval_id, b.phase, b.blocking, b.created_by_id, b.created_at, e.eval_key, e.version, e.title, i.creator_id, i.creator_type FROM issue i JOIN cerebro_workflow_eval_binding b ON b.workspace_id=i.workspace_id AND b.workflow_id=$2 JOIN cerebro_eval e ON e.id=b.eval_id WHERE i.id=$1 AND e.eval_key=$3 AND b.phase=$4 ORDER BY b.created_at DESC LIMIT 1`, issueID, workflowID, d.Block.EvalKey, phase).Scan(
		&binding.ID, &binding.WorkspaceID, &binding.WorkflowID, &binding.EvalID, &binding.Phase, &binding.Blocking,
		&binding.CreatedByID, &binding.CreatedAt, &binding.EvalKey, &binding.EvalVersion, &binding.EvalTitle, &actor, &actorType)
	if err != nil {
		return loops.StepFailed, nil, fmt.Errorf("resolve eval block %q: %w", d.Block.EvalKey, err)
	}
	// Monitor is observability-only, including for legacy rows created before
	// the API rejected blocking monitor bindings.
	blocking := binding.Blocking && phase != "monitor"
	now := time.Now()
	if !actor.Valid {
		return loops.StepFailed, nil, fmt.Errorf("eval block issue has no creator")
	}
	actorID := uuid.UUID(actor.Bytes)
	run, err := r.store.CreateRun(ctx, binding.WorkspaceID, actorID, binding.EvalID, actorType, EvalRunInput{WorkflowID: &workflowID, IssueID: &issueID, StartedAt: &now})
	if err != nil {
		if !blocking {
			r.warnAdvisory(ctx, binding, issueID, nil, RunStatusError)
			outcome, _ := json.Marshal(map[string]any{"status": "error", "phase": phase, "blocking": false, "warning": true, "error": err.Error()})
			return loops.StepCompleted, outcome, nil
		}
		return loops.StepFailed, nil, err
	}
	outcome, _ := json.Marshal(map[string]any{"eval_run_id": run.ID, "status": run.Status, "phase": phase, "blocking": blocking})
	if run.Status == RunStatusPassed {
		return loops.StepCompleted, outcome, nil
	}
	if !blocking {
		r.warnAdvisory(ctx, binding, issueID, &run.ID, run.Status)
		outcome, _ = json.Marshal(map[string]any{"eval_run_id": run.ID, "status": run.Status, "phase": phase, "blocking": false, "warning": true})
		return loops.StepCompleted, outcome, nil
	}
	return loops.StepFailed, outcome, nil
}

func (r *BlockRunner) warnAdvisory(ctx context.Context, binding Binding, issueID uuid.UUID, runID *uuid.UUID, status string) {
	if r.warner == nil {
		return
	}
	if err := r.warner.WarnBinding(ctx, binding, issueID, runID, status); err != nil {
		slog.Warn("eval block advisory warn failed", "workflow_id", binding.WorkflowID, "issue_id", issueID, "phase", binding.Phase, "error", err)
	}
}
