package wecom

// metrics.go — the health signals this adapter emits.
//
// Every failure in the connection path degrades quietly by design: a dial that
// fails, a handshake the server refuses and a full ingest queue all end the
// same way, with the connection handed back to the Supervisor for backoff and
// retry. That is the right behaviour for the person in front of the chat and
// the wrong behaviour for the operator behind it — nothing on a dashboard
// changes when a bot has been unable to connect for an hour.
//
// The counters here are chosen for what somebody would page on rather than for
// completeness: the connection is not coming up, and if so whether that needs a
// person or just time; and the read loop is being made to wait by an ingest
// worker that cannot keep up.
//
// No installation id anywhere. It is an unbounded identifier and the metrics
// package rejects that class of label outright; per-installation attribution is
// in the structured logs, which carry it at every one of these call sites.

// Metrics is the sink this adapter reports to. Every method must tolerate being
// called concurrently, and none of them may block: they run on the read loop.
type Metrics interface {
	// RecordConnectFailure — a dial, a handshake write or a handshake read
	// that did not complete. Excludes an outright credential rejection,
	// which has its own counter: one is infrastructure and usually recovers
	// on its own, the other needs an admin.
	RecordConnectFailure()
	// RecordAuthFailure — aibot_subscribe answered with a non-zero errcode.
	// The bot will not connect until somebody fixes the credentials, so a
	// sustained rate here is an alert and not a blip.
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
}

// nopMetrics is what the constructor falls back to. A nil sink must never be a
// nil-pointer dereference on the read loop.
type nopMetrics struct{}

func (nopMetrics) RecordConnectFailure()       {}
func (nopMetrics) RecordAuthFailure()          {}
func (nopMetrics) RecordCallbackQueued()       {}
func (nopMetrics) RecordCallbackQueueBlocked() {}

// orNopMetrics turns an unset sink into one that is safe to call.
func orNopMetrics(m Metrics) Metrics {
	if m == nil {
		return nopMetrics{}
	}
	return m
}
