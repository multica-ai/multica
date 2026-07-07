package dingtalk

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

// InstallationParams is the input shape RegistrationService assembles
// after a successful device-flow scan-to-install. The credentials are
// supplied as plaintext — encryption happens inside
// InstallationService.Upsert via the supplied *secretbox.Box, so callers
// never see (and therefore cannot leak) the ciphertext that lands in
// the DB.
type InstallationParams struct {
	WorkspaceID     pgtype.UUID
	AgentID         pgtype.UUID
	ClientID        string
	ClientSecret    string // plaintext; encrypted at the service boundary
	InstallerUserID pgtype.UUID
}

var (
	// ErrAppOwnedByAnotherWorkspace is returned when the scanned DingTalk
	// app (its client_id) is already installed in a DIFFERENT Multica
	// workspace — it would collide with the (channel_type, app_id) routing
	// index. Reusing it here requires disconnecting it there first.
	ErrAppOwnedByAnotherWorkspace = errors.New("dingtalk: this DingTalk app is already connected to another Multica workspace")

	// ErrAgentAlreadyConnected is returned when re-pointing the app at a
	// new agent would collide with a DIFFERENT DingTalk installation that
	// agent already holds. Disconnect the agent's existing bot first.
	ErrAgentAlreadyConnected = errors.New("dingtalk: the selected agent already has a different DingTalk bot installed")
)

// pgUniqueViolation is the Postgres SQLSTATE for a unique-constraint violation.
const pgUniqueViolation = "23505"

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

// installQueries is the slice of generated queries the Upsert flow needs.
// WithTx returns the same interface bound to a transaction so the
// move-agent path (upsert + chat-session retire) commits atomically — and
// so tests can inject a fake without a real DB (the slack.InstallService
// adapter pattern).
type installQueries interface {
	WithTx(tx pgx.Tx) installQueries
	GetChannelInstallationByAppID(ctx context.Context, arg db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error)
	UpsertChannelInstallation(ctx context.Context, arg db.UpsertChannelInstallationParams) (db.ChannelInstallation, error)
	UpsertChannelInstallationByAppID(ctx context.Context, arg db.UpsertChannelInstallationByAppIDParams) (db.ChannelInstallation, error)
	DeleteChannelChatSessionBindingsByInstallation(ctx context.Context, arg db.DeleteChannelChatSessionBindingsByInstallationParams) error
}

// dbInstallQueries adapts *db.Queries to installQueries — the generated
// WithTx returns *db.Queries, so we wrap it to return the interface.
type dbInstallQueries struct{ *db.Queries }

func (q dbInstallQueries) WithTx(tx pgx.Tx) installQueries {
	return dbInstallQueries{q.Queries.WithTx(tx)}
}

// InstallationService creates, refreshes and revokes per-agent DingTalk
// installations. It owns the at-rest encryption of the client_secret so
// no caller can accidentally insert a row with plaintext credentials —
// the only path to writing a dingtalk channel_installation goes through
// here. Mirrors lark.InstallationService.
type InstallationService struct {
	queries *ChannelStore
	q       installQueries
	tx      engine.TxStarter
	box     *secretbox.Box
}

// NewInstallationService binds the service to a queries handle, a tx
// starter (*pgxpool.Pool) and a secretbox keyed for at-rest encryption.
// The box MUST be non-nil; we refuse to fall back to plaintext storage
// even in test or dev configurations.
func NewInstallationService(queries *db.Queries, tx engine.TxStarter, box *secretbox.Box) (*InstallationService, error) {
	if queries == nil {
		return nil, errors.New("dingtalk: InstallationService requires queries")
	}
	return newInstallationService(dbInstallQueries{queries}, tx, NewChannelStore(queries), box)
}

// newInstallationService is the testable core: it takes the
// installQueries interface so tests can inject a fake (with a fake
// TxStarter) without a real DB.
func newInstallationService(q installQueries, tx engine.TxStarter, store *ChannelStore, box *secretbox.Box) (*InstallationService, error) {
	if box == nil {
		return nil, errors.New("dingtalk: InstallationService requires a non-nil secretbox.Box")
	}
	if q == nil {
		return nil, errors.New("dingtalk: InstallationService requires queries")
	}
	if tx == nil {
		return nil, errors.New("dingtalk: InstallationService requires a tx starter")
	}
	return &InstallationService{queries: store, q: q, tx: tx, box: box}, nil
}

