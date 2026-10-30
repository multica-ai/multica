package daemon

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestRunTaskRejectsMismatchedAgentIdentityBeforePreparation(t *testing.T) {
	t.Parallel()

	d := &Daemon{}
	_, err := d.runTask(context.Background(), Task{
		ID:          "task-identity-mismatch",
		WorkspaceID: "workspace-a",
		AgentID:     "agent-a",
		Agent:       &AgentData{ID: "agent-b", Name: "Agent B"},
	}, "claude", 0, slog.Default())
	if !errors.Is(err, errInvalidTaskIdentity) {
		t.Fatalf("runTask error = %v, want invalid task identity", err)
	}
	if got := taskRunFailureReason(err); got != taskfailure.ReasonInvalidTaskIdentity.String() {
		t.Fatalf("failure reason = %q, want %q", got, taskfailure.ReasonInvalidTaskIdentity)
	}
}

func TestValidateTaskIdentityRequiresExpandedAgent(t *testing.T) {
	t.Parallel()

	err := validateTaskIdentity(Task{ID: "task-agent-missing", AgentID: "agent-a"})
	if !errors.Is(err, errInvalidTaskIdentity) {
		t.Fatalf("validateTaskIdentity error = %v, want invalid task identity", err)
	}
}

func TestValidateTaskIdentityAcceptsMatchingAgent(t *testing.T) {
	t.Parallel()

	if err := validateTaskIdentity(Task{ID: "task-agent-match", AgentID: "agent-a", Agent: &AgentData{ID: "agent-a"}}); err != nil {
		t.Fatalf("validateTaskIdentity: %v", err)
	}
}
