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

	// BotDisplayName is the bot's name as it appears in a chat. Optional;
	// see Installation.BotDisplayName for why it exists and what an empty
	// value falls back to. Empty on a re-install of the SAME bot keeps
	// whatever is already on the row.
	BotDisplayName string
}

// InstallationService creates, refreshes and revokes wecom smart-bot
// installations through the shared channel_installation table. It requires
// a non-nil *secretbox.Box so a caller cannot accidentally fall back to
// plaintext storage — the same invariant lark.InstallationService enforces.
type InstallationService struct {
	store *Store
	tx    engine.TxStarter
	box   *secretbox.Box

	// probe proves the caller holds the bot's live credentials before the
	// reclaim below acts on them. Never nil: NewInstallationService defaults it
	// to the real handshake probe and refuses an explicit nil, and Upsert
	// refuses to run without one. Proof of control is a security invariant, so
	// there is no fail-open setting for it — a deployment that cannot reach
	// WeCom at install time gets a 503 and retries, it does not get to install
	// unverified credentials over somebody else's row.
	probe CredentialProbe
}

// InstallationOption configures the service at construction.
type InstallationOption func(*InstallationService)

// WithCredentialProbe overrides the control check. Production omits it and
// gets NewHandshakeProbe(nil, ""); tests pass a fake. Passing nil is an error
// at construction, not a way to turn the check off.
func WithCredentialProbe(p CredentialProbe) InstallationOption {
	return func(s *InstallationService) { s.probe = p }
}

// ErrProbeRequired is returned when a service would otherwise be built — or
// run — without a credential probe. Proof of control gates a statement that
// hard-deletes another workspace's installation and every binding under it, so
// a nil probe is a wiring bug, not a deployment mode. Fail closed at
// construction so the mistake surfaces at boot rather than on the first
// install.
var ErrProbeRequired = errors.New("wecom: InstallationService requires a non-nil CredentialProbe")

// NewInstallationService binds the service to a queries handle, a transaction
// starter (so the lock-read-probe-reclaim-upsert sequence runs atomically), and
// a secretbox keyed for at-rest encryption. Returns an error when the box is
// nil; callers should surface it (do not fall back to plaintext).
//
// The credential probe defaults to the real handshake against WeCom. Callers
// that must not open a socket (tests) pass WithCredentialProbe with a fake;
// there is no option that leaves the check off.
func NewInstallationService(queries *db.Queries, tx engine.TxStarter, box *secretbox.Box, opts ...InstallationOption) (*InstallationService, error) {
	if box == nil {
		return nil, errors.New("wecom: InstallationService requires a non-nil secretbox.Box")
	}
	svc := &InstallationService{
		store: NewStore(queries),
		tx:    tx,
		box:   box,
		probe: NewHandshakeProbe(nil, ""),
	}
	for _, o := range opts {
		o(svc)
	}
	if svc.probe == nil {
		return nil, ErrProbeRequired
	}
	return svc, nil
}

