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

// skipReason names a completion this adapter did not send because it was not
// WeCom's to send: not owed, not applicable, or not attributable to a WeCom
// chat at all. Kept in its own set, and behind its own counter, because "this
// was not ours to deliver" and "we owed this and failed" answer different
// questions, and counting a web-UI question's answer as a failed WeCom delivery
// makes ordinary usage read as an outage.
//
// The boundary is the obligation, not the wire. Most of the reasons above are
// settled before a frame is written too — a task that cannot be resolved, no
// socket to write on, a delivery refused admission, a budget spent before its
// turn — and what makes those drops is that a WeCom user was owed the answer.
//
// What stopped holding when skipNoDeliveryRow arrived is the other half of what
// this set used to claim: that none of them is an incident. That one is a skip
// for the third reason rather than the first — a turn the channel ingested with
// nothing left that can say which chat — so a reply may well be owed and nobody
// can place it. actionable() says which side of that line each reason falls
// on.
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

	// skipNotWecomTurn — the turn has a delivery row and it names another
	// platform. This subscriber sees every chat:done in the deployment, so on
	// a workspace running Slack or Lark alongside WeCom this is ordinary and
	// frequent. Named rather than left silent because the branch next to it —
	// no delivery row at all — is not ordinary, and one unnamed exit shared by
	// the two makes the abnormal one invisible.
	skipNotWecomTurn skipReason = "not_wecom_turn"

	// skipNoDeliveryRow — the turn's input came in over a channel, and no
	// channel_task_delivery row says which one. Nothing here can address a
	// reply, so nothing is sent.
	//
	// The only reason this happens in a healthy deployment is an UPGRADE: a
	// task enqueued by a build that did not write delivery rows yet, finishing
	// under one that reads them (service/task.go writes the row inside the
	// enqueue transaction for every channel turn, so a steady-state channel
	// task always has one). The user is left waiting on an answer that exists
	// in the Multica transcript, which is the shape of #7215 all over again —
	// so this is the one skip reason a person should act on: drain the running
	// and queued channel tasks before swapping the image, or backfill the rows
	// for the ones still in flight.
	//
	// Not a DROP: this adapter never wrote to a socket and was never going to —
	// there is no address to write to — and dropped is the number an operator
	// reads as "a reply this adapter owed did not arrive at the platform".
	//
	// It is reached BEHIND the origin gate, which is what makes it mean
	// anything. Ahead of the gate the same branch also caught every question
	// ever typed in the Multica web UI, and an exit shared with those could
	// only be silent. A delivery row naming another platform leaves by
	// skipNotWecomTurn above, so what is left here is a channel turn nobody can
	// address.
	skipNoDeliveryRow skipReason = "no_delivery_row"

	// skipRouteUnattributable — no delivery row, and no way to say whose turn
	// it was. The gate delivered it on the open side of an unanswerable
	// verdict: its input batch has no owner (chat_input_task_id NULL), which
	// migration 158 left on legacy web rows and legacy channel rows alike, so
	// nothing establishes this turn was ever a channel's.
	//
	// Same branch as skipNoDeliveryRow, one level quieter, and the split is the
	// point. Delivering under an unanswerable verdict is right — the auto-retry
	// of a legacy channel turn depends on it, and a reply in the wrong place
	// costs less than a room waiting forever. WARNING under one is not: a
	// pre-158 web turn that auto-retried has exactly this shape, and it would
	// arrive as the loudest line this adapter has.
	//
	// So one verdict, two directions: deliver on the open side, warn on the
	// closed one. Counted rather than folded into the reason above, because an
	// operator who sees this number rise has learned something different — that
	// rows this old are still finishing — and nothing they can act on.
	skipRouteUnattributable skipReason = "route_unattributable"
)

// actionable reports whether a reason is one a person should look at. Every
// drop is, now that the two ordinary outcomes have moved to skipReason.
func (r dropReason) actionable() bool { return true }

