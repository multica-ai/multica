package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/multica-ai/multica/server/internal/realtime"
)

type RealtimeCollector struct {
	metrics *realtime.Metrics

	connectsTotal          *prometheus.Desc
	disconnectsTotal       *prometheus.Desc
	activeConnections      *prometheus.Desc
	slowEvictionsTotal     *prometheus.Desc
	messagesSentTotal      *prometheus.Desc
	messagesDropped        *prometheus.Desc
	inboundTooLarge        *prometheus.Desc
	redisConnected         *prometheus.Desc
	redisXAddTotal         *prometheus.Desc
	redisXAddErrors        *prometheus.Desc
	redisXReadTotal        *prometheus.Desc
	redisXReadErrors       *prometheus.Desc
	redisAckTotal          *prometheus.Desc
	redisMirrorErrors      *prometheus.Desc
	redisMirrorDiverged    *prometheus.Desc
	redisStreamTrimmed     *prometheus.Desc
	redisStreamMissing     *prometheus.Desc
	redisRetentionErrors   *prometheus.Desc
	redisStreamsWithoutTTL *prometheus.Desc
	redisUsedMemoryBytes   *prometheus.Desc
	redisMaxMemoryBytes    *prometheus.Desc
	redisEvictedKeys       *prometheus.Desc
	redisStreamEntries     *prometheus.Desc
	redisStreamMemory      *prometheus.Desc
	redisStreamPTTL        *prometheus.Desc
}

func NewRealtimeCollector(m *realtime.Metrics) *RealtimeCollector {
	return &RealtimeCollector{
		metrics: m,

		connectsTotal:          newRealtimeDesc("connects_total", "Total realtime WebSocket connections opened."),
		disconnectsTotal:       newRealtimeDesc("disconnects_total", "Total realtime WebSocket connections closed."),
		activeConnections:      newRealtimeDesc("active_connections", "Current realtime WebSocket connections."),
		slowEvictionsTotal:     newRealtimeDesc("slow_evictions_total", "Total realtime clients evicted for slow consumption."),
		messagesSentTotal:      newRealtimeDesc("messages_sent_total", "Total realtime messages sent."),
		messagesDropped:        newRealtimeDesc("messages_dropped_total", "Total realtime messages dropped."),
		inboundTooLarge:        newRealtimeDesc("inbound_too_large_total", "Total realtime connections closed for exceeding the inbound message size limit."),
		redisConnected:         newRealtimeDesc("redis_connected", "Whether the realtime Redis relay is connected."),
		redisXAddTotal:         newRealtimeDesc("redis_xadd_total", "Total Redis XADD operations by the realtime relay."),
		redisXAddErrors:        newRealtimeDesc("redis_xadd_errors_total", "Total Redis XADD errors by the realtime relay."),
		redisXReadTotal:        newRealtimeDesc("redis_xread_total", "Total Redis XREAD operations by the realtime relay."),
		redisXReadErrors:       newRealtimeDesc("redis_xread_errors_total", "Total Redis XREAD errors by the realtime relay."),
		redisAckTotal:          newRealtimeDesc("redis_ack_total", "Total Redis stream acknowledgements by the realtime relay."),
		redisMirrorErrors:      prometheus.NewDesc("multica_realtime_redis_mirror_errors_total", "Total Redis mirror write errors by the realtime relay.", []string{"target"}, nil),
		redisMirrorDiverged:    newRealtimeDesc("redis_mirror_divergence_total", "Total Redis mirror divergence events by the realtime relay."),
		redisStreamTrimmed:     newRealtimeDesc("redis_stream_trimmed_entries_total", "Total Redis Stream entries removed by retention maintenance."),
		redisStreamMissing:     newRealtimeDesc("redis_stream_missing_total", "Total observed relay stream disappearance transitions, including eviction and expiry."),
		redisRetentionErrors:   newRealtimeDesc("redis_retention_errors_total", "Total Redis relay retention maintenance errors."),
		redisStreamsWithoutTTL: newRealtimeDesc("redis_streams_without_ttl", "Current number of observed relay streams missing an expiry while TTL protection is enabled."),
		redisUsedMemoryBytes:   newRealtimeDesc("redis_used_memory_bytes", "Redis used_memory sampled by the realtime relay."),
		redisMaxMemoryBytes:    newRealtimeDesc("redis_maxmemory_bytes", "Redis maxmemory sampled by the realtime relay; zero means unlimited."),
		redisEvictedKeys:       newRealtimeDesc("redis_evicted_keys", "Redis instance evicted_keys sampled by the realtime relay."),
		redisStreamEntries:     prometheus.NewDesc("multica_realtime_redis_stream_entries", "Current entry count of a sampled relay stream.", []string{"stream"}, nil),
		redisStreamMemory:      prometheus.NewDesc("multica_realtime_redis_stream_memory_bytes", "Sampled memory usage of a relay stream.", []string{"stream"}, nil),
		redisStreamPTTL:        prometheus.NewDesc("multica_realtime_redis_stream_pttl_milliseconds", "Remaining relay stream TTL in milliseconds; -1 means no TTL and -2 means missing.", []string{"stream"}, nil),
	}
}

