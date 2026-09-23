package main

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// What closes the WeCom streaming bubble is a bus subscription, and it is
// invisible when it is missing: the events keep being published, nothing
// panics, nothing logs, and the user watches a spinner until the server's
// window runs out on it. Nothing in the wecom package fails either, because
// every unit test builds its own manager and registers it itself.
//
// So this asserts the whole of it off the REAL boot path — NewRouter, the same
// call main() makes. Two routers are built on two buses, one with the WeCom
// key set and one without, and the difference between them is what the WeCom
// block did. Comparing the two is what lets the subscription half stay honest
// without having to know which other subsystems listen to the same events.
//
// A nil pool is deliberate: nothing in the WeCom boot block queries the
// database, and metrics_test.go boots the same way.
func TestWecomBubbleClosersAreWiredOnTheRealBootPath(t *testing.T) {
	key := make([]byte, secretbox.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate a wecom secretbox key: %v", err)
	}

	withoutWecom := events.New()
	NewRouter(nil, realtime.NewHub(), withoutWecom, analytics.NoopClient{}, nil)

	t.Setenv("MULTICA_WECOM_SECRET_KEY", base64.StdEncoding.EncodeToString(key))
	withWecom := events.New()
	NewRouter(nil, realtime.NewHub(), withWecom, analytics.NoopClient{}, nil)

	// Anti-vacuity: if the WeCom block did not run at all, nothing below can
	// fail for the reason it names. chat:done is the subscription that has
	// been wired all along, so it is the marker that the block was entered.
	if got, base := withWecom.SubscriberCount(protocol.EventChatDone),
		withoutWecom.SubscriberCount(protocol.EventChatDone); got <= base {
		t.Fatalf("the WeCom boot block did not run: chat:done listeners %d with the key set vs %d without. "+
			"Re-point this guard at wherever WeCom is wired now", got, base)
	}

	for _, sub := range []struct {
		event       string
		consequence string
	}{
		{
			event: protocol.EventTaskQueued,
			consequence: "no bubble is ever bound to the run that will answer it, so every ending — the answer " +
				"included — finds nothing to close and arrives as a plain message under a spinner that " +
				"runs until the server's window expires",
		},
		{
			event: protocol.EventTaskFailed,
			consequence: "a run that fails publishes no chat:done, so the bubble it opened is never closed and " +
				"the user watches a spinner for an answer nobody is producing",
		},
		{
			event: protocol.EventTaskCancelled,
			consequence: "a cancelled run publishes no chat:done and no task:failed, so its bubble spins until " +
				"the server's window runs out on a run the user stopped themselves",
		},
	} {
		with := withWecom.SubscriberCount(sub.event)
		without := withoutWecom.SubscriberCount(sub.event)
		if with <= without {
			t.Errorf("nothing in the WeCom boot path subscribes to %s (%d listeners with WeCom enabled, %d without): %s. "+
				"Check TypingIndicatorManager.Register.",
				sub.event, with, without, sub.consequence)
		}
	}
}
