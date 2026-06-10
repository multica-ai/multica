package wecom

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrInstallationNotFound = errors.New("wecom: installation not found")

type InstallationParams struct {
	WorkspaceID       pgtype.UUID
	AgentID           pgtype.UUID
	BotID             string
	BotSecret         string
	CorpID            string
	CorpSecret        string
	SelfBuildAgentID  string
	InstallerUserID   pgtype.UUID
}

type InstallationService struct {
	queries *db.Queries
	box     *secretbox.Box
}

func NewInstallationService(queries *db.Queries, box *secretbox.Box) (*InstallationService, error) {
	if box == nil {
		return nil, errors.New("wecom: InstallationService requires a non-nil secretbox.Box")
	}
	return &InstallationService{queries: queries, box: box}, nil
}

func (s *InstallationService) Upsert(ctx context.Context, p InstallationParams) (db.WecomInstallation, error) {
	if err := validateInstallationParams(p); err != nil {
		return db.WecomInstallation{}, err
	}
	botSealed, err := s.box.Seal([]byte(p.BotSecret))
	if err != nil {
		return db.WecomInstallation{}, fmt.Errorf("encrypt bot secret: %w", err)
	}
	corpSealed, err := s.box.Seal([]byte(p.CorpSecret))
	if err != nil {
		return db.WecomInstallation{}, fmt.Errorf("encrypt corp secret: %w", err)
	}
	return s.queries.UpsertWecomInstallation(ctx, db.UpsertWecomInstallationParams{
		WorkspaceID:         p.WorkspaceID,
		AgentID:             p.AgentID,
		BotID:               p.BotID,
		BotSecretEncrypted:  botSealed,
		CorpID:              p.CorpID,
		CorpSecretEncrypted: corpSealed,
		SelfBuildAgentID:    textOrNull(p.SelfBuildAgentID),
		InstallerUserID:     p.InstallerUserID,
	})
}

func (s *InstallationService) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.queries.SetWecomInstallationStatus(ctx, db.SetWecomInstallationStatusParams{
		ID:     id,
		Status: string(InstallationRevoked),
	})
}

func (s *InstallationService) ListByWorkspace(ctx context.Context, wsID pgtype.UUID) ([]db.WecomInstallation, error) {
	return s.queries.ListWecomInstallationsByWorkspace(ctx, wsID)
}

func (s *InstallationService) GetInWorkspace(ctx context.Context, id, wsID pgtype.UUID) (db.WecomInstallation, error) {
	row, err := s.queries.GetWecomInstallationInWorkspace(ctx, db.GetWecomInstallationInWorkspaceParams{
		ID: id, WorkspaceID: wsID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.WecomInstallation{}, ErrInstallationNotFound
		}
		return db.WecomInstallation{}, err
	}
	return row, nil
}

func (s *InstallationService) DecryptBotSecret(inst db.WecomInstallation) (string, error) {
	plain, err := s.box.Open(inst.BotSecretEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt bot secret: %w", err)
	}
	return string(plain), nil
}

func (s *InstallationService) DecryptCorpSecret(inst db.WecomInstallation) (string, error) {
	plain, err := s.box.Open(inst.CorpSecretEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt corp secret: %w", err)
	}
	return string(plain), nil
}

func validateInstallationParams(p InstallationParams) error {
	if !p.WorkspaceID.Valid || !p.AgentID.Valid || !p.InstallerUserID.Valid {
		return errors.New("wecom: workspace_id, agent_id, and installer_user_id are required")
	}
	if p.BotID == "" || p.BotSecret == "" || p.CorpID == "" || p.CorpSecret == "" {
		return errors.New("wecom: bot_id, bot_secret, corp_id, and corp_secret are required")
	}
	return nil
}

func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}
