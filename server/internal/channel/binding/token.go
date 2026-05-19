// Package binding implements the one-shot token issuance and consumption
// flow that links an external IM (e.g. Feishu) user to a Multica user.
//
// Security properties (DESIGN §6 risk 3):
//   - Plaintext tokens are 32 random bytes (crypto/rand) base64url-encoded.
//   - Only the SHA-256 hash of the plaintext is ever persisted; the
//     plaintext is delivered to the user via a private IM channel and
//     never sees the database.
//   - Tokens expire after a fixed TTL (default 10 minutes, PRD AC3.4).
//   - Consumption is one-shot, enforced by an UPDATE ... WHERE
//     consumed_at IS NULL ... predicate that returns RowsAffected == 1
//     for the single winning consumer (TC-risk-token-replay).
package binding

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DefaultTokenTTL is the lifetime of an issued binding token. Matches
// PRD AC3.4 (10 minutes).
const DefaultTokenTTL = 10 * time.Minute

const (
	PurposeUserIdentity  = "user_identity"
	PurposeChatWorkspace = "chat_workspace"
)

// Sentinel errors. Callers use errors.Is to discriminate; we never match
// against error strings.
var (
	// ErrTokenInvalid is returned when no row exists for the supplied
	// plaintext, or the token has been tampered with.
	ErrTokenInvalid = errors.New("binding: token invalid")

	// ErrTokenExpired is returned when the token's expires_at is in the
	// past at the moment of consumption.
	ErrTokenExpired = errors.New("binding: token expired")

	// ErrTokenAlreadyConsumed is returned when a token was already
	// consumed by an earlier request. The DB-level UPDATE predicate is
	// what makes the second consumer lose the race.
	ErrTokenAlreadyConsumed = errors.New("binding: token already consumed")
)

// TokenStore is the narrow subset of *db.Queries that the issuer/consumer
// depend on. Defining an interface here lets unit tests inject fakes that
// capture the parameters without touching Postgres, while production code
// passes the real *db.Queries (which satisfies the interface implicitly).
type TokenStore interface {
	CreateChannelBindToken(ctx context.Context, arg db.CreateChannelBindTokenParams) (db.ChannelBindToken, error)
	ConsumeChannelBindToken(ctx context.Context, tokenHash []byte) (db.ChannelBindToken, error)
	GetChannelBindToken(ctx context.Context, tokenHash []byte) (db.ChannelBindToken, error)
}

// IssueResult is the public return shape of TokenIssuer.Issue. Only the
// plaintext leaves the server boundary (delivered to the user through the
// IM private channel); the hash and expires_at fields are returned so the
// caller can log / audit without re-deriving them.
type IssueResult struct {
	// Plaintext is the base64url-encoded random token. MUST be sent to
	// the user over a confidential channel and MUST NOT be logged or
	// persisted server-side.
	Plaintext string
	// ExpiresAt is the absolute deadline after which consumption fails
	// with ErrTokenExpired.
	ExpiresAt time.Time
}

type IssueChatWorkspaceReq struct {
	Provider                string
	ConnectionID            string
	InitiatorExternalUserID string
	ExternalChatID          string
	ExternalChatType        string
	ExternalChatName        string
}

// TokenIssuer issues one-shot binding tokens. It is safe for concurrent
// use; all state lives in the underlying TokenStore.
type TokenIssuer struct {
	store TokenStore
}

// NewTokenIssuer constructs a TokenIssuer backed by store. Callers in
// production pass *db.Queries; tests pass a fake.
func NewTokenIssuer(store TokenStore) *TokenIssuer {
	return &TokenIssuer{store: store}
}

// Issue creates a fresh one-shot token for the given (provider,
// externalUserID) pair. The returned IssueResult.Plaintext is the only
// place the plaintext exists; the database only ever sees the SHA-256
// hash (DESIGN §6 risk 3).
func (i *TokenIssuer) Issue(ctx context.Context, provider, externalUserID string) (IssueResult, error) {
	return i.IssueUserIdentity(ctx, provider, externalUserID)
}

func (i *TokenIssuer) IssueUserIdentity(ctx context.Context, provider, externalUserID string) (IssueResult, error) {
	return i.IssueUserIdentityForConnection(ctx, provider, provider, externalUserID)
}

func (i *TokenIssuer) IssueUserIdentityForConnection(ctx context.Context, provider, connectionID, externalUserID string) (IssueResult, error) {
	if connectionID == "" {
		connectionID = provider
	}
	return i.issue(ctx, db.CreateChannelBindTokenParams{
		Purpose:        PurposeUserIdentity,
		Provider:       provider,
		ConnectionID:   connectionID,
		ExternalUserID: externalUserID,
	})
}

func (i *TokenIssuer) IssueChatWorkspace(ctx context.Context, req IssueChatWorkspaceReq) (IssueResult, error) {
	if req.ConnectionID == "" {
		req.ConnectionID = req.Provider
	}
	return i.issue(ctx, db.CreateChannelBindTokenParams{
		Purpose:          PurposeChatWorkspace,
		Provider:         req.Provider,
		ConnectionID:     req.ConnectionID,
		ExternalUserID:   req.InitiatorExternalUserID,
		ExternalChatID:   pgtype.Text{String: req.ExternalChatID, Valid: req.ExternalChatID != ""},
		ExternalChatType: pgtype.Text{String: req.ExternalChatType, Valid: req.ExternalChatType != ""},
		ExternalChatName: pgtype.Text{String: req.ExternalChatName, Valid: req.ExternalChatName != ""},
	})
}

