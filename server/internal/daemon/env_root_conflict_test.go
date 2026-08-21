package daemon

import (
	"errors"
	"fmt"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestTaskRunFailureReasonEnvRootConflicts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
	}{
		{"remove existing env", fmt.Errorf("execenv: reset existing env: %w: directory is not empty", execenv.ErrEnvRootConflict)},
		{"different owner", fmt.Errorf("execenv: %w: belongs to another task", execenv.ErrEnvRootConflict)},
		{"unowned content", fmt.Errorf("execenv: %w: names no owning task", execenv.ErrEnvRootConflict)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, execenv.ErrEnvRootConflict) {
				t.Fatalf("fixture does not wrap ErrEnvRootConflict: %v", tc.err)
			}
			if got, want := taskRunFailureReason(tc.err), taskfailure.ReasonEnvPrepDirConflict.String(); got != want {
				t.Fatalf("taskRunFailureReason() = %q, want %q", got, want)
			}
		})
	}
}

func TestTaskRunFailureReasonDoesNotTextMatchEnvRootConflict(t *testing.T) {
	t.Parallel()
	err := errors.New("execenv: env root belongs to task; directory is not empty")
	if got := taskRunFailureReason(err); got == taskfailure.ReasonEnvPrepDirConflict.String() {
		t.Fatalf("plain-text error classified as %q without the sentinel", got)
	}
}
