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
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
// ever close, which the guard later turns into a promise of a reply that has
// already been sent.
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
			"one run cannot close two bubbles, and the spare spins until the guard promises a reply that has already been sent", got)
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
	// this process started. Either way it has no bubble here.
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
	rig.q.tasks[taskUUID(t, "retry")] = db.AgentTaskQueue{
		ID:              mustParseTestUUID(t, "retry"),
		ChatInputTaskID: mustParseTestUUID(t, "task-1"),
	}
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
func TestTheRetryLookupIsNotPaidForOnEveryAnswer(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-C1", 1, "task-1")
	rig.answer(t, "the agent reply", "task-1")

	if rig.q.taskGets != 0 {
		t.Fatalf("an answer that matched its own round still read %d task row(s)", rig.q.taskGets)
	}
	// Nothing open now, so an unmatched ending must not read a row either.
	rig.answer(t, "a late stray", "task-3")
	if rig.q.taskGets != 0 {
		t.Fatalf("an ending for a session with no open round read %d task row(s)", rig.q.taskGets)
	}
}

// ---- 3. cancellation ----

// TestACancelledRunClosesItsBubble.
//
// Cancellation publishes task:cancelled and nothing else: no chat:done, no
// task:failed. Subscribing only to failure leaves the bubble spinning for the
// full five minutes, after which the guard tells the user "还在处理，完成后我再
// 单独回复你" — a promise of a separate reply, about a run they cancelled
// themselves, that will never come.
func TestACancelledRunClosesItsBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-X", 1, "task-1")

	rig.cancelled(t, "task-1")

	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 {
		t.Fatalf("a cancelled run wrote %d stream frames, want 2 — the bubble spins until the guard promises a reply that will never come", len(frames))
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
	if content == streamCopyFailed {
		t.Errorf("a cancelled run was closed with the failure copy %q", content)
	}
	if content != streamCopyCancelled {
		t.Errorf("cancellation copy = %q, want %q", content, streamCopyCancelled)
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
		if f["finish"] != true || f["content"] != streamCopyCancelled {
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

// TestCancellingAfterTheGuardKeepsThePromise. Once the guard has closed a
// bubble it has promised a separate reply, and that promise outlives the
// bubble. Cancelling the run makes the promise void, and the only way to say
// so is a plain message to the chat the promise was made in.
func TestCancellingAfterTheGuardKeepsThePromise(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	sessionID := bubbleSessionID(t)
	rig.ran(t, "REQ-G1", 1, "task-1")

	// The guard closes the bubble at five minutes; the run carries on.
	if _, ok := rig.streams.takeBatch(sessionID, 1, roundContinues); !ok {
		t.Fatal("could not guard-close the round")
	}

	rig.cancelled(t, "task-1")

	pushes := rig.conn.pushes(t)
	if len(pushes) != 1 {
		t.Fatalf("a cancel after the guard sent %d plain messages, want 1 — the guard promised a separate reply and nothing ever came", len(pushes))
	}
	md, _ := pushes[0]["markdown"].(map[string]any)
	if md == nil || md["content"] != streamCopyCancelled {
		t.Fatalf("the promised reply did not say it was cancelled: %v", pushes[0])
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

// ---- the guard, now that a round always knows its run ----

// TestAGuardClosedRoundsFailureIsStillReported. The guard consumes the handle
// at five minutes, so a run that fails after that finds no bubble. The note it
// left is what turns the promise into a delivered message, and it is matched
// by the run's own id — a promise left by a DIFFERENT round must not be spent
// on this one.
func TestAGuardClosedRoundsFailureIsStillReported(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	sessionID := bubbleSessionID(t)
	rig.ran(t, "REQ-P1", 1, "task-1")
	rig.ran(t, "REQ-P2", 2, "task-2")

	// Both bubbles run into the window and are guard-closed mid-run.
	if _, ok := rig.streams.takeBatch(sessionID, 1, roundContinues); !ok {
		t.Fatal("could not guard-close the first round")
	}
	if _, ok := rig.streams.takeBatch(sessionID, 2, roundContinues); !ok {
		t.Fatal("could not guard-close the second round")
	}

	// The first round's run answers — the separate reply its guard promised.
	rig.answer(t, "the first answer", "task-1")
	// The second round's run then fails. Its own promise is still outstanding.
	rig.failed(t, "task-2", false)

	pushes := rig.conn.pushes(t)
	if len(pushes) != 2 {
		t.Fatalf("got %d plain messages, want 2 (the first round's answer, then the second round's failure) — "+
			"the second asker was promised a reply and heard nothing", len(pushes))
	}
	md, _ := pushes[1]["markdown"].(map[string]any)
	if md == nil || md["content"] != streamCopyFailed {
		t.Fatalf("the second round's failure did not reach its asker: %v", pushes[1])
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
	if frames[2]["content"] != streamCopyNotStarted {
		t.Errorf("settle copy = %q, want %q", frames[2]["content"], streamCopyNotStarted)
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
	if _, ok := rig.streams.takeTask(sessionID, taskUUID(t, "task-1"), roundOver); ok {
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
