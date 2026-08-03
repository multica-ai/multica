package taskmandate

import (
	"encoding/json"
	"errors"
)

type VerdictCode string
type RecoveryAction string

const (
	VerdictAllowed              VerdictCode = "allowed"
	VerdictMandateMissing       VerdictCode = "task_mandate_missing"
	VerdictMandateExpired       VerdictCode = "task_mandate_expired"
	VerdictIdentityMismatch     VerdictCode = "task_identity_mismatch"
	VerdictStaleGeneration      VerdictCode = "task_generation_stale"
	VerdictToolNotAuthorized    VerdictCode = "task_tool_not_authorized"
	VerdictFinalizationConflict VerdictCode = "task_finalization_conflict"
	VerdictInternalError        VerdictCode = "task_mandate_internal_error"

	RecoveryNone               RecoveryAction = "none"
	RecoveryRetry              RecoveryAction = "retry"
	RecoveryRetryClaim         RecoveryAction = "retry_claim"
	RecoveryStartNewTask       RecoveryAction = "start_new_task"
	RecoveryRefreshTaskContext RecoveryAction = "refresh_task_context"
	RecoveryFixInventory       RecoveryAction = "fix_inventory"
)

// Verdict is the additive, machine-readable decision contract shared by every
// Task Mandate surface. Message is stable operator copy; callers must not branch
// on it. Code and RecoveryAction are the API contract.
type Verdict struct {
	Allowed        bool           `json:"allowed"`
	Code           VerdictCode    `json:"code"`
	RecoveryAction RecoveryAction `json:"recovery_action"`
	Message        string         `json:"message"`
}

func AllowedVerdict() Verdict {
	return Verdict{Allowed: true, Code: VerdictAllowed, RecoveryAction: RecoveryNone, Message: "The callable is inside the active Task Mandate."}
}

func VerdictForError(err error) Verdict {
	switch {
	case err == nil:
		return AllowedVerdict()
	case errors.Is(err, ErrMissing):
		return Verdict{Code: VerdictMandateMissing, RecoveryAction: RecoveryRetryClaim, Message: "No Task Mandate is available for this task identity."}
	case errors.Is(err, ErrExpired):
		return Verdict{Code: VerdictMandateExpired, RecoveryAction: RecoveryStartNewTask, Message: "The Task Mandate run window has ended."}
	case errors.Is(err, ErrIdentityMismatch):
		return Verdict{Code: VerdictIdentityMismatch, RecoveryAction: RecoveryRefreshTaskContext, Message: "The task, workspace, and agent identity do not match the Task Mandate."}
	case errors.Is(err, ErrStaleClaimGeneration), errors.Is(err, ErrStaleFinalizationWriter):
		return Verdict{Code: VerdictStaleGeneration, RecoveryAction: RecoveryRetryClaim, Message: "The caller is using a stale Task Mandate generation."}
	case errors.Is(err, ErrToolDeny):
		return Verdict{Code: VerdictToolNotAuthorized, RecoveryAction: RecoveryStartNewTask, Message: "The callable is outside the finalized Task Mandate."}
	case errors.Is(err, ErrFinalizedGrantsChanged):
		return Verdict{Code: VerdictFinalizationConflict, RecoveryAction: RecoveryFixInventory, Message: "The finalized callable inventory changed during claim finalization."}
	default:
		return Verdict{Code: VerdictInternalError, RecoveryAction: RecoveryRetry, Message: "The Task Mandate decision could not be completed."}
	}
}

// VerdictJSON is the transport-neutral denial envelope used where a surface
// can only return a string (notably Gateway tool and MCP error results). The
// nested verdict remains the stable machine contract; detail preserves the
// underlying diagnostic for operators and older clients that display it.
func VerdictJSON(err error) string {
	payload := struct {
		Verdict Verdict `json:"verdict"`
		Detail  string  `json:"detail,omitempty"`
	}{Verdict: VerdictForError(err)}
	if err != nil {
		payload.Detail = err.Error()
	}
	raw, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return `{"verdict":{"allowed":false,"code":"task_mandate_internal_error","recovery_action":"retry","message":"The Task Mandate decision could not be completed."}}`
	}
	return string(raw)
}
