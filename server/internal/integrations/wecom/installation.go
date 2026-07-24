package wecom

// installation.go — the write surface for wecom channel_installation rows.
// It centralises secretbox encryption of the smart-bot secret so no caller
// ever handles plaintext beyond this file's boundary, and it is the ONLY
// path to a wecom row in channel_installation — an admin CLI or an HTTP
// install endpoint both go through Upsert.

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// InstallationParams is the plaintext-bearing input to InstallationService.
// The caller supplies the raw (BotID, Secret) pair from the WeChat Work
// admin console; the service seals Secret before it touches the DB.
type InstallationParams struct {
	WorkspaceID     pgtype.UUID
	AgentID         pgtype.UUID
	InstallerUserID pgtype.UUID

	// BotID is the smart-bot identifier shown on the WeChat Work admin
	// console. Stable per-bot; used as both auth identity in the subscribe
	// frame and the routing key persisted at config->>'app_id'.
	BotID string

	// Secret is the plaintext long-connection secret shown once at bot
	// creation on the admin console. Sealed at the service boundary.
	Secret string
}

// InstallationService creates, refreshes and revokes wecom smart-bot
// installations through the shared channel_installation table. It requires
// a non-nil *secretbox.Box so a caller cannot accidentally fall back to
// plaintext storage — the same invariant lark.InstallationService enforces.
type InstallationService struct {
	store *Store
	box   *secretbox.Box
}

// NewInstallationService binds the service to a queries handle and a
// secretbox keyed for at-rest encryption. Returns an error when the box
// is nil; callers should surface it (do not fall back to plaintext).
func NewInstallationService(queries *db.Queries, box *secretbox.Box) (*InstallationService, error) {
	if box == nil {
		return nil, errors.New("wecom: InstallationService requires a non-nil secretbox.Box")
	}
	return &InstallationService{store: NewStore(queries), box: box}, nil
}

// Upsert creates or refreshes an installation row. The conflict key on
// channel_installation is (workspace_id, agent_id, channel_type), so
// re-running Upsert against an existing (workspace, agent, wecom) triple
// rotates every field on the row and flips status back to 'active'. The
// returned Installation reflects the post-write DB state.
func (s *InstallationService) Upsert(ctx context.Context, p InstallationParams) (Installation, error) {
	if err := validateInstallationParams(p); err != nil {
		return Installation{}, err
	}
	sealed, err := s.box.Seal([]byte(p.Secret))
	if err != nil {
		return Installation{}, fmt.Errorf("wecom: encrypt secret: %w", err)
	}
	cfg, err := encodeInstallConfig(Installation{
		BotID:           p.BotID,
		SecretEncrypted: sealed,
	})
	if err != nil {
		return Installation{}, err
	}

	row, err := s.store.Queries.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{
		WorkspaceID:     p.WorkspaceID,
		AgentID:         p.AgentID,
		ChannelType:     channelTypeWecom,
		Config:          cfg,
		InstallerUserID: p.InstallerUserID,
	})
	if err != nil {
		return Installation{}, err
	}
	return installationFromRow(row)
}

// Revoke flips status to 'revoked' — the row is preserved so audit trails
// remain queryable, and a subsequent Upsert flips it back to 'active'
// atomically. A revoked row is skipped by the router's installation resolver
// (Active=false → invalid_event drop with audit).
func (s *InstallationService) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.store.Queries.SetChannelInstallationStatus(ctx, db.SetChannelInstallationStatusParams{
		ID:     id,
		Status: string(InstallationRevoked),
	})
}

// ErrInstallationNotFound is returned by GetInWorkspace when either no row
// exists at the given (id, workspace) or the row belongs to a different
// channel_type. It is distinct from a plain pgx.ErrNoRows so HTTP handlers
// can map it to 404 without importing pgx.
var ErrInstallationNotFound = errors.New("wecom: installation not found")

// ListByWorkspace returns every wecom installation for the given workspace
// in creation order. Used by the Settings and Agent-Integrations tabs to
// render "connected bots" lists; revoked rows are included so operators can
// see history (the UI filters on Status).
func (s *InstallationService) ListByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]Installation, error) {
	rows, err := s.store.Queries.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: workspaceID,
		ChannelType: channelTypeWecom,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Installation, 0, len(rows))
	for _, row := range rows {
		inst, err := installationFromRow(row)
		if err != nil {
			return nil, fmt.Errorf("wecom: decode installation %s: %w", row.ID.String(), err)
		}
		out = append(out, inst)
	}
	return out, nil
}

// GetInWorkspace loads one installation scoped to (id, workspace_id) so a
// forged UUID from another workspace returns not-found instead of leaking
// existence. Returns ErrInstallationNotFound on either missing row or a
// row that exists but belongs to another channel_type.
func (s *InstallationService) GetInWorkspace(ctx context.Context, id, workspaceID pgtype.UUID) (Installation, error) {
	row, err := s.store.Queries.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID:          id,
		WorkspaceID: workspaceID,
		ChannelType: channelTypeWecom,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Installation{}, ErrInstallationNotFound
		}
		return Installation{}, err
	}
	return installationFromRow(row)
}

// validateInstallationParams is a lightweight pre-write check for
// required fields. It does NOT verify anything against WeChat.
func validateInstallationParams(p InstallationParams) error {
	if !p.WorkspaceID.Valid {
		return errors.New("wecom: workspace_id is required")
	}
	if !p.AgentID.Valid {
		return errors.New("wecom: agent_id is required")
	}
	if !p.InstallerUserID.Valid {
		return errors.New("wecom: installer_user_id is required")
	}
	if p.BotID == "" {
		return errors.New("wecom: bot_id is required")
	}
	if p.Secret == "" {
		return errors.New("wecom: secret is required")
	}
	return nil
}
