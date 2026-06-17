package octo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// BindingToken is the public shape of a freshly minted token. The raw token is
// returned exactly once — it is the unguessable secret embedded in the binding
// URL the bot replies with. After Mint returns, only the hash exists
// server-side; the raw value cannot be recovered from the DB.
type BindingToken struct {
	Raw       string
	ExpiresAt time.Time
}

// RedeemedBindingToken is returned after a successful redemption.
type RedeemedBindingToken struct {
	WorkspaceID    pgtype.UUID
	InstallationID pgtype.UUID
	OctoUID        UID
}

// BindingTokenService mints and redeems binding tokens for the "you're not bound
// yet, click here" flow. TTL is fixed at BindingTokenTTL (15 min); the DB CHECK
// enforces the same cap. Redemption is transactional: consuming the token and
// inserting the binding commit together.
type BindingTokenService struct {
	queries *db.Queries
	tx      TxStarter
	now     func() time.Time
}

// NewBindingTokenService constructs the default service (time.Now clock).
func NewBindingTokenService(queries *db.Queries, tx TxStarter) *BindingTokenService {
	return NewBindingTokenServiceWithClock(queries, tx, time.Now)
}

// NewBindingTokenServiceWithClock is the test seam; production uses
// NewBindingTokenService.
func NewBindingTokenServiceWithClock(queries *db.Queries, tx TxStarter, now func() time.Time) *BindingTokenService {
	return &BindingTokenService{queries: queries, tx: tx, now: now}
}

// Mint creates a single-use binding token and returns the raw secret + expiry.
// The raw value must be delivered over a secure channel (an Octo DM) and never
// logged. Mint is the only function that produces a raw token; later reads are
// by hash.
func (s *BindingTokenService) Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, uid UID) (BindingToken, error) {
	raw, err := randomToken(32)
	if err != nil {
		return BindingToken{}, fmt.Errorf("generate token: %w", err)
	}
	expiresAt := s.now().Add(BindingTokenTTL)
	if _, err := s.queries.CreateOctoBindingToken(ctx, db.CreateOctoBindingTokenParams{
		TokenHash:      hashToken(raw),
		WorkspaceID:    workspaceID,
		InstallationID: installationID,
		OctoUid:        string(uid),
		ExpiresAt:      pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		return BindingToken{}, fmt.Errorf("persist token: %w", err)
	}
	return BindingToken{Raw: raw, ExpiresAt: expiresAt}, nil
}

// RedeemAndBind atomically consumes a raw token and writes the octo_user_binding
// row in one transaction. The redeemer's identity is the supplied
// multicaUserID (from the session, never the token), so a stolen token cannot
// bind an Octo uid to an attacker's account.
//
// Typed failures:
//   - ErrBindingTokenInvalid: token missing / consumed / expired (one opaque
//     error to avoid a replay timing oracle).
//   - ErrBindingAlreadyAssigned: (installation, uid) already bound to a
//     different user; rolled back so the existing holder is undisturbed.
//   - ErrBindingNotWorkspaceMember: redeemer isn't a member of the token's
//     workspace (composite FK to member trips).
func (s *BindingTokenService) RedeemAndBind(ctx context.Context, raw string, multicaUserID pgtype.UUID) (RedeemedBindingToken, error) {
	if s.tx == nil {
		return RedeemedBindingToken{}, errors.New("octo: BindingTokenService missing TxStarter")
	}
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return RedeemedBindingToken{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.queries.WithTx(tx)

	row, err := qtx.ConsumeOctoBindingToken(ctx, hashToken(raw))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RedeemedBindingToken{}, ErrBindingTokenInvalid
		}
		return RedeemedBindingToken{}, fmt.Errorf("consume token: %w", err)
	}

	_, err = qtx.CreateOctoUserBinding(ctx, db.CreateOctoUserBindingParams{
		WorkspaceID:    row.WorkspaceID,
		MulticaUserID:  multicaUserID,
		InstallationID: row.InstallationID,
		OctoUid:        row.OctoUid,
	})
	if err != nil {
		// pgx.ErrNoRows: the conflict row exists but points at a different
		// multica_user_id, so the ON CONFLICT DO UPDATE WHERE rejected the rebind.
		if errors.Is(err, pgx.ErrNoRows) {
			return RedeemedBindingToken{}, ErrBindingAlreadyAssigned
		}
		// 23503 = foreign_key_violation against member(workspace_id, user_id):
		// the redeemer is not a member of the token's workspace.
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
		OctoUID:        UID(row.OctoUid),
	}, nil
}

// ErrBindingTokenInvalid: token hash missing, already consumed, or expired. The
// caller must NOT distinguish the sub-cases (avoids a replay timing oracle).
var ErrBindingTokenInvalid = errors.New("binding token invalid or expired")

// ErrBindingAlreadyAssigned: the (installation, uid) pair is already bound to a
// different Multica user. Account transfer must go through an explicit unbind.
var ErrBindingAlreadyAssigned = errors.New("octo uid is already bound to a different user")

// ErrBindingNotWorkspaceMember: the redeemer is not a member of the token's
// workspace. Translated to 403 at the HTTP boundary.
var ErrBindingNotWorkspaceMember = errors.New("redeemer is not a workspace member")

func randomToken(n int) (string, error) { return util.RandomToken(n) }

func hashToken(raw string) string { return util.HashTokenSHA256(raw) }
