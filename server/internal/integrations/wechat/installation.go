package wechat

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// InstallationService handles encrypted storage and retrieval of WeCom
// bot installations. Parallels lark.InstallationService.
type InstallationService struct {
	queries InstallationQueries
	box     *secretbox.Box
}

type InstallationQueries interface {
	CreateWechatInstallation(ctx context.Context, arg db.CreateWechatInstallationParams) (db.WechatInstallation, error)
	GetWechatInstallation(ctx context.Context, id pgtype.UUID) (db.WechatInstallation, error)
	GetWechatInstallationByBotID(ctx context.Context, botID string) (db.WechatInstallation, error)
	ListWechatInstallationsByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]db.WechatInstallation, error)
	ListActiveWechatInstallations(ctx context.Context) ([]db.WechatInstallation, error)
	RevokeWechatInstallation(ctx context.Context, id pgtype.UUID) error
	AcquireWechatWSLease(ctx context.Context, arg db.AcquireWechatWSLeaseParams) (db.WechatInstallation, error)
	ReleaseWechatWSLease(ctx context.Context, arg db.ReleaseWechatWSLeaseParams) error
}

func NewInstallationService(queries InstallationQueries, box *secretbox.Box) (*InstallationService, error) {
	if box == nil {
		return nil, fmt.Errorf("wechat: secretbox is required")
	}
	return &InstallationService{queries: queries, box: box}, nil
}

type CreateInstallationParams struct {
	WorkspaceID     pgtype.UUID
	AgentID         pgtype.UUID
	BotID           string
	Secret          string
	InstallerUserID pgtype.UUID
}

func (s *InstallationService) Create(ctx context.Context, p CreateInstallationParams) (db.WechatInstallation, error) {
	encrypted, err := s.box.Seal([]byte(p.Secret))
	if err != nil {
		return db.WechatInstallation{}, fmt.Errorf("encrypt secret: %w", err)
	}
	return s.queries.CreateWechatInstallation(ctx, db.CreateWechatInstallationParams{
		WorkspaceID:     p.WorkspaceID,
		AgentID:         p.AgentID,
		BotID:           p.BotID,
		SecretEncrypted: encrypted,
		InstallerUserID: p.InstallerUserID,
	})
}

func (s *InstallationService) DecryptSecret(inst db.WechatInstallation) (string, error) {
	plain, err := s.box.Open(inst.SecretEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plain), nil
}

func (s *InstallationService) Get(ctx context.Context, id pgtype.UUID) (db.WechatInstallation, error) {
	return s.queries.GetWechatInstallation(ctx, id)
}

func (s *InstallationService) GetByBotID(ctx context.Context, botID string) (db.WechatInstallation, error) {
	return s.queries.GetWechatInstallationByBotID(ctx, botID)
}

func (s *InstallationService) ListByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]db.WechatInstallation, error) {
	return s.queries.ListWechatInstallationsByWorkspace(ctx, workspaceID)
}

func (s *InstallationService) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.queries.RevokeWechatInstallation(ctx, id)
}
