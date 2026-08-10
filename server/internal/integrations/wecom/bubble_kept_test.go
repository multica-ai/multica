package wecom

// bubble_kept_test.go — what a failed ending leaves behind, and which failure
// decides it.
//
// A round's bubble is consumed by the take, because that is the exclusion which
// makes two racing closers write one closing frame. For most failures that is
// the end of it: the words may be on the screen, the stream may be past saving,
// and all the ledger can do is leave the run owed an ending somebody says
// somewhere else. For the one failure this package can place on the near side of
// the wire it is not: nothing was written, so the spinner is exactly where it
// was and the round goes back on the list holding it, for the next publisher to
// seal in place.
//
// These tests are about that fork. What comes out of it is the difference
// between an answer inside the bubble and an answer sitting next to a spinner
// that never stops.
//
// And about what a round handed back is NOT. It is a bubble waiting for words
// that already exist, not a run still going, so it does not queue the next round
// behind it, it is not the guard's to promise anything about, and it is not
// anybody's to seal twice. The rest of these say so one reader at a time.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

// ---- which failures hand the bubble back ----

// TestOnlyAFailureThatWentNowhereHandsTheBubbleBack is the predicate on its own,
// because the cost of getting it wrong is not a missed seal but a second copy of
// the answer.
//
// Two of these are the reconnect window's own shapes and both mean the same
// thing: the server never saw a frame, so the stream is where the opening frame
// left it. The rest are all the ways an attempt can fail with the bubble already
// beyond this process's account — an errcode that says this stream is finished
// with, and, the one that matters, a delivery whose closing frame reached the
// wire and lost only its verdict.
func TestOnlyAFailureThatWentNowhereHandsTheBubbleBack(t *testing.T) {
	t.Parallel()
	broken := errors.New("write tcp 10.0.0.2:52170->10.0.0.9:443: write: broken pipe")
	cases := []struct {
		name string
		err  error
		want bool
		why  string
	}{{
		name: "a write that did not finish",
		err:  fmt.Errorf("%w: %w", errFrameNotOnTheWire, broken),
		want: true,
		why:  "gorilla writes a frame in one Write and a truncated one reaches no application",
	}, {
		name: "no socket at all",
		err:  errNoLiveConnection,
		want: true,
		why:  "there was nothing to write to, so the stream is where the opening frame left it",
	}, {
		name: "the server refused the frame",
		err:  &streamError{Code: errcodeStreamExpired},
		want: false,
		why:  "846608 is the server saying this stream will never take another frame",
	}, {
		name: "the push was refused",
		err:  &wecomAPIError{Cmd: cmdSendMsg, Code: errcodeRefusedPush},
		want: false,
		why:  "an errcode says nothing about whether the bubble is still writable",
	}, {
		name: "the ack never came back",
		err:  errStreamAckTimeout,
		want: false,
		why:  "the frame reached the wire and only its verdict is missing",
	}, {
		name: "the frame may be on the screen and the fallback was refused on the wire",
		err:  errors.Join(errWordsMayBeOnScreen, errStreamAckTimeout, fmt.Errorf("%w: %w", errFrameNotOnTheWire, broken)),
		want: false,
		why: "the fallback's failure is the one that went nowhere; the closing frame ahead of " +
			"it may be sealing the bubble right now, and handing that bubble back is how a " +
			"retry writes the same words into it a second time",
	}, {
		name: "a caller that ran out of time",
		err:  context.DeadlineExceeded,
		want: false,
		why:  "nothing here can say where the words got to",
	}, {
		name: "no failure at all",
		err:  nil,
		want: false,
		why:  "a delivery that landed has sealed the bubble itself",
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := bubbleSurvivedTheFailure(c.err); got != c.want {
				t.Fatalf("bubbleSurvivedTheFailure(%v) = %v, want %v — %s", c.err, got, c.want, c.why)
			}
		})
	}
}

// ---- and what that is worth end to end ----

