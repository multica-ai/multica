package dingtalk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file is the DingTalk install backend. DingTalk uses the
// bring-your-own-app (BYO) model: the workspace admin creates their own DingTalk
// Stream-mode robot, and pastes its AppKey (client id) + AppSecret (client
// secret) into Multica (the paste path lives in byo_install.go). The
// InstallService owns the at-rest encryption of the AppSecret — so no caller can
// write a dingtalk_connector with a plaintext secret — plus the shared
// persistInstall transaction and the list / get / revoke management surface.

var (
	// ErrInstallationNotFound surfaces "no row matches in this workspace".
	ErrInstallationNotFound = errors.New("dingtalk installation not found")
)

// installQueries is the slice of generated queries InstallService needs. WithTx
// returns the same interface bound to a transaction so persistInstall runs its
// upsert atomically (and so tests can inject a fake without a real DB).
type installQueries interface {
	WithTx(tx pgx.Tx) installQueries
	LockWorkspaceForChatSessionCreate(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error)
	LockDingTalkInstallTarget(ctx context.Context, arg db.LockDingTalkInstallTargetParams) (pgtype.UUID, error)
	LockDingTalkConnectorAppID(ctx context.Context, appID string) error
	GetDingTalkConnectorByAppIDForUpdate(ctx context.Context, appID string) (db.DingtalkConnector, error)
	LockDingTalkConnectorForUpdate(ctx context.Context, id pgtype.UUID) (db.DingtalkConnector, error)
	CreateDingTalkConnector(ctx context.Context, arg db.CreateDingTalkConnectorParams) (db.DingtalkConnector, error)
	UpdateDingTalkConnectorCredentials(ctx context.Context, arg db.UpdateDingTalkConnectorCredentialsParams) (db.DingtalkConnector, error)
	UpsertDingTalkWorkspaceGrant(ctx context.Context, arg db.UpsertDingTalkWorkspaceGrantParams) (db.DingtalkWorkspaceGrant, error)
	ListDingTalkInstallationsByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]db.ListDingTalkInstallationsByWorkspaceRow, error)
	GetDingTalkInstallationInWorkspace(ctx context.Context, arg db.GetDingTalkInstallationInWorkspaceParams) (db.GetDingTalkInstallationInWorkspaceRow, error)
	RevokeDingTalkWorkspaceGrantOnly(ctx context.Context, arg db.RevokeDingTalkWorkspaceGrantOnlyParams) (pgtype.UUID, error)
	CountActiveDingTalkWorkspaceGrants(ctx context.Context, connectorID pgtype.UUID) (int64, error)
	RevokeDingTalkConnector(ctx context.Context, id pgtype.UUID) error
	PurgeDingTalkConnectorUnscopedAudit(ctx context.Context, connectorID pgtype.UUID) error
}

// dbInstallQueries adapts *db.Queries to installQueries — the generated WithTx
// returns *db.Queries, so we wrap it to return the interface (the same adapter
// pattern slack.InstallService uses).
type dbInstallQueries struct{ *db.Queries }

func (q dbInstallQueries) WithTx(tx pgx.Tx) installQueries {
	return dbInstallQueries{q.Queries.WithTx(tx)}
}

// InstallService owns the at-rest encryption of the AppSecret (so no caller can
// write a dingtalk_connector with a plaintext secret) and the shared install
// transaction. The box MUST be non-nil (we refuse plaintext storage even in
// dev).
type InstallService struct {
	box               *secretbox.Box
	q                 installQueries
	tx                engine.TxStarter
	httpClient        *http.Client
	logger            *slog.Logger
	validationTimeout time.Duration

	// apiBase overrides the DingTalk Open-API base for the BYO access-token
	// validation call (tests point it at an httptest server). Empty uses the real
	// DingTalk API.
	apiBase string
}

// NewInstallService binds the service to queries, a tx starter (*pgxpool.Pool),
// and an encryption box. Listing / revoking and BYO register all require only
// the box (the at-rest key); there is no hosted OAuth credential.
func NewInstallService(q *db.Queries, tx engine.TxStarter, box *secretbox.Box, logger *slog.Logger) (*InstallService, error) {
	if q == nil {
		return nil, errors.New("dingtalk: InstallService requires queries")
	}
	return newInstallService(dbInstallQueries{q}, tx, box, logger)
}

