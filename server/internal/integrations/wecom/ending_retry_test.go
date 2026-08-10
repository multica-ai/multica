package wecom

// ending_retry_test.go — what happens to an ending after the attempt that was
// supposed to deliver it did not.
//
// Three questions, and they are separate. Does the delivery get a budget it can
// actually speak in once the ledger hands it the round? When it comes back
// undelivered, does the publisher come again? And when it comes back undelivered
// but the words may be on the screen anyway, does it have the sense not to?

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---- the budget a delivery speaks on ----

// TestAnAnswerIsNotDeliveredOnWhatTheWaitLeftOver is the ordering the previous
// round's fix created and did not close.
//
// A wait that LOSES comes back as errEndingDeferred and books a retry. A wait
// that is WON costs the caller exactly as much and books nothing: the round is
// consumed, the ledger hands the answer its turn, and the binding row, the
// installation row and the ack all run on whatever the wait left — which after
// a ten-second wait on a ten-second budget is nothing. Every one of them then
// fails with a context error, which is neither a deferral nor a refusal, so the
// subscriber returns it to a WARN and the answer is gone. chat:done fires once
// and the completion lives nowhere this process can go back to.
//
// What the asker is left with is the guard's "还在处理，完成后我再单独回复你" and
// nothing behind it — the same screen as the deferral bug, one instant later.
//
// The budget is spent here where the parent's probe spends it, at the binding
// lookup, rather than by racing a real wait: what the test is about is the
// delivery's budget, and a delivery that starts with none behaves the same
// whichever way it got there.
func TestAnAnswerIsNotDeliveredOnWhatTheWaitLeftOver(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.ran(t, "REQ-SPENT-BUDGET", 1, "task-1")
	rig.guardClosed(t, 1) // the bubble is gone and a promise stands in its place

	budget, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rig.q.atBinding = cancel

	if err := rig.out.processEvent(budget, events.Event{
		ChatSessionID: bubbleSession,
		TaskID:        taskUUID(t, "task-1"),
		Payload:       protocol.ChatDonePayload{Content: "42"},
	}); err != nil {
		t.Fatalf("the answer reported %v — its round trips ran on a budget the wait had already "+
			"spent, so the delivery died on a context error with the round consumed and no "+
			"deferral to book a retry from", err)
	}
	wantSaid(t, rig.conn, []string{streamCopyStillWorking, "42"},
		"the answer never reached the wire; the asker keeps the guard's promise of a separate "+
			"reply and the completion exists nowhere this process can go back to")
}

// TestASettleIsNotDeliveredOnWhatTheWaitLeftOver is the same budget on the path
// with no second publisher of any kind.
//
// A settle whose delivery dies on a context error DOES book a retry — this
// manager books one for any undelivered ending — so the loss here is smaller
// than the answer's. It is still a full streamCloseTimeout of the asker staring
// at a spinner for a run that never started, paid for nothing.
func TestASettleIsNotDeliveredOnWhatTheWaitLeftOver(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ask(t, "REQ-SPENT-SETTLE", 1)

	budget, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// A settle with a bubble still on screen writes a closing frame, so the
	// budget has to go before that rather than at a binding lookup it never
	// makes.
	cancel()

	rig.typing.OnSettled(budget, bubbleSessionID(t), 1)

	wantSaid(t, rig.conn, []string{streamCopyNotStarted},
		"the settled round's closing frame ran on a budget that was already gone")
}

// ---- coming back for an ending that did not land ----

// TestACancellationRefusedOnTheWireIsSaidAgain is the case the previous round's
// own doc argued for and the code did not do.
//
// sayCancelledRun's doc says this is "the ending with the fewest publishers of
// all — one broadcast, no sweeper repeat, no redelivery", which is the argument
// for coming back. It then applied that argument to a wait that ran out of
// budget and to nothing else, leaving the commoner failure — a send refused
// inside a reconnect window — exactly where it was: the cancellation gone, and
// the asker keeping the guard's promise about a run they stopped themselves.
func TestACancellationRefusedOnTheWireIsSaidAgain(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.typing.endingRetryAfter = 20 * time.Millisecond
	rig.ran(t, "REQ-CANCEL-REFUSED", 1, "task-1")
	rig.guardClosed(t, 1)

	rig.conn.refusePushes = 1 // the reconnect window; the next one goes through
	rig.cancelled(t, "task-1")

	rig.waitUntilPushed(t, []string{streamCopyCancelled, streamCopyCancelled},
		"the one notice this ending has was refused and nothing came back for it — the asker is "+
			"left on 还在处理，完成后我再单独回复你 for a run they cancelled themselves")
}

