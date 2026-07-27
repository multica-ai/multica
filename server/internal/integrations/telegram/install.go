package telegram

import (
	"context"
	"crypto/rand"
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

// This file is the Telegram install backend. Each workspace admin creates
// their own bot via BotFather and pastes its bot token into Multica: unlike
// Slack, there is no OAuth exchange and no separate app-level token — the
// bot token is validated live via getMe, encrypted at rest, and the
// installation is persisted BEFORE the Telegram webhook is registered so
// the row exists for the reclaim/conflict checks that key the webhook path.
// If setWebhook fails after persisting, the just-created row is revoked
// (best-effort) so we never leave an "active" install that silently
// receives nothing.

var (
	// ErrInstallationNotFound surfaces "no row matches in this workspace".
	ErrInstallationNotFound = errors.New("telegram installation not found")
	// ErrBotOwnedByAnotherWorkspace is returned when the pasted bot is already
	// connected to a live owner in a DIFFERENT Multica workspace — it would
	// collide with the (channel_type, app_id) routing index. A Telegram bot is
	// one identity and maps to one agent; reusing it here requires disconnecting
	// it in the other workspace first.
	ErrBotOwnedByAnotherWorkspace = errors.New("telegram: this bot is already connected to a different Multica workspace")
	// ErrBotOwnedBySameWorkspace is returned when the bot is already connected to
	// a DIFFERENT (live, non-archived) agent in the SAME workspace.
	ErrBotOwnedBySameWorkspace = errors.New("telegram: this bot is already connected to another agent in this workspace")
	// ErrBotOwnedByArchivedAgent is returned when the bot's owning agent is
	// archived (and so still holds the bot, since archiving is reversible). The
	// user recovers by restoring that agent or disconnecting its bot.
	ErrBotOwnedByArchivedAgent = errors.New("telegram: this bot is connected to an archived agent in this workspace")
)

// installQueries is the slice of generated queries InstallService needs. WithTx
// returns the same interface bound to a transaction so persistInstall runs its
// upsert atomically (and so tests can inject a fake without a real DB).
type installQueries interface {
	WithTx(tx pgx.Tx) installQueries
	UpsertChannelInstallation(ctx context.Context, arg db.UpsertChannelInstallationParams) (db.ChannelInstallation, error)
	ReclaimDeadChannelInstallationByAppID(ctx context.Context, arg db.ReclaimDeadChannelInstallationByAppIDParams) (pgtype.UUID, error)
	GetChannelInstallationOwnerByAppID(ctx context.Context, arg db.GetChannelInstallationOwnerByAppIDParams) (db.GetChannelInstallationOwnerByAppIDRow, error)
	ListChannelInstallationsByWorkspace(ctx context.Context, arg db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error)
	GetChannelInstallationInWorkspace(ctx context.Context, arg db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error)
	SetChannelInstallationStatus(ctx context.Context, arg db.SetChannelInstallationStatusParams) error
}

// dbInstallQueries adapts *db.Queries to installQueries — the generated WithTx
// returns *db.Queries, so we wrap it to return the interface (the same adapter
// pattern Slack's install service uses).
type dbInstallQueries struct{ *db.Queries }

func (q dbInstallQueries) WithTx(tx pgx.Tx) installQueries {
	return dbInstallQueries{q.Queries.WithTx(tx)}
}

// InstallService owns the at-rest encryption of the bot token (so no caller
// can write a channel_installation with a plaintext token), the shared
// install transaction, and Telegram webhook registration. The box MUST be
// non-nil (we refuse plaintext storage even in dev).
type InstallService struct {
	box       *secretbox.Box
	q         installQueries
	tx        engine.TxStarter
	publicURL string
	logger    *slog.Logger

	// apiBase overrides the Telegram API base for getMe/setWebhook/deleteWebhook
	// (tests point it at an httptest server). Empty uses the real Telegram API.
	apiBase string
}

// NewInstallService binds the service to queries, a tx starter (*pgxpool.Pool),
// an encryption box, and the public base URL Telegram's webhook is registered
// against (publicURL + "/api/webhooks/telegram/<bot_id>").
func NewInstallService(q *db.Queries, tx engine.TxStarter, box *secretbox.Box, publicURL string, logger *slog.Logger) (*InstallService, error) {
	if q == nil {
		return nil, errors.New("telegram: InstallService requires queries")
	}
	return newInstallService(dbInstallQueries{q}, tx, box, publicURL, logger)
}

// newInstallService is the testable core: it takes the installQueries interface
// so tests can inject a fake (with a fake TxStarter) without a real DB.
func newInstallService(q installQueries, tx engine.TxStarter, box *secretbox.Box, publicURL string, logger *slog.Logger) (*InstallService, error) {
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
		box:       box,
		q:         q,
		tx:        tx,
		publicURL: publicURL,
		logger:    logger,
	}, nil
}

