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

// TestARoundPutBackKeepsTheGuardItHadLeft is the timer the restore has to hand
// back with the round.
//
// The take stops the guard, because a round that has left the list has nothing
// to guard. Putting the round back without it leaves a bubble with no closer of
// last resort: if the attempt that comes next never happens, the spinner runs
// past the protocol's window and the server stops accepting frames for it at
// all. Re-arming it for a fresh nine minutes is the same failure with extra
// steps — the window runs from the OPENING frame, not from whenever an ending
// was last attempted — so what goes back on is what was left of the deadline
// this timer already had.
func TestARoundPutBackKeepsTheGuardItHadLeft(t *testing.T) {
	t.Parallel()
	s := newStreamStore()
	sessionID := bubbleSessionID(t)
	taskID := taskUUID(t, "task-1")
	if v := s.open(sessionID, 1, streamHandle{ReqID: "REQ-GUARD-BACK", StreamID: "STREAM-1", InstallationID: mustTestUUID(t)}); v != roundOpened {
		t.Fatalf("open = %v, want roundOpened", v)
	}
	s.bind(sessionID, 1, taskID)

	fired := make(chan struct{}, 1)
	due := time.Now().Add(150 * time.Millisecond)
	s.arm(sessionID, 1, time.AfterFunc(time.Until(due), func() { fired <- struct{}{} }), due)

	if _, err := s.sayEnding(context.Background(), sessionID, byTask(taskID), roundOver, nil,
		func(context.Context, roundTurn) (roundAddress, error) {
			return roundAddress{}, fmt.Errorf("%w: write: broken pipe", errFrameNotOnTheWire)
		}); err == nil {
		t.Fatal("the delivery reported success")
	}

	select {
	case <-fired:
	case <-time.After(3 * time.Second):
		t.Fatal("the guard never fired for a round that came back — it was stopped by the take, " +
			"so a restore that drops it leaves the bubble with nothing to close it, and one " +
			"that re-arms it for a fresh nine minutes fires past the window entirely")
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

// TestARoundPutBackCanStillLearnItsRunsName is the bookkeeping the take files on
// its way out and the restore has to unfile.
//
// The take records the batch as finished, which is what stops a badly delayed
// OnIngested from painting a second bubble for a run that has already answered.
// It also stops bind: a round whose flush has not reported yet would never learn
// which run it belongs to, and every ending after that names a task this store
// cannot match to a bubble.
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
	if v := s.open(sessionID, batch, handle); v != roundJoined {
		t.Errorf("a late ingest for the round that came back was answered %v, want roundJoined "+
			"— its bubble is on screen and this message is not painting a second one", v)
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
