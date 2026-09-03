package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (s *WorkflowService) ListLatestDefinitions(ctx context.Context, workspaceID pgtype.UUID) ([]db.WorkflowDefinition, error) {
	rows, err := s.Queries.ListLatestWorkflowDefinitions(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workflow definitions: %w", err)
	}
	return rows, nil
}

func (s *WorkflowService) GetDefinition(ctx context.Context, workspaceID, id pgtype.UUID) (db.WorkflowDefinition, error) {
	row, err := s.Queries.GetWorkflowDefinitionInWorkspace(ctx, db.GetWorkflowDefinitionInWorkspaceParams{ID: id, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.WorkflowDefinition{}, ErrWorkflowDefinitionNotFound
	}
	if err != nil {
		return db.WorkflowDefinition{}, fmt.Errorf("get workflow definition: %w", err)
	}
	return row, nil
}

func (s *WorkflowService) GetCurrentOrLatestRun(ctx context.Context, workspaceID, issueID pgtype.UUID) (db.WorkflowRun, error) {
	row, err := s.Queries.GetActiveWorkflowRunForIssue(ctx, db.GetActiveWorkflowRunForIssueParams{WorkspaceID: workspaceID, IssueID: issueID})
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.WorkflowRun{}, fmt.Errorf("get active workflow run: %w", err)
	}
	row, err = s.Queries.GetLatestWorkflowRunForIssue(ctx, db.GetLatestWorkflowRunForIssueParams{WorkspaceID: workspaceID, IssueID: issueID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.WorkflowRun{}, ErrWorkflowRunNotFound
	}
	if err != nil {
		return db.WorkflowRun{}, fmt.Errorf("get latest workflow run: %w", err)
	}
	return row, nil
}

func (s *WorkflowService) ListTransitions(ctx context.Context, workspaceID, runID pgtype.UUID) ([]db.WorkflowTransition, error) {
	rows, err := s.Queries.ListWorkflowTransitions(ctx, db.ListWorkflowTransitionsParams{WorkspaceID: workspaceID, WorkflowRunID: runID})
	if err != nil {
		return nil, fmt.Errorf("list workflow transitions: %w", err)
	}
	return rows, nil
}
