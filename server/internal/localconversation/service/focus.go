package service

import (
	"context"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

// focusTracker knows which clients are looking at which conversations and
// which clients are connected at all. A client is known by a key that survives
// its reconnects, so several connections can come and go for one client.
type focusTracker struct {
	mu            sync.Mutex
	clients       map[string]*focusClient
	conversations map[string]*focusedConversation
	// endedAt is when a conversation's last client left; its process counts as
	// idle from then at the earliest.
	endedAt map[string]time.Time
	// connections counts open connections of every client.
	connections    int
	lastConnection time.Time
	// queue holds conversations waiting to preheat, one running at a time.
	queue   []preheatItem
	working bool
}

type focusClient struct {
	connections int
	// lostAt is when the last connection closed; zero while one is open.
	lostAt        time.Time
	conversations map[string]bool
}

type focusedConversation struct {
	conv    sqlitedb.Conversation
	clients map[string]bool
	// requested is set once a preheat was asked for, until nobody is focused:
	// more clients or a reconnect do not repeat it.
	requested bool
}

type preheatItem struct {
	id     string
	caller timing.Caller
}

func newFocusTracker() *focusTracker {
	return &focusTracker{clients: map[string]*focusClient{}, conversations: map[string]*focusedConversation{}, endedAt: map[string]time.Time{}}
}

func (f *focusTracker) client(key string) *focusClient {
	c := f.clients[key]
	if c == nil {
		c = &focusClient{conversations: map[string]bool{}}
		f.clients[key] = c
	}
	return c
}

// ClientConnected records an open connection of a client and returns what to
// call when it closes. A closing connection leaves the client's focus in place
// for focusGrace, so moving to a direct connection does not read as leaving.
func (s *Service) ClientConnected(key string) (disconnected func()) {
	f := s.focus
	f.mu.Lock()
	c := f.client(key)
	c.connections++
	c.lostAt = time.Time{}
	f.connections++
	f.mu.Unlock()
	s.kickMaintenance()
	var once sync.Once
	return func() {
		once.Do(func() {
			now := s.opts.Now()
			f.mu.Lock()
			defer f.mu.Unlock()
			c.connections--
			f.connections--
			f.lastConnection = now
			if c.connections > 0 {
				return
			}
			c.lostAt = now
			if len(c.conversations) == 0 {
				delete(f.clients, key)
			}
		})
	}
}

// clientsPresent reports whether a client is connected or left within grace.
func (f *focusTracker) clientsPresent(now time.Time, grace time.Duration) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connections > 0 || (!f.lastConnection.IsZero() && now.Sub(f.lastConnection) < grace)
}

// SetFocus records that a client started or stopped looking at a conversation.
// It is a view, not a user action: nothing here marks the conversation as acted
// on. The first client to focus a conversation asks for its preheat.
func (s *Service) SetFocus(ctx context.Context, scopeID int64, conversationID, clientKey string, focused bool, caller timing.Caller) error {
	conv, err := s.scopedConversation(ctx, scopeID, conversationID)
	if err != nil {
		return err
	}
	f := s.focus
	f.mu.Lock()
	c := f.client(clientKey)
	if !focused {
		f.removeLocked(s, conversationID, clientKey, timing.ReasonUnfocused)
		if c.connections == 0 && len(c.conversations) == 0 {
			delete(f.clients, clientKey)
		}
		f.mu.Unlock()
		return nil
	}
	entry := f.conversations[conversationID]
	if entry == nil {
		entry = &focusedConversation{conv: conv, clients: map[string]bool{}}
		f.conversations[conversationID] = entry
	}
	added := !entry.clients[clientKey]
	entry.clients[clientKey] = true
	entry.conv = conv
	c.conversations[conversationID] = true
	size := len(entry.clients)
	first := !entry.requested
	f.mu.Unlock()
	if added {
		s.traces.conversation(conv, caller).MarkCount(timing.StageFocusChanged, timing.ReasonFocused, int64(size))
	}
	if first {
		s.requestPreheat(conv, caller)
	}
	return nil
}

// removeLocked takes a client off a conversation; f.mu is held. A conversation
// nobody looks at starts counting as idle and leaves the preheat queue.
func (f *focusTracker) removeLocked(s *Service, conversationID, clientKey, reason string) {
	entry := f.conversations[conversationID]
	if c := f.clients[clientKey]; c != nil {
		delete(c.conversations, conversationID)
	}
	if entry == nil || !entry.clients[clientKey] {
		return
	}
	delete(entry.clients, clientKey)
	trace := s.traces.conversation(entry.conv, timing.Caller{})
	trace.MarkCount(timing.StageFocusChanged, reason, int64(len(entry.clients)))
	if len(entry.clients) > 0 {
		return
	}
	delete(f.conversations, conversationID)
	f.endedAt[conversationID] = s.opts.Now()
	for i, item := range f.queue {
		if item.id == conversationID {
			f.queue = append(f.queue[:i:i], f.queue[i+1:]...)
			trace.MarkReason(timing.StagePreheatCancelled, timing.ReasonNotFocused)
			break
		}
	}
}

