package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/wecom"
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
// when a task lookup and a delivery lookup were configured, because neither
// event carries a chat session or says the run was asked over WeCom — so
// dropping TypingIndicatorConfig.Tasks or .Deliveries from the boot block
// disables them and nothing else changes.
//
// So this asserts the subscriptions off the REAL boot path — NewRouter, the
// same call main() makes — rather than off a hand-built manager. Two routers
// are built on two buses, one with the WeCom key set and one without, and the
// WeCom one has to add a listener on each event. Comparing the two is what
// keeps the guard honest without it having to know which other subsystems
// subscribe to the same events.
//
// The subscriptions are half of it. The other half is who may see the steps:
// the manager reads the asker's identity to decide, and with no identity
// lookup every bubble falls to the tier that shows none. That is a constructor
// field like Tasks, nil by default, so the boot path is also checked for the
// warning NewTypingIndicator logs when it is missing.
//
// A nil pool is deliberate: nothing in the WeCom boot block queries the
// database, and metrics_test.go boots the same way.
func TestWecomProgressSubscribesOnTheRealBootPath(t *testing.T) {
	key := make([]byte, secretbox.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate a wecom secretbox key: %v", err)
	}

	var logged bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(restore) })

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
				"is going. Check TypingIndicatorManager.Register and that TypingIndicatorConfig.Tasks and .Deliveries are still passed "+
				"— the progress subscriptions are gated on both lookups being non-nil, so dropping either disables them silently.",
				event, with, without, event)
		}
	}

	if got := logged.String(); strings.Contains(got, "wecom typing: no identity lookup wired") {
		t.Errorf("the production typing indicator was built without an identity lookup, so no bubble ever "+
			"shows a step: every asker lands on the closed tier. Pass Identities to wecom.NewTypingIndicator "+
			"in router.go.\nlog:\n%s", got)
	}
}

// With more than one replica, a run's reports reach whichever replica served
// the daemon's request, and the bubble is on the one that painted it. The
// relay carries a progress frame between them, and on the receiving side it is
// handed to the typing indicator — if the boot block attached it. If it did
// not, every frame is read and dropped, and a run whose reports land on the
// other replica shows no steps. The relay says so once (deliverProgress), so
// that warning is what this checks for, off the real boot path.
//
// Anti-vacuity: a relay that was never given to a router has to warn, or the
// absence of the warning below means nothing.
func TestWecomRelayHandsProgressToTheTypingIndicatorOnTheRealBootPath(t *testing.T) {
	key := make([]byte, secretbox.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate a wecom secretbox key: %v", err)
	}
	t.Setenv("MULTICA_WECOM_SECRET_KEY", base64.StdEncoding.EncodeToString(key))

	const warning = "nothing here shows them"
	frame := []byte(`{"kind":"progress","installation_id":"11111111-1111-1111-1111-111111111111",` +
		`"session_id":"22222222-2222-2222-2222-222222222222","task_id":"33333333-3333-3333-3333-333333333333","seq":1}`)

	var strayLog bytes.Buffer
	stray := wecom.NewRelayOutbound(nil, nil, wecom.RelayConfig{},
		slog.New(slog.NewTextHandler(&strayLog, &slog.HandlerOptions{Level: slog.LevelDebug})))
	stray.DeliverWecomOutbound("11111111-1111-1111-1111-111111111111", frame, "progress-1")
	if !strings.Contains(strayLog.String(), warning) {
		t.Fatalf("a relay attached to nothing did not warn about a progress frame, so the check below "+
			"cannot fail; re-point it at whatever deliverProgress says now.\nlog:\n%s", strayLog.String())
	}

	var bootLog bytes.Buffer
	booted := wecom.NewRelayOutbound(nil, nil, wecom.RelayConfig{},
		slog.New(slog.NewTextHandler(&bootLog, &slog.HandlerOptions{Level: slog.LevelDebug})))
	NewRouterWithOptions(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil,
		RouterOptions{WecomRelayOutbound: booted})
	booted.DeliverWecomOutbound("11111111-1111-1111-1111-111111111111", frame, "progress-1")
	if strings.Contains(bootLog.String(), warning) {
		t.Errorf("the boot path built a cross-replica relay and never attached the typing indicator to it: "+
			"a run whose reports reach another replica shows no steps in its bubble. Check that router.go "+
			"still calls wecomTyping.WithRelay.\nlog:\n%s", bootLog.String())
	}
}
