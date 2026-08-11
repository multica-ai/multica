package util

import "testing"

func TestIssueExecutionSuppressed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		metadata string
		want     bool
	}{
		{name: "boolean false", metadata: `{"workflow_role":"parent_orchestrator","execution_expected":false}`, want: true},
		{name: "string false", metadata: `{"workflow_role":"parent_orchestrator","execution_expected":"false"}`, want: true},
		{name: "normalized strings", metadata: `{"workflow_role":" Parent_Orchestrator ","execution_expected":" FALSE "}`, want: true},
		{name: "execution expected true", metadata: `{"workflow_role":"parent_orchestrator","execution_expected":true}`, want: false},
		{name: "string true", metadata: `{"workflow_role":"parent_orchestrator","execution_expected":"true"}`, want: false},
		{name: "role only", metadata: `{"workflow_role":"parent_orchestrator"}`, want: false},
		{name: "false only", metadata: `{"execution_expected":false}`, want: false},
		{name: "different role", metadata: `{"workflow_role":"stage","execution_expected":false}`, want: false},
		{name: "numeric zero is not false", metadata: `{"workflow_role":"parent_orchestrator","execution_expected":0}`, want: false},
		{name: "malformed", metadata: `{`, want: false},
		{name: "empty", metadata: ``, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IssueExecutionSuppressed([]byte(tc.metadata)); got != tc.want {
				t.Fatalf("IssueExecutionSuppressed(%s) = %v, want %v", tc.metadata, got, tc.want)
			}
		})
	}
}
