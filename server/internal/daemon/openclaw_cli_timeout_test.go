package daemon

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// TestTaskRunFailureReasonLabelsOpenclawCLITimeout is the daemon half of the
// #7112 fix. The error text ends in "context deadline exceeded", which
// taskfailure.Classify maps to agent_error.provider_network — so before the
// sentinel, a local `openclaw config file` stall was reported to the user as a
// dropped connection to the model provider, and auto-retried because that
// reason is on the retry allowlist. The stall is local and deterministic: the
// retry re-pays the same 8-11s and fails identically.
func TestTaskRunFailureReasonLabelsOpenclawCLITimeout(t *testing.T) {
	err := fmt.Errorf(
		"prepare execution environment: execenv: prepare openclaw config: locate openclaw active config: %w",
		fmt.Errorf("openclaw config file: context deadline exceeded (process: signal: killed): %w", execenv.ErrOpenclawCLITimeout),
	)

	want := taskfailure.ReasonRuntimeCLITimeout.String()
	if got := taskRunFailureReason(err); got != want {
		t.Errorf("taskRunFailureReason = %q, want %q", got, want)
	}

	// The reason must stay off the auto-retry allowlist (asserted next to that
	// table in internal/service): nothing about the host changes between
	// attempts, so a retry only burns another full preparation before failing
	// the same way.
}

// TestTaskRunFailureReasonKeepsProviderDeadline pins the other side of the
// same boundary: a real provider-side deadline (no sentinel) must keep landing
// in the retryable provider bucket.
func TestTaskRunFailureReasonKeepsProviderDeadline(t *testing.T) {
	err := fmt.Errorf("stream from provider: %w", context.DeadlineExceeded)

	want := taskfailure.ReasonAgentProviderNetwork.String()
	if got := taskRunFailureReason(err); got != want {
		t.Errorf("taskRunFailureReason = %q, want %q", got, want)
	}
	if errors.Is(err, execenv.ErrOpenclawCLITimeout) {
		t.Error("a bare context deadline must not match the openclaw CLI sentinel")
	}
}
