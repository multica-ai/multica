package servicetoken

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
)

// ErrNotConfigured is returned by Authenticate when no service is wired (e.g.
// a router built for inspection with a nil pool). The auth middleware treats
// it as an invalid token and returns 401 — fail closed.
var ErrNotConfigured = errors.New("service token subsystem not configured")

var (
	ErrDisabled       = errors.New("service token feature is disabled")
	ErrExpiryRequired = errors.New("service token expiry is required")
	ErrExpiryInvalid  = errors.New("service token expiry must be in the future and no more than 365 days")
)

const (
	FlagKey       = "cerebro_service_tokens"
	MaxExpiryDays = 365
)

// Identity is the resolved, server-authoritative result of authenticating a
// service token. It carries no user identity — a service token is a machine
// actor bound to a workspace with an explicit scope set.
type Identity struct {
	TokenID     string
	WorkspaceID string
	Scopes      []string
}

type auditRequestKey struct{}

type auditRequest struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

func withAuditRequest(ctx context.Context, method, path string) context.Context {
	return context.WithValue(ctx, auditRequestKey{}, auditRequest{Method: method, Path: path})
}

// TokenService owns the credential lifecycle: mint, list, revoke, and
// authenticate. It depends only on Store, so it is fully unit-testable.
type TokenService struct {
	store Store
}

// NewTokenService builds the service over a Store.
func NewTokenService(store Store) *TokenService { return &TokenService{store: store} }

// Mint creates a new service token and returns the persisted row plus the raw
// secret (shown to the caller exactly once — never stored, never retrievable
// again). Scopes are validated here; an unknown or empty scope set is
// rejected before anything is written.
func (s *TokenService) Mint(ctx context.Context, workspaceID, name string, scopes []string, expiresAt *time.Time, createdBy string) (Token, string, error) {
	enabled, err := s.Enabled(ctx, workspaceID)
	if err != nil {
		return Token{}, "", err
	}
	if !enabled {
		return Token{}, "", ErrDisabled
	}
	if expiresAt == nil {
		return Token{}, "", ErrExpiryRequired
	}
	now := time.Now()
	if !expiresAt.After(now) || expiresAt.After(now.Add(MaxExpiryDays*24*time.Hour)) {
		return Token{}, "", ErrExpiryInvalid
	}
	normScopes, err := NormalizeScopes(scopes)
	if err != nil {
		return Token{}, "", err
	}
	raw, err := GenerateServiceToken()
	if err != nil {
		return Token{}, "", err
	}
	tok, err := s.store.Create(ctx, CreateParams{
		WorkspaceID: workspaceID,
		Name:        name,
		TokenHash:   auth.HashToken(raw),
		TokenPrefix: tokenPrefix(raw),
		Scopes:      normScopes,
		ExpiresAt:   expiresAt,
		CreatedBy:   createdBy,
		AuditDetail: auditDetail(map[string]any{"name": name, "scopes": normScopes}),
	})
	if err != nil {
		return Token{}, "", err
	}
	return tok, raw, nil
}

// Enabled resolves the workspace-level kill switch. Missing configuration and
// lookup errors fail closed at callers.
func (s *TokenService) Enabled(ctx context.Context, workspaceID string) (bool, error) {
	return s.store.FeatureEnabled(ctx, workspaceID)
}

// List returns the workspace's tokens (never the raw secret or hash).
func (s *TokenService) List(ctx context.Context, workspaceID string) ([]Token, error) {
	return s.store.ListByWorkspace(ctx, workspaceID)
}

// Revoke marks a token revoked. It is idempotent and workspace-scoped: a
// token belonging to another workspace, or already gone, yields revoked=false
// without error. On a real revoke it writes an audit row.
func (s *TokenService) Revoke(ctx context.Context, tokenID, workspaceID, actorUserID string) (bool, error) {
	_, err := s.store.Revoke(ctx, tokenID, workspaceID, actorUserID)
	if err != nil {
		if errors.Is(err, errNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Authenticate resolves a raw "msv_" token to an Identity. Authentication is
// accepted only after the kill switch is confirmed ON and a durable per-use
// audit row has been written. Failed audit persistence fails closed.
func (s *TokenService) Authenticate(ctx context.Context, rawToken string) (*Identity, error) {
	tok, err := s.store.GetByHash(ctx, auth.HashToken(rawToken))
	if err != nil {
		return nil, err
	}
	if tok.ExpiresAt == nil || !tok.ExpiresAt.After(time.Now()) || tok.Revoked {
		return nil, errNoRows
	}
	enabled, err := s.Enabled(ctx, tok.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, ErrDisabled
	}
	req, _ := ctx.Value(auditRequestKey{}).(auditRequest)
	if err := s.store.AppendAudit(ctx, AuditEvent{
		TokenID:     tok.ID,
		WorkspaceID: tok.WorkspaceID,
		Event:       "used",
		Detail:      auditDetail(map[string]any{"method": req.Method, "path": req.Path}),
	}); err != nil {
		return nil, err
	}
	if err := s.store.Touch(ctx, tok.ID); err != nil {
		slog.Warn("service token: failed to bump last_used_at", "token_id", tok.ID, "error", err)
	}
	return &Identity{TokenID: tok.ID, WorkspaceID: tok.WorkspaceID, Scopes: tok.Scopes}, nil
}

func auditDetail(v map[string]any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
