package wecom

// installation.go — the write surface for wecom channel_installation rows.
// It centralises secretbox encryption of the smart-bot secret so no caller
// ever handles plaintext beyond this file's boundary, and it is the ONLY
// path to a wecom row in channel_installation — an admin CLI or an HTTP
// install endpoint both go through Upsert.

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// InstallationParams is the plaintext-bearing input to InstallationService.
// The caller supplies the raw (BotID, Secret) pair from the WeCom
// admin console; the service seals Secret before it touches the DB.
type InstallationParams struct {
	WorkspaceID     pgtype.UUID
	AgentID         pgtype.UUID
	InstallerUserID pgtype.UUID

	// BotID is the smart-bot identifier shown on the WeCom admin
	// console. Stable per-bot; used as both auth identity in the subscribe
	// frame and the routing key persisted at config->>'app_id'.
	BotID string

	// Secret is the plaintext long-connection secret shown once at bot
	// creation on the admin console. Sealed at the service boundary.
	Secret string
}

// InstallationService creates, refreshes and revokes wecom smart-bot
// installations through the shared channel_installation table. It requires
// a non-nil *secretbox.Box so a caller cannot accidentally fall back to
// plaintext storage — the same invariant lark.InstallationService enforces.
type InstallationService struct {
	store *Store
	tx    engine.TxStarter
	box   *secretbox.Box
}

// NewInstallationService binds the service to a queries handle, a transaction
// starter (so the reclaim-then-upsert runs atomically), and a secretbox keyed
// for at-rest encryption. Returns an error when the box is nil; callers should
// surface it (do not fall back to plaintext).
func NewInstallationService(queries *db.Queries, tx engine.TxStarter, box *secretbox.Box) (*InstallationService, error) {
	if box == nil {
		return nil, errors.New("wecom: InstallationService requires a non-nil secretbox.Box")
	}
	return &InstallationService{store: NewStore(queries), tx: tx, box: box}, nil
}

