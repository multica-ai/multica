package infoflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file is the InfoFlow install backend. InfoFlow uses the
// bring-your-own-app (BYO) model: an agent owner or workspace admin creates an
// InfoFlow robot, then pastes its AppKey + AppSecret into Multica. The
// InstallService owns the at-rest encryption of the AppSecret — so no caller can
// write a channel_installation with a plaintext secret — plus the shared
// install transaction and the list / get / revoke management surface.

var (
	// ErrInstallationNotFound surfaces "no row matches in this workspace".
	ErrInstallationNotFound = errors.New("infoflow installation not found")
	// ErrInvalidAppKey / ErrInvalidAppSecret are returned when a pasted
	// credential is empty. The handler maps them to 400 so the dialog can show a
	// precise hint instead of a generic failure.
	ErrInvalidAppKey    = errors.New("infoflow: AppKey is required")
	ErrInvalidAppSecret = errors.New("infoflow: AppSecret is required")
	// ErrAppOwnedByAnotherWorkspace is returned when the pasted InfoFlow robot
	// is already connected to a live owner in a DIFFERENT workspace.
	ErrAppOwnedByAnotherWorkspace = errors.New("infoflow: this bot is already connected to a different Multica workspace")
	// ErrAppOwnedBySameWorkspace is returned when the bot is already connected
	// to a DIFFERENT (live, non-archived) agent in the SAME workspace.
	ErrAppOwnedBySameWorkspace = errors.New("infoflow: this bot is already connected to another agent in this workspace")
	// ErrAppOwnedByArchivedAgent is returned when the bot's owning agent is
	// archived. The user recovers by restoring that agent or disconnecting it.
	ErrAppOwnedByArchivedAgent = errors.New("infoflow: this bot is connected to an archived agent in this workspace")
	// ErrAccess is returned when the actor may not manage the installation.
	ErrAccess = errors.New("infoflow: you do not have access to this installation")
)

// installQueries is the slice of generated queries InstallService needs. WithTx
// returns the same interface bound to a transaction so persistInstall runs its
// upsert atomically (and so tests can inject a fake without a real DB).
type installQueries interface {
	WithTx(tx pgx.Tx) installQueries
	UpsertChannelInstallation(ctx context.Context, arg db.UpsertChannelInstallationParams) (db.ChannelInstallation, error)
	ListChannelInstallationsByWorkspace(ctx context.Context, arg db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error)
	ClearChannelInstallationBotScopedRows(ctx context.Context, installationID pgtype.UUID) (db.ClearChannelInstallationBotScopedRowsRow, error)
	ReclaimDeadChannelInstallationByAppID(ctx context.Context, arg db.ReclaimDeadChannelInstallationByAppIDParams) (pgtype.UUID, error)
	GetChannelInstallationOwnerByAppID(ctx context.Context, arg db.GetChannelInstallationOwnerByAppIDParams) (db.GetChannelInstallationOwnerByAppIDRow, error)
	GetChannelInstallationInWorkspace(ctx context.Context, arg db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error)
	SetChannelInstallationStatus(ctx context.Context, arg db.SetChannelInstallationStatusParams) error
	GetMemberByUserAndWorkspace(ctx context.Context, arg db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
	GetAgentInWorkspace(ctx context.Context, arg db.GetAgentInWorkspaceParams) (db.Agent, error)
}

// dbInstallQueries adapts *db.Queries to installQueries — the generated WithTx
// returns *db.Queries, so we wrap it to return the interface (the same adapter
// pattern dingtalk.InstallService uses).
type dbInstallQueries struct{ *db.Queries }

func (q dbInstallQueries) WithTx(tx pgx.Tx) installQueries {
	return dbInstallQueries{q.Queries.WithTx(tx)}
}

// InstallService owns the at-rest encryption of the AppSecret (so no caller can
// write a channel_installation with a plaintext secret) and the shared install
// transaction.
type InstallService struct {
	box    *secretbox.Box
	q      installQueries
	tx     engine.TxStarter
	logger *slog.Logger
}

// NewInstallService binds the service to queries, a tx starter (*pgxpool.Pool),
// and an encryption box.
func NewInstallService(q *db.Queries, tx engine.TxStarter, box *secretbox.Box, logger *slog.Logger) (*InstallService, error) {
	if q == nil {
		return nil, errors.New("infoflow: InstallService requires queries")
	}
	if box == nil {
		return nil, errors.New("infoflow: InstallService requires a non-nil secretbox.Box")
	}
	if tx == nil {
		return nil, errors.New("infoflow: InstallService requires a tx starter")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &InstallService{box: box, q: dbInstallQueries{q}, tx: tx, logger: logger}, nil
}

// RegisterBYOParams are the inputs for a bring-your-own-app install: the agent
// this bot represents, who is installing, and the two credentials the user
// pasted from their own InfoFlow robot.
type RegisterBYOParams struct {
	WorkspaceID pgtype.UUID
	AgentID     pgtype.UUID
	InitiatorID pgtype.UUID
	AppKey      string
	AppSecret   string
}

// PublicConfig is the non-secret view of an installation config, safe to render
// in the management UI. The AppSecret is deliberately absent.
type PublicConfig struct {
	AppKey string `json:"app_key"`
}

// DecodePublicConfig extracts the non-secret fields of a stored config. A
// malformed blob decodes to an empty AppKey rather than an error, matching the
// best-effort read the HTTP surface needs.
func DecodePublicConfig(raw json.RawMessage) PublicConfig {
	var cfg installConfig
	_ = json.Unmarshal(raw, &cfg)
	return PublicConfig{AppKey: cfg.AppID}
}

// RegisterBYO installs a user-supplied ("bring your own") InfoFlow robot for an
// agent. The AppKey is stored as the inbound routing key (config->>'app_id');
// the AppSecret is sealed with the at-rest key. The channel supervisor picks the
// installation up and opens its WebSocket session; this method only persists the
// installation.
func (s *InstallService) RegisterBYO(ctx context.Context, p RegisterBYOParams) (db.ChannelInstallation, error) {
	appKey := strings.TrimSpace(p.AppKey)
	appSecret := strings.TrimSpace(p.AppSecret)
	if appKey == "" {
		return db.ChannelInstallation{}, ErrInvalidAppKey
	}
	if appSecret == "" {
		return db.ChannelInstallation{}, ErrInvalidAppSecret
	}

	sealedSecret, err := s.box.Seal([]byte(appSecret))
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encrypt infoflow app secret: %w", err)
	}
	cfgJSON, err := json.Marshal(installConfig{
		AppID:              appKey,
		AppSecretEncrypted: base64.StdEncoding.EncodeToString(sealedSecret),
	})
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encode infoflow installation config: %w", err)
	}

	return s.persistInstall(ctx, installPersist{
		wsID:        p.WorkspaceID,
		agentID:     p.AgentID,
		installerID: p.InitiatorID,
		appIDKey:    appKey,
		configJSON:  cfgJSON,
	})
}

