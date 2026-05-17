package conversationctx

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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
	var raw []byte
	var expiresAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT entities, expires_at
		FROM channel_conversation_context
		WHERE connection_id = $1
		  AND workspace_id = $2
		  AND chat_id = $3
		  AND sender_external_id = $4
		  AND thread_id = $5
		  AND expires_at > now()
	`, scope.ConnectionID, scope.WorkspaceID, scope.ChatID, scope.SenderID, scope.ThreadID).Scan(&raw, &expiresAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ConversationContext{}, false, nil
		}
		return ConversationContext{}, false, err
	}
	entities, err := decodeEntities(raw)
	if err != nil {
		return ConversationContext{}, false, err
	}
	return ConversationContext{
		Scope:     scope,
		Entities:  entities,
		ExpiresAt: expiresAt,
	}, true, nil
}

// Upsert writes or replaces the conversation context for a scope.
func (s *DBStore) Upsert(ctx context.Context, cc ConversationContext) error {
	entities, err := encodeEntities(normalizeEntities(cc.Entities, 0))
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO channel_conversation_context (
			connection_id,
			workspace_id,
			chat_id,
			sender_external_id,
			thread_id,
			entities,
			expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
		ON CONFLICT (connection_id, workspace_id, chat_id, sender_external_id, thread_id)
		DO UPDATE SET
			entities = EXCLUDED.entities,
			expires_at = EXCLUDED.expires_at,
			updated_at = now()
	`, cc.Scope.ConnectionID, cc.Scope.WorkspaceID, cc.Scope.ChatID, cc.Scope.SenderID, cc.Scope.ThreadID, entities, cc.ExpiresAt)
	return err
}

// AppendEntities merges new entities into the existing context for a scope.
func (s *DBStore) AppendEntities(ctx context.Context, scope Scope, entities []EntityRef, max int, ttl time.Duration) error {
	entities = normalizeEntities(entities, 0)
	if len(entities) == 0 {
		return nil
	}
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	emptyEntities, err := encodeEntities(nil)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO channel_conversation_context (
			connection_id,
			workspace_id,
			chat_id,
			sender_external_id,
			thread_id,
			entities,
			expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
		ON CONFLICT (connection_id, workspace_id, chat_id, sender_external_id, thread_id) DO NOTHING
	`, scope.ConnectionID, scope.WorkspaceID, scope.ChatID, scope.SenderID, scope.ThreadID, emptyEntities, expiresAt)
	if err != nil {
		return err
	}

	var raw []byte
	err = tx.QueryRow(ctx, `
		SELECT entities
		FROM channel_conversation_context
		WHERE connection_id = $1
		  AND workspace_id = $2
		  AND chat_id = $3
		  AND sender_external_id = $4
		  AND thread_id = $5
		FOR UPDATE
	`, scope.ConnectionID, scope.WorkspaceID, scope.ChatID, scope.SenderID, scope.ThreadID).Scan(&raw)
	if err != nil {
		return err
	}
	existing, err := decodeEntities(raw)
	if err != nil {
		return err
	}
	merged := mergeEntities(existing, entities, max)
	encoded, err := encodeEntities(merged)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE channel_conversation_context
		SET entities = $6::jsonb,
		    expires_at = $7,
		    updated_at = now()
		WHERE connection_id = $1
		  AND workspace_id = $2
		  AND chat_id = $3
		  AND sender_external_id = $4
		  AND thread_id = $5
	`, scope.ConnectionID, scope.WorkspaceID, scope.ChatID, scope.SenderID, scope.ThreadID, encoded, expiresAt)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteExpired removes all expired contexts.
func (s *DBStore) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM channel_conversation_context
		WHERE expires_at < $1
	`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

var _ Store = (*DBStore)(nil)

func encodeEntities(entities []EntityRef) ([]byte, error) {
	if entities == nil {
		entities = []EntityRef{}
	}
	return json.Marshal(entities)
}

func decodeEntities(raw []byte) ([]EntityRef, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var entities []EntityRef
	if err := json.Unmarshal(raw, &entities); err != nil {
		return nil, err
	}
	return normalizeEntities(entities, 0), nil
}

func mergeEntities(existing []EntityRef, incoming []EntityRef, max int) []EntityRef {
	merged := make([]EntityRef, 0, len(existing)+len(incoming))
	index := make(map[string]int, len(existing)+len(incoming))
	for _, entity := range normalizeEntities(existing, 0) {
		index[entity.Key] = len(merged)
		merged = append(merged, entity)
	}
	for _, entity := range normalizeEntities(incoming, 0) {
		if i, ok := index[entity.Key]; ok {
			merged[i] = mergeEntity(merged[i], entity)
			continue
		}
		index[entity.Key] = len(merged)
		merged = append(merged, entity)
	}
	return normalizeEntities(merged, max)
}

func mergeEntity(existing EntityRef, incoming EntityRef) EntityRef {
	if incoming.MentionedAt.After(existing.MentionedAt) {
		existing.MentionedAt = incoming.MentionedAt
	}
	if incoming.ID != "" {
		existing.ID = incoming.ID
	}
	if incoming.Type != "" {
		existing.Type = incoming.Type
	}
	if incoming.Display != "" {
		existing.Display = incoming.Display
	}
	if incoming.URL != "" {
		existing.URL = incoming.URL
	}
	return existing
}

func normalizeEntities(entities []EntityRef, max int) []EntityRef {
	if len(entities) == 0 {
		return nil
	}
	out := make([]EntityRef, 0, len(entities))
	seen := make(map[string]int, len(entities))
	for _, entity := range entities {
		entity.Key = strings.ToUpper(strings.TrimSpace(entity.Key))
		if entity.Key == "" {
			continue
		}
		if entity.Type == "" {
			entity.Type = EntityTypeIssue
		}
		if i, ok := seen[entity.Key]; ok {
			out[i] = mergeEntity(out[i], entity)
			continue
		}
		seen[entity.Key] = len(out)
		out = append(out, entity)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].MentionedAt.After(out[j].MentionedAt)
	})
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}
