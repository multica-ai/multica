package wecom

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

var (
	// ErrBindingTokenInvalid: token unknown / already consumed / expired.
	ErrBindingTokenInvalid = errors.New("wecom: binding token invalid or expired")
	// ErrBindingAlreadyAssigned: this WeCom userid is already bound to a
	// different Multica user.
	ErrBindingAlreadyAssigned = errors.New("wecom: user id is already bound to a different user")
	// ErrBindingNotWorkspaceMember: the redeemer is not a member of the token's
	// workspace.
	ErrBindingNotWorkspaceMember = errors.New("wecom: redeemer is not a workspace member")
)

// BindingToken is a freshly minted token. The raw value is returned exactly
// once; only its hash is persisted (spec §6.2).
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

// BindingTokenService mints and redeems WeCom binding tokens.
type BindingTokenService struct {
	q   *db.Queries
	tx  engine.TxStarter
	now func() time.Time
}

// NewBindingTokenService constructs the service.
func NewBindingTokenService(q *db.Queries, tx engine.TxStarter) *BindingTokenService {
	return &BindingTokenService{q: q, tx: tx, now: time.Now}
}

// Mint creates a single-use binding token for (installation, wecomUserID).
// Raw token must be delivered over WeCom DM only; never log or persist it.
func (s *BindingTokenService) Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, wecomUserID string) (BindingToken, error) {
	raw, err := randomBindingToken(32)
	if err != nil {
		return BindingToken{}, fmt.Errorf("generate token: %w", err)
	}
	expiresAt := s.now().Add(BindingTokenTTL)
	if _, err := s.q.CreateChannelBindingToken(ctx, db.CreateChannelBindingTokenParams{
		TokenHash:      hashBindingToken(raw),
		WorkspaceID:    workspaceID,
		InstallationID: installationID,
		ChannelType:    string(TypeWecom),
		ChannelUserID:  wecomUserID,
		ExpiresAt:      pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		return BindingToken{}, fmt.Errorf("persist token: %w", err)
	}
	return BindingToken{Raw: raw, ExpiresAt: expiresAt}, nil
}

// RedeemAndBind atomically consumes a raw token and binds the WeCom userid to
// multicaUserID from the session.
func (s *BindingTokenService) RedeemAndBind(ctx context.Context, raw string, multicaUserID pgtype.UUID) (RedeemedBindingToken, error) {
	if s.tx == nil {
		return RedeemedBindingToken{}, errors.New("wecom: BindingTokenService missing TxStarter")
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
		ChannelType:    string(TypeWecom),
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
