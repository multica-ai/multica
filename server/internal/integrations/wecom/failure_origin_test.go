package wecom

// failure_origin_test.go — a chat_session bound to a WeCom room is not
// exclusively WeCom's, and that is as true of a run that failed as of one
// that answered.
//
// The engine makes the INSTALLER the creator of a group's chat_session, so
// that session appears in their own Multica chat list. They can open it in a
// browser and ask the agent something. Both runs die the same way — one
// task:failed on the shared bus, carrying the same chat_session_id — and
// nothing in the event says which surface asked. Without the question,
// sayTheRunFailed resolves the room off the binding row and announces, in
// front of everyone in it, that something they never saw has gone wrong.
//
// The first two tests are the pair that matters: the same event, the same
// session, opposite verdicts, decided only by where the question came from.
// The rest pin the branches where the origin cannot be established, because
// the cost of guessing runs both ways — a wrong yes puts a line in a room it
// did not belong in, a wrong no leaves a WeCom user sitting on the guard's
// "还在处理，完成后我再单独回复你" forever.

import (
	"errors"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// newBoundRoomRig is a bubbleRig whose manager also holds the binding row —
// the address of last resort for a failure notice, and the one the leak
// travels down. newBubbleRig leaves it nil because its own tests are about the
// rounds this process holds; here it is the whole point.
func newBoundRoomRig(t *testing.T) *bubbleRig {
	t.Helper()
	rig := newBubbleRig(t)
	rig.typing = NewTypingIndicator(TypingIndicatorConfig{
		Senders:  rig.senders,
		Streams:  rig.streams,
		Tasks:    rig.q,
		Bindings: rig.q,
		// No guard: these tests drive the endings themselves.
		GuardAfter: -1,
	})
	rig.bus = events.New()
	rig.typing.Register(rig.bus)
	return rig
}

// askedInTheBrowser files a task row for a run the installer started in the
// web UI: it owns its own input batch, like every direct task since MUL-4351,
// and the messages in that batch carry no channel_ingested stamp.
func (r *bubbleRig) askedInTheBrowser(t *testing.T, taskName string) {
	t.Helper()
	r.q.fileTask(t, taskUUID(t, taskName))
	r.q.channelIngested = askedInTheWebUI()
}

// askedInTheRoom files the same row for a question typed in WeCom. The stamp
// is stated here rather than left to the fake: this is the control the first
// test is read against, and a control that only holds because of a default is
// not one.
func (r *bubbleRig) askedInTheRoom(t *testing.T, taskName string) {
	t.Helper()
	r.q.fileTask(t, taskUUID(t, taskName))
	r.q.channelIngested = askedOverWecom()
}

// pushedTexts is what the room actually read: the markdown of every
// aibot_send_msg the connection was asked to write.
func pushedTexts(t *testing.T, c *bubbleConn) []string {
	t.Helper()
	var out []string
	for _, body := range c.pushes(t) {
		md, _ := body["markdown"].(map[string]any)
		if md == nil {
			out = append(out, "")
			continue
		}
		s, _ := md["content"].(string)
		out = append(out, s)
	}
	return out
}

// TestAWebUIRunsFailureIsNotAnnouncedInTheRoom is the fix.
//
// Nobody in the room asked anything. The installer asked in a browser and that
// run failed, and the only thing tying it to WeCom is a binding row on the
// session they share.
func TestAWebUIRunsFailureIsNotAnnouncedInTheRoom(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheBrowser(t, "task-1")

	rig.failed(t, "task-1", false)

	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Fatalf("the room was told %q about a run nobody in it started — everyone in the chat "+
			"just learned that a question they never saw had gone wrong", got)
	}
	if frames := rig.conn.streamFrames(t); len(frames) != 0 {
		t.Fatalf("the room got %d stream frames for a browser run's failure, want none", len(frames))
	}
}

// The control, and the direction that costs more to get wrong. The round was
// asked in WeCom, ran past the stream window, and its bubble was closed on the
// guard's promise of a separate reply. This notice IS that reply — the only
// "that run did not go through" WeCom ever produces.
func TestAWecomRunsFailureStillReachesTheAskerAfterThePromise(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")
	rig.ran(t, "REQ-1", 1, "task-1")
	rig.guardClosed(t, 1) // "还在处理，完成后我再单独回复你"

	rig.failed(t, "task-1", false)

	got := pushedTexts(t, rig.conn)
	if len(got) != 1 || got[0] != streamCopyFailed {
		t.Fatalf("the asker read %q, want exactly [%q] — they were promised a separate reply "+
			"and the failure of their own question never arrived", got, streamCopyFailed)
	}
}

