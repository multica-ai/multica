package telegram

// delivery.go — reply-delivery ownership.
//
// A task's reply reaches Telegram through three paths: the streamed
// placeholder, the final answer, and the failure notice. In a multi-replica
// deployment they also run in three different processes — the daemon's
// transcript report and its completion callback are independent HTTP requests
// that land wherever the load balancer sends them, and the event bus is
// in-process. A process that cannot see the placeholder posts its own copy of
// the same answer, which is the duplicate users report (GH #8049, #7750).
//
// channel_reply_delivery is the shared owner, and this file is the only way to
// touch it. Three rules hold everywhere:
//
//   - One user turn, one owner. A path takes the turn's lease before it calls
//     Telegram and proves it still holds the lease when it records what
//     happened. "The UPDATE is atomic" is not the same as "only one process
//     delivers".
//   - A placeholder is not progress. Having an editable message says nothing
//     about how much of the final answer has been delivered; conflating the
//     two silently truncates replies.
//   - A send whose result was lost stays lost. Telegram's sendMessage takes no
//     caller-supplied idempotency key, so a retry cannot be deduplicated by
//     the provider. An unknown outcome ends delivery with the evidence kept.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	deliveryPhaseStreaming = "streaming"
	deliveryPhaseTerminal  = "terminal"
	deliveryPhaseSettled   = "settled"

	deliverySendNone     = "none"
	deliverySendInFlight = "in_flight"
	deliverySendKnown    = "known"
	deliverySendUnknown  = "unknown"
)

const (
	// deliveryLeaseTTL has to outlive one Telegram round trip, because the
	// lease is held across the call: a shorter lease would let a second
	// process take a turn over while the first is still talking to Telegram.
	// It is also the longest a dead process can block a turn, so it is not
	// generous.
	deliveryLeaseTTL = 30 * time.Second
	// deliveryCallTimeout bounds one provider call made while holding a turn.
	//
	// The shared Bot API client allows 65s, which getUpdates needs for long
	// polling but a delivery call must never take: a request that outlives the
	// lease can land after another process has taken the turn over and
	// finished it. The budget has to close, so one call plus recording its
	// outcome stays inside the lease — see TestDeliveryCallBudgetFitsTheLease.
	deliveryCallTimeout = 20 * time.Second
	// deliveryRecordTimeout bounds the writes that record what Telegram did.
	// They run on a context detached from the caller's: the deadline that
	// killed a send must not also stop us recording that the send happened.
	deliveryRecordTimeout = 5 * time.Second
	// deliveryBusyRetry spaces attempts on a turn another process holds.
	deliveryBusyRetry = 250 * time.Millisecond
	// maxDeliveryAcquireAttempts bounds those attempts. The lease is what
	// frees a turn held by a process that died, so this has to outlast one;
	// it bounds how long a live holder can keep the session's queue waiting.
	maxDeliveryAcquireAttempts = 240
	// maxDeliveryClaimErrorAttempts is the budget when the ownership write
	// itself fails. That is a database problem, not a busy turn: retrying it
	// for a minute holds the session's queue for a minute, and the reply is no
	// more deliverable at the end of it.
	maxDeliveryClaimErrorAttempts = 5
)

// deliveryOutcome is what Telegram did with one send.
type deliveryOutcome int

const (
	// deliveryAccepted — the provider returned a message id.
	deliveryAccepted deliveryOutcome = iota
	// deliveryRefused — the provider answered and refused. Nothing is in the
	// chat, so the send may be attempted again.
	deliveryRefused
	// deliveryUnknown — no answer came back. The message may be in the chat.
	deliveryUnknown
)

// deliveryStatus is why an acquire did not hand over the turn.
type deliveryStatus int

const (
	deliveryAcquired deliveryStatus = iota
	// deliveryBusy — another process holds a live lease. Retry.
	deliveryBusy
	// deliveryClosed — the turn is settled, or the final answer has taken over
	// a reply a streaming frame wanted. Stop.
	deliveryClosed
)

// deliveryLease is this process's hold on one turn's reply, together with the
// state it found. Every write goes through the lease, so a caller cannot
// record an outcome against a turn it no longer owns.
type deliveryLease struct {
	turnID pgtype.UUID
	token  pgtype.UUID
	row    db.ChannelReplyDelivery
}

