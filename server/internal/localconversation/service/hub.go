package service

import (
	"context"
	"errors"
	"sync"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

var (
	// ErrResubscribe ends a subscription whose buffer exceeded its budget;
	// the client subscribes again from its last applied sequence (plan §5.3).
	ErrResubscribe = errors.New("local conversation: subscription buffer exceeded, resubscribe")
	// ErrSnapshotRequired means the client's cursor is ahead of the store, so
	// its view came from another database; it must take a new snapshot.
	ErrSnapshotRequired = errors.New("local conversation: cursor ahead of store, snapshot required")
)

// Source is the committed history subscriptions catch up from, implemented
// by *store.Store.
type Source interface {
	LastSeq(ctx context.Context, conversationID string) (int64, error)
	EventsAfter(ctx context.Context, conversationID string, afterSeq, throughSeq int64, limit int) ([]sqlitedb.Event, error)
	SummariesAfter(ctx context.Context, scopeID, afterScopeSeq int64, limit int) ([]store.ConversationSummary, int64, error)
}

// HubOptions bounds subscriptions. Zero values use the plan's starting
// points: 4 MiB buffered per subscription, 500 rows per catch-up page.
type HubOptions struct {
	MaxBufferedBytes int
	PageSize         int
}

// Hub fans committed events and summaries out to subscribers. A subscriber
// first catches up from the store, then receives live commits; it never sees
// a gap, a duplicate, or an out-of-order item, and a slow subscriber is
// dropped instead of slowing commits.
type Hub struct {
	source Source
	opts   HubOptions

	mu     sync.Mutex
	convs  map[string]map[*Subscription]bool
	scopes map[int64]map[*ScopeSubscription]bool
}

// NewHub returns a hub reading history from source.
func NewHub(source Source, opts HubOptions) *Hub {
	if opts.MaxBufferedBytes <= 0 {
		opts.MaxBufferedBytes = 4 << 20
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 500
	}
	return &Hub{source: source, opts: opts, convs: map[string]map[*Subscription]bool{}, scopes: map[int64]map[*ScopeSubscription]bool{}}
}

// Subscription delivers one conversation's events after a sequence.
type Subscription struct {
	stream[store.CommittedEvent]
	conversationID string
}

// ScopeSubscription delivers changed conversation summaries of one scope.
// A summary replaces any earlier summary of the same conversation.
type ScopeSubscription struct {
	stream[store.ConversationSummary]
	scopeID int64
}

// Subscribe starts delivering conversationID's events after afterSeq and
// returns the committed high-water mark the catch-up runs to.
func (h *Hub) Subscribe(ctx context.Context, conversationID string, afterSeq int64) (*Subscription, int64, error) {
	sub := &Subscription{conversationID: conversationID}
	// Read the mark and register under one lock: every commit is then either
	// at or below the mark (served by catch-up) or published live.
	h.mu.Lock()
	high, err := h.source.LastSeq(ctx, conversationID)
	if err != nil {
		h.mu.Unlock()
		return nil, 0, err
	}
	if afterSeq > high {
		h.mu.Unlock()
		return nil, 0, ErrSnapshotRequired
	}
	sub.init(afterSeq, high, h.opts.MaxBufferedBytes, func(e store.CommittedEvent) (int64, int) { return e.Seq, len(e.Payload) + 64 })
	if h.convs[conversationID] == nil {
		h.convs[conversationID] = map[*Subscription]bool{}
	}
	h.convs[conversationID][sub] = true
	sub.onEnd = func() { h.removeConversation(sub) }
	h.mu.Unlock()

	go sub.catchUp(func(after int64) ([]store.CommittedEvent, error) {
		rows, err := h.source.EventsAfter(context.Background(), conversationID, after, high, h.opts.PageSize)
		events := make([]store.CommittedEvent, len(rows))
		for i, row := range rows {
			events[i] = store.CommittedFromRow(row)
		}
		return events, err
	})
	return sub, high, nil
}

// SubscribeScope starts delivering summaries changed after afterScopeSeq and
// returns the scope sequence the catch-up runs to.
func (h *Hub) SubscribeScope(ctx context.Context, scopeID, afterScopeSeq int64) (*ScopeSubscription, int64, error) {
	sub := &ScopeSubscription{scopeID: scopeID}
	h.mu.Lock()
	_, high, err := h.source.SummariesAfter(ctx, scopeID, 0, 0)
	if err != nil {
		h.mu.Unlock()
		return nil, 0, err
	}
	if afterScopeSeq > high {
		h.mu.Unlock()
		return nil, 0, ErrSnapshotRequired
	}
	sub.init(afterScopeSeq, high, h.opts.MaxBufferedBytes, func(c store.ConversationSummary) (int64, int) { return c.SummarySeq, 512 })
	sub.reload = func(after int64) ([]store.ConversationSummary, error) {
		rows, _, err := h.source.SummariesAfter(context.Background(), scopeID, after, h.opts.PageSize)
		return rows, err
	}
	if h.scopes[scopeID] == nil {
		h.scopes[scopeID] = map[*ScopeSubscription]bool{}
	}
	h.scopes[scopeID][sub] = true
	sub.onEnd = func() { h.removeScope(sub) }
	h.mu.Unlock()

	go sub.catchUp(func(after int64) ([]store.ConversationSummary, error) {
		rows, _, err := h.source.SummariesAfter(context.Background(), scopeID, after, h.opts.PageSize)
		// Summaries changed after the mark arrive live.
		for i, row := range rows {
			if row.SummarySeq > high {
				return rows[:i], err
			}
		}
		return rows, err
	})
	return sub, high, nil
}

// Publish delivers a durable commit to live subscribers.
func (h *Hub) Publish(conversationID string, committed store.Committed) {
	h.mu.Lock()
	subs := make([]*Subscription, 0, len(h.convs[conversationID]))
	for sub := range h.convs[conversationID] {
		subs = append(subs, sub)
	}
	var scopeSubs []*ScopeSubscription
	if committed.Summary != nil {
		for sub := range h.scopes[committed.Summary.ScopeID] {
			scopeSubs = append(scopeSubs, sub)
		}
	}
	h.mu.Unlock()
	for _, sub := range subs {
		sub.live(committed.Events)
	}
	for _, sub := range scopeSubs {
		sub.live([]store.ConversationSummary{*committed.Summary})
	}
}

func (h *Hub) removeConversation(s *Subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if subs := h.convs[s.conversationID]; subs != nil {
		delete(subs, s)
		if len(subs) == 0 {
			delete(h.convs, s.conversationID)
		}
	}
}

func (h *Hub) removeScope(s *ScopeSubscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if subs := h.scopes[s.scopeID]; subs != nil {
		delete(subs, s)
		if len(subs) == 0 {
			delete(h.scopes, s.scopeID)
		}
	}
}

// stream is an ordered, bounded delivery queue fed first by catch-up pages
// and then by live publishes.
type stream[T any] struct {
	measure func(T) (seq int64, bytes int)
	limit   int
	onEnd   func()

	mu       sync.Mutex
	ready    chan struct{} // reader wake-up
	drained  chan struct{} // catch-up wake-up after the reader took the queue
	queue    []T
	bytes    int
	lastSeq  int64 // last sequence queued
	highSeq  int64 // catch-up covers sequences up to here
	catching bool
	held     []T // live items that arrived during catch-up
	heldSize int
	err      error
	// reload, when set, re-reads committed items after a sequence. Streams
	// whose commits can be published out of order (scope summaries, from
	// one goroutine per conversation) catch up from it on a gap instead
	// of dropping the late item.
	reload func(after int64) ([]T, error)
}

func (s *stream[T]) init(afterSeq, highSeq int64, limit int, measure func(T) (int64, int)) {
	s.measure, s.limit = measure, limit
	s.ready, s.drained = make(chan struct{}, 1), make(chan struct{}, 1)
	s.lastSeq, s.highSeq, s.catching = afterSeq, highSeq, true
}

// Next waits for the next items, in sequence order.
func (s *stream[T]) Next(ctx context.Context) ([]T, error) {
	for {
		s.mu.Lock()
		if len(s.queue) > 0 {
			out := s.queue
			s.queue, s.bytes = nil, 0
			notify(s.drained)
			s.mu.Unlock()
			return out, nil
		}
		if s.err != nil {
			err := s.err
			s.mu.Unlock()
			return nil, err
		}
		s.mu.Unlock()
		select {
		case <-s.ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Live reports whether catch-up has finished.
func (s *stream[T]) Live() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.catching
}

// Close ends the subscription.
func (s *stream[T]) Close() { s.end(context.Canceled) }

// catchUp loads pages after the cursor up to the high-water mark. It waits
// for the reader to take each page before loading the next, so a long
// history is bounded like live delivery; then it releases held live items.
func (s *stream[T]) catchUp(load func(after int64) ([]T, error)) {
	for {
		s.mu.Lock()
		after, done, failed := s.lastSeq, s.lastSeq >= s.highSeq, s.err != nil
		s.mu.Unlock()
		if failed {
			return
		}
		if done {
			break
		}
		page, err := load(after)
		if err != nil {
			s.end(err)
			return
		}
		if len(page) == 0 {
			break
		}
		s.mu.Lock()
		s.appendLocked(page)
		s.mu.Unlock()
		if !s.waitDrained() {
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return
	}
	s.catching = false
	s.appendLocked(s.held)
	s.held, s.heldSize = nil, 0
	s.checkLocked()
}

// live queues a published commit, or holds it while catch-up runs. Items
// at or below the high-water mark are already served by catch-up.
func (s *stream[T]) live(items []T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return
	}
	for _, item := range items {
		seq, size := s.measure(item)
		if seq <= s.highSeq {
			continue
		}
		if !s.catching && s.reload != nil && seq > s.lastSeq+1 {
			// An earlier commit is published late: read it from the store.
			s.catching, s.highSeq = true, seq
			go s.catchUp(s.reload)
		}
		if s.catching {
			s.held = append(s.held, item)
			s.heldSize += size
			continue
		}
		s.appendLocked([]T{item})
	}
	s.checkLocked()
}

func (s *stream[T]) appendLocked(items []T) {
	for _, item := range items {
		seq, size := s.measure(item)
		if seq <= s.lastSeq {
			continue
		}
		s.queue = append(s.queue, item)
		s.bytes += size
		s.lastSeq = seq
	}
	if len(s.queue) > 0 {
		notify(s.ready)
	}
}

// checkLocked drops a subscriber whose reader fell behind its budget.
func (s *stream[T]) checkLocked() {
	if s.bytes+s.heldSize > s.limit {
		s.endLocked(ErrResubscribe)
	}
}

func (s *stream[T]) waitDrained() bool {
	for {
		s.mu.Lock()
		empty, failed := len(s.queue) == 0, s.err != nil
		s.mu.Unlock()
		if failed {
			return false
		}
		if empty {
			return true
		}
		<-s.drained
	}
}

func (s *stream[T]) end(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endLocked(err)
}

func (s *stream[T]) endLocked(err error) {
	if s.err != nil {
		return
	}
	s.err = err
	s.queue, s.bytes, s.held, s.heldSize = nil, 0, nil, 0
	notify(s.ready)
	notify(s.drained)
	if s.onEnd != nil {
		go s.onEnd()
	}
}

func notify(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
