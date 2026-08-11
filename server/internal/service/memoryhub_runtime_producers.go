// Package service: production wiring for the review scheduler and the
// compensation sweeper. Owner: ALL-16.
//
// ReviewTaskEnqueuer commits the reviewer agent_task_queue row plus the
// execution ledger row in ONE transaction (V5-7.2 dispatching -> queued) and
// returns the new reviewer task id; the scheduler stores it via
// MarkReviewQueuedCAS.
//
// CompensationExecutor runs the idempotent remote side effect for a claimed
// compensation row. It is the production boundary for the §11 sweeper.
package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ReviewerAgentResolver loads the reviewer agent's runtime id so the created
// task can be claimed by the reviewer's runtime.
type ReviewerAgentResolver interface {
	GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error)
}

// DefaultReviewTaskEnqueuer is the production ReviewTaskEnqueuer. It resolves
// the reviewer's runtime, creates the reviewer task and the execution ledger
// in one transaction, and returns the task id. review_policy is 'none'
// (recursion guard); memory_policy is 'optional' (refs-only evidence).
type DefaultReviewTaskEnqueuer struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

// NewDefaultReviewTaskEnqueuer builds the production enqueuer.
func NewDefaultReviewTaskEnqueuer(q *db.Queries, tx TxStarter) *DefaultReviewTaskEnqueuer {
	return &DefaultReviewTaskEnqueuer{Queries: q, TxStarter: tx}
}

// Enqueue creates the reviewer task and ledger atomically and returns the
// task id. On any error no partial row survives (single transaction).
func (e *DefaultReviewTaskEnqueuer) Enqueue(ctx context.Context, rec db.ExecutionEvidenceRecord) (pgtype.UUID, error) {
	tx, err := e.TxStarter.Begin(ctx)
	if err != nil {
		return pgtype.UUID{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := e.Queries.WithTx(tx)

	reviewerID := rec.ReviewerAgentID
	agent, err := qtx.GetAgent(ctx, reviewerID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("reviewer agent lookup: %w", err)
	}

	task, err := qtx.CreateReviewerTask(ctx, db.CreateReviewerTaskParams{
		AgentID:             reviewerID,
		RuntimeID:           agent.RuntimeID,
		Priority:            0,
		TriggerSummary:      pgtype.Text{String: "independent review of execution " + uuidString(rec.ExecutionID), Valid: true},
		ReviewOfExecutionID: rec.ExecutionID,
	})
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("create reviewer task: %w", err)
	}

	workspaceID := uuidString(rec.WorkspaceID)
	_ = workspaceID
	_, err = qtx.InsertExecutionLedger(ctx, db.InsertExecutionLedgerParams{
		ExecutionID:    rec.ExecutionID,
		Attempt:        1,
		TaskID:         task.ID,
		TaskVersion:    1,
		WorkspaceID:    rec.WorkspaceID,
		ScopeKind:      "workspace",
		RunID:          "review-" + uuidString(rec.ExecutionID),
		AgentID:        reviewerID,
		RuntimeID:      agent.RuntimeID,
		Model:          "",
		Origin:         pgtype.Text{String: "memoryhub_review", Valid: true},
		IdempotencyKey: "review:" + uuidString(rec.ExecutionID),
		ReviewPolicy:   string(protocol.ReviewPolicyNone),
		ProjectID:      pgtype.UUID{},
		ReviewerAgentID: reviewerID,
	})
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("insert review ledger: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return pgtype.UUID{}, err
	}
	return task.ID, nil
}

var _ ReviewTaskEnqueuer = (*DefaultReviewTaskEnqueuer)(nil)

// CompensationOpExecutor is the production CompensationExecutor. It maps each
// frozen op to the idempotent RemoteClient call. Re-drive never duplicates a
// remote side effect because the remote methods are find-or-create and the
// compensation row already carries the remote_ref.
type CompensationOpExecutor struct {
	Remote RemoteClient
}

// NewCompensationOpExecutor builds the production executor.
func NewCompensationOpExecutor(remote RemoteClient) *CompensationOpExecutor {
	return &CompensationOpExecutor{Remote: remote}
}

// Execute runs the op for the claimed row. ErrCompensationUnconfigured is
// classified as transient so the row backs off instead of dead-lettering when
// the remote is temporarily unavailable.
func (x *CompensationOpExecutor) Execute(ctx context.Context, comp db.MemoryhubCompensation) error {
	if x.Remote == nil {
		return ErrCompensationTransient
	}
	key := uuidString(comp.WorkspaceID)
	switch CompensationOp(comp.Op) {
	case CompensationCreateRemote:
		_, err := x.Remote.FindOrCreateTeam(ctx, string(CompensationCreateRemote), key)
		return err
	case CompensationReuseRemote:
		_, err := x.Remote.FindRemote(ctx, string(CompensationReuseRemote), key)
		return err
	case CompensationRebindRemote:
		_, err := x.Remote.FindOrCreateAgent(ctx, string(CompensationRebindRemote), key)
		return err
	case CompensationDeleteRemote:
		return x.Remote.DeleteRemote(ctx, key)
	case CompensationPurgeMemory:
		// No remote delete endpoint; treat as satisfied once the local row is
		// marked compensated (the sweeper owns local cleanup).
		return nil
	default:
		return errors.New("memoryhub: unknown compensation op " + comp.Op)
	}
}

var _ CompensationExecutor = (*CompensationOpExecutor)(nil)

var _ = pgx.ErrNoRows