// TestAnEndingTheSocketNeverTookIsSealedIntoTheSameBubble is the whole change,
// as the person in the chat experiences it.
//
// The round has a bubble. The socket dies, so the closing frame does not go and
// neither does the plain message behind it. The attempt this manager books comes
// back fifteen seconds later onto a connection that works — and what it has to
// write into is the question: a handle, or nothing.
//
// Before this it was nothing. The take had consumed the round, the retry found
// only a debt, and the words went out as an ordinary message underneath a
// spinner that would never stop. A req_id belongs to the turn rather than to the
// socket it arrived on (measured; senders_registry.go), so the bubble opened
// before the drop is sealable after it, and this is that seal: same stream,
// finish=true, on the new connection, with nothing said beside it.
func TestAnEndingTheSocketNeverTookIsSealedIntoTheSameBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.typing.endingRetryAfter = 20 * time.Millisecond
	rig.ran(t, "REQ-KEPT-BUBBLE", 1, "task-1")

	opened := rig.conn.streamFrames(t)
	if len(opened) != 1 || opened[0]["finish"] != false {
		t.Fatalf("the question opened %v, want one unfinished bubble", opened)
	}
	rig.conn.breakTheSocket()

	rig.cancelled(t, "task-1") // both routes fail on a socket that takes nothing
	next := rig.reconnect()    // the Supervisor comes back

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(said(t, next)) == 0 {
		time.Sleep(time.Millisecond)
	}
	if got := said(t, next); len(got) != 1 || got[0] != streamCopyCancelled {
		t.Fatalf("after the socket came back the asker read %q, want [%q]",
			got, streamCopyCancelled)
	}
	sealed := next.streamFrames(t)
	if len(sealed) != 1 || sealed[0]["id"] != opened[0]["id"] || sealed[0]["finish"] != true {
		t.Fatalf("the attempt that came back wrote %v, want one closing frame on stream %v — "+
			"nothing reached the peer on the attempt that failed, so the round still had the "+
			"handle that seals the spinner the asker is watching", sealed, opened[0]["id"])
	}
	if got := pushedTexts(t, next); len(got) != 0 {
		t.Fatalf("the words also went out as a new message (%q) — that is the answer arriving "+
			"beside the spinner instead of inside it", got)
	}
}

// TestTheGuardsOwnFailureLeavesTheRunOwedRatherThanTheBubble is the ending
// deliberately left out of the fork.
//
// The guard is the one publisher that books no retry: what it can be waiting
// behind is a settle, which is the round's real ending and books its own (see
// fireGuard). So a bubble handed back to the guard has nobody left to write into
// it, and the run would have given up the debt I3 files in exchange for that
// nobody. It fires at nine minutes besides, one minute short of the point where
// the server stops accepting frames for the stream at all.
func TestTheGuardsOwnFailureLeavesTheRunOwedRatherThanTheBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	sessionID := bubbleSessionID(t)
	rig.ran(t, "REQ-GUARD-DIES", 1, "task-1")
	rig.conn.breakTheSocket()

	rig.typing.fireGuard(context.Background(), sessionID, 1)

	if n := rig.streams.depth(); n != 0 {
		t.Fatalf("%d round(s) still open after the guard's promise reached nobody, want 0 — a "+
			"round handed back to the guard has no publisher left to seal it, and the debt "+
			"that would have reached the next one is gone", n)
	}
	if !rig.streams.owesEnding(sessionID, taskUUID(t, "task-1")) {
		t.Fatal("the run is owed nothing after the guard's promise reached nobody — its bubble " +
			"went with the take, so what the asker has is a spinner and the next publisher " +
			"finds no reason to say anything at all")
	}
}

// ---- the round a publisher put back is the round the next one finds ----

