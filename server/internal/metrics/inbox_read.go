package metrics

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus"
)

// Labels contain fixed operation/reason names only, never user/workspace IDs.
type InboxReadMetrics struct {
	active        prometheus.Gauge
	queued        prometheus.Gauge
	rejected      *prometheus.CounterVec
	duration      *prometheus.HistogramVec
	queryDuration *prometheus.HistogramVec
	timeouts      *prometheus.CounterVec
}

func NewInboxReadMetrics() *InboxReadMetrics {
	return &InboxReadMetrics{
		active:        prometheus.NewGauge(prometheus.GaugeOpts{Name: "multica_inbox_read_active", Help: "Inbox read transactions holding execution capacity."}),
		queued:        prometheus.NewGauge(prometheus.GaugeOpts{Name: "multica_inbox_read_queued", Help: "Inbox reads waiting for execution capacity."}),
		rejected:      prometheus.NewCounterVec(prometheus.CounterOpts{Name: "multica_inbox_read_rejected_total", Help: "Inbox reads rejected before acquiring a connection."}, []string{"operation", "reason"}),
		duration:      prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "multica_inbox_read_duration_seconds", Help: "Inbox transaction duration including acquisition and cleanup, excluding admission wait.", Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2, 3, 5}}, []string{"operation"}),
		queryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "multica_inbox_read_query_duration_seconds", Help: "Inbox SQL execution and row scanning duration.", Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2, 3, 5}}, []string{"operation"}),
		timeouts:      prometheus.NewCounterVec(prometheus.CounterOpts{Name: "multica_inbox_read_timeouts_total", Help: "Inbox database or connection acquisition timeouts."}, []string{"operation"}),
	}
}

func (m *InboxReadMetrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{m.active, m.queued, m.rejected, m.duration, m.queryDuration, m.timeouts}
}

func (m *InboxReadMetrics) ObserveQuery(operation string, elapsed time.Duration) {
	if m != nil {
		m.queryDuration.WithLabelValues(operation).Observe(elapsed.Seconds())
	}
}
func (m *InboxReadMetrics) Active(n float64) {
	if m != nil {
		m.active.Add(n)
	}
}
func (m *InboxReadMetrics) Queue(n float64) {
	if m != nil {
		m.queued.Add(n)
	}
}
func (m *InboxReadMetrics) Reject(operation, reason string) {
	if m != nil {
		m.rejected.WithLabelValues(operation, reason).Inc()
	}
}
func (m *InboxReadMetrics) Observe(operation string, elapsed time.Duration, err error) {
	if m == nil {
		return
	}
	m.duration.WithLabelValues(operation).Observe(elapsed.Seconds())
	if isInboxTimeout(err) {
		m.timeouts.WithLabelValues(operation).Inc()
	}
}

func isInboxTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &pgErr) && pgErr.Code == "57014")
}
