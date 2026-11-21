package sharecrm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	ErrInstallationNotFound         = errors.New("sharecrm installation not found")
	ErrRobotOwnedByAnotherWorkspace = errors.New("sharecrm: this bot is already connected to a different Multica workspace")
	ErrRobotOwnedBySameWorkspace    = errors.New("sharecrm: this bot is already connected to another agent in this workspace")
	ErrRobotOwnedByArchivedAgent    = errors.New("sharecrm: this bot is connected to an archived agent in this workspace")
)

type installQueries interface {
	WithTx(tx pgx.Tx) installQueries
	UpsertChannelInstallation(ctx context.Context, arg db.UpsertChannelInstallationParams) (db.ChannelInstallation, error)
	ReclaimDeadChannelInstallationByAppID(ctx context.Context, arg db.ReclaimDeadChannelInstallationByAppIDParams) (pgtype.UUID, error)
	GetChannelInstallationOwnerByAppID(ctx context.Context, arg db.GetChannelInstallationOwnerByAppIDParams) (db.GetChannelInstallationOwnerByAppIDRow, error)
	LockShareCRMInstallationOwner(ctx context.Context, arg db.LockShareCRMInstallationOwnerParams) error
	GetShareCRMInstallationOwnerForUpdate(ctx context.Context, arg db.GetShareCRMInstallationOwnerForUpdateParams) (db.GetShareCRMInstallationOwnerForUpdateRow, error)
	DeleteShareCRMInstallationForReplacement(ctx context.Context, arg db.DeleteShareCRMInstallationForReplacementParams) (pgtype.UUID, error)
	ListChannelInstallationsByWorkspace(ctx context.Context, arg db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error)
	GetChannelInstallationInWorkspace(ctx context.Context, arg db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error)
	SetChannelInstallationStatus(ctx context.Context, arg db.SetChannelInstallationStatusParams) error
}

type dbInstallQueries struct{ *db.Queries }

func (q dbInstallQueries) WithTx(tx pgx.Tx) installQueries {
	return dbInstallQueries{q.Queries.WithTx(tx)}
}

// InstallService owns at-rest encryption of the appSecret and BYO install.
type InstallService struct {
	box               *secretbox.Box
	q                 installQueries
	tx                engine.TxStarter
	httpClient        *http.Client
	logger            *slog.Logger
	validationTimeout time.Duration
}

func NewInstallService(q *db.Queries, tx engine.TxStarter, box *secretbox.Box, logger *slog.Logger) (*InstallService, error) {
	if q == nil {
		return nil, errors.New("sharecrm: InstallService requires queries")
	}
	return newInstallService(dbInstallQueries{q}, tx, box, logger)
}

