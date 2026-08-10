package wecom

// stream_ledger_test.go — the ledger's invariants, held shut from the outside.
//
// Five review rounds have found five different ways to break the same promise,
// and every one of them was a scenario nobody had written a test for. So the
// table here is not a scenario: it enumerates every terminal path a round has
// and asserts the properties the ledger claims for all of them at once.
//
// The fifth way was the one that shape could not reach. It takes two publishers
// of one ending overlapping, which no sequential walk of the paths produces, and
// the tests for it are at the bottom of the file — one publisher held on the
// wire with its words unanswered, and a second deciding what to do about them.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---- the reconnect window ----

// TestAPromiseIsNotKeptUntilTheWordsAreAccepted is the round-4 regression,
// reproduced exactly as the review describes it: guard-close the round, clear
// the registered sender, deliver an empty completion (which returns
// errNoLiveConnection), reconnect, replay the completion.
//
// The guard told the user "还在处理，完成后我再单独回复你". The empty completion
// is the separate reply it promised — there is nothing to add, so the words are
// the no-reply copy — and a send refused because the WebSocket happened to be
// mid-reconnect must not count as having said them. WeCom redelivers, the
// sweeper repeats, and either way the next attempt is the last chance the
// promise has; a ledger that recorded the first attempt as delivered spends it
// on nothing and the asker waits for a reply that has already been filed as
// sent.
func TestAPromiseIsNotKeptUntilTheWordsAreAccepted(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-1", 1, "task-1")

	// Nine minutes pass with the run still going: the guard takes the bubble
	// and leaves the promise behind.
	rig.guardClosed(t, 1)

	// The socket drops. Nothing else about the process changes — the store is
	// built at boot, not per connection.
	rig.senders.clear(rig.instID, rig.conn.sender)

	// The run finishes with nothing to say, into a registry with no sender.
	if err := rig.answerErr(t, "", "task-1"); err == nil {
		t.Fatal("delivering into an empty sender registry returned no error; " +
			"the reconnect window this test is about did not happen")
	}

	// The Supervisor reconnects and the same completion is redelivered.
	conn := rig.reconnect()
	if err := rig.answerErr(t, "", "task-1"); err != nil {
		t.Fatalf("replayed completion after the reconnect: %v", err)
	}

	got := pushedTexts(t, conn)
	if len(got) != 1 {
		t.Fatalf("the guard promised a separate reply and the user got %d message(s), want 1 — "+
			"the promise was recorded as kept by a send that was refused, so the replay found "+
			"nothing owed and stayed silent: %q", len(got), got)
	}
	if got[0] != streamCopyNoReply {
		t.Fatalf("the promised reply said %q, want %q", got[0], streamCopyNoReply)
	}
}

// answerErr is answer without the fatal: the reconnect-window tests are about
// what a refused send does to the ledger, so the error is the subject.
func (r *bubbleRig) answerErr(t *testing.T, content, taskName string) error {
	t.Helper()
	return r.out.processEvent(context.Background(), events.Event{
		ChatSessionID: bubbleSession,
		TaskID:        taskUUID(t, taskName),
		Payload:       protocol.ChatDonePayload{Content: content},
	})
}

// ---- the invariants, over every terminal path ----

// terminalPath is one way a round can end. Between them these are all of them:
// the two subscribers the manager registers, the chat-done subscriber, and each
// of those against a round that still has its bubble and a round the guard has
// already closed on a promise.
type terminalPath struct {
	name string
	// afterTheGuard runs the nine-minute guard on this round first, so the
	// ending arrives to a promise rather than a bubble.
	afterTheGuard bool
	// fire publishes the ending for the round bound to task-1, the way its
	// real publisher does.
	fire func(t *testing.T, rig *bubbleRig)
	// want is what the person in the chat has to end up reading.
	want string
}

