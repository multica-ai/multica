package auth

import "github.com/prometheus/client_golang/prometheus"

// PATLastUsedMetrics instruments the batched last_used_at writer. Register it
// once (see NewPATLastUsedMetrics) and pass it to NewBatchedPATLastUsedRecorder.
// A nil *PATLastUsedMetrics is safe — the recorder no-ops every increment.
type PATLastUsedMetrics struct {
	recordedTotal     prometheus.Counter
	deduplicatedTotal prometheus.Counter
	droppedTotal      prometheus.Counter
	pendingGauge      prometheus.Gauge
	flushBatchSize    prometheus.Histogram
	flushErrorsTotal  prometheus.Counter
	flushSkippedTotal prometheus.Counter
	panicRecoveredTot prometheus.Counter
}

// NewPATLastUsedMetrics registers the collectors on reg. Mirrors how the
// existing BusinessMetrics collectors are registered.
func NewPATLastUsedMetrics(reg prometheus.Registerer) *PATLastUsedMetrics {
	factory := func(c prometheus.Collector) { reg.MustRegister(c) }
	m := &PATLastUsedMetrics{
		recordedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "multica_pat_lastused_recorded_total",
			Help: "PAT last_used marks accepted into the pending set.",
		}),
		deduplicatedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "multica_pat_lastused_deduplicated_total",
			Help: "PAT last_used marks merged into an already-pending token.",
		}),
		droppedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "multica_pat_lastused_dropped_total",
			Help: "PAT last_used marks dropped because the pending set was full.",
		}),
		pendingGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "multica_pat_lastused_pending",
			Help: "PAT last_used token ids currently buffered for flush.",
		}),
		flushBatchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "multica_pat_lastused_flush_batch_size",
			Help:    "Token ids written per successful last_used flush chunk.",
			Buckets: []float64{1, 10, 50, 100, 250, 500},
		}),
		flushErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "multica_pat_lastused_flush_errors_total",
			Help: "PAT last_used flush chunks that failed with a DB error.",
		}),
		flushSkippedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "multica_pat_lastused_flush_skipped_batches_total",
			Help: "PAT last_used flush chunks dropped due to budget/prior-failure, not a DB error.",
		}),
		panicRecoveredTot: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "multica_pat_lastused_panic_recovered_total",
			Help: "Panics recovered inside a PAT last_used flush.",
		}),
	}
	for _, c := range []prometheus.Collector{
		m.recordedTotal, m.deduplicatedTotal, m.droppedTotal, m.pendingGauge,
		m.flushBatchSize, m.flushErrorsTotal, m.flushSkippedTotal, m.panicRecoveredTot,
	} {
		factory(c)
	}
	return m
}

func (m *PATLastUsedMetrics) recorded()          { m.recordedTotal.Inc() }
func (m *PATLastUsedMetrics) deduplicated()      { m.deduplicatedTotal.Inc() }
func (m *PATLastUsedMetrics) dropped()           { m.droppedTotal.Inc() }
func (m *PATLastUsedMetrics) setPending(n int)   { m.pendingGauge.Set(float64(n)) }
func (m *PATLastUsedMetrics) flushBatch(n int)   { m.flushBatchSize.Observe(float64(n)) }
func (m *PATLastUsedMetrics) flushError()        { m.flushErrorsTotal.Inc() }
func (m *PATLastUsedMetrics) flushSkippedBatch() { m.flushSkippedTotal.Inc() }
func (m *PATLastUsedMetrics) panicRecovered()    { m.panicRecoveredTot.Inc() }
