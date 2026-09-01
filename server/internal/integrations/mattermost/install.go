package mattermost

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

// This file is the Mattermost install backend, mirroring slack/install.go and
// telegram/install.go: the workspace admin creates a bot account in the
// Mattermost System Console and pastes its server URL and access token. The
// InstallService owns the at-rest encryption of the token — no caller can
// write a channel_installation with a plaintext token — plus the shared
// persist transaction and the list / get / revoke management surface.

var (
	// ErrInstallationNotFound surfaces "no row matches in this workspace".
	ErrInstallationNotFound = errors.New("mattermost installation not found")
	// ErrCredentialsRejected means Mattermost itself rejected the token. Keep
	// it distinct from an unreachable server so users are never told to rotate
	// a valid credential because the network is down.
	ErrCredentialsRejected = errors.New("mattermost: the server rejected this access token")
	// ErrCredentialsUnverifiable means Multica could not complete the live
	// check. Nothing has been persisted or changed.
	ErrCredentialsUnverifiable = errors.New("mattermost: could not reach this Mattermost server to verify the bot")
	// ErrNotABotAccount means the token authenticates a human user rather than
	// a bot account. Personal tokens would work technically, but the account's
	// own messages would then loop back in as inbound.
	ErrNotABotAccount = errors.New("mattermost: this token belongs to a user account, not a bot account")
	// ErrBotOwnedByAnotherWorkspace: the bot is already connected to a live
	// owner in a DIFFERENT Multica workspace.
	ErrBotOwnedByAnotherWorkspace = errors.New("mattermost: this bot is already connected to a different Multica workspace")
	// ErrBotOwnedBySameWorkspace: the bot is already connected to a different
	// live agent in the SAME workspace.
	ErrBotOwnedBySameWorkspace = errors.New("mattermost: this bot is already connected to another agent in this workspace")
	// ErrBotOwnedByArchivedAgent: the bot's owning agent is archived.
	ErrBotOwnedByArchivedAgent = errors.New("mattermost: this bot is connected to an archived agent in this workspace")
)

// installQueries is the slice of generated queries InstallService needs,
// interface-shaped so tests inject a fake (same adapter pattern as Slack).
type installQueries interface {
	WithTx(tx pgx.Tx) installQueries
	UpsertChannelInstallation(ctx context.Context, arg db.UpsertChannelInstallationParams) (db.ChannelInstallation, error)
	ReclaimDeadChannelInstallationByAppID(ctx context.Context, arg db.ReclaimDeadChannelInstallationByAppIDParams) (pgtype.UUID, error)
	GetChannelInstallationOwnerByAppID(ctx context.Context, arg db.GetChannelInstallationOwnerByAppIDParams) (db.GetChannelInstallationOwnerByAppIDRow, error)
	ListChannelInstallationsByWorkspace(ctx context.Context, arg db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error)
	GetChannelInstallationInWorkspace(ctx context.Context, arg db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error)
	SetChannelInstallationStatus(ctx context.Context, arg db.SetChannelInstallationStatusParams) error
}

type dbInstallQueries struct{ *db.Queries }

func (q dbInstallQueries) WithTx(tx pgx.Tx) installQueries {
	return dbInstallQueries{q.Queries.WithTx(tx)}
}

// InstallService owns the at-rest encryption of the access token and the
// install transaction. The box MUST be non-nil (we refuse plaintext storage
// even in dev).
type InstallService struct {
	box        *secretbox.Box
	q          installQueries
	tx         engine.TxStarter
	httpClient *http.Client
	logger     *slog.Logger
}

// NewInstallService binds the service to queries, a tx starter, and an
// encryption box.
func NewInstallService(q *db.Queries, tx engine.TxStarter, box *secretbox.Box, logger *slog.Logger) (*InstallService, error) {
	if q == nil {
		return nil, errors.New("mattermost: InstallService requires queries")
	}
	return newInstallService(dbInstallQueries{q}, tx, box, logger)
}

func newInstallService(q installQueries, tx engine.TxStarter, box *secretbox.Box, logger *slog.Logger) (*InstallService, error) {
	if box == nil {
		return nil, errors.New("mattermost: InstallService requires a non-nil secretbox.Box")
	}
	if q == nil {
		return nil, errors.New("mattermost: InstallService requires queries")
	}
	if tx == nil {
		return nil, errors.New("mattermost: InstallService requires a tx starter")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &InstallService{
		box:        box,
		q:          q,
		tx:         tx,
		httpClient: newHTTPClient(defaultTimeout),
		logger:     logger,
	}, nil
}

// RegisterParams are the inputs for a bot install: the agent this bot
// represents, who is installing, and the pasted server URL plus access token.
type RegisterParams struct {
	WorkspaceID pgtype.UUID
	AgentID     pgtype.UUID
	InitiatorID pgtype.UUID
	ServerURL   string
	AccessToken string
}

// Register installs a user-supplied Mattermost bot for an agent: canonicalize
// the server URL, validate the token live via GET /users/me (which also yields
// the bot user id and the username @-mention detection needs), encrypt the
// token at rest, and persist the installation keyed by (workspace, agent) with
// the composed server+bot key in the routing slot.
func (s *InstallService) Register(ctx context.Context, p RegisterParams) (db.ChannelInstallation, error) {
	serverURL, err := canonicalServerURL(p.ServerURL)
	if err != nil {
		return db.ChannelInstallation{}, err
	}
	token, err := validateAccessToken(p.AccessToken)
	if err != nil {
		return db.ChannelInstallation{}, err
	}

	me, err := newRESTClient(serverURL, token, s.httpClient).GetMe(ctx)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("mattermost getMe: %w", classifyCredentialVerificationError(err))
	}
	if me.ID == "" {
		return db.ChannelInstallation{}, fmt.Errorf("%w: response carried no user id", ErrCredentialsRejected)
	}
	if !me.IsBot {
		return db.ChannelInstallation{}, ErrNotABotAccount
	}
	if me.Username == "" {
		return db.ChannelInstallation{}, fmt.Errorf("%w: bot account has no username", ErrCredentialsRejected)
	}

	sealed, err := s.box.Seal([]byte(token))
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encrypt mattermost access token: %w", err)
	}
	appID := installationKey(serverURL, me.ID)
	cfgJSON, err := json.Marshal(installConfig{
		AppID:                appID,
		ServerURL:            serverURL,
		BotUserID:            me.ID,
		BotUsername:          me.Username,
		AccessTokenEncrypted: base64.StdEncoding.EncodeToString(sealed),
	})
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encode mattermost installation config: %w", err)
	}
	return s.persistInstall(ctx, installPersist{
		wsID:        p.WorkspaceID,
		agentID:     p.AgentID,
		installerID: p.InitiatorID,
		appIDKey:    appID,
		configJSON:  cfgJSON,
	})
}

