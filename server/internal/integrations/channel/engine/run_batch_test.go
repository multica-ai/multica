package engine

// run_batch_test.go — the contract a per-run indicator rests on: the engine
// tells a platform which run a message belongs to, and the number of runs it
// names is exactly the number of runs it creates.
//
// A platform that has to show one affordance per run (WeCom paints a streaming
// bubble at ingest and replaces it with the answer) cannot work this out for
// itself. The only other way to know is to re-measure the gap between two
// messages against the same window the debouncer uses — a second clock, read
// on a goroutine the Router detached, against a decision already taken here.
// Near the boundary the two disagree, and the disagreement is silent and
// user-visible in both directions: two affordances for one run leaves one that
// nothing ever closes, and one affordance for two runs lets the first answer
// seal the bubble the user attached their second question to. These tests pin
// the ids to the flushes so that cannot drift.

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
)

// TestRunBatch_ScheduleKeepsOneIdPerFlush drives the batcher directly across a
// re-arm and a fire. The id is minted per BATCH, not per Schedule call: a
// message that lands in an armed window is told the id already collecting, and
// only a message that finds no window gets the one it brought.
func TestRunBatch_ScheduleKeepsOneIdPerFlush(t *testing.T) {
	f := &fakeTimerFactory{}
	b := newTestBatcher(f)

	var flushed []RunBatchID
	record := func(id RunBatchID) { flushed = append(flushed, id) }

	first := b.Schedule("s", 10, record)
	joined := b.Schedule("s", 11, record)
	if first != 10 {
		t.Fatalf("the first message of a window got batch %d, want the id it brought (10)", first)
	}
	if joined != first {
		t.Fatalf("a message that re-armed the window got batch %d, want %d — two ids for the one run the flush will answer", joined, first)
	}

	f.fireArmed()
	if len(flushed) != 1 || flushed[0] != first {
		t.Fatalf("flushes = %v, want exactly one, answering batch %d", flushed, first)
	}

	next := b.Schedule("s", 12, record)
	if next == first {
		t.Fatalf("a message arriving after the flush got batch %d again; its own run would share the previous run's affordance", next)
	}
	f.fireArmed()
	if len(flushed) != 2 || flushed[1] != next {
		t.Fatalf("flushes = %v, want a second one answering batch %d", flushed, next)
	}
}

// TestRunBatch_IngestIdsMatchTheFlushesOnBothSidesOfTheBoundary is the
// invariant itself, end to end through the Router: however two messages fall
// either side of the window, the number of DISTINCT batch ids the Router
// reports at ingest equals the number of runs it goes on to create.
//
// The two orderings are the boundary from both sides. They differ only in
// whether the timer fires between the two Handle calls, which is exactly the
// jitter a second clock cannot resolve.
func TestRunBatch_IngestIdsMatchTheFlushesOnBothSidesOfTheBoundary(t *testing.T) {
	cases := []struct {
		name string
		// fireBetween is whether the debounce window expires between the two
		// messages: false = the batcher merges them, true = it splits them.
		fireBetween bool
		wantRuns    int
	}{
		{name: "second message lands just inside the window", fireBetween: false, wantRuns: 1},
		{name: "second message lands just past the window", fireBetween: true, wantRuns: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			timers := &fakeTimerFactory{}
			h.router.batcher = newTestBatcher(timers)

			first := p2pMessage(t)
			first.MessageID = "m1"
			if err := h.router.Handle(context.Background(), first); err != nil {
				t.Fatalf("first Handle: %v", err)
			}
			if !waitFor(time.Second, func() bool { return h.typing.calls() == 1 }) {
				t.Fatal("the first message was never reported to the typing indicator")
			}
			if tc.fireBetween {
				timers.fireArmed()
			}

			second := p2pMessage(t)
			second.MessageID = "m2"
			if err := h.router.Handle(context.Background(), second); err != nil {
				t.Fatalf("second Handle: %v", err)
			}
			if !waitFor(time.Second, func() bool { return h.typing.calls() == 2 }) {
				t.Fatal("the second message was never reported to the typing indicator")
			}
			timers.fireArmed()
			if !waitFor(time.Second, func() bool { return h.tasks.calls() == tc.wantRuns }) {
				t.Fatalf("the batcher created %d runs, want %d", h.tasks.calls(), tc.wantRuns)
			}

			ingested := h.typing.ingested()
			distinct := map[RunBatchID]bool{}
			for _, id := range ingested {
				if id == 0 {
					t.Fatalf("an ingested message was reported with no batch at all: %v", ingested)
				}
				distinct[id] = true
			}
			if len(distinct) != tc.wantRuns {
				t.Fatalf("the Router named %d run(s) at ingest (%v) but created %d — "+
					"a per-run indicator would show %d affordance(s) for %d run(s)",
					len(distinct), ingested, tc.wantRuns, len(distinct), tc.wantRuns)
			}

			// And every run reported back names one of those same ids, so an
			// indicator opened at ingest can be matched to the run that
			// answers it without inferring anything from order.
			started := h.typing.started()
			if len(started) != tc.wantRuns {
				t.Fatalf("runs reported to the indicator = %v, want %d", started, tc.wantRuns)
			}
			for _, id := range started {
				if !distinct[id] {
					t.Fatalf("run reported under batch %d, which no ingested message was told about (%v)", id, ingested)
				}
			}
		})
	}
}

