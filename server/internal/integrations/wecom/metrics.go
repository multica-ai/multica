package wecom

// WecomMetrics records WeCom adapter health signals. The production
// implementation is metrics.WecomAdapterMetrics (internal/metrics/wecom.go);
// NoopMetrics is the default when the metrics listener is disabled.
//
// No method takes an installation id: it cannot become a Prometheus label
// (unbounded cardinality, same class as workspace_id), and every call site
// already logs it. Queue depth is intentionally absent too — it is a
// DB-derived value, so it belongs in the scrape-time sampler
// (multica_channel_outbound_queue_depth) where every server instance reports
// the same number, not in a push method only the lease holder would call.
type WecomMetrics interface {
	RecordConnectFailure()
	RecordAuthFailure()
	RecordWelcomeSkippedNonSingle()
	RecordWelcomeFailure()
	// RecordOutboundEnqueued attributes an enqueued reply to the path that
	// produced it: enqueuePathFast (the events.Bus subscriber) or
	// enqueuePathReconcile (the lagged compensating scanner). A non-zero
	// reconcile rate means the realtime path is broken and users are seeing
	// late replies.
	RecordOutboundEnqueued(path, sourceKind string)
	// RecordOutboundDelivery records the outcome of one delivery attempt on
	// the outbox consumer.
	RecordOutboundDelivery(outcome string)
	// RecordReconcileRaceLost counts reconciler enqueues that lost the
	// business-key race to the fast path. Unlike a reconcile-path enqueue,
	// this is expected background noise.
	RecordReconcileRaceLost()
	// RecordInstallSessionTerminal records how an install session ended:
	// installResultSucceeded, or the InstallError* reason it failed with.
	RecordInstallSessionTerminal(result string)
}

// Enqueue paths and delivery outcomes reported through WecomMetrics. They
// mirror the allow-lists in internal/metrics so an unknown value degrades to
// an "other" bucket instead of inflating cardinality.
const (
	enqueuePathFast      = "fast"
	enqueuePathReconcile = "reconcile"

	deliveryOutcomeSent     = "sent"
	deliveryOutcomeDeferred = "deferred"
	deliveryOutcomeRetried  = "retried"
	deliveryOutcomeFailed   = "failed"
	deliveryOutcomeFenced   = "fenced"

	// installResultSucceeded is the one non-error terminal result; the
	// failures reuse the InstallError* reason strings verbatim.
	installResultSucceeded = "succeeded"
)

type noopWecomMetrics struct{}

func (noopWecomMetrics) RecordConnectFailure()                 {}
func (noopWecomMetrics) RecordAuthFailure()                    {}
func (noopWecomMetrics) RecordWelcomeSkippedNonSingle()        {}
func (noopWecomMetrics) RecordWelcomeFailure()                 {}
func (noopWecomMetrics) RecordOutboundEnqueued(string, string) {}
func (noopWecomMetrics) RecordOutboundDelivery(string)         {}
func (noopWecomMetrics) RecordReconcileRaceLost()              {}
func (noopWecomMetrics) RecordInstallSessionTerminal(string)   {}

// NoopMetrics returns a WecomMetrics that discards all observations.
func NoopMetrics() WecomMetrics { return noopWecomMetrics{} }