// TestThePublisherBehindAFailedDeliverySealsTheBubbleItLeft races the two
// publishers one run's ending can have, and asks what the second one is handed.
//
// The first takes the round and its delivery reaches nobody. The second is
// parked on that delivery — it must be, because until the first reports there is
// no fact about the screen to give it — and when it is released the round is
// back on the list with the bubble it had. So the second SEALS, where before it
// would have been handed a promise to speak beside.
//
// Driven at the ledger rather than through the bus because the window is the
// ledger's: what is under test is what one publisher hands the next under the
// lock they share, and a conn cannot hold a write that fails before it reaches
// the wire.
func TestThePublisherBehindAFailedDeliverySealsTheBubbleItLeft(t *testing.T) {
	t.Parallel()
	s := newStreamStore()
	sessionID := bubbleSessionID(t)
	taskID := taskUUID(t, "task-1")
	handle := streamHandle{ReqID: "REQ-RACE-BACK", StreamID: "STREAM-1", InstallationID: mustTestUUID(t), ChatID: "CHAT_1"}
	if v := s.open(sessionID, 1, handle); v != roundOpened {
		t.Fatalf("open = %v, want roundOpened", v)
	}
	s.bind(sessionID, 1, taskID)

	inFlight, release := make(chan struct{}), make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := s.sayEnding(context.Background(), sessionID, byTask(taskID), roundOver, nil,
			func(context.Context, roundTurn) (roundAddress, error) {
				close(inFlight)
				<-release
				return roundAddress{}, fmt.Errorf("%w: write: broken pipe", errFrameNotOnTheWire)
			})
		firstDone <- err
	}()
	<-inFlight

	var second roundTurn
	secondDone := make(chan roundVerdict, 1)
	go func() {
		verdict, err := s.sayEnding(context.Background(), sessionID, byTask(taskID), roundOver, nil,
			func(_ context.Context, turn roundTurn) (roundAddress, error) {
				second = turn
				return handle.address(), nil
			})
		if err != nil {
			t.Errorf("the publisher behind the failed delivery reported %v", err)
		}
		secondDone <- verdict
	}()

	// Long enough that a store which answered the second publisher outright
	// would have done so by now.
	time.Sleep(50 * time.Millisecond)
	close(release)

	if err := <-firstDone; !errors.Is(err, errFrameNotOnTheWire) {
		t.Fatalf("the first delivery reported %v, want the write that did not reach the wire", err)
	}
	if verdict := <-secondDone; verdict != roundOwesAnEnding {
		t.Fatalf("the publisher behind it was answered %v, want roundOwesAnEnding — the "+
			"delivery ahead of it put nothing on the screen", verdict)
	}
	if !second.HasBubble || second.Handle.StreamID != handle.StreamID {
		t.Fatalf("it was handed HasBubble=%v stream %q, want the round's own bubble (%q) — the "+
			"attempt ahead of it reached nobody, so the spinner is still there and this is the "+
			"publisher that can seal it", second.HasBubble, second.Handle.StreamID, handle.StreamID)
	}
	if n := s.depth(); n != 0 {
		t.Fatalf("%d bubble(s) still open after the second publisher sealed one, want 0", n)
	}
	if !s.wasTold(sessionID, taskID) {
		t.Fatal("the run is not recorded as told after its ending reached the screen")
	}
}

