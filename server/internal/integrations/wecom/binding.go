package wecom

// binding.go — the WeCom smart-bot user-binding token flow. An unbound WeCom
// user who messages the bot gets a "link your account" prompt (minted here,
// delivered by the OutboundReplier), clicks through to the in-product redeem
// page, and their WeCom userid is bound to their Multica account. Mirrors
// slack.BindingTokenService — runs on the generic channel_binding_token /
// channel_user_binding tables with channel_type='wecom'.
//
// Why we need this: aibot smart-bot events carry an anonymized userid
// ("T"-prefixed) that is stable per (bot, user) but bears no relationship
// to the enterprise's real userid or email. Any implicit identity heuristic
// (email-prefix match, corp userid lookup) is impossible. An explicit
// binding table is the ONLY correct answer.

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
// enforces the same 15-minute cap so a misconfigured caller cannot mint
// longer.
const BindingTokenTTL = 15 * time.Minute

// BindingTokenMintInterval is how often one platform user can be issued a new
// binding link. Every message from an unbound user reaches the needs_binding
// outcome, so without a floor here a user who types six lines writes six token
// rows and receives six links, only the last of which they will click.
//
// This is a burst window, not a session-long one, and the distinction matters.
// Suppressing a mint asserts that the link already sent is in the user's
// hands, and nothing verifies that: the send returns as soon as the transport
// accepts the frame and the socket acks asynchronously. If the first prompt is
// lost, a long window turns that into a dead end — every message is answered
// with "tap the link I sent you", pointing at something that never arrived,
// and the user cannot link their account until the window passes.
//
// That covers the send we never hear back about, and equally the send we know
// failed: the row is written before delivery is attempted, so a prompt the
// transport refused outright (no live connection mid-reconnect or lease flip)
// still suppresses the next minute of mints. Nothing here reacts to that
// error — the window itself, not a delivery receipt, is what bounds the
// damage, which is the other reason to keep it short.
//
// Sixty seconds does the job the throttle was written for: six lines typed in
// one breath still write one row, at a cost of at most one row a minute for a
// user who keeps going, against rows that expire in fifteen. The price of
// being wrong is one more message, not ten minutes of a bot insisting it
// already answered.
//
// It must stay comfortably inside BindingTokenTTL so a link a throttled user
// is pointed back at still has real time left on it.
const BindingTokenMintInterval = time.Minute

var (
	// ErrBindingTokenInvalid: token unknown / already consumed / expired.
	// One opaque error for all three avoids a replay timing oracle.
	ErrBindingTokenInvalid = errors.New("wecom: binding token invalid or expired")
	// ErrBindingAlreadyAssigned: this WeCom userid is already bound to a
	// different Multica user (account transfer must go through explicit
	// unbind, not implemented in iter 1 — an admin can DELETE the row).
	ErrBindingAlreadyAssigned = errors.New("wecom: user id is already bound to a different user")
	// ErrBindingNotWorkspaceMember: the redeemer is not a member of the
	// token's workspace. Translated to 403 at the HTTP boundary.
	ErrBindingNotWorkspaceMember = errors.New("wecom: redeemer is not a workspace member")
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
	WecomUserID    string
}

// bindingMintQueries is the slice of generated queries the mint path uses.
// Narrow on purpose: mint is the only part of the service that runs outside a
// transaction, so it is the only part a unit test can drive with a fake.
// *db.Queries satisfies it.
type bindingMintQueries interface {
	CreateChannelBindingToken(ctx context.Context, arg db.CreateChannelBindingTokenParams) (db.ChannelBindingToken, error)
	FindLiveChannelBindingToken(ctx context.Context, arg db.FindLiveChannelBindingTokenParams) (db.ChannelBindingToken, error)
}

// BindingTokenService mints and redeems WeCom binding tokens. Redemption is
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

