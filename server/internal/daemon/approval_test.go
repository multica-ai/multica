package daemon

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestApprovalSimilarKey_CommandUsesConcreteCommand(t *testing.T) {
	t.Parallel()

	reqA := agent.ApprovalRequest{Type: "command_approval", Detail: "git status"}
	reqB := agent.ApprovalRequest{Type: "command_approval", Detail: "rm -rf /tmp/example"}

	if approvalSimilarKey(reqA) == approvalSimilarKey(reqB) {
		t.Fatalf("different commands should not share the same similarity key")
	}
}

func TestApprovalSimilarKey_FileChangeUsesPath(t *testing.T) {
	t.Parallel()

	reqA := agent.ApprovalRequest{Type: "file_change_approval", Detail: `{"path":"src/a.ts"}`}
	reqB := agent.ApprovalRequest{Type: "file_change_approval", Detail: `{"path":"src/b.ts"}`}

	if approvalSimilarKey(reqA) == approvalSimilarKey(reqB) {
		t.Fatalf("different file paths should not share the same similarity key")
	}
}
