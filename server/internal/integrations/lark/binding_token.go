package lark

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// BindingToken is the public shape of a freshly minted token. The raw
// token is returned to the caller exactly once — it is the unguessable
// secret embedded in the binding URL the Bot replies with. After this
// call returns, only the hash exists server-side; the raw value
// cannot be recovered from the DB.
type BindingToken struct {
	Raw       string
	ExpiresAt time.Time
}

// RedeemedBindingToken is the row returned to the caller after a
// successful redemption. The redemption path uses these fields to
// write the lark_user_binding row.
type RedeemedBindingToken struct {
	WorkspaceID    pgtype.UUID
	InstallationID pgtype.UUID
	LarkOpenID     OpenID
}

// InstallerBinder is the narrow surface RegistrationService needs to
// record the installer's lark_user_binding row in the same business
// step as the installation insert. Without this step the first inbound
// message from the installer would be dropped as `unbound_user` and
// the Bot would reply "you're not bound, click here…" to the person
// who just authorized the install seconds ago.
//
// Implementations MUST be idempotent on (installation_id, lark_open_id):
// a re-install by the same user should not error.
//
// `qtx` is the *db.Queries handle to run the bind against. The caller
// opens the transaction so the installation insert and the binding
// write commit together; nil means "use the service's own
// (non-transactional) queries handle".
type InstallerBinder interface {
	BindInstallerTx(ctx context.Context, qtx *db.Queries, p InstallerBindParams) error
}

// InstallerBindParams carries the inputs InstallerBinder needs. Kept
// as a struct so adding union_id (Phase 2) does not break callers.
type InstallerBindParams struct {
	WorkspaceID    pgtype.UUID
	InstallationID pgtype.UUID
	MulticaUserID  pgtype.UUID // the installer's Multica account
	LarkOpenID     OpenID      // the installer's per-installation open_id
}

// BindingTokenService mints and redeems binding tokens for the
// "you're not bound yet, click here" flow. The TTL is fixed at
// BindingTokenTTL (15 min); the DB CHECK enforces the same cap so a
// misconfigured caller cannot quietly mint a longer-lived token.
//
// Redemption (RedeemAndBind) is transactional: consuming the token
// and inserting the lark_user_binding row commit together, so a
// failed bind never burns a token, and a successful bind never
// leaves a consumed-but-unused token behind.
type BindingTokenService struct {
	queries *db.Queries
	tx      TxStarter
	now     func() time.Time
}

// NewBindingTokenService constructs the default service. The clock
// is injectable so tests can pin time for deterministic expiry
// behavior; production callers use NewBindingTokenServiceWithClock
// with time.Now.
func NewBindingTokenService(queries *db.Queries, tx TxStarter) *BindingTokenService {
	return NewBindingTokenServiceWithClock(queries, tx, time.Now)
}

// NewBindingTokenServiceWithClock is the seam for tests; production
// callers should use NewBindingTokenService.
func NewBindingTokenServiceWithClock(queries *db.Queries, tx TxStarter, now func() time.Time) *BindingTokenService {
	return &BindingTokenService{queries: queries, tx: tx, now: now}
}

// Mint creates a new single-use binding token and returns the raw
// secret + expiry. The raw value MUST be sent over a secure channel
// to the intended recipient — Lark DMs are encrypted in transit by
// the platform — and never logged. Mint is the only function in this
// package that produces a raw token; subsequent reads are by hash.
func (s *BindingTokenService) Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, openID OpenID) (BindingToken, error) {
	raw, err := randomToken(32)
	if err != nil {
		return BindingToken{}, fmt.Errorf("generate token: %w", err)
	}
	hash := hashToken(raw)
	expiresAt := s.now().Add(BindingTokenTTL)

	if _, err := s.queries.CreateLarkBindingToken(ctx, db.CreateLarkBindingTokenParams{
		TokenHash:      hash,
		WorkspaceID:    workspaceID,
		InstallationID: installationID,
		LarkOpenID:     string(openID),
		ExpiresAt:      pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		return BindingToken{}, fmt.Errorf("persist token: %w", err)
	}
	return BindingToken{Raw: raw, ExpiresAt: expiresAt}, nil
}

// RedeemAndBind atomically consumes a raw token and writes the
// lark_user_binding row in a single DB transaction. The redeemer's
// identity is the supplied multicaUserID (taken from the session by
// the handler, never from the token), so a stolen token cannot bind
// a Lark open_id to an attacker's account.
//
// Failure modes are returned as typed errors:
//
//   - ErrBindingTokenInvalid: token doesn't exist / already consumed /
//     expired. Same opaque error for all three to avoid a timing
//     oracle for replay races.
//
//   - ErrBindingAlreadyAssigned: a binding already exists for this
//     (installation, open_id), pointing at a DIFFERENT Multica user.
//     The token is NOT consumed in this case — we roll back so the
//     correct holder of the existing binding is not disrupted and
//     ops can still revoke the surplus token explicitly. Account
//     transfer must go through an explicit unbind, not a redemption.
//
//   - ErrBindingNotWorkspaceMember: the redeemer is not a member of
//     the token's workspace, which trips the composite FK to
//     member(workspace_id, user_id). Rolled back identically.
//
// On the happy path the consume + bind commit together: a successful
// return guarantees both the consumed_at write and the binding row
// landed; a returned error guarantees neither did.
func (s *BindingTokenService) RedeemAndBind(ctx context.Context, raw string, multicaUserID pgtype.UUID) (RedeemedBindingToken, error) {
	if s.tx == nil {
		return RedeemedBindingToken{}, errors.New("lark: BindingTokenService missing TxStarter")
	}
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return RedeemedBindingToken{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.queries.WithTx(tx)

	row, err := qtx.ConsumeLarkBindingToken(ctx, hashToken(raw))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RedeemedBindingToken{}, ErrBindingTokenInvalid
		}
		return RedeemedBindingToken{}, fmt.Errorf("consume token: %w", err)
	}

	_, err = qtx.CreateLarkUserBinding(ctx, db.CreateLarkUserBindingParams{
		WorkspaceID:    row.WorkspaceID,
		MulticaUserID:  multicaUserID,
		InstallationID: row.InstallationID,
		LarkOpenID:     row.LarkOpenID,
	})
	if err != nil {
		// pgx.ErrNoRows here means the conflict row exists but its
		// multica_user_id differs from ours, so the WHERE clause on
		// the ON CONFLICT DO UPDATE rejected the rebind. See the
		// comment on CreateLarkUserBinding in queries/lark.sql.
		if errors.Is(err, pgx.ErrNoRows) {
			return RedeemedBindingToken{}, ErrBindingAlreadyAssigned
		}
		// 23503 is foreign_key_violation. The relevant FK here is
		// lark_user_binding_member_fk (workspace_id, multica_user_id)
		// → member; tripping it means the redeemer is not a member
		// of the token's workspace.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return RedeemedBindingToken{}, ErrBindingNotWorkspaceMember
		}
		return RedeemedBindingToken{}, fmt.Errorf("create binding: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return RedeemedBindingToken{}, fmt.Errorf("commit: %w", err)
	}
	return RedeemedBindingToken{
		WorkspaceID:    row.WorkspaceID,
		InstallationID: row.InstallationID,
		LarkOpenID:     OpenID(row.LarkOpenID),
	}, nil
}

