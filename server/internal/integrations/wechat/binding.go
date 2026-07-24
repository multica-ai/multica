package wechat

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

// This file is the WeChat user-binding token flow: an unbound WeChat user who
// messages the bot gets a "link your account" prompt (minted here, delivered by
// the OutboundReplier), clicks through to the in-product redeem page, and their
// WeChat user id is bound to their Multica account. It mirrors
// slack.BindingTokenService and lark.BindingTokenService, running on the generic
// channel_* queries with channel_type='wechat'.

// BindingTokenTTL bounds a token's life. The channel_binding_token CHECK
// enforces the same 15-minute cap so a misconfigured caller cannot mint longer.
const BindingTokenTTL = 15 * time.Minute

var (
	// ErrBindingTokenInvalid: token unknown / already consumed / expired. One
	// opaque error for all three avoids a replay timing oracle.
	ErrBindingTokenInvalid = errors.New("wechat: binding token invalid or expired")
	// ErrBindingAlreadyAssigned: this WeChat user id is already bound to a
	// different Multica user (account transfer must go through explicit unbind).
	ErrBindingAlreadyAssigned = errors.New("wechat: user id is already bound to a different user")
	// ErrBindingNotWorkspaceMember: the redeemer is not a member of the token's
	// workspace. Translated to 403 at the HTTP boundary.
	ErrBindingNotWorkspaceMember = errors.New("wechat: redeemer is not a workspace member")
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
	WeChatUserID   string
}

// BindingTokenService mints and redeems WeChat binding tokens. Redemption is
// transactional: consuming the token and inserting the channel_user_binding row
// commit together, so a failed bind never burns a token.
type BindingTokenService struct {
	q   *db.Queries
	tx  engine.TxStarter
	now func() time.Time
}

// NewBindingTokenService constructs the service. tx (a *pgxpool.Pool) is needed
// for the transactional redeem path.
func NewBindingTokenService(q *db.Queries, tx engine.TxStarter) *BindingTokenService {
	return &BindingTokenService{q: q, tx: tx, now: time.Now}
}

// Mint creates a single-use binding token for (installation, wechatUserID) and
// returns the raw secret + expiry. The raw value is delivered over WeChat and
// never logged.
func (s *BindingTokenService) Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, wechatUserID string) (BindingToken, error) {
	raw, err := randomBindingToken(32)
	if err != nil {
		return BindingToken{}, fmt.Errorf("generate token: %w", err)
	}
	expiresAt := s.now().Add(BindingTokenTTL)
	if _, err := s.q.CreateChannelBindingToken(ctx, db.CreateChannelBindingTokenParams{
		TokenHash:      hashBindingToken(raw),
		WorkspaceID:    workspaceID,
		InstallationID: installationID,
		ChannelType:    string(TypeWechat),
		ChannelUserID:  wechatUserID,
		ExpiresAt:      pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		return BindingToken{}, fmt.Errorf("persist token: %w", err)
	}
	return BindingToken{Raw: raw, ExpiresAt: expiresAt}, nil
}

// RedeemAndBind atomically consumes a raw token and binds the WeChat user id to
// multicaUserID (taken from the session, never from the token). Returns
// ErrBindingTokenInvalid / ErrBindingAlreadyAssigned / ErrBindingNotWorkspaceMember.
func (s *BindingTokenService) RedeemAndBind(ctx context.Context, raw string, multicaUserID pgtype.UUID) (RedeemedBindingToken, error) {
	if s.tx == nil {
		return RedeemedBindingToken{}, errors.New("wechat: BindingTokenService missing TxStarter")
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
		ChannelType:    string(TypeWechat),
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
		WeChatUserID:   row.ChannelUserID,
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
