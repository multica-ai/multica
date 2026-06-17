package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrWorkspaceSecretNotFound = errors.New("workspace secret not found")

type WorkspaceSecretService struct {
	Queries *db.Queries
	Box     *secretbox.Box
}

func NewWorkspaceSecretService(q *db.Queries, box *secretbox.Box) *WorkspaceSecretService {
	return &WorkspaceSecretService{Queries: q, Box: box}
}

// UpsertSecret encrypts and stores a secret. Caller must verify the actor is
// a workspace owner or admin before calling.
func (s *WorkspaceSecretService) UpsertSecret(ctx context.Context, workspaceID pgtype.UUID, name string, value string, createdBy pgtype.UUID) error {
	sealed, err := s.Box.Seal([]byte(value))
	if err != nil {
		return fmt.Errorf("encrypt secret: %w", err)
	}
	return s.Queries.UpsertWorkspaceSecret(ctx, db.UpsertWorkspaceSecretParams{
		WorkspaceID:    workspaceID,
		Name:           name,
		EncryptedValue: sealed,
		CreatedBy:      createdBy,
	})
}

// GetSecret decrypts and returns a secret value. Caller must verify the actor
// is authorized (workspace owner/admin) before calling.
func (s *WorkspaceSecretService) GetSecret(ctx context.Context, workspaceID pgtype.UUID, name string) (string, error) {
	row, err := s.Queries.GetWorkspaceSecret(ctx, db.GetWorkspaceSecretParams{WorkspaceID: workspaceID, Name: name})
	if err != nil {
		return "", fmt.Errorf("get workspace secret: %w", err)
	}
	plain, err := s.Box.Open(row.EncryptedValue)
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plain), nil
}

// DeleteSecret removes a secret. Caller must verify the actor is a workspace
// owner or admin before calling.
func (s *WorkspaceSecretService) DeleteSecret(ctx context.Context, workspaceID pgtype.UUID, name string) error {
	return s.Queries.DeleteWorkspaceSecret(ctx, db.DeleteWorkspaceSecretParams{WorkspaceID: workspaceID, Name: name})
}

// ListSecretNames returns secret names with metadata (no values).
func (s *WorkspaceSecretService) ListSecretNames(ctx context.Context, workspaceID pgtype.UUID) ([]db.ListWorkspaceSecretNamesRow, error) {
	return s.Queries.ListWorkspaceSecretNames(ctx, workspaceID)
}
