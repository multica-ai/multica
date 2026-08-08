package util

import "strings"

const parentOrchestratorWorkflowRole = "parent_orchestrator"

// IssueExecutionSuppressed reports whether issue metadata marks the issue as
// an aggregation-only parent orchestrator. Both keys are required so ordinary
// issues that use execution_expected=false as governance classification remain
// runnable unless they explicitly opt into the parent-orchestrator contract.
//
// Metadata written before the CLI inferred JSON booleans may contain the
// string "false", so the predicate accepts either that representation or the
// typed boolean false. Empty and malformed metadata fail open.
func IssueExecutionSuppressed(raw []byte) bool {
	metadata := JSONObjectOrEmpty(raw)
	role, ok := metadata["workflow_role"].(string)
	if !ok || strings.ToLower(strings.TrimSpace(role)) != parentOrchestratorWorkflowRole {
		return false
	}

	switch expected := metadata["execution_expected"].(type) {
	case bool:
		return !expected
	case string:
		return strings.EqualFold(strings.TrimSpace(expected), "false")
	default:
		return false
	}
}
