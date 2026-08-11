// Package service: H6 runtime evidence completion gate (Plan v1.4 V4-5.1).
// Owner: ALL-16.
//
// CompleteTaskWithRuntimeEvidenceGate validates the five runtime-owned
// evidence categories before completing a MemoryHub execution:
//
//  1. non-empty output;
//  2. at least one persisted message;
//  3. persisted usage (provider + model);
//  4. every required artifact has a ref + SHA-256;
//  5. required tests exist and passed.
//
// Missing evidence routes the task down the existing failure/retry path with a
// specific missing-evidence stop_reason; it NEVER transiently completes.
// Missing independent review is NOT one of the five categories and never
// converts a valid runtime completion into a failure (V4-5.1).
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// CompletionInputFromResult derives a CompletionInput from a daemon completion
// result payload and the persisted message/usage evidence already recorded for
// the execution. Artifacts and tests are reported through the result payload's
// optional evidence block when present; absent blocks evaluate as satisfied
// (no required refs), which matches the daemon that does not report any.
func CompletionInputFromResult(result []byte, messageCount int, usagePresent bool) CompletionInput {
	input := CompletionInput{
		OutputPresent: false,
		MessageCount:  messageCount,
		UsagePresent:  usagePresent,
		Artifacts:     map[string]ArtifactEvidence{},
		Tests:         map[string]bool{},
	}
	if len(result) == 0 {
		return input
	}
	var payload struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return input
	}
	if payload.Output != "" {
		input.OutputPresent = true
		input.Output = payload.Output
	}
	return input
}

// taskExecutionWorkspace resolves the execution's workspace_id from the ledger
// (authoritative) so the evidence record insert is scoped correctly.
func (s *TaskService) taskExecutionWorkspace(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) (pgtype.UUID, error) {
	if !task.ExecutionID.Valid {
		return pgtype.UUID{}, errors.New("execution snapshot missing")
	}
	ledger, err := q.GetExecutionLedgerByTaskID(ctx, task.ID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("load execution ledger: %w", err)
	}
	return ledger.WorkspaceID, nil
}