// installPersist carries the resolved fields persistInstall writes.
type installPersist struct {
	wsID        pgtype.UUID
	agentID     pgtype.UUID
	installerID pgtype.UUID
	appIDKey    string
	configJSON  []byte
}

// pgUniqueViolation is the Postgres SQLSTATE for a unique-constraint violation.
const pgUniqueViolation = "23505"

// persistInstall stores one InfoFlow bot per (workspace, agent). Reconnecting
// the SAME AppKey updates the row in place and preserves its installation-scoped
// state. Connecting a DIFFERENT AppKey keeps the installation id but clears its
// bot-scoped bindings so no identity state crosses from one robot identity to
// another (weixin does the same via ClearChannelInstallationBotScopedRows).
//
// The (channel_type, app_id) routing index is the only OTHER unique constraint.
// It is NOT this upsert's conflict target, so binding the bot to a DIFFERENT
// agent would trip it. Before upserting we reclaim a DEAD prior owner of the
// AppKey (a revoked placeholder, or an orphan whose workspace/agent was
// deleted); a LIVE owner trips the unique index and is refused with an accurate
// conflict sentinel.
func (s *InstallService) persistInstall(ctx context.Context, p installPersist) (db.ChannelInstallation, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("begin infoflow install tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	// Free the (infoflow, app_id) routing slot from any DEAD prior owner so a
	// bot whose old owner is gone can be rebound.
	if _, err := qtx.ReclaimDeadChannelInstallationByAppID(ctx, db.ReclaimDeadChannelInstallationByAppIDParams{
		ChannelType: string(TypeInfoFlow),
		AppID:       p.appIDKey,
		WorkspaceID: p.wsID,
		AgentID:     p.agentID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return db.ChannelInstallation{}, fmt.Errorf("reclaim dead infoflow installation: %w", err)
	}

	// If this agent already has an infoflow installation bound to a DIFFERENT
	// AppKey, clear its bot-scoped state so user/session bindings never cross
	// robot identities.
	existing, err := s.agentInstallation(ctx, qtx, p.wsID, p.agentID)
	if err != nil {
		return db.ChannelInstallation{}, err
	}
	if existing != nil && existing.Status == "active" {
		if prev := DecodePublicConfig(existing.Config); prev.AppKey != "" && prev.AppKey != p.appIDKey {
			if _, err := qtx.ClearChannelInstallationBotScopedRows(ctx, existing.ID); err != nil {
				return db.ChannelInstallation{}, fmt.Errorf("clear replaced infoflow bindings: %w", err)
			}
		}
	}

	inst, err := qtx.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{
		WorkspaceID:     p.wsID,
		AgentID:         p.agentID,
		ChannelType:     string(TypeInfoFlow),
		Config:          p.configJSON,
		InstallerUserID: p.installerID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return db.ChannelInstallation{}, s.liveOwnerConflictErr(ctx, p.wsID, p.appIDKey)
		}
		return db.ChannelInstallation{}, fmt.Errorf("upsert infoflow installation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("commit infoflow install: %w", err)
	}
	return inst, nil
}

// agentInstallation returns the agent's current infoflow installation, or nil.
func (s *InstallService) agentInstallation(ctx context.Context, q installQueries, wsID, agentID pgtype.UUID) (*db.ChannelInstallation, error) {
	rows, err := q.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: wsID,
		ChannelType: string(TypeInfoFlow),
	})
	if err != nil {
		return nil, fmt.Errorf("list infoflow installations: %w", err)
	}
	for i := range rows {
		if rows[i].AgentID == agentID {
			return &rows[i], nil
		}
	}
	return nil, nil
}

