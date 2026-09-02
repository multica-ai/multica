package wecom

// stream_round_identity_test.go — which run a bubble stands for, and the three
// ways that used to be guessed at.
//
// The bubble is a promise: this is where your answer will appear. Keeping it
// means knowing, for every message, which run will answer it, and for every
// ending, which bubble it belongs in. Both facts are carried in — the batch id
// from the debouncer that decides the boundary, the task id from the flush
// that creates the run — and these tests hold that shut from the outside: they
// drive the seam the Router drives, never the store's internals, so a store
// that went back to inferring either one fails them.

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// ---- 1. the round boundary ----

// TestTheBubbleCountFollowsTheBatcherNotTheClock is the boundary case from
// both sides at once, with the clock deliberately lying in each direction.
//
// A store that measured the gap itself would fold the first pair into one
// round (they arrive in the same instant) and split the second pair into two
// (they arrive a full window apart) — the opposite of what the batcher
// decided, and both mistakes are user-visible: a merged pair loses the second
// question's receipt entirely, and a split pair leaves a bubble no run will
// ever close.
func TestTheBubbleCountFollowsTheBatcherNotTheClock(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)

	// The batcher split these two, though nothing separates them on the clock.
	rig.ask(t, "REQ-1", 1)
	rig.ask(t, "REQ-2", 2)
	if got := rig.streams.depth(); got != 2 {
		t.Fatalf("two messages the batcher gave separate runs opened %d bubble(s), want 2 — "+
			"the second question's run has no bubble and its asker saw no receipt at all", got)
	}

	// The batcher merged these two, though a whole window separates them.
	rig.now = rig.now.Add(engine.DefaultChatRunBatchWindow * 2)
	rig.ask(t, "REQ-3", 3)
	rig.now = rig.now.Add(engine.DefaultChatRunBatchWindow * 2)
	rig.ask(t, "REQ-4", 3)
	if got := rig.streams.depth(); got != 3 {
		t.Fatalf("two messages the batcher folded into one run opened %d bubbles in total, want 3 — "+
			"one run cannot close two bubbles, and the spare spins until its window runs out", got)
	}
}

// TestARunCreatedBeforeItsBubbleWasPaintedStillOwnsIt drives the ordering the
// Router does not guarantee: OnIngested runs on a detached goroutine, so the
// debounced flush that creates the run can reach the store first.
//
// The binding has to survive that. If the flush's report were dropped for want
// of a round to attach it to, this round would have no run on file, and the
// answer — which names only the task — would find no bubble and land as a
// plain message underneath a spinner nothing would ever close.
func TestARunCreatedBeforeItsBubbleWasPaintedStillOwnsIt(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)

	// The flush wins the race: the run exists before the bubble is painted.
	rig.runStarted(t, 1, "task-1")
	rig.ask(t, "REQ-LATE", 1)

	if got := rig.streams.depth(); got != 1 {
		t.Fatalf("store holds %d open bubbles, want 1", got)
	}
	rig.answer(t, "the agent reply", "task-1")

	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 {
		t.Fatalf("got %d stream frames, want 2 (open + seal)", len(frames))
	}
	if frames[1]["id"] != frames[0]["id"] || frames[1]["finish"] != true {
		t.Fatalf("the answer did not seal the bubble its question opened: %v", frames[1])
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Fatalf("the answer went out as %d plain message(s) instead, leaving the bubble spinning", len(pushes))
	}
}

// TestAnEndingNeverTakesABubbleItWasNotBoundTo is the other half of the same
// promise. A run whose id was never bound to a round has no bubble here, and
// taking one on position would seal somebody else's question with this answer.
func TestAnEndingNeverTakesABubbleItWasNotBoundTo(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)

	rig.ran(t, "REQ-MINE", 1, "task-1")
	// task-2 belongs to a different session's round, or to a turn from before
	// this process started. Either way it has no bubble here. Its row is filed
	// because it is a real task somewhere — what it does not have is a round
	// on this rig, and that is what has to refuse it. Leave the row out and
	// the origin gate refuses it first, for want of a task, and the binding
	// this test is named after is never consulted.
	rig.q.fileTask(t, taskUUID(t, "task-2"))
	rig.answer(t, "somebody else's answer", "task-2")

	frames := rig.conn.streamFrames(t)
	if len(frames) != 1 {
		t.Fatalf("an unbound run wrote %d stream frames, want 1 (only the opening one) — "+
			"it sealed a bubble that belongs to a question it never saw", len(frames))
	}
	if rig.streams.depth() != 1 {
		t.Fatalf("store holds %d open bubbles, want 1 — this session's own question lost its bubble", rig.streams.depth())
	}
}

// ---- 2. an auto-retry's intermediate failure ----

