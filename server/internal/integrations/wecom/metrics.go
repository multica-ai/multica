package wecom

// metrics.go — the health signals this adapter emits.
//
// Every failure in the connection path degrades quietly by design. A dial that
// fails and a handshake the server refuses hand the connection back to the
// Supervisor for backoff and retry; a full ingest queue parks the read loop
// instead, and the socket stops being drained until the worker catches up.
// Quiet is the right behaviour for the person in front of the chat and the
// wrong behaviour for the operator behind it — nothing on a dashboard changes
// when a bot has been unable to connect for an hour.
//
// The counters here are chosen for what somebody would page on rather than for
// completeness: the connection is not coming up, and if so whether that needs a
// person or just time; and the read loop is being made to wait by an ingest
// worker that cannot keep up.
//
// No installation id anywhere. It is an unbounded identifier and the metrics
// package rejects that class of label outright (forbiddenMetricLabels in
// internal/metrics/labels.go). What attribution exists is in the structured
// logs, and it is not uniform: the two connection counters have it, because
// the failure they count is also returned to the Supervisor, which logs it
// with installation_id. The two inbound counters have nothing beside them —
// a queue that blocks writes no log line at all, so the counter tells an
// operator that some bot is behind and not which one.

// Metrics is the sink this adapter reports to. Every method must tolerate being
// called concurrently, and none of them may block: they run on the read loop.
type Metrics interface {
	// RecordConnectFailure — a dial, a handshake write or a handshake read
	// that did not complete, or a handshake the server answered with a code
	// that classifySubscribeAck could not verify (a throttle, a platform-side
	// failure). Excludes an outright credential rejection, which has its own
	// counter: everything counted here recovers on its own, that one needs an
	// admin.
	RecordConnectFailure()
	// RecordAuthFailure — aibot_subscribe was refused on the credentials, as
	// classifySubscribeAck judges it (ErrCredentialsRejected: 40001 / 40013).
	// Deliberately not every non-zero errcode — the codes that only mean "could
	// not verify" go to RecordConnectFailure, because paging an operator to
	// rotate a good secret costs a second outage. The bot will not connect
	// until somebody fixes the credentials, so a sustained rate here is an
	// alert and not a blip.
	RecordAuthFailure()

	// RecordCallbackQueued — one inbound callback handed to the worker. The
	// baseline every other inbound number is read against.
	RecordCallbackQueued()
	// RecordCallbackQueueBlocked — the worker queue was full and the read
	// loop had to wait. Backpressure, deliberately: it is how a slow ingest
	// stops rather than a message being dropped. A rising rate says the
	// engine is not keeping up with one bot's traffic, and past a point
	// WeCom stops seeing the socket drained and replaces the connection.
	RecordCallbackQueueBlocked()

	// RecordOutboundDelivered — one reply reached the user. It is the
	// denominator: without it a flat drop counter cannot be told apart from a
	// quiet day, and "the bot went silent" is exactly the report that cannot
	// afford that ambiguity.
	RecordOutboundDelivered()
	// RecordOutboundDropped — one reply the adapter owed a user and did not
	// deliver, labelled with why (outbound_outcome.go's closed reason set).
	// Every reason here leaves somebody in WeCom waiting on an answer that is
	// not coming, so this IS an error total and the label says which failure it
	// was. The ordinary outcomes — a question typed in the web UI on a
	// WeCom-bound session, an installation revoked between trigger and reply —
	// are not counted here; RecordOutboundSkipped has them.
	RecordOutboundDropped(reason string)
	// RecordOutboundSkipped — a completion this adapter was never going to
	// deliver, because it was not owed to a WeCom user in the first place. Kept
	// apart from dropped on purpose: counting a web-UI question's answer as a
	// failed WeCom delivery makes ordinary web usage look like an outage.
	RecordOutboundSkipped(reason string)

	// RecordAttachmentDelivered / RecordAttachmentDropped count FILES, one per
	// file, while the three above count REPLIES, one per reply. The two units
	// are separate because a reply whose words arrived and whose file did not
	// is a delivered reply with a failed attachment, and collapsing that into
	// one number can only lie in one direction or the other.
	RecordAttachmentDelivered()
	RecordAttachmentDropped(reason string)

	// RecordAttachmentDeliveryShed counts a SCHEDULING decision: one delivery
	// attempt refused admission before the lookup ran. Its own unit because at
	// that point nothing knows whether the turn carries zero files or five —
	// counting it as file drops fabricates cardinality in both directions.
	RecordAttachmentDeliveryShed()

	// RecordOutboundUnconfirmed / RecordAttachmentUnconfirmed — the outcome is
	// UNKNOWN: the frame reached the wire (or the wait for its verdict was cut
	// short) and the message may already be in front of the user. Its own pair
	// because folding it into dropped would inflate a definite failure rate
	// with sends that probably succeeded, and an operator paging on the drop
	// rate would be paged for deliveries that happened.
	RecordOutboundUnconfirmed(reason string)
	RecordAttachmentUnconfirmed(reason string)

	// RecordRelayShed counts an ADMISSION decision on the cross-replica
	// dispatcher: one routed frame refused because its queue was full. Its own
	// unit, labelled by what kind of frame it was, because the reply counters
	// above are per REPLY and the relay carries inbox notifications too — an
	// inbox push counted as a dropped reply would make the delivered/dropped
	// ratio track which replica happened to hold a socket rather than any
	// outcome. Always recorded, on whichever replica refused the frame.
	//
	// It moves no reply counter, not even on the replica holding the socket.
	// Every replica reads every frame, and during a lease handoff two of them
	// hold a sender at once, so no replica can tell locally whether its own
	// shed cost the user anything — each one answering for itself is what
	// reported a single reply as delivered and dropped together. Whether a shed
	// reply was really lost is settled once by the replica that routed it, in
	// RelayOutbound.watchOutcomes.
	RecordRelayShed(kind string)
}

// nopMetrics is what the constructor falls back to. A nil sink must never be a
// nil-pointer dereference on the read loop.
type nopMetrics struct{}

func (nopMetrics) RecordConnectFailure()              {}
func (nopMetrics) RecordAuthFailure()                 {}
func (nopMetrics) RecordCallbackQueued()              {}
func (nopMetrics) RecordCallbackQueueBlocked()        {}
func (nopMetrics) RecordOutboundDelivered()           {}
func (nopMetrics) RecordOutboundDropped(string)       {}
func (nopMetrics) RecordOutboundSkipped(string)       {}
func (nopMetrics) RecordAttachmentDelivered()         {}
func (nopMetrics) RecordAttachmentDropped(string)     {}
func (nopMetrics) RecordAttachmentDeliveryShed()      {}
func (nopMetrics) RecordOutboundUnconfirmed(string)   {}
func (nopMetrics) RecordAttachmentUnconfirmed(string) {}
func (nopMetrics) RecordRelayShed(string)             {}

// orNopMetrics turns an unset sink into one that is safe to call.
func orNopMetrics(m Metrics) Metrics {
	if m == nil {
		return nopMetrics{}
	}
	return m
}
