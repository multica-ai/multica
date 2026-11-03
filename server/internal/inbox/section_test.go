package inbox

import "testing"

func TestDefaultSection(t *testing.T) {
	cases := []struct {
		notifType string
		want      string
	}{
		{"issue_assigned", SectionAction},
		{"mentioned", SectionAction},
		{"task_failed", SectionAction},
		{"children_done", SectionAction},
		{"quick_create_failed", SectionAction},
		{"autopilot_paused", SectionAction},
		// A row without a stored section does not record who the comment
		// @mentioned, so it stays action whoever wrote it.
		{"new_comment", SectionAction},
		{"status_changed", SectionActivity},
		{"priority_changed", SectionActivity},
		{"assignee_changed", SectionActivity},
		{"reaction_added", SectionActivity},
		{"quick_create_done", SectionActivity},
		{"task_completed", SectionActivity},
		{"some_future_type", SectionAction},
	}
	for _, c := range cases {
		if got := DefaultSection(c.notifType); got != c.want {
			t.Errorf("DefaultSection(%q) = %q, want %q", c.notifType, got, c.want)
		}
	}
}