// actionable reports whether a skip is one a person should look at. Almost none
// are, which is a property of this set rather than its definition — what
// separates it from dropReason is the obligation, see the type comment — so the
// one that is gets a WARN and the rest stay at DEBUG, where they can be read as
// a rate without drowning the log.
func (r skipReason) actionable() bool { return r == skipNoDeliveryRow }

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
	case errors.Is(err, errNotAttempted):
		// AHEAD of the context branch below, which this error also matches:
		// every not-attempted failure wraps the ctx.Err() that ended it. What
		// it marks is that the wait ended before a frame existed — the chat's
		// turn never came, or the context was already over when request was
		// entered — so these are the context failures on this path that are
		// certain rather than unknown. "interrupted" would tell an operator
		// not to resend a message nobody ever sent.
		return ""
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
// unconfirmedSealReason names why a closing frame's outcome is unknown, for
// the one label an operator reads off outbound_unconfirmed. It mirrors
// unconfirmedReason and adds the seal's own case: a frame written and retried
// with no verdict ever coming back.
func unconfirmedSealReason(err error) string {
	if r := unconfirmedReason(err); r != "" {
		return r
	}
	return "seal_unacked"
}

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

// skipped records one completion this adapter did not send. DEBUG for all but
// one of them: none of those is an incident, and on a workspace where people
// use the web UI against WeCom-bound sessions this is the busiest path here.
// skipNoDeliveryRow is the exception and logs at WARN — see its comment for why
// an upgrade is the only thing that produces it.
func (o *Outbound) skipped(ctx context.Context, e events.Event, reason skipReason) {
	o.skippedFor(ctx, e.ChatSessionID, reason)
}

func (o *Outbound) skippedFor(ctx context.Context, sessionID string, reason skipReason) {
	o.mx().RecordOutboundSkipped(string(reason))
	attrs := []any{"reason", string(reason), "chat_session_id", sessionID}
	switch {
	case reason.actionable():
		// Its own line, not the default one at a louder level: "not owed to
		// WeCom" is what the ordinary reasons mean, and the actionable one is
		// precisely the case where the reply may well have been owed and there
		// is no longer anything that can say to whom.
		o.logger.WarnContext(ctx, "wecom outbound: a channel turn with no route, so nothing was sent", attrs...)
	case reason == skipRouteUnattributable:
		// Its own line too, at DEBUG. "Not owed" is a claim this reason cannot
		// make: with no route and no batch owner, nothing here can say whether
		// the turn was ever a channel's, let alone whether a reply was owed.
		o.logger.DebugContext(ctx, "wecom outbound: no route and no batch owner to attribute the turn to, so nothing was sent", attrs...)
	default:
		o.logger.DebugContext(ctx, "wecom outbound: reply not owed to WeCom", attrs...)
	}
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
// recordSend files the counter for one completed text send. It is the single
// definition of that mapping, and it has to stay single: the two paths that
// put an agent's words in front of a WeCom user — processEvent on the replica
// that produced the completion, and deliverRelayed on the one holding the
// socket — used to classify a PARTIAL send differently. The same user-visible
// event, piece one in the chat and piece two refused, counted as
// outbound_delivered on a single-replica deployment and outbound_dropped once
// the reply went through the relay, so whether a partial delivery paged
// anybody depended on which replica happened to hold the lease.
//
// A partial send counts as delivered, and WARNs with what did not land. The
// alternative reading — record a drop — tells an operator to resend an answer
// the user is already reading, and a resend would print the first piece a
// second time. Neither counter is a good fit for "most of it arrived"; this is
// the one that does not invite a harmful action.
//
// Callers must not also return the error to a layer that classifies it again:
// one send moves one counter.
// errOutcomeRecorded says the caller must not classify this error: the branch
// that produced it has already filed the outcome through recordSend, and
// handleEvent counts everything processEvent returns.
var errOutcomeRecorded = errors.New("wecom: outcome already recorded")

func (o *Outbound) recordSend(ctx context.Context, sessionID, eventType string, err error) {
	switch {
	case err == nil:
		o.delivered()
	case errors.Is(err, errPartiallySent):
		o.logger.WarnContext(ctx, "wecom outbound: only part of a long answer reached the chat",
			"error", err, "chat_session_id", sessionID, "event", eventType)
		o.delivered()
	default:
		if reason := unconfirmedReason(err); reason != "" {
			o.unconfirmedFor(ctx, sessionID, eventType, reason, err)
			return
		}
		o.droppedFor(ctx, sessionID, eventType, classifyDrop(err), err)
	}
}

func (o *Outbound) delivered() { o.mx().RecordOutboundDelivered() }

// mx returns the metrics sink, or a no-op one. Mirrors wecomChannel.mx.
func (o *Outbound) mx() Metrics { return orNopMetrics(o.metrics) }

// WithOutboundMetrics attaches the adapter's health sink to the subscriber.
func WithOutboundMetrics(m Metrics) OutboundOption {
	return func(o *Outbound) { o.metrics = m }
}
