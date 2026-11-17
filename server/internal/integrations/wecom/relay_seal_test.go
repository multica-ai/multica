package wecom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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

// ---- and the frame has to get there ----

// The tests above check each end on its own: the publisher routes a seal, and
// a seal handed to deliverRelayed closes the round. What lies between them is
// the relay, and it addressed every frame by installation: the stream reader
// drops a frame with no scope id, and perform asks whether this replica owns
// that installation's socket before anything else. A seal carries no
// installation, so it reached nobody. These run both processes joined by the
// relay itself.

// sealPair is two processes joined by the relay: holder painted the bubble for
// task-1, elsewhere holds no round and no socket, and the ending lands there.
type sealPair struct {
	holder, elsewhere *bubbleRig
	dedupe            *sharedDedupe
}

func newSealPair(t *testing.T) sealPair {
	t.Helper()
	p := sealPair{holder: newBubbleRig(t), elsewhere: newBoundRoomRig(t), dedupe: newSharedDedupe()}
	p.holder.ran(t, "REQ-1", "task-1")
	p.elsewhere.senders.clear(p.elsewhere.instID, p.elsewhere.conn.sender)
	p.elsewhere.askedInTheRoom(t, "task-1")

	fan := &fanoutRelay{}
	for _, rig := range []*bubbleRig{p.holder, p.elsewhere} {
		router := NewRelayOutbound(fan, p.dedupe, RelayConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		fan.register(router)
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(func() { cancel(); router.Wait() })
		router.Start(ctx)
		router.Attach(rig.out)
		if rig == p.elsewhere {
			rig.typing.WithRelay(router)
			rig.out = NewOutbound(rig.q, rig.senders, rig.streams, nil, WithRelay(router))
		}
	}
	return p
}

// sealedWith waits for the holder's bubble to close and returns its last words.
func (p sealPair) sealedWith(t *testing.T) string {
	t.Helper()
	sealed := ""
	waitFor(t, "the holder to close the bubble", func() bool {
		for _, f := range p.holder.conn.streamFrames(t) {
			if f["finish"] == true {
				sealed, _ = f["content"].(string)
			}
		}
		return sealed != ""
	})
	return sealed
}

// REVERSE VERIFICATION: publish the seal under its installation again, or put
// it back behind the ownership gate, and the bubble is never closed. Put it
// back behind the claim and no claim is the assertion that fails.
func TestACancellationElsewhereClosesTheBubbleThroughTheRelay(t *testing.T) {
	t.Parallel()
	p := newSealPair(t)

	p.elsewhere.cancelled(t, "task-1")

	if got, want := p.sealedWith(t), copyFor(DefaultLocale).StreamCancelled; got != want {
		t.Errorf("sealed with %q, want %q", got, want)
	}
	if n := p.dedupe.claimCount(); n != 0 {
		t.Errorf("the seal took %d claim(s) — claimed once for the deployment, a replica without the "+
			"round can win it and the holder never closes the bubble", n)
	}
	if got := pushedTexts(t, p.elsewhere.conn); len(got) != 0 {
		t.Errorf("the replica without the round said %q", got)
	}
}

// The other wordless ending that is routed: a completion with nothing to say.
func TestACompletionWithNothingToSayElsewhereClosesTheBubbleThroughTheRelay(t *testing.T) {
	t.Parallel()
	p := newSealPair(t)
	p.elsewhere.queueTask(t, taskUUID(t, "task-1"), "")

	p.elsewhere.answer(t, "", "task-1")

	if got, want := p.sealedWith(t), wordlessSealCopy(DefaultLocale, false); got != want {
		t.Errorf("sealed with %q, want %q", got, want)
	}
	if n := p.dedupe.claimCount(); n != 0 {
		t.Errorf("the seal took %d claim(s)", n)
	}
}

// A seal carries no installation, so dispatching it by installation put every
// seal in the deployment on one shard queue. A cancel-all burst bigger than
// that one queue was shed, and those bubbles kept spinning.
//
// REVERSE VERIFICATION: dispatch seals by InstallationID again and the burst
// lands on one queue, which overflows.
func TestABurstOfSealsIsSpreadAcrossTheDispatchQueues(t *testing.T) {
	t.Parallel()
	const shards, depth = 8, 4
	r := NewRelayOutbound(nil, nil, RelayConfig{Shards: shards, QueueDepth: depth},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Not started: frames stay in the queues where they can be counted.
	const n = shards * depth / 2
	for i := 0; i < n; i++ {
		frame, err := json.Marshal(relayFrame{
			Kind:       relayKindSeal,
			SealReason: sealReasonCancelled,
			TaskID:     fmt.Sprintf("00000000-0000-0000-0000-%012d", i),
		})
		if err != nil {
			t.Fatal(err)
		}
		r.DeliverWecomOutbound("", frame, fmt.Sprintf("seal-%d", i))
	}
	queued, used := 0, 0
	for _, q := range r.queues {
		queued += len(q)
		if len(q) > 0 {
			used++
		}
	}
	if queued != n {
		t.Errorf("%d of %d seals were queued, the rest shed", queued, n)
	}
	if used < 2 {
		t.Errorf("all seals went to %d queue(s); they should spread by task id", used)
	}
}
