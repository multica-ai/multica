package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// ResolveSecretRef parses a secret_ref string and returns the plaintext value.
// Accepted format: "secret://workspace/<name>"
func (s *WorkspaceSecretService) ResolveSecretRef(ctx context.Context, workspaceID pgtype.UUID, secretRef string) (string, error) {
	secretRef = strings.TrimSpace(secretRef)
	if secretRef == "" {
		return "", ErrWorkspaceSecretNotFound
	}
	name, err := parseSecretRef(secretRef)
	if err != nil {
		return "", err
	}
	value, err := s.GetSecret(ctx, workspaceID, name)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrWorkspaceSecretNotFound, name)
	}
	return value, nil
}

func parseSecretRef(ref string) (string, error) {
	const prefix = "secret://workspace/"
	rest, ok := strings.CutPrefix(ref, prefix)
	if !ok || strings.TrimSpace(rest) == "" {
		return "", fmt.Errorf("invalid secret_ref format: expected secret://workspace/<name>")
	}
	rest = strings.TrimSpace(rest)
	if strings.Contains(rest, "/") || strings.Contains(rest, "\\") {
		return "", fmt.Errorf("invalid secret_ref: name must not contain path separators")
	}
	return rest, nil
}
