package service

import "testing"

// TestIsCoreTask pins the RIC-1024 task-category classification: core dev
// (implementation, architecture, refactor, security, backend/frontend, Chinese
// equivalents) routes to the squad, while ops / inspection / light tasks and
// uncategorized titles stay on the single-agent fast track.
func TestIsCoreTask(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		description string
		want        bool
	}{
		// English core markers.
		{"core implementation", "Implement the auth flow", "", true},
		{"feature", "Add feature: export dashboard", "", true},
		{"architecture", "Design the new service architecture", "", true},
		{"refactor", "Refactor the task queue", "", true},
		{"security", "Fix the SSRF vulnerability", "", true},
		{"backend", "Build the backend API", "", true},
		{"frontend", "Build the frontend component", "", true},
		{"database migration", "Write the DB migration", "", true},
		{"marker in description only", "Title", "Implement the protocol spec", true},
		// Chinese core markers.
		{"chinese core", "实现核心功能", "", true},
		{"chinese refactor", "重构调度模块", "", true},
		{"chinese security", "修复安全问题", "", true},
		// Fast-track strong ops markers always win over core (inspection /
		// monitoring / backup / cleanup / report is operational no matter the
		// subject — "security inspection" is an inspection, not a fix).
		{"security inspection is ops", "Run the security inspection", "", false},
		{"monitor not core", "Monitor API health", "", false},
		{"health check not core", "Health check the database", "", false},
		{"backup the database is ops", "Backup the database", "", false},
		{"report ops", "Generate the ops report", "", false},
		// Soft ops markers (deploy/ops/sync) only classify when no core is
		// present: "deploy the new API backend" is core; "deploy to prod" is ops.
		{"deploy core feature is core", "Deploy the new API backend", "", true},
		{"deploy alone is ops", "Deploy to production", "", false},
		{"ops alone is fast track", "Daily ops summary", "", false},
		// Neither set — unclassified stays fast track.
		{"unclassified", "Triage the incoming request", "", false},
		{"empty", "", "", false},
		{"bare core not a marker", "pge-core-squad rotation", "", false},
		{"ssrf is security core", "Fix the SSRF vulnerability", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCoreTask(tt.title, tt.description); got != tt.want {
				t.Fatalf("IsCoreTask(%q, %q) = %v, want %v", tt.title, tt.description, got, tt.want)
			}
		})
	}
}
