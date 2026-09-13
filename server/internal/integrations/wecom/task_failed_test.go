package wecom

// task_failed_test.go — what a failed run says in the chat, and who says it.
//
// #7952 established the words: the platform's own redacted reason, behind a
// warning mark, the way DingTalk and Lark deliver theirs. It sent them from
// Outbound, because at the time nothing else was listening.
//
// Here the typing indicator sends them instead, and that is not a preference:
// a failed run has a bubble waiting on it, and the indicator is the only
// party that can seal it. Sending from Outbound as well would put two messages
// in the chat for one failure — the bubble's ending and a plain message under
// it — which is what TestAFailedRunProducesExactlyOneMessage guards.

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// failedWithReason publishes the task:failed FailTask broadcasts for a run
// whose failure carries the platform's redacted text.
func (r *bubbleRig) failedWithReason(t *testing.T, taskName, reason string) {
	t.Helper()
	id := taskUUID(t, taskName)
	r.bus.Publish(events.Event{
		Type:          protocol.EventTaskFailed,
		ChatSessionID: bubbleSession,
		TaskID:        id,
		Payload: map[string]any{
			"task_id":        id,
			"failure_reason": "provider_network",
			"retry_pending":  false,
			"error":          reason,
		},
	})
}

// The reason is the whole value of the notice: "上下文超出模型限制" tells a
// person what to do next where a generic line does not.
//
// REVERSE VERIFICATION: make failureText return copyFor(l).StreamFailed
// unconditionally and this fails with the generic line.
func TestAFailureCarriesThePlatformsOwnReason(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")

	rig.failedWithReason(t, "task-1", "上下文超出模型限制")

	got := pushedTexts(t, rig.conn)
	if len(got) != 1 || got[0] != "⚠️ 上下文超出模型限制" {
		t.Fatalf("the asker read %q, want exactly the platform's reason", got)
	}
}

// The same reason, written into the bubble rather than under it, when the
// round still has one.
func TestAFailureWithAReasonSealsTheBubbleWithIt(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")
	rig.ran(t, "REQ-1", 1, "task-1")

	rig.failedWithReason(t, "task-1", "上下文超出模型限制")

	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 || frames[1]["finish"] != true || frames[1]["content"] != "⚠️ 上下文超出模型限制" {
		t.Fatalf("the bubble was left as %v, want it sealed with the platform's reason", frames)
	}
	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Fatalf("the reason ALSO went out as a plain message %q — the asker reads the same failure twice", got)
	}
}

// A failure that arrives with no reason still ends the turn. This is the one
// place this tree departs from #7952, which stayed silent: silence is fine
// when nothing is waiting, and wrong here, because the bubble the question
// opened would spin until the platform's own window closed it.
func TestAFailureWithNoReasonStillEndsTheTurn(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")

	rig.failed(t, "task-1", false)

	got := pushedTexts(t, rig.conn)
	if len(got) != 1 || got[0] != streamCopyFailed {
		t.Fatalf("the asker read %q, want the generic ending %q", got, streamCopyFailed)
	}
}

// An attempt the platform is already retrying is not an ending: its
// replacement is queued, and the retry's answer would land underneath a
// bubble that had already declared failure.
func TestARetryPendingFailureSaysNothing(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")
	rig.ran(t, "REQ-1", 1, "task-1")

	rig.failed(t, "task-1", true)

	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Fatalf("the asker read %q for an attempt the platform is already retrying", got)
	}
	frames := rig.conn.streamFrames(t)
	if len(frames) != 1 {
		t.Fatalf("%d stream frames, want 1 — the bubble stays open for the attempt that reports the real outcome", len(frames))
	}
}

// The guard on the arrangement itself: exactly one party announces a failure.
// Subscribing in Outbound as well — which is how #7952 shipped upstream, and
// what a merge from main re-introduces — produces the bubble's ending AND a
// plain message under it.
//
// REVERSE VERIFICATION: add `bus.Subscribe(protocol.EventTaskFailed,
// o.handleEvent)` back to Outbound.Register and this fails with two.
func TestAFailedRunProducesExactlyOneMessage(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	// BOTH subscribers on one bus, the way router.go wires them. Without the
	// Outbound registration this test cannot see the defect it exists for:
	// the indicator alone will always produce exactly one message.
	rig.out.Register(rig.bus)
	rig.askedInTheRoom(t, "task-1")
	rig.ran(t, "REQ-1", 1, "task-1")

	rig.failedWithReason(t, "task-1", "上下文超出模型限制")

	// Counting subscribers rather than messages, because the message count
	// depends on which handler the bus reaches first: with Outbound ahead of
	// the indicator it takes the round and seals the bubble with its
	// "nothing to say" copy, and the indicator then sends the failure under
	// it; the other way round Outbound finds the round gone and says nothing.
	// One of those orders is silent, so only the subscription itself is a
	// stable statement of the rule.
	if n := rig.bus.SubscriberCount(protocol.EventTaskFailed); n != 1 {
		t.Fatalf("task:failed has %d subscribers, want exactly 1 — the run's ending has one owner, "+
			"and a second announcer is a second message in somebody's chat", n)
	}
	sealed := 0
	for _, f := range rig.conn.streamFrames(t) {
		if f["finish"] == true {
			sealed++
		}
	}
	if total := sealed + len(pushedTexts(t, rig.conn)); total != 1 {
		t.Fatalf("one failure produced %d message(s) in the chat (%d sealed bubbles, %d plain), want 1",
			total, sealed, len(pushedTexts(t, rig.conn)))
	}
}