// BindInstallerTx is the auto-binding path for the device-flow
// install: the user who just authorized the install is recorded as
// bound to their own open_id, so the first inbound message in the
// bot's DM arrives at a `bound` identity check and the user is NOT
// prompted with a redundant "click here to bind" card.
//
// `qtx` is the RegistrationService's transaction-scoped queries
// handle. The service opens a transaction that wraps the
// lark_installation insert and this binding write so a half-applied
// install (installation row without the installer binding) cannot
// land. When `qtx` is nil the service's own (non-transactional)
// queries handle is used, which is the right behavior for tests that
// don't need atomicity.
//
// Token redemption deliberately does NOT share this code path:
//   - RedeemAndBind consumes a server-minted token in the same tx as
//     the binding insert; that's how anti-replay works.
//   - BindInstallerTx is invoked from the device-flow success hook
//     where the authoritative proof of identity is the Lark-validated
//     polling response (open_id returned alongside the freshly minted
//     client_id / client_secret). There is no token to consume, and
//     inventing one would only widen the attack surface.
//
// The underlying CreateLarkUserBinding query is idempotent on
// (installation_id, lark_open_id) when multica_user_id matches (the
// ON CONFLICT DO UPDATE gating spelled out on the SQL), so a
// re-install by the same user is a no-op metadata refresh. A
// re-install by a DIFFERENT user surfaces as ErrBindingAlreadyAssigned
// — the registration caller treats that as a hard error and the
// frontend surfaces it as "this Lark account is bound elsewhere",
// preventing one workspace admin from silently rebinding another's
// PersonalAgent install.
func (s *BindingTokenService) BindInstallerTx(ctx context.Context, qtx *db.Queries, p InstallerBindParams) error {
	q := qtx
	if q == nil {
		q = s.queries
	}
	_, err := q.CreateLarkUserBinding(ctx, db.CreateLarkUserBindingParams{
		WorkspaceID:    p.WorkspaceID,
		MulticaUserID:  p.MulticaUserID,
		InstallationID: p.InstallationID,
		LarkOpenID:     string(p.LarkOpenID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBindingAlreadyAssigned
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrBindingNotWorkspaceMember
		}
		return fmt.Errorf("bind installer: %w", err)
	}
	return nil
}

// ErrBindingTokenInvalid is returned by RedeemAndBind when the token
// hash does not exist, the token has already been consumed, or it
// has expired. The caller must NOT distinguish those sub-cases —
// that distinction enables timing oracles for token replay races and
// adds no product value (the user sees the same "link invalid or
// expired, please request a new one" copy either way).
var ErrBindingTokenInvalid = errors.New("binding token invalid or expired")

// ErrBindingAlreadyAssigned is returned by RedeemAndBind when a
// lark_user_binding row already exists for the (installation,
// open_id) pair and points at a different Multica user. Account
// transfer must go through an explicit unbind flow; a binding token
// cannot be used to grab an already-bound open_id from another user.
var ErrBindingAlreadyAssigned = errors.New("lark open_id is already bound to a different user")

// ErrBindingNotWorkspaceMember is returned by RedeemAndBind when the
// redeemer is not (or no longer) a member of the token's workspace,
// detected as a foreign-key violation against member(workspace_id,
// user_id). Translated to 403 at the HTTP boundary.
var ErrBindingNotWorkspaceMember = errors.New("redeemer is not a workspace member")

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	// URL-safe so the token embeds cleanly in the binding URL
	// without escaping. RawURLEncoding drops `=` padding which is
	// optional for decoders and would otherwise look ugly in
	// user-visible URLs.
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