// Mint creates a single-use binding token for (installation, wecomUserID)
// and returns the raw secret + expiry. The raw value must be delivered over
// the aibot WebSocket (encrypted in transit by the platform) and never
// logged.
//
// Throttled: if this user already has a live, unconsumed token minted inside
// BindingTokenMintInterval, no row is written and the result comes back with
// Reused set and Raw empty — the raw secret was never stored, so there is
// nothing to hand back. Callers point the user at the link already in their
// chat instead. This is what keeps an unbound user's message stream from
// writing a token row per message.
//
// Best-effort, not a guarantee: the lookup and the insert are two statements,
// so two replies racing on the same user can both miss and both mint. In
// practice they are serialised by a wide margin — the inbound read loop
// dispatches one frame at a time and the second reply is a whole dispatch
// behind — and losing the race costs one extra row and one extra link, both
// private to that same user and both expiring on the usual TTL. Tightening it
// further would mean holding a lock across the inbound hot path, which is not
// worth it for a cosmetic throttle.
func (s *BindingTokenService) Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, wecomUserID string) (BindingToken, error) {
	if s.mint == nil {
		return BindingToken{}, errors.New("wecom: BindingTokenService missing queries")
	}
	live, err := s.mint.FindLiveChannelBindingToken(ctx, db.FindLiveChannelBindingTokenParams{
		InstallationID: installationID,
		ChannelType:    channelTypeWecom,
		ChannelUserID:  wecomUserID,
		MintInterval:   pgtype.Interval{Microseconds: BindingTokenMintInterval.Microseconds(), Valid: true},
	})
	switch {
	case err == nil:
		return BindingToken{ExpiresAt: live.ExpiresAt.Time, Reused: true}, nil
	case errors.Is(err, pgx.ErrNoRows):
		// Nothing live for this user — mint below.
	default:
		return BindingToken{}, fmt.Errorf("wecom: look up live token: %w", err)
	}

	raw, err := randomBindingToken(32)
	if err != nil {
		return BindingToken{}, fmt.Errorf("wecom: generate token: %w", err)
	}
	expiresAt := s.now().Add(BindingTokenTTL)
	if _, err := s.mint.CreateChannelBindingToken(ctx, db.CreateChannelBindingTokenParams{
		TokenHash:      hashBindingToken(raw),
		WorkspaceID:    workspaceID,
		InstallationID: installationID,
		ChannelType:    channelTypeWecom,
		ChannelUserID:  wecomUserID,
		ExpiresAt:      pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		return BindingToken{}, fmt.Errorf("wecom: persist token: %w", err)
	}
	return BindingToken{Raw: raw, ExpiresAt: expiresAt}, nil
}

// RedeemAndBind atomically consumes a raw token and binds the WeCom userid
// to multicaUserID (taken from the session, never from the token). Returns
// ErrBindingTokenInvalid / ErrBindingAlreadyAssigned /
// ErrBindingNotWorkspaceMember.
func (s *BindingTokenService) RedeemAndBind(ctx context.Context, raw string, multicaUserID pgtype.UUID) (RedeemedBindingToken, error) {
	if s.tx == nil {
		return RedeemedBindingToken{}, errors.New("wecom: BindingTokenService missing TxStarter")
	}
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return RedeemedBindingToken{}, fmt.Errorf("wecom: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	row, err := qtx.ConsumeChannelBindingToken(ctx, hashBindingToken(raw))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RedeemedBindingToken{}, ErrBindingTokenInvalid
		}
		return RedeemedBindingToken{}, fmt.Errorf("wecom: consume token: %w", err)
	}
	// Guard: the token row's channel_type must be 'wecom' — the redeem
	// endpoint is wecom-scoped but the underlying table is shared across
	// platforms. A caller who steals a slack token and posts it here would
	// otherwise silently bind a slack userid via the wecom endpoint.
	if row.ChannelType != channelTypeWecom {
		return RedeemedBindingToken{}, ErrBindingTokenInvalid
	}

	// Explicit membership gate (no member FK): returning before Commit rolls
	// the consume back, so a non-member's attempt does not burn the token.
	if _, err := qtx.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      multicaUserID,
		WorkspaceID: row.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RedeemedBindingToken{}, ErrBindingNotWorkspaceMember
		}
		return RedeemedBindingToken{}, fmt.Errorf("wecom: check membership: %w", err)
	}

	if _, err := qtx.CreateChannelUserBinding(ctx, db.CreateChannelUserBindingParams{
		WorkspaceID:    row.WorkspaceID,
		MulticaUserID:  multicaUserID,
		InstallationID: row.InstallationID,
		ChannelType:    channelTypeWecom,
		ChannelUserID:  row.ChannelUserID,
		Config:         []byte(`{}`),
	}); err != nil {
		// pgx.ErrNoRows means the existing binding points at a different
		// user — the ON CONFLICT DO UPDATE WHERE multica_user_id=… gating
		// rejected it.
		if errors.Is(err, pgx.ErrNoRows) {
			return RedeemedBindingToken{}, ErrBindingAlreadyAssigned
		}
		return RedeemedBindingToken{}, fmt.Errorf("wecom: create binding: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return RedeemedBindingToken{}, fmt.Errorf("wecom: commit: %w", err)
	}
	return RedeemedBindingToken{
		WorkspaceID:    row.WorkspaceID,
		InstallationID: row.InstallationID,
		WecomUserID:    row.ChannelUserID,
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