// messageID is the Telegram message this turn owns, or 0 when there is none to
// edit. An in-flight or lost send has no usable id.
func (l *deliveryLease) messageID() int64 {
	if l == nil || l.row.SendState != deliverySendKnown || l.row.MessageID == "" {
		return 0
	}
	id, err := strconv.ParseInt(l.row.MessageID, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// chunksSent is how many parts of the final answer are already in the chat.
func (l *deliveryLease) chunksSent() int {
	if l == nil {
		return 0
	}
	return int(l.row.ChunksSent)
}

func (l *deliveryLease) sendState() string {
	if l == nil {
		return deliverySendNone
	}
	return l.row.SendState
}

// replyTurn is which user turn a task belongs to and how far down that turn's
// retry chain the task sits.
type replyTurn struct {
	id    pgtype.UUID
	depth int32
}

// turnFor maps a task to its user turn: the root of its automatic-retry chain.
// A retry runs under a new task id and has to finish the reply its previous
// attempt started rather than answer beside it.
//
// A task with no queue row is its own turn, at depth zero — that is a fact, not
// a failure. Any other error is reported: guessing that the task is its own
// turn would open a second reply next to the one the previous attempt is still
// holding, which is the bug this lineage exists to prevent.
func (o *Outbound) turnFor(ctx context.Context, taskID pgtype.UUID) (replyTurn, error) {
	row, err := o.q.GetChannelReplyTurn(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return replyTurn{id: taskID}, nil
		}
		return replyTurn{}, fmt.Errorf("resolve reply turn: %w", err)
	}
	if !row.TurnID.Valid {
		return replyTurn{id: taskID}, nil
	}
	return replyTurn{id: row.TurnID, depth: int32(row.AttemptDepth)}, nil
}

// acquireDelivery takes the turn's lease for one of the delivery phases.
func (o *Outbound) acquireDelivery(ctx context.Context, target *replyTarget, turn replyTurn, phase string) (*deliveryLease, deliveryStatus, error) {
	turnID := turn.id
	token := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	row, err := o.q.AcquireChannelReplyDelivery(ctx, db.AcquireChannelReplyDeliveryParams{
		TurnID:         turnID,
		TaskID:         target.taskID,
		AttemptDepth:   turn.depth,
		BindingID:      target.bindingID,
		InstallationID: target.installationID,
		ChannelType:    string(TypeTelegram),
		ChatID:         target.channelChatID,
		Phase:          phase,
		OwnerToken:     token,
		LeaseSeconds:   o.leaseSeconds(),
	})
	if err == nil {
		return &deliveryLease{turnID: turnID, token: token, row: row}, deliveryAcquired, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, deliveryBusy, err
	}

	// No row means the turn was refused, and the three reasons need different
	// answers: settled and taken-over are final, a live owner is not.
	current, readErr := o.q.GetChannelReplyDelivery(ctx, turnID)
	if readErr != nil {
		if errors.Is(readErr, pgx.ErrNoRows) {
			// Raced with a concurrent close; nothing to deliver.
			return nil, deliveryClosed, nil
		}
		return nil, deliveryBusy, readErr
	}
	if current.Phase == deliveryPhaseSettled {
		return nil, deliveryClosed, nil
	}
	if phase == deliveryPhaseStreaming && current.Phase == deliveryPhaseTerminal {
		return nil, deliveryClosed, nil
	}
	if turn.depth < current.AttemptDepth {
		// A later attempt of this turn has taken over. This one is not waiting
		// for anything — it has been superseded, and its frames must not
		// rewrite what the user is now reading.
		return nil, deliveryClosed, nil
	}
	return nil, deliveryBusy, nil
}

// releaseDelivery hands the turn back so the next path does not wait out the
// lease. Best effort: an unreleased lease expires on its own.
func (o *Outbound) releaseDelivery(ctx context.Context, lease *deliveryLease) {
	if lease == nil {
		return
	}
	ctx, cancel := o.recordContext(ctx)
	defer cancel()
	if _, err := o.q.ReleaseChannelReplyDelivery(ctx, db.ReleaseChannelReplyDeliveryParams{
		TurnID:     lease.turnID,
		OwnerToken: lease.token,
	}); err != nil {
		o.logger.WarnContext(ctx, "telegram outbound: releasing the delivery lease failed", "error", err)
	}
}

// claimSend publishes a send before it is made, so any other process reads
// "a send is outstanding" rather than "nothing has been sent".
func (o *Outbound) claimSend(ctx context.Context, lease *deliveryLease) bool {
	rows, err := o.q.MarkChannelReplyDeliverySending(ctx, db.MarkChannelReplyDeliverySendingParams{
		TurnID:     lease.turnID,
		OwnerToken: lease.token,
	})
	if err != nil {
		o.logger.WarnContext(ctx, "telegram outbound: publishing the send failed; not sending", "error", err)
		return false
	}
	return rows == 1
}

// classifySend turns a Bot API result into what the turn now knows.
func classifySend(err error) deliveryOutcome {
	switch {
	case err == nil:
		return deliveryAccepted
	case isDefiniteRejection(err):
		return deliveryRefused
	default:
		return deliveryUnknown
	}
}

// isDefiniteRejection reports whether Telegram answered in a way that proves
// the message was not posted.
//
// A 5xx does not prove that. Telegram can accept a sendMessage and still fail
// on the way back, and treating that as "nothing was sent" is what puts the
// answer in the chat twice. Only a client error — including a 429, which is
// refused outright — is evidence of a message that never landed.
func isDefiniteRejection(err error) bool {
	var ae *apiError
	if !errors.As(err, &ae) {
		return false
	}
	return ae.Code >= 400 && ae.Code < 500
}

// recordSend writes what Telegram did. placeholder distinguishes the streamed
// message, which gives the turn something to edit, from a part of the final
// answer, which is progress. chunksSent is ignored for a placeholder.
func (o *Outbound) recordSend(ctx context.Context, lease *deliveryLease, placeholder bool, messageID int64, chunksSent int, sendErr error) deliveryOutcome {
	outcome := classifySend(sendErr)
	ctx, cancel := o.recordContext(ctx)
	defer cancel()

	var err error
	switch outcome {
	case deliveryAccepted:
		if placeholder {
			_, err = o.q.RecordChannelReplyDeliveryPlaceholder(ctx, db.RecordChannelReplyDeliveryPlaceholderParams{
				TurnID:     lease.turnID,
				OwnerToken: lease.token,
				MessageID:  strconv.FormatInt(messageID, 10),
			})
		} else {
			_, err = o.q.RecordChannelReplyDeliveryChunk(ctx, db.RecordChannelReplyDeliveryChunkParams{
				TurnID:     lease.turnID,
				OwnerToken: lease.token,
				MessageID:  strconv.FormatInt(messageID, 10),
				ChunksSent: int32(chunksSent),
			})
		}
	case deliveryRefused:
		_, err = o.q.ResetChannelReplyDeliverySend(ctx, db.ResetChannelReplyDeliverySendParams{
			TurnID:     lease.turnID,
			OwnerToken: lease.token,
		})
	default:
		o.logger.WarnContext(ctx, "telegram outbound: send result unknown; delivery stops rather than risk a duplicate",
			"turn_id", uuidText(lease.turnID), "error", sendErr)
		// Deliberately not owner-checked: this write has to land even if our
		// lease expired while the request hung, because the successor reading
		// this row must not conclude that nothing was ever sent.
		_, err = o.q.MarkChannelReplyDeliverySendUnknown(ctx, lease.turnID)
	}
	if err != nil {
		// The row now disagrees with Telegram. Say so loudly: a lost progress
		// write is what makes a later delivery repeat a part, and a lost
		// unknown write is what makes it re-send.
		o.logger.ErrorContext(ctx, "telegram outbound: recording the send outcome failed; delivery state is behind Telegram",
			"turn_id", uuidText(lease.turnID), "outcome", outcome, "error", err)
	}
	return outcome
}

// settleDelivery ends the turn. Afterwards no path sends or edits for it,
// including a text frame that was still in flight when the task finished.
func (o *Outbound) settleDelivery(ctx context.Context, lease *deliveryLease, reason string) {
	ctx, cancel := o.recordContext(ctx)
	defer cancel()
	if _, err := o.q.SettleChannelReplyDelivery(ctx, db.SettleChannelReplyDeliveryParams{
		TurnID:        lease.turnID,
		OwnerToken:    lease.token,
		SettledReason: reason,
	}); err != nil {
		o.logger.ErrorContext(ctx, "telegram outbound: settling the reply failed; a late frame may reopen it",
			"turn_id", uuidText(lease.turnID), "reason", reason, "error", err)
	}
}

// closeOutcome is what closeTurn learned about the turn. The two closed
// outcomes are kept apart because a file-only reply delivers its files from
// the close: only the call that actually ended the turn may do that, or a
// duplicate chat:done would send them again.
type closeOutcome int

const (
	// closeHeld: a live owner still holds the turn; worth waiting for.
	closeHeld closeOutcome = iota
	// closedNow: this call ended the turn.
	closedNow
	// closedAlready: settled earlier, or superseded by a later attempt —
	// closed as far as this attempt is concerned, but not by it.
	closedAlready
)

// closeTurn ends a turn with no answer to deliver — cancelled, or completed
// empty. It creates the row when the turn never reached Telegram at all:
// without that, a first text frame arriving after the cancellation would find
// nothing, open a placeholder, and leave it there with nothing to finish it.
func (o *Outbound) closeTurn(ctx context.Context, target *replyTarget, turn replyTurn, reason string) (closeOutcome, error) {
	turnID := turn.id
	_, err := o.q.CloseChannelReplyDeliveryTurn(ctx, db.CloseChannelReplyDeliveryTurnParams{
		TurnID:         turnID,
		TaskID:         target.taskID,
		AttemptDepth:   turn.depth,
		BindingID:      target.bindingID,
		InstallationID: target.installationID,
		ChannelType:    string(TypeTelegram),
		ChatID:         target.channelChatID,
		SettledReason:  reason,
	})
	if err == nil {
		return closedNow, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return closeHeld, err
	}
	// Settled, superseded, or held by a live owner — only the last is worth
	// waiting for.
	current, readErr := o.q.GetChannelReplyDelivery(ctx, turnID)
	if readErr != nil {
		return closeHeld, readErr
	}
	if turn.depth < current.AttemptDepth {
		// A later attempt owns this turn. Closing it is not this attempt's to
		// do, and waiting for the chance would hold the session's queue for a
		// reply that is already someone else's.
		return closedAlready, nil
	}
	if current.Phase == deliveryPhaseSettled {
		return closedAlready, nil
	}
	return closeHeld, nil
}

// recordContext detaches a write that records what Telegram did from the
// caller's deadline. The request that timed out is exactly when recording
// matters most: dropping the write leaves the row claiming a send is still in
// flight, and that blocks the turn until the lease expires.
func (o *Outbound) recordContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), deliveryRecordTimeout)
}

