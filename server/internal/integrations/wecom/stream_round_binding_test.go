package wecom

// stream_round_binding_test.go — the seam that replaced the engine's own
// report, driven end to end.
//
// A bubble used to learn which run it stood for from the Router, which handed
// the batcher's verdict and the flush's task id straight to the notifier. Both
// are gone: the Router says only "this message was ingested", and the run says
// "I was queued" on the bus that every enqueue path already publishes to. What
// has to hold, unchanged, is what the person in the chat sees — one bubble per
// run, each answer replacing the bubble its own question opened, and never a
// plain message where a bubble was on screen.
//
// So every test here reads the WIRE: the frames that went out, in order, and
// whether anything took the "as a new message" path. That is what the user
// sees, and it is the only thing that can say the new seam did not degrade.

import (
	"context"
	"testing"
)

// ---- 1. one debounce window, one bubble ----

// Two messages in one debounce window are one run, and one run is one bubble:
// a second bubble here is one nobody would ever close, because the run produces
// exactly one answer.
//
// Nothing is queued between the two messages, which is the whole of what says
// they are one run. The store never measures the gap itself — see
// TestTheBubbleCountFollowsTheRunsNotTheClock, which lies with the clock in
// both directions.
//
// REVERSE VERIFICATION: make open always start a new round (drop the
// collectingLocked branch) and this fails with two bubbles and two messages in
// the chat — the second one spinning for good.
func TestTwoMessagesInOneWindowShareOneBubbleAndOneAnswer(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)

	rig.ask(t, "REQ-W1")
	rig.ask(t, "REQ-W2")
	if got := rig.streams.depth(); got != 1 {
		t.Fatalf("two messages inside one debounce window opened %d bubbles, want 1 — "+
			"the run answers once, so every extra bubble spins until its window runs out", got)
	}

	rig.queued(t, "task-1")
	rig.answer(t, "the agent reply", "task-1")

	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 {
		t.Fatalf("got %d stream frames, want 2 (one open, one seal)", len(frames))
	}
	if frames[1]["id"] != frames[0]["id"] || frames[1]["finish"] != true {
		t.Fatalf("the answer did not replace the bubble the questions opened: %v", frames[1])
	}
	if got := said(t, rig.conn); len(got) != 1 || got[0] != "the agent reply" {
		t.Fatalf("the asker read %d message(s) %q, want exactly one carrying the answer", len(got), got)
	}
}

// ---- 2. a question asked while the last one is still running ----

// TestASecondQuestionWhileTheFirstRunsKeepsBothBubbles is the proof that this
// change did not degrade anything, and the reason it is worth the most here.
//
// Two runs, overlapping: the second question arrives after the first has its
// run, so it is a round of its own and gets a bubble of its own immediately.
// Each answer then has to replace ITS OWN bubble in place. Everything that
// could go wrong shows up in the same two counts — a store that bound both runs
// to one bubble, or matched an ending by position, seals one question with the
// other's answer and leaves the survivor spinning above a plain message.
//
// REVERSE VERIFICATION: make bindNext take the NEWEST unbound round instead of
// the oldest and this fails with the answers swapped between the two bubbles.
func TestASecondQuestionWhileTheFirstRunsKeepsBothBubbles(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)

	rig.ask(t, "REQ-A")
	rig.queued(t, "task-1")
	rig.ask(t, "REQ-B")
	rig.queued(t, "task-2")

	if got := rig.streams.depth(); got != 2 {
		t.Fatalf("two overlapping questions hold %d bubbles, want 2 — one of the two askers "+
			"has no receipt at all", got)
	}

	rig.answer(t, "the first answer", "task-1")
	rig.answer(t, "the second answer", "task-2")

	frames := rig.conn.streamFrames(t)
	if len(frames) != 4 {
		t.Fatalf("got %d stream frames, want 4 (two opens, two seals)", len(frames))
	}
	if frames[2]["id"] != frames[0]["id"] {
		t.Fatalf("the first answer sealed bubble %v, want the first question's %v — the wrong "+
			"asker read this answer", frames[2]["id"], frames[0]["id"])
	}
	if frames[3]["id"] != frames[1]["id"] {
		t.Fatalf("the second answer sealed bubble %v, want the second question's %v",
			frames[3]["id"], frames[1]["id"])
	}
	if frames[2]["content"] != "the first answer" || frames[3]["content"] != "the second answer" {
		t.Fatalf("the answers landed in the wrong bubbles: %q then %q",
			frames[2]["content"], frames[3]["content"])
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Fatalf("%d answer(s) went out as a plain message instead of replacing a bubble; "+
			"a spinner is left above each of them", len(pushes))
	}
	if got := said(t, rig.conn); len(got) != 2 {
		t.Fatalf("the asker read %d message(s) %q, want exactly two — one per question", len(got), got)
	}
}

// ---- 3. the run announced before the bubble was painted ----

