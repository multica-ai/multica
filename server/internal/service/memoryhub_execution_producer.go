// Package service: execution ledger producers (Plan v1.4 §6 P1-P8).
// Owner: ALL-16.
//
// Queue rows and execution_ledger rows are written in ONE transaction
// (TaskService.runInTx). This file owns the enqueue-time producers; the claim
// path transitions the ledger with the same transactional discipline.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// executionEnqueueInput carries the frozen execution snapshot fields an enqueue
// producer stamps on the queue row and the ledger row. Prefix is the
// idempotency-key prefix ("enqueue", "quick_create", "retry", "rerun",
// "delegation", "handoff", "merge"); the frozen key is
// sha256("<prefix>|"+task_id).
type executionEnqueueInput struct {
	WorkspaceID     pgtype.UUID
	ProjectID       pgtype.UUID
	IssueID         pgtype.UUID
	AgentID         pgtype.UUID
	RuntimeID       pgtype.UUID
	Model           string
	ReviewPolicy    string
	ReviewerAgentID pgtype.UUID
	RetryOf         pgtype.UUID
	RerunOf         pgtype.UUID
	DelegatedFrom   pgtype.UUID
	HandoffOf       pgtype.UUID
	Prefix          string
}

func executionIdempotencyKey(prefix string, taskID pgtype.UUID) string {
	sum := sha256.Sum256([]byte(prefix + "|" + util.UUIDToString(taskID)))
	return hex.EncodeToString(sum[:])
}

func pgUUIDFromString(s string) pgtype.UUID {
	u, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: u, Valid: true}
}

// enqueueTaskWithExecutionLedger (P1) wraps CreateAgentTask + InsertExecutionLedger
// in runInTx with key sha256("enqueue|"+task_id). The queue row is stamped with
// the frozen execution_id/memoryhub_run_id in the same transaction.
func (s *TaskService) enqueueTaskWithExecutionLedger(ctx context.Context, create func(q *db.Queries) (db.AgentTaskQueue, error), in executionEnqueueInput) (db.AgentTaskQueue, error) {
	return s.enqueueWithLedger(ctx, create, in, 1)
}

// createRetryTaskWithLedger (P4) wraps CreateAgentTask + InsertExecutionLedger
// with retry lineage (attempt+1, retry_of).
func (s *TaskService) createRetryTaskWithLedger(ctx context.Context, create func(q *db.Queries) (db.AgentTaskQueue, error), in executionEnqueueInput, attempt int32) (db.AgentTaskQueue, error) {
	return s.enqueueWithLedger(ctx, create, in, attempt)
}

// rerunIssueWithLedger (P5) wraps CreateAgentTask + InsertExecutionLedger with
// rerun lineage (attempt=1, rerun_of).
func (s *TaskService) rerunIssueWithLedger(ctx context.Context, create func(q *db.Queries) (db.AgentTaskQueue, error), in executionEnqueueInput) (db.AgentTaskQueue, error) {
	return s.enqueueWithLedger(ctx, create, in, 1)
}

// enqueueWithLedger is the single transaction that writes the queue row (via
// the caller's create closure) AND the execution ledger row. Both commit
// together or neither does.
func (s *TaskService) enqueueWithLedger(ctx context.Context, create func(q *db.Queries) (db.AgentTaskQueue, error), in executionEnqueueInput, attempt int32) (db.AgentTaskQueue, error) {
	executionID := uuid.New().String()
	runID := uuid.New().String()
	var task db.AgentTaskQueue
	err := s.runInTx(ctx, func(qtx *db.Queries) error {
		t, err := create(qtx)
		if err != nil {
			return err
		}
		task = t
		scopeKind := "workspace"
		if in.ProjectID.Valid {
			scopeKind = "project"
		}
		if _, err := qtx.StampTaskExecutionIdentity(ctx, db.StampTaskExecutionIdentityParams{
			ID:              t.ID,
			ExecutionID:     pgUUIDFromString(executionID),
			MemoryhubRunID:  pgtype.Text{String: runID, Valid: true},
			ReviewPolicy:    pgtype.Text{String: in.ReviewPolicy, Valid: in.ReviewPolicy != ""},
			ReviewerAgentID: in.ReviewerAgentID,
		}); err != nil {
			return fmt.Errorf("stamp execution identity: %w", err)
		}
		_, err = qtx.InsertExecutionLedger(ctx, db.InsertExecutionLedgerParams{
			ExecutionID:     pgUUIDFromString(executionID),
			Attempt:         attempt,
			TaskID:          t.ID,
			TaskVersion:     1,
			WorkspaceID:     in.WorkspaceID,
			ProjectID:       in.ProjectID,
			ScopeKind:       scopeKind,
			IssueID:         in.IssueID,
			RunID:           runID,
			AgentID:         in.AgentID,
			RuntimeID:       in.RuntimeID,
			Model:           in.Model,
			Origin:          pgtype.Text{String: in.Prefix, Valid: in.Prefix != ""},
			IdempotencyKey:  executionIdempotencyKey(in.Prefix, t.ID),
			ReviewPolicy:    in.ReviewPolicy,
			ReviewerAgentID: in.ReviewerAgentID,
			RetryOf:         in.RetryOf,
			RerunOf:         in.RerunOf,
			DelegatedFrom:   in.DelegatedFrom,
			HandoffOf:       in.HandoffOf,
		})
		if err != nil {
			return fmt.Errorf("insert execution ledger: %w", err)
		}
		return nil
	})
	if err != nil {
		return db.AgentTaskQueue{}, err
	}
	return task, nil
}
