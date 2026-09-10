package dingtalk

import (
	"sync"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

const maxReplySources = 1024

type replySource struct {
	installationID pgtype.UUID
	message        channel.InboundMessage
}

// This bounded cache associates accepted input with its in-process provider
// anchor. It never repairs shared routing, persists callback data, or recovers
// after restart. Losing an entry suppresses optional reactions for that input.
type replySourceCache struct {
	mu      sync.Mutex
	entries map[pgtype.UUID]replySource
	order   []pgtype.UUID
	next    int
}

func (c *Client) rememberReplySource(installationID, inputID pgtype.UUID, msg channel.InboundMessage) {
	if c == nil || !installationID.Valid || !inputID.Valid || msg.MessageID == "" || msg.Source.ChatID == "" {
		return
	}
	source := replySource{installationID: installationID, message: channel.InboundMessage{MessageID: msg.MessageID, Source: msg.Source}}
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
		delete(cache.entries, cache.order[cache.next])
		cache.order[cache.next] = inputID
		cache.next = (cache.next + 1) % maxReplySources
	}
	cache.entries[inputID] = source
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
