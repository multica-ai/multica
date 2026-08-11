package sharecrm

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
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// BindingTokenTTL bounds a token's life. The channel_binding_token CHECK
// enforces the same 15-minute cap so a misconfigured caller cannot mint longer.
const BindingTokenTTL = 15 * time.Minute

// BindingTokenMintInterval is the per-user mint throttle. An unbound user who
// keeps typing at the bot must not write one token row (and receive one fresh
// link) per message. When a live, unconsumed token was minted inside this
// window, Mint returns Reused with an empty Raw and the caller points the user
// at the link already in their 1:1. Port of wecom.BindingTokenMintInterval
// (MUL-5880): same SQL (FindLiveChannelBindingToken), same best-effort window.
//
// Must stay comfortably inside BindingTokenTTL so a throttled user is still
// pointed at a link with real life left on it.
const BindingTokenMintInterval = time.Minute

var (
	// ErrBindingTokenInvalid: token unknown / already consumed / expired.
	// One opaque error for all three avoids a replay timing oracle.
	ErrBindingTokenInvalid = errors.New("sharecrm: binding token invalid or expired")
	// ErrBindingAlreadyAssigned: this ShareCRM user id is already bound to a
	// different Multica user (account transfer must go through explicit unbind).
	ErrBindingAlreadyAssigned = errors.New("sharecrm: user id is already bound to a different user")
	// ErrBindingNotWorkspaceMember: the redeemer is not a member of the token's
	// workspace. Translated to 403 at the HTTP boundary.
	ErrBindingNotWorkspaceMember = errors.New("sharecrm: redeemer is not a workspace member")
)

// BindingToken is a freshly minted token. The raw value is returned exactly
// once (embedded in the binding URL); only its hash is persisted.
type BindingToken struct {
	Raw       string
	ExpiresAt time.Time

	// Reused says the throttle suppressed the mint because a live link is
	// already sitting in the user's chat. Raw is empty in that case and there
	// is no way to recover it — the table only ever held the hash — so the
	// caller must point the user back at the earlier message rather than
	// building a URL. ExpiresAt carries the live token's expiry, not a fresh
	// one's.
	Reused bool
}

// RedeemedBindingToken is returned after a successful redemption.
type RedeemedBindingToken struct {
	WorkspaceID    pgtype.UUID
	InstallationID pgtype.UUID
	ShareCRMUserID string
}

// bindingMintQueries is the slice of generated queries the mint path uses.
// Narrow on purpose: mint is the only part of the service that runs outside a
// transaction, so it is the only part a unit test can drive with a fake.
// *db.Queries satisfies it.
type bindingMintQueries interface {
	CreateChannelBindingToken(ctx context.Context, arg db.CreateChannelBindingTokenParams) (db.ChannelBindingToken, error)
	FindLiveChannelBindingToken(ctx context.Context, arg db.FindLiveChannelBindingTokenParams) (db.ChannelBindingToken, error)
}

// BindingTokenService mints and redeems ShareCRM binding tokens. Redemption is
// transactional: consuming the token and inserting the channel_user_binding
// row commit together, so a failed bind never burns a token.
type BindingTokenService struct {
	q    *db.Queries
	mint bindingMintQueries
	tx   engine.TxStarter
	now  func() time.Time
}

// NewBindingTokenService constructs the service. tx (a *pgxpool.Pool) is
// needed for the transactional redeem path.
func NewBindingTokenService(q *db.Queries, tx engine.TxStarter) *BindingTokenService {
	return &BindingTokenService{q: q, mint: q, tx: tx, now: time.Now}
}

