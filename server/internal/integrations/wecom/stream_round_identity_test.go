package wecom

// stream_round_identity_test.go — which run a bubble stands for, and the ways
// that must never be guessed at.
//
// The bubble is a promise: this is where your answer will appear. Keeping it
// means knowing, for every message, which run will answer it, and for every
// ending, which bubble it belongs in. The run announces itself on the bus
// (task:queued) and the bubble is whatever is still waiting for one, and these
// tests hold that shut from the outside: they drive the two seams the Router
// and the task service drive — an ingest, and a published event — never the
// store's internals.

import (
	"context"
	"testing"
	"time"
)

// ---- 1. the round boundary ----

// TestAnEndingNeverTakesABubbleItWasNotBoundTo: a run whose id was never bound
// to a round has no bubble here, and taking one on position would seal
// somebody else's question with this answer.
func TestAnEndingNeverTakesABubbleItWasNotBoundTo(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)

	rig.ran(t, "REQ-MINE", "task-1")
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

// ---- 2. cancellation ----

// TestCancellingEveryQueuedTurnClosesEachOwnBubble covers the bulk paths:
// CancelQueuedChatTasks for a session's waiting follow-ups, and the
// agent-level "cancel all", both of which broadcast task:cancelled per row.
// Each round has a bubble of its own, so each needs its own closing frame —
// one frame for the lot would leave the rest spinning.
func TestCancellingEveryQueuedTurnClosesEachOwnBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-Q1", "task-1")
	rig.ran(t, "REQ-Q2", "task-2")
	rig.ran(t, "REQ-Q3", "task-3")

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

