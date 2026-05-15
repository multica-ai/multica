package daemon

import "github.com/multica-ai/multica/server/pkg/protocol"

func shouldUseClaudeSDKBridge(runMode, approvalPolicy string) bool {
	if runMode == protocol.TaskRunModePlan {
		return true
	}
	return approvalPolicy == protocol.ApprovalPolicyPrompt
}
