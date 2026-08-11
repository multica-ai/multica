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
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---- the budget a delivery speaks on ----

// TestAnAnswerIsNotDeliveredOnWhatTheWaitLeftOver is the ordering the previous
// round's fix created and did not close.
//
// A wait that LOSES comes back as errEndingDeferred. A wait that is WON costs
// the caller exactly as much: the round is consumed, the ledger hands the answer
// its turn, and the binding row, the installation row and the ack all run on
// whatever the wait left — which after a ten-second wait on a ten-second budget
// is nothing. Every one of them then fails with a context error against a round
// already spent, so the words the retry would carry go out beside a spinner
// nothing seals rather than into it. chat:done fires once and the completion
// lives nowhere this process can go back to.
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

// TestADeliveryBudgetOutlivesACallerThatGivesUp is what the two tests above rest
// on, stated without the coincidence they rest on it through.
//
// They pass today because a caller's budget and streamCloseTimeout are both ten
// seconds (chatRunFlushTimeout, engine/router.go), which makes "cancelled" and
// "nearly out of time" the same input. At eleven they come apart, and each of
// them lands on a different half of what the ledger has to guarantee: a caller
// cancelled BEFORE the delivery starts, and a caller that gives up DURING one —
// which is where the answer's own test cancels, at the binding row. The round is
// consumed either way and the wait did not lose, so neither has a deferral to
// come back on and both are the same lost answer.
//
// An hour on the clock in both halves, to say that how much is left is not the
// question.
func TestADeliveryBudgetOutlivesACallerThatGivesUp(t *testing.T) {
	t.Parallel()

	t.Run("cancelled before the delivery starts", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		cancel()

		sayCtx, done := deliveryBudget(ctx)
		defer done()

		if err := sayCtx.Err(); err != nil {
			t.Fatalf("the delivery was handed a context already %v, with an hour of the caller's "+
				"deadline unspent — the round is consumed by the time this is called, so a send "+
				"that cannot start is an ending nobody says", err)
		}
		deadline, ok := sayCtx.Deadline()
		if !ok || time.Until(deadline) > streamCloseTimeout {
			t.Fatalf("the delivery's deadline = %v (set: %v), want at most %v from now — a budget "+
				"detached from its caller still has to be bounded", deadline, ok, streamCloseTimeout)
		}
	})

	t.Run("cancelled under the delivery", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		sayCtx, done := deliveryBudget(ctx)
		defer done()

		cancel() // the caller gives up while the send is on its way

		if err := sayCtx.Err(); err != nil {
			t.Fatalf("the delivery's budget died with its caller (%v) — the bubble is already "+
				"gone, so the send that was under way is the only thing that could have put "+
				"anything under it", err)
		}
	})
}

