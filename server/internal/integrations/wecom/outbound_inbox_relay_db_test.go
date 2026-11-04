package wecom

// outbound_inbox_relay_db_test.go — an inbox push offered to two bots, across
// two replicas.
//
// The first review round fixed the LOCAL half of the fallback: an inbox push
// whose preferred (acting agent's) bot provably delivered nothing is offered
// to the recipient-wide bot instead. What the second round found is that the
// RELAY half of that decision was still a bool, so the two cases that matter
// most were indistinguishable from the outside:
//
//
//  1. The actor's bot is offline while the relay IS wired. A successful publish
//     only proves the frame was ENQUEUED. If no replica holds that bot's socket,
//     every dispatcher discards the frame, but the push reported success and
//     never tried the online recipient-wide bot: the notification was dropped
//     silently.
//
//  2. A publish call that returns an error. The frame may already have been
//     accepted and delivered, with only the RESPONSE lost; falling back there
//     puts a second card in the member's chat.
//
// These run against a real database with the same fanoutRelay / sharedDedupe /
// newRelayReplica rig the reply-path tests use, so the acceptance window and
// the claim fence are exercised for real. They skip when no migrated database
// is reachable.

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	inboxRelayWS     = "7d1a0000-0000-4000-8000-000000000001"
	inboxRelayUser   = "7d1a0000-0000-4000-8000-000000000002"
	inboxRelayAgentA = "7d1a0000-0000-4000-8000-000000000003"
	inboxRelayAgentB = "7d1a0000-0000-4000-8000-000000000004"
	inboxRelayInstA  = "7d1a0000-0000-4000-8000-000000000005"
	inboxRelayInstB  = "7d1a0000-0000-4000-8000-000000000006"

	// The anonymized userids the two bots address the same member by.
	inboxRelayChatA = "TBOT_A_MEMBER"
	inboxRelayChatB = "TBOT_B_MEMBER"
)

// inboxRelayFixture is one workspace where a member is bound on two bots: the
// acting agent's (A) and a second one (B) they bound more recently.
type inboxRelayFixture struct {
	workspaceID  string
	recipientID  string
	actingAgent  string
	actorInst    string
	fallbackInst string
}

func seedInboxRelayFixture(t *testing.T, pool *pgxpool.Pool) inboxRelayFixture {
	t.Helper()
	ctx := context.Background()
	clean := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM channel_user_binding WHERE workspace_id = $1`, inboxRelayWS)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE workspace_id = $1`, inboxRelayWS)
		_, _ = pool.Exec(ctx, `DELETE FROM agent WHERE workspace_id = $1`, inboxRelayWS)
		_, _ = pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, inboxRelayUser)
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, inboxRelayWS)
	}
	clean()
	t.Cleanup(clean)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed inbox relay fixture: %v", err)
		}
	}
	exec(`INSERT INTO workspace (id, name, slug) VALUES ($1, $2, $3)`,
		inboxRelayWS, "inbox relay", "inbox-relay")
	exec(`INSERT INTO "user" (id, name, email) VALUES ($1, $2, $3)`,
		inboxRelayUser, "Inbox Recipient", "inbox-relay@example.com")
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode) VALUES ($1, $2, $3, 'local')`,
		inboxRelayAgentA, inboxRelayWS, "inbox-agent-a")
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode) VALUES ($1, $2, $3, 'local')`,
		inboxRelayAgentB, inboxRelayWS, "inbox-agent-b")
	exec(`INSERT INTO channel_installation
	        (id, workspace_id, agent_id, channel_type, status, installer_user_id)
	      VALUES ($1, $2, $3, 'wecom', 'active', $4)`,
		inboxRelayInstA, inboxRelayWS, inboxRelayAgentA, inboxRelayUser)
	exec(`INSERT INTO channel_installation
	        (id, workspace_id, agent_id, channel_type, status, installer_user_id)
	      VALUES ($1, $2, $3, 'wecom', 'active', $4)`,
		inboxRelayInstB, inboxRelayWS, inboxRelayAgentB, inboxRelayUser)
	// B is bound LAST, so the recipient-wide lookup answers B. That is the
	// fallback route, and it is deliberately a different bot from A.
	exec(`INSERT INTO channel_user_binding
	        (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id, bound_at)
	      VALUES ($1, $2, $3, 'wecom', $4, now() - interval '1 hour')`,
		inboxRelayWS, inboxRelayUser, inboxRelayInstA, inboxRelayChatA)
	exec(`INSERT INTO channel_user_binding
	        (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id, bound_at)
	      VALUES ($1, $2, $3, 'wecom', $4, now())`,
		inboxRelayWS, inboxRelayUser, inboxRelayInstB, inboxRelayChatB)
	return inboxRelayFixture{
		workspaceID:  inboxRelayWS,
		recipientID:  inboxRelayUser,
		actingAgent:  inboxRelayAgentA,
		actorInst:    inboxRelayInstA,
		fallbackInst: inboxRelayInstB,
	}
}

