package daemonws

import "sync/atomic"

type Metrics struct {
	ConnectsTotal      atomic.Int64
	DisconnectsTotal   atomic.Int64
	ActiveConnections  atomic.Int64
	SlowEvictionsTotal atomic.Int64

	WakeupPublishedTotal atomic.Int64
	WakeupPublishErrors  atomic.Int64
	WakeupReceivedTotal  atomic.Int64
	WakeupDeliveredHit   atomic.Int64
	WakeupDeliveredMiss  atomic.Int64
}

var M = &Metrics{}

func (m *Metrics) Snapshot() map[string]any {
	return map[string]any{
		"connects_total":              m.ConnectsTotal.Load(),
		"disconnects_total":           m.DisconnectsTotal.Load(),
		"active_connections":          m.ActiveConnections.Load(),
		"slow_evictions_total":        m.SlowEvictionsTotal.Load(),
		"wakeup_published_total":      m.WakeupPublishedTotal.Load(),
		"wakeup_publish_errors":       m.WakeupPublishErrors.Load(),
		"wakeup_received_total":       m.WakeupReceivedTotal.Load(),
		"wakeup_delivered_hit_total":  m.WakeupDeliveredHit.Load(),
		"wakeup_delivered_miss_total": m.WakeupDeliveredMiss.Load(),
	}
}

func (m *Metrics) Reset() {
	m.ConnectsTotal.Store(0)
	m.DisconnectsTotal.Store(0)
	m.ActiveConnections.Store(0)
	m.SlowEvictionsTotal.Store(0)
	m.WakeupPublishedTotal.Store(0)
	m.WakeupPublishErrors.Store(0)
	m.WakeupReceivedTotal.Store(0)
	m.WakeupDeliveredHit.Store(0)
	m.WakeupDeliveredMiss.Store(0)
}
