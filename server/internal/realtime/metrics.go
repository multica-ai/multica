package realtime

import (
	"sort"
	"sync"
	"sync/atomic"
)

// Metrics collects lightweight counters describing the realtime subsystem.
//
// These are intentionally simple atomic counters rather than a full Prometheus
// registry so Phase 0 does not introduce a new dependency. They give us the
// minimum visibility called out in the MUL-1138 plan: connection counts,
// slow-client evictions, and per-event-type send/drop QPS. A future phase can
// re-export the same numbers via Prometheus or OpenTelemetry without changing
// any producer code.
type Metrics struct {
	// Hub-level counters.
	ConnectsTotal       atomic.Int64
	DisconnectsTotal    atomic.Int64
	ActiveConnections   atomic.Int64
	SlowEvictionsTotal  atomic.Int64
	MessagesSentTotal   atomic.Int64
	MessagesDroppedTotal atomic.Int64

	// Per-event-type send counters. Keyed by event type string
	// (e.g. "issue:updated", "task:message"). Value is *atomic.Int64.
	eventSent sync.Map
}

// M is the package-level metrics singleton. Producers call M.RecordEvent(type)
// before broadcasting so we can see fanout pressure broken down by event type.
var M = &Metrics{}

// RecordEvent increments the per-event-type send counter.
func (m *Metrics) RecordEvent(eventType string) {
	if eventType == "" {
		return
	}
	if v, ok := m.eventSent.Load(eventType); ok {
		v.(*atomic.Int64).Add(1)
		return
	}
	counter := new(atomic.Int64)
	counter.Store(1)
	if existing, loaded := m.eventSent.LoadOrStore(eventType, counter); loaded {
		existing.(*atomic.Int64).Add(1)
	}
}

// Snapshot returns a JSON-friendly copy of the current counter values.
// Safe to call concurrently with updates; values reflect best-effort
// point-in-time totals.
func (m *Metrics) Snapshot() map[string]any {
	events := map[string]int64{}
	m.eventSent.Range(func(k, v any) bool {
		events[k.(string)] = v.(*atomic.Int64).Load()
		return true
	})

	// Stable key order for predictable test output.
	keys := make([]string, 0, len(events))
	for k := range events {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	orderedEvents := make(map[string]int64, len(events))
	for _, k := range keys {
		orderedEvents[k] = events[k]
	}

	return map[string]any{
		"connects_total":         m.ConnectsTotal.Load(),
		"disconnects_total":      m.DisconnectsTotal.Load(),
		"active_connections":     m.ActiveConnections.Load(),
		"slow_evictions_total":   m.SlowEvictionsTotal.Load(),
		"messages_sent_total":    m.MessagesSentTotal.Load(),
		"messages_dropped_total": m.MessagesDroppedTotal.Load(),
		"events_sent_by_type":    orderedEvents,
	}
}

// Reset zeroes all counters. Intended for tests; production code never calls
// this.
func (m *Metrics) Reset() {
	m.ConnectsTotal.Store(0)
	m.DisconnectsTotal.Store(0)
	m.ActiveConnections.Store(0)
	m.SlowEvictionsTotal.Store(0)
	m.MessagesSentTotal.Store(0)
	m.MessagesDroppedTotal.Store(0)
	m.eventSent.Range(func(k, _ any) bool {
		m.eventSent.Delete(k)
		return true
	})
}
