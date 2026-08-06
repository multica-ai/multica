package metrics

import "github.com/prometheus/client_golang/prometheus"

// WecomAdapterMetrics is the production implementation of the WeCom adapter's
// metrics sink (the WecomMetrics interface in internal/integrations/wecom).
// The adapter shipped with only a no-op implementation, so every WeCom health
// signal the spec asks for was silently discarded — that is how a dead
// realtime outbound path stayed invisible behind a 30s-lagged reconciler.
//
// Deliberately no installation_id label anywhere: an installation id is the
// same class of unbounded identifier as workspace_id / session_id, which
// forbiddenMetricLabels rejects outright. Per-installation attribution belongs
// in the structured logs, which already carry installation_id at every call
// site. Every label value here goes through a fixed allow-list so a caller
// cannot widen the series space either.
type WecomAdapterMetrics struct {
	ConnectFailures     prometheus.Counter
	AuthFailures        prometheus.Counter
	WelcomeSkipped      prometheus.Counter
	WelcomeFailures     prometheus.Counter
	OutboundEnqueued    *prometheus.CounterVec
	OutboundDelivery    *prometheus.CounterVec
	ReconcileRaceLosses prometheus.Counter
	InstallSessions     *prometheus.CounterVec
}

const (
	// WecomEnqueuePathFast is the events.Bus subscriber that enqueues a reply
	// the moment a task reaches a terminal state.
	WecomEnqueuePathFast = "fast"
	// WecomEnqueuePathReconcile is the compensating scanner. Its window is
	// deliberately lagged, so a sustained non-zero rate on this path is the
	// alerting signal that the fast path has stopped working: replies still
	// arrive, just tens of seconds late.
	WecomEnqueuePathReconcile = "reconcile"
)

const (
	WecomDeliverySent     = "sent"
	WecomDeliveryDeferred = "deferred"
	WecomDeliveryRetried  = "retried"
	WecomDeliveryFailed   = "failed"
	// WecomDeliveryFenced is a row dropped before send because its
	// installation or session binding stopped being deliverable after
	// enqueue. Distinct from "failed" (a send that did not succeed).
	WecomDeliveryFenced = "fenced"
)

var (
	knownWecomEnqueuePaths = map[string]struct{}{
		WecomEnqueuePathFast:      {},
		WecomEnqueuePathReconcile: {},
	}
	knownWecomSourceKinds = map[string]struct{}{
		"chat_done":      {},
		"task_failed":    {},
		"binding_prompt": {},
	}
	knownWecomDeliveryOutcomes = map[string]struct{}{
		WecomDeliverySent:     {},
		WecomDeliveryDeferred: {},
		WecomDeliveryRetried:  {},
		WecomDeliveryFailed:   {},
		WecomDeliveryFenced:   {},
	}
	// knownWecomInstallResults mirrors the adapter's install terminal states:
	// "succeeded" plus each InstallError* reason string.
	knownWecomInstallResults = map[string]struct{}{
		"succeeded":                {},
		"expired":                  {},
		"generate_failed":          {},
		"integration_unconfigured": {},
		"installation_conflict":    {},
		"wecom_protocol_error":     {},
		"internal_error":           {},
	}
)

