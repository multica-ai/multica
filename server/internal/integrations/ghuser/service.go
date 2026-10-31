// Package ghuser holds a Multica user's personal GitHub authorization: the
// GitHub App user-to-server OAuth flow, the encrypted token store, and a thin
// REST / GraphQL client that sends every request as that user (MUL-8433).
//
// PR writes made from Multica go to GitHub with the clicking person's own
// token, so GitHub applies their repository permissions and branch protection.
// The installation token is never used for writes.
//
// Credential hygiene: access and refresh tokens are sealed at rest and are
// never written to a log, an error message, or a response body.
package ghuser

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	defaultOAuthBase = "https://github.com"
	defaultAPIBase   = "https://api.github.com"

	// An access token this close to expiry is refreshed before use, so a
	// request never races the expiry boundary.
	refreshSkew = 5 * time.Minute
	// Bounds a refresh, including the row lock it holds.
	refreshTimeout = 20 * time.Second
)

var (
	// ErrNotConfigured means the deployment lacks the App client ID, client
	// secret, or MULTICA_GITHUB_SECRET_KEY.
	ErrNotConfigured = errors.New("ghuser: github user authorization is not configured")
	// ErrNotConnected means the user has not connected a GitHub account.
	ErrNotConnected = errors.New("ghuser: github account not connected")
	// ErrReauthRequired means the authorization expired or was revoked. The
	// stored connection has been removed; the user must connect again.
	ErrReauthRequired = errors.New("ghuser: github authorization must be renewed")
)

// TxBeginner is the subset of a pgx pool the refresh path needs to hold the
// per-user row lock. *pgxpool.Pool satisfies it.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Config is the deployment-level GitHub App OAuth configuration.
type Config struct {
	ClientID     string
	ClientSecret string
	// SecretKey is MULTICA_GITHUB_SECRET_KEY: 32 bytes that seal tokens at rest
	// and, under a separate derivation, sign the OAuth state.
	SecretKey []byte
}

// Service owns the OAuth flow and the token store. A nil *Service is a valid
// "not configured" value; Configured reports false for it.
type Service struct {
	clientID     string
	clientSecret string
	box          *secretbox.Box
	stateKey     []byte
	queries      *db.Queries
	pool         TxBeginner

	oauthBase  string
	apiBase    string
	httpClient *http.Client
	now        func() time.Time
}

// New builds a Service. Every Config field is required.
func New(cfg Config, queries *db.Queries, pool TxBeginner) (*Service, error) {
	if strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.ClientSecret) == "" {
		return nil, errors.New("ghuser: client id and client secret are required")
	}
	box, err := secretbox.New(cfg.SecretKey)
	if err != nil {
		return nil, err
	}
	return &Service{
		clientID:     strings.TrimSpace(cfg.ClientID),
		clientSecret: strings.TrimSpace(cfg.ClientSecret),
		box:          box,
		stateKey:     deriveStateKey(cfg.SecretKey),
		queries:      queries,
		pool:         pool,
		oauthBase:    defaultOAuthBase,
		apiBase:      defaultAPIBase,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		now:          time.Now,
	}, nil
}