// inboxRelayEvent is one issue-comment notification as the listeners publish
// it: a member recipient, and agent A as the actor whose bot should carry it.
func inboxRelayEvent(fx inboxRelayFixture, itemID string) events.Event {
	return events.Event{
		Type: protocol.EventInboxNew,
		Payload: map[string]any{
			"item": map[string]any{
				"id":             itemID,
				"type":           "new_comment",
				"title":          "An issue",
				"recipient_type": "member",
				"recipient_id":   fx.recipientID,
				"workspace_id":   fx.workspaceID,
				"actor_type":     util.TextToPtr(pgtype.Text{String: "agent", Valid: true}),
				"actor_id":       util.UUIDToPtr(mustUUID(fx.actingAgent)),
			},
		},
	}
}

// TestInboxRelay_NoReplicaHoldsTheActorsBotFallsBack is finding 1: the actor's
// bot is offline while the relay is wired, so the publish is accepted by the
// bus and claimed by nobody. The push must fall back to the recipient-wide bot
// — and exactly one card must reach the member.
func TestInboxRelay_NoReplicaHoldsTheActorsBotFallsBack(t *testing.T) {
	pool := twoReplicaDB(t)
	fx := seedInboxRelayFixture(t, pool)
	relay, dedupe := &fanoutRelay{}, newSharedDedupe()
	cfg := RelayConfig{AcceptanceWindow: 200 * time.Millisecond}

	// This replica served the inbox event and holds NO socket; the other holds
	// only the recipient-wide bot's socket, never the acting agent's.
	publisher := newRelayReplicaWith(t, pool, fx.actorInst, false, relay, dedupe, cfg)
	holder := newRelayReplicaWith(t, pool, fx.fallbackInst, true, relay, dedupe, cfg)

	publisher.bus.Publish(inboxRelayEvent(fx, "inbox-no-holder"))

	waitFor(t, "the recipient-wide bot to carry the card", func() bool { return holder.frames() == 1 })
	if body := holder.conn.sendBody(t, 0); body["chatid"] != inboxRelayChatB {
		t.Errorf("card chatid = %v, want the recipient-wide bot's userid %s", body["chatid"], inboxRelayChatB)
	}
	if n := publisher.frames(); n != 0 {
		t.Errorf("the publisher's empty registry carried %d cards, want 0", n)
	}
	if got := relay.publishedTo(fx.actorInst); got != 1 {
		t.Errorf("the acting agent's bot was offered %d frames, want 1 (the accepted, unclaimed one)", got)
	}
	if got := relay.publishedTo(fx.fallbackInst); got != 1 {
		t.Errorf("the recipient-wide bot was offered %d frames, want 1", got)
	}
	// The unclaimed claim is FENCED under the publisher's own token, so a
	// dispatcher that shows up late cannot deliver a duplicate.
	key := dedupeKey(relayInboxEventID("inbox-no-holder", fx.recipientID, fx.actorInst))
	if v := dedupe.valueOf(key); v == "" || v == claimSettledValue || v == claimLostValue {
		t.Errorf("the unclaimed acting-agent claim holds %q, want a fencing token", v)
	}
}

// TestInboxRelay_UncertainPublishDoesNotFallBack is finding 2: publish reports
// failure AFTER the frame was fanned out (the response was lost). The card is
// on its way through the actor's bot, so the recipient-wide bot must NOT be
// tried — one card, not two.
func TestInboxRelay_UncertainPublishDoesNotFallBack(t *testing.T) {
	pool := twoReplicaDB(t)
	fx := seedInboxRelayFixture(t, pool)
	relay, dedupe := &fanoutRelay{failAfterFanout: true}, newSharedDedupe()
	cfg := RelayConfig{AcceptanceWindow: 200 * time.Millisecond}

	publisher := newRelayReplicaWith(t, pool, fx.actorInst, false, relay, dedupe, cfg)
	holder := newRelayReplicaWith(t, pool, fx.actorInst, true, relay, dedupe, cfg)

	publisher.bus.Publish(inboxRelayEvent(fx, "inbox-lost-response"))

	waitFor(t, "the acting agent's bot to carry the card", func() bool { return holder.frames() == 1 })
	if body := holder.conn.sendBody(t, 0); body["chatid"] != inboxRelayChatA {
		t.Errorf("card chatid = %v, want the acting agent's bot %s", body["chatid"], inboxRelayChatA)
	}
	// Give a wrong fallback room to land before asserting its absence.
	time.Sleep(100 * time.Millisecond)
	if n := holder.frames(); n != 1 {
		t.Errorf("the member received %d cards, want exactly 1: an uncertain publish must not be re-routed", n)
	}
	if got := relay.publishedTo(fx.fallbackInst); got != 0 {
		t.Errorf("the recipient-wide bot was offered %d frames after an uncertain publish, want 0", got)
	}
}
