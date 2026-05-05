package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/multica-ai/multica/server/internal/daemonws"
)

type DaemonWSCollector struct {
	metrics *daemonws.Metrics

	connectsTotal        *prometheus.Desc
	disconnectsTotal     *prometheus.Desc
	activeConnections    *prometheus.Desc
	slowEvictionsTotal   *prometheus.Desc
	wakeupPublishedTotal *prometheus.Desc
	wakeupPublishErrors  *prometheus.Desc
	wakeupReceivedTotal  *prometheus.Desc
	wakeupDeliveredTotal *prometheus.Desc
}

func NewDaemonWSCollector(m *daemonws.Metrics) *DaemonWSCollector {
	return &DaemonWSCollector{
		metrics: m,

		connectsTotal:        newDaemonWSDesc("connects_total", "Total daemon WebSocket connections opened."),
		disconnectsTotal:     newDaemonWSDesc("disconnects_total", "Total daemon WebSocket connections closed."),
		activeConnections:    newDaemonWSDesc("active_connections", "Current daemon WebSocket connections."),
		slowEvictionsTotal:   newDaemonWSDesc("slow_evictions_total", "Total daemon WebSocket clients evicted for slow consumption."),
		wakeupPublishedTotal: newDaemonWSDesc("wakeup_published_total", "Total daemon wakeups published to the Redis relay."),
		wakeupPublishErrors:  newDaemonWSDesc("wakeup_publish_errors_total", "Total daemon wakeup Redis publish errors."),
		wakeupReceivedTotal:  newDaemonWSDesc("wakeup_received_total", "Total daemon wakeups received from the Redis relay."),
		wakeupDeliveredTotal: prometheus.NewDesc("multica_daemonws_wakeup_delivered_total", "Total daemon wakeup local delivery attempts.", []string{"result"}, nil),
	}
}

func newDaemonWSDesc(name, help string) *prometheus.Desc {
	return prometheus.NewDesc("multica_daemonws_"+name, help, nil, nil)
}

func (c *DaemonWSCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{
		c.connectsTotal,
		c.disconnectsTotal,
		c.activeConnections,
		c.slowEvictionsTotal,
		c.wakeupPublishedTotal,
		c.wakeupPublishErrors,
		c.wakeupReceivedTotal,
		c.wakeupDeliveredTotal,
	} {
		ch <- desc
	}
}

func (c *DaemonWSCollector) Collect(ch chan<- prometheus.Metric) {
	if c.metrics == nil {
		return
	}
	m := c.metrics
	ch <- prometheus.MustNewConstMetric(c.connectsTotal, prometheus.CounterValue, float64(m.ConnectsTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.disconnectsTotal, prometheus.CounterValue, float64(m.DisconnectsTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.activeConnections, prometheus.GaugeValue, float64(m.ActiveConnections.Load()))
	ch <- prometheus.MustNewConstMetric(c.slowEvictionsTotal, prometheus.CounterValue, float64(m.SlowEvictionsTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.wakeupPublishedTotal, prometheus.CounterValue, float64(m.WakeupPublishedTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.wakeupPublishErrors, prometheus.CounterValue, float64(m.WakeupPublishErrors.Load()))
	ch <- prometheus.MustNewConstMetric(c.wakeupReceivedTotal, prometheus.CounterValue, float64(m.WakeupReceivedTotal.Load()))
	ch <- prometheus.MustNewConstMetric(c.wakeupDeliveredTotal, prometheus.CounterValue, float64(m.WakeupDeliveredHit.Load()), "hit")
	ch <- prometheus.MustNewConstMetric(c.wakeupDeliveredTotal, prometheus.CounterValue, float64(m.WakeupDeliveredMiss.Load()), "miss")
}
