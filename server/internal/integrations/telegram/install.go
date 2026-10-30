package telegram

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file is the Telegram install backend, mirroring slack/install.go +
// byo_install.go: the workspace admin creates a bot via @BotFather and pastes
// its token into Multica. The InstallService owns the at-rest encryption of
// the token — no caller can write a channel_installation with a plaintext
// token — plus the shared persist transaction and the list / get / revoke
// management surface.

var (
	// ErrInstallationNotFound surfaces "no row matches in this workspace".
	ErrInstallationNotFound = errors.New("telegram installation not found")
	// ErrInvalidBotToken is returned when the pasted token is malformed
	// ("<numeric id>:<secret>") — mapped to 400 by the handler.
	ErrInvalidBotToken = errors.New("telegram: bot token must look like 123456:ABC-DEF…")
	// ErrCredentialsRejected means Telegram itself rejected the token. Keep it
	// distinct from an unreachable API so users are never told to rotate a
	// valid credential because the deployment's proxy or network is down.
	ErrCredentialsRejected = errors.New("telegram: Telegram rejected this bot token")
	// ErrCredentialsUnverifiable means Multica could not complete the live
	// Telegram check. The token has not been persisted or changed.
	ErrCredentialsUnverifiable = errors.New("telegram: could not reach Telegram to verify this bot")
	// ErrBotOwnedByAnotherWorkspace: the pasted bot is already connected to a
	// live owner in a DIFFERENT Multica workspace.
	ErrBotOwnedByAnotherWorkspace = errors.New("telegram: this bot is already connected to a different Multica workspace")
	// ErrBotOwnedBySameWorkspace: the bot is already connected to a different
	// live agent in the SAME workspace.
	ErrBotOwnedBySameWorkspace = errors.New("telegram: this bot is already connected to another agent in this workspace")
	// ErrBotOwnedByArchivedAgent: the bot's owning agent is archived.
	ErrBotOwnedByArchivedAgent = errors.New("telegram: this bot is connected to an archived agent in this workspace")
	// ErrWebhookConfigured means the bot is currently managed by an outgoing
	// webhook. Long polling and webhooks are mutually exclusive, so do not
	// silently delete another integration's webhook during installation.
	ErrWebhookConfigured = errors.New("telegram: bot has an outgoing webhook configured")
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

// InstallService owns the at-rest encryption of the bot token and the install
// transaction. The box MUST be non-nil (we refuse plaintext storage even in
// dev).
type InstallService struct {
	box        *secretbox.Box
	q          installQueries
	tx         engine.TxStarter
	httpClient *http.Client
	logger     *slog.Logger

	// apiBase overrides the Bot API host for the getMe validation call (tests
	// point it at an httptest server). Empty uses the real API.
	apiBase string
}

// NewInstallService binds the service to queries, a tx starter, and an
// encryption box.
func NewInstallService(q *db.Queries, tx engine.TxStarter, box *secretbox.Box, logger *slog.Logger) (*InstallService, error) {
	if q == nil {
		return nil, errors.New("telegram: InstallService requires queries")
	}
	return newInstallService(dbInstallQueries{q}, tx, box, logger)
}

func newInstallService(q installQueries, tx engine.TxStarter, box *secretbox.Box, logger *slog.Logger) (*InstallService, error) {
	if box == nil {
		return nil, errors.New("telegram: InstallService requires a non-nil secretbox.Box")
	}
	if q == nil {
		return nil, errors.New("telegram: InstallService requires queries")
	}
	if tx == nil {
		return nil, errors.New("telegram: InstallService requires a tx starter")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &InstallService{
		box:        box,
		q:          q,
		tx:         tx,
		httpClient: newCredentialVerificationClient(),
		logger:     logger,
	}, nil
}

// RegisterParams are the inputs for a bot install: the agent this bot
// represents, who is installing, and the pasted BotFather token.
type RegisterParams struct {
	WorkspaceID pgtype.UUID
	AgentID     pgtype.UUID
	InitiatorID pgtype.UUID
	BotToken    string
}

// Register installs a user-supplied Telegram bot for an agent: validate the
// token live via getMe (which also yields the bot's username for @-mention
// detection), encrypt the token at rest, and persist the installation keyed
// by (workspace, agent) with the bot id in the routing slot.
func (s *InstallService) Register(ctx context.Context, p RegisterParams) (db.ChannelInstallation, error) {
	token := strings.TrimSpace(p.BotToken)
	botID, err := parseBotID(token)
	if err != nil {
		return db.ChannelInstallation{}, err
	}
	api := newBotAPI(s.apiBase, token, s.httpClient)
	me, err := api.GetMe(ctx)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("telegram getMe: %w", classifyCredentialVerificationError(err))
	}
	if !me.IsBot || me.Username == "" {
		return db.ChannelInstallation{}, fmt.Errorf("%w: response is not a bot with a username", ErrCredentialsRejected)
	}
	webhook, err := api.GetWebhookInfo(ctx)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("telegram getWebhookInfo: %w", classifyCredentialVerificationError(err))
	}
	if webhook.URL != "" {
		return db.ChannelInstallation{}, ErrWebhookConfigured
	}

	sealed, err := s.box.Seal([]byte(token))
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encrypt telegram bot token: %w", err)
	}
	cfgJSON, err := json.Marshal(installConfig{
		AppID:             botID,
		BotUsername:       me.Username,
		BotTokenEncrypted: base64.StdEncoding.EncodeToString(sealed),
	})
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encode telegram installation config: %w", err)
	}
	return s.persistInstall(ctx, installPersist{
		wsID:        p.WorkspaceID,
		agentID:     p.AgentID,
		installerID: p.InitiatorID,
		appIDKey:    botID,
		configJSON:  cfgJSON,
	})
}

