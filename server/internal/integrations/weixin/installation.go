package weixin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type InstallationParams struct {
	WorkspaceID     pgtype.UUID
	AgentID         pgtype.UUID
	InstallerUserID pgtype.UUID
	BotID           string
	WeixinUserID    string
	BaseURL         string
	Token           string
}

type InstallationService struct {
	store *Store
	tx    engine.TxStarter
	box   *secretbox.Box
}

func NewInstallationService(q *db.Queries, tx engine.TxStarter, box *secretbox.Box) (*InstallationService, error) {
	if box == nil {
		return nil, errors.New("weixin: installation service requires secretbox")
	}
	return &InstallationService{store: NewStore(q), tx: tx, box: box}, nil
}

func (s *InstallationService) Upsert(ctx context.Context, p InstallationParams) (Installation, error) {
	if !p.WorkspaceID.Valid || !p.AgentID.Valid || !p.InstallerUserID.Valid || strings.TrimSpace(p.BotID) == "" || strings.TrimSpace(p.WeixinUserID) == "" || strings.TrimSpace(p.Token) == "" {
		return Installation{}, errors.New("weixin: incomplete installation")
	}
	baseURL, err := normalizeBaseURL(p.BaseURL)
	if err != nil {
		return Installation{}, err
	}
	sealed, err := s.box.Seal([]byte(p.Token))
	if err != nil {
		return Installation{}, fmt.Errorf("weixin: encrypt token: %w", err)
	}
	cfg, err := encodeInstallConfig(Installation{
		BotID: p.BotID, WeixinUserID: p.WeixinUserID, BaseURL: baseURL, TokenEncrypted: sealed,
	})
	if err != nil {
		return Installation{}, err
	}
	if s.tx == nil {
		return Installation{}, errors.New("weixin: transaction starter missing")
	}
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return Installation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.store.WithTx(tx)
	if _, err := qtx.ReclaimDeadChannelInstallationByAppID(ctx, db.ReclaimDeadChannelInstallationByAppIDParams{
		ChannelType: channelTypeWeixin, AppID: p.BotID, WorkspaceID: p.WorkspaceID, AgentID: p.AgentID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Installation{}, fmt.Errorf("weixin: reclaim installation: %w", err)
	}
	row, err := qtx.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{
		WorkspaceID: p.WorkspaceID, AgentID: p.AgentID, ChannelType: channelTypeWeixin,
		Config: cfg, InstallerUserID: p.InstallerUserID,
	})
	if err != nil {
		return Installation{}, fmt.Errorf("weixin: upsert installation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Installation{}, err
	}
	return installationFromRow(row)
}

func (s *InstallationService) ListByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]Installation, error) {
	rows, err := s.store.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: workspaceID, ChannelType: channelTypeWeixin,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Installation, 0, len(rows))
	for _, row := range rows {
		inst, err := installationFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	return out, nil
}

var ErrInstallationNotFound = errors.New("weixin: installation not found")

func (s *InstallationService) GetInWorkspace(ctx context.Context, id, workspaceID pgtype.UUID) (Installation, error) {
	row, err := s.store.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID: id, WorkspaceID: workspaceID, ChannelType: channelTypeWeixin,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Installation{}, ErrInstallationNotFound
	}
	if err != nil {
		return Installation{}, err
	}
	return installationFromRow(row)
}

func (s *InstallationService) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.store.SetChannelInstallationStatus(ctx, db.SetChannelInstallationStatusParams{ID: id, Status: string(InstallationRevoked)})
}

func (s *InstallationService) Box() *secretbox.Box { return s.box }