func terminalPaths() []terminalPath {
	answer := func(content string) func(*testing.T, *bubbleRig) {
		return func(t *testing.T, rig *bubbleRig) {
			t.Helper()
			_ = rig.answerErr(t, content, "task-1")
		}
	}
	failed := func(t *testing.T, rig *bubbleRig) { t.Helper(); rig.failed(t, "task-1", false) }
	cancelled := func(t *testing.T, rig *bubbleRig) { t.Helper(); rig.cancelled(t, "task-1") }

	return []terminalPath{
		{name: "an answer", fire: answer("the answer"), want: "the answer"},
		{name: "an empty answer", fire: answer(""), want: streamCopyNoReply},
		{name: "a failure", fire: failed, want: streamCopyFailed},
		{name: "a cancellation", fire: cancelled, want: streamCopyCancelled},
		{name: "an answer after the guard", afterTheGuard: true, fire: answer("the answer"), want: "the answer"},
		{name: "an empty answer after the guard", afterTheGuard: true, fire: answer(""), want: streamCopyNoReply},
		{name: "a failure after the guard", afterTheGuard: true, fire: failed, want: streamCopyFailed},
		{name: "a cancellation after the guard", afterTheGuard: true, fire: cancelled, want: streamCopyCancelled},
	}
}

// controlRound is a SECOND run of the same session, set up alongside the one
// under test and never touched by it. The two shapes are the two ways a run can
// be reached once its ending arrives, and each is what a different earlier
// defect took something from.
type controlRound struct {
	name string
	// setUp arranges the control round as task-2.
	setUp func(t *testing.T, rig *bubbleRig)
}

func controlRounds() []controlRound {
	return []controlRound{
		{
			// A promise outstanding, sitting at the HEAD of the owed list — the
			// entry a claim that spends the head instead of its own takes.
			name: "a round still owed the guard's promise",
			setUp: func(t *testing.T, rig *bubbleRig) {
				t.Helper()
				rig.ran(t, "REQ-2", 2, "task-2")
				rig.guardClosed(t, 2)
			},
		},
		{
			// No bubble, no promise, nothing on file: this run's ending has to
			// find its chat in the binding row. It is the one a dedup keyed by
			// "this session has a note" silences, because by the time it
			// arrives the round under test has left a note of its own.
			name:  "a round this process holds nothing for",
			setUp: func(t *testing.T, rig *bubbleRig) { t.Helper() },
		},
	}
}

// setUpTwoRounds gives a session the round under test, bound to task-1, plus a
// control round on task-2 that nothing the tested path does is allowed to
// touch: not spend its promise, not file an ending in its name, not leave a
// note that silences it.
func setUpTwoRounds(t *testing.T, p terminalPath, c controlRound) *bubbleRig {
	t.Helper()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")
	rig.askedInTheRoom(t, "task-2")
	rig.ran(t, "REQ-1", 1, "task-1")
	c.setUp(t, rig)
	if p.afterTheGuard {
		rig.guardClosed(t, 1)
	}
	return rig
}

