package wecom

// stream_cancelled_test.go — a cancelled run has to close the bubble it opened.
//
// The bubble is a promise that an answer is coming. task:failed was the only
// ending subscribed for, and it is not the only ending there is: the engine
// cancels a turn it cannot serve and publishes task:cancelled, which nothing
// here was listening to. The spinner then outlived the run by design — until
// the five-minute guard replaced it with "still working, I'll reply
// separately", a promise about a run that had already been abandoned.
//
// These drive a real events.Bus from the publisher's side, the way the engine
// does, so they fail if the subscription is missing rather than if a helper is.

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Both endings mean the same thing to the person watching: no answer is
// coming. Running them as one table is what stops the cancelled case being
// treated as the special one — they share a handler and must share a fate.
func TestARunThatEndsWithoutAnAnswerClosesTheBubble(t *testing.T) {
	for _, ending := range []string{protocol.EventTaskFailed, protocol.EventTaskCancelled} {
		t.Run(ending, func(t *testing.T) {
			rig := newBubbleRig(t)
			bus := events.New()
			rig.typing.Register(bus)

			rig.ask(t, "REQ-X")
			if n := len(rig.conn.streamFrames(t)); n != 1 {
				t.Fatalf("the question painted %d frames, want the 1 opening bubble", n)
			}

			bus.Publish(events.Event{
				Type:          ending,
				ChatSessionID: bubbleSession,
				TaskID:        "task-1",
			})

			frames := rig.conn.streamFrames(t)
			if len(frames) != 2 {
				t.Fatalf("a %s run produced %d frames, want 2 (the bubble and its closing frame) — "+
					"nothing subscribes to %s, so the spinner runs on for an answer nobody is producing",
					ending, len(frames), ending)
			}
			closing := frames[1]
			if closing["finish"] != true {
				t.Errorf("the second frame does not seal the bubble: %v", closing)
			}
			if content, _ := closing["content"].(string); content != streamCopyFailed {
				t.Errorf("the closing frame says %q, want %q — a bubble that closes on empty text is one WeCom discards, "+
					"and the spinner never stops", content, streamCopyFailed)
			}
		})
	}
}
