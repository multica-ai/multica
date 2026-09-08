package metrics

import "github.com/prometheus/client_golang/prometheus"

// ChannelPushMetrics observes the inbox-push notifier: what happened to every
// inbox:new it saw, split by why.
//
// One counter rather than several, because the question operators ask is a
// ratio — of the review moments that reached the notifier, how many actually
// reached a human's phone. Separate counters make that a join.
type ChannelPushMetrics struct {
	pushes *prometheus.CounterVec
}

func NewChannelPushMetrics() *ChannelPushMetrics {
	return &ChannelPushMetrics{
		pushes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "channel_push",
			Name:      "pushes_total",
			Help: "Inbox notifications the IM push notifier handled, by outcome and destination channel. " +
				"Exactly one is recorded per inbox:new. delivered/handed_off are successes; " +
				"not_whitelisted/not_member/unbound/no_adapter are routine skips; " +
				"failed/record_failed/empty/malformed are defects. channel_type is empty on the " +
				"paths that end before a binding is resolved.",
		}, []string{"outcome", "channel_type"}),
	}
}

// RecordPush satisfies notify.Metrics.
func (m *ChannelPushMetrics) RecordPush(outcome, channelType string) {
	if m == nil {
		return
	}
	m.pushes.WithLabelValues(outcome, channelType).Inc()
}

func (m *ChannelPushMetrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{m.pushes}
}
