package issueguard

import "testing"

func TestIsClosedStatus(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"done", true},
		{"cancelled", true},
		{"archived", true},
		{"backlog", false},
		{"todo", false},
		{"in_progress", false},
		{"in_review", false},
		{"blocked", false},
		{"", false},
		{"DONE", false}, // case-sensitive; statuses are stored lowercase
	}
	for _, c := range cases {
		if got := IsClosedStatus(c.status); got != c.want {
			t.Errorf("IsClosedStatus(%q) = %v, want %v", c.status, got, c.want)
		}
	}
}

func TestIsCompletedStatus(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"done", true},
		{"cancelled", true},
		{"archived", false}, // closed but NOT completed (KTD2)
		{"backlog", false},
		{"todo", false},
		{"in_progress", false},
		{"in_review", false},
		{"blocked", false},
		{"", false},
		{"DONE", false},
	}
	for _, c := range cases {
		if got := IsCompletedStatus(c.status); got != c.want {
			t.Errorf("IsCompletedStatus(%q) = %v, want %v", c.status, got, c.want)
		}
	}
}

// Archived must be closed but not completed — this is the whole point of the
// KTD2 split.  Pin it directly so a future edit can't silently collapse the
// two sets.
func TestArchivedIsClosedNotCompleted(t *testing.T) {
	if !IsClosedStatus("archived") {
		t.Error("archived must be closed")
	}
	if IsCompletedStatus("archived") {
		t.Error("archived must NOT be completed")
	}
}

// completed must remain a strict subset of closed: anything completed is
// closed, but not vice versa.
func TestCompletedIsSubsetOfClosed(t *testing.T) {
	for _, s := range []string{"backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled", "archived"} {
		if IsCompletedStatus(s) && !IsClosedStatus(s) {
			t.Errorf("status %q is completed but not closed — completed must be a subset of closed", s)
		}
	}
}
