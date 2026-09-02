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

// The WeCom bubble's in-flight steps are driven entirely by two bus
// subscriptions, and both are invisible when they are missing: the events keep
// being published, nobody reads them, and the bubble opens and closes with a
// spinner and nothing in between. The package builds, every renderer test
// passes, and the feature is not there.
//
// Worse, the two are registered conditionally — Register subscribes them only
// when a task lookup was configured, because neither event carries a chat
// session — so dropping TypingIndicatorConfig.Tasks from the boot block
// disables them and nothing else changes.
//
// So this asserts the subscriptions off the REAL boot path — NewRouter, the
// same call main() makes — rather than off a hand-built manager. Two routers
// are built on two buses, one with the WeCom key set and one without, and the
// WeCom one has to add a listener on each event. Comparing the two is what
// keeps the guard honest without it having to know which other subsystems
// subscribe to the same events.
//
// A nil pool is deliberate: nothing in the WeCom boot block queries the
// database, and metrics_test.go boots the same way.
func TestWecomProgressSubscribesOnTheRealBootPath(t *testing.T) {
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

	for _, event := range []string{protocol.EventTaskProgress, protocol.EventTaskMessage} {
		with := withWecom.SubscriberCount(event)
		without := withoutWecom.SubscriberCount(event)
		if with <= without {
			t.Errorf("nothing in the WeCom boot path subscribes to %s (%d listeners with WeCom enabled, %d without). "+
				"The streaming bubble opens and closes with nothing in between: %s is what fills it in while the run "+
				"is going. Check TypingIndicatorManager.Register and that TypingIndicatorConfig.Tasks is still passed "+
				"— the progress subscriptions are gated on a non-nil task lookup, so dropping it disables them silently.",
				event, with, without, event)
		}
	}
}