// TestRunBatch_FlushNamesTheTaskItCreated pins the other half of the binding:
// the flush hands over the task id, so every later task lifecycle event can be
// matched to the affordance the question opened rather than to whichever one
// happens to be at the head of the queue.
func TestRunBatch_FlushNamesTheTaskItCreated(t *testing.T) {
	h := newHarness(t)
	timers := &fakeTimerFactory{}
	h.router.batcher = newTestBatcher(timers)

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !waitFor(time.Second, func() bool { return h.typing.calls() == 1 }) {
		t.Fatal("the message was never reported to the typing indicator")
	}
	timers.fireArmed()
	if !waitFor(time.Second, func() bool { return len(h.typing.started()) == 1 }) {
		t.Fatal("the flush created a run without naming it to the typing indicator")
	}

	batch := h.typing.started()[0]
	if got := h.typing.ingested()[0]; got != batch {
		t.Fatalf("the run was reported under batch %d but its message was ingested under %d", batch, got)
	}
	h.typing.mu.Lock()
	bound := h.typing.boundTasks[batch]
	h.typing.mu.Unlock()
	if !bound.Valid {
		t.Fatal("the flush reported no task id, so an ending could only be matched to its round by guessing")
	}
	if enqueued := h.tasks.enqueuedIDs(); len(enqueued) != 1 || bound != enqueued[0] {
		t.Fatalf("the indicator was bound to task %v, want the run the flush actually enqueued %v", bound, enqueued)
	}
}

// TestRunBatch_SettledFlushNamesItsBatch covers the other outcome of a flush.
// A run trigger that enqueued nothing publishes no task lifecycle event at
// all, so this call is the only chance to clear that batch's affordance — and
// it has to say WHICH batch, or a session with a queued round behind it clears
// the wrong one.
func TestRunBatch_SettledFlushNamesItsBatch(t *testing.T) {
	h := newHarness(t)
	timers := &fakeTimerFactory{}
	h.router.batcher = newTestBatcher(timers)
	h.tasks.err = service.ErrChatTaskAgentNoRuntime

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !waitFor(time.Second, func() bool { return h.typing.calls() == 1 }) {
		t.Fatal("the message was never reported to the typing indicator")
	}
	timers.fireArmed()
	if !waitFor(time.Second, func() bool { return len(h.typing.settledOn()) == 1 }) {
		t.Fatal("a flush that enqueued nothing never cleared its batch")
	}
	if got, want := h.typing.settledOn()[0], h.typing.ingested()[0]; got != want {
		t.Fatalf("the settled batch was %d, want %d — the batch the message was ingested under", got, want)
	}
}

// TestRunBatch_StartedChatNamesItsRun covers the one path that never reaches
// the debouncer: `/new <body>` commits its task inside StartSession's own
// transaction, so no flush is ever armed and no flush ever mints an id. The
// Router still reports the message as scheduled, so the platform is told to
// open an affordance — and both halves have to come from here instead.
//
// Nothing else in the suite fails when they are missing. Without the id the
// affordance never opens (a zero batch is dropped downstream); without the
// binding it opens and never closes, because every ending is matched to a
// round by task id.
func TestRunBatch_StartedChatNamesItsRun(t *testing.T) {
	h := newHarness(t)
	h.media.noMedia = true
	msg := p2pMessage(t)
	msg.Text = "/new look at the deploy"

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !waitFor(time.Second, func() bool { return h.typing.calls() == 1 }) {
		t.Fatal("a /new that started a run was never reported to the typing indicator")
	}

	batch := h.typing.ingested()[0]
	if batch == 0 {
		t.Fatal("the started run was ingested with no batch at all, so no affordance can open for it")
	}
	h.typing.mu.Lock()
	bound := h.typing.boundTasks[batch]
	h.typing.mu.Unlock()
	if !bound.Valid {
		t.Fatalf("batch %d named no task, so its affordance could never be closed by an ending", batch)
	}
	if enqueued := h.tasks.enqueuedIDs(); len(enqueued) != 1 || bound != enqueued[0] {
		t.Fatalf("the indicator was bound to task %v, want the task /new actually committed %v", bound, enqueued)
	}
}