// TestAnIntermediateFailureBeingRetriedLeavesTheBubbleOpen.
//
// FailTask publishes task:failed for an attempt it has ALREADY replaced with a
// retry child, flagged retry_pending so consumers stay quiet — taskFailedFields
// even withholds the error text in that case. Closing the bubble on it tells
// the user "这次没跑通" about an attempt whose replacement is already queued,
// and the retry's answer then arrives underneath a bubble that has declared
// failure: the user is told it failed AND gets an answer, in that order.
func TestAnIntermediateFailureBeingRetriedLeavesTheBubbleOpen(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-R", 1, "task-1")

	rig.failed(t, "task-1", true)

	frames := rig.conn.streamFrames(t)
	if len(frames) != 1 {
		t.Fatalf("an attempt the platform is already retrying wrote %d stream frames, want 1 (the opening one) — "+
			"the bubble was closed as a failure and the retry's answer will land underneath it: content = %q",
			len(frames), frames[len(frames)-1]["content"])
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Fatalf("a retry-pending attempt sent %d plain message(s); the user is told it failed before it has", len(pushes))
	}
	if rig.streams.depth() != 1 {
		t.Fatalf("store holds %d open rounds, want 1 — the retry has nowhere to answer", rig.streams.depth())
	}
}

// TestTheRetryAnswerLandsInTheBubbleTheFirstAttemptOpened finishes that story.
//
// The retry child is a NEW task row with a new id, so its chat:done names an id
// no round was ever bound to. What it does inherit is chat_input_task_id — the
// turn that owns the input batch, which is exactly the id the flush bound this
// round under. Reading that column is what routes the answer home; falling
// back to "whichever bubble is at the head" would be a guess that happens to
// work only while a session has one round open.
func TestTheRetryAnswerLandsInTheBubbleTheFirstAttemptOpened(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-R1", 1, "task-1")
	// A second question is waiting behind it with a bubble of its own, so a
	// positional fallback would have two candidates and could pick either.
	rig.ran(t, "REQ-R2", 2, "task-2")

	rig.failed(t, "task-1", true)

	// FailTask's retry child: fresh id, inheriting the parent's input batch.
	rig.q.fileRetryClone(t, taskUUID(t, "retry"), taskUUID(t, "task-1"))
	rig.answer(t, "the retry's answer", "retry")

	frames := rig.conn.streamFrames(t)
	if len(frames) != 3 {
		t.Fatalf("got %d stream frames, want 3 (two opens, then the retry sealing the first) — "+
			"the retry's answer did not reach the bubble its question opened", len(frames))
	}
	if frames[2]["id"] != frames[0]["id"] {
		t.Fatalf("the retry sealed bubble %v, want the first question's %v — the wrong asker read this answer",
			frames[2]["id"], frames[0]["id"])
	}
	if frames[2]["content"] != "the retry's answer" || frames[2]["finish"] != true {
		t.Fatalf("the retry did not seal the bubble with its answer: %v", frames[2])
	}
	if rig.streams.depth() != 1 {
		t.Fatalf("store holds %d open rounds, want 1 — the waiting question kept its own bubble", rig.streams.depth())
	}
}

// TestTheRetryLookupIsNotPaidForOnEveryAnswer keeps the extra read honest: it
// happens only when the id on the event matches no round in a session that
// still has one open.
//
// Two readers want the task row on this path now. The origin gate reads it for
// every answer it lets through, and that read is not optional — it is what
// keeps a web question's answer out of the room. So the gate's one read is the
// floor, and what this test measures is whether the round matcher adds a
// second on top of it.
func TestTheRetryLookupIsNotPaidForOnEveryAnswer(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-C1", 1, "task-1")

	before := rig.q.taskGets
	rig.answer(t, "the agent reply", "task-1")
	if got := rig.q.taskGets - before; got != 1 {
		t.Fatalf("an answer that matched its own round read %d task rows, want 1 (the origin gate's) — "+
			"the retry lookup was paid for on an answer that already named its own round", got)
	}
	// Nothing open now, so an unmatched ending must not reach the matcher
	// either. It never gets that far: task-3 was never filed, so the gate
	// refuses it on the read it was always going to make.
	before = rig.q.taskGets
	rig.answer(t, "a late stray", "task-3")
	if got := rig.q.taskGets - before; got != 1 {
		t.Fatalf("an ending for a session with no open round read %d task rows, want 1 "+
			"(the origin gate's, which finds no row and stops there)", got)
	}
}

// ---- 3. cancellation ----

