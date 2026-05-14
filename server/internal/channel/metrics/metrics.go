package metrics

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/multica-ai/multica/server/internal/channel/inbound"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

const unknown = "unknown"

type Recorder struct {
	inboundEvents       *prometheus.CounterVec
	inboundStepDuration *prometheus.HistogramVec
	inboundStepErrors   *prometheus.CounterVec
	outboundCards       *prometheus.CounterVec
	outboundFailures    *prometheus.CounterVec
	outboundOutbox      *prometheus.CounterVec
	adapterDrops        *prometheus.CounterVec
	leaderState         *prometheus.GaugeVec
	adapterConnected    *prometheus.GaugeVec
}

var M = NewRecorder()

func NewRecorder() *Recorder {
	return &Recorder{
		inboundEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "channel",
			Name:      "inbound_events_total",
			Help:      "Total channel inbound pipeline runs.",
		}, []string{"provider", "event_type", "result", "terminal_step"}),
		inboundStepDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "multica",
			Subsystem: "channel",
			Name:      "inbound_step_duration_seconds",
			Help:      "Channel inbound step duration.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"provider", "step", "result"}),
		inboundStepErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "channel",
			Name:      "inbound_step_errors_total",
			Help:      "Total channel inbound step errors.",
		}, []string{"provider", "step", "error_class"}),
		outboundCards: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "channel",
			Name:      "outbound_cards_total",
			Help:      "Total channel outbound card send outcomes.",
		}, []string{"provider", "event_kind", "result"}),
		outboundFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "channel",
			Name:      "outbound_failures_total",
			Help:      "Total channel outbound failures by stage.",
		}, []string{"provider", "event_kind", "stage", "retryable"}),
		outboundOutbox: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "channel",
			Name:      "outbound_outbox_notifications_total",
			Help:      "Total notifications handled by the durable outbound outbox.",
		}, []string{"provider", "result"}),
		adapterDrops: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "channel",
			Name:      "adapter_dropped_events_total",
			Help:      "Total channel adapter events dropped before the inbound pipeline.",
		}, []string{"provider", "reason"}),
		leaderState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "multica",
			Subsystem: "channel",
			Name:      "leader_state",
			Help:      "Whether this process currently owns the channel leader slot.",
		}, []string{"provider"}),
		adapterConnected: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "multica",
			Subsystem: "channel",
			Name:      "adapter_connected",
			Help:      "Whether this process currently has a connected channel adapter.",
		}, []string{"provider"}),
	}
}

func (r *Recorder) Collectors() []prometheus.Collector {
	if r == nil {
		return nil
	}
	return []prometheus.Collector{
		r.inboundEvents,
		r.inboundStepDuration,
		r.inboundStepErrors,
		r.outboundCards,
		r.outboundFailures,
		r.outboundOutbox,
		r.adapterDrops,
		r.leaderState,
		r.adapterConnected,
	}
}

func (r *Recorder) StepDone(evt port.InboundEvent, step string, decision inbound.Decision, duration time.Duration, err error) {
	if r == nil {
		return
	}
	result := inboundResult(decision, err)
	r.inboundStepDuration.WithLabelValues(label(evt.ChannelName), label(step), result).Observe(duration.Seconds())
	if err != nil {
		r.inboundStepErrors.WithLabelValues(label(evt.ChannelName), label(step), errorClass(err)).Inc()
	}
}

func (r *Recorder) PipelineDone(evt port.InboundEvent, outcome inbound.Outcome, _ time.Duration, err error) {
	if r == nil {
		return
	}
	r.inboundEvents.WithLabelValues(
		label(evt.ChannelName),
		label(string(evt.Type)),
		inboundResult(outcome.Decision, err),
		label(outcome.Terminal),
	).Inc()
}

func (r *Recorder) RecordOutboundCard(provider, eventKind, result string) {
	if r == nil {
		return
	}
	r.outboundCards.WithLabelValues(label(provider), label(eventKind), label(result)).Inc()
}

func (r *Recorder) RecordOutboundFailure(provider, eventKind, stage string, retryable bool) {
	if r == nil {
		return
	}
	r.outboundFailures.WithLabelValues(label(provider), label(eventKind), label(stage), boolLabel(retryable)).Inc()
}

func (r *Recorder) RecordOutboundOutbox(provider, result string, count int) {
	if r == nil || count <= 0 {
		return
	}
	r.outboundOutbox.WithLabelValues(label(provider), label(result)).Add(float64(count))
}

func (r *Recorder) RecordAdapterDrop(provider, reason string) {
	if r == nil {
		return
	}
	r.adapterDrops.WithLabelValues(label(provider), label(reason)).Inc()
}

func (r *Recorder) SetLeaderState(provider string, active bool) {
	if r == nil {
		return
	}
	r.leaderState.WithLabelValues(label(provider)).Set(boolFloat(active))
}

func (r *Recorder) SetAdapterConnected(provider string, connected bool) {
	if r == nil {
		return
	}
	r.adapterConnected.WithLabelValues(label(provider)).Set(boolFloat(connected))
}

func inboundResult(decision inbound.Decision, err error) string {
	if err != nil {
		return "error"
	}
	if decision == inbound.DecisionSkip {
		return "skip"
	}
	return "ok"
}

func errorClass(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	default:
		return "other"
	}
}

func label(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return unknown
	}
	return value
}

func boolLabel(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func boolFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

var _ inbound.Observer = (*Recorder)(nil)