// NewFromEnv reads GITHUB_APP_CLIENT_ID, GITHUB_APP_CLIENT_SECRET and
// MULTICA_GITHUB_SECRET_KEY. It returns (nil, nil) when any of them is unset,
// so the feature reports configured=false and the server starts normally; a
// malformed key is returned as an error for the operator.
func NewFromEnv(queries *db.Queries, pool TxBeginner) (*Service, error) {
	clientID := strings.TrimSpace(os.Getenv("GITHUB_APP_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("GITHUB_APP_CLIENT_SECRET"))
	if clientID == "" || clientSecret == "" || os.Getenv("MULTICA_GITHUB_SECRET_KEY") == "" {
		return nil, nil
	}
	key, err := secretbox.LoadKey("MULTICA_GITHUB_SECRET_KEY")
	if err != nil {
		return nil, err
	}
	return New(Config{ClientID: clientID, ClientSecret: clientSecret, SecretKey: key}, queries, pool)
}

// SetEndpoints points the service at other GitHub hosts. Tests use it to
// target an httptest server.
func (s *Service) SetEndpoints(oauthBase, apiBase string) {
	s.oauthBase = strings.TrimRight(oauthBase, "/")
	s.apiBase = strings.TrimRight(apiBase, "/")
}

// SetClock replaces the time source. Tests use it to exercise expiry.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Configured reports whether the feature can be used on this deployment.
func (s *Service) Configured() bool { return s != nil && s.box != nil }

// ClientID is the GitHub App client ID, used to build the authorize URL.
func (s *Service) ClientID() string { return s.clientID }

func deriveStateKey(secretKey []byte) []byte {
	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte("multica/github-user-oauth-state/v1"))
	return mac.Sum(nil)
}

// Account is the GitHub identity a user connected.
type Account struct {
	GitHubUserID int64
	Login        string
	AvatarURL    string
}

// Token is a usable access token plus the identity it acts as.
type Token struct {
	Account
	AccessToken string
}

// Status reports the stored connection without touching GitHub. connected is
// false when the stored authorization can no longer be renewed.
func (s *Service) Status(ctx context.Context, userID pgtype.UUID) (Account, bool, error) {
	if !s.Configured() {
		return Account{}, false, ErrNotConfigured
	}
	row, err := s.queries.GetGitHubUserConnection(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, false, nil
	}
	if err != nil {
		return Account{}, false, err
	}
	usable := !s.needsRefresh(row) || s.refreshUsable(row)
	return accountFromRow(row), usable, nil
}

// AccessToken returns a usable access token for the user, refreshing it when
// it expires within refreshSkew. Refreshes are serialized per user by a row
// lock, so concurrent callers trigger a single refresh and all receive the
// rotated token.
func (s *Service) AccessToken(ctx context.Context, userID pgtype.UUID) (Token, error) {
	if !s.Configured() {
		return Token{}, ErrNotConfigured
	}
	row, err := s.queries.GetGitHubUserConnection(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Token{}, ErrNotConnected
	}
	if err != nil {
		return Token{}, err
	}
	if !s.needsRefresh(row) {
		access, err := s.box.Open(row.AccessTokenEncrypted)
		if err != nil {
			// Sealed under a different key: unusable until reconnected.
			_ = s.queries.DeleteGitHubUserConnection(ctx, userID)
			return Token{}, ErrReauthRequired
		}
		return Token{Account: accountFromRow(row), AccessToken: string(access)}, nil
	}
	return s.refresh(ctx, userID)
}

func (s *Service) refresh(ctx context.Context, userID pgtype.UUID) (Token, error) {
	// Once GitHub answers, the old refresh token is already spent. A caller
	// that disconnects mid-refresh must not roll the rotated pair back, so the
	// refresh and its commit run on a context detached from the request.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
	defer cancel()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Token{}, err
	}
	defer tx.Rollback(ctx)
	q := s.queries.WithTx(tx)

	row, err := q.LockGitHubUserConnection(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Token{}, ErrNotConnected
	}
	if err != nil {
		return Token{}, err
	}
	forget := func() (Token, error) {
		if err := q.DeleteGitHubUserConnection(ctx, userID); err != nil {
			return Token{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Token{}, err
		}
		return Token{}, ErrReauthRequired
	}

	// Another request refreshed while this one waited for the lock.
	if !s.needsRefresh(row) {
		access, err := s.box.Open(row.AccessTokenEncrypted)
		if err != nil {
			return forget()
		}
		if err := tx.Commit(ctx); err != nil {
			return Token{}, err
		}
		return Token{Account: accountFromRow(row), AccessToken: string(access)}, nil
	}

	if !s.refreshUsable(row) {
		return forget()
	}
	refreshToken, err := s.box.Open(row.RefreshTokenEncrypted)
	if err != nil {
		return forget()
	}
	tokens, err := s.refreshTokens(ctx, string(refreshToken))
	if err != nil {
		var oauthErr *oauthError
		if errors.As(err, &oauthErr) && oauthErr.revokesConnection() {
			return forget()
		}
		return Token{}, err
	}
	params, err := s.sealTokens(tokens)
	if err != nil {
		return Token{}, err
	}
	// GitHub returns a new refresh token with every refresh; keep the stored
	// one only if a response ever omits it.
	if params.RefreshTokenEncrypted == nil {
		params.RefreshTokenEncrypted = row.RefreshTokenEncrypted
		params.RefreshTokenExpiresAt = row.RefreshTokenExpiresAt
	}
	if err := q.UpdateGitHubUserConnectionTokens(ctx, db.UpdateGitHubUserConnectionTokensParams{
		UserID:                userID,
		AccessTokenEncrypted:  params.AccessTokenEncrypted,
		AccessTokenExpiresAt:  params.AccessTokenExpiresAt,
		RefreshTokenEncrypted: params.RefreshTokenEncrypted,
		RefreshTokenExpiresAt: params.RefreshTokenExpiresAt,
	}); err != nil {
		return Token{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Token{}, err
	}
	return Token{Account: accountFromRow(row), AccessToken: tokens.AccessToken}, nil
}

// Connect completes the OAuth flow for userID: it exchanges the code, reads
// the GitHub identity, and stores the sealed tokens.
func (s *Service) Connect(ctx context.Context, userID pgtype.UUID, code, redirectURI string) (Account, error) {
	if !s.Configured() {
		return Account{}, ErrNotConfigured
	}
	tokens, err := s.exchangeCode(ctx, code, redirectURI)
	if err != nil {
		return Account{}, err
	}
	account, err := s.fetchViewer(ctx, tokens.AccessToken)
	if err != nil {
		return Account{}, err
	}
	sealed, err := s.sealTokens(tokens)
	if err != nil {
		return Account{}, err
	}
	avatar := pgtype.Text{}
	if account.AvatarURL != "" {
		avatar = pgtype.Text{String: account.AvatarURL, Valid: true}
	}
	if _, err := s.queries.UpsertGitHubUserConnection(ctx, db.UpsertGitHubUserConnectionParams{
		UserID:                userID,
		GithubUserID:          account.GitHubUserID,
		Login:                 account.Login,
		AvatarUrl:             avatar,
		AccessTokenEncrypted:  sealed.AccessTokenEncrypted,
		AccessTokenExpiresAt:  sealed.AccessTokenExpiresAt,
		RefreshTokenEncrypted: sealed.RefreshTokenEncrypted,
		RefreshTokenExpiresAt: sealed.RefreshTokenExpiresAt,
	}); err != nil {
		return Account{}, fmt.Errorf("ghuser: store connection: %w", err)
	}
	return account, nil
}

// Disconnect revokes the user's grant on GitHub and deletes the stored
// connection. The row is deleted even when revocation fails; the returned
// error only reports a failed delete.
func (s *Service) Disconnect(ctx context.Context, userID pgtype.UUID) (revoked bool, err error) {
	if !s.Configured() {
		return false, ErrNotConfigured
	}
	if tok, err := s.AccessToken(ctx, userID); err == nil {
		revoked = s.revokeGrant(ctx, tok.AccessToken) == nil
	} else if errors.Is(err, ErrNotConnected) || errors.Is(err, ErrReauthRequired) {
		return false, nil
	}
	return revoked, s.queries.DeleteGitHubUserConnection(ctx, userID)
}

// Forget deletes the stored connection without contacting GitHub. Callers use
// it when GitHub rejects the access token itself (401).
func (s *Service) Forget(ctx context.Context, userID pgtype.UUID) error {
	if !s.Configured() {
		return ErrNotConfigured
	}
	return s.queries.DeleteGitHubUserConnection(ctx, userID)
}

func (s *Service) needsRefresh(row db.GithubUserConnection) bool {
	return row.AccessTokenExpiresAt.Valid && !s.now().Add(refreshSkew).Before(row.AccessTokenExpiresAt.Time)
}

func (s *Service) refreshUsable(row db.GithubUserConnection) bool {
	if len(row.RefreshTokenEncrypted) == 0 {
		return false
	}
	return !row.RefreshTokenExpiresAt.Valid || s.now().Before(row.RefreshTokenExpiresAt.Time)
}

type sealedTokens struct {
	AccessTokenEncrypted  []byte
	AccessTokenExpiresAt  pgtype.Timestamptz
	RefreshTokenEncrypted []byte
	RefreshTokenExpiresAt pgtype.Timestamptz
}

func (s *Service) sealTokens(t tokenResponse) (sealedTokens, error) {
	now := s.now()
	access, err := s.box.Seal([]byte(t.AccessToken))
	if err != nil {
		return sealedTokens{}, err
	}
	out := sealedTokens{AccessTokenEncrypted: access}
	if t.ExpiresIn > 0 {
		out.AccessTokenExpiresAt = pgtype.Timestamptz{Time: now.Add(time.Duration(t.ExpiresIn) * time.Second), Valid: true}
	}
	if t.RefreshToken != "" {
		refresh, err := s.box.Seal([]byte(t.RefreshToken))
		if err != nil {
			return sealedTokens{}, err
		}
		out.RefreshTokenEncrypted = refresh
		if t.RefreshTokenExpiresIn > 0 {
			out.RefreshTokenExpiresAt = pgtype.Timestamptz{Time: now.Add(time.Duration(t.RefreshTokenExpiresIn) * time.Second), Valid: true}
		}
	}
	return out, nil
}

func accountFromRow(row db.GithubUserConnection) Account {
	return Account{GitHubUserID: row.GithubUserID, Login: row.Login, AvatarURL: row.AvatarUrl.String}
}