// TestAFailedRunsNoticeRefusedOnTheWireIsSaidAgain is the same shape one step
// less sharp: a failure has a second publisher in principle, but only for the
// runs a sweeper tick times out. One FailTask already reported is not repeated,
// so a notice refused in a reconnect window is as gone as the cancellation
// above.
func TestAFailedRunsNoticeRefusedOnTheWireIsSaidAgain(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.typing.endingRetryAfter = 20 * time.Millisecond
	rig.ran(t, "REQ-FAILED-REFUSED", 1, "task-1")
	rig.guardClosed(t, 1)

	rig.conn.refusePushes = 1
	rig.failed(t, "task-1", false)

	rig.waitUntilPushed(t, []string{streamCopyFailed, streamCopyFailed},
		"the failure notice was refused on the wire and nothing came back for it — "+
			"streamCopyFailed is the only 'that run did not go through' WeCom produces")
}

// ---- and not coming back when the words may already be there ----

// TestAnEndingThatMayAlreadyBeOnScreenIsNotSaidAgain is the boundary the retry
// above must not cross, and the reason the verdict is three-valued rather than a
// boolean.
//
// wsSender.write takes the writer mutex and the socket deadline on
// context.Background() and never reads the caller's context; the caller's
// context is consulted only in the ack select afterwards. So a closing frame can
// reach the wire — sealing the bubble the asker is watching — and STILL come
// back as an ack timeout. If the plain-message fallback behind it is then
// cleanly refused, the refusal alone says "nothing reached the user", and a
// retry reading only that puts the same words on the screen a second time,
// underneath a bubble that already carries them.
//
// The round here has its bubble, the closing frame goes out unanswered, and the
// fallback is refused. Exactly one message may be attempted.
func TestAnEndingThatMayAlreadyBeOnScreenIsNotSaidAgain(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.typing.endingRetryAfter = 20 * time.Millisecond
	rig.ran(t, "REQ-UNKNOWN-ACK", 1, "task-1")

	sender := rig.senders.get(rig.instID)
	if sender == nil {
		t.Fatal("the rig has no live sender")
	}
	sender.ackTimeout = 20 * time.Millisecond
	rig.conn.swallowClosingAck = true // on the wire, verdict unknown forever
	rig.conn.refusePushes = 1         // and the fallback is cleanly turned away

	rig.cancelled(t, "task-1")

	// Well past the first slot of a 20ms schedule. A retry booked on the
	// fallback's refusal alone would have fired several times over by now.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if got := pushedTexts(t, rig.conn); len(got) > 1 {
			t.Fatalf("the cancellation was attempted %d times (%q) — the closing frame was on the "+
				"wire with its verdict unknown, so the bubble may already carry these words and "+
				"the repeat is a second copy of them", len(got), got)
		}
		time.Sleep(time.Millisecond)
	}
}

// ---- the schedule ----

// TestTheRetryScheduleIsTheOneItsDocStates drives the arithmetic the two retry
// books share, because prose about a schedule is not a schedule.
//
// The doc says three attempts at 15s, 45s and 105s after the first. What the
// code produced before this was 15s, 55s and 125s with the chain ending at 135s,
// because every attempt parks a whole streamCloseTimeout on the delivery it is
// waiting out before it defers, and a delay measured from the attempt that just
// ended restarts the clock after each of those waits.
func TestTheRetryScheduleIsTheOneItsDocStates(t *testing.T) {
	t.Parallel()
	first := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	r := endingRetries{}.begin(first)

	// Each attempt spends its whole streamCloseTimeout on the wait before it
	// books the next, which is what made the delay clock restart.
	now := first
	var fired []time.Duration
	for range endingRetryAttempts {
		now = now.Add(streamCloseTimeout)
		next, wait, ok := r.next(retryDeferred, endingRetryAfter, endingRetryAttempts, now)
		if !ok {
			t.Fatalf("the chain gave up after %d attempts, want %d", len(fired), endingRetryAttempts)
		}
		now = now.Add(wait)
		fired = append(fired, now.Sub(first))
		r = next
	}
	want := []time.Duration{15 * time.Second, 45 * time.Second, 105 * time.Second}
	for i := range want {
		if fired[i] != want[i] {
			t.Fatalf("the attempts fired at %v, want %v — an answer sits undelivered for as long "+
				"as this says it does, and the comment above endingRetryAfter states the second "+
				"of these", fired, want)
		}
	}

	// A fourth deferral is not booked; the chain is spent on that cause alone.
	if _, _, ok := r.next(retryDeferred, endingRetryAfter, endingRetryAttempts, now); ok {
		t.Fatalf("a %dth deferral was booked, want the chain spent", endingRetryAttempts+1)
	}
}

