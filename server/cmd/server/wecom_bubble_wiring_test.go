package main

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/wecom"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// What closes the WeCom streaming bubble is a bus subscription holding four
// dependencies, and every one of those five things is invisible when it is
// missing: the events keep being published, nothing panics, nothing logs, and
// the user watches a spinner until the server's window runs out on it. Nothing in the wecom package fails
// either, because every unit test builds its own manager and hands it its own
// dependencies.
//
// So this asserts the whole of it off the REAL boot path — NewRouter, the same
// call main() makes. Two routers are built on two buses, one with the WeCom
// key set and one without, and the difference between them is what the WeCom
// block did. Comparing the two is what lets the subscription half stay honest
// without having to know which other subsystems listen to the same events.
//
// Subscriptions alone are not enough, and that is the point of the second
// half. Register subscribes unconditionally: drop Tasks or Deliveries from the
// boot block and both subscriptions still appear, the counts still rise, and
// every bubble the manager is supposed to close silently stays open. So the
// dependencies are read back off the manager the boot path actually
// registered, not off one this test built.
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
	_, h := NewRouterWithOptions(nil, realtime.NewHub(), withWecom, analytics.NoopClient{}, nil, RouterOptions{})

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

	// The other half: the manager behind those subscriptions has to be able to
	// act on them. Read it back off the channel router, which is where the
	// boot block left the one instance it built.
	set, ok := h.ChannelRouter.RegisteredSet(wecom.TypeWecom)
	if !ok {
		t.Fatalf("the WeCom boot block ran but registered no resolver set on the channel router. " +
			"engine.Router.Register drops an incomplete set with nothing but a log line, so WeCom would boot, " +
			"announce itself enabled, and answer no inbound message at all. Check wecom.NewResolverSet.")
	}
	typing, ok := set.Typing.(*wecom.TypingIndicatorManager)
	if !ok {
		t.Fatalf("the WeCom resolver set carries no *wecom.TypingIndicatorManager (Typing is %T). "+
			"Nothing opens or closes a stream bubble, so every WeCom user waits with no sign the bot heard them "+
			"until the whole answer lands at once.", set.Typing)
	}

	// Each of these is optional at construction and silently narrows the
	// manager when it is missing. The consequence is what the user sees, which
	// is the only reason any of them is worth a test.
	wiring := typing.Wiring()
	for _, dep := range []struct {
		field       string
		wired       bool
		consequence string
	}{
		{
			field: "Senders",
			wired: wiring.Senders,
			consequence: "no closing frame and no fallback message can be written at all, so every bubble " +
				"WeCom opens spins until the client gives up on it",
		},
		{
			field: "Streams",
			wired: wiring.Streams,
			consequence: "every task:failed and task:cancelled handler returns on its first line, so the " +
				"subscriptions above are registered and do nothing and no ending ever closes a bubble",
		},
		{
			field: "Tasks",
			wired: wiring.Tasks,
			consequence: "task:progress and task:message are never subscribed at all (Register gates both " +
				"on it), so a bubble opens and spins with no steps in it; and failureBelongsOnWecom " +
				"can no longer read the run's input batch, so any failure for a run this process holds " +
				"no round for is refused as unattributable and the user is told nothing",
		},
		{
			field: "Deliveries",
			wired: wiring.Deliveries,
			consequence: "a run that fails after its bubble is gone — the process restarted mid-run, or " +
				"the opening frame was refused — has no chat to speak in, so the user is told nothing",
		},
		{
			field: "Identities",
			wired: wiring.Identities,
			consequence: "nobody can be recognised as the bot's principal, so every bubble falls to the tier " +
				"that shows no steps at all — the bubble opens, spins for the length of the run and closes, " +
				"which is exactly what the in-flight step list exists to stop",
		},
	} {
		if !dep.wired {
			t.Errorf("the WeCom boot path built its TypingIndicatorManager without %s: %s. "+
				"The subscriptions still register, so nothing else here fails. "+
				"Check the wecom.TypingIndicatorConfig in NewRouterWithOptions.", dep.field, dep.consequence)
		}
	}
}
