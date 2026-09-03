package issuepolicy

import "testing"

func TestLegacyStateUsesPhaseOutcomeWithoutCollapsingPolicies(t *testing.T) {
	tests := []struct {
		category string
		phase    string
		outcome  string
		terminal bool
		active   bool
	}{
		{"backlog", "backlog", "", false, false},
		{"todo", "unstarted", "", false, false},
		{"in_progress", "started", "", false, true},
		{"in_review", "started", "", false, false},
		{"blocked", "started", "", false, false},
		{"done", "completed", "completed", true, false},
		{"cancelled", "cancelled", "cancelled", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			state := FromLegacyCategory(tt.category)
			if state.Phase != tt.phase || state.Outcome != tt.outcome {
				t.Fatalf("state = %#v, want phase=%q outcome=%q", state, tt.phase, tt.outcome)
			}
			if state.IsTerminal() != tt.terminal {
				t.Fatalf("IsTerminal = %v, want %v", state.IsTerminal(), tt.terminal)
			}
			if state.AgentOwnsActiveWork() != tt.active {
				t.Fatalf("AgentOwnsActiveWork = %v, want %v", state.AgentOwnsActiveWork(), tt.active)
			}
		})
	}
}
