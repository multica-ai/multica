package servicetoken

import (
	"context"
	"time"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// Token is the domain view of a service token row — plain Go types so the
// service and handlers never touch pgtype/JSONB directly, and so tests can
// use an in-memory Store.
type Token struct {
	ID          string
	WorkspaceID string
	Name        string
	TokenPrefix string
	Scopes      []string
	ExpiresAt   *time.Time
	LastUsedAt  *time.Time
	Revoked     bool
	CreatedBy   string // empty when the minting user has since been deleted
	CreatedAt   time.Time
}

// CreateParams is the domain input for minting a token. Scopes must already
// be normalized (validated + deduped) by the caller.
type CreateParams struct {
	WorkspaceID string
	Name        string
	TokenHash   string
	TokenPrefix string
	Scopes      []string
	ExpiresAt   *time.Time
	CreatedBy   string
}

// AuditEvent is a lifecycle event: "issued" or "revoked". (Per-request "use"
// is recorded on the token's last_used_at column, matching the PAT
// precedent, to avoid flooding the audit table.)
type AuditEvent struct {
	TokenID     string
	WorkspaceID string
	Event       string
	ActorUserID string // the human who issued/revoked
	Detail      []byte // optional JSON
}

// Store is the persistence seam. *cerebrodb.Queries satisfies it via
// cerebroStore; tests inject a fake.
type Store interface {
	Create(ctx context.Context, p CreateParams) (Token, error)
	GetByHash(ctx context.Context, tokenHash string) (Token, error)
	Touch(ctx context.Context, tokenID string) error
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Token, error)
	Revoke(ctx context.Context, tokenID, workspaceID string) (Token, error)
	AppendAudit(ctx context.Context, e AuditEvent) error
}

// cerebroStore adapts the sqlc-generated cerebrodb.Queries to Store.
type cerebroStore struct {
	q *cerebrodb.Queries
}

// NewStore returns a Store backed by the cerebro sqlc queries.
func NewStore(q *cerebrodb.Queries) Store { return &cerebroStore{q: q} }

func (s *cerebroStore) Create(ctx context.Context, p CreateParams) (Token, error) {
	scopesJSON, err := marshalScopes(p.Scopes)
	if err != nil {
		return Token{}, err
	}
	row, err := s.q.CreateCerebroServiceToken(ctx, cerebrodb.CreateCerebroServiceTokenParams{
		WorkspaceID: toUUID(p.WorkspaceID),
		Name:        p.Name,
		TokenHash:   p.TokenHash,
		TokenPrefix: p.TokenPrefix,
		Scopes:      scopesJSON,
		ExpiresAt:   timePtrToTs(p.ExpiresAt),
		CreatedBy:   uuidOrNil(p.CreatedBy),
	})
	if err != nil {
		return Token{}, err
	}
	return rowToToken(row)
}

func (s *cerebroStore) GetByHash(ctx context.Context, tokenHash string) (Token, error) {
	row, err := s.q.GetCerebroServiceTokenByHash(ctx, tokenHash)
	if err != nil {
		return Token{}, err
	}
	return rowToToken(row)
}

func (s *cerebroStore) Touch(ctx context.Context, tokenID string) error {
	return s.q.TouchCerebroServiceToken(ctx, toUUID(tokenID))
}

func (s *cerebroStore) ListByWorkspace(ctx context.Context, workspaceID string) ([]Token, error) {
	rows, err := s.q.ListCerebroServiceTokensByWorkspace(ctx, toUUID(workspaceID))
	if err != nil {
		return nil, err
	}
	out := make([]Token, 0, len(rows))
	for _, row := range rows {
		t, err := rowToToken(row)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func (s *cerebroStore) Revoke(ctx context.Context, tokenID, workspaceID string) (Token, error) {
	row, err := s.q.RevokeCerebroServiceToken(ctx, cerebrodb.RevokeCerebroServiceTokenParams{
		ID:          toUUID(tokenID),
		WorkspaceID: toUUID(workspaceID),
	})
	if err != nil {
		return Token{}, err
	}
	return rowToToken(row)
}

func (s *cerebroStore) AppendAudit(ctx context.Context, e AuditEvent) error {
	return s.q.AppendCerebroServiceTokenAudit(ctx, cerebrodb.AppendCerebroServiceTokenAuditParams{
		ServiceTokenID: toUUID(e.TokenID),
		WorkspaceID:    toUUID(e.WorkspaceID),
		Event:          e.Event,
		ActorUserID:    uuidOrNil(e.ActorUserID),
		Detail:         e.Detail,
	})
}

func rowToToken(row cerebrodb.CerebroServiceToken) (Token, error) {
	scopes, err := unmarshalScopes(row.Scopes)
	if err != nil {
		return Token{}, err
	}
	return Token{
		ID:          util.UUIDToString(row.ID),
		WorkspaceID: util.UUIDToString(row.WorkspaceID),
		Name:        row.Name,
		TokenPrefix: row.TokenPrefix,
		Scopes:      scopes,
		ExpiresAt:   tsToPtr(row.ExpiresAt),
		LastUsedAt:  tsToPtr(row.LastUsedAt),
		Revoked:     row.Revoked,
		CreatedBy:   util.UUIDToString(row.CreatedBy),
		CreatedAt:   row.CreatedAt.Time,
	}, nil
}