// TestEveryTerminalPathEndsInWordsOrLeavesTheRunOwed is the ledger's contract,
// asserted as a property of all of its paths rather than as a story about one.
//
// The first four rounds of review found four ways to break the same promise, and
// each was fixed with a scenario test that could not have caught the next one:
// a FIFO claim, a delivery path that settled nothing, dedup keyed by session,
// a claim recorded before its send. All four are instances of two properties
// this table checks on every path at once — what the user ends up reading, and
// what a run that was never spoken for is still owed.
func TestEveryTerminalPathEndsInWordsOrLeavesTheRunOwed(t *testing.T) {
	t.Parallel()
	for _, p := range terminalPaths() {
		// I1 and the not-twice rule: the words go out once, and a second
		// publisher of the same ending adds nothing. task:failed has two
		// publishers and a sweeper tick can repeat either, and WeCom redelivers
		// callbacks after a reconnect, so every one of these arrives twice in
		// production sooner or later.
		t.Run(p.name+"/says it once", func(t *testing.T) {
			t.Parallel()
			rig := setUpTwoRounds(t, p, controlRounds()[0])
			before := len(said(t, rig.conn))

			p.fire(t, rig)
			if got := said(t, rig.conn)[before:]; len(got) != 1 || got[0] != p.want {
				t.Fatalf("%s left the user reading %q, want [%q]", p.name, got, p.want)
			}
			p.fire(t, rig)
			if got := said(t, rig.conn)[before:]; len(got) != 1 {
				t.Fatalf("%s republished brought the user to %q, want one message — "+
					"the second publisher of one run's ending told them twice", p.name, got)
			}
		})

		// The other half of the not-twice rule, and the one a replay cannot
		// reach: once a run's ending has been said, no OTHER ending for that
		// same run may speak. A run has several publishers with different
		// copy — an answer, a failure, a cancel — and the promise a
		// guard-closed round left is spent by whichever of them arrives; a
		// path that delivers without settling leaves it on the list for the
		// next one, which then contradicts what the user has just read.
		t.Run(p.name+"/nothing else speaks for the run afterwards", func(t *testing.T) {
			t.Parallel()
			rig := setUpTwoRounds(t, p, controlRounds()[0])
			p.fire(t, rig)
			before := len(said(t, rig.conn))

			rig.failed(t, "task-1", false)
			rig.cancelled(t, "task-1")
			if got := said(t, rig.conn)[before:]; len(got) != 0 {
				t.Fatalf("after %s the same run's other endings added %q — "+
					"the user reads a second, contradicting account of one run", p.name, got)
			}
		})

		// I3: a delivery nothing accepted is not a delivery. The ledger has to
		// come back unchanged, so the run is still owed its ending and the next
		// publisher says it. This is the shape of the reconnect window — the
		// registry is momentarily empty while the Supervisor redials — and it
		// is the only window in which every one of these paths is silent.
		t.Run(p.name+"/a refused delivery is not a delivery", func(t *testing.T) {
			t.Parallel()
			rig := setUpTwoRounds(t, p, controlRounds()[0])
			before := len(said(t, rig.conn))

			rig.senders.clear(rig.instID, rig.conn.sender)
			p.fire(t, rig)
			if got := said(t, rig.conn)[before:]; len(got) != 0 {
				t.Fatalf("%s wrote %q with no live connection", p.name, got)
			}

			conn := rig.reconnect()
			p.fire(t, rig)
			if got := said(t, conn); len(got) != 1 || got[0] != p.want {
				t.Fatalf("after a refused %s the user ended up reading %q, want [%q] — "+
					"the ending was recorded as said by a send nothing accepted, so the "+
					"next publisher of it found the run already spoken for", p.name, got, p.want)
			}
		})

		// I2: matched by the run's own id and by nothing else. A second run of
		// the same session is what every earlier defect took something from —
		// the head of the owed list a FIFO claim spends, the promise a path
		// that settles nothing leaves lying about, the session note a dedup
		// keyed by session reads as this run's own. So each path is run against
		// both shapes a second run can be in, and neither is allowed to lose
		// its own ending to the first round's.
		for _, c := range controlRounds() {
			t.Run(p.name+"/leaves "+c.name+" alone", func(t *testing.T) {
				t.Parallel()
				rig := setUpTwoRounds(t, p, c)
				p.fire(t, rig)
				before := len(said(t, rig.conn))

				rig.failed(t, "task-2", false)
				if got := said(t, rig.conn)[before:]; len(got) != 1 || got[0] != streamCopyFailed {
					t.Fatalf("after %s on the first round, %s was left reading %q for its own "+
						"failure, want [%q] — the first round's ending is what became of it",
						p.name, c.name, got, streamCopyFailed)
				}
			})
		}
	}
}

// said is everything the person in the chat ended up reading on a connection:
// the text of every sealed bubble and every plain message, in write order.
//
// Both, deliberately. A closing frame and a push are the same thing to the
// reader, and they are how the same words reach them depending on whether the
// bubble survived — so a ledger test watching only one of them would call a
// path silent while its words were on the screen, or count one ending twice.
// The opening frame is not in here: it carries no words, only the spinner.
func said(t *testing.T, c *bubbleConn) []string {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []string{}
	for _, f := range c.frames {
		var body map[string]any
		if err := json.Unmarshal(f.Body, &body); err != nil {
			t.Fatalf("decode frame body: %v", err)
		}
		switch f.Cmd {
		case cmdRespondMsg:
			stream, _ := body["stream"].(map[string]any)
			if stream == nil || stream["finish"] != true {
				continue
			}
			s, _ := stream["content"].(string)
			out = append(out, s)
		case cmdSendMsg:
			md, _ := body["markdown"].(map[string]any)
			if md == nil {
				continue
			}
			s, _ := md["content"].(string)
			out = append(out, s)
		}
	}
	return out
}

