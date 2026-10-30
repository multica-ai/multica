package wecom

import (
	"context"
	"time"
)

// What every closer does with a closing frame that failed, in one place.
//
// There are three of them — the answer (outbound.go), the failure and
// cancellation notices (typing_indicator.go), and a delivery routed from
// another replica (relay_outbound.go) — and they were three separate readings
// of the same evidence until the readings disagreed. Each one now asks
// classifySeal what the error means and fallbackBudget what it may spend.
//
// Adding a fourth closer means calling these two, not writing a third reading.

// sealVerdict says what a seal's outcome means for the words it carried.
type sealVerdict int

const (
	// sealOnScreen: the frame was accepted. The words are in the bubble.
	sealOnScreen sealVerdict = iota

	// sealUnknown: the words MAY be on screen and nothing here can tell.
	// Saying them again is the one error with no way back — WeCom has no
	// unsend, so a duplicate is permanent while a delivery nobody confirmed
	// can be re-asked. The caller records it and stops.
	//
	// This is the CASCADE case, not the exception: cancelAck's note explains
	// that one lost ack times out every later frame on the same req_id, so a
	// fallback on this evidence repeats the answer once per retry.
	sealUnknown

	// sealNotOnScreen: proof the words are not in the bubble, which is the
	// only thing that licenses saying them again. Two kinds of proof —
	// streamUnusable (846605 / 846608: this stream will never take another
	// frame, so nothing was written into it) and provablyNotSent (the failure
	// was raised before any byte reached the socket).
	sealNotOnScreen
)

// classifySeal reads a seal's error. Note what is NOT proof: an ack that never
// came, and a write whose own error may still have left bytes with the peer
// (errWriteAttempted's doc says exactly that).
//
// Staleness is not one of the questions. A callback's req_id belongs to the
// turn rather than to the socket it arrived on, and a stream opened before a
// reconnect is still writable after it — measured against a live tenant, see
// senders_registry.go.
func classifySeal(err error) sealVerdict {
	switch {
	case err == nil:
		return sealOnScreen
	case streamUnusable(err), provablyNotSent(err):
		return sealNotOnScreen
	default:
		return sealUnknown
	}
}

// fallbackBudget gives the plain message a budget the bubble cannot have
// already spent.
//
// THE BUBBLE MUST NOT BE ABLE TO SPEND THE ANSWER'S BUDGET. seal retries a lost
// ack up to streamCloseRetries times, each attempt costing an ackTimeout and a
// streamCloseRetryDelay, and the caller's streamCloseTimeout does not cover
// that (see TestTheCloseRetryPolicyFitsTheBudgetItRunsUnder). When it ran out
// inside seal, the plain message that is this path's whole point then ran on
// the expired context and wrote nothing, while the WARN said it had been sent.
//
// The test is what is LEFT, not whether it is already gone. A context that has
// expired never reaches here — an expired seal returns a context error, which
// is not proof of non-delivery and is classified sealUnknown. What does reach
// here is a seal that spent most of the budget and then read a real refusal,
// leaving the plain message less time than one push needs.
//
// WithoutCancel rather than a longer deadline: the reason this budget is short
// is the bubble, which is over.
func fallbackBudget(ctx context.Context) (context.Context, context.CancelFunc) {
	if d, ok := ctx.Deadline(); ok && time.Until(d) < ackTimeout {
		return context.WithTimeout(context.WithoutCancel(ctx), fallbackSendTimeout)
	}
	return ctx, func() {}
}
