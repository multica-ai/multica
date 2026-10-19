package metrics

import "github.com/prometheus/client_golang/prometheus"

// WecomMetrics implements the adapter's production metrics sink.
// Separate infrastructure connection failures from credential rejections so
// operators can distinguish retryable outages from installations needing repair.
// The adapter classifies acknowledgements; this sink records its verdict.
//
// Outbound outcomes distinguish delivered, dropped, skipped, and unconfirmed
// frames. A missing acknowledgement may already be visible to the user and is
// not a confirmed drop. Reasons are bounded by wecom/outbound_outcome.go;
// installation, workspace, and session IDs belong in logs, never metric labels.
type WecomMetrics struct {
	ConnectFailures       prometheus.Counter
	AuthFailures          prometheus.Counter
	CallbacksQueued       prometheus.Counter
	CallbackQueueBlocked  prometheus.Counter
	StreamOpened          prometheus.Counter
	StreamFinished        prometheus.Counter
	StreamFellBack        prometheus.Counter
	OutboundDelivered     prometheus.Counter
	OutboundDropped       *prometheus.CounterVec
	OutboundSkipped       *prometheus.CounterVec
	AttachmentDelivered   prometheus.Counter
	AttachmentDropped     *prometheus.CounterVec
	AttachmentSheds       prometheus.Counter
	OutboundUnconfirmed   *prometheus.CounterVec
	AttachmentUnconfirmed *prometheus.CounterVec
	// RelayShed counts admission decisions on the cross-replica dispatcher,
	// labelled by frame kind. Its own metric because the relay carries inbox
	// notifications as well as replies and the reply counters are per reply.
	RelayShed *prometheus.CounterVec
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
		StreamOpened: counter("stream_opened_total",
			"Bubbles painted on ingest. The denominator for the two below: opened minus finished minus fell_back is the number nobody ever ended, which is the only failure the finished/fell_back ratio cannot show."),
		StreamFinished: counter("stream_finished_total",
			"Answers that landed in the bubble the question opened."),
		StreamFellBack: counter("stream_fell_back_total",
			"Answers sent as a new message because the bubble refused the closing frame. The answer is not lost; the experience is the one the bubble was built to replace."),
		OutboundDelivered: counter("outbound_delivered_total",
			"Agent replies this adapter put in front of a WeCom user. The denominator the drop breakdown is read against."),
		OutboundDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica", Subsystem: "wecom", Name: "outbound_dropped_total",
			Help: "Agent replies the adapter owed a WeCom user and did not deliver, by reason. Every reason here means somebody in WeCom is waiting on an answer that is not coming; most of them are settled before the frame reaches WeCom, so read this as a count of unpaid obligations rather than of network failures. The completions this adapter did not owe, and the ones nothing here can attribute to a WeCom chat, are counted apart, in outbound_skipped_total.",
		}, []string{"reason"}),
		OutboundSkipped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica", Subsystem: "wecom", Name: "outbound_skipped_total",
			Help: "Completions this adapter did not send to WeCom, by reason: it did not owe them, or nothing here could attribute them to a WeCom chat. Kept apart from dropped, which counts the replies a WeCom user was owed and did not get; what separates the two is the obligation, not whether a frame was written. Four of these were never owed to WeCom, and counting them as drops would make ordinary usage read as an outage: origin_not_channel is a question typed in Multica on a WeCom-bound session, not_wecom_turn is another platform's turn on the bus this subscriber shares, installation_inactive means there is no longer an installation to deliver through, nothing_to_say is an empty completion carrying no file. The fifth is the one label here worth an alert: no_delivery_row is a turn the channel ingested with no row saying which chat, so a reply may well be owed and nothing left can name the room — in practice a task enqueued before an upgrade and finishing after it. The sixth is neither: route_unattributable is a turn with no row and no batch owner, so nothing here can say whether it was ever a channel's, let alone whether a reply was owed — in practice a row old enough to predate batch owners still finishing, and a rising count says only that such rows remain.",
		}, []string{"reason"}),
		AttachmentDelivered: counter("outbound_attachment_delivered_total",
			"Files put in front of a WeCom user. Counts FILES; the outbound_delivered/dropped/skipped trio counts REPLIES."),
		AttachmentDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica", Subsystem: "wecom", Name: "outbound_attachment_dropped_total",
			Help: "Files an agent produced that did not reach the WeCom user, by reason. Separate from the reply counters because a reply whose words arrived and whose file did not is a delivered reply with a failed attachment, and one number cannot say both.",
		}, []string{"reason"}),
		AttachmentSheds: counter("outbound_attachment_delivery_shed_total",
			"Attachment delivery attempts refused admission before the lookup ran. Counts SCHEDULING decisions, not files: at that point nothing knows whether the turn carries zero files or five, so a per-file count from this gate would fabricate cardinality."),
		OutboundUnconfirmed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica", Subsystem: "wecom", Name: "outbound_unconfirmed_total",
			Help: "Agent replies whose delivery outcome is UNKNOWN: the frame reached the wire (write_attempted), the verdict never came back (ack_timeout), or the wait was cut short (interrupted). The message may already be in front of the user, which is why these are not drops — an operator paging on the drop rate must not be paged for deliveries that probably happened, and nothing here should prompt a resend.",
		}, []string{"reason"}),
		AttachmentUnconfirmed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica", Subsystem: "wecom", Name: "outbound_attachment_unconfirmed_total",
			Help: "Files whose delivery outcome is UNKNOWN, by reason. Counts FILES, same unit as the attachment delivered/dropped pair; see outbound_unconfirmed_total for why unknown is not a drop.",
		}, []string{"reason"}),
		RelayShed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica", Subsystem: "wecom", Name: "outbound_relay_shed_total",
			Help: "Frames the cross-replica dispatcher refused because a shard queue was full, by kind (reply|inbox). An ADMISSION decision, not a per-reply outcome: the relay carries inbox notifications too, and counting those as dropped replies would make the delivered/dropped ratio track which replica held a socket instead of what happened to anyone's message. Recorded on whichever replica refused the frame, and it moves no reply counter: every replica reads every frame, so no replica can tell locally whether its own shed cost the user anything. Whether a shed reply was in fact lost is settled once by the replica that routed it, as outbound_dropped_total{reason=\"no_live_connection\"} when no replica claimed the delivery.",
		}, []string{"kind"}),
	}
}

func (m *WecomMetrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.ConnectFailures, m.AuthFailures,
		m.CallbacksQueued, m.CallbackQueueBlocked,
		m.StreamOpened, m.StreamFinished, m.StreamFellBack,
		m.OutboundDelivered, m.OutboundDropped, m.OutboundSkipped,
		m.AttachmentDelivered, m.AttachmentDropped, m.AttachmentSheds,
		m.OutboundUnconfirmed, m.AttachmentUnconfirmed,
		m.RelayShed,
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
func (m *WecomMetrics) RecordOutboundUnconfirmed(reason string) {
	m.OutboundUnconfirmed.WithLabelValues(reason).Inc()
}
func (m *WecomMetrics) RecordAttachmentUnconfirmed(reason string) {
	m.AttachmentUnconfirmed.WithLabelValues(reason).Inc()
}

func (m *WecomMetrics) RecordRelayShed(kind string) {
	m.RelayShed.WithLabelValues(kind).Inc()
}

func (m *WecomMetrics) RecordStreamOpened()   { m.StreamOpened.Inc() }
func (m *WecomMetrics) RecordStreamFinished() { m.StreamFinished.Inc() }
func (m *WecomMetrics) RecordStreamFellBack() { m.StreamFellBack.Inc() }
