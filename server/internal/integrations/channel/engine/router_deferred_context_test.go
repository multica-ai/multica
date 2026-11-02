package engine

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestRouterDeferredContextFinalization(t *testing.T) {
	for _, outcome := range []string{"success", "timeout", "shutdown"} {
		t.Run(outcome, func(t *testing.T) {
			h := newHarness(t)
			budget := time.Second
			if outcome == "timeout" {
				budget = 30 * time.Millisecond
			}
			h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{MediaTimeout: budget, Logger: discardLogger()})
			started := make(chan struct{})
			h.media.resolve = func(ctx context.Context, msg channel.InboundMessage) channel.InboundMessage {
				close(started)
				if outcome != "success" {
					<-ctx.Done()
				}
				msg.Text = "resolved source [Image 1: unavailable]"
				msg.BodyResolved = true
				msg.MediaRefs = []channel.MediaRef{{Type: channel.MsgTypeImage, StorageKey: "context-image"}}
				return msg
			}
			registerMediaSet(h, h.media)
			msg := p2pMessage(t)
			msg.Text = "quoted context unavailable"
			if err := h.router.Handle(t.Context(), msg); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("deferred context did not start")
			}
			if outcome != "shutdown" && !waitFor(time.Second, func() bool { return h.binder.boundMedia().MessageID.Valid }) {
				t.Fatal("context was not finalized")
			}
			drainCtx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if !h.router.Drain(drainCtx) {
				t.Fatal("context work did not stop before drain completed")
			}
			bound := h.binder.boundMedia()
			if !bound.MessageID.Valid || h.tasks.promotionCalls() != 1 || h.media.calls() != 1 || h.dedup.releases() != 0 {
				t.Fatal("context finalization lost or replayed the accepted message")
			}
			if outcome == "success" {
				if !bound.BodyResolved || bound.Body != "resolved source [Image 1: unavailable]" || len(bound.MediaRefs) != 1 {
					t.Fatalf("resolved body or media lost: %+v", bound)
				}
			} else if bound.BodyResolved || len(bound.MediaRefs) != 0 {
				t.Fatalf("incomplete context could overwrite the durable fallback: %+v", bound)
			}
		})
	}
}
