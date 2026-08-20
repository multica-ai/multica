package attribution

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// TestDelegatedSubscriber_AgentCreateInheritsHuman is the core MUL-5483 case:
// an agent files a sub-issue while running on a human's behalf, so that human
// inherits visibility of it under the reduced 'delegated' tier.
func TestDelegatedSubscriber_AgentCreateInheritsHuman(t *testing.T) {
	got, reason, ok := DelegatedSubscriber(SubscriptionFacts{
		CreatorType:      "agent",
		OriginType:       "agent_create",
		OriginOriginator: human,
	})

	if !ok {
		t.Fatal("expected an agent_create issue with a resolved originator to subscribe someone")
	}
	if got != human {
		t.Fatalf("subscriber = %v, want the origin task's human %v", got, human)
	}
	if reason != "delegated" {
		t.Fatalf("reason = %q, want delegated", reason)
	}
}

// TestDelegatedSubscriber_QuickCreateIsDirectIntent locks in the deliberate
// asymmetry: the quick-create human asked for THAT issue by name, so it keeps
// the direct 'creator' tier and full notifications. Collapsing it into
// 'delegated' would quietly downgrade an existing flow.
func TestDelegatedSubscriber_QuickCreateIsDirectIntent(t *testing.T) {
	got, reason, ok := DelegatedSubscriber(SubscriptionFacts{
		CreatorType:      "agent",
		OriginType:       "quick_create",
		OriginOriginator: human,
	})

	if !ok {
		t.Fatal("expected a quick_create issue to subscribe its requester")
	}
	if got != human {
		t.Fatalf("subscriber = %v, want %v", got, human)
	}
	if reason != "creator" {
		t.Fatalf("reason = %q, want creator — quick create is direct human intent", reason)
	}
}

// TestDelegatedSubscriber_AutopilotExcluded: an autopilot has an explicitly
// configured subscriber template, and THAT list is the intended audience for
// its issues. Subscribing whoever armed the trigger to every issue the rule
// ever spawns would be a different product decision, made silently.
func TestDelegatedSubscriber_AutopilotExcluded(t *testing.T) {
	if _, _, ok := DelegatedSubscriber(SubscriptionFacts{
		CreatorType:      "agent",
		OriginType:       "autopilot",
		OriginOriginator: human,
	}); ok {
		t.Fatal("autopilot issues must not take a delegated subscription")
	}
}

// TestDelegatedSubscriber_NoHumanNoSubscription: degraded attribution
// (owner_fallback / unattributed) leaves no originator. We surface that as "no
// human resolved" rather than fabricating one to notify.
func TestDelegatedSubscriber_NoHumanNoSubscription(t *testing.T) {
	if _, _, ok := DelegatedSubscriber(SubscriptionFacts{
		CreatorType:      "agent",
		OriginType:       "agent_create",
		OriginOriginator: pgtype.UUID{},
	}); ok {
		t.Fatal("an unattributed origin task must not produce a subscriber")
	}
}

// TestDelegatedSubscriber_MemberCreatedIssueUnaffected: a human-created issue
// already subscribes its human through the ordinary 'creator' rule. Firing
// here too would write a second, wrong-tier row for the same person.
func TestDelegatedSubscriber_MemberCreatedIssueUnaffected(t *testing.T) {
	if _, _, ok := DelegatedSubscriber(SubscriptionFacts{
		CreatorType:      "member",
		OriginType:       "agent_create",
		OriginOriginator: human,
	}); ok {
		t.Fatal("member-created issues must not take a delegated subscription")
	}
}

// TestDelegatedSubscriber_UnknownOriginIsInert guards the open-ended origin
// vocabulary: a newly-modeled origin type must not silently start subscribing
// people before someone decides that is what it should do.
func TestDelegatedSubscriber_UnknownOriginIsInert(t *testing.T) {
	if _, _, ok := DelegatedSubscriber(SubscriptionFacts{
		CreatorType:      "agent",
		OriginType:       "some_future_origin",
		OriginOriginator: human,
	}); ok {
		t.Fatal("an unrecognized origin_type must not subscribe anyone")
	}
}
