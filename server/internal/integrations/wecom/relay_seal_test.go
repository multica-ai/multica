package wecom

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

// ---- every ending reaches the bubble, including the ones with no words ----
//
// The relay could carry a reply that HAS words. Every other ending — a
// cancellation, a completion with nothing to say, an answer that is only files
// — had no way to reach the replica holding the bubble, so off-lease it left a
// spinner claiming work was in progress for the rest of the protocol's window.
//
// Nothing ended it either: the sweep deletes map entries without writing a
// frame, OnSettled needs an unbound round, and the next question opens its own
// bubble. The user watched it for the full window.
//
// These are the three, plus the two rules that keep the fix from becoming a
// worse bug than the gap.

func TestACancellationOffLeaseIsRoutedToTheReplicaHoldingTheBubble(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	relay := &recordingRelay{}
	rig.typing.WithRelay(relay)
	rig.senders.clear(rig.instID, rig.conn.sender)
	rig.askedInTheRoom(t, "task-1")

	rig.cancelled(t, "task-1")

	if len(relay.frames) != 1 {
		t.Fatalf("%d frames routed, want 1 — the bubble on the holder keeps turning for a run "+
			"that is already over, and nothing else will ever close it", len(relay.frames))
	}
	f := relay.frames[0]
	if f.Kind != relayKindSeal {
		t.Fatalf("routed kind %q, want %q — an ordinary reply whose round is gone falls through "+
			"to a plain push, so one cancel-all would put a message in every chat",
			f.Kind, relayKindSeal)
	}
	if f.SealReason != sealReasonCancelled {
		t.Errorf("seal reason = %q, want %q", f.SealReason, sealReasonCancelled)
	}
	if f.Content != "" {
		t.Errorf("the frame carries the words %q; they are the round's and the round's locale is "+
			"on the holder", f.Content)
	}
	if f.TaskID != taskUUID(t, "task-1") {
		t.Errorf("frame names task %q, want %q — without it the holder cannot find the round",
			f.TaskID, taskUUID(t, "task-1"))
	}
}

// The other cancellation exit: this replica holds rounds, just not this one.
func TestACancellationForASiblingsRoundIsRoutedToo(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	relay := &recordingRelay{}
	rig.typing.WithRelay(relay)
	rig.askedInTheRoom(t, "task-1")
	rig.ask(t, "REQ-OTHER") // a round of somebody else's, so holding() is true

	rig.cancelled(t, "task-1")

	if len(relay.frames) != 1 {
		t.Fatalf("%d frames routed, want 1 — holding another installation's round is not the same "+
			"as holding this one", len(relay.frames))
	}
	if got := relay.frames[0].Kind; got != relayKindSeal {
		t.Errorf("routed kind %q, want %q", got, relayKindSeal)
	}
}

// A completion with no words and no file. The turn is over; the bubble is not.
func TestACompletionWithNothingToSayStillClosesTheBubbleElsewhere(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	relay := &recordingRelay{}
	rig.out = NewOutbound(rig.q, rig.senders, rig.streams, nil, WithRelay(relay))
	rig.q.fileTask(t, taskUUID(t, "task-1"))
	rig.queueTask(t, taskUUID(t, "task-1"), "")

	rig.answer(t, "", "task-1")

	if len(relay.frames) != 1 {
		t.Fatalf("%d frames routed, want 1 — an empty completion is an ending too, and off-lease "+
			"nothing else tells the holder the round is over", len(relay.frames))
	}
	f := relay.frames[0]
	if f.Kind != relayKindSeal || f.SealReason != sealReasonNoReply {
		t.Fatalf("routed %+v, want a %q frame reasoned %q", f, relayKindSeal, sealReasonNoReply)
	}
}

// ---- and the two rules that keep the fix honest ----

// A seal frame whose round is not here does NOTHING. This is the whole reason
// it is its own kind: a reply with an empty body would fall through to an
// ordinary push, and one cancel-all click would put 这次处理已取消 into every
// chat in the deployment.
func TestASealFrameWithNoRoundHereSaysNothingAtAll(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)

	// The installation and chat ARE addressable, so a frame that fell through to
	// the reply path would land in the room. Without them deliverRelayed returns
	// at the id parse and this case passes for the wrong reason — it did, until
	// reverse-verifying it against a seal-turned-reply stayed green.
	res := rig.out.deliverRelayed(context.Background(), relayFrame{
		Kind:           relayKindSeal,
		SealReason:     sealReasonCancelled,
		InstallationID: util.UUIDToString(rig.instID),
		ChatID:         "CHAT_1",
		ChatType:       chatTypeGroupInt,
		TaskID:         taskUUID(t, "task-1"),
		SessionID:      bubbleSession,
	})
	if res.outcome != outcomeDone {
		t.Fatalf("outcome = %v, want outcomeDone — a seal with no round is finished, not retryable", res.outcome)
	}
	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Fatalf("a seal frame with no round put %q in the chat", got)
	}
	if frames := rig.conn.streamFrames(t); len(frames) != 0 {
		t.Fatalf("a seal frame with no round wrote %d stream frame(s)", len(frames))
	}
}

// An answer that is only files ends the round as well. The take used to be
// gated on the frame having words, so the file arrived and the spinner above it
// kept turning.
func TestARelayedAttachmentOnlyAnswerStillClosesTheBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-FILES", "task-1")

	rig.out.deliverRelayed(context.Background(), relayFrame{
		Kind:           relayKindReply,
		InstallationID: util.UUIDToString(rig.instID),
		ChatID:         "CHAT_1",
		ChatType:       chatTypeGroupInt,
		Content:        "",
		CarriesFiles:   true,
		TaskID:         taskUUID(t, "task-1"),
		SessionID:      bubbleSession,
	})

	sealed := ""
	for _, f := range rig.conn.streamFrames(t) {
		if f["finish"] == true {
			sealed, _ = f["content"].(string)
		}
	}
	if sealed == "" {
		t.Fatal("the round was never closed — the file arrived and the spinner above it kept turning")
	}
	if want := copyFor(DefaultLocale).StreamNoReplyWithFiles; sealed != want {
		t.Errorf("sealed with %q, want %q — the copy is the round's, in the round's own locale", sealed, want)
	}
}
