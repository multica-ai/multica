package octo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// InstallationParams carries the inputs to create or update an Octo bot
// installation. BotToken is the plaintext bf_* token; it is encrypted at rest
// via secretbox before storage and never persisted in the clear.
type InstallationParams struct {
	WorkspaceID     pgtype.UUID
	AgentID         pgtype.UUID
	BotToken        string
	RobotID         string
	BotName         string
	OwnerUID        string
	APIURL          string
	WSURL           string
	InstallerUserID pgtype.UUID
}

// InstallationService manages octo_installation rows, encrypting the bot token
// at rest with a secretbox.Box. It also satisfies the outbound TokenDecryptor
// interface (DecryptBotToken).
type InstallationService struct {
	queries *db.Queries
	box     *secretbox.Box
}

// NewInstallationService constructs the service. The box MUST be non-nil; the
// whole Octo integration is gated on a configured MULTICA_OCTO_SECRET_KEY, so a
// nil box is a programming error rather than a degraded mode.
func NewInstallationService(queries *db.Queries, box *secretbox.Box) (*InstallationService, error) {
	if box == nil {
		return nil, errors.New("octo: InstallationService requires a non-nil secretbox.Box")
	}
	return &InstallationService{queries: queries, box: box}, nil
}

// Upsert creates or refreshes the (workspace, agent) installation, sealing the
// bot token before write.
func (s *InstallationService) Upsert(ctx context.Context, p InstallationParams) (db.OctoInstallation, error) {
	if err := validateInstallationParams(p); err != nil {
		return db.OctoInstallation{}, err
	}
	sealed, err := s.box.Seal([]byte(p.BotToken))
	if err != nil {
		return db.OctoInstallation{}, fmt.Errorf("seal bot token: %w", err)
	}
	return s.queries.UpsertOctoInstallation(ctx, db.UpsertOctoInstallationParams{
		WorkspaceID:       p.WorkspaceID,
		AgentID:           p.AgentID,
		BotTokenEncrypted: sealed,
		RobotID:           p.RobotID,
		BotName:           p.BotName,
		OwnerUid:          p.OwnerUID,
		ApiUrl:            p.APIURL,
		WsUrl:             p.WSURL,
		InstallerUserID:   p.InstallerUserID,
	})
}

// Revoke marks an installation revoked; the hub tears down its WS on the next
// sweep.
func (s *InstallationService) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.queries.SetOctoInstallationStatus(ctx, db.SetOctoInstallationStatusParams{
		ID:     id,
		Status: string(InstallationRevoked),
	})
}

// DecryptBotToken returns the plaintext bot token for an installation. It
// satisfies the outbound TokenDecryptor interface.
func (s *InstallationService) DecryptBotToken(inst db.OctoInstallation) (string, error) {
	plain, err := s.box.Open(inst.BotTokenEncrypted)
	if err != nil {
		return "", fmt.Errorf("open bot token: %w", err)
	}
	return string(plain), nil
}

// GetInWorkspace loads a workspace-scoped installation (HTTP handler path).
func (s *InstallationService) GetInWorkspace(ctx context.Context, id, workspaceID pgtype.UUID) (db.OctoInstallation, error) {
	return s.queries.GetOctoInstallationInWorkspace(ctx, db.GetOctoInstallationInWorkspaceParams{
		ID:          id,
		WorkspaceID: workspaceID,
	})
}

// ListByWorkspace lists a workspace's installations (HTTP handler path).
func (s *InstallationService) ListByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]db.OctoInstallation, error) {
	return s.queries.ListOctoInstallationsByWorkspace(ctx, workspaceID)
}

func validateInstallationParams(p InstallationParams) error {
	switch {
	case !p.WorkspaceID.Valid:
		return errors.New("octo: installation requires workspace_id")
	case !p.AgentID.Valid:
		return errors.New("octo: installation requires agent_id")
	case p.BotToken == "":
		return errors.New("octo: installation requires a bot token")
	case p.RobotID == "":
		return errors.New("octo: installation requires robot_id")
	case p.APIURL == "":
		return errors.New("octo: installation requires api_url")
	case !p.InstallerUserID.Valid:
		return errors.New("octo: installation requires installer_user_id")
	}
	return nil
}
