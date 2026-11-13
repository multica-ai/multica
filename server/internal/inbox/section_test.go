package inbox

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestDefaultSection(t *testing.T) {
	for notifType, want := range map[string]string{
		"mentioned":                SectionNeedsYou,
		"issue_assigned":           SectionNeedsYou,
		"agent_completed":          SectionNeedsYou,
		"task_failed":              SectionNeedsYou,
		"app_action_requested":     SectionNeedsYou,
		"autopilot_paused":         SectionNeedsYou,
		"autopilot_quota_exceeded": SectionNeedsYou,
		"quick_create_failed":      SectionNeedsYou,
		// Results and courtesies ask nothing of the recipient.
		"issue_subscribed":   SectionUpdates,
		"quick_create_done":  SectionUpdates,
		"app_action_result":  SectionUpdates,
		"reaction_added":     SectionUpdates,
		"unassigned":         SectionUpdates,
		"a_type_added_later": SectionUpdates,
	} {
		if got := DefaultSection(notifType); got != want {
			t.Errorf("DefaultSection(%q) = %q, want %q", notifType, got, want)
		}
	}
}

func TestSubscriberSection(t *testing.T) {
	for _, tc := range []struct {
		notifType    string
		addressed    bool
		waitsOnHuman bool
		want         string
	}{
		// The same comment needs whoever answers for the issue and is an update
		// for someone who only follows it.
		{"new_comment", true, false, SectionNeedsYou},
		{"new_comment", false, false, SectionUpdates},
		{"task_failed", true, false, SectionNeedsYou},
		{"task_failed", false, false, SectionUpdates},
		{"agent_blocked", true, false, SectionNeedsYou},
		{"agent_blocked", false, false, SectionUpdates},
		// A status change needs them only when the new status waits on a person.
		{"status_changed", true, true, SectionNeedsYou},
		{"status_changed", true, false, SectionUpdates},
		{"status_changed", false, true, SectionUpdates},
		// Field edits are updates for everyone.
		{"priority_changed", true, true, SectionUpdates},
		{"assignee_changed", true, true, SectionUpdates},
		{"due_date_changed", true, true, SectionUpdates},
		{"issue_created", true, true, SectionUpdates},
	} {
		if got := SubscriberSection(tc.notifType, tc.addressed, tc.waitsOnHuman); got != tc.want {
			t.Errorf("SubscriberSection(%q, addressed=%v, waits=%v) = %q, want %q",
				tc.notifType, tc.addressed, tc.waitsOnHuman, got, tc.want)
		}
	}
}

func TestEffectiveSection(t *testing.T) {
	stored := func(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }
	for _, tc := range []struct {
		name      string
		stored    pgtype.Text
		notifType string
		read      bool
		want      string
	}{
		{"a stored section wins over the type", stored(SectionUpdates), "mentioned", false, SectionUpdates},
		{"a stored section survives being read", stored(SectionNeedsYou), "new_comment", true, SectionNeedsYou},
		{"an unread legacy mention still needs the recipient", pgtype.Text{}, "mentioned", false, SectionNeedsYou},
		{"a read legacy mention was handled when it was read", pgtype.Text{}, "mentioned", true, SectionUpdates},
		{"a legacy comment has no recorded relation", pgtype.Text{}, "new_comment", false, SectionUpdates},
		{"a value from the retired routing is treated as legacy", stored("action"), "issue_assigned", false, SectionNeedsYou},
		{"a retired value on a read row is an update", stored("activity"), "issue_assigned", true, SectionUpdates},
	} {
		if got := EffectiveSection(tc.stored, tc.notifType, tc.read); got != tc.want {
			t.Errorf("%s: EffectiveSection = %q, want %q", tc.name, got, tc.want)
		}
	}
}
