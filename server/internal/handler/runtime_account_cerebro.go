// CEREBRO-PATCH(runtime-account-cerebro): cerebro-only glue for the JEH-997
// daemon-driven account registration flow. Net-new fork file in the upstream-
// zone path. Account info is piggybacked on the heartbeat (option A) — there
// is no separate HTTP endpoint; recordHeartbeatAccount is invoked from both
// the HTTP and WS heartbeat paths on every beat.
package handler

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// RuntimeAccountInvoker is the upstream-side seam that the cerebro runtime
// account service plugs into. Concrete implementation lives in
// server/internal/cerebro/runtime so the upstream handler package never
// imports the cerebro tree (which would create an import cycle).
type RuntimeAccountInvoker interface {
	RecordAccount(
		ctx context.Context,
		runtimeID pgtype.UUID,
		workspaceID pgtype.UUID,
		provider string,
		loginIdentity string,
	) (RuntimeAccountRecord, error)
}

// RuntimeAccountRecord is the upstream-facing view of the post-record state.
// The cerebro service constructs this from its internal RecordResult before
// returning so the upstream handler never depends on cerebrodb types.
type RuntimeAccountRecord struct {
	AccountID      pgtype.UUID
	Provider       string
	LoginIdentity  string
	CreatedAt      pgtype.Timestamptz
	UpdatedAt      pgtype.Timestamptz
	AccountCreated bool
	RuntimeUpdated bool
}

// Sentinel errors returned by RuntimeAccountInvoker.RecordAccount when the
// reported identity is missing or empty. Exported so the cerebro service
// can return them directly and the heartbeat hook can treat them as
// "daemon sent something the server can't act on yet" rather than as a
// 500-class failure.
var (
	ErrRuntimeAccountInvalidProvider      = errors.New("provider is required")
	ErrRuntimeAccountInvalidLoginIdentity = errors.New("login_identity is required")
)

// recordHeartbeatAccount is the cerebro hook called from both DaemonHeartbeat
// (HTTP) and HandleDaemonWSHeartbeat (WS) after the heartbeat itself succeeds.
// It is intentionally best-effort: failures are logged at warn level but never
// propagate, because the heartbeat ack must keep flowing even when account
// registration is degraded. The daemon retries on the next tick so a transient
// DB error self-heals within ~15-30s.
//
// account is nil when the daemon could not detect a login identity yet
// (auth-state file missing, unsupported provider). That is a valid steady
// state and produces no log noise.
//
// Returns the registered account UUID string so callers can forward it in the
// heartbeat ack (JEH-881). Returns "" on any error or when no identity is
// available.
func (h *Handler) recordHeartbeatAccount(ctx context.Context, rt db.AgentRuntime, account *protocol.DaemonHeartbeatAccount) string {
	if account == nil {
		return ""
	}
	if h.RuntimeAccount == nil {
		return ""
	}
	if account.Provider == "" || account.LoginIdentity == "" {
		// The daemon should never send a half-filled account block; treat
		// it as a soft client bug worth knowing about but not as a failure.
		slog.Debug("heartbeat account hook: empty provider/login_identity skipped",
			"runtime_id", uuidToString(rt.ID),
			"provider", account.Provider,
			"login_identity_present", account.LoginIdentity != "")
		return ""
	}
	rec, err := h.RuntimeAccount.RecordAccount(ctx, rt.ID, rt.WorkspaceID, account.Provider, account.LoginIdentity)
	if err != nil {
		slog.Warn("heartbeat account hook: record failed",
			"runtime_id", uuidToString(rt.ID),
			"provider", account.Provider,
			"error", err)
		return ""
	}
	if rec.AccountCreated || rec.RuntimeUpdated {
		slog.Info("heartbeat account hook: runtime account bound",
			"runtime_id", uuidToString(rt.ID),
			"provider", account.Provider,
			"login_identity", account.LoginIdentity,
			"account_id", uuidToString(rec.AccountID),
			"account_created", rec.AccountCreated,
			"runtime_link_changed", rec.RuntimeUpdated)
	}
	return uuidToString(rec.AccountID)
}