// Upsert creates a new installation or refreshes an existing one.
//
// DingTalk's scan-to-create flow ("一键创建") re-authorizes the org's
// EXISTING app on a re-scan, returning the same client_id the first
// install minted. The app — not the (workspace, agent) pair — is the
// natural identity, so when the incoming client_id already belongs to
// another agent in the same workspace the row MOVES to the new agent
// (contrast Slack, where a re-used app id means a paste mistake and is
// refused). User bindings survive the move (they hang off the
// installation id); chat-session bindings are retired because each
// chat_session is permanently tied to the agent it was created under.
func (s *InstallationService) Upsert(ctx context.Context, p InstallationParams) (Installation, error) {
	if err := validateInstallationParams(p); err != nil {
		return Installation{}, err
	}
	sealed, err := s.box.Seal([]byte(p.ClientSecret))
	if err != nil {
		return Installation{}, fmt.Errorf("encrypt client_secret: %w", err)
	}
	cfg, err := encodeInstallConfig(Installation{
		ClientID:           p.ClientID,
		AppSecretEncrypted: sealed,
	})
	if err != nil {
		return Installation{}, err
	}

	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return Installation{}, fmt.Errorf("begin install tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	// Who holds this client_id today? Decides which upsert shape applies
	// and fences the cross-workspace case with a clear error instead of a
	// raw unique violation.
	prev, err := qtx.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: channelTypeDingTalk,
		AppID:       p.ClientID,
	})
	prevFound := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Installation{}, fmt.Errorf("lookup installation by client_id: %w", err)
	}
	if prevFound && !uuidEqual(prev.WorkspaceID, p.WorkspaceID) {
		return Installation{}, ErrAppOwnedByAnotherWorkspace
	}

	var row db.ChannelInstallation
	if prevFound && !uuidEqual(prev.AgentID, p.AgentID) {
		// Agent switch: conflict on the (channel_type, app_id) routing
		// index moves the existing row to the new agent and reactivates
		// it. The query's workspace fence turns a cross-workspace race
		// into zero rows.
		row, err = qtx.UpsertChannelInstallationByAppID(ctx, db.UpsertChannelInstallationByAppIDParams{
			WorkspaceID:     p.WorkspaceID,
			AgentID:         p.AgentID,
			ChannelType:     channelTypeDingTalk,
			Config:          cfg,
			InstallerUserID: p.InstallerUserID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Installation{}, ErrAppOwnedByAnotherWorkspace
			}
			if isUniqueViolation(err) {
				// Moving would collide with the target agent's existing
				// (workspace_id, agent_id, channel_type) row.
				return Installation{}, ErrAgentAlreadyConnected
			}
			return Installation{}, fmt.Errorf("move installation to agent: %w", err)
		}
		// Existing chat sessions are pinned to the OLD agent; retire
		// their bindings so the next inbound message opens a fresh
		// session under the new one. chat_session rows stay for history.
		if err := qtx.DeleteChannelChatSessionBindingsByInstallation(ctx, db.DeleteChannelChatSessionBindingsByInstallationParams{
			InstallationID: row.ID,
			ChannelType:    channelTypeDingTalk,
		}); err != nil {
			return Installation{}, fmt.Errorf("retire chat session bindings: %w", err)
		}
	} else {
		// Fresh install, or a re-scan for the same agent (including the
		// recovery path where the org-side app was deleted and the scan
		// minted a NEW client_id: the (workspace, agent, channel) conflict
		// key replaces that agent's config in place).
		row, err = qtx.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{
			WorkspaceID:     p.WorkspaceID,
			AgentID:         p.AgentID,
			ChannelType:     channelTypeDingTalk,
			Config:          cfg,
			InstallerUserID: p.InstallerUserID,
		})
		if err != nil {
			if isUniqueViolation(err) {
				// Lost a race on the (channel_type, app_id) routing index.
				return Installation{}, ErrAppOwnedByAnotherWorkspace
			}
			return Installation{}, fmt.Errorf("upsert installation: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Installation{}, fmt.Errorf("commit install tx: %w", err)
	}
	return installationFromRow(row)
}

// Revoke flips status to 'revoked'. The row is preserved (no DELETE) so
// audit history remains queryable; a subsequent re-install via Upsert
// flips status back to 'active' atomically.
func (s *InstallationService) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.queries.SetDingTalkInstallationStatus(ctx, id, InstallationRevoked)
}

// DecryptClientSecret returns the plaintext client_secret for the
// supplied installation row. Reserved for the future inbound transport
// (DingTalk Stream Mode) that must authenticate on behalf of an
// installation; the plaintext value must never round-trip through an
// HTTP response.
func (s *InstallationService) DecryptClientSecret(inst Installation) (string, error) {
	plain, err := s.box.Open(inst.AppSecretEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt client_secret: %w", err)
	}
	return string(plain), nil
}

// GetInWorkspace is the workspace-scoped lookup helper, so a forged
// installation_id from a different workspace returns NotFound instead
// of leaking existence.
func (s *InstallationService) GetInWorkspace(ctx context.Context, id, workspaceID pgtype.UUID) (Installation, error) {
	row, err := s.queries.GetDingTalkInstallationInWorkspace(ctx, id, workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Installation{}, ErrInstallationNotFound
		}
		return Installation{}, err
	}
	return row, nil
}

// ListByWorkspace returns every installation rooted at the workspace,
// active and revoked, oldest first.
func (s *InstallationService) ListByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]Installation, error) {
	return s.queries.ListDingTalkInstallationsByWorkspace(ctx, workspaceID)
}

// ErrInstallationNotFound surfaces "no row matches in this workspace" —
// used by the HTTP layer to return 404. Distinct from a plain
// pgx.ErrNoRows so handlers do not need to import pgx.
var ErrInstallationNotFound = errors.New("dingtalk installation not found")

func validateInstallationParams(p InstallationParams) error {
	switch {
	case !p.WorkspaceID.Valid:
		return errors.New("workspace_id is required")
	case !p.AgentID.Valid:
		return errors.New("agent_id is required")
	case !p.InstallerUserID.Valid:
		return errors.New("installer_user_id is required")
	case p.ClientID == "":
		return errors.New("client_id is required")
	case p.ClientSecret == "":
		return errors.New("client_secret is required")
	}
	return nil
}