// liveOwnerConflictErr classifies who holds the (infoflow, app_id) routing slot
// after the dead-owner reclaim ran, so persistInstall returns a sentinel the
// handler renders as an accurate message. Read on the base pool (s.q), since the
// failed upsert has aborted the tx.
func (s *InstallService) liveOwnerConflictErr(ctx context.Context, requestingWorkspaceID pgtype.UUID, appID string) error {
	owner, err := s.q.GetChannelInstallationOwnerByAppID(ctx, db.GetChannelInstallationOwnerByAppIDParams{
		ChannelType: string(TypeInfoFlow),
		AppID:       appID,
	})
	if err != nil {
		return ErrAppOwnedByAnotherWorkspace
	}
	switch {
	case owner.WorkspaceID != requestingWorkspaceID:
		return ErrAppOwnedByAnotherWorkspace
	case owner.AgentArchivedAt.Valid:
		return ErrAppOwnedByArchivedAgent
	default:
		return ErrAppOwnedBySameWorkspace
	}
}

// ListByWorkspace returns every InfoFlow installation in the workspace (active
// and revoked), for the management surface.
func (s *InstallService) ListByWorkspace(ctx context.Context, wsID pgtype.UUID) ([]db.ChannelInstallation, error) {
	return s.q.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: wsID,
		ChannelType: string(TypeInfoFlow),
	})
}

// GetInWorkspace is the workspace-scoped lookup so a forged installation id from
// another workspace returns NotFound instead of leaking existence.
func (s *InstallService) GetInWorkspace(ctx context.Context, id, wsID pgtype.UUID) (db.ChannelInstallation, error) {
	inst, err := s.q.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID:          id,
		WorkspaceID: wsID,
		ChannelType: string(TypeInfoFlow),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ChannelInstallation{}, ErrInstallationNotFound
		}
		return db.ChannelInstallation{}, err
	}
	return inst, nil
}

// CanManage reports whether the actor may disconnect this installation: a
// workspace owner/admin, or the agent owner when the installation belongs to an
// agent they own. An orphaned installation leaves cleanup to workspace admins.
func (s *InstallService) CanManage(ctx context.Context, inst db.ChannelInstallation, wsID, actor pgtype.UUID) bool {
	member, err := s.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: actor, WorkspaceID: wsID})
	if err != nil {
		return false
	}
	if member.Role == "owner" || member.Role == "admin" {
		return true
	}
	agent, err := s.q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: inst.AgentID, WorkspaceID: wsID})
	if err != nil {
		return false
	}
	return agent.OwnerID.Valid && agent.OwnerID == actor
}

// Revoke flips status to 'revoked'. The row is preserved for audit; a re-install
// flips it back to 'active'. The Supervisor stops supervising the installation
// (ListActiveInstallations filters to active), so its WebSocket session winds
// down, and outbound drops too.
func (s *InstallService) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.q.SetChannelInstallationStatus(ctx, db.SetChannelInstallationStatusParams{
		ID:     id,
		Status: "revoked",
	})
}