func NewWecomAdapterMetrics() *WecomAdapterMetrics {
	m := &WecomAdapterMetrics{
		ConnectFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "wecom",
			Name:      "connect_failures_total",
			Help:      "WeCom long-connection dial or handshake failures (excludes auth rejections).",
		}),
		AuthFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "wecom",
			Name:      "auth_failures_total",
			Help:      "WeCom subscribe handshakes rejected with a non-zero errcode; a sustained rate means a stale or revoked bot secret.",
		}),
		WelcomeSkipped: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "wecom",
			Name:      "welcome_skipped_non_single_total",
			Help:      "Welcome messages skipped because the chat was not a single (p2p) chat.",
		}),
		WelcomeFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "wecom",
			Name:      "welcome_failures_total",
			Help:      "Welcome messages that failed to send.",
		}),
		OutboundEnqueued: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "wecom",
			Name:      "outbound_enqueued_total",
			Help:      "Outbound replies enqueued, split by which path enqueued them. path=\"reconcile\" means the realtime path missed the event and the reply is arriving late.",
		}, []string{"path", "source_kind"}),
		OutboundDelivery: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "wecom",
			Name:      "outbound_delivery_total",
			Help:      "Terminal and non-terminal outcomes of outbound queue delivery attempts.",
		}, []string{"outcome"}),
		ReconcileRaceLosses: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "wecom",
			Name:      "reconcile_race_losses_total",
			Help:      "Reconciler enqueues that hit the business-key conflict because the realtime path won the race. Healthy background noise, unlike outbound_enqueued_total{path=\"reconcile\"}.",
		}),
		InstallSessions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "wecom",
			Name:      "install_sessions_total",
			Help:      "Install sessions that reached a terminal state, by result: \"succeeded\" or the failure reason recorded on the session.",
		}, []string{"result"}),
	}
	// Pre-seed the label combinations we know about so rate() over a freshly
	// restarted process is not "no data" and an alert on the reconcile path
	// has a zero baseline to compare against.
	for path := range knownWecomEnqueuePaths {
		for _, kind := range []string{"chat_done", "task_failed"} {
			m.OutboundEnqueued.WithLabelValues(path, kind)
		}
	}
	for outcome := range knownWecomDeliveryOutcomes {
		m.OutboundDelivery.WithLabelValues(outcome)
	}
	return m
}

func (m *WecomAdapterMetrics) Collectors() []prometheus.Collector {
	if m == nil {
		return nil
	}
	return []prometheus.Collector{
		m.ConnectFailures,
		m.AuthFailures,
		m.WelcomeSkipped,
		m.WelcomeFailures,
		m.OutboundEnqueued,
		m.OutboundDelivery,
		m.ReconcileRaceLosses,
		m.InstallSessions,
	}
}

func (m *WecomAdapterMetrics) RecordConnectFailure() {
	if m == nil {
		return
	}
	m.ConnectFailures.Inc()
}

func (m *WecomAdapterMetrics) RecordAuthFailure() {
	if m == nil {
		return
	}
	m.AuthFailures.Inc()
}

func (m *WecomAdapterMetrics) RecordWelcomeSkippedNonSingle() {
	if m == nil {
		return
	}
	m.WelcomeSkipped.Inc()
}

func (m *WecomAdapterMetrics) RecordWelcomeFailure() {
	if m == nil {
		return
	}
	m.WelcomeFailures.Inc()
}

func (m *WecomAdapterMetrics) RecordOutboundEnqueued(path, sourceKind string) {
	if m == nil {
		return
	}
	m.OutboundEnqueued.WithLabelValues(
		normalizeAllowed(path, knownWecomEnqueuePaths),
		normalizeAllowed(sourceKind, knownWecomSourceKinds),
	).Inc()
}

func (m *WecomAdapterMetrics) RecordOutboundDelivery(outcome string) {
	if m == nil {
		return
	}
	m.OutboundDelivery.WithLabelValues(normalizeAllowed(outcome, knownWecomDeliveryOutcomes)).Inc()
}

func (m *WecomAdapterMetrics) RecordReconcileRaceLost() {
	if m == nil {
		return
	}
	m.ReconcileRaceLosses.Inc()
}

func (m *WecomAdapterMetrics) RecordInstallSessionTerminal(result string) {
	if m == nil {
		return
	}
	m.InstallSessions.WithLabelValues(normalizeAllowed(result, knownWecomInstallResults)).Inc()
}

// normalizeAllowed collapses anything outside the allow-list to "other" so a
// new enum value in the adapter cannot inflate cardinality before someone
// adds it here deliberately.
func normalizeAllowed(value string, allowed map[string]struct{}) string {
	if _, ok := allowed[value]; ok {
		return value
	}
	return "other"
}