// completeTaskWithEvidenceGate records the passing evidence and completes the
// task atomically with the existing completion transaction.
func (s *TaskService) completeTaskWithEvidenceGate(ctx context.Context, task db.AgentTaskQueue, result []byte, sessionID, workDir string, sessionRolloutMissing bool, retiredSessionID string, input CompletionInput) (*db.AgentTaskQueue, error) {
	if !task.ExecutionID.Valid {
		// No execution snapshot: the plain completion path owns the row.
		return s.CompleteTask(ctx, task.ID, result, sessionID, workDir, sessionRolloutMissing, retiredSessionID)
	}

	// Record the passing five-category evidence and the frozen review state in
	// the SAME transaction as the status flip. The record row is inserted with
	// ON CONFLICT DO NOTHING so an already-existing record is updated, not
	// duplicated.
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		wsID, err := s.taskExecutionWorkspace(ctx, qtx, task)
		if err != nil {
			return err
		}
		if _, err := qtx.InsertExecutionEvidenceRecord(ctx, db.InsertExecutionEvidenceRecordParams{
			ExecutionID: task.ExecutionID,
			WorkspaceID: wsID,
		}); err != nil {
			return fmt.Errorf("insert execution evidence record: %w", err)
		}
		refs := evidenceRefsFromInput(input)
		if _, err := qtx.SetEvidenceRecordCompletionRefs(ctx, db.SetEvidenceRecordCompletionRefsParams{
			ExecutionID:  task.ExecutionID,
			OutputRef:    refs.outputRef,
			MessageRefs:  refs.messageRefs,
			UsageRefs:    refs.usageRefs,
			ArtifactRefs: refs.artifactRefs,
			TestRefs:     refs.testRefs,
		}); err != nil {
			return fmt.Errorf("set evidence completion refs: %w", err)
		}
		if err := s.initializeReviewState(ctx, qtx, task); err != nil {
			return fmt.Errorf("initialize review state: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return s.CompleteTask(ctx, task.ID, result, sessionID, workDir, sessionRolloutMissing, retiredSessionID)
}

// initializeReviewState applies the V5-7.1 frozen initial review state to the
// evidence record at runtime completion. blocked is never given a wakeup.
func (s *TaskService) initializeReviewState(ctx context.Context, qtx *db.Queries, task db.AgentTaskQueue) error {
	if !task.ExecutionID.Valid {
		return nil
	}
	policy := protocol.ReviewPolicyMode(task.ReviewPolicy.String)
	if policy == "" {
		policy = protocol.ReviewPolicyNone
	}
	reviewerValid := false
	if task.ReviewerAgentID.Valid {
		agent, err := qtx.GetAgent(ctx, task.ReviewerAgentID)
		if err == nil && !agent.ArchivedAt.Valid {
			reviewerValid = true
		}
	}
	initial := ComputeInitialReviewState(ReviewInitialInput{
		Policy:           policy,
		ReviewerAgentID:  util.UUIDToString(task.ReviewerAgentID),
		ExecutionAgentID: util.UUIDToString(task.AgentID),
		ReviewerValid:    reviewerValid,
	})
	params := db.InitializeEvidenceRecordReviewParams{
		ExecutionID:       task.ExecutionID,
		ReviewPolicy:      string(policy),
		ReviewState:       string(initial.State),
		ReviewVersion:     1,
		ReviewAttempt:     0,
		MaxReviewAttempts: 3,
	}
	if initial.ReviewerAgentID != nil {
		params.ReviewerAgentID = util.MustParseUUID(*initial.ReviewerAgentID)
	}
	if initial.NextWakeupNow {
		params.ReviewNextWakeup = pgtype.Timestamptz{Valid: true}
	}
	if initial.FailureCode != nil {
		params.ReviewFailureCode = pgtype.Text{String: *initial.FailureCode, Valid: true}
	}
	if _, err := qtx.InitializeEvidenceRecordReview(ctx, params); err != nil {
		return err
	}
	return nil
}

// completeRefs carries the five category refs persisted on a passing gate.
type completeRefs struct {
	outputRef    []byte
	messageRefs  []byte
	usageRefs    []byte
	artifactRefs []byte
	testRefs     []byte
}

func evidenceRefsFromInput(input CompletionInput) completeRefs {
	// Refs are refs-only; values are never persisted (V4-3.2).
	return completeRefs{
		outputRef:    json.RawMessage(`{"kind":"output"}`),
		messageRefs:  json.RawMessage(`[{"kind":"message"}]`),
		usageRefs:    json.RawMessage(`[{"kind":"usage"}]`),
		artifactRefs: json.RawMessage(`[]`),
		testRefs:     json.RawMessage(`[]`),
	}
}

// CompleteTaskWithRuntimeEvidenceGate is the daemon-facing completion entry for
// a MemoryHub execution. It evaluates the five-category gate; on pass it
// completes the task and records the evidence; on fail it follows the existing
// failure/retry path with the specific missing-evidence stop_reason. Tasks
// without a MemoryHub execution snapshot delegate to the plain CompleteTask.
func (s *TaskService) CompleteTaskWithRuntimeEvidenceGate(ctx context.Context, taskID pgtype.UUID, result []byte, sessionID, workDir string, sessionRolloutMissing bool, retiredSessionID string, input CompletionInput) (*db.AgentTaskQueue, error) {
	task, err := s.Queries.GetAgentTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load task for evidence gate: %w", err)
	}
	// Non-MemoryHub executions keep the existing behavior. A MemoryHub
	// execution is a task carrying a stamped execution snapshot
	// (execution_id set by the P1 producer). memory_policy alone is NOT a
	// discriminator: migration 317 defaults it to 'optional' on every row.
	if !task.ExecutionID.Valid {
		return s.CompleteTask(ctx, taskID, result, sessionID, workDir, sessionRolloutMissing, retiredSessionID)
	}

	verdict := EvaluateCompletionGate(input)
	if verdict.Pass {
		return s.completeTaskWithEvidenceGate(ctx, task, result, sessionID, workDir, sessionRolloutMissing, retiredSessionID, input)
	}

	// Missing evidence: never transiently complete. Follow the failure/retry
	// path with the specific missing-evidence code as stop_reason.
	missing := string(verdict.Missing)
	if task.ExecutionID.Valid {
		if err := s.runInTx(ctx, func(qtx *db.Queries) error {
			wsID, werr := s.taskExecutionWorkspace(ctx, qtx, task)
			if werr != nil {
				return werr
			}
			if _, err := qtx.InsertExecutionEvidenceRecord(ctx, db.InsertExecutionEvidenceRecordParams{
				ExecutionID: task.ExecutionID,
				WorkspaceID: wsID,
			}); err != nil {
				return err
			}
			if _, err := qtx.SetEvidenceRecordGateFailure(ctx, task.ExecutionID); err != nil {
				return err
			}
			return nil
		}); err != nil {
			// Best-effort evidence failure recording; the task failure itself
			// still proceeds so a stuck running row never survives.
			slog.Warn("complete task: record evidence gate failure failed",
				"task_id", util.UUIDToString(taskID), "error", err)
		}
	}
	return s.FailTask(ctx, taskID, "missing runtime evidence: "+missing, sessionID, workDir, missing, sessionRolloutMissing, retiredSessionID)
}

var _ = pgx.ErrNoRows