// TestARoundPutBackComesBackWithoutItsGuard is the timer the restore
// deliberately does not hand back.
//
// The take stops the guard, and a round that comes back is one whose run has
// already produced its ending — so the guard has nothing left to promise. Giving
// it the timer would let it seal the bubble at nine minutes with
// streamCopyStillWorking, which is false about a finished run and, worse,
// consumes the handle the restore is holding for the publisher that has the
// real words.
func TestARoundPutBackComesBackWithoutItsGuard(t *testing.T) {
	t.Parallel()
	s := newStreamStore()
	sessionID := bubbleSessionID(t)
	taskID := taskUUID(t, "task-1")
	if v := s.open(sessionID, 1, streamHandle{ReqID: "REQ-GUARD-BACK", StreamID: "STREAM-1", InstallationID: mustTestUUID(t)}); v != roundOpened {
		t.Fatalf("open = %v, want roundOpened", v)
	}
	s.bind(sessionID, 1, taskID)

	fired := make(chan struct{}, 1)
	s.arm(sessionID, 1, time.AfterFunc(60*time.Millisecond, func() { fired <- struct{}{} }))

	if _, err := s.sayEnding(context.Background(), sessionID, byTask(taskID), roundOver, nil,
		func(context.Context, roundTurn) (roundAddress, error) {
			return roundAddress{}, fmt.Errorf("%w: write: broken pipe", errFrameNotOnTheWire)
		}); err == nil {
		t.Fatal("the delivery reported success")
	}

	select {
	case <-fired:
		t.Fatal("the guard fired for a round whose run had already ended — its sentence promises " +
			"a reply that is still coming, and saying it consumes the bubble the next " +
			"publisher was going to put the real ending in")
	case <-time.After(250 * time.Millisecond):
	}
	if n := openRounds(s, sessionID); n != 1 {
		t.Errorf("%d round(s) on the open list after the failed ending, want the one that came "+
			"back", n)
	}
	if !s.knowsRound(sessionID, taskID) {
		t.Error("the round is not on file after being put back, so the failure gate would have " +
			"to prove from the database what this process just did itself")
	}
}

// TestTheGuardSaysNothingAboutARunTheUserStopped is the same refusal reached the
// way production reaches it, and the reason it cannot be left to the timer
// alone.
//
// The user cancels the run. The cancel notice finds a socket that takes nothing,
// so it reaches nobody and the round goes back with its bubble. The guard is
// then a goroutine already on its way here — Stop() lost the race, or the timer
// had fired before the cancel arrived — and it asks for the same round. Letting
// it speak seals the spinner with streamCopyStillWorking about a run the user
// themselves stopped, and consumes the bubble on the way.
func TestTheGuardSaysNothingAboutARunTheUserStopped(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	// No booked retry: what this test watches is the guard arriving on its own.
	rig.typing.endingRetryAfter = -1
	rig.ran(t, "REQ-CANCEL-GUARD", 1, "task-1")
	rig.conn.breakTheSocket()

	rig.cancelled(t, "task-1") // both routes fail; the round comes back
	next := rig.reconnect()    // and the socket the guard would write on works

	rig.typing.fireGuard(context.Background(), bubbleSessionID(t), 1)

	if frames := next.streamFrames(t); len(frames) != 0 {
		t.Fatalf("the guard wrote %v into the bubble of a run the user cancelled, want nothing — "+
			"the promise is false and the handle it spends is the one the cancellation's own "+
			"next attempt needs", frames)
	}
	if got := pushedTexts(t, next); len(got) != 0 {
		t.Fatalf("the guard pushed %q beside the bubble of a cancelled run, want nothing", got)
	}
	if n := openRounds(rig.streams, bubbleSessionID(t)); n != 1 {
		t.Fatalf("%d round(s) on the open list after the guard was refused, want the one it was "+
			"refused — the bubble is still the asker's and still writable", n)
	}
}