func newRealtimeDesc(name, help string) *prometheus.Desc {
	return prometheus.NewDesc("multica_realtime_"+name, help, nil, nil)
}

func (c *RealtimeCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{
		c.connectsTotal,
		c.disconnectsTotal,
		c.activeConnections,
		c.slowEvictionsTotal,
		c.messagesSentTotal,
		c.messagesDropped,
		c.inboundTooLarge,
		c.redisConnected,
		c.redisXAddTotal,
		c.redisXAddErrors,
		c.redisXReadTotal,
		c.redisXReadErrors,
		c.redisAckTotal,
		c.redisMirrorErrors,
		c.redisMirrorDiverged,
		c.redisStreamTrimmed,
		c.redisStreamMissing,
		c.redisRetentionErrors,
		c.redisStreamsWithoutTTL,
		c.redisUsedMemoryBytes,
		c.redisMaxMemoryBytes,
		c.redisEvictedKeys,
		c.redisStreamEntries,
		c.redisStreamMemory,
		c.redisStreamPTTL,
	} {
		ch <- desc
	}
}

func (c *RealtimeCollector) Collect(ch chan<- prometheus.Metric) {
	if c.metrics == nil {
		return
	}
	m := c.metrics
	ch <- prometheus.MustNewConstMetric(c.connectsTotal, prometheus.CounterValue, float64(m.ConnectsTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.disconnectsTotal, prometheus.CounterValue, float64(m.DisconnectsTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.activeConnections, prometheus.GaugeValue, float64(m.ActiveConnections.Load()))
	ch <- prometheus.MustNewConstMetric(c.slowEvictionsTotal, prometheus.CounterValue, float64(m.SlowEvictionsTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.messagesSentTotal, prometheus.CounterValue, float64(m.MessagesSentTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.messagesDropped, prometheus.CounterValue, float64(m.MessagesDroppedTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.inboundTooLarge, prometheus.CounterValue, float64(m.InboundTooLargeTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisConnected, prometheus.GaugeValue, boolFloat(m.RedisConnected.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisXAddTotal, prometheus.CounterValue, float64(m.RedisXAddTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisXAddErrors, prometheus.CounterValue, float64(m.RedisXAddErrors.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisXReadTotal, prometheus.CounterValue, float64(m.RedisXReadTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisXReadErrors, prometheus.CounterValue, float64(m.RedisXReadErrors.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisAckTotal, prometheus.CounterValue, float64(m.RedisAckTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisMirrorErrors, prometheus.CounterValue, float64(m.RedisMirrorPrimaryErrors.Load()), "primary")
	ch <- prometheus.MustNewConstMetric(c.redisMirrorErrors, prometheus.CounterValue, float64(m.RedisMirrorSecondaryErrors.Load()), "secondary")
	ch <- prometheus.MustNewConstMetric(c.redisMirrorDiverged, prometheus.CounterValue, float64(m.RedisMirrorDivergenceTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisStreamTrimmed, prometheus.CounterValue, float64(m.RedisRelayStreamTrimmedTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisStreamMissing, prometheus.CounterValue, float64(m.RedisRelayStreamMissingTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisRetentionErrors, prometheus.CounterValue, float64(m.RedisRelayRetentionErrors.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisStreamsWithoutTTL, prometheus.GaugeValue, float64(m.RedisRelayStreamsWithoutTTL.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisUsedMemoryBytes, prometheus.GaugeValue, float64(m.RedisUsedMemoryBytes.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisMaxMemoryBytes, prometheus.GaugeValue, float64(m.RedisMaxMemoryBytes.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisEvictedKeys, prometheus.GaugeValue, float64(m.RedisEvictedKeys.Load()))
	for stream, observation := range m.RedisStreamObservations() {
		ch <- prometheus.MustNewConstMetric(c.redisStreamEntries, prometheus.GaugeValue, float64(observation.Entries), stream)
		ch <- prometheus.MustNewConstMetric(c.redisStreamMemory, prometheus.GaugeValue, float64(observation.MemoryBytes), stream)
		ch <- prometheus.MustNewConstMetric(c.redisStreamPTTL, prometheus.GaugeValue, float64(observation.PTTLMillis), stream)
	}
}

func boolFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}
