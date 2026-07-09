package slack

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

// This file is the Slack install backend (MUL-3666). Slack uses the
// bring-your-own-app (BYO) model: the workspace admin creates their own Slack
// app, installs it to their Slack workspace, and pastes its bot token (xoxb-) +
// app-level token (xapp-) into Multica (the paste path lives in byo_install.go).
// The InstallService owns the at-rest encryption of those tokens — so no caller
// can write a channel_installation with a plaintext token — plus the shared
// persistInstall transaction and the list / get / revoke management surface.

var (
	// ErrInstallationNotFound surfaces "no row matches in this workspace".
	ErrInstallationNotFound = errors.New("slack installation not found")
	// ErrTeamOwnedByAnotherWorkspace is returned when the pasted Slack app is
	// already connected to an agent in a DIFFERENT Multica workspace — it would
	// collide with the (channel_type, app_id) routing index. A Slack app is one
	// bot identity and maps to one agent; reusing it elsewhere requires
	// disconnecting it there first. (#4810: this is now returned ONLY for a
	// genuinely different workspace — a same-workspace conflict returns
	// ErrTeamOwnedByAnotherAgent, and a dead-owner slot is reclaimed instead.)
	ErrTeamOwnedByAnotherWorkspace = errors.New("slack: this Slack app is already connected to a different Multica workspace")
	// ErrTeamOwnedByAnotherAgent is returned when the pasted Slack app is already
	// connected to a DIFFERENT agent in the SAME workspace. The old text wrongly
	// blamed "another workspace" (#4810); the accurate guidance is to disconnect
	// it from that agent first.
	ErrTeamOwnedByAnotherAgent = errors.New("slack: this Slack app is already connected to another agent in this workspace")
)

// installQueries is the slice of generated queries InstallService needs. WithTx
// returns the same interface bound to a transaction so persistInstall runs its
// upsert atomically (and so tests can inject a fake without a real DB).
type installQueries interface {
	WithTx(tx pgx.Tx) installQueries
	UpsertChannelInstallation(ctx context.Context, arg db.UpsertChannelInstallationParams) (db.ChannelInstallation, error)
	ReclaimOrphanedChannelInstallationByAppID(ctx context.Context, arg db.ReclaimOrphanedChannelInstallationByAppIDParams) error
	GetChannelInstallationByAppID(ctx context.Context, arg db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error)
	ListChannelInstallationsByWorkspace(ctx context.Context, arg db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error)
	GetChannelInstallationInWorkspace(ctx context.Context, arg db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error)
	SetChannelInstallationStatus(ctx context.Context, arg db.SetChannelInstallationStatusParams) error
}

// dbInstallQueries adapts *db.Queries to installQueries — the generated WithTx
// returns *db.Queries, so we wrap it to return the interface (the same adapter
// pattern engine.ChatSession uses).
type dbInstallQueries struct{ *db.Queries }

func (q dbInstallQueries) WithTx(tx pgx.Tx) installQueries {
	return dbInstallQueries{q.Queries.WithTx(tx)}
}

// InstallService owns the at-rest encryption of the bot + app tokens (so no
// caller can write a channel_installation with a plaintext token) and the shared
// install transaction. The box MUST be non-nil (we refuse plaintext storage even
// in dev).
type InstallService struct {
	box        *secretbox.Box
	q          installQueries
	tx         engine.TxStarter
	httpClient *http.Client
	logger     *slog.Logger

	// apiURL overrides the Slack API base for the BYO auth.test call (tests point
	// it at an httptest server). Empty uses the real Slack API.
	apiURL string
}

// NewInstallService binds the service to queries, a tx starter (*pgxpool.Pool),
// and an encryption box. Listing / revoking and BYO register all require only
// the box (the at-rest key); there is no hosted OAuth credential.
func NewInstallService(q *db.Queries, tx engine.TxStarter, box *secretbox.Box, logger *slog.Logger) (*InstallService, error) {
	if q == nil {
		return nil, errors.New("slack: InstallService requires queries")
	}
	return newInstallService(dbInstallQueries{q}, tx, box, logger)
}

