package daemon

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestShouldUseClaudeSDKBridge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		runMode        string
		approvalPolicy string
		want           bool
	}{
		{name: "plan always uses sdk", runMode: protocol.TaskRunModePlan, approvalPolicy: protocol.ApprovalPolicyAuto, want: true},
		{name: "prompt normal uses sdk", runMode: protocol.TaskRunModeNormal, approvalPolicy: protocol.ApprovalPolicyPrompt, want: true},
		{name: "auto normal stays native", runMode: protocol.TaskRunModeNormal, approvalPolicy: protocol.ApprovalPolicyAuto, want: false},
		{name: "deny normal stays native", runMode: protocol.TaskRunModeNormal, approvalPolicy: protocol.ApprovalPolicyDeny, want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldUseClaudeSDKBridge(tt.runMode, tt.approvalPolicy); got != tt.want {
				t.Fatalf("shouldUseClaudeSDKBridge(%q, %q) = %v, want %v", tt.runMode, tt.approvalPolicy, got, tt.want)
			}
		})
	}
}
