package agent

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
)

// ErrArgvLimitExceeded reports that the OS refused to start a runtime because
// its command line was too large. Without it the member only ever sees a raw
// errno ("argument list too long") with no hint at the cause or the remedy
// (#9362). It wraps the original error, so errors.Is on the errno still holds.
var ErrArgvLimitExceeded = errors.New("agent command line exceeds the OS argument size limit")

// windowsErrorFilenameExcedRange is ERROR_FILENAME_EXCED_RANGE (206), the code
// CreateProcess returns for an over-long command line ("The filename or
// extension is too long", #6538).
const windowsErrorFilenameExcedRange = syscall.Errno(206)

// isArgvLimitError reports whether err is the OS refusing an oversized command
// line: E2BIG from execve on Unix, ERROR_FILENAME_EXCED_RANGE on Windows.
func isArgvLimitError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.E2BIG) {
		return true
	}
	return runtime.GOOS == "windows" && errors.Is(err, windowsErrorFilenameExcedRange)
}

// wrapArgvLimitError turns an oversized-command-line start failure into a
// named, actionable error and returns every other error unchanged.
func wrapArgvLimitError(err error) error {
	if !isArgvLimitError(err) {
		return err
	}
	return fmt.Errorf("%w (large content such as pasted logs in the issue thread or oversized custom_args can cause this; move large artifacts into issue attachments): %w",
		ErrArgvLimitExceeded, err)
}