// The Router detaches the ingest goroutine, and a session's first message
// enqueues its task inside dispatch rather than on the debounced flush, so
// task:queued routinely arrives before anything has been painted. A run with no
// bubble waiting has to wait for one, or this round has no run on file and its
// answer — which names only the task — lands as a plain message underneath a
// spinner nothing will ever close.
//
// REVERSE VERIFICATION: drop the pending queue (return from bindNext when no
// round is waiting) and this fails with the answer arriving as a plain message
// and the opening frame never sealed.
func TestARunQueuedBeforeItsBubbleStillBindsToIt(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)

	rig.queued(t, "task-1")
	rig.ask(t, "REQ-EARLY")
	rig.answer(t, "the agent reply", "task-1")

	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 {
		t.Fatalf("got %d stream frames, want 2 (open + seal)", len(frames))
	}
	if frames[1]["id"] != frames[0]["id"] || frames[1]["finish"] != true ||
		frames[1]["content"] != "the agent reply" {
		t.Fatalf("the answer did not replace the bubble its question opened: %v", frames[1])
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Fatalf("the answer went out as %d plain message(s), leaving the bubble spinning", len(pushes))
	}
}

// ---- 4. the runs that are not this conversation's ----

// TestAnIssueRunNeverTakesAWaitingBubble.
//
// task:queued fires for every run in the deployment. An /issue command, an
// autopilot run, a rerun from the web card: they all carry an issue id, and
// they all answer somewhere other than this chat. One of them binding a WeCom
// bubble would leave the real turn with nowhere to land and seal the asker's
// question with an answer produced for somebody else's issue.
//
// A chat turn is the one with no issue at all — CreateChatTask inserts issue_id
// as NULL, and taskEvent serializes that as the empty string.
//
// REVERSE VERIFICATION: delete the issue_id check in handleTaskQueued and this
// fails — the issue run takes the bubble, and the chat run's own answer arrives
// as a plain message under a spinner the issue run's ending never closes.
func TestAnIssueRunNeverTakesAWaitingBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)

	rig.ask(t, "REQ-ISSUE")
	rig.queueTask(t, taskUUID(t, "issue-run"), "99999999-0000-0000-0000-000000000001")

	if rig.streams.has(bubbleSessionID(t), taskUUID(t, "issue-run")) {
		t.Fatal("an /issue run took the bubble a chat question opened; its answer never comes " +
			"back to this chat, so the spinner has no ending left")
	}

	// The conversation's own run arrives and takes the bubble it was always for.
	rig.queued(t, "task-1")
	rig.answer(t, "the agent reply", "task-1")

	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 {
		t.Fatalf("got %d stream frames, want 2 (open + seal)", len(frames))
	}
	if frames[1]["id"] != frames[0]["id"] || frames[1]["content"] != "the agent reply" {
		t.Fatalf("the chat run's answer did not replace the bubble its question opened: %v", frames[1])
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Fatalf("the answer went out as %d plain message(s)", len(pushes))
	}
}

// ---- 5. the auto-retry clone ----

// TestARetryCloneTakesTheRoundItIsReplacingNotTheNextQuestions.
//
// An auto-retry clone is a NEW task row: new id, chat_session_id carried over,
// issue_id still NULL. On the bus it is indistinguishable from a fresh chat
// turn, so without the retry handling it would bind whatever bubble is waiting
// — the SECOND question's — and then seal it with the FIRST question's answer.
//
// The order below is FailTask's own: it broadcasts task:queued for the retry
// child (service/task.go, the `if retried != nil` block) BEFORE it broadcasts
// the parent's task:failed. So the clone is already waiting when the failure
// arrives, and retryUnbind is what pairs the two.
//
// REVERSE VERIFICATION: drop the retryUnbind call from handleTaskFailed's
// retry_pending branch and this fails — the clone binds the second question's
// bubble, the second question's own run is left waiting, and the retry's answer
// seals the wrong asker's spinner.
func TestARetryCloneTakesTheRoundItIsReplacingNotTheNextQuestions(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)

	rig.ran(t, "REQ-R1", "task-1")

	// FailTask, in its own publish order.
	rig.q.fileRetryClone(t, taskUUID(t, "retry"), taskUUID(t, "task-1"))
	rig.queueTask(t, taskUUID(t, "retry"), "")
	rig.failed(t, "task-1", true)

	if got := len(rig.conn.streamFrames(t)); got != 1 {
		t.Fatalf("got %d stream frames, want 1 — the bubble was closed for an attempt whose "+
			"replacement is already queued", got)
	}

	// A second question arrives while the retry is running. It must get a bubble
	// of its own, and the clone must not be holding it.
	rig.ran(t, "REQ-R2", "task-2")
	if got := rig.streams.depth(); got != 2 {
		t.Fatalf("store holds %d bubbles, want 2 (the retried round and the new question)", got)
	}

	rig.answer(t, "the retry's answer", "retry")
	rig.answer(t, "the second answer", "task-2")

	frames := rig.conn.streamFrames(t)
	if len(frames) != 4 {
		t.Fatalf("got %d stream frames, want 4 (two opens, two seals)", len(frames))
	}
	if frames[2]["id"] != frames[0]["id"] || frames[2]["content"] != "the retry's answer" {
		t.Fatalf("the retry sealed %v with %q, want the first question's bubble %v",
			frames[2]["id"], frames[2]["content"], frames[0]["id"])
	}
	if frames[3]["id"] != frames[1]["id"] || frames[3]["content"] != "the second answer" {
		t.Fatalf("the second question's answer sealed %v with %q, want its own bubble %v",
			frames[3]["id"], frames[3]["content"], frames[1]["id"])
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Fatalf("%d answer(s) degraded to a plain message", len(pushes))
	}
}