// sweepFocus ends the focus of clients gone for longer than the grace period.
func (s *Service) sweepFocus() {
	now := s.opts.Now()
	f := s.focus
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, c := range f.clients {
		if c.connections > 0 || c.lostAt.IsZero() || now.Sub(c.lostAt) < focusGrace {
			continue
		}
		for id := range c.conversations {
			f.removeLocked(s, id, key, timing.ReasonConnectionLost)
		}
		delete(f.clients, key)
	}
	for id, at := range f.endedAt {
		if now.Sub(at) >= sentIdleLimit {
			delete(f.endedAt, id)
		}
	}
}

// preheatRefusal is why a conversation must not preheat now, or "". A side
// conversation without a native session stays out: its first prompt carries the
// source context only while the session is empty.
func (s *Service) preheatRefusal(conv sqlitedb.Conversation) string {
	switch {
	case conv.ProviderSessionID == "":
		return timing.ReasonNoSession
	case conv.Archived == 1:
		return timing.ReasonArchived
	case s.acceptTurn() != nil:
		return timing.ReasonDraining
	case s.opts.PreheatOldConversations != nil && !s.opts.PreheatOldConversations():
		return timing.ReasonDisabled
	}
	return ""
}

// requestPreheat queues a conversation for preheat if it qualifies. Only a
// refusal that can pass by itself leaves it unrequested, for Undrain to retry.
func (s *Service) requestPreheat(conv sqlitedb.Conversation, caller timing.Caller) {
	trace := s.traces.conversation(conv, caller)
	if reason := s.preheatRefusal(conv); reason != "" {
		trace.MarkReason(timing.StagePreheatCancelled, reason)
		return
	}
	f := s.focus
	f.mu.Lock()
	entry := f.conversations[conv.ID]
	if entry == nil || entry.requested {
		f.mu.Unlock()
		return
	}
	entry.requested = true
	f.queue = append(f.queue, preheatItem{id: conv.ID, caller: caller})
	start := !f.working
	f.working = true
	f.mu.Unlock()
	trace.Mark(timing.StagePreheatRequested)
	if start && !s.goBackground(s.preheatWorker) {
		f.mu.Lock()
		f.working = false
		f.mu.Unlock()
	}
}

// preheatFocused asks again for conversations focused while the service could
// not preheat.
func (s *Service) preheatFocused() {
	f := s.focus
	f.mu.Lock()
	var pending []sqlitedb.Conversation
	for _, entry := range f.conversations {
		if !entry.requested {
			pending = append(pending, entry.conv)
		}
	}
	f.mu.Unlock()
	for _, conv := range pending {
		if latest, err := s.store.Conversation(context.Background(), conv.ID); err == nil {
			s.requestPreheat(latest, timing.Caller{})
		}
	}
}

// preheatWorker runs queued preheats one at a time. Real sends never queue here:
// they run themselves, or join the conversation's task if one is underway.
func (s *Service) preheatWorker() {
	f := s.focus
	for {
		f.mu.Lock()
		if len(f.queue) == 0 {
			f.working = false
			f.mu.Unlock()
			return
		}
		item := f.queue[0]
		f.queue = f.queue[1:]
		f.mu.Unlock()
		s.preheat(item)
	}
}

// allowPreheat lets a conversation whose preheat could not run ask again.
func (f *focusTracker) allowPreheat(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if entry := f.conversations[id]; entry != nil {
		entry.requested = false
	}
}

func (s *Service) preheat(item preheatItem) {
	ctx, cancel := context.WithTimeout(context.Background(), prepareDeadline+time.Minute)
	defer cancel()
	conv, err := s.store.Conversation(ctx, item.id)
	if err != nil {
		return
	}
	trace := s.traces.conversation(conv, item.caller)
	if reason := s.preheatRefusal(conv); reason != "" {
		trace.MarkReason(timing.StagePreheatCancelled, reason)
		s.focus.allowPreheat(item.id)
		return
	}
	finished := trace.Begin(timing.StagePreheatStarted)
	r, err := s.runner(ctx, conv)
	if err == nil {
		err = r.Prepare(ctx, trace)
	}
	finished(err)
	if err != nil && s.acceptTurn() != nil {
		trace.MarkReason(timing.StagePreheatCancelled, timing.ReasonDraining)
		s.focus.allowPreheat(item.id)
		return
	}
	if err != nil {
		trace.MarkFailed(timing.StagePreheatFailed, "provider_error")
		s.opts.Logger.Info("local conversations: preheat failed", "conversation_id", conv.ID, "error", err)
		return
	}
	trace.Mark(timing.StagePreheatFinished)
}
