package loops

// status_setter.go is the concrete implementation of gate_evaluator.go's
// StatusSetter: it moves an issue's status back to Build/Plan when a gate
// revises, so the board shows where the loop actually is instead of a status
// the loop has already moved past (FIR-2283 v2 slice 2d — Jesper's proposal:
// "the gate reacts to the change, and if it's not pass, the gate's action is
// to change the status").
//
// This reuses the exact DB call workflows.actionSetStatus already uses for
// the "done" transition (UpdateIssueStatus) — the only addition is looking
// the issue up first, because UpdateIssueStatus requires the workspace id as
// a tenant guard and the gate evaluator is only ever handed an issue id.

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// StatusQueries is the narrow slice of the upstream issue queries
// IssueStatusSetter needs.
type StatusQueries interface {
	GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error)
	UpdateIssueStatus(ctx context.Context, arg db.UpdateIssueStatusParams) (db.Issue, error)
}

// IssueStatusSetter implements StatusSetter over the upstream issue queries.
type IssueStatusSetter struct {
	queries StatusQueries
}

// NewIssueStatusSetter builds an IssueStatusSetter over the given queries.
func NewIssueStatusSetter(queries StatusQueries) *IssueStatusSetter {
	return &IssueStatusSetter{queries: queries}
}

// SetStatus loads the issue to resolve its workspace, then updates its
// status. Best-effort from the caller's point of view (gate_evaluator.go's
// revise() never lets a failure here block the actual revision dispatch).
func (s *IssueStatusSetter) SetStatus(ctx context.Context, issueID pgtype.UUID, status string) error {
	issue, err := s.queries.GetIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("status setter: load issue: %w", err)
	}
	if _, err := s.queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:          issueID,
		Status:      status,
		WorkspaceID: issue.WorkspaceID,
	}); err != nil {
		return fmt.Errorf("status setter: update status: %w", err)
	}
	return nil
}