func uuidText(id pgtype.UUID) string {
	text, err := id.Value()
	if err != nil || text == nil {
		return ""
	}
	s, _ := text.(string)
	return s
}

// renewDelivery re-proves the turn is ours and pushes the lease out. Terminal
// delivery holds a turn across several scheduler rounds, so it asks again
// before each Telegram call rather than trusting a lease taken minutes ago.
// False means another process owns the turn now and this one must stop.
func (o *Outbound) renewDelivery(ctx context.Context, lease *deliveryLease) bool {
	rows, err := o.q.RenewChannelReplyDelivery(ctx, db.RenewChannelReplyDeliveryParams{
		TurnID:       lease.turnID,
		OwnerToken:   lease.token,
		LeaseSeconds: o.leaseSeconds(),
	})
	if err != nil {
		o.logger.WarnContext(ctx, "telegram outbound: renewing the delivery lease failed; not sending", "error", err)
		return false
	}
	return rows == 1
}

// inheritedSend reports a send this process did not make and cannot resolve.
// Acquiring a turn whose previous owner died mid-send does not mean nothing
// was sent — it means nobody knows. Recording that is what stops the successor
// from posting a second copy, and what lets the turn finish instead of waiting
// on a lease forever.
func (o *Outbound) inheritedSend(ctx context.Context, lease *deliveryLease) bool {
	switch lease.sendState() {
	case deliverySendUnknown:
		return true
	case deliverySendInFlight:
		o.logger.WarnContext(ctx, "telegram outbound: inherited a send from a process that stopped; outcome unknown",
			"turn_id", uuidText(lease.turnID))
		recordCtx, cancel := o.recordContext(ctx)
		defer cancel()
		if _, err := o.q.MarkChannelReplyDeliverySendUnknown(recordCtx, lease.turnID); err != nil {
			o.logger.ErrorContext(ctx, "telegram outbound: recording the inherited send failed",
				"turn_id", uuidText(lease.turnID), "error", err)
		}
		return true
	}
	return false
}

// leaseSeconds is the lease length this Outbound hands out.
func (o *Outbound) leaseSeconds() float64 {
	if o.leaseTTL > 0 {
		return o.leaseTTL.Seconds()
	}
	return deliveryLeaseTTL.Seconds()
}

// callContext bounds one provider call so it cannot outlive the lease that
// authorises it. Paired with re-proving the lease immediately beforehand, this
// is what makes "the turn was taken over mid-call" a budget question rather
// than a race: the call cannot still be running when the lease lapses.
func (o *Outbound) callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	budget := deliveryCallTimeout
	if o.leaseTTL > 0 && o.leaseTTL < deliveryLeaseTTL {
		// Tests shorten the lease to exercise takeover; the call budget has to
		// shrink with it or the relationship under test stops holding.
		budget = o.leaseTTL / 3
	}
	return context.WithTimeout(ctx, budget)
}
