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
}

// RedeemedBindingToken is returned after a successful redemption.
type RedeemedBindingToken struct {
	WorkspaceID    pgtype.UUID
	InstallationID pgtype.UUID
	WecomUserID    string
}

// BindingTokenService mints and redeems WeCom binding tokens. Redemption is
// transactional: consuming the token and inserting the channel_user_binding
// row commit together, so a failed bind never burns a token.
type BindingTokenService struct {
	q   *db.Queries
	tx  engine.TxStarter
	now func() time.Time
}

// NewBindingTokenService constructs the service. tx (a *pgxpool.Pool) is
// needed for the transactional redeem path.
func NewBindingTokenService(q *db.Queries, tx engine.TxStarter) *BindingTokenService {
	return &BindingTokenService{q: q, tx: tx, now: time.Now}
}

// Mint creates a single-use binding token for (installation, wecomUserID)
// and returns the raw secret + expiry. The raw value must be delivered over
// the aibot WebSocket (encrypted in transit by the platform) and never
// logged.
func (s *BindingTokenService) Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, wecomUserID string) (BindingToken, error) {
	raw, err := randomBindingToken(32)
	if err != nil {
		return BindingToken{}, fmt.Errorf("wecom: generate token: %w", err)
	}
	expiresAt := s.now().Add(BindingTokenTTL)
	if _, err := s.q.CreateChannelBindingToken(ctx, db.CreateChannelBindingTokenParams{
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
