package realtime

import (
	"sync"
	"testing"
)

func TestMetrics_RecordEvent(t *testing.T) {
	m := &Metrics{}

	m.RecordEvent("issue:updated")
	m.RecordEvent("issue:updated")
	m.RecordEvent("task:message")
	m.RecordEvent("") // ignored

	snap := m.Snapshot()
	events, ok := snap["events_sent_by_type"].(map[string]int64)
	if !ok {
		t.Fatalf("expected events_sent_by_type to be map[string]int64, got %T", snap["events_sent_by_type"])
	}
	if events["issue:updated"] != 2 {
		t.Errorf("issue:updated count = %d, want 2", events["issue:updated"])
	}
	if events["task:message"] != 1 {
		t.Errorf("task:message count = %d, want 1", events["task:message"])
	}
	if _, ok := events[""]; ok {
		t.Errorf("empty event type should not be recorded")
	}
}

func TestMetrics_RecordEvent_Concurrent(t *testing.T) {
	m := &Metrics{}
	var wg sync.WaitGroup
	const goroutines = 50
	const perGoroutine = 200
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				m.RecordEvent("comment:created")
			}
		}()
	}
	wg.Wait()

	snap := m.Snapshot()
	events := snap["events_sent_by_type"].(map[string]int64)
	want := int64(goroutines * perGoroutine)
	if events["comment:created"] != want {
		t.Errorf("comment:created count = %d, want %d", events["comment:created"], want)
	}
}

func TestMetrics_Snapshot_IncludesCounters(t *testing.T) {
	m := &Metrics{}
	m.ConnectsTotal.Store(10)
	m.DisconnectsTotal.Store(3)
	m.ActiveConnections.Store(7)
	m.SlowEvictionsTotal.Store(2)
	m.MessagesSentTotal.Store(123)
	m.MessagesDroppedTotal.Store(4)

	snap := m.Snapshot()
	for k, want := range map[string]int64{
		"connects_total":         10,
		"disconnects_total":      3,
		"active_connections":     7,
		"slow_evictions_total":   2,
		"messages_sent_total":    123,
		"messages_dropped_total": 4,
	} {
		if got, _ := snap[k].(int64); got != want {
			t.Errorf("snapshot[%q] = %v, want %d", k, snap[k], want)
		}
	}
}

// Compile-time guarantee that *Hub continues to satisfy Broadcaster, in case
// someone changes hub.go method signatures without updating the interface.
func TestHubImplementsBroadcaster(t *testing.T) {
	var _ Broadcaster = NewHub()
}