// ---- the terminal path with no second publisher of its own ----

// TestASettledRoundThatCouldNotBeToldIsStillOwedItsWords is the reconnect
// window on the one path that has no natural second chance.
//
// A flush that started no run — agent offline, archived, an enqueue that failed
// — is closed by OnSettled and by nothing else: with no task there is no task
// lifecycle event, so neither the chat-done subscriber nor the failure
// subscriber will ever fire for it. If the closing frame and the plain message
// both fail in a reconnect window, the round has already left the open list and
// its bubble has gone with the attempt. A ledger that recorded nothing for it
// leaves the asker a spinner that nothing can ever seal and no words at all —
// the one loss I3 exists to make impossible, on the one path where nothing else
// can recover from it.
//
// The retry is switched off here on purpose: what this pins is that the ROUND
// is still owed its ending afterwards, so whoever says it next says it, rather
// than that a timer happens to be the one who does.
func TestASettledRoundThatCouldNotBeToldIsStillOwedItsWords(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.typing.settleRetryAfter = -1
	sessionID := bubbleSessionID(t)
	rig.ask(t, "REQ-SETTLE", 1) // a bubble, and a flush that never named a run

	// The socket drops, and the flush settles into it: the closing frame is
	// refused and so is the plain message it falls back to.
	rig.senders.clear(rig.instID, rig.conn.sender)
	rig.typing.OnSettled(context.Background(), sessionID, 1)

	// The Supervisor reconnects and the same settle is published again.
	conn := rig.reconnect()
	rig.typing.OnSettled(context.Background(), sessionID, 1)

	got := pushedTexts(t, conn)
	if len(got) != 1 || got[0] != streamCopyNotStarted {
		t.Fatalf("after a settle nothing accepted, the asker ended up reading %q, want [%q] — "+
			"a round that never became a run has no later lifecycle event to rescue it, so a "+
			"replay that finds nothing owed leaves them watching a spinner for good",
			got, streamCopyNotStarted)
	}

	// And having been said, it is said once: a third publisher adds nothing.
	rig.typing.OnSettled(context.Background(), sessionID, 1)
	if got := pushedTexts(t, conn); len(got) != 1 {
		t.Fatalf("a repeated settle brought the asker to %q, want one message", got)
	}
}

// TestASettleNobodyHeardIsSaidAgainWithoutAnotherPublisher is the other half of
// the same fix, and the reason the ledger entry is worth having.
//
// Exactly one of OnRunStarted and OnSettled fires per batch, so unlike every
// other ending this one has no second publisher in production: no sweeper tick
// repeats it, no redelivery reaches it, and there is no task for a lifecycle
// event to hang off. The debt the ledger now files would sit there unspent. So
// the manager books the publisher itself, and it goes back through the same
// sayEnding — which is what makes it silent if anything else got there first.
func TestASettleNobodyHeardIsSaidAgainWithoutAnotherPublisher(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.typing.settleRetryAfter = 20 * time.Millisecond
	sessionID := bubbleSessionID(t)
	rig.ask(t, "REQ-SETTLE-RETRY", 1)

	rig.senders.clear(rig.instID, rig.conn.sender)
	rig.typing.OnSettled(context.Background(), sessionID, 1)
	conn := rig.reconnect()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(pushedTexts(t, conn)) == 0 {
		time.Sleep(time.Millisecond)
	}
	got := pushedTexts(t, conn)
	if len(got) != 1 || got[0] != streamCopyNotStarted {
		t.Fatalf("the asker read %q after the socket came back, want [%q] — nothing else ever "+
			"publishes this round's ending, so an attempt that failed is the last of it unless "+
			"this path books its own", got, streamCopyNotStarted)
	}
}

// ---- two publishers of one run's ending, with nothing on file ----

