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
		OriginRootSource: SourceDirectHuman,
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
		OriginRootSource: SourceDirectHuman,
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
		OriginRootSource: SourceTriggerOwner,
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
		OriginRootSource: SourceDirectHuman,
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
		OriginRootSource: SourceDirectHuman,
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
		OriginRootSource: SourceDirectHuman,
	}); ok {
		t.Fatal("an unrecognized origin_type must not subscribe anyone")
	}
}

// TestDelegatedSubscriber_ArmedTriggerChainSubscribesNobody is MUL-7051. The
// originator is a real, valid member — the person who armed the schedule — and
// before MUL-6951 no such value existed, so "there is a human here" was safe to
// read as "a human asked for this". It no longer is, and every issue the
// autopilot's agent filed started subscribing that member: the exact outcome
// TestDelegatedSubscriber_AutopilotExcluded forbids, one hop further down.
func TestDelegatedSubscriber_ArmedTriggerChainSubscribesNobody(t *testing.T) {
	if _, _, ok := DelegatedSubscriber(SubscriptionFacts{
		CreatorType:      "agent",
		OriginType:       "agent_create",
		OriginOriginator: human,
		OriginRootSource: SourceTriggerOwner,
	}); ok {
		t.Fatal("an issue filed inside an autopilot run must not subscribe whoever armed the trigger")
	}
}

// TestDelegatedSubscriber_UnknownRootIsInert is the origin-vocabulary guard
// (TestDelegatedSubscriber_UnknownOriginIsInert) applied to the other open
// vocabulary this rule now reads. MUL-7051 happened because a new root source
// turned the rule on for a population nobody had chosen; a whitelist means the
// next one has to be chosen deliberately.
//
// An empty label — a pre-attribution row, or a lineage truncated before its root
// — is the same answer: unproven, so no subscription.
func TestDelegatedSubscriber_UnknownRootIsInert(t *testing.T) {
	for _, root := range []Source{SourceDelegation, SourceCommentSource, SourceRuleOwner, SourceOwnerFallback, SourceBackfill, SourceUnattributed, "some_future_root", ""} {
		if _, _, ok := DelegatedSubscriber(SubscriptionFacts{
			CreatorType:      "agent",
			OriginType:       "agent_create",
			OriginOriginator: human,
			OriginRootSource: root,
		}); ok {
			t.Fatalf("root source %q must not subscribe anyone: only a direct human act is a request", root)
		}
	}
}
