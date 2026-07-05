package dingtalk

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// InstallationParams is the input shape RegistrationService assembles
// after a successful device-flow scan-to-install. The credentials are
// supplied as plaintext — encryption happens inside
// InstallationService.Upsert via the supplied *secretbox.Box, so callers
// never see (and therefore cannot leak) the ciphertext that lands in
// the DB.
type InstallationParams struct {
	WorkspaceID     pgtype.UUID
	AgentID         pgtype.UUID
	ClientID        string
	ClientSecret    string // plaintext; encrypted at the service boundary
	InstallerUserID pgtype.UUID
}

// InstallationService creates, refreshes and revokes per-agent DingTalk
// installations. It owns the at-rest encryption of the client_secret so
// no caller can accidentally insert a row with plaintext credentials —
// the only path to writing a dingtalk channel_installation goes through
// here. Mirrors lark.InstallationService.
type InstallationService struct {
	queries *ChannelStore
	box     *secretbox.Box
}

// NewInstallationService binds the service to a queries handle and a
// secretbox keyed for at-rest encryption. The box MUST be non-nil; we
// refuse to fall back to plaintext storage even in test or dev
// configurations.
func NewInstallationService(queries *db.Queries, box *secretbox.Box) (*InstallationService, error) {
	if box == nil {
		return nil, errors.New("dingtalk: InstallationService requires a non-nil secretbox.Box")
	}
	return &InstallationService{queries: NewChannelStore(queries), box: box}, nil
}

// Upsert creates a new installation or refreshes an existing one in
// place (matching on the (workspace_id, agent_id, channel_type)
// UNIQUE). Re-install resets status to 'active'.
func (s *InstallationService) Upsert(ctx context.Context, p InstallationParams) (Installation, error) {
	if err := validateInstallationParams(p); err != nil {
		return Installation{}, err
	}
	sealed, err := s.box.Seal([]byte(p.ClientSecret))
	if err != nil {
		return Installation{}, fmt.Errorf("encrypt client_secret: %w", err)
	}
	return s.queries.UpsertDingTalkInstallation(ctx, UpsertInstallationParams{
		WorkspaceID:        p.WorkspaceID,
		AgentID:            p.AgentID,
		ClientID:           p.ClientID,
		AppSecretEncrypted: sealed,
		InstallerUserID:    p.InstallerUserID,
	})
}

// Revoke flips status to 'revoked'. The row is preserved (no DELETE) so
// audit history remains queryable; a subsequent re-install via Upsert
// flips status back to 'active' atomically.
func (s *InstallationService) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.queries.SetDingTalkInstallationStatus(ctx, id, InstallationRevoked)
}

// DecryptClientSecret returns the plaintext client_secret for the
// supplied installation row. Reserved for the future inbound transport
// (DingTalk Stream Mode) that must authenticate on behalf of an
// installation; the plaintext value must never round-trip through an
// HTTP response.
func (s *InstallationService) DecryptClientSecret(inst Installation) (string, error) {
	plain, err := s.box.Open(inst.AppSecretEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt client_secret: %w", err)
	}
	return string(plain), nil
}

// GetInWorkspace is the workspace-scoped lookup helper, so a forged
// installation_id from a different workspace returns NotFound instead
// of leaking existence.
func (s *InstallationService) GetInWorkspace(ctx context.Context, id, workspaceID pgtype.UUID) (Installation, error) {
	row, err := s.queries.GetDingTalkInstallationInWorkspace(ctx, id, workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Installation{}, ErrInstallationNotFound
		}
		return Installation{}, err
	}
	return row, nil
}

// ListByWorkspace returns every installation rooted at the workspace,
// active and revoked, oldest first.
func (s *InstallationService) ListByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]Installation, error) {
	return s.queries.ListDingTalkInstallationsByWorkspace(ctx, workspaceID)
}

// ErrInstallationNotFound surfaces "no row matches in this workspace" —
// used by the HTTP layer to return 404. Distinct from a plain
// pgx.ErrNoRows so handlers do not need to import pgx.
var ErrInstallationNotFound = errors.New("dingtalk installation not found")

func validateInstallationParams(p InstallationParams) error {
	switch {
	case !p.WorkspaceID.Valid:
		return errors.New("workspace_id is required")
	case !p.AgentID.Valid:
		return errors.New("agent_id is required")
	case !p.InstallerUserID.Valid:
		return errors.New("installer_user_id is required")
	case p.ClientID == "":
		return errors.New("client_id is required")
	case p.ClientSecret == "":
		return errors.New("client_secret is required")
	}
	return nil
}