// The same control one step earlier: the bubble is still open, so the notice
// goes into it rather than under it. A gate that refuses a WeCom round leaves
// this bubble spinning with no ending at all.
func TestAWecomRunsFailureStillClosesTheBubbleItOpened(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")
	rig.ran(t, "REQ-1", 1, "task-1")

	rig.failed(t, "task-1", false)

	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 || frames[1]["finish"] != true || frames[1]["content"] != streamCopyFailed {
		t.Fatalf("the bubble was left as %v, want it sealed with %q — the asker is watching a "+
			"spinner for a run that is already dead", frames, streamCopyFailed)
	}
}

// The gate must not seal a WeCom round's bubble with a web run's ending. It
// runs before the take for exactly this: the room has a live question of its
// own, and the browser run's failure has no business ending it.
func TestAWebUIRunsFailureLeavesTheRoomsOwnBubbleAlone(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")
	rig.ran(t, "REQ-1", 1, "task-1") // the room's own question, still running
	rig.askedInTheBrowser(t, "task-2")

	rig.failed(t, "task-2", false)

	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Fatalf("the room was told %q about a browser run", got)
	}
	if frames := rig.conn.streamFrames(t); len(frames) != 1 {
		t.Fatalf("the room's bubble went from 1 frame to %d — a browser run's failure wrote "+
			"into the bubble the room's own question is still waiting on", len(frames))
	}
	if rig.streams.depth() != 1 {
		t.Fatalf("the room's round is gone (depth %d) — its own answer now has nowhere to land",
			rig.streams.depth())
	}
}

// ---- where the origin cannot be established ----
//
// Every one of these delivers. The asymmetry with the answer path is the
// point: an answer held back is still readable in Multica, and a failure
// notice held back is a promise broken in silence.

// A task:failed with no task id on it cannot be attributed at all.
func TestAFailureWithNoTaskIDIsStillDelivered(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.q.channelIngested = askedInTheWebUI() // would refuse, if it were ever asked

	rig.bus.Publish(events.Event{
		Type:          protocol.EventTaskFailed,
		ChatSessionID: bubbleSession,
		Payload:       map[string]any{"failure_reason": "provider_network"},
	})

	if got := pushedTexts(t, rig.conn); len(got) != 1 || got[0] != streamCopyFailed {
		t.Fatalf("the asker read %q, want [%q] — an unattributable failure was swallowed",
			got, streamCopyFailed)
	}
}

// The task row is gone — cancelled and reaped while its failure was in flight.
// The answer path drops a reply here; a failure notice still goes out, because
// the round on the other side may be holding the guard's promise.
func TestAVanishedTaskRowStillDeliversTheFailure(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.q.channelIngested = askedInTheWebUI() // no row to ask about, so this never applies

	rig.failed(t, "task-1", false) // rig.q.tasks holds no row for it

	if got := pushedTexts(t, rig.conn); len(got) != 1 || got[0] != streamCopyFailed {
		t.Fatalf("the asker read %q, want [%q] — a run whose row had been reaped went unreported",
			got, streamCopyFailed)
	}
}

// The database did not answer. Same call: a lookup that failed is not a
// verdict that the room should hear nothing.
func TestAnUnreadableOriginStillDeliversTheFailure(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")
	rig.q.originErr = errors.New("connection refused")

	rig.failed(t, "task-1", false)

	if got := pushedTexts(t, rig.conn); len(got) != 1 || got[0] != streamCopyFailed {
		t.Fatalf("the asker read %q, want [%q] — a failed origin lookup was treated as a refusal",
			got, streamCopyFailed)
	}
}

// The verdict is read off the batch OWNER, not off the task that failed. An
// auto-retry clone's own id owns no messages, so asking about it would answer
// "not from the channel" and silence the failure of every WeCom question long
// enough to be retried.
func TestTheOriginOfARetryCloneIsItsParentsBatch(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")
	// FailTask's retry child: fresh id, inheriting the parent's input batch.
	rig.q.fileRetryClone(t, taskUUID(t, "retry"), taskUUID(t, "task-1"))

	rig.failed(t, "retry", false)

	asked := rig.q.originAsked()
	if len(asked) != 1 || asked[0] != taskUUID(t, "task-1") {
		t.Fatalf("the origin was asked about %v, want [%s] — a retry's failure is judged on the "+
			"question that started it, not on the clone's own empty batch",
			asked, taskUUID(t, "task-1"))
	}
	if got := pushedTexts(t, rig.conn); len(got) != 1 || got[0] != streamCopyFailed {
		t.Fatalf("the asker read %q, want [%q]", got, streamCopyFailed)
	}
}