// classifyCredentialVerificationError separates the server's authoritative
// credential rejection from failures where no verdict was obtained, so the
// operator is told the right next action. A 401 or 403 is the credential; a
// timeout, DNS failure, refused redirect or 5xx is the deployment.
func classifyCredentialVerificationError(err error) error {
	switch statusOf(err) {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: %v", ErrCredentialsRejected, err)
	default:
		return fmt.Errorf("%w: %v", ErrCredentialsUnverifiable, err)
	}
}

// installPersist carries the resolved fields persistInstall writes.
type installPersist struct {
	wsID        pgtype.UUID
	agentID     pgtype.UUID
	installerID pgtype.UUID
	appIDKey    string
	configJSON  []byte
}

const pgUniqueViolation = "23505"

// persistInstall upserts the installation keyed by (workspace_id, agent_id,
// channel_type): ONE Mattermost bot per agent. A unique violation on the
// (channel_type, app_id) routing index means the pasted bot is already
// connected to a different live agent or workspace — refuse rather than steal,
// with the accurate conflict message (same policy as Slack #4810).
func (s *InstallService) persistInstall(ctx context.Context, p installPersist) (db.ChannelInstallation, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("begin install tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	// Free the routing slot from any DEAD prior owner before the upsert.
	if _, err := qtx.ReclaimDeadChannelInstallationByAppID(ctx, db.ReclaimDeadChannelInstallationByAppIDParams{
		ChannelType: string(TypeMattermost),
		AppID:       p.appIDKey,
		WorkspaceID: p.wsID,
		AgentID:     p.agentID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return db.ChannelInstallation{}, fmt.Errorf("reclaim dead mattermost installation: %w", err)
	}

	inst, err := qtx.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{
		WorkspaceID:     p.wsID,
		AgentID:         p.agentID,
		ChannelType:     string(TypeMattermost),
		Config:          p.configJSON,
		InstallerUserID: p.installerID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return db.ChannelInstallation{}, s.liveOwnerConflictErr(ctx, p.wsID, p.appIDKey)
		}
		return db.ChannelInstallation{}, fmt.Errorf("upsert mattermost installation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("commit mattermost install: %w", err)
	}
	return inst, nil
}

// liveOwnerConflictErr classifies who holds the routing slot so the handler
// renders an accurate message.
func (s *InstallService) liveOwnerConflictErr(ctx context.Context, requestingWorkspaceID pgtype.UUID, appID string) error {
	owner, err := s.q.GetChannelInstallationOwnerByAppID(ctx, db.GetChannelInstallationOwnerByAppIDParams{
		ChannelType: string(TypeMattermost),
		AppID:       appID,
	})
	if err != nil {
		return ErrBotOwnedByAnotherWorkspace
	}
	switch {
	case owner.WorkspaceID != requestingWorkspaceID:
		return ErrBotOwnedByAnotherWorkspace
	case owner.AgentArchivedAt.Valid:
		return ErrBotOwnedByArchivedAgent
	default:
		return ErrBotOwnedBySameWorkspace
	}
}

// ListByWorkspace returns every Mattermost installation in the workspace, for
// the management surface.
func (s *InstallService) ListByWorkspace(ctx context.Context, wsID pgtype.UUID) ([]db.ChannelInstallation, error) {
	return s.q.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: wsID,
		ChannelType: string(TypeMattermost),
	})
}

// GetInWorkspace is the workspace-scoped lookup so a forged installation id
// from another workspace returns NotFound instead of leaking existence.
func (s *InstallService) GetInWorkspace(ctx context.Context, id, wsID pgtype.UUID) (db.ChannelInstallation, error) {
	inst, err := s.q.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID:          id,
		WorkspaceID: wsID,
		ChannelType: string(TypeMattermost),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ChannelInstallation{}, ErrInstallationNotFound
		}
		return db.ChannelInstallation{}, err
	}
	return inst, nil
}

// Revoke flips status to 'revoked'. The row is preserved for audit; existing
// chat sessions stay in Multica. The Supervisor stops supervising the
// installation, so its WebSocket connection winds down and outbound drops too.
func (s *InstallService) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.q.SetChannelInstallationStatus(ctx, db.SetChannelInstallationStatusParams{
		ID:     id,
		Status: "revoked",
	})
}
