package wecom

// stream_rotation_test.go — a run longer than one stream's window keeps a live
// bubble by moving onto a fresh stream on the same req_id.
//
// The stream's ten-minute window belongs to the stream, not to the req_id that
// carried it (streamMaxAge, measured 2026-08-09): sealing a stream and opening
// another on the same req_id puts a second, live bubble right under the first.
// So the guard hands the round over instead of ending it, and the answer lands
// wherever the round is by the time it arrives. Nothing is promised across the
// hand-over and nothing is owed: the new bubble is on screen before the old one
// is gone.

import (
	"context"
	"testing"
)

// The guard at nine minutes: the old stream is sealed with the hand-over line,
// a fresh stream is opened on the SAME req_id, the round stays on file, and
// the answer later lands in the new stream in place.
//
// REVERSE VERIFICATION: delete the seal call in fireGuard (write the opener
// only) and this fails on frame 2, which is then the opener rather than the
// hand-over; delete the opener write instead and it fails on frame 3.
func TestTheGuardRotatesTheRoundOntoAFreshStream(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-ROT", 1, "task-1")

	old, next := rig.rotated(t, 1)

	frames := rig.conn.streamFrames(t)
	if len(frames) != 3 {
		t.Fatalf("a rotation wrote %d stream frames, want 3 (open, hand-over, reopen): %v", len(frames), frames)
	}
	if frames[1]["id"] != old || frames[1]["finish"] != true || frames[1]["content"] != streamCopyContinued {
		t.Fatalf("frame 2 = %v, want the old stream %s sealed with %q", frames[1], old, streamCopyContinued)
	}
	if frames[2]["id"] != next || frames[2]["finish"] != false || frames[2]["content"] != streamThinkingPlaceholder {
		t.Fatalf("frame 3 = %v, want a fresh stream %s opened with the thinking placeholder", frames[2], next)
	}
	if reqIDs := rig.conn.streamReqIDs(); len(reqIDs) != 3 || reqIDs[1] != "REQ-ROT" || reqIDs[2] != "REQ-ROT" {
		t.Fatalf("the rotation echoed req_ids %v, want the callback's on every frame — WeCom refuses any other value", reqIDs)
	}
	if rig.streams.depth() != 1 {
		t.Fatalf("store holds %d rounds after the rotation, want 1 — the round was retired instead of moved", rig.streams.depth())
	}

	rig.answer(t, "the answer after nine minutes", "task-1")

	frames = rig.conn.streamFrames(t)
	if len(frames) != 4 {
		t.Fatalf("got %d stream frames after the answer, want 4", len(frames))
	}
	if frames[3]["id"] != next || frames[3]["finish"] != true || frames[3]["content"] != "the answer after nine minutes" {
		t.Fatalf("the answer did not seal the rotated bubble %s in place: %v", next, frames[3])
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Errorf("the answer went out as %d plain message(s) as well", len(pushes))
	}
	if rig.streams.depth() != 0 {
		t.Errorf("store holds %d rounds after the answer, want 0", rig.streams.depth())
	}
}

// Another nine minutes on, the guard fires again on the same round: the second
// stream is handed over the same way, and the answer lands in the third.
//
// REVERSE VERIFICATION: drop `next.CreatedAt = s.now()` in rotate and the
// second rotation is refused — the round is judged by the FIRST stream's
// window, which is over by then — so rotated fails on "still on stream". (The
// re-arm itself is the timer-driven test in stream_bubble_test.go, which waits
// for the second rotation.)
func TestTheGuardRotatesTheSameRoundAgain(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-ROT2", 1, "task-1")

	// The first guard, nine minutes in.
	rig.now = rig.now.Add(streamGuardAfter)
	first, second := rig.rotated(t, 1)
	// The second, nine minutes after that: eighteen minutes since the
	// question, past the FIRST stream's window and inside the second's. A
	// rotation that had not reset the window would refuse to move the round
	// now.
	rig.now = rig.now.Add(streamGuardAfter)
	second2, third := rig.rotated(t, 1)
	if second2 != second {
		t.Fatalf("the second rotation left stream %s, want %s", second2, second)
	}

	frames := rig.conn.streamFrames(t)
	if len(frames) != 5 {
		t.Fatalf("two rotations wrote %d stream frames, want 5: %v", len(frames), frames)
	}
	if frames[3]["id"] != second || frames[3]["finish"] != true || frames[3]["content"] != streamCopyContinued {
		t.Fatalf("frame 4 = %v, want the second stream %s sealed with %q", frames[3], second, streamCopyContinued)
	}
	if frames[4]["id"] != third || frames[4]["finish"] != false {
		t.Fatalf("frame 5 = %v, want a third stream %s opened", frames[4], third)
	}
	if first == second || second == third || first == third {
		t.Fatalf("stream ids repeat across rotations: %s %s %s", first, second, third)
	}

	rig.answer(t, "the answer after eighteen minutes", "task-1")
	frames = rig.conn.streamFrames(t)
	last := frames[len(frames)-1]
	if last["id"] != third || last["finish"] != true || last["content"] != "the answer after eighteen minutes" {
		t.Fatalf("the answer did not seal the third bubble %s in place: %v", third, last)
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Errorf("the answer went out as %d plain message(s) as well", len(pushes))
	}
}