// publishFailure is failed() without the testing handle: the two-publisher tests
// run it on goroutines of their own, and t is not theirs to touch.
func (r *bubbleRig) publishFailure(taskID string) {
	r.bus.Publish(events.Event{
		Type:          protocol.EventTaskFailed,
		ChatSessionID: bubbleSession,
		TaskID:        taskID,
		Payload: map[string]any{
			"task_id":        taskID,
			"failure_reason": "provider_network",
			"retry_pending":  false,
		},
	})
}

// waitForRepeatAtTheLedger blocks until a second publisher has reached the
// ledger for a run this store DOES hold a round or a promise for.
//
// The first publisher of such a run never asks the origin gate: knowsRound
// answers off the open list or the owed list and no row is read. By the time the
// repeat arrives the first one has taken both — the round is off the list and
// its promise is in speaking — so the repeat falls through to the row, and that
// read is the signal. One read means the repeat is past the gate with nothing
// left to do but reach the store.
func (r *bubbleRig) waitForRepeatAtTheLedger(t *testing.T) {
	t.Helper()
	r.waitForOriginReads(t, 1)
}

// waitForOriginReads blocks until the origin gate has been asked n times — the
// last thing a publisher does before it reaches the ledger. It is what lets a
// test say "the repeat has arrived" without watching a clock.
func (r *bubbleRig) waitForOriginReads(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(r.q.originAsked()) >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("only %d publisher(s) reached the origin gate, want %d", len(r.q.originAsked()), n)
}

// letTheRepeatSpeak is the window in which the wrong behaviour would show
// itself: the repeat is past the origin gate, and a store that reserved nothing
// would have it writing its own copy of the notice about now. Waiting it out
// only lengthens the passing path.
func letTheRepeatSpeak() { time.Sleep(200 * time.Millisecond) }

// TestASecondPublisherOfOneFailureWaitsForTheFirst is the fix.
//
// task:failed has two publishers and the bus is synchronous, so a repeat can
// reach the store while the first notice's words are still on the wire. For a
// run this process holds no round for — a restart mid-run, a round whose opening
// frame was refused — the ledger has no note to exclude it with, deliberately:
// anything filed there would be read by knowsRound as proof the question was
// asked in the room. Without a reservation of some other kind, both publishers
// resolve the chat off the binding row and both speak, and the room reads
// "⚠️ 这次没跑通" twice for one run.
func TestASecondPublisherOfOneFailureWaitsForTheFirst(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")
	taskID := taskUUID(t, "task-1")

	// The first notice is held on the wire, unanswered, for as long as the test
	// wants it there.
	rig.conn.holdPush = make(chan struct{})
	rig.conn.pushSent = make(chan struct{}, 4)

	first := make(chan struct{})
	go func() { defer close(first); rig.publishFailure(taskID) }()
	<-rig.conn.pushSent

	second := make(chan struct{})
	go func() { defer close(second); rig.publishFailure(taskID) }()
	rig.waitForOriginReads(t, 2)
	letTheRepeatSpeak()

	close(rig.conn.holdPush)
	<-first
	<-second

	got := pushedTexts(t, rig.conn)
	if len(got) != 1 || got[0] != streamCopyFailed {
		t.Fatalf("one run's failure left the room reading %q, want [%q] — its two publishers "+
			"raced inside the window before the first delivery reported, and both spoke",
			got, streamCopyFailed)
	}
	// And the reservation that made that so is not evidence of anything: a run
	// this store held no round for is still not proof the question was asked in
	// the room.
	if rig.streams.knowsRound(bubbleSessionID(t), taskID) {
		t.Fatal("an ending this store merely tried to say is being read as proof the question " +
			"came from the room — the origin gate would then wave a browser run's failure into " +
			"the chat with no database check at all")
	}
}

