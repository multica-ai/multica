package conversationctx

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EntityType is P0 only issue; P1 will expand to pr / project.
type EntityType string

const (
	EntityTypeIssue EntityType = "issue"
)

// EntityRef is a single entity record in conversation context.
type EntityRef struct {
	Key         string     `json:"key"`
	ID          string     `json:"id,omitempty"`
	Type        EntityType `json:"type"`
	Display     string     `json:"display,omitempty"`
	URL         string     `json:"url,omitempty"`
	MentionedAt time.Time  `json:"mentioned_at"`
}

// Scope is the isolation dimension for conversation context.
type Scope struct {
	ConnectionID string
	WorkspaceID  string
	ChatID       string
	SenderID     string
	ThreadID     string
}

// ConversationContext is the full snapshot for a scope.
type ConversationContext struct {
	Scope     Scope
	Entities  []EntityRef
	ExpiresAt time.Time
}

// Store is the abstraction for conversation context storage.
type Store interface {
	Get(ctx context.Context, scope Scope) (ConversationContext, bool, error)
	Upsert(ctx context.Context, cc ConversationContext) error
	AppendEntities(ctx context.Context, scope Scope, entities []EntityRef, max int, ttl time.Duration) error
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}

// DBStore is the PostgreSQL implementation of Store.
type DBStore struct {
	pool *pgxpool.Pool
}

// NewDBStore creates a new DBStore.
func NewDBStore(pool *pgxpool.Pool) *DBStore {
	return &DBStore{pool: pool}
}

// Get retrieves the conversation context for a scope.
func (s *DBStore) Get(ctx context.Context, scope Scope) (ConversationContext, bool, error) {
	return ConversationContext{}, false, errors.New("not implemented")
}

// Upsert writes or replaces the conversation context for a scope.
func (s *DBStore) Upsert(ctx context.Context, cc ConversationContext) error {
	return errors.New("not implemented")
}

// AppendEntities merges new entities into the existing context for a scope.
func (s *DBStore) AppendEntities(ctx context.Context, scope Scope, entities []EntityRef, max int, ttl time.Duration) error {
	return errors.New("not implemented")
}

// DeleteExpired removes all expired contexts.
func (s *DBStore) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	return 0, errors.New("not implemented")
}

var _ Store = (*DBStore)(nil)