// A hand-over the server refuses ends the rotating. The round is marked
// unusable: no further guard touches it, no refresh goes to it, and the answer
// goes out as a plain message.
//
// REVERSE VERIFICATION: delete the markUnusable call in fireGuard's
// streamUnusable branch. The second fireGuard then rotates again (two more
// frames), and the answer tries the bubble before falling back.
func TestARefusedHandOverStopsRotatingAndTheAnswerGoesPlain(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.conn.refuseClosingCode = errcodeStreamExpired
	rig.ran(t, "REQ-ROT3", 1, "task-1")
	sessionID := bubbleSessionID(t)

	rig.stepped(t, 1)
	rig.typing.fireGuard(context.Background(), sessionID, 1)
	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 || frames[1]["finish"] != true {
		t.Fatalf("a refused hand-over wrote %d stream frames, want 2 (open, the refused seal): %v", len(frames), frames)
	}
	if rig.streams.depth() != 1 {
		t.Fatalf("store holds %d rounds, want 1 — the round is kept for its addressing", rig.streams.depth())
	}

	// The guard again, as the re-armed timer would do if rotating had not
	// stopped. Nothing may be written.
	rig.typing.fireGuard(context.Background(), sessionID, 1)
	if got := len(rig.conn.streamFrames(t)); got != 2 {
		t.Fatalf("after a refused hand-over the guard wrote %d more frame(s); a stream the server has disowned is written to again", got-2)
	}

	rig.answer(t, "the answer", "task-1")
	if got := len(rig.conn.streamFrames(t)); got != 2 {
		t.Fatalf("the answer wrote %d frame(s) to a stream the server had refused; it would be refused and the user would never see it", got-2)
	}
	pushes := rig.conn.pushes(t)
	if len(pushes) != 1 || pushText(pushes[0]) != "the answer" {
		t.Fatalf("the answer did not arrive as a plain message: %v", pushes)
	}
}

// A round that was never painted has nothing to hand over, and the guard
// leaves it alone: no frame, no change.
func TestTheGuardLeavesAnUnpaintedRoundAlone(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	// The flush wins the race: the run is bound and no opening frame has been
	// written.
	rig.runStarted(t, 1, "task-1")

	rig.typing.fireGuard(context.Background(), bubbleSessionID(t), 1)

	if got := len(rig.conn.streamFrames(t)); got != 0 {
		t.Fatalf("the guard wrote %d frame(s) for a round with no bubble", got)
	}
	if !rig.streams.has(bubbleSessionID(t), taskUUID(t, "task-1")) {
		t.Fatal("the guard retired a round that still has an answer coming")
	}
}

// A run that has shown no sign of life since its stream opened is not handed a
// fresh bubble. The case that makes this matter: an agent archived on main
// cancels its runs without publishing task:cancelled, so nothing ever ends the
// round from outside — and a guard that rotated on the clock alone would open
// a new bubble in the chat every nine minutes for as long as the process
// lived. The quiet round keeps its stream until the server ends it.
//
// REVERSE VERIFICATION: drop the lastStep check in rotate and this fails with
// the round on a new stream and two frames on the wire.
func TestTheGuardLeavesAQuietRoundAlone(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-QUIET", 1, "task-1")
	before := rig.streamIDOf(t, 1)
	frames := len(rig.conn.streamFrames(t))

	rig.typing.fireGuard(context.Background(), bubbleSessionID(t), 1)

	if after := rig.streamIDOf(t, 1); after != before {
		t.Fatalf("a quiet round was rotated from %s to %s", before, after)
	}
	if n := len(rig.conn.streamFrames(t)); n != frames {
		t.Fatalf("%d stream frames after the guard, want %d: nothing is written for a quiet round", n, frames)
	}
	// A step brings it back to life for the next guard.
	rig.stepped(t, 1)
	rig.typing.fireGuard(context.Background(), bubbleSessionID(t), 1)
	if after := rig.streamIDOf(t, 1); after == before {
		t.Fatalf("the round stepped and was still not rotated")
	}
}

// The backstop: a run that keeps stepping is handed at most maxRotations fresh
// bubbles, then left to the window.
//
// REVERSE VERIFICATION: drop the rotations check in rotate and this fails with
// a rotation past the cap.
func TestRotationStopsAtItsCap(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-CAP", 1, "task-1")
	for i := 0; i < maxRotations; i++ {
		rig.rotated(t, 1)
	}
	before := rig.streamIDOf(t, 1)
	rig.stepped(t, 1)
	rig.typing.fireGuard(context.Background(), bubbleSessionID(t), 1)
	if after := rig.streamIDOf(t, 1); after != before {
		t.Fatalf("rotated past the cap of %d: %s -> %s", maxRotations, before, after)
	}
}