// TestARoundPutBackAfterItsEndingWasSaidIsNotSaidAgain is the hole the restore
// opened in the take.
//
// Finding a round used to be the whole of the right to take it, because a taken
// round never came back. Now one can, and it can come back to a run that has
// been spoken for in the meantime — this is that sequence, and every step of it
// is a path this store already had. The first publisher takes the round and its
// delivery goes quiet for longer than a reservation lives. The sweep retires it
// and leaves the debt. The second publisher spends the debt and its words reach
// the user, so the run is on told. Only then does the first report, naming a
// write that never left the wire — and the round goes back on the list under a
// run whose ending is already on somebody's screen.
//
// What must not happen next is the third publisher sealing that bubble with the
// same ending a second time.
func TestARoundPutBackAfterItsEndingWasSaidIsNotSaidAgain(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	s := newStreamStore()
	s.now = func() time.Time { return now }

	sessionID := bubbleSessionID(t)
	taskID := taskUUID(t, "task-1")
	handle := streamHandle{
		ReqID: "REQ-TOLD-BACK", StreamID: "STREAM-1",
		InstallationID: mustTestUUID(t), ChatID: "CHAT_1",
	}
	if v := s.open(sessionID, 1, handle); v != roundOpened {
		t.Fatalf("open = %v, want roundOpened", v)
	}
	s.bind(sessionID, 1, taskID)

	heldIn, releaseHeld := make(chan struct{}), make(chan struct{})
	held := make(chan struct{})
	go func() {
		defer close(held)
		s.sayEnding(context.Background(), sessionID, byTask(taskID), roundOver, nil,
			func(context.Context, roundTurn) (roundAddress, error) {
				close(heldIn)
				<-releaseHeld
				return roundAddress{}, fmt.Errorf("%w: write: broken pipe", errFrameNotOnTheWire)
			})
	}()
	<-heldIn
	now = now.Add(inFlightMaxAge + time.Second)

	// The second publisher sweeps the first's reservation away on its way in,
	// takes the debt that leaves behind, and delivers.
	if _, err := s.sayEnding(context.Background(), sessionID, byTask(taskID), roundOver, nil,
		func(context.Context, roundTurn) (roundAddress, error) { return handle.address(), nil }); err != nil {
		t.Fatalf("the publisher that spent the debt reported %v", err)
	}
	if !s.wasTold(sessionID, taskID) {
		t.Fatal("the run is not on told after its ending reached the user")
	}

	close(releaseHeld)
	<-held
	if n := openRounds(s, sessionID); n != 1 {
		t.Fatalf("%d round(s) on the open list, want the one the late holder put back — without "+
			"it this test proves nothing about what the next publisher finds", n)
	}

	spoke := false
	verdict, err := s.sayEnding(context.Background(), sessionID, byTask(taskID), roundOver, nil,
		func(context.Context, roundTurn) (roundAddress, error) {
			spoke = true
			return handle.address(), nil
		})
	if err != nil {
		t.Fatalf("the publisher after the restore reported %v", err)
	}
	if spoke {
		t.Fatal("a round whose ending was already said was taken and said again — the asker " +
			"reads the same ending twice, once beside the spinner and once sealed into it")
	}
	if verdict != roundToldAlready {
		t.Errorf("the publisher after the restore was answered %v, want roundToldAlready", verdict)
	}
}

// TestASweptRoundStillLeavesTheRunOwedItsEnding is what the round was standing
// in for while it was on the list.
//
// A failed ending used to file a debt on the note, which lives for roundMemory.
// The restore files no debt because the round itself is the better record — but
// only until the protocol's window runs out, which is six times sooner. Left
// there, a change made to keep a bubble would have quietly cut the ledger's
// memory of an undelivered ending from an hour to ten minutes, and a publisher
// arriving in between would find nothing owed and nothing on file. So the sweep
// hands the debt on as it takes the round away.
func TestASweptRoundStillLeavesTheRunOwedItsEnding(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	s := newStreamStore()
	s.now = func() time.Time { return now }

	sessionID := bubbleSessionID(t)
	taskID := taskUUID(t, "task-1")
	handle := streamHandle{
		ReqID: "REQ-SWEPT-DEBT", StreamID: "STREAM-1",
		InstallationID: mustTestUUID(t), ChatID: "CHAT_1",
	}
	if v := s.open(sessionID, 1, handle); v != roundOpened {
		t.Fatalf("open = %v, want roundOpened", v)
	}
	s.bind(sessionID, 1, taskID)
	if _, err := s.sayEnding(context.Background(), sessionID, byTask(taskID), roundOver, nil,
		func(context.Context, roundTurn) (roundAddress, error) {
			return roundAddress{}, fmt.Errorf("%w: write: broken pipe", errFrameNotOnTheWire)
		}); err == nil {
		t.Fatal("the delivery reported success")
	}
	if s.owesEnding(sessionID, taskID) {
		t.Fatal("a debt was filed while the round was still on the list — the round is the record " +
			"there, and a claim against the debt would hand a second publisher the promise")
	}

	// A publisher arriving past the window: the sweep runs in front of it, the
	// round it would have sealed is gone, and what it is handed is what the
	// round was standing in for.
	now = now.Add(streamMaxAge + time.Minute)
	var late roundTurn
	verdict, err := s.sayEnding(context.Background(), sessionID, byTask(taskID), roundOver, nil,
		func(_ context.Context, got roundTurn) (roundAddress, error) {
			late = got
			return handle.address(), nil
		})
	if err != nil {
		t.Fatalf("the late publisher reported %v", err)
	}
	if verdict != roundOwesAnEnding || !late.Promised {
		t.Fatalf("the late publisher was answered %v (promised=%v), want roundOwesAnEnding with "+
			"the debt the swept round left — without it a cancel or a settle finds no reason to "+
			"speak and no address to speak in, and the ending is dropped in silence",
			verdict, late.Promised)
	}
	if late.HasBubble {
		t.Error("a handle past the protocol's window was handed out; the server refuses that frame")
	}
	if !late.Addr.known() {
		t.Error("the late publisher was given no address, so it has nowhere to say the ending")
	}
	if n := s.depth(); n != 0 {
		t.Errorf("%d bubble(s) survived the protocol's window, want 0", n)
	}
}