// Mint creates a single-use binding token for (installation, channelUserID)
// and returns the raw secret + expiry. The raw value must be delivered over
// ShareCRM (encrypted in transit by the platform) and never logged.
//
// Throttled: if this user already has a live, unconsumed token minted inside
// BindingTokenMintInterval, no row is written and the result comes back with
// Reused set and Raw empty — the raw secret was never stored, so there is
// nothing to hand back. Callers point the user at the link already in their
// chat instead. This is what keeps an unbound user's message stream from
// writing a token row per message.
//
// Best-effort, not a guarantee: the lookup and the insert are two statements,
// so two replies racing on the same user can both miss and both mint. Losing
// the race costs one extra row and one extra link, both private to that same
// user and both expiring on the usual TTL.
func (s *BindingTokenService) Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, channelUserID string) (BindingToken, error) {
	if s.mint == nil {
		return BindingToken{}, errors.New("sharecrm: BindingTokenService missing queries")
	}
	live, err := s.mint.FindLiveChannelBindingToken(ctx, db.FindLiveChannelBindingTokenParams{
		InstallationID: installationID,
		ChannelType:    string(TypeShareCRM),
		ChannelUserID:  channelUserID,
		MintInterval:   pgtype.Interval{Microseconds: BindingTokenMintInterval.Microseconds(), Valid: true},
	})
	switch {
	case err == nil:
		return BindingToken{ExpiresAt: live.ExpiresAt.Time, Reused: true}, nil
	case errors.Is(err, pgx.ErrNoRows):
		// Nothing live for this user — mint below.
	default:
		return BindingToken{}, fmt.Errorf("sharecrm: look up live token: %w", err)
	}

	raw, err := randomBindingToken(32)
	if err != nil {
		return BindingToken{}, fmt.Errorf("sharecrm: generate token: %w", err)
	}
	expiresAt := s.now().Add(BindingTokenTTL)
	if _, err := s.mint.CreateChannelBindingToken(ctx, db.CreateChannelBindingTokenParams{
		TokenHash:      hashBindingToken(raw),
		WorkspaceID:    workspaceID,
		InstallationID: installationID,
		ChannelType:    string(TypeShareCRM),
		ChannelUserID:  channelUserID,
		ExpiresAt:      pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		return BindingToken{}, fmt.Errorf("sharecrm: persist token: %w", err)
	}
	return BindingToken{Raw: raw, ExpiresAt: expiresAt}, nil
}

// RedeemAndBind atomically consumes a raw token and binds the ShareCRM user id
// to multicaUserID (taken from the session, never from the token). Returns
// ErrBindingTokenInvalid / ErrBindingAlreadyAssigned / ErrBindingNotWorkspaceMember.
func (s *BindingTokenService) RedeemAndBind(ctx context.Context, raw string, multicaUserID pgtype.UUID) (RedeemedBindingToken, error) {
	if s.tx == nil {
		return RedeemedBindingToken{}, errors.New("sharecrm: BindingTokenService missing TxStarter")
	}
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return RedeemedBindingToken{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	row, err := qtx.ConsumeChannelBindingToken(ctx, hashBindingToken(raw))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RedeemedBindingToken{}, ErrBindingTokenInvalid
		}
		return RedeemedBindingToken{}, fmt.Errorf("consume token: %w", err)
	}
	if row.ChannelType != string(TypeShareCRM) {
		return RedeemedBindingToken{}, ErrBindingTokenInvalid
	}

	// Explicit membership gate (no member FK): returning before Commit rolls the
	// consume back, so a non-member's attempt does not burn the token.
	if _, err := qtx.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      multicaUserID,
		WorkspaceID: row.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RedeemedBindingToken{}, ErrBindingNotWorkspaceMember
		}
		return RedeemedBindingToken{}, fmt.Errorf("check membership: %w", err)
	}

	if _, err := qtx.CreateChannelUserBinding(ctx, db.CreateChannelUserBindingParams{
		WorkspaceID:    row.WorkspaceID,
		MulticaUserID:  multicaUserID,
		InstallationID: row.InstallationID,
		ChannelType:    string(TypeShareCRM),
		ChannelUserID:  row.ChannelUserID,
		Config:         []byte(`{}`),
	}); err != nil {
		// pgx.ErrNoRows means the existing binding points at a different user —
		// the ON CONFLICT DO UPDATE WHERE multica_user_id=… gating rejected it.
		if errors.Is(err, pgx.ErrNoRows) {
			return RedeemedBindingToken{}, ErrBindingAlreadyAssigned
		}
		return RedeemedBindingToken{}, fmt.Errorf("create binding: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return RedeemedBindingToken{}, fmt.Errorf("commit: %w", err)
	}
	return RedeemedBindingToken{
		WorkspaceID:    row.WorkspaceID,
		InstallationID: row.InstallationID,
		ShareCRMUserID: row.ChannelUserID,
	}, nil
}

func randomBindingToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashBindingToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
