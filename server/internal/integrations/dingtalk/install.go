package dingtalk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
// write a channel_installation with a plaintext secret — plus the shared
// persistInstall transaction and the list / get / revoke management surface.

var (
	// ErrInstallationNotFound surfaces "no row matches in this workspace".
	ErrInstallationNotFound = errors.New("dingtalk installation not found")
	// ErrAppOwnedByAnotherAgent is returned when the pasted DingTalk robot is
	// already connected to a LIVE agent. A DingTalk robot is one bot identity and
	// maps to one agent, so reusing it requires disconnecting the owner first.
	// "Live" is anything ReclaimDeadAppID refuses: an active installation, an
	// archived agent (archiving is reversible), or a REVOKED binding owned by
	// another workspace (its data is preserved for that workspace to re-install).
	// Only a DEAD prior binding — orphaned (workspace/agent deleted) or revoked in
	// the SAME workspace — is reclaimed so the robot can move to the new agent.
	ErrAppOwnedByAnotherAgent = errors.New("dingtalk: this DingTalk robot is already connected to another agent")
)

// installQueries is the slice of generated queries InstallService needs. WithTx
// returns the same interface bound to a transaction so persistInstall runs its
// upsert atomically (and so tests can inject a fake without a real DB).
type installQueries interface {
	WithTx(tx pgx.Tx) installQueries
	UpsertChannelInstallation(ctx context.Context, arg db.UpsertChannelInstallationParams) (db.ChannelInstallation, error)
	ListChannelInstallationsByWorkspace(ctx context.Context, arg db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error)
	GetChannelInstallationInWorkspace(ctx context.Context, arg db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error)
	GetChannelInstallationReclaimByAppID(ctx context.Context, arg db.GetChannelInstallationReclaimByAppIDParams) (db.GetChannelInstallationReclaimByAppIDRow, error)
	SetChannelInstallationStatus(ctx context.Context, arg db.SetChannelInstallationStatusParams) error
	DeleteChannelInstallation(ctx context.Context, id pgtype.UUID) error
}

// dbInstallQueries adapts *db.Queries to installQueries — the generated WithTx
// returns *db.Queries, so we wrap it to return the interface (the same adapter
// pattern slack.InstallService uses).
type dbInstallQueries struct{ *db.Queries }

func (q dbInstallQueries) WithTx(tx pgx.Tx) installQueries {
	return dbInstallQueries{q.Queries.WithTx(tx)}
}

// InstallService owns the at-rest encryption of the AppSecret (so no caller can
// write a channel_installation with a plaintext secret) and the shared install
// transaction. The box MUST be non-nil (we refuse plaintext storage even in
// dev).
type InstallService struct {
	box        *secretbox.Box
	q          installQueries
	tx         engine.TxStarter
	httpClient *http.Client
	logger     *slog.Logger

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
		box:        box,
		q:          q,
		tx:         tx,
		httpClient: http.DefaultClient,
		logger:     logger,
	}, nil
}

// installPersist carries the resolved fields persistInstall writes. configJSON
// holds the AppKey (config->>'app_id') used for inbound routing; the ROW itself
// is keyed by (workspace, agent) — one bot per agent. appID mirrors that routing
// key so the reclaim probe can look the robot up without re-parsing configJSON.
type installPersist struct {
	wsID        pgtype.UUID
	agentID     pgtype.UUID
	installerID pgtype.UUID
	appID       string
	configJSON  []byte
}

// pgUniqueViolation is the Postgres SQLSTATE for a unique-constraint violation.
const pgUniqueViolation = "23505"

// persistInstall upserts the installation keyed by (workspace_id, agent_id,
// channel_type): ONE DingTalk bot per agent. Re-connecting an agent — including
// swapping it to a NEW DingTalk robot after a disconnect — UPDATES that agent's
// row in place instead of colliding with the (workspace, agent, channel) unique.
//
// The (channel_type, app_id) routing index is the only OTHER unique constraint.
// It is NOT this upsert's conflict target, so binding the robot to a DIFFERENT
// agent would trip it. Before upserting we therefore reclaim a DEAD prior binding
// (see ReclaimDeadAppID) so the robot can move to the new agent; a LIVE owner —
// including another workspace's revoked binding — is refused with
// ErrAppOwnedByAnotherAgent. The reclaim runs in the SAME
// tx and BEFORE the upsert on purpose: a failed statement aborts a pgx tx, so we
// must not let the unique violation fire and then try to recover after it.
func (s *InstallService) persistInstall(ctx context.Context, p installPersist) (db.ChannelInstallation, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("begin install tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	if err := engine.ReclaimDeadAppID(ctx, qtx, string(TypeDingTalk), p.appID, p.wsID, p.agentID); err != nil {
		if errors.Is(err, engine.ErrAppOwnedByLiveAgent) {
			return db.ChannelInstallation{}, ErrAppOwnedByAnotherAgent
		}
		return db.ChannelInstallation{}, err
	}

	inst, err := qtx.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{
		WorkspaceID:     p.wsID,
		AgentID:         p.agentID,
		ChannelType:     string(TypeDingTalk),
		Config:          p.configJSON,
		InstallerUserID: p.installerID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			// A LIVE owner raced in between the reclaim probe and this upsert; the
			// routing index remains the ultimate guard, so treat it as a conflict.
			return db.ChannelInstallation{}, ErrAppOwnedByAnotherAgent
		}
		return db.ChannelInstallation{}, fmt.Errorf("upsert dingtalk installation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("commit dingtalk install: %w", err)
	}
	return inst, nil
}

// ListByWorkspace returns every DingTalk installation in the workspace (active
// and revoked), for the management surface.
func (s *InstallService) ListByWorkspace(ctx context.Context, wsID pgtype.UUID) ([]db.ChannelInstallation, error) {
	return s.q.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: wsID,
		ChannelType: string(TypeDingTalk),
	})
}

// GetInWorkspace is the workspace-scoped lookup so a forged installation id from
// another workspace returns NotFound instead of leaking existence.
func (s *InstallService) GetInWorkspace(ctx context.Context, id, wsID pgtype.UUID) (db.ChannelInstallation, error) {
	inst, err := s.q.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID:          id,
		WorkspaceID: wsID,
		ChannelType: string(TypeDingTalk),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ChannelInstallation{}, ErrInstallationNotFound
		}
		return db.ChannelInstallation{}, err
	}
	return inst, nil
}

// Revoke flips status to 'revoked'. The row is preserved for audit; a re-install
// flips it back to 'active'. The Supervisor stops supervising the installation
// (ListActiveInstallations filters to active), so its Stream connection winds
// down, and outbound drops too.
func (s *InstallService) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.q.SetChannelInstallationStatus(ctx, db.SetChannelInstallationStatusParams{
		ID:     id,
		Status: "revoked",
	})
}
