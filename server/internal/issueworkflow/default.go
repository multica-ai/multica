package issueworkflow

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DefaultStore is what storing the Default workflow needs.
type DefaultStore interface {
	issuestatus.Querier
	GetDefaultIssueWorkflow(ctx context.Context, workspaceID pgtype.UUID) (db.IssueWorkflow, error)
	ListNamedIssueWorkflowSteps(ctx context.Context, workspaceID pgtype.UUID) ([][]byte, error)
	CreateDefaultIssueWorkflow(ctx context.Context, arg db.CreateDefaultIssueWorkflowParams) ([]db.IssueWorkflow, error)
}

// EnsureDefault returns the workspace's stored Default workflow, creating it
// first when needed. The new row lists every active status no other workflow
// lists, so a workspace that already uses custom statuses keeps its columns,
// while statuses added for one workflow stay out of Default.
func EnsureDefault(ctx context.Context, q DefaultStore, workspaceID pgtype.UUID) (db.IssueWorkflow, error) {
	row, err := q.GetDefaultIssueWorkflow(ctx, workspaceID)
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.IssueWorkflow{}, err
	}
	keys, err := DefaultSnapshotKeys(ctx, q, workspaceID)
	if err != nil {
		return db.IssueWorkflow{}, err
	}
	raw, err := EncodeKeys(keys)
	if err != nil {
		return db.IssueWorkflow{}, err
	}
	created, err := q.CreateDefaultIssueWorkflow(ctx, db.CreateDefaultIssueWorkflowParams{WorkspaceID: workspaceID, Steps: raw})
	if err != nil {
		return db.IssueWorkflow{}, err
	}
	if len(created) > 0 {
		return created[0], nil
	}
	// Another request stored it first.
	return q.GetDefaultIssueWorkflow(ctx, workspaceID)
}

// DefaultSnapshotKeys lists what a new Default row holds: the built-in
// statuses, backlog and blocked included, plus every active custom status no
// other workflow lists, in catalog order.
func DefaultSnapshotKeys(ctx context.Context, q DefaultStore, workspaceID pgtype.UUID) ([]string, error) {
	active, err := issuestatus.ActiveKeys(ctx, q, workspaceID)
	if err != nil {
		return nil, err
	}
	named, err := q.ListNamedIssueWorkflowSteps(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	claimed := map[string]bool{}
	for _, raw := range named {
		keys, err := DecodeKeys(raw)
		if err != nil {
			continue
		}
		for _, key := range keys {
			claimed[key] = true
		}
	}
	middle := make([]string, 0, len(active))
	for _, key := range active {
		if claimed[key] && !issuestatus.IsBuiltIn(key) {
			continue
		}
		middle = append(middle, key)
	}
	return ArrangeKeys(middle), nil
}
