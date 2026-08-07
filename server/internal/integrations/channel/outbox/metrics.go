package outbox

import "sync/atomic"

// Metrics records outbound queue health. The production implementation is
// metrics.ChannelOutboxMetrics; NoopMetrics is the default when the metrics
// listener is disabled.
//
// Every method takes channel_type because that is the one label worth having
// here: it is bounded by the number of adapters, and without it a broken
// realtime path on one platform hides in another's traffic. No method takes an
// installation id — that is unbounded cardinality, the same class as
// workspace_id — and every call site already logs it.
//
// Queue depth is deliberately absent: it is a DB-derived value, so it belongs
// in the scrape-time sampler where every replica reports the same number,
// not in a push method only the lease holder would call.
type Metrics interface {
	// RecordEnqueued attributes an enqueued row to the path that produced it:
	// EnqueuePathRealtime or EnqueuePathReconcile. A sustained non-zero
	// reconcile rate means the realtime path is broken and users are waiting
	// tens of seconds longer than they should.
	RecordEnqueued(channelType, path, sourceKind string)
	// RecordDelivery records the outcome of one delivery attempt.
	RecordDelivery(channelType, outcome string)
	// RecordReconcileRaceLost counts reconciler enqueues that lost the
	// business-key race to the realtime path. Unlike a reconcile-path
	// enqueue, this is expected background noise.
	RecordReconcileRaceLost(channelType string)
}

// Enqueue paths reported through Metrics.
const (
	// EnqueuePathRealtime is the producer that enqueues as soon as the
	// business result exists.
	EnqueuePathRealtime = "realtime"
	// EnqueuePathReconcile is the compensating scanner. Its window lags on
	// purpose, so traffic here is the alerting signal for a dead realtime
	// path rather than routine work.
	EnqueuePathReconcile = "reconcile"
)

// Delivery outcomes reported through Metrics.
const (
	OutcomeSent     = "sent"
	OutcomeDeferred = "deferred"
	OutcomeRetried  = "retried"
	OutcomeFailed   = "failed"
	// OutcomeFenced is a row dropped before send because its installation or
	// session binding stopped being deliverable after enqueue. Distinct from
	// OutcomeFailed, which is a send that was attempted and did not succeed.
	OutcomeFenced = "fenced"
)

type noopMetrics struct{}

func (noopMetrics) RecordEnqueued(string, string, string) {}
func (noopMetrics) RecordDelivery(string, string)         {}
func (noopMetrics) RecordReconcileRaceLost(string)        {}

// NoopMetrics returns a Metrics that discards all observations.
func NoopMetrics() Metrics { return noopMetrics{} }

// MetricsRef is a Metrics whose sink can be swapped after construction.
//
// It exists because of boot order: the channel adapters are wired before the
// Prometheus registry is built (the registry needs the metrics listener config,
// which is read later), so a producer or consumer created during wiring has no
// real sink to bind to yet. Handing them a ref and pointing it at the registry
// once it exists keeps that ordering out of every adapter's constructor.
//
// Safe for concurrent use: a swap mid-flight loses no observation, it only
// decides which sink counts it.
type MetricsRef struct {
	target atomic.Pointer[Metrics]
}

var _ Metrics = (*MetricsRef)(nil)

// NewMetricsRef returns a ref that discards observations until Set is called.
func NewMetricsRef() *MetricsRef { return &MetricsRef{} }

// Set points the ref at m. A nil m reverts the ref to discarding.
func (r *MetricsRef) Set(m Metrics) {
	if r == nil {
		return
	}
	if m == nil {
		r.target.Store(nil)
		return
	}
	r.target.Store(&m)
}

func (r *MetricsRef) sink() Metrics {
	if r == nil {
		return noopMetrics{}
	}
	if m := r.target.Load(); m != nil {
		return *m
	}
	return noopMetrics{}
}

func (r *MetricsRef) RecordEnqueued(channelType, path, sourceKind string) {
	r.sink().RecordEnqueued(channelType, path, sourceKind)
}

func (r *MetricsRef) RecordDelivery(channelType, outcome string) {
	r.sink().RecordDelivery(channelType, outcome)
}

func (r *MetricsRef) RecordReconcileRaceLost(channelType string) {
	r.sink().RecordReconcileRaceLost(channelType)
}
