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

// Identity is the resolved, server-authoritative result of authenticating a
// service token. It carries no user identity — a service token is a machine
// actor bound to a workspace with an explicit scope set.
type Identity struct {
	TokenID     string
	WorkspaceID string
	Scopes      []string
	// CreatedBy is the user who minted the token. It is NOT the caller's
	// identity (a service token never resolves to a user for authz); it is
	// carried only so a write performed via the token can be attributed to a
	// real actor in the human-modeled issue tables. Empty when the minting
	// user has since been deleted.
	CreatedBy string
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
	})
	if err != nil {
		return Token{}, "", err
	}
	s.audit(ctx, AuditEvent{
		TokenID:     tok.ID,
		WorkspaceID: workspaceID,
		Event:       "issued",
		ActorUserID: createdBy,
		Detail:      auditDetail(map[string]any{"name": name, "scopes": normScopes}),
	})
	return tok, raw, nil
}

// List returns the workspace's tokens (never the raw secret or hash).
func (s *TokenService) List(ctx context.Context, workspaceID string) ([]Token, error) {
	return s.store.ListByWorkspace(ctx, workspaceID)
}

// Revoke marks a token revoked. It is idempotent and workspace-scoped: a
// token belonging to another workspace, or already gone, yields revoked=false
// without error. On a real revoke it writes an audit row.
func (s *TokenService) Revoke(ctx context.Context, tokenID, workspaceID, actorUserID string) (bool, error) {
	tok, err := s.store.Revoke(ctx, tokenID, workspaceID)
	if err != nil {
		if errors.Is(err, errNoRows) {
			return false, nil
		}
		return false, err
	}
	s.audit(ctx, AuditEvent{
		TokenID:     tok.ID,
		WorkspaceID: workspaceID,
		Event:       "revoked",
		ActorUserID: actorUserID,
	})
	return true, nil
}

// Authenticate resolves a raw "msv_" token to an Identity. The store query
// already excludes revoked/expired rows, so a non-nil result is a live token.
// last_used_at is bumped best-effort (the "use" record — matching the PAT
// precedent — without flooding the audit table).
func (s *TokenService) Authenticate(ctx context.Context, rawToken string) (*Identity, error) {
	tok, err := s.store.GetByHash(ctx, auth.HashToken(rawToken))
	if err != nil {
		return nil, err
	}
	if err := s.store.Touch(ctx, tok.ID); err != nil {
		slog.Warn("service token: failed to bump last_used_at", "token_id", tok.ID, "error", err)
	}
	return &Identity{TokenID: tok.ID, WorkspaceID: tok.WorkspaceID, Scopes: tok.Scopes, CreatedBy: tok.CreatedBy}, nil
}

func (s *TokenService) audit(ctx context.Context, e AuditEvent) {
	if err := s.store.AppendAudit(ctx, e); err != nil {
		// Audit is best-effort: never fail a mint/revoke because the audit
		// insert failed, but make the gap loud.
		slog.Error("service token: audit write failed", "event", e.Event, "token_id", e.TokenID, "error", err)
	}
}

func auditDetail(v map[string]any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
