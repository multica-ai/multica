package taskfailure

import (
	"strings"
	"time"
)

// IsInterruptedRunTimeout identifies known routing/thread-setup deadlines.
// Error text alone does not establish that process cleanup completed;
// callers must not use this predicate as permission to retry.
func IsInterruptedRunTimeout(raw string) bool {
	raw = strings.TrimSuffix(strings.TrimSpace(raw), "; retry suppressed: process cleanup/reap not confirmed")
	if raw == "workspace routing discovery timed out" {
		return true
	}
	for _, method := range []string{"thread/start", "thread/resume"} {
		prefix := "codex " + method + " failed: codex app-server handshake timeout: " + method + " did not respond after "
		if strings.HasPrefix(raw, prefix) {
			duration, err := time.ParseDuration(strings.TrimPrefix(raw, prefix))
			return err == nil && duration > 0
		}
	}
	return false
}