// TestARefusedFailureNoticeIsStillTheRepeatsToSay is the other ordering, and
// the direction that costs more to get wrong.
//
// It is a guard rather than a regression: it holds on both sides of the fix, and
// has to, because what it forbids is the shortest way to write the fix wrong.
// The exclusion must not become "already said". Nothing has been said until a
// delivery reports that it was, so a repeat parked on a first attempt that then
// failed is the retry that news has left — silencing it is defect 4 in another
// coat, a promise recorded as kept by a send nothing accepted.
//
// The held push is what puts the repeat on the reservation rather than behind
// the first one in the sender's write queue: it is waiting on the outcome, and
// the outcome it gets is a refusal.
func TestARefusedFailureNoticeIsStillTheRepeatsToSay(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")
	taskID := taskUUID(t, "task-1")

	rig.conn.refusePushes = 1 // the first notice is refused; a later one is not
	rig.conn.holdPush = make(chan struct{})
	rig.conn.pushSent = make(chan struct{}, 4)

	first := make(chan struct{})
	go func() { defer close(first); rig.publishFailure(taskID) }()
	<-rig.conn.pushSent

	second := make(chan struct{})
	go func() { defer close(second); rig.publishFailure(taskID) }()
	rig.waitForOriginReads(t, 2)
	letTheRepeatSpeak()

	close(rig.conn.holdPush)
	<-first
	<-second

	got := pushedTexts(t, rig.conn)
	if len(got) != 2 || got[1] != streamCopyFailed {
		t.Fatalf("after the first notice was refused the room ended up reading %q, want the "+
			"repeat to have said %q — a delivery nothing accepted is not a delivery, and "+
			"treating the repeat as told swallows the only retry this news had",
			got, streamCopyFailed)
	}
}

// ---- two publishers of one run's ending, with a round on file ----

// The four tests below are the same race against a round this store DOES hold
// something for, which is where the exclusion used to be an answer rather than a
// wait: the repeat found the run on the note's speaking list and returned as
// though the user had been told. Both states a round can be in are covered, and
// each in both directions, because the two directions are what a wait is for and
// only one of them shows the defect.
//
// The success direction holds on both sides of the fix, and has to: it forbids
// the shortest way to write the fix wrong, which is to let the waiter speak
// whatever the first delivery reports. The failure direction is the regression —
// before the fix the repeat was gone before the outcome it came for existed, and
// with the round consumed and no publisher left the asker kept a spinner nothing
// would ever seal.

// raceTwoFailurePublishers publishes one run's failure twice, holding the first
// delivery on the wire until the repeat has reached the ledger.
//
// sent is where the first delivery reports having got onto the wire and hold is
// what it is parked on: the closing-frame pair for a round that still has its
// bubble, the plain-message pair for one the guard has already closed. Both
// publishers run on goroutines of their own — the bus is synchronous, so a
// publisher occupies the goroutine that published the event.
func (r *bubbleRig) raceTwoFailurePublishers(t *testing.T, taskID string, sent, hold chan struct{}) {
	t.Helper()
	first := make(chan struct{})
	go func() { defer close(first); r.publishFailure(taskID) }()
	<-sent

	second := make(chan struct{})
	go func() { defer close(second); r.publishFailure(taskID) }()
	r.waitForRepeatAtTheLedger(t)
	letTheRepeatSpeak()

	close(hold)
	<-first
	<-second
}

// TestASecondPublisherOfAnOpenRoundsFailureWaitsForTheFirst is the success
// direction on a round that still has its bubble.
//
// The first publisher took the round and its closing frame is on the wire. The
// repeat must add nothing — but it must not decide that before the frame is
// answered, because the deciding fact is whether those words landed, and at that
// moment nobody knows it yet. So it waits, hears that they did, and goes quiet.
func TestASecondPublisherOfAnOpenRoundsFailureWaitsForTheFirst(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.ran(t, "REQ-RACE-OPEN", 1, "task-1")

	rig.conn.holdClosing = make(chan struct{})
	rig.conn.closingSent = make(chan struct{}, 4)

	rig.raceTwoFailurePublishers(t, taskUUID(t, "task-1"), rig.conn.closingSent, rig.conn.holdClosing)

	if got := said(t, rig.conn); len(got) != 1 || got[0] != streamCopyFailed {
		t.Fatalf("one open round's failure left the asker reading %q, want [%q] — its two "+
			"publishers raced inside the window before the closing frame was answered, and "+
			"both spoke", got, streamCopyFailed)
	}
	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Fatalf("the repeat also pushed %q underneath the bubble the first publisher had "+
			"just sealed", got)
	}
}