// TestACancelledRunThisProcessNeverSawStaysSilent is the deliberate limit on
// the above. A bulk "cancel all tasks" sweeps every session an agent serves;
// chasing an address through the binding row for rounds this process holds
// nothing for would turn one click into a message in every one of those chats,
// including sessions where WeCom never showed a bubble at all.
func TestACancelledRunThisProcessNeverSawStaysSilent(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	// One unrelated round on file, so the subscriber does not bail early.
	rig.ran(t, "REQ-K", "task-1")

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

// TestALateIngestForAnAnsweredRunOpensABubbleTheNextQuestionJoins is the price
// of taking the round boundary off the engine, stated rather than hidden.
//
// OnIngested is detached and carries the Router's reply budget, so a badly
// delayed one can arrive after the run it was painting for has already
// answered. Nothing on that call says which run it belonged to any more — the
// batch id it used to carry is gone — so the store cannot tell a straggler from
// a new question and paints. What it must not do is leave that bubble stranded:
// it is a round waiting for a run, which is exactly what the next question
// joins and the next run then closes.
func TestALateIngestForAnAnsweredRunOpensABubbleTheNextQuestionJoins(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-D1", "task-1")
	rig.answer(t, "the agent reply", "task-1")

	// The second message of the same run, painted far too late.
	rig.ask(t, "REQ-D2")
	if got := rig.streams.depth(); got != 1 {
		t.Fatalf("a late ingest left %d bubble(s) open, want 1", got)
	}

	// The next question joins it rather than adding a second, and its own run
	// closes it — so nothing is left spinning.
	rig.ask(t, "REQ-D3")
	rig.queued(t, "task-2")
	rig.answer(t, "the next reply", "task-2")

	if got := rig.streams.depth(); got != 0 {
		t.Fatalf("%d bubble(s) still open after the next question was answered — a late ingest "+
			"stranded a spinner nothing can close", got)
	}
	frames := rig.conn.streamFrames(t)
	if len(frames) != 4 {
		t.Fatalf("got %d stream frames, want 4 (open+seal, then the late open and its seal)", len(frames))
	}
	if frames[3]["id"] != frames[2]["id"] || frames[3]["content"] != "the next reply" {
		t.Fatalf("the next question's answer did not seal the bubble the late ingest opened: %v", frames[3])
	}
}

// ---- the settled flush ----

// TestAFlushThatStartedNoRunClosesTheBubbleWithNoRun. A flush that produced no
// task has no name to pass, so OnSettled closes the session's oldest round that
// never became a run. A round ahead of it has its own run and its own ending
// coming; closing that one instead would seal a question that is still being
// answered.
func TestAFlushThatStartedNoRunClosesTheBubbleWithNoRun(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	sessionID := bubbleSessionID(t)
	rig.ran(t, "REQ-S1", "task-1") // running, bound
	rig.ask(t, "REQ-S2")           // the flush that found no runtime

	rig.typing.OnSettled(context.Background(), sessionID)

	frames := rig.conn.streamFrames(t)
	if len(frames) != 3 {
		t.Fatalf("got %d stream frames, want 3 (two opens, one settle)", len(frames))
	}
	if frames[2]["id"] != frames[1]["id"] {
		t.Fatalf("the settled flush sealed bubble %v, want the unbound question's %v — it closed "+
			"the running question's bubble instead, and that answer now has nowhere to land",
			frames[2]["id"], frames[1]["id"])
	}
	if frames[2]["content"] != streamCopyNotStarted {
		t.Errorf("settle copy = %q, want %q", frames[2]["content"], streamCopyNotStarted)
	}
}

// TestASettledFlushLeavesARoundWaitingForItsRetry: a round released by
// retryUnbind has no run bound to it either, but its replacement is already on
// the way. Closing it as "never started" would tell the user a run that is
// about to answer them never began.
func TestASettledFlushLeavesARoundWaitingForItsRetry(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-RETRY-SETTLE", "task-1")
	rig.failed(t, "task-1", true) // the attempt is being retried; the round waits

	rig.typing.OnSettled(context.Background(), bubbleSessionID(t))

	if got := len(rig.conn.streamFrames(t)); got != 1 {
		t.Fatalf("got %d stream frames, want 1 (the opening one) — a settled flush closed the "+
			"bubble of a round whose retry is already queued", got)
	}
}

// ---- housekeeping ----

// TestAStaleRoundIsSweptRatherThanKept guards what the store would otherwise
// leak: a round whose run produced no ending at all.
func TestAStaleRoundIsSweptRatherThanKept(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	sessionID := bubbleSessionID(t)

	rig.ran(t, "REQ-OLD", "task-1")
	rig.now = rig.now.Add(streamMaxAge + time.Minute)
	// Any operation that sweeps: a later question in the same session.
	rig.ask(t, "REQ-NEW")

	if got := rig.streams.depth(); got != 1 {
		t.Fatalf("store holds %d open bubbles, want 1 (only the new question's)", got)
	}
	if rig.streams.has(sessionID, taskUUID(t, "task-1")) {
		t.Fatal("a round from beyond the stream window was still on file")
	}
}

// TestACancelledRunNeverBindsTheNextQuestionsBubble covers the ordering the two
// facts arrive in when the slower one is the bubble.
//
// The Router detaches OnIngested and a session's first message enqueues inside
// dispatch, so a run can be queued before any opening frame has landed — it
// waits for the bubble that is coming. A cancel arriving in that window is the
// run's LAST event: cancellation publishes no chat:done and no task:failed. If
// the run were left waiting, the bubble landing a moment later would bind
// itself to it and spin with nothing left that could close it.
func TestACancelledRunNeverBindsTheNextQuestionsBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	// The enqueue wins the race: the run is queued, nothing is painted yet, and
	// nothing else in this process holds a round.
	rig.queued(t, "task-1")

	rig.cancelled(t, "task-1")

	// The ingest goroutine finally gets to the socket.
	rig.ask(t, "REQ-1")

	if rig.streams.has(bubbleSessionID(t), taskUUID(t, "task-1")) {
		t.Fatal("the bubble bound itself to a run that was cancelled before it was painted; " +
			"a cancel publishes no chat:done and no task:failed, so there is no ending left to close it")
	}
	// And it is still a usable bubble: the question's own run closes it.
	rig.queued(t, "task-2")
	rig.answer(t, "the agent reply", "task-2")
	if got := rig.streams.depth(); got != 0 {
		t.Fatalf("%d bubble(s) on screen after the question was answered, want 0", got)
	}
}
