package daemon

import (
	"fmt"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestTaskRunFailureReasonLabelsStorageExhaustion(t *testing.T) {
	err := fmt.Errorf(
		"prepare execution environment: execenv: mkdir /data/workspaces/82be3a30-d4ec-401e-8ea0-8b564565ebaf: %w",
		execenv.ErrStorageExhausted,
	)

	if got, want := taskRunFailureReason(err), taskfailure.ReasonRuntimeStorageExhausted.String(); got != want {
		t.Errorf("taskRunFailureReason = %q, want %q", got, want)
	}
}
