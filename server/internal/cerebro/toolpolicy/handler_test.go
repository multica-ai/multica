package toolpolicy

import "testing"

// TestLooserThanOwner pins the System cap≤owner rule (FIR-1609 Phase 2): a System
// rule may never be authored looser than the owner it is born from. The check is
// pure (rank comparison), so it is exhaustively table-tested without a database.
func TestLooserThanOwner(t *testing.T) {
	cases := []struct {
		name     string
		proposed Setting
		owner    Setting
		reject   bool
	}{
		// Owner allows everything — System may be anything.
		{"owner allow, system allow", SettingAllow, SettingAllow, false},
		{"owner allow, system ask", SettingAsk, SettingAllow, false},
		{"owner allow, system deny", SettingDeny, SettingAllow, false},

		// Owner only asks — System may tighten to Ask/Deny but not loosen to Allow.
		{"owner ask, system allow", SettingAllow, SettingAsk, true},
		{"owner ask, system ask", SettingAsk, SettingAsk, false},
		{"owner ask, system deny", SettingDeny, SettingAsk, false},

		// Owner denies — only Deny is permitted for System.
		{"owner deny, system allow", SettingAllow, SettingDeny, true},
		{"owner deny, system ask", SettingAsk, SettingDeny, true},
		{"owner deny, system deny", SettingDeny, SettingDeny, false},

		// Inherit grants nothing, so it can never exceed an owner — never rejected.
		{"inherit under owner deny", SettingInherit, SettingDeny, false},
		{"inherit under owner allow", SettingInherit, SettingAllow, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := looserThanOwner(tc.proposed, tc.owner); got != tc.reject {
				t.Fatalf("looserThanOwner(%q, %q) = %v, want %v", tc.proposed, tc.owner, got, tc.reject)
			}
		})
	}
}
