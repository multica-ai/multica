package conversationctx

import (
	"context"
	"sort"
	"sync"
	"time"
)

// FakeStore is an in-memory Store for unit tests.
type FakeStore struct {
	mu   sync.Mutex
	data map[Scope]ConversationContext
}

func NewFakeStore() *FakeStore {
	return &FakeStore{data: make(map[Scope]ConversationContext)}
}

func (f *FakeStore) Get(ctx context.Context, scope Scope) (ConversationContext, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cc, ok := f.data[scope]
	if !ok || time.Now().After(cc.ExpiresAt) {
		return ConversationContext{}, false, nil
	}
	return cc, true, nil
}

func (f *FakeStore) Upsert(ctx context.Context, cc ConversationContext) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[cc.Scope] = cc
	return nil
}

func (f *FakeStore) AppendEntities(ctx context.Context, scope Scope, entities []EntityRef, max int, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cc, ok := f.data[scope]
	if !ok {
		cc = ConversationContext{Scope: scope, Entities: []EntityRef{}}
	}
	now := time.Now()
	seen := make(map[string]struct{}, len(cc.Entities))
	for _, e := range cc.Entities {
		seen[e.Key] = struct{}{}
	}
	for _, e := range entities {
		if _, exists := seen[e.Key]; exists {
			// update timestamp for existing
			for i := range cc.Entities {
				if cc.Entities[i].Key == e.Key {
					cc.Entities[i].MentionedAt = e.MentionedAt
					break
				}
			}
			continue
		}
		seen[e.Key] = struct{}{}
		cc.Entities = append(cc.Entities, e)
	}
	// sort by MentionedAt DESC
	sort.Slice(cc.Entities, func(i, j int) bool {
		return cc.Entities[i].MentionedAt.After(cc.Entities[j].MentionedAt)
	})
	if max > 0 && len(cc.Entities) > max {
		cc.Entities = cc.Entities[:max]
	}
	cc.ExpiresAt = now.Add(ttl)
	f.data[scope] = cc
	return nil
}

func (f *FakeStore) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for k, v := range f.data {
		if v.ExpiresAt.Before(before) {
			delete(f.data, k)
			n++
		}
	}
	return n, nil
}

var _ Store = (*FakeStore)(nil)
