package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/multica-ai/multica/server/internal/realtime"
)

type RealtimeCollector struct {
	metrics *realtime.Metrics

	connectsTotal       *prometheus.Desc
	disconnectsTotal    *prometheus.Desc
	activeConnections   *prometheus.Desc
	slowEvictionsTotal  *prometheus.Desc
	messagesSentTotal   *prometheus.Desc
	messagesDropped     *prometheus.Desc
	redisConnected      *prometheus.Desc
	redisXAddTotal      *prometheus.Desc
	redisXAddErrors     *prometheus.Desc
	redisXReadTotal     *prometheus.Desc
	redisXReadErrors    *prometheus.Desc
	redisAckTotal       *prometheus.Desc
	redisMirrorErrors   *prometheus.Desc
	redisMirrorDiverged *prometheus.Desc
}

func NewRealtimeCollector(m *realtime.Metrics) *RealtimeCollector {
	return &RealtimeCollector{
		metrics: m,

		connectsTotal:       newRealtimeDesc("connects_total", "Total realtime WebSocket connections opened."),
		disconnectsTotal:    newRealtimeDesc("disconnects_total", "Total realtime WebSocket connections closed."),
		activeConnections:   newRealtimeDesc("active_connections", "Current realtime WebSocket connections."),
		slowEvictionsTotal:  newRealtimeDesc("slow_evictions_total", "Total realtime clients evicted for slow consumption."),
		messagesSentTotal:   newRealtimeDesc("messages_sent_total", "Total realtime messages sent."),
		messagesDropped:     newRealtimeDesc("messages_dropped_total", "Total realtime messages dropped."),
		redisConnected:      newRealtimeDesc("redis_connected", "Whether the realtime Redis relay is connected."),
		redisXAddTotal:      newRealtimeDesc("redis_xadd_total", "Total Redis XADD operations by the realtime relay."),
		redisXAddErrors:     newRealtimeDesc("redis_xadd_errors_total", "Total Redis XADD errors by the realtime relay."),
		redisXReadTotal:     newRealtimeDesc("redis_xread_total", "Total Redis XREAD operations by the realtime relay."),
		redisXReadErrors:    newRealtimeDesc("redis_xread_errors_total", "Total Redis XREAD errors by the realtime relay."),
		redisAckTotal:       newRealtimeDesc("redis_ack_total", "Total Redis stream acknowledgements by the realtime relay."),
		redisMirrorErrors:   prometheus.NewDesc("multica_realtime_redis_mirror_errors_total", "Total Redis mirror write errors by the realtime relay.", []string{"target"}, nil),
		redisMirrorDiverged: newRealtimeDesc("redis_mirror_divergence_total", "Total Redis mirror divergence events by the realtime relay."),
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
		c.redisConnected,
		c.redisXAddTotal,
		c.redisXAddErrors,
		c.redisXReadTotal,
		c.redisXReadErrors,
		c.redisAckTotal,
		c.redisMirrorErrors,
		c.redisMirrorDiverged,
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
	ch <- prometheus.MustNewConstMetric(c.redisConnected, prometheus.GaugeValue, boolFloat(m.RedisConnected.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisXAddTotal, prometheus.CounterValue, float64(m.RedisXAddTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisXAddErrors, prometheus.CounterValue, float64(m.RedisXAddErrors.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisXReadTotal, prometheus.CounterValue, float64(m.RedisXReadTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisXReadErrors, prometheus.CounterValue, float64(m.RedisXReadErrors.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisAckTotal, prometheus.CounterValue, float64(m.RedisAckTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.redisMirrorErrors, prometheus.CounterValue, float64(m.RedisMirrorPrimaryErrors.Load()), "primary")
	ch <- prometheus.MustNewConstMetric(c.redisMirrorErrors, prometheus.CounterValue, float64(m.RedisMirrorSecondaryErrors.Load()), "secondary")
	ch <- prometheus.MustNewConstMetric(c.redisMirrorDiverged, prometheus.CounterValue, float64(m.RedisMirrorDivergenceTotal.Load()))
}

func boolFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}