// Upsert creates or refreshes an installation row. The conflict key on
// channel_installation is (workspace_id, agent_id, channel_type), so
// re-running Upsert against an existing (workspace, agent, wecom) triple
// rotates every field on the row and flips status back to 'active'. The
// returned Installation reflects the post-write DB state.
//
// The whole sequence — lock the bot's routing slot, read its current owner,
// refuse or probe, reclaim, write — runs inside one transaction. Order is the
// point:
//
//   - idx_channel_installation_type_appid is UNIQUE on
//     (channel_type, config->>'app_id') with NO workspace in it, so a bot id's
//     routing slot is global across the deployment, and
//     ReclaimDeadChannelInstallationByAppID hard-deletes whoever holds it along
//     with every user binding, chat-session binding and pending token beneath.
//     Bot ids are not secret — every member talking to the bot can read one —
//     so the reclaim has to be gated on proof that the caller controls the bot.
//     That is the probe.
//
//   - But the probe is itself a side effect on the live platform: it subscribes,
//     and WeCom allows exactly one live subscriber per bot, so subscribing
//     displaces whoever is connected. Running it before knowing who owns the
//     slot means a request that is about to be REFUSED still knocks the rightful
//     owner offline — repeat it and it is a denial of service against a bot the
//     caller was never permitted to touch. So the owner read comes first, and a
//     request that cannot succeed returns its conflict having touched nothing,
//     locally or at WeCom.
//
//   - Both steps read state that a concurrent install or reconnect can change
//     underneath them, so they are serialized on the slot itself
//     (LockChannelInstallationAppIDSlot, a transaction-level advisory lock). A
//     pre-read outside the transaction would leave the TOCTOU window open.
func (s *InstallationService) Upsert(ctx context.Context, p InstallationParams) (Installation, error) {
	if err := validateInstallationParams(p); err != nil {
		return Installation{}, err
	}
	if s.probe == nil {
		// Belt and braces for a service built by struct literal rather than
		// NewInstallationService. Nothing may reach the reclaim unproven, so
		// this is checked before anything else that could fail first and hide
		// it.
		return Installation{}, ErrProbeRequired
	}
	if s.tx == nil {
		return Installation{}, errors.New("wecom: InstallationService requires a transaction starter")
	}

	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return Installation{}, fmt.Errorf("wecom: begin install tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.store.WithTx(tx)

	// The serialization boundary. Held until commit/rollback, so every step
	// below sees one consistent answer for who owns this bot.
	if err := qtx.Queries.LockChannelInstallationAppIDSlot(ctx, db.LockChannelInstallationAppIDSlotParams{
		ChannelType: channelTypeWecom,
		AppID:       p.BotID,
	}); err != nil {
		return Installation{}, fmt.Errorf("wecom: lock bot routing slot: %w", err)
	}

	// Who holds the slot right now — read inside the lock, before anything
	// external happens.
	if err := botSlotConflictErr(ctx, qtx, p); err != nil {
		return Installation{}, err
	}

	// Nothing live is at stake now: the slot is free, revoked, orphaned, or
	// already this caller's own. Prove control before the reclaim acts on it.
	if err := s.probe.Probe(ctx, p.BotID, p.Secret); err != nil {
		return Installation{}, err
	}

	sealed, err := s.box.Seal([]byte(p.Secret))
	if err != nil {
		return Installation{}, fmt.Errorf("wecom: encrypt secret: %w", err)
	}

	// Reclaim-then-upsert. UpsertChannelInstallation conflicts on
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
	// index below. botSlotConflictErr has already refused every live owner
	// above, so reaching that unique violation now means the slot changed hands
	// despite the lock; botOwnerConflictErr still turns it into an accurate 409
	// rather than a raw Postgres string. Mirrors slack/install.go's
	// persistInstall.
	if _, err := qtx.Queries.ReclaimDeadChannelInstallationByAppID(ctx, db.ReclaimDeadChannelInstallationByAppIDParams{
		ChannelType: channelTypeWecom,
		AppID:       p.BotID,
		WorkspaceID: p.WorkspaceID,
		AgentID:     p.AgentID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		// pgx.ErrNoRows just means nothing was dead — a no-op, not a failure.
		return Installation{}, fmt.Errorf("wecom: reclaim dead installation: %w", err)
	}

	// The row this upsert is about to overwrite, read on the tx handle so it is
	// serialized with the write. Zero when this is a first install.
	carried, err := currentInstallation(ctx, qtx.Queries, p.WorkspaceID, p.AgentID)
	if err != nil {
		return Installation{}, err
	}
	// The chat name is optional in the dialog, so an admin rotating a leaked
	// secret leaves it blank — and blanking it would put group slash commands
	// back to the whitespace guess that this field exists to replace. Keep what
	// is on the row. A bot SWAP is different: the old name belongs to the old
	// bot, and carrying it would make the new bot answer to a mention that is
	// not its own.
	displayName := p.BotDisplayName
	if displayName == "" && carried.BotID == p.BotID {
		displayName = carried.BotDisplayName
	}
	cfg, err := encodeInstallConfig(Installation{
		BotID:           p.BotID,
		SecretEncrypted: sealed,
		BotDisplayName:  displayName,
	})
	if err != nil {
		return Installation{}, err
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

// currentInstallation reads the (workspace, agent, wecom) row an upsert is
// about to replace, or the zero Installation when there is none.
//
// There is no query keyed on that triple — the conflict key of the upsert is
// not something any read path needed until now — so this filters the
// workspace's wecom rows, of which there are a handful. Running it on the
// caller's queries handle is what makes it serializable with the write.
func currentInstallation(ctx context.Context, q *db.Queries, workspaceID, agentID pgtype.UUID) (Installation, error) {
	rows, err := q.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: workspaceID,
		ChannelType: channelTypeWecom,
	})
	if err != nil {
		return Installation{}, fmt.Errorf("wecom: read current installation: %w", err)
	}
	for _, row := range rows {
		if row.AgentID == agentID {
			return installationFromRow(row)
		}
	}
	return Installation{}, nil
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

// botSlotConflictErr answers one question: may this install act on the
// (wecom, bot_id) routing slot at all? It returns the sentinel to refuse with,
// or nil to proceed. MUST be called inside the transaction holding
// LockChannelInstallationAppIDSlot — outside it the answer is a guess that a
// concurrent install or reconnect can invalidate before it is used.
//
// It is the gate in front of BOTH side effects Upsert can produce: the probe's
// subscribe (which displaces whoever is connected to this bot at WeCom) and the
// reclaim's hard delete (which takes the row and its bindings). A refusal here
// costs the caller nothing and the current owner nothing.
//
// The classification mirrors ReclaimDeadChannelInstallationByAppID's own
// definition of "dead", so the two cannot drift into disagreeing about which
// rows are takeable:
//
//	no row            → the slot is free.
//	orphan            → the workspace or agent row is gone; the reclaim frees
//	                    it and nothing is connected to it.
//	revoked           → the owner's explicit "I'm done with this bot"; the
//	                    reclaim takes it (or, if it is this caller's own row,
//	                    the upsert reactivates it in place).
//	active, ours      → a re-install or secret rotation of a bot this
//	                    (workspace, agent) already holds. Not a conflict:
//	                    the upsert refreshes it in place and the reclaim
//	                    spares it.
//	active, somebody  → refused. This is the case the whole gate exists for.
//	          else's
func botSlotConflictErr(ctx context.Context, qtx *Store, p InstallationParams) error {
	owner, err := qtx.Queries.GetChannelInstallationSlotOwnerByAppID(ctx, db.GetChannelInstallationSlotOwnerByAppIDParams{
		ChannelType: channelTypeWecom,
		AppID:       p.BotID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("wecom: read bot routing slot owner: %w", err)
	}
	switch {
	case !owner.WorkspaceExists || !owner.AgentExists:
		return nil
	case owner.Status == string(InstallationRevoked):
		return nil
	case owner.WorkspaceID == p.WorkspaceID && owner.AgentID == p.AgentID:
		return nil
	case owner.WorkspaceID != p.WorkspaceID:
		return ErrBotOwnedByAnotherWorkspace
	case owner.AgentArchivedAt.Valid:
		return ErrBotOwnedByArchivedAgent
	default:
		return ErrBotOwnedBySameWorkspace
	}
}

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

// ErrInvalidInstallationParams marks a request the caller can fix by filling
// something in, as opposed to a failure of ours. It exists so the HTTP layer
// can tell them apart: a missing field is the caller's 400, ErrCredentialsRejected
// and ErrCredentialsUnverifiable carry the two outcomes of actually asking WeCom
// (400 and 503), and everything left over is a 500 — because telling an admin
// their credentials are wrong when Postgres briefly went away sends them to
// rotate a secret that was fine, and a WeCom secret, once rotated, cannot be
// recovered. writeWecomInstallError holds the full matrix.
var ErrInvalidInstallationParams = errors.New("wecom: invalid installation parameters")

// validateInstallationParams is a lightweight pre-write check for
// required fields. It does NOT verify anything against WeCom.
func validateInstallationParams(p InstallationParams) error {
	missing := ""
	switch {
	case !p.WorkspaceID.Valid:
		missing = "workspace_id"
	case !p.AgentID.Valid:
		missing = "agent_id"
	case !p.InstallerUserID.Valid:
		missing = "installer_user_id"
	case p.BotID == "":
		missing = "bot_id"
	case p.Secret == "":
		missing = "secret"
	default:
		return nil
	}
	return fmt.Errorf("%w: %s is required", ErrInvalidInstallationParams, missing)
}
