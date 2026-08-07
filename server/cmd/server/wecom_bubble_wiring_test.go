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

// What closes the WeCom streaming bubble is a bus subscription, and a
// subscription that is missing is invisible: the events keep being published,
// nobody reads them, and the user watches a spinner until the five-minute
// guard replaces it with a promise about a run that is already over. Nothing
// in the package fails, because every unit test builds its own manager and
// registers it by hand.
//
// So this asserts the subscriptions off the REAL boot path — NewRouter, the
// same call main() makes. Two routers are built on two buses, one with the
// WeCom key set and one without, and the WeCom one has to add a listener on
// each event. Comparing the two is what keeps the guard honest without it
// having to know which other subsystems subscribe to the same events.
//
// A nil pool is deliberate: nothing in the WeCom boot block queries the
// database, and metrics_test.go boots the same way.
func TestWecomBubbleClosersSubscribeOnTheRealBootPath(t *testing.T) {
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

	for _, event := range []string{protocol.EventTaskFailed, protocol.EventTaskCancelled} {
		with := withWecom.SubscriberCount(event)
		without := withoutWecom.SubscriberCount(event)
		if with <= without {
			t.Errorf("nothing in the WeCom boot path subscribes to %s (%d listeners with WeCom enabled, %d without). "+
				"A run that ends on %s publishes no chat:done, so the bubble it opened is never closed and the user "+
				"watches a spinner for an answer nobody is producing. Check TypingIndicatorManager.Register.",
				event, with, without, event)
		}
	}
}
