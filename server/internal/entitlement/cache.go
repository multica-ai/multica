package entitlement

import (
	"container/list"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

var errVersionRegression = errors.New("entitlement subscription version regression")

type policySnapshot struct {
	policyRevision      int64
	subscriptionVersion int64
	cloudValidUntil     time.Time
	gates               map[GateName]Gate
}

type cacheEntry struct {
	hasPolicy  bool
	policy     policySnapshot
	freshUntil time.Time
	staleUntil time.Time
	retryAfter time.Time
}

type cacheItem struct {
	workspaceID uuid.UUID
	entry       cacheEntry
}

// policyCache is a workspace-keyed LRU. The workspace key is never inferred
// from response data, preventing one tenant's policy from entering another
// tenant's cache slot.
type policyCache struct {
	mu         sync.Mutex
	maxEntries int
	order      *list.List
	entries    map[uuid.UUID]*list.Element
}

func newPolicyCache(maxEntries int) *policyCache {
	return &policyCache{
		maxEntries: maxEntries,
		order:      list.New(),
		entries:    make(map[uuid.UUID]*list.Element, maxEntries),
	}
}

func (c *policyCache) get(workspaceID uuid.UUID) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.entries[workspaceID]
	if !ok {
		return cacheEntry{}, false
	}
	c.order.MoveToFront(elem)
	return elem.Value.(*cacheItem).entry, true
}

func (c *policyCache) put(workspaceID uuid.UUID, entry cacheEntry, receivedAt time.Time) (cacheEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.entries[workspaceID]; ok {
		current := elem.Value.(*cacheItem).entry
		// The version guard prevents an out-of-order subscription response from
		// replacing a snapshot that is still usable for bounded stale observation.
		// Once that window ends, accepting the current Cloud response lets the
		// service recover from a rollback without a retry storm.
		guardVersions := current.hasPolicy && receivedAt.Before(current.staleUntil)
		if guardVersions && entry.policy.subscriptionVersion < current.policy.subscriptionVersion {
			return current, errVersionRegression
		}
		elem.Value.(*cacheItem).entry = entry
		c.order.MoveToFront(elem)
		return entry, nil
	}

	c.insert(workspaceID, entry)
	return entry, nil
}

func (c *policyCache) markFailure(workspaceID uuid.UUID, retryAfter time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.entries[workspaceID]; ok {
		item := elem.Value.(*cacheItem)
		item.entry.retryAfter = retryAfter
		c.order.MoveToFront(elem)
		return
	}
	c.insert(workspaceID, cacheEntry{retryAfter: retryAfter})
}

// insert requires c.mu to be held.
func (c *policyCache) insert(workspaceID uuid.UUID, entry cacheEntry) {
	elem := c.order.PushFront(&cacheItem{workspaceID: workspaceID, entry: entry})
	c.entries[workspaceID] = elem
	if c.order.Len() > c.maxEntries {
		oldest := c.order.Back()
		item := oldest.Value.(*cacheItem)
		delete(c.entries, item.workspaceID)
		c.order.Remove(oldest)
	}
}

func (c *policyCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