// classifyCredentialVerificationError separates Telegram's authoritative
// credential rejection from failures where no verdict was obtained. getMe and
// getWebhookInfo use the same classifier so a proxy outage at either step has
// the same user-facing next action.
func classifyCredentialVerificationError(err error) error {
	var apiErr *apiError
	if errors.As(err, &apiErr) && apiErr.Code == http.StatusUnauthorized {
		return fmt.Errorf("%w: %v", ErrCredentialsRejected, err)
	}
	return fmt.Errorf("%w: %v", ErrCredentialsUnverifiable, err)
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

const credentialVerificationTimeout = 15 * time.Second

func newCredentialVerificationClient() *http.Client {
	return &http.Client{Timeout: credentialVerificationTimeout}
}

// persistInstall upserts the installation keyed by (workspace_id, agent_id,
// channel_type): ONE Telegram bot per agent. A unique violation on the
// (channel_type, app_id) routing index means the pasted bot is already
// connected to a different live agent or workspace — refuse rather than
// steal, with the accurate conflict message (same policy as Slack #4810).
func (s *InstallService) persistInstall(ctx context.Context, p installPersist) (db.ChannelInstallation, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("begin install tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	// Free the routing slot from any DEAD prior owner before the upsert.
	if _, err := qtx.ReclaimDeadChannelInstallationByAppID(ctx, db.ReclaimDeadChannelInstallationByAppIDParams{
		ChannelType: string(TypeTelegram),
		AppID:       p.appIDKey,
		WorkspaceID: p.wsID,
		AgentID:     p.agentID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return db.ChannelInstallation{}, fmt.Errorf("reclaim dead telegram installation: %w", err)
	}

	inst, err := qtx.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{
		WorkspaceID:     p.wsID,
		AgentID:         p.agentID,
		ChannelType:     string(TypeTelegram),
		Config:          p.configJSON,
		InstallerUserID: p.installerID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return db.ChannelInstallation{}, s.liveOwnerConflictErr(ctx, p.wsID, p.appIDKey)
		}
		return db.ChannelInstallation{}, fmt.Errorf("upsert telegram installation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("commit telegram install: %w", err)
	}
	return inst, nil
}

// liveOwnerConflictErr classifies who holds the routing slot so the handler
// renders an accurate message.
func (s *InstallService) liveOwnerConflictErr(ctx context.Context, requestingWorkspaceID pgtype.UUID, appID string) error {
	owner, err := s.q.GetChannelInstallationOwnerByAppID(ctx, db.GetChannelInstallationOwnerByAppIDParams{
		ChannelType: string(TypeTelegram),
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

// ListByWorkspace returns every Telegram installation in the workspace, for
// the management surface.
func (s *InstallService) ListByWorkspace(ctx context.Context, wsID pgtype.UUID) ([]db.ChannelInstallation, error) {
	return s.q.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: wsID,
		ChannelType: string(TypeTelegram),
	})
}

// GetInWorkspace is the workspace-scoped lookup so a forged installation id
// from another workspace returns NotFound instead of leaking existence.
func (s *InstallService) GetInWorkspace(ctx context.Context, id, wsID pgtype.UUID) (db.ChannelInstallation, error) {
	inst, err := s.q.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID:          id,
		WorkspaceID: wsID,
		ChannelType: string(TypeTelegram),
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
// installation, so its polling loop winds down and outbound drops too.
func (s *InstallService) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.q.SetChannelInstallationStatus(ctx, db.SetChannelInstallationStatusParams{
		ID:     id,
		Status: "revoked",
	})
}
