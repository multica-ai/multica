package wecom

// outbound_outcome.go — what happened to a reply this adapter was asked to
// deliver, as something an operator can count.
//
// Every branch on the outbound path that ends a turn without putting words in
// front of the user used to be a bare `return nil` or a lone WARN. That is the
// shape of GH #7215 and #6890: the answer is in the Multica transcript, the
// WeCom chat stays quiet, and the server-side evidence is either one line with
// no reason attached or nothing at all. With several distinct causes producing
// one indistinguishable symptom, neither we nor a deployment's operator can say
// which one fired, and a fix is a guess.
//
// So each of those branches names itself. The counter is the durable half — it
// is always incremented — and the log level is the judgement half: a reason a
// person should act on logs at WARN, one that is ordinary in a healthy
// deployment logs at DEBUG and is only ever read as a rate.
//
// The reason set is closed on purpose. It is a metric label, and an open one is
// the unbounded-cardinality problem forbiddenMetricLabels exists to prevent.

import (
	"context"
	"errors"

	"github.com/multica-ai/multica/server/internal/events"
)

// dropReason names why a reply did not reach the user. Closed set: see the
// file header.
type dropReason string

const (
	// dropNoConnection — no live WebSocket for this installation in THIS
	// process. Either the Supervisor is mid-reconnect, or, on a multi-replica
	// deployment, the lease is held by a different replica than the one that
	// published the completion, which is the constraint SELF_HOSTING.md
	// documents. The two are indistinguishable from here; a deployment's
	// replica count is what tells them apart.
	dropNoConnection dropReason = "no_live_connection"

	// dropOriginNotChannel — the turn was asked in the Multica web UI on a
	// session that originated in WeCom, so its answer belongs in Multica only.
	// Expected in a healthy deployment: counted, logged at DEBUG.
	dropOriginNotChannel dropReason = "origin_not_channel"

	// dropInstallationInactive — the installation was revoked between the
	// trigger and the reply.
	dropInstallationInactive dropReason = "installation_inactive"

	// dropTaskMissing — the task the completion belongs to could not be
	// resolved: no id on the event, or the row was reaped while its ending was
	// in flight.
	dropTaskMissing dropReason = "task_missing"

	// dropPlatformRefused — WeCom answered the send with a non-zero errcode.
	// A stated refusal: the frame was over budget, the bot is no longer in the
	// chat, the tenant is rate limited.
	dropPlatformRefused dropReason = "platform_refused"

	// dropAckTimeout — the frame went out and no verdict came back inside
	// ackTimeout. NOT proof of non-delivery: the message may well have
	// arrived. Counted apart from a refusal because the two call for opposite
	// responses.
	dropAckTimeout dropReason = "ack_timeout"

	// dropTransport — the write itself failed, or the lookups ahead of it did.
	dropTransport dropReason = "transport_error"
)

// actionable reports whether a reason is one a person should look at. The
// others are ordinary in a healthy deployment and would drown the log.
func (r dropReason) actionable() bool {
	switch r {
	case dropOriginNotChannel, dropInstallationInactive:
		return false
	default:
		return true
	}
}

// errNoLiveConnection — no live WebSocket for this installation in this
// process. A sentinel rather than a fresh errors.New at the call site, so
// classifyDrop can name it instead of pattern-matching prose.
var errNoLiveConnection = errors.New("wecom: connection not ready on this replica")

// classifyDrop turns a send error into the reason an operator reads. Order
// matters: a stated refusal is more specific than a transport failure, and an
// ack that never came is neither.
func classifyDrop(err error) dropReason {
	var apiErr *wecomAPIError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errNoLiveConnection):
		return dropNoConnection
	case errors.As(err, &apiErr):
		return dropPlatformRefused
	case errors.Is(err, errAckTimeout):
		return dropAckTimeout
	default:
		return dropTransport
	}
}

// dropped records one undelivered reply: always a counter, and a log line whose
// level says whether somebody should act.
//
// Deliberately not an error return. Several of these branches are reached on
// events that were never this adapter's to answer, and turning them into errors
// would change what processEvent's callers — and a dozen existing tests — mean
// by "nothing to do here".
func (o *Outbound) dropped(ctx context.Context, e events.Event, reason dropReason, err error) {
	o.mx().RecordOutboundDropped(string(reason))
	attrs := []any{
		"reason", string(reason),
		"chat_session_id", e.ChatSessionID,
		"event", e.Type,
	}
	if err != nil {
		attrs = append(attrs, "error", err)
	}
	if reason.actionable() {
		o.logger.WarnContext(ctx, "wecom outbound: reply not delivered", attrs...)
		return
	}
	o.logger.DebugContext(ctx, "wecom outbound: reply not delivered", attrs...)
}

// delivered records one reply that reached the user. Without it the drop
// counters have no denominator, and "no drops today" cannot be told apart from
// "no traffic today" — which is the same silence #7215 was reported as.
func (o *Outbound) delivered() { o.mx().RecordOutboundDelivered() }

// mx returns the metrics sink, or a no-op one. Mirrors wecomChannel.mx.
func (o *Outbound) mx() Metrics { return orNopMetrics(o.metrics) }

// WithOutboundMetrics attaches the adapter's health sink to the subscriber.
func WithOutboundMetrics(m Metrics) OutboundOption {
	return func(o *Outbound) { o.metrics = m }
}
