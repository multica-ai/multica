package platformcatalog

import "testing"

// TestEnforced pins the per-key "is this policy actually wired to the runtime
// gate" signal (FIR-3091 punkt 8). Every capability is enforced by default; the
// only exceptions are the three surfaced-but-not-yet-wired agent-start keys
// (FIR-3091 slice 4 / FIR-2409), of which only trigger_other_agent is live.
func TestEnforced(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		// The one agent-start key whose gate is actually wired.
		{"trigger_other_agent", true},
		// The three surfaced-but-not-yet-enforced exceptions.
		{"rerun_issue", false},
		{"schedule_agent_wakeup", false},
		{"trigger_autopilot", false},
		// An ordinary enforced capability — enforced by default.
		{"create_issue", true},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			if got := Enforced(tc.key); got != tc.want {
				t.Fatalf("Enforced(%q) = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}
