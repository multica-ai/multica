package main

// CEREBRO-PATCH(skill-notif-channels): FIR-1587 — unit coverage for the skill
// notification routing wiring: the inbox_item.Type → user-facing routing-key
// collapse, and the per-channel defaults that decide where a skill event lands
// out of the box (inbox on, push opt-in). End-to-end delivery (handler gate →
// event → dispatchToMember) is exercised by the handler-package tests and the
// live verification on FIR-1587.

import "testing"

func TestSkillRouteKeyMapping(t *testing.T) {
	cases := map[string]string{
		"skill_change_request_created":  "skill_change_request",
		"skill_change_request_reviewed": "skill_change_request",
		"skill_forked":                  "skill_fork",
		"skill_agent_assigned":          "skill_agent_assigned",
	}
	for inboxType, wantKey := range cases {
		if got := routeKey(inboxType, false); got != wantKey {
			t.Errorf("routeKey(%q) = %q, want %q", inboxType, got, wantKey)
		}
		// isAssignee must not change skill routing (skills aren't issues).
		if got := routeKey(inboxType, true); got != wantKey {
			t.Errorf("routeKey(%q, isAssignee=true) = %q, want %q", inboxType, got, wantKey)
		}
	}
}

func TestSkillChannelDefaults(t *testing.T) {
	skillKeys := []string{"skill_change_request", "skill_fork", "skill_agent_assigned"}

	// Inbox is on by default for every skill key — preserves the prior
	// inbox-only delivery so enabling channel routing changes nothing until
	// the user opts in elsewhere.
	for _, k := range skillKeys {
		if !defaultChannelChoices[channelInbox][k] {
			t.Errorf("expected %q to default ON for inbox", k)
		}
	}

	// Notifications feed and both push channels are off by default — opt-in
	// only, so turning the feature on never starts paging an owner's phone.
	for _, ch := range []string{channelNotifications, channelMobile, channelDesktop} {
		for _, k := range skillKeys {
			if defaultChannelChoices[ch][k] {
				t.Errorf("expected %q to default OFF for channel %q", k, ch)
			}
		}
	}
}
