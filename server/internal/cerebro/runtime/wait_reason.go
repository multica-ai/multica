package runtime

import (
	"fmt"
	"strings"
	"time"
)

// runtime_paused wait_reason prefix — stamped on queued tasks while their
// runtime is paused so the issue UI can explain the wait without posting an
// issue comment (FIR-2717). Format:
//
//	runtime_paused|<pause_reason>|<unpause_at RFC3339>
//
// unpause_at is empty when the circuit is open (manual-only resume).
const runtimePauseWaitReasonPrefix = "runtime_paused|"

// FormatRuntimePauseWaitReason builds the wait_reason value stored on queued
// tasks when a runtime is paused.
func FormatRuntimePauseWaitReason(pauseReason string, unpauseAt time.Time, circuitOpen bool) string {
	reason := strings.TrimSpace(pauseReason)
	if reason == "" {
		reason = "rate_limit"
	}
	until := ""
	if !circuitOpen && !unpauseAt.IsZero() {
		until = unpauseAt.UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("%s%s|%s", runtimePauseWaitReasonPrefix, reason, until)
}

// IsRuntimePauseWaitReason reports whether wait_reason was stamped by PauseRuntime.
func IsRuntimePauseWaitReason(waitReason string) bool {
	return strings.HasPrefix(waitReason, runtimePauseWaitReasonPrefix)
}