func newInstallService(q installQueries, tx engine.TxStarter, box *secretbox.Box, logger *slog.Logger) (*InstallService, error) {
	if box == nil {
		return nil, errors.New("sharecrm: InstallService requires a non-nil secretbox.Box")
	}
	if q == nil {
		return nil, errors.New("sharecrm: InstallService requires queries")
	}
	if tx == nil {
		return nil, errors.New("sharecrm: InstallService requires a tx starter")
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

type installPersist struct {
	wsID        pgtype.UUID
	agentID     pgtype.UUID
	installerID pgtype.UUID
	appIDKey    string
	configJSON  []byte
}

const pgUniqueViolation = "23505"

// persistInstall stores one ShareCRM bot per (workspace, agent). Reconnecting
// the SAME App ID updates the row in place and preserves its installation-scoped
// state. Connecting a DIFFERENT App ID retires that state and inserts a fresh
// installation id: ShareCRM user ids are only meaningful within one bot/app, so
// user and session bindings must never cross from one robot identity to another.
//
// The (channel_type, app_id) routing index is the only OTHER unique constraint.
// It is NOT this upsert's conflict target, so binding the robot to a DIFFERENT
// agent would trip it. Before upserting we therefore reclaim a DEAD prior owner
// of the App ID (a revoked placeholder, or an orphan whose workspace/agent was
// deleted) so the robot can move to the new agent; a LIVE owner trips the unique
// index and is refused with an accurate conflict sentinel.
func (s *InstallService) persistInstall(ctx context.Context, p installPersist) (db.ChannelInstallation, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("begin install tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	// Serialize the logical (workspace, agent, sharecrm) slot across a possible
	// delete+insert gap so concurrent replacements cannot update a just-created
	// identity in place and re-attach old bindings.
	if err := qtx.LockShareCRMInstallationOwner(ctx, db.LockShareCRMInstallationOwnerParams{
		WorkspaceID: p.wsID,
		AgentID:     p.agentID,
	}); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("lock sharecrm installation owner: %w", err)
	}

	// Free the (sharecrm, app_id) routing slot from any DEAD prior owner before
	// the upsert so a robot whose old owner is gone can be rebound. A live
	// owner is left in place and trips the unique index below.
	if _, err := qtx.ReclaimDeadChannelInstallationByAppID(ctx, db.ReclaimDeadChannelInstallationByAppIDParams{
		ChannelType: string(TypeShareCRM),
		AppID:       p.appIDKey,
		WorkspaceID: p.wsID,
		AgentID:     p.agentID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return db.ChannelInstallation{}, fmt.Errorf("reclaim dead sharecrm installation: %w", err)
	}

	current, err := qtx.GetShareCRMInstallationOwnerForUpdate(ctx, db.GetShareCRMInstallationOwnerForUpdateParams{
		WorkspaceID: p.wsID,
		AgentID:     p.agentID,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return db.ChannelInstallation{}, fmt.Errorf("load current sharecrm installation: %w", err)
	}
	if err == nil && current.AppID != p.appIDKey {
		if _, err := qtx.DeleteShareCRMInstallationForReplacement(ctx, db.DeleteShareCRMInstallationForReplacementParams{
			InstallationID: current.ID,
			WorkspaceID:    p.wsID,
			AgentID:        p.agentID,
		}); err != nil {
			return db.ChannelInstallation{}, fmt.Errorf("retire replaced sharecrm installation: %w", err)
		}
	}

	inst, err := qtx.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{
		WorkspaceID:     p.wsID,
		AgentID:         p.agentID,
		ChannelType:     string(TypeShareCRM),
		Config:          p.configJSON,
		InstallerUserID: p.installerID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return db.ChannelInstallation{}, s.liveOwnerConflictErr(ctx, p.wsID, p.appIDKey)
		}
		return db.ChannelInstallation{}, fmt.Errorf("upsert sharecrm installation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("commit sharecrm install: %w", err)
	}
	return inst, nil
}

func (s *InstallService) liveOwnerConflictErr(ctx context.Context, requestingWorkspaceID pgtype.UUID, appID string) error {
	owner, err := s.q.GetChannelInstallationOwnerByAppID(ctx, db.GetChannelInstallationOwnerByAppIDParams{
		ChannelType: string(TypeShareCRM),
		AppID:       appID,
	})
	if err != nil {
		return ErrRobotOwnedByAnotherWorkspace
	}
	switch {
	case owner.WorkspaceID != requestingWorkspaceID:
		return ErrRobotOwnedByAnotherWorkspace
	case owner.AgentArchivedAt.Valid:
		return ErrRobotOwnedByArchivedAgent
	default:
		return ErrRobotOwnedBySameWorkspace
	}
}

func (s *InstallService) ListByWorkspace(ctx context.Context, wsID pgtype.UUID) ([]db.ChannelInstallation, error) {
	return s.q.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: wsID,
		ChannelType: string(TypeShareCRM),
	})
}

func (s *InstallService) GetInWorkspace(ctx context.Context, id, wsID pgtype.UUID) (db.ChannelInstallation, error) {
	inst, err := s.q.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID:          id,
		WorkspaceID: wsID,
		ChannelType: string(TypeShareCRM),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ChannelInstallation{}, ErrInstallationNotFound
		}
		return db.ChannelInstallation{}, err
	}
	return inst, nil
}

func (s *InstallService) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.q.SetChannelInstallationStatus(ctx, db.SetChannelInstallationStatusParams{
		ID:     id,
		Status: "revoked",
	})
}