// SetAPIBaseForTest points getMe/setWebhook/deleteWebhook at an httptest
// server instead of the real Telegram API. Test-only seam.
func (s *InstallService) SetAPIBaseForTest(base string) {
	s.apiBase = base
}

// clientOpts builds the Client options honoring the apiBase override.
func (s *InstallService) clientOpts() []ClientOption {
	if s.apiBase == "" {
		return nil
	}
	return []ClientOption{WithAPIBase(s.apiBase)}
}

// RegisterBYOParams are the inputs for a bring-your-own-bot install: the
// agent this bot represents, who is installing, and the token pasted from
// BotFather.
type RegisterBYOParams struct {
	WorkspaceID pgtype.UUID
	AgentID     pgtype.UUID
	InitiatorID pgtype.UUID
	BotToken    string // "<bot_id>:<secret>" from BotFather
}

// RegisterBYO installs a user-supplied Telegram bot for an agent. The user
// creates their own bot via BotFather and pastes its bot token; there is no
// OAuth exchange. We validate the token live via getMe (which also yields the
// bot's username), encrypt the token at rest, persist the installation, and
// THEN register the Telegram webhook. If setWebhook fails, the just-persisted
// row is revoked (best-effort) so we never leave an active install that
// silently never receives events.
func (s *InstallService) RegisterBYO(ctx context.Context, p RegisterBYOParams) (db.ChannelInstallation, error) {
	botToken := strings.TrimSpace(p.BotToken)
	botID, err := parseTelegramBotID(botToken)
	if err != nil {
		return db.ChannelInstallation{}, err
	}

	client := NewClient(botToken, s.clientOpts()...)

	// Validate the bot token live and learn the bot's username.
	info, err := client.GetMe(ctx)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("telegram getMe: %w", err)
	}

	webhookSecret, err := randomWebhookSecret(32)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("generate telegram webhook secret: %w", err)
	}

	sealed, err := s.box.Seal([]byte(botToken))
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encrypt telegram bot token: %w", err)
	}
	cfgJSON, err := json.Marshal(installConfig{
		AppID:             botID,
		BotUsername:       info.Username,
		BotTokenEncrypted: base64.StdEncoding.EncodeToString(sealed),
		WebhookSecret:     webhookSecret,
	})
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encode telegram installation config: %w", err)
	}

	// Persist one bot per agent (the row is keyed by workspace + agent). The
	// stored config carries the real bot id for inbound routing; persistInstall
	// refuses the pair if that bot is already connected to another agent/workspace.
	inst, err := s.persistInstall(ctx, installPersist{
		wsID:        p.WorkspaceID,
		agentID:     p.AgentID,
		installerID: p.InitiatorID,
		appIDKey:    botID,
		configJSON:  cfgJSON,
	})
	if err != nil {
		return db.ChannelInstallation{}, err
	}

	// Register the webhook only AFTER the row is persisted, since the URL is
	// keyed by botID (the row's routing key) — a failure here must not leave a
	// dangling active install, so best-effort revoke the row we just created.
	webhookURL := strings.TrimRight(s.publicURL, "/") + "/api/webhooks/telegram/" + botID
	if err := client.SetWebhook(ctx, webhookURL, webhookSecret); err != nil {
		if revokeErr := s.Revoke(ctx, inst); revokeErr != nil {
			s.logger.Error("telegram: failed to revoke installation after setWebhook failure",
				"installation_id", inst.ID, "error", revokeErr)
		}
		return db.ChannelInstallation{}, fmt.Errorf("telegram setWebhook: %w", err)
	}

	return inst, nil
}