// Upsert creates or refreshes an installation row. The conflict key on
// channel_installation is (workspace_id, agent_id, channel_type), so
// re-running Upsert against an existing (workspace, agent, wecom) triple
// rotates every field on the row and flips status back to 'active'. The
// returned Installation reflects the post-write DB state.
func (s *InstallationService) Upsert(ctx context.Context, p InstallationParams) (Installation, error) {
	if err := validateInstallationParams(p); err != nil {
		return Installation{}, err
	}
	sealed, err := s.box.Seal([]byte(p.Secret))
	if err != nil {
		return Installation{}, fmt.Errorf("wecom: encrypt secret: %w", err)
	}
	cfg, err := encodeInstallConfig(Installation{
		BotID:           p.BotID,
		SecretEncrypted: sealed,
	})
	if err != nil {
		return Installation{}, err
	}

	// Reclaim-then-upsert, atomically. UpsertChannelInstallation conflicts on
	// (workspace_id, agent_id, channel_type), but the (channel_type, app_id)
	// slot is guarded by idx_channel_installation_type_appid. Disconnect only
	// flips status to 'revoked' — it does not free the row — so without a
	// reclaim step a bot revoked from agent A can never be connected to agent
	// B: the upsert misses its ON CONFLICT and trips the unique index, and the
	// admin is told to "disconnect it there first" for a bot that is already
	// disconnected and has no UI control to free it. ReclaimDeadChannelInstalla-
	// tionByAppID deletes any DEAD owner of this bot's slot (a revoked row held
	// by a DIFFERENT (workspace, agent), or an orphan whose workspace/agent was
	// deleted) and clears its dependent rows in the same statement, while
	// leaving a LIVE owner (active or archived agent) in place to trip the
	// index below — which botOwnerConflictErr turns into an accurate 409.
	// Mirrors slack/install.go's persistInstall.
	if s.tx == nil {
		return Installation{}, errors.New("wecom: InstallationService requires a transaction starter")
	}
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return Installation{}, fmt.Errorf("wecom: begin install tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.store.WithTx(tx)

	if _, err := qtx.Queries.ReclaimDeadChannelInstallationByAppID(ctx, db.ReclaimDeadChannelInstallationByAppIDParams{
		ChannelType: channelTypeWecom,
		AppID:       p.BotID,
		WorkspaceID: p.WorkspaceID,
		AgentID:     p.AgentID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		// pgx.ErrNoRows just means nothing was dead — a no-op, not a failure.
		return Installation{}, fmt.Errorf("wecom: reclaim dead installation: %w", err)
	}

	row, err := qtx.Queries.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{
		WorkspaceID:     p.WorkspaceID,
		AgentID:         p.AgentID,
		ChannelType:     channelTypeWecom,
		Config:          cfg,
		InstallerUserID: p.InstallerUserID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			// A LIVE owner still holds the slot. Read the owner on the
			// non-tx connection (this tx is now in aborted state) to name it.
			return Installation{}, s.botOwnerConflictErr(ctx, p.WorkspaceID, p.BotID)
		}
		return Installation{}, fmt.Errorf("wecom: upsert installation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Installation{}, fmt.Errorf("wecom: commit install tx: %w", err)
	}
	return installationFromRow(row)
}

// Sentinels for the one conflict Upsert cannot resolve: the bot is already
// connected somewhere else. UpsertChannelInstallation conflicts on
// (workspace_id, agent_id, channel_type), but idx_channel_installation_type_appid
// is UNIQUE on (channel_type, config->>'app_id') — so connecting the SAME bot to
// a second agent misses the ON CONFLICT clause entirely and trips the index
// instead. Without these the admin reads the raw Postgres text
// ("duplicate key value violates unique constraint …") in a toast.
//
// One bot is one connection: the WeCom long connection allows a single
// live subscriber per bot, so two agents cannot share one. The way out is
// always to free the bot first, which is what each message says.
var (
	// ErrBotOwnedBySameWorkspace — another agent in the admin's own workspace
	// holds the bot. Reversible from the same settings screen.
	ErrBotOwnedBySameWorkspace = errors.New("wecom: this bot is already connected to another agent in this workspace")

	// ErrBotOwnedByArchivedAgent — the holder is archived, so it does not show
	// up in the agent list and the bot looks free while it is not.
	ErrBotOwnedByArchivedAgent = errors.New("wecom: this bot is connected to an archived agent in this workspace")

	// ErrBotOwnedByAnotherWorkspace — the holder is out of sight entirely and
	// only someone with access there can release it.
	ErrBotOwnedByAnotherWorkspace = errors.New("wecom: this bot is already connected to a different Multica workspace")
)

// pgUniqueViolation is Postgres' unique_violation SQLSTATE.
const pgUniqueViolation = "23505"

// botOwnerConflictErr names who holds the (wecom, bot_id) routing slot so the
// handler can tell the admin where to go, mirroring slack's owner-conflict
// classification. Read after the upsert failed, so a slot that has since been
// freed — or a lookup that fails — falls back to the cross-workspace message:
// it is the only one that is never wrong about where to look, and a retry
// succeeds anyway.
func (s *InstallationService) botOwnerConflictErr(ctx context.Context, requestingWorkspaceID pgtype.UUID, botID string) error {
	owner, err := s.store.Queries.GetChannelInstallationOwnerByAppID(ctx, db.GetChannelInstallationOwnerByAppIDParams{
		ChannelType: channelTypeWecom,
		AppID:       botID,
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

// Revoke flips status to 'revoked' — the row is preserved so audit trails
// remain queryable, and a subsequent Upsert flips it back to 'active'
// atomically. A revoked row is skipped by the router's installation resolver
// (Active=false → invalid_event drop with audit).
func (s *InstallationService) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.store.Queries.SetChannelInstallationStatus(ctx, db.SetChannelInstallationStatusParams{
		ID:     id,
		Status: string(InstallationRevoked),
	})
}

// ErrInstallationNotFound is returned by GetInWorkspace when either no row
// exists at the given (id, workspace) or the row belongs to a different
// channel_type. It is distinct from a plain pgx.ErrNoRows so HTTP handlers
// can map it to 404 without importing pgx.
var ErrInstallationNotFound = errors.New("wecom: installation not found")

// ListByWorkspace returns every wecom installation for the given workspace
// in creation order. Used by the Settings and Agent-Integrations tabs to
// render "connected bots" lists; revoked rows are included so operators can
// see history (the UI filters on Status).
func (s *InstallationService) ListByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]Installation, error) {
	rows, err := s.store.Queries.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: workspaceID,
		ChannelType: channelTypeWecom,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Installation, 0, len(rows))
	for _, row := range rows {
		inst, err := installationFromRow(row)
		if err != nil {
			return nil, fmt.Errorf("wecom: decode installation %s: %w", row.ID.String(), err)
		}
		out = append(out, inst)
	}
	return out, nil
}

// GetInWorkspace loads one installation scoped to (id, workspace_id) so a
// forged UUID from another workspace returns not-found instead of leaking
// existence. Returns ErrInstallationNotFound on either missing row or a
// row that exists but belongs to another channel_type.
func (s *InstallationService) GetInWorkspace(ctx context.Context, id, workspaceID pgtype.UUID) (Installation, error) {
	row, err := s.store.Queries.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID:          id,
		WorkspaceID: workspaceID,
		ChannelType: channelTypeWecom,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Installation{}, ErrInstallationNotFound
		}
		return Installation{}, err
	}
	return installationFromRow(row)
}

// validateInstallationParams is a lightweight pre-write check for
// required fields. It does NOT verify anything against WeCom.
func validateInstallationParams(p InstallationParams) error {
	if !p.WorkspaceID.Valid {
		return errors.New("wecom: workspace_id is required")
	}
	if !p.AgentID.Valid {
		return errors.New("wecom: agent_id is required")
	}
	if !p.InstallerUserID.Valid {
		return errors.New("wecom: installer_user_id is required")
	}
	if p.BotID == "" {
		return errors.New("wecom: bot_id is required")
	}
	if p.Secret == "" {
		return errors.New("wecom: secret is required")
	}
	return nil
}
