package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/inbox"
)

// TestIsNotifMuted pins what a preference group can switch off: an update, and
// never a row addressed to the member (MUL-9215). A muted row is never created,
// so muting one that waits on the member would strand the issue with nothing in
// their Inbox to say so.
func TestIsNotifMuted(t *testing.T) {
	cases := []struct {
		name      string
		prefs     map[string]string
		notifType string
		section   string
		want      bool
	}{
		{"muted comments drops a comment on a followed issue", map[string]string{"comments": "muted"}, "new_comment", inbox.SectionUpdates, true},
		{"muted comments keeps a comment on the member's own issue", map[string]string{"comments": "muted"}, "new_comment", inbox.SectionNeedsYou, false},
		{"muted status drops a status change on a followed issue", map[string]string{"status_changes": "muted"}, "status_changed", inbox.SectionUpdates, true},
		{"muted status keeps a status that waits on the member", map[string]string{"status_changes": "muted"}, "status_changed", inbox.SectionNeedsYou, false},
		{"muted assignments drops a reassignment", map[string]string{"assignments": "muted"}, "assignee_changed", inbox.SectionUpdates, true},
		{"muted assignments keeps an assignment to the member", map[string]string{"assignments": "muted"}, "issue_assigned", inbox.SectionNeedsYou, false},
		{"muted agent activity drops a failed run on a followed issue", map[string]string{"agent_activity": "muted"}, "task_failed", inbox.SectionUpdates, true},
		{"muted agent activity keeps the member's own failed run", map[string]string{"agent_activity": "muted"}, "task_failed", inbox.SectionNeedsYou, false},

		// Groups stay separate: muting comment volume leaves the rest alone.
		{"muted comments leaves status changes alone", map[string]string{"comments": "muted"}, "status_changed", inbox.SectionUpdates, false},

		// A mention is never dropped; `mentions` only says where a group
		// mention is filed (groupMentionsAreUpdates).
		{"mentions is not a mute group", map[string]string{"mentions": "muted"}, "mentioned", inbox.SectionUpdates, false},

		// Absent key means the default "all" — preferences are stored sparse.
		{"empty prefs delivers", map[string]string{}, "new_comment", inbox.SectionUpdates, false},
		{"explicit all delivers", map[string]string{"comments": "all"}, "new_comment", inbox.SectionUpdates, false},

		// Types outside the map are always delivered.
		{"unconfigurable type always delivered", map[string]string{"comments": "muted"}, "reaction_added", inbox.SectionUpdates, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNotifMuted(tc.prefs, tc.notifType, tc.section); got != tc.want {
				t.Fatalf("isNotifMuted(%v, %q, %q) = %v, want %v",
					tc.prefs, tc.notifType, tc.section, got, tc.want)
			}
		})
	}
}

func TestGroupMentionsAreUpdates(t *testing.T) {
	for _, tc := range []struct {
		prefs map[string]string
		want  bool
	}{
		{map[string]string{}, false},
		{map[string]string{"mentions": "all"}, false},
		{map[string]string{"mentions": "muted"}, true},
		{map[string]string{"comments": "muted"}, false},
	} {
		if got := groupMentionsAreUpdates(tc.prefs); got != tc.want {
			t.Fatalf("groupMentionsAreUpdates(%v) = %v, want %v", tc.prefs, got, tc.want)
		}
	}
}
