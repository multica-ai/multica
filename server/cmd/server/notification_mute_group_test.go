package main

import "testing"

// TestIsNotifMutedGroupSplit pins the contract the `mentions` group exists for:
// muting comment VOLUME must never silence someone asking for you BY NAME.
// `new_comment` and `mentioned` shared the `comments` group until #6468, so a
// user who muted comment churn also lost every mention — and a muted item is
// never created, so it was unrecoverable rather than merely delayed.
func TestIsNotifMutedGroupSplit(t *testing.T) {
	cases := []struct {
		name      string
		prefs     map[string]string
		notifType string
		want      bool
	}{
		// The regression this split fixes.
		{"muted comments still delivers mentions", map[string]string{"comments": "muted"}, "mentioned", false},
		{"muted comments drops new_comment", map[string]string{"comments": "muted"}, "new_comment", true},
		{"muted mentions drops mentioned", map[string]string{"mentions": "muted"}, "mentioned", true},
		{"muted mentions still delivers new_comment", map[string]string{"mentions": "muted"}, "new_comment", false},
		{"both muted drops both", map[string]string{"comments": "muted", "mentions": "muted"}, "mentioned", true},

		// Absent key means the default "all" — preferences are stored sparse,
		// which is why existing `{"comments":"muted"}` rows need no migration.
		{"empty prefs delivers mentions", map[string]string{}, "mentioned", false},
		{"empty prefs delivers comments", map[string]string{}, "new_comment", false},
		{"explicit all delivers", map[string]string{"comments": "all"}, "new_comment", false},

		// Other groups are unaffected by the split.
		{"assignments group intact", map[string]string{"assignments": "muted"}, "issue_assigned", true},
		{"status group intact", map[string]string{"status_changes": "muted"}, "status_changed", true},
		{"muting comments leaves assignments alone", map[string]string{"comments": "muted"}, "issue_assigned", false},

		// Types outside the map are always delivered.
		{"unconfigurable type always delivered", map[string]string{"comments": "muted"}, "reaction_added", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNotifMuted(tc.prefs, tc.notifType); got != tc.want {
				t.Fatalf("isNotifMuted(%v, %q) = %v, want %v",
					tc.prefs, tc.notifType, got, tc.want)
			}
		})
	}
}

// TestNotifTypeToGroupMentionsIsOwnGroup guards the mapping itself: a future
// edit that folds `mentioned` back under `comments` should fail here rather
// than silently reintroduce the coupling.
func TestNotifTypeToGroupMentionsIsOwnGroup(t *testing.T) {
	if got := notifTypeToGroup["mentioned"]; got != "mentions" {
		t.Fatalf("notifTypeToGroup[mentioned] = %q, want %q", got, "mentions")
	}
	if got := notifTypeToGroup["new_comment"]; got != "comments" {
		t.Fatalf("notifTypeToGroup[new_comment] = %q, want %q", got, "comments")
	}
}

func TestCanonicalNotificationGroupsOverrideLegacyPerEventPreferences(t *testing.T) {
	cases := []struct {
		name      string
		prefs     map[string]string
		notifType string
		want      bool
	}{
		{"legacy comment mute remains effective", map[string]string{"comments": "muted"}, "new_comment", true},
		{"legacy mention choice remains independent", map[string]string{"comments": "muted"}, "mentioned", false},
		{"canonical comments group overrides both legacy keys", map[string]string{"comments_mentions": "muted", "mentions": "all"}, "mentioned", true},
		{"progress canonical override covers successful task events", map[string]string{"task_agent_progress": "muted", "agent_activity": "all"}, "task_completed", true},
		{"system health canonical override separates failures from progress", map[string]string{"system_health": "all", "agent_activity": "muted"}, "task_failed", false},
		{"legacy agent activity still mutes failure before migration", map[string]string{"agent_activity": "muted"}, "task_failed", true},
		{"previously ungrouped reaction follows comments and mentions", map[string]string{"comments_mentions": "muted"}, "reaction_added", true},
		{"previously ungrouped quick-create failure follows system health", map[string]string{"system_health": "muted"}, "quick_create_failed", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNotifMuted(tc.prefs, tc.notifType); got != tc.want {
				t.Fatalf("isNotifMuted(%v, %q) = %v, want %v", tc.prefs, tc.notifType, got, tc.want)
			}
		})
	}
}
