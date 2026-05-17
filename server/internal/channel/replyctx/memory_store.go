package replyctx

import (
	"context"
	"sync"
	"time"
)

// InMemoryStore is a thread-safe in-memory implementation of Store for tests.
type InMemoryStore struct {
	mu    sync.RWMutex
	items map[string]Context
}

// NewInMemoryStore creates an empty in-memory store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{items: make(map[string]Context)}
}

func (s *InMemoryStore) key(connectionID, externalUserID, chatID string) string {
	return connectionID + "\x00" + externalUserID + "\x00" + chatID
}

// Upsert saves or replaces the reply context for the given connection + user + chat.
func (s *InMemoryStore) Upsert(_ context.Context, item Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[s.key(item.ConnectionID, item.ExternalUserID, item.ChatID)] = item
	return nil
}

// Lookup retrieves the active reply context for the given connection + user + chat.
func (s *InMemoryStore) Lookup(_ context.Context, connectionID, externalUserID, chatID string, now time.Time) (Context, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.items[s.key(connectionID, externalUserID, chatID)]
	if !ok && chatID != "" {
		item, ok = s.items[s.key(connectionID, externalUserID, "")]
		if ok {
			item.ChatID = chatID
		}
	}
	if !ok {
		return Context{}, false, nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	if now.After(item.ExpiresAt) {
		return Context{}, false, nil
	}
	return item, true, nil
}

// Clear removes the reply context for the given connection + user + chat.
func (s *InMemoryStore) Clear(_ context.Context, connectionID, externalUserID, chatID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, s.key(connectionID, externalUserID, chatID))
	return nil
}

var _ Store = (*InMemoryStore)(nil)
