package inbox

import "testing"

func TestSubscriptionDelivery(t *testing.T) {
	cases := []struct {
		name     string
		reason   string
		prefs    map[string]string
		someones bool
		want     Delivery
	}{
		{"passive on someone else's issue", "commenter", nil, true, DeliveryKeyUpdates},
		{"creator on someone else's issue", "creator", nil, true, DeliveryKeyUpdates},
		{"explicit key_updates preference", "mentioned", map[string]string{OthersIssuesPref: "key_updates"}, true, DeliveryKeyUpdates},
		{"passive on own issue", "assignee", nil, false, DeliveryAll},
		{"passive with others_issues=all", "commenter", map[string]string{OthersIssuesPref: "all"}, true, DeliveryAll},
		{"manual follow", "manual", nil, true, DeliveryAll},
		{"autopilot", "autopilot", nil, true, DeliveryAll},
		{"delegated", "delegated", nil, false, DeliveryDelegated},
		{"delegated ignores others_issues", "delegated", map[string]string{OthersIssuesPref: "all"}, true, DeliveryDelegated},
		// An unrecognized reason must fall back to full delivery: silently
		// dropping a member's notifications is the worse failure.
		{"unknown reason", "some_future_reason", nil, true, DeliveryAll},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SubscriptionDelivery(c.reason, c.prefs, func() bool { return c.someones }); got != c.want {
				t.Fatalf("SubscriptionDelivery(%q) = %q, want %q", c.reason, got, c.want)
			}
		})
	}
}

func TestSubscriptionDelivery_ReadsOwnerOnlyWhenNeeded(t *testing.T) {
	calls := 0
	owner := func() bool { calls++; return true }
	SubscriptionDelivery("manual", nil, owner)
	SubscriptionDelivery("delegated", nil, owner)
	SubscriptionDelivery("commenter", map[string]string{OthersIssuesPref: "all"}, owner)
	if calls != 0 {
		t.Fatalf("owner read %d times for subscriptions it cannot narrow, want 0", calls)
	}
}

func TestIssueOwner_IsSomeoneElses(t *testing.T) {
	if (IssueOwner{}).IsSomeoneElses("u1") {
		t.Error("an unassigned issue is nobody else's")
	}
	if (IssueOwner{assigned: true, ownerID: "u1"}).IsSomeoneElses("u1") {
		t.Error("an issue assigned to u1 is u1's own")
	}
	if !(IssueOwner{assigned: true, ownerID: "u2"}).IsSomeoneElses("u1") {
		t.Error("an issue assigned to u2 is someone else's for u1")
	}
	if !(IssueOwner{assigned: true}).IsSomeoneElses("u1") {
		t.Error("a squad's issue is someone else's")
	}
}