// TestASweptRoundCannotBePaintedASecondBubble is the protection the restore used
// to give up.
//
// The take files the batch as finished, which is the one thing standing between
// a badly delayed OnIngested and a second bubble for a run that has already
// answered. Unfiling it on the way back was safe only while the round stayed on
// the list to be found instead; once the sweep took the round, nothing was left
// and a late paint opened a spinner nothing would ever close. So the take's
// filing is permanent, and bind — the one caller that needed the unfile — asks
// the round before it asks the list.
func TestASweptRoundCannotBePaintedASecondBubble(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	s := newStreamStore()
	s.now = func() time.Time { return now }

	sessionID := bubbleSessionID(t)
	taskID := taskUUID(t, "task-1")
	handle := streamHandle{
		ReqID: "REQ-SWEPT-PAINT", StreamID: "STREAM-1",
		InstallationID: mustTestUUID(t), ChatID: "CHAT_1",
	}
	if v := s.open(sessionID, 1, handle); v != roundOpened {
		t.Fatalf("open = %v, want roundOpened", v)
	}
	s.bind(sessionID, 1, taskID)
	if _, err := s.sayEnding(context.Background(), sessionID, byTask(taskID), roundOver, nil,
		func(context.Context, roundTurn) (roundAddress, error) {
			return roundAddress{}, fmt.Errorf("%w: write: broken pipe", errFrameNotOnTheWire)
		}); err == nil {
		t.Fatal("the delivery reported success")
	}

	now = now.Add(streamMaxAge + time.Minute)
	late := streamHandle{ReqID: "REQ-SWEPT-PAINT", StreamID: "STREAM-2", InstallationID: mustTestUUID(t), ChatID: "CHAT_1"}
	if v := s.open(sessionID, 1, late); v != roundFinished {
		t.Fatalf("a badly delayed ingest for a swept round was answered %v, want roundFinished — "+
			"this run has had its closer, and the bubble this would paint is one no ending is "+
			"left to close", v)
	}
}

// TestARoundPutBackDoesNotQueueTheNextOneBehindIt is what the open list says
// about a round whose run is over.
//
// QueuedBehind is how an empty answer picks its copy: a round that waited in
// line behind another closes with streamCopyMerged, because the reply ahead of
// it covered the message. A round put back is not ahead of anything —
// its run finished, and its ending is on the list precisely because nobody read
// it. Counting it tells the next asker their message was folded into a reply
// that was never delivered.
func TestARoundPutBackDoesNotQueueTheNextOneBehindIt(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	// No booked retry: the first round has to still be on the list when the
	// second one opens, which is the state under test.
	rig.typing.endingRetryAfter = -1
	rig.ran(t, "REQ-QB-1", 1, "task-1")
	rig.conn.breakTheSocket()

	rig.cancelled(t, "task-1") // the notice reaches nobody; round 1 comes back
	next := rig.reconnect()

	rig.ran(t, "REQ-QB-2", 2, "task-2")
	rig.answer(t, "", "task-2")

	frames := next.streamFrames(t)
	if len(frames) != 2 {
		t.Fatalf("the second round wrote %d frames, want 2 (its open and its seal): %v", len(frames), frames)
	}
	if frames[1]["content"] != streamCopyNoReply {
		t.Fatalf("the second round's empty answer closed with %q, want %q — the round ahead of "+
			"it never delivered anything, so there is no reply above for this message to have "+
			"been folded into", frames[1]["content"], streamCopyNoReply)
	}
}

