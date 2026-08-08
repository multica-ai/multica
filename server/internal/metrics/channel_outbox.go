package metrics

import "github.com/prometheus/client_golang/prometheus"

// ChannelOutboxMetrics is the production implementation of the outbound
// queue's metrics sink (the Metrics interface in
// internal/integrations/channel/outbox).
//
// channel_type is the only identifying label: it is bounded by the number of
// adapters, and without it a dead realtime path on one platform hides inside
// another's traffic. Deliberately no installation_id or workspace_id anywhere
// — that is the unbounded cardinality forbiddenMetricLabels rejects outright,
// and every call site already carries it in the structured logs. The other
// label values go through fixed allow-lists so a caller cannot widen the
// series space either.
type ChannelOutboxMetrics struct {
	Enqueued            *prometheus.CounterVec
	Delivery            *prometheus.CounterVec
	ReconcileRaceLosses *prometheus.CounterVec
}

// Enqueue paths. These mirror the constants in the outbox package.
const (
	ChannelOutboxPathRealtime = "realtime"
	// ChannelOutboxPathReconcile is the compensating scanner. Its window lags
	// on purpose, so a sustained non-zero rate here is the alerting signal
	// that the realtime path has stopped working: replies still arrive, just
	// tens of seconds late.
	ChannelOutboxPathReconcile = "reconcile"
)

// Delivery outcomes. These mirror the constants in the outbox package.
const (
	ChannelOutboxDeliverySent     = "sent"
	ChannelOutboxDeliveryDeferred = "deferred"
	ChannelOutboxDeliveryRetried  = "retried"
	ChannelOutboxDeliveryFailed   = "failed"
	// ChannelOutboxDeliveryFenced is a row dropped before send because its
	// installation or session binding stopped being deliverable after
	// enqueue. Distinct from "failed", which is an attempted send.
	ChannelOutboxDeliveryFenced = "fenced"
)

// labelOther is the sink for a value outside its allow-list. Bucketing beats
// dropping: the observation still shows up, and a spike on "other" is itself
// the signal that an allow-list has drifted behind the code.
const labelOther = "other"

var (
	knownChannelOutboxTypes = map[string]struct{}{
		"wecom":    {},
		"feishu":   {},
		"lark":     {},
		"slack":    {},
		"dingtalk": {},
	}
	knownChannelOutboxPaths = map[string]struct{}{
		ChannelOutboxPathRealtime:  {},
		ChannelOutboxPathReconcile: {},
	}
	knownChannelOutboxSourceKinds = map[string]struct{}{
		"chat_done":      {},
		"task_failed":    {},
		"binding_prompt": {},
		"inbox_notify":   {},
	}
	knownChannelOutboxOutcomes = map[string]struct{}{
		ChannelOutboxDeliverySent:     {},
		ChannelOutboxDeliveryDeferred: {},
		ChannelOutboxDeliveryRetried:  {},
		ChannelOutboxDeliveryFailed:   {},
		ChannelOutboxDeliveryFenced:   {},
	}
)

func NewChannelOutboxMetrics() *ChannelOutboxMetrics {
	return &ChannelOutboxMetrics{
		Enqueued: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "channel_outbox",
			Name:      "enqueued_total",
			Help:      "Rows enqueued onto the channel outbound queue, by channel, producing path, and source kind.",
		}, []string{"channel_type", "path", "source_kind"}),
		Delivery: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "channel_outbox",
			Name:      "delivery_total",
			Help:      "Outbound delivery attempts by channel and outcome (sent, deferred, retried, failed, fenced).",
		}, []string{"channel_type", "outcome"}),
		ReconcileRaceLosses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "channel_outbox",
			Name:      "reconcile_race_lost_total",
			Help:      "Reconciler enqueues that lost the business-key race to the realtime path (expected background noise).",
		}, []string{"channel_type"}),
	}
}

func (m *ChannelOutboxMetrics) Collectors() []prometheus.Collector {
	if m == nil {
		return nil
	}
	return []prometheus.Collector{m.Enqueued, m.Delivery, m.ReconcileRaceLosses}
}

func (m *ChannelOutboxMetrics) RecordEnqueued(channelType, path, sourceKind string) {
	if m == nil || m.Enqueued == nil {
		return
	}
	m.Enqueued.WithLabelValues(
		bucketLabel(channelType, knownChannelOutboxTypes),
		bucketLabel(path, knownChannelOutboxPaths),
		bucketLabel(sourceKind, knownChannelOutboxSourceKinds),
	).Inc()
}

func (m *ChannelOutboxMetrics) RecordDelivery(channelType, outcome string) {
	if m == nil || m.Delivery == nil {
		return
	}
	m.Delivery.WithLabelValues(
		bucketLabel(channelType, knownChannelOutboxTypes),
		bucketLabel(outcome, knownChannelOutboxOutcomes),
	).Inc()
}

func (m *ChannelOutboxMetrics) RecordReconcileRaceLost(channelType string) {
	if m == nil || m.ReconcileRaceLosses == nil {
		return
	}
	m.ReconcileRaceLosses.WithLabelValues(bucketLabel(channelType, knownChannelOutboxTypes)).Inc()
}

func bucketLabel(value string, known map[string]struct{}) string {
	if _, ok := known[value]; ok {
		return value
	}
	return labelOther
}
