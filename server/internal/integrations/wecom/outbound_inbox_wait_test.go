package wecom

// outbound_inbox_wait_test.go — WHERE the acceptance window is paid.
//
// Bus delivery is synchronous on the caller's goroutine, and the publisher of
// an inbox:new is the HTTP request that created the comment. Only one replica
// holds a given bot's socket, so the confirmation wait is the common path in a
// multi-replica deployment. It is worth paying only where the answer can be
// acted on: the agent-attributed attempt, which has the recipient-wide bot
// behind it. These pin that, with the real relay and claim store and the
// production default window, so a regression that spends 2s where the verdict
// is discarded cannot hide behind a 40ms test window.

import (
	"context"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	inboxWaitAgentInst    = "aaaaaaaa-1111-1111-1111-111111111111"
	inboxWaitFallbackInst = "bbbbbbbb-2222-2222-2222-222222222222"
	inboxWaitAgentID      = "55555555-5555-5555-5555-555555555555"
)

// inboxWaitRig wires one Outbound to a real relay with nothing holding either
// bot's socket here, which is the multi-replica shape: the replica serving the
// request is not the one holding the socket.
type inboxWaitRig struct {
	outbound *Outbound
	relay    *fanoutRelay
	dedupe   *sharedDedupe
	queries  *fakeOutboundQueries
}

func newInboxWaitRig(t *testing.T, window time.Duration) inboxWaitRig {
	t.Helper()
	q := &fakeOutboundQueries{workspace: db.Workspace{Slug: "acme"}}
	relay, dedupe := &fanoutRelay{}, newSharedDedupe()
	router := NewRelayOutbound(relay, dedupe, RelayConfig{AcceptanceWindow: window}, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); router.Wait() })
	router.Start(ctx)

	o := NewOutbound(q, newSendersRegistry(), nil, slog.Default(), WithRelay(router))
	router.Attach(o)
	relay.register(router)
	q.agentMemberBinding = db.ChannelUserBinding{ChannelUserID: "T_AGENT_BOT", InstallationID: mustUUID(inboxWaitAgentInst)}
	q.memberBinding = db.ChannelUserBinding{ChannelUserID: "T_LATEST_BOT", InstallationID: mustUUID(inboxWaitFallbackInst)}
	return inboxWaitRig{outbound: o, relay: relay, dedupe: dedupe, queries: q}
}

func (r inboxWaitRig) deliver(ctx context.Context, item map[string]any) (bool, time.Duration) {
	start := time.Now()
	got := r.outbound.tryDeliverInbox(ctx, item,
		"33333333-3333-3333-3333-333333333333", "44444444-4444-4444-4444-444444444444")
	return got, time.Since(start)
}

// TestTryDeliverInbox_MemberAuthoredPushDoesNotWaitForTheAcceptanceWindow: a
// member-authored notification has no second route, so the verdict is thrown
// away by construction. It must not be paid for -- this is the 2s the review
// measured on the request that created the comment.
func TestTryDeliverInbox_MemberAuthoredPushDoesNotWaitForTheAcceptanceWindow(t *testing.T) {
	t.Parallel()
	// The production default: cmd/server does not set AcceptanceWindow.
	rig := newInboxWaitRig(t, 2*time.Second)
	item := agentInboxItem("new_comment", inboxWaitAgentID)
	item["actor_type"] = util.TextToPtr(pgtype.Text{String: "member", Valid: true})

	got, elapsed := rig.deliver(context.Background(), item)
	if elapsed > 300*time.Millisecond {
		t.Fatalf("a member-authored push blocked %v; a verdict with no fallback behind it must not be waited for", elapsed)
	}
	if !got {
		t.Error("tryDeliverInbox = false; the bus took the frame and there is no second route to report a miss to")
	}
	if n := rig.dedupe.claimStateCount(); n != 0 {
		t.Errorf("the claim was polled %d times; a member-authored push must not enter the acceptance window", n)
	}
}

// TestTryDeliverInbox_WaitIsBoundedByTheCallersDeadline: the wait runs on the
// request's goroutine, so the caller's budget has to cap it. A 3s window with a
// 150ms caller must come back in the caller's time, not the window's.
func TestTryDeliverInbox_WaitIsBoundedByTheCallersDeadline(t *testing.T) {
	t.Parallel()
	rig := newInboxWaitRig(t, 3*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	got, elapsed := rig.deliver(ctx, agentInboxItem("new_comment", inboxWaitAgentID))
	if elapsed > 900*time.Millisecond {
		t.Fatalf("the push blocked %v with a 150ms caller budget; the caller's ctx must cap the window", elapsed)
	}
	if got {
		t.Error("tryDeliverInbox = true; a caller that ran out of time confirmed nothing")
	}
}

// TestTryDeliverInbox_FallbackAttemptDoesNotWaitAgain: the fallback is the last
// route, so a no-holder verdict for it changes nothing. It must publish and
// return, leaving the cost at exactly one window rather than two.
func TestTryDeliverInbox_FallbackAttemptDoesNotWaitAgain(t *testing.T) {
	t.Parallel()
	// Virtual time keeps CI scheduling delays from consuming the acceptance
	// poll's timeout cushion while still exercising the real relay and timers.
	synctest.Test(t, func(t *testing.T) {
		const window = 400 * time.Millisecond
		rig := newInboxWaitRig(t, window)

		got, elapsed := rig.deliver(context.Background(), agentInboxItem("new_comment", inboxWaitAgentID))
		if !got {
			t.Error("tryDeliverInbox = false; the fallback publish was accepted and had no route behind it")
		}
		if elapsed != window {
			t.Fatalf("the push took %v, want exactly one acceptance window (%v)", elapsed, window)
		}
		// One window at step = window/8 is about nine polls; two would be about
		// eighteen. The bound sits between them.
		if n := rig.dedupe.claimStateCount(); n > 12 {
			t.Errorf("the claim was polled %d times; the fallback attempt paid a second acceptance window", n)
		}
		if n := rig.relay.publishedTo(inboxWaitFallbackInst); n != 1 {
			t.Errorf("the recipient-wide bot was offered %d frames, want 1", n)
		}
		if n := rig.relay.publishedTo(inboxWaitAgentInst); n != 1 {
			t.Errorf("the acting agent's bot was offered %d frames, want 1", n)
		}
	})
}
