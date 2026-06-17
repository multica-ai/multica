package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrCuratorLocalRuntimeUnavailable = errors.New("no online local runtime available for this workspace")
var ErrCuratorSecretNotFound = errors.New("secret_ref target secret not found")
var ErrCuratorDraftDispatched = errors.New("knowledge curator draft dispatched to local runtime")
var ErrCuratorDraftTaskNotFound = errors.New("curator draft task not found")
var ErrCuratorLocalSummarizeUnavailable = errors.New("summarize source is not available in local runtime mode")
var ErrCuratorLocalEmbeddingUnavailable = errors.New("build embedding is not available in local runtime mode")

// CuratorDraftTaskService manages the lifecycle of curator draft tasks
// dispatched to local daemon runtimes.
type CuratorDraftTaskService struct {
	Queries *db.Queries
	Curator *KnowledgeCuratorService
}

func NewCuratorDraftTaskService(q *db.Queries, curator *KnowledgeCuratorService) *CuratorDraftTaskService {
	return &CuratorDraftTaskService{Queries: q, Curator: curator}
}

// CuratorDraftTaskInput is the JSON-serialized payload stored in input_data.
type CuratorDraftTaskInput struct {
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	EmbeddingModel string `json:"embedding_model"`
	Provider       string `json:"provider"`

	// Serialized CuratorDraftInput
	DraftInput CuratorDraftInput `json:"draft_input"`

	// For candidate and governance drafts
	CandidateID  pgtype.UUID `json:"candidate_id,omitempty"`
	FindingID    pgtype.UUID `json:"finding_id,omitempty"`
	Regenerate   bool        `json:"regenerate,omitempty"`
}

// EnqueueDraftTask creates a curator draft task and returns the task ID.
func (s *CuratorDraftTaskService) EnqueueDraftTask(ctx context.Context, workspaceID, runtimeID pgtype.UUID, draftKind string, input CuratorDraftTaskInput, createdBy pgtype.UUID) (pgtype.UUID, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("marshal draft task input: %w", err)
	}
	task, err := s.Queries.CreateCuratorDraftTask(ctx, db.CreateCuratorDraftTaskParams{
		WorkspaceID: workspaceID,
		RuntimeID:   runtimeID,
		DraftKind:   draftKind,
		InputData:   raw,
		CreatedBy:   createdBy,
	})
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("create curator draft task: %w", err)
	}
	return task.ID, nil
}

// ClaimNextDraftTask claims the next queued curator draft task for a runtime.
func (s *CuratorDraftTaskService) ClaimNextDraftTask(ctx context.Context, runtimeID, workspaceID pgtype.UUID) (db.CuratorDraftTask, error) {
	return s.Queries.ClaimNextCuratorDraftTask(ctx, db.ClaimNextCuratorDraftTaskParams{
		RuntimeID:   runtimeID,
		WorkspaceID: workspaceID,
	})
}

// CompleteDraftTask validates the draft, creates a knowledge item, and marks
// the task as completed. Returns the created knowledge detail.
func (s *CuratorDraftTaskService) CompleteDraftTask(ctx context.Context, taskID, runtimeID, workspaceID pgtype.UUID, draft CuratorDraft) (KnowledgeDetail, error) {
	task, err := s.Queries.GetCuratorDraftTask(ctx, db.GetCuratorDraftTaskParams{
		ID:          taskID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return KnowledgeDetail{}, fmt.Errorf("get curator draft task: %w", err)
	}

	// Verify the task belongs to this specific runtime.
	if task.RuntimeID != runtimeID {
		return KnowledgeDetail{}, ErrCuratorDraftTaskNotFound
	}

	if err := validateCuratorDraft(draft); err != nil {
		return KnowledgeDetail{}, err
	}

	var input CuratorDraftTaskInput
	if err := json.Unmarshal(task.InputData, &input); err != nil {
		return KnowledgeDetail{}, fmt.Errorf("unmarshal draft task input: %w", err)
	}

	detail, err := s.Curator.createDraft(ctx, task.WorkspaceID, task.CreatedBy, input.DraftInput, draft)
	if err != nil {
		return KnowledgeDetail{}, err
	}

	// Update candidate or governance finding state based on draft kind.
	switch task.DraftKind {
	case "candidate":
		if input.CandidateID.Valid {
			candidate, err := s.Queries.GetKnowledgeCandidate(ctx, db.GetKnowledgeCandidateParams{
				ID:          input.CandidateID,
				WorkspaceID: task.WorkspaceID,
			})
			if err == nil {
				_, _ = s.Curator.markCandidateDraftSucceeded(ctx, candidate, detail.Item.ID, input.DraftInput.SourceSummary)
			}
		}
	case "governance_finding":
		if input.FindingID.Valid {
			_, err := s.Queries.UpdateKnowledgeGovernanceFindingStatus(ctx, db.UpdateKnowledgeGovernanceFindingStatusParams{
				ID:                   input.FindingID,
				WorkspaceID:          task.WorkspaceID,
				Status:               "drafted",
				DraftKnowledgeItemID: detail.Item.ID,
				ActorID:              task.CreatedBy,
			})
			_ = err
		}
	}

	resultJSON, _ := json.Marshal(draft)
	if _, err := s.Queries.CompleteCuratorDraftTask(ctx, db.CompleteCuratorDraftTaskParams{
		ID:          taskID,
		RuntimeID:   runtimeID,
		WorkspaceID: task.WorkspaceID,
		Result:      resultJSON,
	}); err != nil {
		return KnowledgeDetail{}, fmt.Errorf("complete curator draft task: %w", err)
	}

	return detail, nil
}

// FailDraftTask marks a curator draft task as failed. If the task was for a
// candidate, the candidate metadata is updated with the error.
func (s *CuratorDraftTaskService) FailDraftTask(ctx context.Context, taskID, runtimeID, workspaceID pgtype.UUID, errMsg string) error {
	task, err := s.Queries.GetCuratorDraftTask(ctx, db.GetCuratorDraftTaskParams{
		ID:          taskID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return fmt.Errorf("get curator draft task: %w", err)
	}

	// Verify the task belongs to this specific runtime.
	if task.RuntimeID != runtimeID {
		return ErrCuratorDraftTaskNotFound
	}

	// Update candidate metadata if applicable.
	if task.DraftKind == "candidate" {
		var input CuratorDraftTaskInput
		if err := json.Unmarshal(task.InputData, &input); err == nil && input.CandidateID.Valid {
			candidate, err := s.Queries.GetKnowledgeCandidate(ctx, db.GetKnowledgeCandidateParams{
				ID:          input.CandidateID,
				WorkspaceID: task.WorkspaceID,
			})
			if err == nil {
				_ = s.Curator.markCandidateDraftFailed(ctx, candidate, errors.New(errMsg))
			}
		}
	}

	if _, err := s.Queries.FailCuratorDraftTask(ctx, db.FailCuratorDraftTaskParams{
		ID:          taskID,
		RuntimeID:   runtimeID,
		WorkspaceID: task.WorkspaceID,
		Error:       pgtype.Text{String: errMsg, Valid: true},
	}); err != nil {
		return fmt.Errorf("fail curator draft task: %w", err)
	}
	return nil
}

// GetCuratorDraftTask retrieves a single curator draft task.
func (s *CuratorDraftTaskService) GetCuratorDraftTask(ctx context.Context, taskID, workspaceID pgtype.UUID) (db.CuratorDraftTask, error) {
	return s.Queries.GetCuratorDraftTask(ctx, db.GetCuratorDraftTaskParams{
		ID:          taskID,
		WorkspaceID: workspaceID,
	})
}