// TestADeliveryThatPanicsGivesItsBudgetBack is the timer nobody would ever see.
//
// events.Bus recovers a listener's panic and carries on with the next one, so a
// delivery that panics mid-send unwinds through the ledger and the run continues.
// What it leaves behind is whatever was not released on the way out: the budget
// is a context.WithTimeout, and an uncancelled one keeps a runtime timer armed
// for the whole streamCloseTimeout with nothing holding a reference that could
// stop it.
func TestADeliveryThatPanicsGivesItsBudgetBack(t *testing.T) {
	t.Parallel()
	s := newStreamStore()
	var budget context.Context
	func() {
		defer func() { _ = recover() }()
		_, _ = s.sayEnding(context.Background(), bubbleSessionID(t), byTask(taskUUID(t, "task-1")),
			roundOver, nil,
			func(ctx context.Context, _ roundTurn) (roundAddress, error) {
				budget = ctx
				panic("a bus listener died mid-delivery")
			})
	}()
	if budget == nil {
		t.Fatal("the delivery was never handed a budget")
	}
	if budget.Err() == nil {
		t.Error("the delivery panicked and its budget was left live — the timer behind it stays " +
			"armed for a whole streamCloseTimeout after the goroutine that was using it is gone")
	}
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

// TestASettleOnADeadSocketComesBack is the window the settle's retry was written
// for, and the one a whitelist written over error TYPES cannot see.
//
// A socket that has been reset is not errNoLiveConnection. The read loop is what
// notices a dead connection and it waits out its own 90-second read deadline
// first (readDeadline, wecom_channel.go), so for a minute and a half the registry
// still hands out a sender and every send it takes comes back as a write error
// instead. That is the commonest shape of the reconnect window, not the rare one.
//
// A settled round is the ending with no second publisher of any kind: exactly one
// of OnRunStarted and OnSettled fires per batch, and with no task there is no
// lifecycle event behind it. So an attempt that books nothing here leaves the
// asker watching a spinner with nothing behind it for as long as the chat stays
// open.
func TestASettleOnADeadSocketComesBack(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.typing.endingRetryAfter = 20 * time.Millisecond
	rig.ask(t, "REQ-DEAD-SOCKET", 1) // the bubble is painted while the socket is alive
	rig.conn.breakTheSocket()

	rig.typing.OnSettled(context.Background(), bubbleSessionID(t), 1)

	// The first attempt writes the closing frame, fails, and falls back to a
	// plain message that fails the same way. Everything after the first push is
	// an attempt that came back for the ending.
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := pushedTexts(t, rig.conn)
		if len(got) > 1 {
			for _, text := range got {
				if text != streamCopyNotStarted {
					t.Fatalf("the messages attempted were %q, want every one of them %q",
						got, streamCopyNotStarted)
				}
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the settled round's ending was attempted %d time(s) (%q) on a socket that "+
				"refused every write, want a second attempt — this ending has no other publisher, "+
				"so an attempt that books nothing leaves the asker on a spinner nothing will "+
				"ever seal", len(got), got)
		}
		time.Sleep(time.Millisecond)
	}
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

// TestAnOutcomeNobodyCanClassifyBooksNothingUnderAnyCause is the zero value
// doing what it says.
//
// endingRetryCause answers with a cause and an ok, and the caller that reads it
// checks the ok — so the cause it returns alongside "no" is only ever as
// dangerous as the next caller is careless. retryNone makes that reading safe
// whichever way it is read: it is not a cause anything can be booked under.
func TestAnOutcomeNobodyCanClassifyBooksNothingUnderAnyCause(t *testing.T) {
	t.Parallel()
	cause, ok := endingRetryCause(errors.Join(errWordsMayBeOnScreen, errStreamAckTimeout))
	if ok {
		t.Fatal("an ending that may already be on the screen was classified as repeatable")
	}
	if cause != retryNone {
		t.Errorf("the cause alongside a refusal to repeat = %v, want retryNone — a caller that "+
			"books on it would spend one of the two real causes' attempts", cause)
	}
	now := time.Now()
	spent := endingRetries{}.begin(now)
	if _, _, booked := spent.next(cause, endingRetryAfter, endingRetryAttempts, now); booked {
		t.Error("an attempt was booked under retryNone — the schedule has to refuse it, or the " +
			"whole guard is one caller forgetting to read an ok")
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
	if o.bookAnswerRetry(bubbleSessionID(t), "task", "content", endingRetries{}.begin(time.Now()), retryRefused) {
		t.Error("a subscriber with retries disabled booked one")
	}
}

// TestAnAnswerOnADeadSocketIsSealedIntoTheBubbleItLeft is the same window on
// the one ending that has no publisher behind it whatsoever.
//
// The round keeps its bubble when both routes fail on a socket that took
// nothing (bubbleSurvivedTheFailure), which is what lets the NEXT publisher
// seal the spinner in place. There is no next publisher here. chat:done fires
// once, the guard is refused a round whose run is over (refuseTakeLocked), and
// the completion lives nowhere this process can go back to — so unless the
// subscriber holding the answer comes back for that bubble itself, the answer
// is gone and the spinner runs until the sweep retires it.
//
// The socket dies, the closing frame does not go and neither does the plain
// message behind it, and the Supervisor reconnects. What the asker has to end
// up with is "42" inside the bubble their question opened — once.
func TestAnAnswerOnADeadSocketIsSealedIntoTheBubbleItLeft(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.out.retryAfter = 20 * time.Millisecond
	rig.ran(t, "REQ-DEAD-ANSWER", 1, "task-1")

	opened := rig.conn.streamFrames(t)
	if len(opened) != 1 || opened[0]["finish"] != false {
		t.Fatalf("the question opened %v, want one unfinished bubble", opened)
	}
	rig.conn.breakTheSocket()

	handled := rig.answerErr(t, "42", "task-1")
	next := rig.reconnect()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(said(t, next)) == 0 {
		time.Sleep(time.Millisecond)
	}
	if got := said(t, next); len(got) != 1 || got[0] != "42" {
		t.Fatalf("after the socket came back the asker read %q, want [\"42\"] — both routes "+
			"reached nobody, and the subscriber carrying the completion is the only publisher "+
			"this round has left", got)
	}
	sealed := next.streamFrames(t)
	if len(sealed) != 1 || sealed[0]["id"] != opened[0]["id"] || sealed[0]["finish"] != true {
		t.Fatalf("the attempt that came back wrote %v, want one closing frame on stream %v — "+
			"nothing reached the peer on the attempt that failed, so the round still had the "+
			"handle that seals the spinner the asker is watching", sealed, opened[0]["id"])
	}
	if got := pushedTexts(t, next); len(got) != 0 {
		t.Fatalf("the answer also went out as a new message (%q) — that is a second copy of it "+
			"beside the bubble that already carries it", got)
	}
	if handled != nil {
		t.Errorf("the chat:done reported %v while a booked attempt was carrying the answer; "+
			"the bus has no redelivery, so the error is a WARN about work still in hand", handled)
	}
}

// ---- and the rule that decides, rather than a list of failures it recognises ----

// TestAnAnswerComesBackForAnOutcomeNobodyHasClassified is this round's actual
// subject, stated once instead of one failure at a time.
//
// Four rounds were spent adding an error class to a list of outcomes worth
// another attempt, and each of them was followed by the class that was not on
// the list — a wait that expired, a wait that was WON with the budget spent, a
// send that failed on a dead socket. A list of what may be repeated is not
// closed under a failure nobody has met yet, and every gap in it costs the
// asker the only copy of their answer.
//
// So the direction is the assertion. The last case here is an error value this
// package has never produced and never will: whatever it turns out to mean, it
// is not one of the two ack waits saying the words may be in front of somebody,
// and the answer this subscriber is still holding comes back for the round.
func TestAnAnswerComesBackForAnOutcomeNobodyHasClassified(t *testing.T) {
	t.Parallel()
	broken := errors.New("write tcp 10.0.0.2:52170->10.0.0.9:443: write: broken pipe")
	cases := []struct {
		name  string
		err   error
		cause retryCause
		again bool
		why   string
	}{{
		name: "the words landed", err: nil, cause: retryNone, again: false,
		why: "the run is on told and a repeat is a second copy of the reply",
	}, {
		name: "nothing to say", err: errNothingToSay, cause: retryNone, again: false,
		why: "the delivery declined on purpose; there is nothing to come back for",
	}, {
		name: "the words may be on the screen",
		err:  errors.Join(errWordsMayBeOnScreen, errStreamAckTimeout, fmt.Errorf("%w: %w", errFrameNotOnTheWire, broken)),
		why:  "the closing frame reached the wire and lost only its verdict",
	}, {
		name: "the wait outlasted the budget", err: errEndingDeferred, cause: retryDeferred, again: true,
		why: "nothing was said and nothing recorded; the news is still this one's to deliver",
	}, {
		name: "the socket took nothing", err: fmt.Errorf("%w: %w", errFrameNotOnTheWire, broken),
		cause: retryRefused, again: true,
		why: "a truncated frame reaches no application, so the answer is on nobody's screen",
	}, {
		name: "no socket at all", err: errNoLiveConnection, cause: retryRefused, again: true,
		why: "there was nothing to write to",
	}, {
		name: "the server refused the push", err: &wecomAPIError{Cmd: cmdSendMsg, Code: errcodeRefusedPush},
		cause: retryRefused, again: true,
		why: "an errcode is the server stating that it showed nobody anything",
	}, {
		name: "a row this delivery could not read", err: errors.New("wecom: load installation: connection refused"),
		cause: retryRefused, again: true,
		why: "the send never happened; nothing about the screen changed",
	}, {
		name: "an error class nobody has met", err: errors.New("wecom: something nobody has written yet"),
		cause: retryRefused, again: true,
		why: "unrecognised is not evidence the asker is reading anything, and the answer " +
			"exists nowhere but in this subscriber's hands",
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			cause, again := answerRetryCause(c.err)
			if again != c.again {
				t.Fatalf("answerRetryCause(%v) came back again=%v, want %v — %s", c.err, again, c.again, c.why)
			}
			if cause != c.cause {
				t.Errorf("cause = %v, want %v — the counts are kept apart so a chain of waits "+
					"cannot leave a refused send with nothing to spend", cause, c.cause)
			}
		})
	}
}

// TestAStreamFrameWithNoVerdictSaysTheWordsMayBeThere is the other half of what
// the rule above rests on, and the half a deny-list cannot do without.
//
// Retrying by default is only safe while every outcome that could have put
// words in front of somebody says so itself. Two can: the ack wait behind a
// plain message (TestSendTextDistinguishesALostAckFromARefusal) and this one,
// behind a stream frame. The frame is on the wire and only the verdict is
// missing, so the bubble may be sealed with the answer already.
func TestAStreamFrameWithNoVerdictSaysTheWordsMayBeThere(t *testing.T) {
	t.Parallel()
	conn := &silentConn{} // written, never answered
	sender := newWSSender(conn, nil)
	sender.ackTimeout = 20 * time.Millisecond

	err := sender.respondStream(context.Background(), "REQ-1", "S-1", "the agent reply", true)
	if !errors.Is(err, errStreamAckTimeout) {
		t.Fatalf("a closing frame with no verdict reported %v, want errStreamAckTimeout", err)
	}
	if !errors.Is(err, errWordsMayBeOnScreen) {
		t.Fatal("the frame went out and its verdict never came back, and the failure does not " +
			"say so — every reader of it then treats a bubble that may already carry the " +
			"answer as one that carries nothing, and says the answer again beside it")
	}
	if bubbleSurvivedTheFailure(err) {
		t.Error("the round would be handed its bubble back for a frame that reached the wire — " +
			"the attempt that comes next seals a second copy of the answer into it")
	}
}

// TestAnAnswerNoRouteEvenStartedIsNotWrittenOffAsPossiblyDelivered is the same
// direction one layer down, where the two routes are added together.
//
// A delivery is two sends and only the second one's error comes back whole, so
// deliverAnswer has to say what the FIRST one did. Marking the pair "may be on
// the screen" whenever the closing frame's failure is one it does not recognise
// puts the same drop back: an outcome nobody has classified suppresses the
// attempt, and the asker keeps the spinner.
//
// Here neither send starts. The caller's context is already gone, so the stream
// frame gives up at respondStream's own entry check and the plain message gives
// up at request's — before the writer, before the socket, with nothing on the
// wire either time. That is not ambiguity about the screen, and the answer is
// still this subscriber's to deliver.
func TestAnAnswerNoRouteEvenStartedIsNotWrittenOffAsPossiblyDelivered(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ask(t, "REQ-NO-ROUTE", 1)
	opened := len(rig.conn.streamFrames(t))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := rig.out.deliverAnswer(ctx, bubbleSessionID(t), roundTurn{
		HasBubble: true,
		Handle: streamHandle{
			ReqID: "REQ-NO-ROUTE", StreamID: "S-1",
			InstallationID: rig.instID, ChatID: "CHAT_1", ChatType: chatTypeSingleInt,
		},
	}, "42")

	if err == nil {
		t.Fatal("a delivery on a context that was already gone reported success")
	}
	if errors.Is(err, errWordsMayBeOnScreen) {
		t.Fatalf("the attempt reported %v — neither send reached the writer, so nothing is on "+
			"anybody's screen, and the mark is what stops this subscriber coming back for the "+
			"only copy of the answer", err)
	}
	if _, again := answerRetryCause(err); !again {
		t.Error("the answer was written off after an attempt that put nothing on the wire")
	}
	if now := len(rig.conn.streamFrames(t)); now != opened {
		t.Errorf("%d stream frame(s) were written, want the %d the question opened — this test "+
			"is about the sends that never started", now, opened)
	}
}

// A frame stopped before the write has to say so, or its round loses the bubble
// it never spent.
//
// respondStream has three ways out ahead of the write — no callback req_id, a
// context already over, a body that would not marshal — and each returns a bare
// error with no name on it. Nothing reached the wire on any of them, which is
// exactly what errFrameNotOnTheWire exists to say, and only this function is in
// a position to say it.
//
// What that costs is not the answer. sayTheAnswer comes back for an outcome it
// cannot classify, so the words are still delivered. It is the BUBBLE: the round
// goes back on the open list only when bubbleSurvivedTheFailure agrees the
// spinner is untouched, and an unnamed failure cannot clear that bar. So the
// round is consumed, and the retry sayTheAnswer just booked — the one whose
// whole point is to "seal the spinner the asker is watching, in place, with the
// answer" — arrives to find no bubble and speaks beside it instead.
//
// The context case is the live one: respondStream's callers run on a bus
// subscriber's own budget, so a context already over here is an ordinary busy
// afternoon rather than a programming error.
func TestAFrameStoppedBeforeTheWriteKeepsItsBubble(t *testing.T) {
	t.Parallel()

	over, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name  string
		ctx   context.Context
		reqID string
	}{
		{"no callback req_id", context.Background(), ""},
		{"context already over", over, "REQ-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := &silentConn{}
			sender := newWSSender(conn, nil)

			err := sender.respondStream(tc.ctx, tc.reqID, "S-1", "the answer", true)
			if err == nil {
				t.Fatal("want an error; this case's premise is gone")
			}
			if n := conn.count(); n != 0 {
				t.Fatalf("%d frames were written — the premise of this test is that none was", n)
			}
			if !errors.Is(err, errFrameNotOnTheWire) {
				t.Errorf("err = %v, want it to carry errFrameNotOnTheWire — this is the last place that knows no byte moved", err)
			}
			if !bubbleSurvivedTheFailure(err) {
				t.Errorf("bubbleSurvivedTheFailure(%v) = false — the round gives up a spinner nothing touched, and the retry that follows speaks beside it instead of sealing it", err)
			}
		})
	}
}
