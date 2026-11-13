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

// DefaultUpdater is what adding a status to a stored Default needs.
type DefaultUpdater interface {
	GetDefaultIssueWorkflow(ctx context.Context, workspaceID pgtype.UUID) (db.IssueWorkflow, error)
	UpdateIssueWorkflow(ctx context.Context, arg db.UpdateIssueWorkflowParams) (db.IssueWorkflow, error)
}

// AddToDefault lists a status created outside any workflow in the stored
// Default, the way the first snapshot takes every status no workflow lists. A
// workspace that has not stored Default yet picks it up in that snapshot.
func AddToDefault(ctx context.Context, q DefaultUpdater, workspaceID pgtype.UUID, key string) error {
	row, err := q.GetDefaultIssueWorkflow(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	steps, err := DecodeSteps(row.Steps)
	if err != nil {
		return err
	}
	for _, s := range steps {
		if s.StatusKey == key {
			return nil
		}
	}
	raw, err := EncodeSteps(Arrange(append(steps, Step{StatusKey: key, Handler: Handler{Type: HandlerNone}})))
	if err != nil {
		return err
	}
	_, err = q.UpdateIssueWorkflow(ctx, db.UpdateIssueWorkflowParams{
		ID:               row.ID,
		WorkspaceID:      workspaceID,
		Name:             row.Name,
		Description:      row.Description,
		InitialStatusKey: row.InitialStatusKey,
		Steps:            raw,
	})
	return err
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
