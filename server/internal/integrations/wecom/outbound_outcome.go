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
	// dropNoConnection — no live WebSocket carried this reply. Two situations
	// reach it. Without a relay: none in THIS process, which on a
	// multi-replica deployment cannot be told apart from the lease simply
	// being held elsewhere. With one: the reply WAS routed to every replica
	// and none of them held a connection either, which is the residual window
	// SELF_HOSTING.md describes — recorded once, by the replica that routed
	// it, from the claim nobody took (RelayOutbound.watchOutcomes).
	dropNoConnection dropReason = "no_live_connection"

	// dropTaskMissing — the task the completion belongs to could not be
	// resolved: no id on the event, or the row was reaped while its ending was
	// in flight.
	dropTaskMissing dropReason = "task_missing"

	// dropPlatformRefused — WeCom answered the send with a non-zero errcode.
	// A stated refusal: the frame was over budget, the bot is no longer in the
	// chat, the tenant is rate limited.
	dropPlatformRefused dropReason = "platform_refused"

	// dropTransport — nothing reached the platform, for a local reason. The
	// write itself failed, or a lookup ahead of it did, or this delivery's own
	// budget ran out before it got a turn on the wire. What the three have in
	// common is the fact an operator needs: the failure is on our side of the
	// socket, so nothing was shown to anybody.
	dropTransport dropReason = "transport_error"

	// dropAttachmentNotAdmitted — the delivery was shed because too many were
	// already running or pending.
	//
	// It appears on BOTH units, and means a different thing on each. On the
	// file counter it is one file that will not be sent. On the reply counter
	// it appears only when the files WERE the reply — an empty completion that
	// reached delivery because something was bound to it — and there it means
	// the user got nothing at all. A reply whose words already landed is
	// settled before this gate and never reaches it.
	dropAttachmentNotAdmitted dropReason = "attachment_not_admitted"
)

// skipReason names a completion this adapter was never going to deliver. Kept
// in its own set, and behind its own counter, because "we chose not to send
// this" and "we owed this and failed" answer different questions and only one
// of them is an incident.
type skipReason string

const (
	// skipOriginNotChannel — the turn was asked in the Multica web UI on a
	// session that originated in WeCom, so its answer belongs in Multica only.
	// Ordinary in a healthy deployment, and the single largest source of this
	// counter on a busy workspace.
	skipOriginNotChannel skipReason = "origin_not_channel"

	// skipInstallationInactive — the installation was revoked between the
	// trigger and the reply. Not a delivery failure: there is no longer an
	// installation to deliver through, and the bot is gone from the user's
	// side too.
	skipInstallationInactive skipReason = "installation_inactive"

	// skipNothingToSay — an empty completion carrying no file. There was never
	// a message here.
	skipNothingToSay skipReason = "nothing_to_say"
)

// actionable reports whether a reason is one a person should look at. The
// others are ordinary in a healthy deployment and would drown the log.
// actionable reports whether a reason is one a person should look at. Every
// drop is, now that the two ordinary outcomes have moved to skipReason.
func (r dropReason) actionable() bool { return true }

// errNoLiveConnection — no live WebSocket for this installation in this
// process. A sentinel rather than a fresh errors.New at the call site, so
// classifyDrop can name it instead of pattern-matching prose.
var errNoLiveConnection = errors.New("wecom: connection not ready on this replica")

// unconfirmedReason names a failure whose OUTCOME IS UNKNOWN — the message may
// already be in front of the user — and returns "" for one that is definite.
// The set mirrors sendOutcome's deliveryUnknown exactly: a verdict that never
// came, a failure raised by the write itself (the peer may hold bytes the
// local side reported an error for), and a context cut short while either was
// pending. Callers MUST consult this before classifyDrop: an unknown filed as
// a drop tells an operator to resend a message the user may already have.
func unconfirmedReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errAckTimeout):
		return "ack_timeout"
	case errors.Is(err, errWriteAttempted):
		return "write_attempted"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "interrupted"
	}
	return ""
}

// classifyDrop turns a DEFINITE send failure into the reason an operator
// reads. Definite is the caller's obligation: consult unconfirmedReason first.
func classifyDrop(err error) dropReason {
	var apiErr *wecomAPIError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errNoLiveConnection):
		return dropNoConnection
	case errors.As(err, &apiErr):
		return dropPlatformRefused
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
	o.droppedFor(ctx, e.ChatSessionID, e.Type, reason, err)
}

