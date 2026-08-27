package metrics

import "github.com/prometheus/client_golang/prometheus"

// WecomMetrics is the production sink behind the WeCom adapter's Metrics
// interface (server/internal/integrations/wecom/metrics.go).
//
// The adapter is built to degrade quietly. A dial that fails and a handshake
// the server refuses both yield the connection back to the Supervisor, which
// backs off and tries again; an ingest queue the read loop has to wait on
// yields nothing and simply stops draining the socket until the worker
// catches up. None of the three changes anything an operator can see. A bot
// that has been down since Tuesday and a bot nobody happened to message today
// produce the same silence.
//
// The two connection counters are deliberately separate. A dial or a read that
// fails is infrastructure and usually recovers on its own; a handshake the
// server refuses on its merits is a wrong secret or a deleted bot, and it will
// repeat identically on every backoff until a person fixes the installation.
// Summed into one number the operator cannot tell "wait" from "rotate the
// credential".
//
// Which side of that line an ack falls on is classifySubscribeAck's call, in
// the adapter, and this package only counts the verdict. An ack it cannot
// verify — a throttle, a platform-side failure — is a connect failure, on the
// "wait" side, because rotating a credential that was fine costs a second
// outage.
//
// No installation_id label anywhere. It is the same class of unbounded
// identifier as workspace_id and session_id, which forbiddenMetricLabels
// rejects outright. Attribution falls to the structured logs and is uneven
// there: the two connection failures reach the Supervisor as a returned error
// and are logged with installation_id, while a blocked ingest queue is
// counted and nothing else — the counter says some bot is behind, not which.
// The outbound pair answers a different question from the connection ones, and
// it is the question GH #7215 and #6890 were filed as: a reply was produced,
// the transcript has it, and the WeCom chat stayed quiet. Several unrelated
// causes end that way — no socket in this process, a turn that originated in
// the web UI, a revoked installation, a frame the platform refused — and from
// the chat they are one symptom. Delivered is the denominator: without it, a
// drop rate of zero and a bot nobody messaged look identical, which is the
// same ambiguity the connection counters exist to remove.
//
// reason is a closed set (wecom/outbound_outcome.go). It is the only label
// here, and it stays bounded by construction — no installation, workspace or
// session id, same rule as everywhere else in this package.
type WecomMetrics struct {
	ConnectFailures      prometheus.Counter
	AuthFailures         prometheus.Counter
	CallbacksQueued      prometheus.Counter
	CallbackQueueBlocked prometheus.Counter
	OutboundDelivered    prometheus.Counter
	OutboundDropped      *prometheus.CounterVec
	OutboundSkipped      *prometheus.CounterVec
	AttachmentDelivered  prometheus.Counter
	AttachmentDropped    *prometheus.CounterVec
	AttachmentSheds      prometheus.Counter
}

func NewWecomMetrics() *WecomMetrics {
	counter := func(name, help string) prometheus.Counter {
		return prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "multica", Subsystem: "wecom", Name: name, Help: help,
		})
	}
	return &WecomMetrics{
		ConnectFailures: counter("connect_failures_total",
			"Long-connection attempts that failed for a reason nobody has to act on: the socket never came up, or the server answered the handshake with a code that only means it could not verify the bot (a throttle, a platform-side failure). Excludes credential rejections, which are counted apart."),
		AuthFailures: counter("auth_failures_total",
			"Long-connection handshakes the server refused on the credentials themselves (WeCom errcode 40001 / 40013). The bot stays down until somebody fixes the installation."),
		CallbacksQueued: counter("inbound_callbacks_total",
			"Inbound callbacks handed to the ingest worker. The baseline every other inbound number is read against."),
		CallbackQueueBlocked: counter("inbound_queue_blocked_total",
			"Times the read loop had to wait on a full ingest queue. Backpressure by design; a rising rate means the engine is behind and the socket is about to stop being drained."),
		OutboundDelivered: counter("outbound_delivered_total",
			"Agent replies this adapter put in front of a WeCom user. The denominator the drop breakdown is read against."),
		OutboundDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica", Subsystem: "wecom", Name: "outbound_dropped_total",
			Help: "Agent replies the adapter owed a WeCom user and did not deliver, by reason. Every reason here means somebody in WeCom is waiting on an answer that is not coming; the completions the adapter was never going to deliver are counted apart, in outbound_skipped_total.",
		}, []string{"reason"}),
		OutboundSkipped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica", Subsystem: "wecom", Name: "outbound_skipped_total",
			Help: "Completions this adapter was never going to deliver to WeCom, by reason. Kept apart from dropped because none of these is a delivery failure: origin_not_channel is a question typed in Multica on a WeCom-bound session, installation_inactive means there is no longer an installation to deliver through, nothing_to_say is an empty completion carrying no file. Counting them as drops would make ordinary web usage read as a WeCom outage.",
		}, []string{"reason"}),
		AttachmentDelivered: counter("outbound_attachment_delivered_total",
			"Files put in front of a WeCom user. Counts FILES; the outbound_delivered/dropped/skipped trio counts REPLIES."),
		AttachmentDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica", Subsystem: "wecom", Name: "outbound_attachment_dropped_total",
			Help: "Files an agent produced that did not reach the WeCom user, by reason. Separate from the reply counters because a reply whose words arrived and whose file did not is a delivered reply with a failed attachment, and one number cannot say both.",
		}, []string{"reason"}),
		AttachmentSheds: counter("outbound_attachment_delivery_shed_total",
			"Attachment delivery attempts refused admission before the lookup ran. Counts SCHEDULING decisions, not files: at that point nothing knows whether the turn carries zero files or five, so a per-file count from this gate would fabricate cardinality."),
	}
}

func (m *WecomMetrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.ConnectFailures, m.AuthFailures,
		m.CallbacksQueued, m.CallbackQueueBlocked,
		m.OutboundDelivered, m.OutboundDropped, m.OutboundSkipped,
		m.AttachmentDelivered, m.AttachmentDropped, m.AttachmentSheds,
	}
}

// ---- the adapter's Metrics interface ----

func (m *WecomMetrics) RecordConnectFailure()       { m.ConnectFailures.Inc() }
func (m *WecomMetrics) RecordAuthFailure()          { m.AuthFailures.Inc() }
func (m *WecomMetrics) RecordCallbackQueued()       { m.CallbacksQueued.Inc() }
func (m *WecomMetrics) RecordCallbackQueueBlocked() { m.CallbackQueueBlocked.Inc() }
func (m *WecomMetrics) RecordOutboundDelivered()    { m.OutboundDelivered.Inc() }
func (m *WecomMetrics) RecordOutboundDropped(reason string) {
	m.OutboundDropped.WithLabelValues(reason).Inc()
}
func (m *WecomMetrics) RecordOutboundSkipped(reason string) {
	m.OutboundSkipped.WithLabelValues(reason).Inc()
}
func (m *WecomMetrics) RecordAttachmentDelivered() { m.AttachmentDelivered.Inc() }
func (m *WecomMetrics) RecordAttachmentDropped(reason string) {
	m.AttachmentDropped.WithLabelValues(reason).Inc()
}
func (m *WecomMetrics) RecordAttachmentDeliveryShed() { m.AttachmentSheds.Inc() }