// newInstallService is the testable core: it takes the installQueries interface
// so tests can inject a fake (with a fake TxStarter) without a real DB.
func newInstallService(q installQueries, tx engine.TxStarter, box *secretbox.Box, logger *slog.Logger) (*InstallService, error) {
	if box == nil {
		return nil, errors.New("slack: InstallService requires a non-nil secretbox.Box")
	}
	if q == nil {
		return nil, errors.New("slack: InstallService requires queries")
	}
	if tx == nil {
		return nil, errors.New("slack: InstallService requires a tx starter")
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

// installPersist carries the resolved fields persistInstall writes. appIDKey is
// the value stored at config->>'app_id' — the real Slack app id — and MUST equal
// the app_id inside configJSON; it is the lookup / ON CONFLICT key. installerSlackID
// is the installer's Slack user id to auto-bind, or "" to skip (a BYO paste
// carries no authed_user, so the installer binds via the normal token flow on
// first message).
type installPersist struct {
	wsID        pgtype.UUID
	agentID     pgtype.UUID
	installerID pgtype.UUID
	// appID is the real Slack app id (config->>'app_id'). It MUST equal the
	// app_id inside configJSON; it is the (channel_type, app_id) routing key used
	// to reclaim a dead-owner slot before the upsert and to resolve who currently
	// owns the slot on a conflict (#4810).
	appID string
	// configJSON holds the Slack app id (config->>'app_id') used for inbound
	// routing; the ROW itself is keyed by (workspace, agent) — one bot per agent.
	configJSON []byte
}

// pgUniqueViolation is the Postgres SQLSTATE for a unique-constraint violation.
const pgUniqueViolation = "23505"

// persistInstall upserts the installation keyed by (workspace_id, agent_id,
// channel_type): ONE Slack bot per agent. Re-connecting an agent — including
// swapping it to a NEW Slack app after a disconnect — UPDATES that agent's row
// in place instead of colliding with the (workspace, agent, channel) unique.
//
// The (channel_type, app_id) routing index is the only OTHER unique constraint,
// and it is NOT this upsert's conflict target, so a unique violation here means
// the pasted Slack app is already connected to a DIFFERENT agent or Multica
// workspace — refuse it (ErrTeamOwnedByAnotherWorkspace) rather than steal it.
// No chat-session retire is needed: a row's agent_id never changes (it is part
// of the key), so existing sessions stay valid for the same agent.
func (s *InstallService) persistInstall(ctx context.Context, p installPersist) (db.ChannelInstallation, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("begin install tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	// Self-heal the "dead owner" case (#4810): if the (slack, app_id) slot is held
	// by an installation whose workspace or agent has been deleted, reclaim it
	// before the insert so the bot can reconnect to a new agent. A LIVE owner
	// (active or archived) is left in place and surfaces as the conflict below.
	if err := qtx.ReclaimOrphanedChannelInstallationByAppID(ctx, db.ReclaimOrphanedChannelInstallationByAppIDParams{
		ChannelType: string(TypeSlack),
		AppID:       p.appID,
		WorkspaceID: p.wsID,
		AgentID:     p.agentID,
	}); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("reclaim orphaned slack installation: %w", err)
	}

	inst, err := qtx.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{
		WorkspaceID:     p.wsID,
		AgentID:         p.agentID,
		ChannelType:     string(TypeSlack),
		Config:          p.configJSON,
		InstallerUserID: p.installerID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return db.ChannelInstallation{}, s.ownershipConflict(ctx, p)
		}
		return db.ChannelInstallation{}, fmt.Errorf("upsert slack installation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("commit slack install: %w", err)
	}
	return inst, nil
}

// ownershipConflict resolves an accurate error for a (channel_type, app_id)
// routing-index violation on install: the slot is held by a live owner, and the
// user needs to know WHICH — another agent in THIS workspace (disconnect it
// there first) vs a genuinely different Multica workspace. Before #4810 both
// collapsed into a single, often-wrong "different workspace" message. The lookup
// runs on the pooled handle (s.q, not the now-aborted tx). If the owner has
// since vanished (a concurrent delete freed the slot) we fall back to the
// generic cross-workspace message rather than surfacing a 500 — a retry succeeds.
func (s *InstallService) ownershipConflict(ctx context.Context, p installPersist) error {
	owner, err := s.q.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: string(TypeSlack),
		AppID:       p.appID,
	})
	if err != nil {
		return ErrTeamOwnedByAnotherWorkspace
	}
	if owner.WorkspaceID == p.wsID {
		return ErrTeamOwnedByAnotherAgent
	}
	return ErrTeamOwnedByAnotherWorkspace
}

// ListByWorkspace returns every Slack installation in the workspace (active and
// revoked), for the management surface.
func (s *InstallService) ListByWorkspace(ctx context.Context, wsID pgtype.UUID) ([]db.ChannelInstallation, error) {
	return s.q.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: wsID,
		ChannelType: string(TypeSlack),
	})
}

// GetInWorkspace is the workspace-scoped lookup so a forged installation id from
// another workspace returns NotFound instead of leaking existence.
func (s *InstallService) GetInWorkspace(ctx context.Context, id, wsID pgtype.UUID) (db.ChannelInstallation, error) {
	inst, err := s.q.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID:          id,
		WorkspaceID: wsID,
		ChannelType: string(TypeSlack),
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
// (ListActiveInstallations filters to active), so its Socket Mode connection
// winds down, and outbound drops too.
func (s *InstallService) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.q.SetChannelInstallationStatus(ctx, db.SetChannelInstallationStatusParams{
		ID:     id,
		Status: "revoked",
	})
}