// TestARoundPutBackCanStillLearnItsRunsName is the bookkeeping the take files on
// its way out, and the one caller of it the restore has to get past.
//
// The take records the batch as finished, which is what stops a badly delayed
// OnIngested from painting a second bubble for a run that has already answered.
// It also used to stop bind: a round whose flush had not reported yet would
// never learn which run it belongs to, and every ending after that names a task
// this store cannot match to a bubble. bind asks the open list first for exactly
// this round — the finished list is not unfiled for it, because the round is
// back holding the bubble that list is protecting.
func TestARoundPutBackCanStillLearnItsRunsName(t *testing.T) {
	t.Parallel()
	s := newStreamStore()
	sessionID := bubbleSessionID(t)
	batch := engine.RunBatchID(1)
	handle := streamHandle{ReqID: "REQ-LATE-FLUSH", StreamID: "STREAM-1", InstallationID: mustTestUUID(t)}
	if v := s.open(sessionID, batch, handle); v != roundOpened {
		t.Fatalf("open = %v, want roundOpened", v)
	}

	// The settled flush names no run, so this round is reached by batch.
	if _, err := s.sayEnding(context.Background(), sessionID, byBatch(batch), roundOver, nil,
		func(context.Context, roundTurn) (roundAddress, error) {
			return roundAddress{}, fmt.Errorf("%w: write: broken pipe", errFrameNotOnTheWire)
		}); err == nil {
		t.Fatal("the delivery reported success")
	}

	taskID := taskUUID(t, "task-1")
	s.bind(sessionID, batch, taskID)
	if !s.knowsRound(sessionID, taskID) {
		t.Fatal("the round that came back never learnt its run's name — the take filed its " +
			"batch as finished and bind refuses a finished batch, so every ending after this " +
			"one names a task the store cannot match to the bubble it is holding")
	}
	if v := s.open(sessionID, batch, handle); v != roundFinished {
		t.Errorf("a late ingest for the round that came back was answered %v, want roundFinished "+
			"— the finished list answers first and it is not unfiled, which is what keeps the "+
			"protection alive after the sweep takes the round; either way nothing is painted, "+
			"and the bubble on screen is the one this round is already holding", v)
	}
}