// TestARetryCloneQueuedAfterTheFailureAlsoFindsItsRound is the same story with
// the two events the other way round, which is what a backoff child does: the
// deferred sweeper queues it seconds or minutes after the failure was
// published. The round is already waiting by then, and being the oldest one
// waiting is what makes the clone take it rather than the newer question's.
func TestARetryCloneQueuedAfterTheFailureAlsoFindsItsRound(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)

	rig.ran(t, "REQ-B1", "task-1")
	rig.failed(t, "task-1", true)

	// A new question opens a bubble of its own while the backoff runs.
	rig.ask(t, "REQ-B2")

	rig.q.fileRetryClone(t, taskUUID(t, "retry"), taskUUID(t, "task-1"))
	rig.queueTask(t, taskUUID(t, "retry"), "")

	rig.answer(t, "the retry's answer", "retry")

	frames := rig.conn.streamFrames(t)
	if len(frames) != 3 {
		t.Fatalf("got %d stream frames, want 3 (two opens, one seal)", len(frames))
	}
	if frames[2]["id"] != frames[0]["id"] {
		t.Fatalf("the retry sealed bubble %v, want the round it was replacing (%v) — it took the "+
			"new question's bubble instead", frames[2]["id"], frames[0]["id"])
	}
	if got := rig.streams.depth(); got != 1 {
		t.Fatalf("store holds %d bubbles, want 1 — the new question kept its own", got)
	}
}

// ---- 6. a flush that never became a run ----
//
// Covered by TestAFlushThatStartedNoRunClosesTheBubbleWithNoRun and
// TestASettledFlushLeavesARoundWaitingForItsRetry in
// stream_round_identity_test.go: OnSettled now carries no name, so it closes
// the session's oldest round that never became a run — and deliberately not one
// whose retry is already on the way.

// ---- the store's own arithmetic, from the outside ----

// A bubble that has not been bound yet still belongs to the session, and a
// session's rounds are answered in order. This pins the ordering bindNext
// depends on without reaching into the store: three questions, three runs
// queued in order, and the answers come back out of order on purpose.
func TestEachAnswerFindsItsOwnBubbleWhateverOrderTheyArriveIn(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)

	rig.ran(t, "REQ-1", "task-1")
	rig.ran(t, "REQ-2", "task-2")
	rig.ran(t, "REQ-3", "task-3")

	opened := rig.conn.streamFrames(t)
	if len(opened) != 3 {
		t.Fatalf("got %d opening frames, want 3", len(opened))
	}

	rig.answer(t, "third", "task-3")
	rig.answer(t, "first", "task-1")
	rig.answer(t, "second", "task-2")

	frames := rig.conn.streamFrames(t)
	if len(frames) != 6 {
		t.Fatalf("got %d stream frames, want 6 (three opens, three seals)", len(frames))
	}
	for i, want := range []struct {
		bubble int
		text   string
	}{{2, "third"}, {0, "first"}, {1, "second"}} {
		got := frames[3+i]
		if got["id"] != opened[want.bubble]["id"] || got["content"] != want.text {
			t.Fatalf("seal %d landed in bubble %v with %q, want bubble %v with %q",
				i, got["id"], got["content"], opened[want.bubble]["id"], want.text)
		}
	}
	if got := rig.streams.depth(); got != 0 {
		t.Fatalf("%d bubble(s) still spinning after every run answered", got)
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Fatalf("%d answer(s) degraded to a plain message", len(pushes))
	}
}

// OnSettled with nothing waiting is a no-op rather than a closer of whatever is
// on screen: a session whose only round has a run is a session whose flush
// produced a task, so there is nothing for this to close.
func TestASettledFlushWithNothingWaitingClosesNothing(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-ONLY", "task-1")

	rig.typing.OnSettled(context.Background(), bubbleSessionID(t))

	if got := len(rig.conn.streamFrames(t)); got != 1 {
		t.Fatalf("got %d stream frames, want 1 — a settled flush sealed a bubble whose run is "+
			"still going, and its answer now has nowhere to land", got)
	}
}