// newInstallService is the testable core: it takes the installQueries interface
// so tests can inject a fake (with a fake TxStarter) without a real DB.
func newInstallService(q installQueries, tx engine.TxStarter, box *secretbox.Box, logger *slog.Logger) (*InstallService, error) {
	if box == nil {
		return nil, errors.New("dingtalk: InstallService requires a non-nil secretbox.Box")
	}
	if q == nil {
		return nil, errors.New("dingtalk: InstallService requires queries")
	}
	if tx == nil {
		return nil, errors.New("dingtalk: InstallService requires a tx starter")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &InstallService{
		box:               box,
		q:                 q,
		tx:                tx,
		httpClient:        http.DefaultClient,
		logger:            logger,
		validationTimeout: 10 * time.Second,
	}, nil
}

// installPersist carries the connector identity and the workspace-local grant
// persistInstall writes in one transaction.
type installPersist struct {
	wsID        pgtype.UUID
	agentID     pgtype.UUID
	installerID pgtype.UUID
	// appIDKey is the AppKey stored at config->>'app_id'; it MUST equal the
	// app_id inside configJSON. It keys the dead-owner reclaim and the live-owner
	// lookup that drives the accurate conflict message.
	appIDKey   string
	configJSON []byte
}

// persistInstall creates or rotates one global connector per AppKey, then
// creates/reactivates this workspace's grant. The connector UUID remains the
// installation_id used by generic channel state, so every authorized workspace
// shares one Stream connection without sharing chat sessions or route targets.
func (s *InstallService) persistInstall(ctx context.Context, p installPersist) (db.ChannelInstallation, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("begin install tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)
	if _, err := qtx.LockWorkspaceForChatSessionCreate(ctx, p.wsID); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("lock dingtalk install workspace: %w", err)
	}
	if err := qtx.LockDingTalkConnectorAppID(ctx, p.appIDKey); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("lock dingtalk connector: %w", err)
	}

	connector, err := qtx.GetDingTalkConnectorByAppIDForUpdate(ctx, p.appIDKey)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		connector, err = qtx.CreateDingTalkConnector(ctx, db.CreateDingTalkConnectorParams{
			AppID:           p.appIDKey,
			Config:          p.configJSON,
			InstallerUserID: p.installerID,
		})
	case err == nil:
		connector, err = qtx.UpdateDingTalkConnectorCredentials(ctx, db.UpdateDingTalkConnectorCredentialsParams{
			ID:              connector.ID,
			Config:          p.configJSON,
			InstallerUserID: p.installerID,
		})
	}
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("persist dingtalk connector: %w", err)
	}
	if _, err := qtx.LockDingTalkInstallTarget(ctx, db.LockDingTalkInstallTargetParams{
		InstallerUserID: p.installerID,
		AgentID:         p.agentID,
		WorkspaceID:     p.wsID,
	}); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("lock dingtalk install target: %w", err)
	}

	grant, err := qtx.UpsertDingTalkWorkspaceGrant(ctx, db.UpsertDingTalkWorkspaceGrantParams{
		ConnectorID:     connector.ID,
		WorkspaceID:     p.wsID,
		DefaultAgentID:  p.agentID,
		InstallerUserID: p.installerID,
	})
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("persist dingtalk workspace grant: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("commit dingtalk install: %w", err)
	}
	return channelInstallationFromConnectorGrant(connector, grant), nil
}

// ListByWorkspace returns every DingTalk installation in the workspace (active
// and revoked), for the management surface.
func (s *InstallService) ListByWorkspace(ctx context.Context, wsID pgtype.UUID) ([]db.ChannelInstallation, error) {
	rows, err := s.q.ListDingTalkInstallationsByWorkspace(ctx, wsID)
	if err != nil {
		return nil, err
	}
	out := make([]db.ChannelInstallation, 0, len(rows))
	for _, row := range rows {
		out = append(out, channelInstallationFromListRow(row))
	}
	return out, nil
}

