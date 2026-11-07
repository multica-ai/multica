package ghsnapshot

import "testing"

func TestSnapshotDecided(t *testing.T) {
	cases := []struct {
		name string
		snap PRSnapshot
		want bool
	}{
		{"clean passed", PRSnapshot{Mergeable: "MERGEABLE", HasChecks: true, RollupState: "SUCCESS",
			Contexts: []CheckContext{{Status: "completed", Conclusion: "success"}}}, true},
		{"conflicting decided", PRSnapshot{Mergeable: "CONFLICTING", HasChecks: false}, true},
		{"mergeable unknown", PRSnapshot{Mergeable: "UNKNOWN", HasChecks: true, RollupState: "SUCCESS"}, false},
		{"rollup pending", PRSnapshot{Mergeable: "MERGEABLE", HasChecks: true, RollupState: "PENDING"}, false},
		{"running context", PRSnapshot{Mergeable: "MERGEABLE", HasChecks: true, RollupState: "SUCCESS",
			Contexts: []CheckContext{{Status: "in_progress"}}}, false},
		{"no checks but mergeable", PRSnapshot{Mergeable: "MERGEABLE", HasChecks: false}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.snap.Decided(); got != tc.want {
				t.Fatalf("Decided() = %v, want %v", got, tc.want)
			}
		})
	}
}
