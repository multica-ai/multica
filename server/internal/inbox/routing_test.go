package inbox

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestRoute(t *testing.T) {
	agent := pgtype.Text{String: "agent", Valid: true}
	member := pgtype.Text{String: "member", Valid: true}
	muted := ""

	cases := []struct {
		name      string
		prefs     map[string]string
		notifType string
		actor     pgtype.Text
		want      string // muted means not delivered
	}{
		// Defaults match DefaultSection, so an untouched account sees no change.
		{"default assigned", nil, "issue_assigned", member, SectionAction},
		{"default mention", nil, "mentioned", agent, SectionAction},
		{"default member comment", nil, "new_comment", member, SectionAction},
		{"default agent comment", nil, "new_comment", agent, SectionActivity},
		{"default agent failure", nil, "task_failed", agent, SectionAction},
		{"default status", nil, "status_changed", member, SectionActivity},
		{"default assignee change", nil, "unassigned", member, SectionActivity},
		{"default update", nil, "due_date_changed", member, SectionActivity},
		{"explicit all is the default", map[string]string{"status_changes": "all"}, "status_changed", agent, SectionActivity},

		// The recipient's choice wins.
		{"status routed to action", map[string]string{"status_changes": "action"}, "status_changed", agent, SectionAction},
		{"agent comments routed to action", map[string]string{"agent_comments": "action"}, "new_comment", agent, SectionAction},
		{"mentions routed to activity", map[string]string{"mentions": "activity"}, "mentioned", member, SectionActivity},
		{"updates muted", map[string]string{"updates": "muted"}, "priority_changed", member, muted},
		{"agent comment choice leaves member comments alone", map[string]string{"agent_comments": "muted"}, "new_comment", member, SectionAction},

		// Muting comment volume never silences someone asking for you by name.
		{"muted comments still delivers mentions", map[string]string{"comments": "muted"}, "mentioned", member, SectionAction},
		{"muted mentions drops mentioned", map[string]string{"mentions": "muted"}, "mentioned", member, muted},
		{"muted mentions still delivers new_comment", map[string]string{"mentions": "muted"}, "new_comment", member, SectionAction},

		// A muted pre-routing group mutes both halves until the new key is set.
		{"legacy comments muted drops member comment", map[string]string{"comments": "muted"}, "new_comment", member, muted},
		{"legacy comments muted drops agent comment", map[string]string{"comments": "muted"}, "new_comment", agent, muted},
		{"legacy assignments muted drops assigned", map[string]string{"assignments": "muted"}, "issue_assigned", member, muted},
		{"legacy assignments muted drops unassigned", map[string]string{"assignments": "muted"}, "unassigned", member, muted},
		{"legacy agent_activity muted drops failure", map[string]string{"agent_activity": "muted"}, "task_failed", agent, muted},
		{"new key overrides muted legacy group", map[string]string{"comments": "muted", "agent_comments": "activity"}, "new_comment", agent, SectionActivity},
		{"muting comments leaves assignments alone", map[string]string{"comments": "muted"}, "issue_assigned", member, SectionAction},

		// Types outside the routable kinds always land in their default section.
		{"unroutable reaction", map[string]string{"comments": "muted"}, "reaction_added", member, SectionActivity},
		{"unroutable review request", map[string]string{"assigned": "muted"}, "review_requested", member, SectionAction},
		{"unknown value falls back to default", map[string]string{"status_changes": "loud"}, "status_changed", member, SectionActivity},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Route(c.prefs, c.notifType, c.actor)
			if c.want == muted {
				if ok {
					t.Fatalf("Route = (%q, delivered), want muted", got)
				}
				return
			}
			if !ok || got != c.want {
				t.Fatalf("Route = (%q, %v), want (%q, delivered)", got, ok, c.want)
			}
		})
	}
}

func TestItemSection(t *testing.T) {
	agent := pgtype.Text{String: "agent", Valid: true}
	if got := ItemSection(SectionText(SectionAction), "status_changed", agent); got != SectionAction {
		t.Errorf("stored action = %q", got)
	}
	if got := ItemSection(pgtype.Text{}, "status_changed", agent); got != SectionActivity {
		t.Errorf("unset falls back to default, got %q", got)
	}
	if got := ItemSection(SectionText("bogus"), "mentioned", agent); got != SectionAction {
		t.Errorf("unknown stored value falls back to default, got %q", got)
	}
}
