package inbox

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestSection(t *testing.T) {
	agent := pgtype.Text{String: "agent", Valid: true}
	member := pgtype.Text{String: "member", Valid: true}
	none := pgtype.Text{}

	cases := []struct {
		notifType string
		actor     pgtype.Text
		want      string
	}{
		{"issue_assigned", member, SectionAction},
		{"mentioned", agent, SectionAction},
		{"task_failed", none, SectionAction},
		{"children_done", none, SectionAction},
		{"quick_create_failed", agent, SectionAction},
		{"autopilot_paused", none, SectionAction},
		{"new_comment", member, SectionAction},
		{"new_comment", none, SectionAction},
		{"new_comment", agent, SectionActivity},
		{"status_changed", agent, SectionActivity},
		{"status_changed", member, SectionActivity},
		{"priority_changed", member, SectionActivity},
		{"assignee_changed", agent, SectionActivity},
		{"reaction_added", member, SectionActivity},
		{"quick_create_done", agent, SectionActivity},
		{"task_completed", agent, SectionActivity},
		{"some_future_type", agent, SectionAction},
	}
	for _, c := range cases {
		if got := Section(c.notifType, c.actor); got != c.want {
			t.Errorf("Section(%q, %q) = %q, want %q", c.notifType, c.actor.String, got, c.want)
		}
	}
}
