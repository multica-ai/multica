//go:build windows

package execenv

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestTaskRootRenameRetriesWindowsSharingErrors(t *testing.T) {
	t.Parallel()

	for _, err := range []error{
		windows.ERROR_ACCESS_DENIED,
		windows.ERROR_SHARING_VIOLATION,
		windows.ERROR_LOCK_VIOLATION,
	} {
		if !isRetryableTaskRootRenameError(err) {
			t.Errorf("error %v was not retryable", err)
		}
	}
	if isRetryableTaskRootRenameError(errors.New("permanent")) {
		t.Fatal("unrelated error was retryable")
	}
}
