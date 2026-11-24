package telegram

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

// This file is the Telegram user-binding token flow, mirroring
// slack/binding.go on the generic channel_* queries with
// channel_type='telegram': an unbound Telegram user who messages the bot gets
// a "link your account" prompt, clicks through to the in-product redeem page,
// and their Telegram user id is bound to their Multica account.

// BindingTokenTTL bounds a token's life; the channel_binding_token CHECK
// enforces the same 15-minute cap.
const BindingTokenTTL = 15 * time.Minute

var (
	// ErrBindingTokenInvalid: token unknown / already consumed / expired. One
	// opaque error avoids a replay timing oracle.
	ErrBindingTokenInvalid = errors.New("telegram: binding token invalid or expired")
	// ErrBindingAlreadyAssigned: this Telegram user id is already bound to a
	// different Multica user.
	ErrBindingAlreadyAssigned = errors.New("telegram: user id is already bound to a different user")
	// ErrBindingNotWorkspaceMember: the redeemer is not a member of the token's
	// workspace.
	ErrBindingNotWorkspaceMember = errors.New("telegram: redeemer is not a workspace member")
)

// BindingToken is a freshly minted token; the raw value is returned exactly
// once, only its hash is persisted.
type BindingToken struct {
	Raw       string
	ExpiresAt time.Time
}

// RedeemedBindingToken is returned after a successful redemption.
type RedeemedBindingToken struct {
	WorkspaceID    pgtype.UUID
	InstallationID pgtype.UUID
	TelegramUserID string
}

// BindingTokenService mints and redeems Telegram binding tokens. Redemption
// is transactional: consuming the token and inserting the binding row commit
// together, so a failed bind never burns a token.
type BindingTokenService struct {
	q   *db.Queries
	tx  engine.TxStarter
	now func() time.Time
}

// NewBindingTokenService constructs the service.
func NewBindingTokenService(q *db.Queries, tx engine.TxStarter) *BindingTokenService {
	return &BindingTokenService{q: q, tx: tx, now: time.Now}
}

// Mint creates a single-use binding token for (installation, telegramUserID).
func (s *BindingTokenService) Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, telegramUserID string) (BindingToken, error) {
	raw, err := randomBindingToken(32)
	if err != nil {
		return BindingToken{}, fmt.Errorf("generate token: %w", err)
	}
	expiresAt := s.now().Add(BindingTokenTTL)
	if _, err := s.q.CreateChannelBindingToken(ctx, db.CreateChannelBindingTokenParams{
		TokenHash:      hashBindingToken(raw),
		WorkspaceID:    workspaceID,
		InstallationID: installationID,
		ChannelType:    string(TypeTelegram),
		ChannelUserID:  telegramUserID,
		ExpiresAt:      pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		return BindingToken{}, fmt.Errorf("persist token: %w", err)
	}
	return BindingToken{Raw: raw, ExpiresAt: expiresAt}, nil
}

// RedeemAndBind atomically consumes a raw token and binds the Telegram user
// id to multicaUserID (taken from the session, never from the token).
func (s *BindingTokenService) RedeemAndBind(ctx context.Context, raw string, multicaUserID pgtype.UUID) (RedeemedBindingToken, error) {
	if s.tx == nil {
		return RedeemedBindingToken{}, errors.New("telegram: BindingTokenService missing TxStarter")
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
	if err := validateBindingTokenChannel(row); err != nil {
		// The token table is shared across channel adapters. Keep a token from
		// another adapter from being redeemed through Telegram; returning here
		// rolls the consume back with the surrounding transaction.
		return RedeemedBindingToken{}, err
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
		return RedeemedBindingToken{}, fmt.Errorf("check membership: %w", err)
	}

	if _, err := qtx.CreateChannelUserBinding(ctx, db.CreateChannelUserBindingParams{
		WorkspaceID:    row.WorkspaceID,
		MulticaUserID:  multicaUserID,
		InstallationID: row.InstallationID,
		ChannelType:    string(TypeTelegram),
		ChannelUserID:  row.ChannelUserID,
		Config:         []byte(`{}`),
	}); err != nil {
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
		TelegramUserID: row.ChannelUserID,
	}, nil
}

func validateBindingTokenChannel(row db.ChannelBindingToken) error {
	if row.ChannelType != string(TypeTelegram) {
		return ErrBindingTokenInvalid
	}
	return nil
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