// TestTheTwoRetryCausesDoNotSpendEachOthersAttempts is the counter the settle
// path shares between two failures that have nothing to do with each other.
//
// A settle that loses three waits is a settle that never got near a socket. The
// schedule exists for the OTHER case — a send refused inside a reconnect window,
// on the one ending with no second publisher at all — and a shared counter means
// the round that lost its waits arrives at that case with nothing left.
func TestTheTwoRetryCausesDoNotSpendEachOthersAttempts(t *testing.T) {
	t.Parallel()
	first := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	r := endingRetries{}.begin(first)

	now := first
	for i := range endingRetryAttempts {
		next, wait, ok := r.next(retryDeferred, endingRetryAfter, endingRetryAttempts, now)
		if !ok {
			t.Fatalf("deferral %d was not booked", i+1)
		}
		r, now = next, now.Add(wait)
	}

	next, wait, ok := r.next(retryRefused, endingRetryAfter, endingRetryAttempts, now)
	if !ok {
		t.Fatal("a refused send after three lost waits booked nothing — the two were counted " +
			"together, so the case the schedule was built for had no attempts left")
	}
	if next.refused != 1 || next.deferred != endingRetryAttempts {
		t.Fatalf("counts after the refusal = deferred %d / refused %d, want %d / 1",
			next.deferred, next.refused, endingRetryAttempts)
	}
	// Still backing off across both causes rather than restarting at the first
	// slot, and still far inside roundMemory.
	if at := now.Add(wait).Sub(first); at != 225*time.Second || at >= roundMemory {
		t.Fatalf("the fourth attempt lands %v after the first, want 225s and well inside "+
			"roundMemory (%v)", at, roundMemory)
	}
}

// TestAnAttemptThatOutlastsItsOwnSlotRunsNextRatherThanInThePast is the state
// the anchored schedule makes reachable: an attempt whose wait is longer than
// the gap to its next slot.
func TestAnAttemptThatOutlastsItsOwnSlotRunsNextRatherThanInThePast(t *testing.T) {
	t.Parallel()
	first := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	r := endingRetries{}.begin(first)

	// An hour late for a 15s slot.
	_, wait, ok := r.next(retryDeferred, endingRetryAfter, endingRetryAttempts, first.Add(time.Hour))
	if !ok {
		t.Fatal("the first attempt was not booked")
	}
	if wait != 0 {
		t.Fatalf("an attempt whose slot had already passed was booked %v from now, want 0 — a "+
			"negative delay is a timer that fires immediately anyway, and a positive one here "+
			"would be arithmetic nobody meant", wait)
	}
}

// TestNoRetriesWithoutADelay pins the disable switch both books share, which is
// what the test rigs turn the schedule off with.
func TestNoRetriesWithoutADelay(t *testing.T) {
	t.Parallel()
	m := NewTypingIndicator(TypingIndicatorConfig{EndingRetryAfter: -1})
	if m.bookEndingRetry(endingRetries{}.begin(time.Now()), retryDeferred, func(context.Context, endingRetries) {}) {
		t.Error("a manager with retries disabled booked one")
	}
	o := &Outbound{retryAfter: -1}
	if o.bookAnswerRetry(bubbleSessionID(t), "task", "content", endingRetries{}.begin(time.Now())) {
		t.Error("a subscriber with retries disabled booked one")
	}
}