// TestARoundWithNoBubbleIsNotPutBack is the other half of the entry gate.
//
// A round on file with no bubble — its opening frame refused, or its ingest
// goroutine still behind the flush that named it — has nothing anyone could
// write to. So there is nothing to hand back, and the run is owed its ending
// like any other failure. Handing it back instead would put a round on the list
// whose every future take reports no bubble, and give up the debt that is what
// makes the next publisher speak at all.
func TestARoundWithNoBubbleIsNotPutBack(t *testing.T) {
	t.Parallel()
	s := newStreamStore()
	sessionID := bubbleSessionID(t)

	// A first round, painted and answered. That is what leaves the session's
	// address on the note, which is the address the debt below is worth filing
	// against.
	first := streamHandle{ReqID: "REQ-FIRST", StreamID: "STREAM-1", InstallationID: mustTestUUID(t), ChatID: "CHAT_1"}
	if v := s.open(sessionID, 1, first); v != roundOpened {
		t.Fatalf("open = %v, want roundOpened", v)
	}
	s.bind(sessionID, 1, taskUUID(t, "task-1"))
	if _, err := s.sayEnding(context.Background(), sessionID, byTask(taskUUID(t, "task-1")), roundOver, nil,
		func(context.Context, roundTurn) (roundAddress, error) { return first.address(), nil }); err != nil {
		t.Fatalf("the first round's ending: %v", err)
	}

	// The second round's flush names its run before the ingest goroutine has
	// painted anything, so it is on file with no bubble at all.
	taskID := taskUUID(t, "task-2")
	s.bind(sessionID, 2, taskID)

	var turn roundTurn
	if _, err := s.sayEnding(context.Background(), sessionID, byTask(taskID), roundOver, nil,
		func(_ context.Context, got roundTurn) (roundAddress, error) {
			turn = got
			return roundAddress{}, fmt.Errorf("%w: write: broken pipe", errFrameNotOnTheWire)
		}); err == nil {
		t.Fatal("the delivery reported success")
	}
	if turn.HasBubble {
		t.Fatal("a round that never painted anything handed out a bubble")
	}
	if n := openRounds(s, sessionID); n != 0 {
		t.Fatalf("%d round(s) went back on the list, want 0 — there was no bubble to keep", n)
	}
	if !s.owesEnding(sessionID, taskID) {
		t.Fatal("the run is owed nothing, and no round is on file for it either — the next " +
			"publisher has nothing to say and nowhere this store would tell it to say it")
	}
}

// openRounds is how many rounds a session still has on the open list, painted
// or not. depth() is the wrong question for these tests: it counts bubbles
// across every session and screens on painted, and what is being asked here is
// whether one particular round came back.
func openRounds(s *streamStore, sessionID pgtype.UUID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions[util.UUIDToString(sessionID)])
}

// TestARunThisStoreNeverHeldIsStillLeftAlone is the boundary the origin gate
// rests on, restated against the new fork.
//
// The session is this adapter's and its address is on the note, put there by a
// round that really was asked in the room. The run below is not that round: it
// is one this store holds nothing for, which is what a question typed into the
// installer's own browser against the same session looks like from here.
//
// A failed delivery for it writes nothing at all. Anything filed would be read
// by knowsRound as proof the question came from the room, and would hand the
// failure gate a permission the database was supposed to decide. Putting a round
// back cannot widen that — there is no round — but only a test says so.
func TestARunThisStoreNeverHeldIsStillLeftAlone(t *testing.T) {
	t.Parallel()
	s := newStreamStore()
	sessionID := bubbleSessionID(t)

	asked := streamHandle{ReqID: "REQ-ASKED-HERE", StreamID: "STREAM-1", InstallationID: mustTestUUID(t), ChatID: "CHAT_1"}
	if v := s.open(sessionID, 1, asked); v != roundOpened {
		t.Fatalf("open = %v, want roundOpened", v)
	}
	s.bind(sessionID, 1, taskUUID(t, "task-1"))
	if _, err := s.sayEnding(context.Background(), sessionID, byTask(taskUUID(t, "task-1")), roundOver, nil,
		func(context.Context, roundTurn) (roundAddress, error) { return asked.address(), nil }); err != nil {
		t.Fatalf("the room's own round: %v", err)
	}

	elsewhere := taskUUID(t, "task-2")
	if _, err := s.sayEnding(context.Background(), sessionID, byTask(elsewhere), roundOver, nil,
		func(context.Context, roundTurn) (roundAddress, error) {
			return roundAddress{}, fmt.Errorf("%w: write: broken pipe", errFrameNotOnTheWire)
		}); err == nil {
		t.Fatal("the delivery reported success")
	}
	if s.knowsRound(sessionID, elsewhere) {
		t.Fatalf("a run this store held no round for is on file after its delivery failed — "+
			"knowsRound reads that as proof the question was asked in the WeCom room, and "+
			"hands the failure gate a permission the database was supposed to decide "+
			"(session %s)", util.UUIDToString(sessionID))
	}
	if n := openRounds(s, sessionID); n != 0 {
		t.Fatalf("%d round(s) on the open list for a run this store never held", n)
	}
}