// TestACancelledRunClosesItsBubble.
//
// Cancellation publishes task:cancelled and nothing else: no chat:done, no
// task:failed. Subscribing only to failure leaves the bubble spinning until
// the server's window runs out on it — for a run the user stopped themselves.
func TestACancelledRunClosesItsBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-X", 1, "task-1")

	rig.cancelled(t, "task-1")

	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 {
		t.Fatalf("a cancelled run wrote %d stream frames, want 2 — the bubble spins until its window runs out", len(frames))
	}
	if frames[1]["finish"] != true {
		t.Fatal("the cancellation did not seal the bubble")
	}
	content, _ := frames[1]["content"].(string)
	if !hasVisibleChar(content) {
		t.Fatalf("the closing frame carries nothing visible (%q); WeCom discards it and the bubble spins forever", content)
	}
	// Asserted against the FAILURE copy, not just against its own constant: a
	// cancellation closed with "请稍后再试一次" invites a retry of something the
	// user just stopped on purpose.
	if content == copyFor(DefaultLocale).StreamFailed {
		t.Errorf("a cancelled run was closed with the failure copy %q", content)
	}
	if content != copyFor(DefaultLocale).StreamCancelled {
		t.Errorf("cancellation copy = %q, want %q", content, copyFor(DefaultLocale).StreamCancelled)
	}
	if rig.streams.depth() != 0 {
		t.Fatalf("store holds %d open rounds after the cancel, want 0", rig.streams.depth())
	}
}

// TestCancellingEveryQueuedTurnClosesEachOwnBubble covers the bulk paths:
// CancelQueuedChatTasks for a session's waiting follow-ups, and the
// agent-level "cancel all", both of which broadcast task:cancelled per row.
// Each round has a bubble of its own, so each needs its own closing frame —
// one frame for the lot would leave the rest spinning.
func TestCancellingEveryQueuedTurnClosesEachOwnBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-Q1", 1, "task-1")
	rig.ran(t, "REQ-Q2", 2, "task-2")
	rig.ran(t, "REQ-Q3", 3, "task-3")

	rig.cancelled(t, "task-1")
	rig.cancelled(t, "task-2")
	rig.cancelled(t, "task-3")

	if got := rig.streams.depth(); got != 0 {
		t.Fatalf("%d bubble(s) still spinning after every run in the session was cancelled", got)
	}
	frames := rig.conn.streamFrames(t)
	if len(frames) != 6 {
		t.Fatalf("got %d stream frames, want 6 (three opens, three cancels)", len(frames))
	}
	opened := map[any]bool{}
	for _, f := range frames[:3] {
		opened[f["id"]] = true
	}
	for _, f := range frames[3:] {
		if f["finish"] != true || f["content"] != copyFor(DefaultLocale).StreamCancelled {
			t.Fatalf("a closing frame did not carry the cancellation: %v", f)
		}
		if !opened[f["id"]] {
			t.Fatalf("a cancellation sealed stream %v, which no question opened", f["id"])
		}
		delete(opened, f["id"])
	}
	if len(opened) != 0 {
		t.Fatalf("%d bubble(s) were never sealed", len(opened))
	}
}

// TestACancelledRunThisProcessNeverSawStaysSilent is the deliberate limit on
// the above. A bulk "cancel all tasks" sweeps every session an agent serves;
// chasing an address through the binding row for rounds this process holds
// nothing for would turn one click into a message in every one of those chats,
// including sessions where WeCom never showed a bubble at all.
func TestACancelledRunThisProcessNeverSawStaysSilent(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	// One unrelated round on file, so the subscriber does not bail early.
	rig.ran(t, "REQ-K", 1, "task-1")

	rig.cancelled(t, "task-2")

	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Fatalf("a cancel for a run with no round on file sent %d plain message(s)", len(pushes))
	}
	if got := len(rig.conn.streamFrames(t)); got != 1 {
		t.Fatalf("got %d stream frames, want 1 — the unrelated round's bubble was sealed by somebody else's cancel", got)
	}
}

// ---- an empty answer with no bubble ----

// An empty completion for a round with no bubble is what it looks like —
// nothing to send. Inside a bubble the copy stands in for the silence, because
// a spinner has to end in words; with no bubble there is nothing waiting on
// them, and a line in the room would be noise for every empty chat:done on a
// bound session, including the ones a browser produced.
func TestAnEmptyAnswerWithNoBubbleSaysNothing(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")

	rig.answer(t, "", "task-1")

	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Fatalf("an empty answer for a round with no bubble sent %d plain message(s) into the room", len(pushes))
	}
	if frames := rig.conn.streamFrames(t); len(frames) != 0 {
		t.Fatalf("an empty answer for a round with no bubble wrote %d stream frames", len(frames))
	}
}

