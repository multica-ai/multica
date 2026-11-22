package taskfailure

import (
	"strings"
	"time"
)

// IsInterruptedRunTimeout recognizes startup/routing deadlines whose failed
// attempt can continue on the same workdir and session. Do not broaden this to
// arbitrary timeout text: agent decisions and deterministic CLI stalls have
// different retry policies.
func IsInterruptedRunTimeout(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "workspace routing discovery timed out" {
		return true
	}
	for _, method := range []string{"initialize", "thread/start", "thread/resume"} {
		prefix := "codex " + method + " failed: codex app-server handshake timeout: " + method + " did not respond after "
		if strings.HasPrefix(raw, prefix) {
			duration, err := time.ParseDuration(strings.TrimPrefix(raw, prefix))
			return err == nil && duration > 0
		}
	}
	return false
}
