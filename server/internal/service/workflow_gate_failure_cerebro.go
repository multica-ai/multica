package service

import "strings"

// A Workflow completion-gate rejection is addressed to the agent runtime, not
// to the people reading the issue. When the daemon converts the 400 into a
// FailTask, the raw error text used to be posted as the agent's comment —
// replacing the run's actual answer with an HTTP error in a human thread
// (FIR-4643). The gate's own hook_run rows keep the audit trail, so nothing is
// lost by keeping the rejection out of the conversation.
//
// Source of truth for these strings: ErrTaskContinuationRequired and
// ErrTaskCompletionContextUnavailable in
// server/internal/cerebro/workflows/task_completion_gate.go. That package
// imports this one, so the dependency cannot be reversed; a test in the
// workflows package guards both literals against drift.
var workflowGateFailureMarkers = []string{
	"task completion requires an actual continuation",
	"task completion context is unavailable",
}

// IsWorkflowGateFailure reports whether a daemon-reported failure message is a
// Workflow completion-gate rejection rather than a real agent failure.
func IsWorkflowGateFailure(errMsg string) bool {
	for _, marker := range workflowGateFailureMarkers {
		if strings.Contains(errMsg, marker) {
			return true
		}
	}
	return false
}
