package handler

import "testing"

// FIR-4727: the run-modal blocker line only fires for an agent running inside a
// task, and never renders a blank entry.
func TestCommentGateBlockerLine(t *testing.T) {
	tests := []struct {
		name       string
		authorType string
		taskID     string
		message    string
		wantLine   string
		wantOK     bool
	}{
		{"agent in task, real message", "agent", "task-1", "Konklusion mangler i første linje.", "Konklusion mangler i første linje.", true},
		{"agent in task, empty message falls back", "agent", "task-1", "  ", "Comment blocked by a before.message.send hook.", true},
		{"member author never surfaces", "member", "task-1", "blocked", "", false},
		{"agent without a task has no run modal", "agent", "", "blocked", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			line, ok := commentGateBlockerLine(tc.authorType, tc.taskID, tc.message)
			if ok != tc.wantOK || line != tc.wantLine {
				t.Fatalf("commentGateBlockerLine(%q,%q,%q) = (%q,%v), want (%q,%v)",
					tc.authorType, tc.taskID, tc.message, line, ok, tc.wantLine, tc.wantOK)
			}
		})
	}
}