// TestARefusedClosingFrameLeavesTheOpenRoundToTheRepeat is the regression, and
// the sequence the review describes.
//
// The first publisher takes the round, its closing frame is refused, and so is
// the plain message it falls back to — the reconnect window, in which every
// delivery path is silent. The round is gone either way: the handle is consumed
// under the lock that found it, so what the asker is looking at is a spinner
// nothing can seal any more, and the ledger records the run as owed its ending
// for whoever says it next.
//
// The repeat IS whoever says it next. It is standing right there, past the origin
// gate, with a live connection; turning it away with "already said" while the
// first delivery is still on the wire spends the last publisher this news has,
// and the debt filed a moment later has nobody left to spend it.
func TestARefusedClosingFrameLeavesTheOpenRoundToTheRepeat(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.ran(t, "REQ-RACE-OPEN-REFUSED", 1, "task-1")

	rig.conn.refuseClosingCode = errcodeStreamExpired
	rig.conn.refusePushes = 1 // the first notice is refused; a later one is not
	rig.conn.holdPush = make(chan struct{})
	rig.conn.pushSent = make(chan struct{}, 4)

	rig.raceTwoFailurePublishers(t, taskUUID(t, "task-1"), rig.conn.pushSent, rig.conn.holdPush)

	got := pushedTexts(t, rig.conn)
	if len(got) != 2 || got[1] != streamCopyFailed {
		t.Fatalf("after the first publisher's frame and fallback were both refused, the messages "+
			"attempted were %q, want two — the refused first one and then %q from the repeat. It "+
			"was turned away before the outcome it came for existed, and nothing else will ever "+
			"seal the bubble the asker is watching", got, streamCopyFailed)
	}
}

// TestASecondPublisherOfAGuardOwedFailureWaitsForTheFirst is the success
// direction on a round the guard has already closed.
//
// The promise is on the owed list, the first publisher has claimed it, and its
// message is on the wire. The repeat waits, hears the words landed, and adds
// nothing — one promise, one reply.
func TestASecondPublisherOfAGuardOwedFailureWaitsForTheFirst(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.ran(t, "REQ-RACE-OWED", 1, "task-1")
	rig.guardClosed(t, 1)

	rig.conn.holdPush = make(chan struct{})
	rig.conn.pushSent = make(chan struct{}, 4)

	rig.raceTwoFailurePublishers(t, taskUUID(t, "task-1"), rig.conn.pushSent, rig.conn.holdPush)

	if got := pushedTexts(t, rig.conn); len(got) != 1 || got[0] != streamCopyFailed {
		t.Fatalf("one guard-owed round's failure left the asker reading %q, want [%q] — the "+
			"repeat spoke against a promise the first publisher had already claimed, so the "+
			"separate reply the guard promised arrived twice", got, streamCopyFailed)
	}
}

// TestARefusedGuardOwedNoticeIsStillTheRepeatsToSay is the milder half of the
// regression, and the one that shows what the discarded retry costs even when
// the promise survives.
//
// The claim comes back to owed when the delivery fails, so a LATER publisher
// could still spend it — but the concurrent one was already available, already
// past the origin gate, and had a live connection. Sending it away turns a
// promise that could have been kept in the same breath into one that waits on
// whatever publisher happens to come next, and on this path nothing is scheduled
// to.
func TestARefusedGuardOwedNoticeIsStillTheRepeatsToSay(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.ran(t, "REQ-RACE-OWED-REFUSED", 1, "task-1")
	rig.guardClosed(t, 1)

	rig.conn.refusePushes = 1 // the first notice is refused; a later one is not
	rig.conn.holdPush = make(chan struct{})
	rig.conn.pushSent = make(chan struct{}, 4)

	rig.raceTwoFailurePublishers(t, taskUUID(t, "task-1"), rig.conn.pushSent, rig.conn.holdPush)

	got := pushedTexts(t, rig.conn)
	if len(got) != 2 || got[1] != streamCopyFailed {
		t.Fatalf("after the first notice against the guard's promise was refused, the messages "+
			"attempted were %q, want two — the refused first one and then %q from the repeat. "+
			"The promise came back to the owed list with the one publisher that could still "+
			"have kept it already sent away", got, streamCopyFailed)
	}
}