// TestABubbleIsNeverRepaintedForARunThatHasAnswered. OnIngested is detached and
// carries the Router's reply budget; a badly delayed one can arrive after the
// run it was painting for has already answered. Painting then would open a
// second bubble for a finished run — one nothing would ever close.
func TestABubbleIsNeverRepaintedForARunThatHasAnswered(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-D1", 1, "task-1")
	rig.answer(t, "the agent reply", "task-1")

	// The second message of the same run, painted far too late.
	rig.ask(t, "REQ-D2", 1)

	if got := rig.streams.depth(); got != 0 {
		t.Fatalf("a late ingest re-opened %d bubble(s) for a run that has already answered", got)
	}
	if got := len(rig.conn.streamFrames(t)); got != 2 {
		t.Fatalf("got %d stream frames, want 2 (open + seal); the extra one spins forever", got)
	}
}

// ---- the settled flush ----

// TestAFlushThatStartedNoRunClosesItsOwnBubble. The flush reports the batch it
// was answering, so a session with a queued round behind it closes the right
// bubble rather than the newest one.
func TestAFlushThatStartedNoRunClosesItsOwnBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	sessionID := bubbleSessionID(t)
	rig.ran(t, "REQ-S1", 1, "task-1")
	rig.ask(t, "REQ-S2", 2)

	// Batch 1's flush is the one that found no runtime.
	rig.typing.OnSettled(context.Background(), sessionID, 1)

	frames := rig.conn.streamFrames(t)
	if len(frames) != 3 {
		t.Fatalf("got %d stream frames, want 3 (two opens, one settle)", len(frames))
	}
	if frames[2]["id"] != frames[0]["id"] {
		t.Fatalf("the settled flush sealed bubble %v, want batch 1's %v — it closed the waiting question's bubble instead",
			frames[2]["id"], frames[0]["id"])
	}
	if frames[2]["content"] != copyFor(DefaultLocale).StreamNotStarted {
		t.Errorf("settle copy = %q, want %q", frames[2]["content"], copyFor(DefaultLocale).StreamNotStarted)
	}
}

// ---- housekeeping ----

// TestAStaleRoundIsSweptRatherThanKept guards the one thing an entry that can
// exist without a bubble could otherwise leak: a flush that named a run whose
// ingest goroutine never arrived leaves a round with nothing to close it.
func TestAStaleRoundIsSweptRatherThanKept(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	sessionID := bubbleSessionID(t)

	rig.runStarted(t, 1, "task-1")
	rig.now = rig.now.Add(streamMaxAge + time.Minute)
	// Any operation that sweeps: a later question in the same session.
	rig.ask(t, "REQ-NEW", 2)

	if got := rig.streams.depth(); got != 1 {
		t.Fatalf("store holds %d open bubbles, want 1 (only the new question's)", got)
	}
	if rig.streams.has(sessionID, taskUUID(t, "task-1")) {
		t.Fatal("a round from beyond the stream window was still on file")
	}
}

// TestOpenIsIgnoredWithoutABatch: a caller with no batch id has no way to say
// which run this is, and a round it could not name is a bubble nothing could
// close.
func TestOpenIsIgnoredWithoutABatch(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ask(t, "REQ-NOBATCH", 0)
	if got := rig.streams.depth(); got != 0 {
		t.Fatalf("a message with no run batch opened %d bubble(s)", got)
	}
	if got := len(rig.conn.streamFrames(t)); got != 0 {
		t.Fatalf("a message with no run batch wrote %d stream frames", got)
	}
}

// TestACancelRetiresARoundWhoseBubbleIsStillInFlight covers the ordering the
// two facts arrive in when the slower one is the bubble.
//
// The Router detaches OnIngested, so a round can be bound to its run before
// the opening frame has landed — the entry exists with a task and no bubble,
// which roundEntry documents as an ordinary state. A cancel arriving in that
// window is the run's LAST event: cancellation publishes no chat:done and no
// task:failed, so if the round is not retired here nothing will ever retire
// it, and the opening frame landing a moment later paints a spinner with no
// closer — for a run the user themselves stopped.
//
// The subscriber used to return before looking, because its cheap rejection
// counted PAINTED rounds and there were none anywhere in the process. This is
// that gap: one round, unpainted, and nothing else on file.
func TestACancelRetiresARoundWhoseBubbleIsStillInFlight(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	// The flush wins the race: the run is bound, its opening frame has not
	// been written yet, and nothing else in this process holds a round.
	rig.runStarted(t, 1, "task-1")

	rig.cancelled(t, "task-1")

	// The ingest goroutine finally gets to the socket. Retiring the round is
	// what it reads: open sees the batch on the session's finished ring and
	// paints nothing.
	rig.ask(t, "REQ-1", 1)

	if frames := rig.conn.streamFrames(t); len(frames) != 0 {
		t.Fatalf("the opening frame painted %d bubble(s) for a run that was cancelled before it "+
			"landed; a cancel publishes no chat:done and no task:failed, so there is no ending "+
			"left to close them", len(frames))
	}
	if got := rig.streams.depth(); got != 0 {
		t.Fatalf("%d bubble(s) on screen for a cancelled run, want 0", got)
	}
}
