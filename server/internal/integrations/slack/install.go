package slack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
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
	// ErrTeamOwnedByAnotherWorkspace is returned when the Slack app is already
	// connected to a DIFFERENT Multica workspace. A Slack app stays bound to its
	// first Multica workspace: migrating it is an operator/support action
	// (revoking just sets status='revoked' and keeps the row + unique index), not
	// a silent re-install from the other workspace.
	ErrTeamOwnedByAnotherWorkspace = errors.New("slack: this Slack app is already connected to a different Multica workspace")
)

// installQueries is the slice of generated queries InstallService needs. WithTx
// returns the same interface bound to a transaction so persistInstall can run
// its lookup → upsert → binding cleanup → installer-bind atomically.
type installQueries interface {
	WithTx(tx pgx.Tx) installQueries
	GetChannelInstallationByAppID(ctx context.Context, arg db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error)
	UpsertChannelInstallationByAppID(ctx context.Context, arg db.UpsertChannelInstallationByAppIDParams) (db.ChannelInstallation, error)
	CreateChannelUserBinding(ctx context.Context, arg db.CreateChannelUserBindingParams) (db.ChannelUserBinding, error)
	DeleteChannelChatSessionBindingsByInstallation(ctx context.Context, arg db.DeleteChannelChatSessionBindingsByInstallationParams) error
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
	wsID             pgtype.UUID
	agentID          pgtype.UUID
	installerID      pgtype.UUID
	appIDKey         string
	configJSON       []byte
	installerSlackID string
}

// persistInstall runs the lookup → upsert → stale-binding retire → installer
// bind in ONE transaction. The cross-workspace guard is atomic in the upsert's
// WHERE clause: an app_id already owned by a DIFFERENT Multica workspace updates
// no row and returns pgx.ErrNoRows, which maps to ErrTeamOwnedByAnotherWorkspace.
func (s *InstallService) persistInstall(ctx context.Context, p installPersist) (db.ChannelInstallation, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("begin install tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	// Look up any existing installation under this app_id key. Drives ONLY the
	// agent-change cleanup below — NOT the cross-workspace guard (a plain SELECT
	// can't win the concurrent-install race; that guard is in the upsert's WHERE).
	existing, lookupErr := qtx.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: string(TypeSlack),
		AppID:       p.appIDKey,
	})
	hadExisting := lookupErr == nil
	if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
		return db.ChannelInstallation{}, fmt.Errorf("lookup existing slack installation: %w", lookupErr)
	}

	// app-id-keyed upsert: re-installing the same app — including to represent a
	// different agent in the SAME workspace — updates the existing row rather than
	// colliding with the (channel_type, app_id) index. Its ON CONFLICT update is
	// fenced to the same Multica workspace (the atomic cross-workspace guard).
	inst, err := qtx.UpsertChannelInstallationByAppID(ctx, db.UpsertChannelInstallationByAppIDParams{
		WorkspaceID:     p.wsID,
		AgentID:         p.agentID,
		ChannelType:     string(TypeSlack),
		Config:          p.configJSON,
		InstallerUserID: p.installerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ChannelInstallation{}, ErrTeamOwnedByAnotherWorkspace
		}
		return db.ChannelInstallation{}, fmt.Errorf("upsert slack installation: %w", err)
	}

	// Agent change within the same workspace: each existing chat_session is
	// permanently tied to the agent it was created under (session.go reuses a
	// session purely by (installation_id, channel_chat_id)), so without this a
	// moved bot's existing DMs / threads would keep routing to the OLD agent
	// (Elon review). Retire the stale chat-session bindings so the next inbound
	// message creates a fresh session under the new agent. User bindings stay
	// valid (same users, same workspace) and are intentionally kept.
	if hadExisting && existing.AgentID != p.agentID {
		if err := qtx.DeleteChannelChatSessionBindingsByInstallation(ctx, db.DeleteChannelChatSessionBindingsByInstallationParams{
			InstallationID: inst.ID,
			ChannelType:    string(TypeSlack),
		}); err != nil {
			return db.ChannelInstallation{}, fmt.Errorf("retire stale chat-session bindings: %w", err)
		}
	}

	// Auto-bind the installer to their Slack user id so their own first DM /
	// mention is not dropped as unbound — mirroring Feishu's installer auto-bind.
	// Skipped when installerSlackID is empty. An id already bound to a DIFFERENT
	// Multica user is a benign skip (the gated upsert returns no rows); a real DB
	// error poisons the tx and must abort the whole install.
	if p.installerSlackID != "" {
		if _, err := qtx.CreateChannelUserBinding(ctx, db.CreateChannelUserBindingParams{
			WorkspaceID:    p.wsID,
			MulticaUserID:  p.installerID,
			InstallationID: inst.ID,
			ChannelType:    string(TypeSlack),
			ChannelUserID:  p.installerSlackID,
			Config:         []byte(`{}`),
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				s.logger.WarnContext(ctx, "slack: installer already bound to a different user; skipping auto-bind",
					"installation_id", util.UUIDToString(inst.ID))
			} else {
				return db.ChannelInstallation{}, fmt.Errorf("bind installer: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("commit slack install: %w", err)
	}
	return inst, nil
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