// droppedFor is dropped for a caller that has a session id rather than the
// event — the attachment path, which runs long after the event is gone.
func (o *Outbound) droppedFor(ctx context.Context, sessionID, eventType string, reason dropReason, err error) {
	o.mx().RecordOutboundDropped(string(reason))
	attrs := []any{
		"reason", string(reason),
		"chat_session_id", sessionID,
		"event", eventType,
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

// unconfirmed records one reply whose outcome is unknown. WARN, because a
// person deciding whether to resend needs to know this is NOT a failure.
func (o *Outbound) unconfirmed(ctx context.Context, e events.Event, reason string, err error) {
	o.unconfirmedFor(ctx, e.ChatSessionID, e.Type, reason, err)
}

func (o *Outbound) unconfirmedFor(ctx context.Context, sessionID, eventType, reason string, err error) {
	o.mx().RecordOutboundUnconfirmed(reason)
	attrs := []any{"reason", reason, "chat_session_id", sessionID, "event", eventType}
	if err != nil {
		attrs = append(attrs, "error", err)
	}
	o.logger.WarnContext(ctx, "wecom outbound: reply delivery unconfirmed", attrs...)
}

// attachmentUnconfirmed records one FILE whose outcome is unknown.
func (o *Outbound) attachmentUnconfirmed(ctx context.Context, reason string, err error) {
	o.mx().RecordAttachmentUnconfirmed(reason)
	attrs := []any{"reason", reason}
	if err != nil {
		attrs = append(attrs, "error", err)
	}
	o.logger.WarnContext(ctx, "wecom outbound: attachment delivery unconfirmed", attrs...)
}

// skipped records one completion this adapter was never going to deliver.
// Always DEBUG: none of these is an incident, and on a workspace where people
// use the web UI against WeCom-bound sessions this is the busiest path here.
func (o *Outbound) skipped(ctx context.Context, e events.Event, reason skipReason) {
	o.skippedFor(ctx, e.ChatSessionID, reason)
}

func (o *Outbound) skippedFor(ctx context.Context, sessionID string, reason skipReason) {
	o.mx().RecordOutboundSkipped(string(reason))
	o.logger.DebugContext(ctx, "wecom outbound: reply not owed to WeCom",
		"reason", string(reason), "chat_session_id", sessionID)
}

// attachmentDelivered / attachmentDropped record ONE FILE. See the note on the
// Metrics interface for why files and replies are counted separately.
func (o *Outbound) attachmentDelivered() { o.mx().RecordAttachmentDelivered() }

func (o *Outbound) attachmentDropped(ctx context.Context, reason dropReason, err error) {
	o.mx().RecordAttachmentDropped(string(reason))
	attrs := []any{"reason", string(reason)}
	if err != nil {
		attrs = append(attrs, "error", err)
	}
	o.logger.WarnContext(ctx, "wecom outbound: attachment not delivered", attrs...)
}

// attachmentShed records one delivery attempt refused admission before the
// lookup. Not a file count: nothing knows yet whether a file exists.
func (o *Outbound) attachmentShed() { o.mx().RecordAttachmentDeliveryShed() }

// worseUnconfirmedReason is worseDropReason's twin, for the other unit. A reply
// whose files came back unknown for more than one reason needs its own outcome
// chosen by a rule rather than by whichever row the loop happened to end on —
// the same objection that made the definite side a documented precedence.
//
// Ordered by how much each one establishes about the frame. ack_timeout is the
// most specific: the frame reached the wire and only its verdict is missing.
// write_attempted is next: the local side reported a failure, and the peer may
// still hold the bytes. interrupted says least — the wait ended and where it
// ended is not knowable from here.
func worseUnconfirmedReason(a, b string) string {
	rank := func(r string) int {
		switch r {
		case "ack_timeout":
			return 3
		case "write_attempted":
			return 2
		case "interrupted":
			return 1
		default:
			return 0
		}
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

// worseDropReason is the aggregation rule for a reply whose files failed for
// more than one reason. Precedence: a stated refusal beats a local transport
// failure beats a verdict that never came — ordered by how specific a fact
// each is about why the content did not arrive. Stable and documented so the
// multi-file reply reason is a rule, not an accident of loop order.
func worseDropReason(a, b dropReason) dropReason {
	rank := func(r dropReason) int {
		switch r {
		case dropPlatformRefused:
			return 3
		case dropTransport:
			return 2
		default:
			return 0
		}
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
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