func (i *TokenIssuer) issue(ctx context.Context, params db.CreateChannelBindTokenParams) (IssueResult, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return IssueResult{}, err
	}
	plaintext := base64.RawURLEncoding.EncodeToString(raw)

	hash := sha256.Sum256([]byte(plaintext))
	expiresAt := time.Now().Add(DefaultTokenTTL)

	params.TokenHash = hash[:]
	params.ExpiresAt = pgtype.Timestamptz{Time: expiresAt, Valid: true}
	_, err := i.store.CreateChannelBindToken(ctx, db.CreateChannelBindTokenParams{
		TokenHash:        params.TokenHash,
		Purpose:          params.Purpose,
		Provider:         params.Provider,
		ConnectionID:     params.ConnectionID,
		ExternalUserID:   params.ExternalUserID,
		ExternalChatID:   params.ExternalChatID,
		ExternalChatType: params.ExternalChatType,
		ExternalChatName: params.ExternalChatName,
		ExpiresAt:        params.ExpiresAt,
	})
	if err != nil {
		return IssueResult{}, err
	}

	return IssueResult{Plaintext: plaintext, ExpiresAt: expiresAt}, nil
}

// TokenConsumer consumes a previously-issued plaintext token. It is the
// single chokepoint for the "use the link to bind" flow.
type TokenConsumer struct {
	store TokenStore
}

// NewTokenConsumer constructs a TokenConsumer backed by store.
func NewTokenConsumer(store TokenStore) *TokenConsumer {
	return &TokenConsumer{store: store}
}

func (c *TokenConsumer) Peek(ctx context.Context, plaintext string) (db.ChannelBindToken, error) {
	hash, err := hashPlaintext(plaintext)
	if err != nil {
		return db.ChannelBindToken{}, err
	}
	row, err := c.store.GetChannelBindToken(ctx, hash[:])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ChannelBindToken{}, ErrTokenInvalid
		}
		return db.ChannelBindToken{}, err
	}
	if row.ExpiresAt.Valid && row.ExpiresAt.Time.Before(time.Now()) {
		return db.ChannelBindToken{}, ErrTokenExpired
	}
	if row.ConsumedAt.Valid {
		return db.ChannelBindToken{}, ErrTokenAlreadyConsumed
	}
	return row, nil
}

// Consume validates and burns the supplied plaintext. On success it
// returns the row that was just marked consumed (so the caller can read
// provider + external_user_id and proceed to write the user binding).
//
// Failure modes:
//   - ErrTokenInvalid          — no such token, or already consumed/expired
//     in a way that can't be distinguished without
//     a second query (consumer should treat all of
//     these as "click the new link").
//   - ErrTokenExpired          — token row exists but is past expires_at.
//   - ErrTokenAlreadyConsumed  — token row exists, not expired, but
//     consumed_at is non-null.
func (c *TokenConsumer) Consume(ctx context.Context, plaintext string) (db.ChannelBindToken, error) {
	// Defence-in-depth: reject obviously malformed input before touching
	// the database. This prevents a caller from forwarding arbitrary user
	// input (e.g. empty string, non-base64url) straight to a DB round-trip.
	hash, err := hashPlaintext(plaintext)
	if err != nil {
		return db.ChannelBindToken{}, err
	}

	// First attempt: optimistic UPDATE ... WHERE consumed_at IS NULL AND
	// expires_at > now(). If this succeeds, we win the race.
	row, err := c.store.ConsumeChannelBindToken(ctx, hash[:])
	if err == nil {
		return row, nil
	}

	// If the UPDATE returned no rows, it could mean:
	//   1. The token never existed (tampered / typo).
	//   2. The token is expired.
	//   3. The token was already consumed.
	// We disambiguate 2 and 3 with a follow-up SELECT so the caller can
	// surface a more precise error message.
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := c.store.GetChannelBindToken(ctx, hash[:])
		if getErr != nil {
			return db.ChannelBindToken{}, ErrTokenInvalid
		}
		if existing.ExpiresAt.Valid && existing.ExpiresAt.Time.Before(time.Now()) {
			return db.ChannelBindToken{}, ErrTokenExpired
		}
		if existing.ConsumedAt.Valid {
			return db.ChannelBindToken{}, ErrTokenAlreadyConsumed
		}
		// Shouldn't reach here (no rows from UPDATE but row exists and
		// looks valid). Treat as invalid to be safe.
		return db.ChannelBindToken{}, ErrTokenInvalid
	}

	return db.ChannelBindToken{}, err
}

func hashPlaintext(plaintext string) ([32]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(plaintext)
	if err != nil || len(raw) < 32 {
		return [32]byte{}, ErrTokenInvalid
	}
	return sha256.Sum256([]byte(plaintext)), nil
}
