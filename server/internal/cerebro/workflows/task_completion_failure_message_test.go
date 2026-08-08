package workflows

import (
	"fmt"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

// FIR-4643: service.IsWorkflowGateFailure matches on literal strings because
// the service package cannot import this one. This test is the drift guard —
// rewording either error here must fail until the literals are updated too.
func TestGateErrorsAreRecognisedAsWorkflowGateFailures(t *testing.T) {
	for _, err := range []error{ErrTaskContinuationRequired, ErrTaskCompletionContextUnavailable} {
		// Shaped like the message the daemon builds in reportTaskResult.
		daemonMessage := fmt.Sprintf(
			`complete task failed: POST /api/daemon/tasks/x/complete returned 400: {"error":"%s: do something"}`,
			err.Error(),
		)
		if !service.IsWorkflowGateFailure(daemonMessage) {
			t.Fatalf("service.IsWorkflowGateFailure did not recognise %q", err.Error())
		}
	}
}

// A real agent failure must still reach the issue as a comment.
func TestAgentFailureIsNotAWorkflowGateFailure(t *testing.T) {
	if service.IsWorkflowGateFailure("agent exited with code 1: build failed") {
		t.Fatal("agent failure suppressed as a gate rejection")
	}
}