// randomWebhookSecret generates n cryptographically random bytes, URL-safe
// base64 encoded, for use as the Telegram webhook's secret_token (verified on
// every inbound request via the X-Telegram-Bot-Api-Secret-Token header).
func randomWebhookSecret(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// installPersist carries the resolved fields persistInstall writes. appIDKey is
// the value stored at config->>'app_id' — the Telegram bot id — and MUST equal
// the app_id inside configJSON; it is the lookup / ON CONFLICT key.
type installPersist struct {
	wsID        pgtype.UUID
	agentID     pgtype.UUID
	installerID pgtype.UUID
	// appIDKey is the Telegram bot id stored at config->>'app_id'; it MUST equal
	// the app_id inside configJSON. It keys the dead-owner reclaim and the
	// live-owner lookup that drives the accurate conflict message.
	appIDKey string
	// configJSON holds the Telegram bot id (config->>'app_id') used for inbound
	// routing; the ROW itself is keyed by (workspace, agent) — one bot per agent.
	configJSON []byte
}

// pgUniqueViolation is the Postgres SQLSTATE for a unique-constraint violation.
const pgUniqueViolation = "23505"

// persistInstall upserts the installation keyed by (workspace_id, agent_id,
// channel_type): ONE Telegram bot per agent. Re-connecting an agent —
// including swapping it to a NEW bot after a disconnect — UPDATES that
// agent's row in place instead of colliding with the (workspace, agent,
// channel) unique.
//
// The (channel_type, app_id) routing index is the only OTHER unique
// constraint, and it is NOT this upsert's conflict target, so a unique
// violation here means the pasted bot is already connected to a DIFFERENT
// agent or Multica workspace — refuse it (ErrBotOwnedByAnotherWorkspace)
// rather than steal it.
func (s *InstallService) persistInstall(ctx context.Context, p installPersist) (db.ChannelInstallation, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("begin install tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	// Free the (telegram, app_id) routing slot from any DEAD prior owner — a
	// revoked placeholder, or an orphan whose owning workspace/agent was
	// deleted — before the upsert, so a bot whose old owner is gone can be
	// rebound. A live owner (active agent, including an archived one) is left
	// in place and trips the unique index below, which we turn into an
	// accurate conflict.
	if _, err := qtx.ReclaimDeadChannelInstallationByAppID(ctx, db.ReclaimDeadChannelInstallationByAppIDParams{
		ChannelType: string(TypeTelegram),
		AppID:       p.appIDKey,
		WorkspaceID: p.wsID,
		AgentID:     p.agentID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		// pgx.ErrNoRows just means nothing was dead — a no-op, not a failure.
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

// liveOwnerConflictErr classifies who holds the (telegram, app_id) routing
// slot after the dead-owner reclaim ran, so persistInstall returns a sentinel
// the handler renders as an accurate message. Read on the base pool (s.q),
// since the failed upsert has aborted the tx. A now-free slot (concurrent
// disconnect) or lookup error falls back to the generic cross-workspace
// sentinel — a retry then succeeds.
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

// ListByWorkspace returns every Telegram installation in the workspace
// (active and revoked), for the management surface.
func (s *InstallService) ListByWorkspace(ctx context.Context, wsID pgtype.UUID) ([]db.ChannelInstallation, error) {
	return s.q.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: wsID,
		ChannelType: string(TypeTelegram),
	})
}

// GetInWorkspace is the workspace-scoped lookup so a forged installation id from
// another workspace returns NotFound instead of leaking existence.
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

// Revoke flips status to 'revoked' and best-effort deletes the Telegram
// webhook using the decrypted bot token from the row's stored config, so
// Telegram stops delivering updates for a bot we no longer serve. The row is
// preserved for audit; a re-install flips it back to 'active'. Failure to
// delete the webhook is logged, not returned — the local status flip is the
// authoritative "stop serving this installation" signal.
func (s *InstallService) Revoke(ctx context.Context, inst db.ChannelInstallation) error {
	if err := s.q.SetChannelInstallationStatus(ctx, db.SetChannelInstallationStatusParams{
		ID:     inst.ID,
		Status: "revoked",
	}); err != nil {
		return err
	}

	creds, err := decodeCredentials(inst.Config, s.box.Open)
	if err != nil {
		s.logger.Error("telegram: failed to decode installation config for webhook deletion", "installation_id", inst.ID, "error", err)
		return nil
	}
	client := NewClient(creds.BotToken, s.clientOpts()...)
	if err := client.DeleteWebhook(ctx); err != nil {
		s.logger.Error("telegram: failed to delete webhook on revoke", "installation_id", inst.ID, "error", err)
	}
	return nil
}
