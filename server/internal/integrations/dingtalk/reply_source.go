package dingtalk

import (
	"sync"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

const maxReplySources = 1024

type replySource struct {
	installationID pgtype.UUID
	sessionID      pgtype.UUID
	message        channel.InboundMessage
}

// This bounded cache associates accepted input with its in-process provider
// anchor. It never repairs shared routing, persists callback data, or recovers
// after restart. Losing an entry suppresses optional reactions for that input.
type replySourceCache struct {
	mu       sync.Mutex
	entries  map[pgtype.UUID]replySource
	order    []pgtype.UUID
	next     int
	sessions map[pgtype.UUID]int
}

func (c *Client) rememberReplySource(installationID, inputID, sessionID pgtype.UUID, msg channel.InboundMessage) {
	if c == nil || !installationID.Valid || !inputID.Valid || msg.MessageID == "" || msg.Source.ChatID == "" {
		return
	}
	source := replySource{installationID: installationID, sessionID: sessionID, message: channel.InboundMessage{MessageID: msg.MessageID, Source: msg.Source}}
	cache := &c.sources
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.entries == nil {
		cache.entries = make(map[pgtype.UUID]replySource)
	}
	if _, exists := cache.entries[inputID]; exists {
		return
	}
	if len(cache.order) < maxReplySources {
		cache.order = append(cache.order, inputID)
	} else {
		cache.releaseSession(cache.entries[cache.order[cache.next]].sessionID)
		delete(cache.entries, cache.order[cache.next])
		cache.order[cache.next] = inputID
		cache.next = (cache.next + 1) % maxReplySources
	}
	cache.entries[inputID] = source
	cache.retainSession(sessionID)
}

func (c *Client) replySourceFor(installationID, inputID pgtype.UUID) (replySource, bool) {
	if c == nil || !installationID.Valid || !inputID.Valid {
		return replySource{}, false
	}
	cache := &c.sources
	cache.mu.Lock()
	source, ok := cache.entries[inputID]
	cache.mu.Unlock()
	return source, ok && source.installationID == installationID
}

// Resolve only a locally accepted provider anchor. The cache is bounded, and
// installation plus conversation identity must match before reading its input.
func (c *Client) replyInputFor(installationID pgtype.UUID, msg channel.InboundMessage) pgtype.UUID {
	if c == nil || !installationID.Valid {
		return pgtype.UUID{}
	}
	c.sources.mu.Lock()
	defer c.sources.mu.Unlock()
	for id, source := range c.sources.entries {
		if source.installationID == installationID && source.message.MessageID == msg.MessageID && source.message.Source == msg.Source {
			return id
		}
	}
	return pgtype.UUID{}
}

// Register interest before the input can become visible to a task. The caller
// releases its in-flight reference after commit/source capture or on failure.
// Retained references belong to the bounded source cache; concurrent calls own
// separate references so one failed append cannot hide another accepted input.
func (c *Client) beginReplyInput(sessionID pgtype.UUID) func() {
	if c == nil || !sessionID.Valid {
		return func() {}
	}
	cache := &c.sources
	cache.mu.Lock()
	cache.retainSession(sessionID)
	cache.mu.Unlock()
	return func() {
		cache.mu.Lock()
		cache.releaseSession(sessionID)
		cache.mu.Unlock()
	}
}

func (c *Client) hasReplySession(sessionID pgtype.UUID) bool {
	if c == nil {
		return false
	}
	c.sources.mu.Lock()
	defer c.sources.mu.Unlock()
	return c.sources.sessions[sessionID] > 0
}

// Caller holds cache.mu.
func (cache *replySourceCache) retainSession(sessionID pgtype.UUID) {
	if !sessionID.Valid {
		return
	}
	if cache.sessions == nil {
		cache.sessions = make(map[pgtype.UUID]int)
	}
	cache.sessions[sessionID]++
}

func (cache *replySourceCache) releaseSession(sessionID pgtype.UUID) {
	if cache.sessions[sessionID] <= 1 {
		delete(cache.sessions, sessionID)
	} else {
		cache.sessions[sessionID]--
	}
}