// GetInWorkspace is the workspace-scoped lookup so a forged installation id from
// another workspace returns NotFound instead of leaking existence.
func (s *InstallService) GetInWorkspace(ctx context.Context, id, wsID pgtype.UUID) (db.ChannelInstallation, error) {
	row, err := s.q.GetDingTalkInstallationInWorkspace(ctx, db.GetDingTalkInstallationInWorkspaceParams{
		ConnectorID: id,
		WorkspaceID: wsID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ChannelInstallation{}, ErrInstallationNotFound
		}
		return db.ChannelInstallation{}, err
	}
	return channelInstallationFromGetRow(row), nil
}

// Revoke disables only this workspace's grant. The shared connector remains
// active while another workspace grant is active; revoking the last grant also
// stops the connector and its Stream connection. Reinstalling reactivates both.
func (s *InstallService) Revoke(ctx context.Context, id, wsID pgtype.UUID) error {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin dingtalk revoke tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)
	if _, err := qtx.LockWorkspaceForChatSessionCreate(ctx, wsID); err != nil {
		return err
	}
	if _, err := qtx.LockDingTalkConnectorForUpdate(ctx, id); err != nil {
		return err
	}
	if _, err := qtx.RevokeDingTalkWorkspaceGrantOnly(ctx, db.RevokeDingTalkWorkspaceGrantOnlyParams{
		ConnectorID: id,
		WorkspaceID: wsID,
	}); err != nil {
		return err
	}
	remaining, err := qtx.CountActiveDingTalkWorkspaceGrants(ctx, id)
	if err != nil {
		return err
	}
	if remaining == 0 {
		if err := qtx.RevokeDingTalkConnector(ctx, id); err != nil {
			return err
		}
		if err := qtx.PurgeDingTalkConnectorUnscopedAudit(ctx, id); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit dingtalk revoke: %w", err)
	}
	return nil
}

func channelInstallationFromConnectorGrant(connector db.DingtalkConnector, grant db.DingtalkWorkspaceGrant) db.ChannelInstallation {
	return db.ChannelInstallation{
		ID: connector.ID, WorkspaceID: grant.WorkspaceID,
		AgentID: grant.DefaultAgentID, ChannelType: string(TypeDingTalk),
		Config: connector.Config, Status: grant.Status,
		WsLeaseToken: connector.WsLeaseToken, WsLeaseExpiresAt: connector.WsLeaseExpiresAt,
		InstallerUserID: grant.InstallerUserID, InstalledAt: grant.InstalledAt,
		CreatedAt: grant.CreatedAt, UpdatedAt: laterTimestamp(connector.UpdatedAt, grant.UpdatedAt),
	}
}

func channelInstallationFromListRow(row db.ListDingTalkInstallationsByWorkspaceRow) db.ChannelInstallation {
	return db.ChannelInstallation{
		ID: row.ID, WorkspaceID: row.WorkspaceID, AgentID: row.AgentID,
		ChannelType: row.ChannelType, Config: row.Config, Status: row.Status,
		WsLeaseToken: row.WsLeaseToken, WsLeaseExpiresAt: row.WsLeaseExpiresAt,
		InstallerUserID: row.InstallerUserID, InstalledAt: row.InstalledAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func channelInstallationFromGetRow(row db.GetDingTalkInstallationInWorkspaceRow) db.ChannelInstallation {
	return db.ChannelInstallation{
		ID: row.ID, WorkspaceID: row.WorkspaceID, AgentID: row.AgentID,
		ChannelType: row.ChannelType, Config: row.Config, Status: row.Status,
		WsLeaseToken: row.WsLeaseToken, WsLeaseExpiresAt: row.WsLeaseExpiresAt,
		InstallerUserID: row.InstallerUserID, InstalledAt: row.InstalledAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func laterTimestamp(a, b pgtype.Timestamptz) pgtype.Timestamptz {
	if !a.Valid || (b.Valid && b.Time.After(a.Time)) {
		return b
	}
	return a
}
