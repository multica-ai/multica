package workflows

import (
	"errors"
	"log/slog"
)

// Hook lifecycle transitions worth an audit record. Read-only routes are not
// audited: they change nothing and would drown the trail.
const (
	hookAuditCreate       = "create"
	hookAuditUpdate       = "update"
	hookAuditPublish      = "publish"
	hookAuditDisable      = "disable"
	hookAuditDiscardDraft = "discard_draft"
	hookAuditDelete       = "delete"
)

// hookLifecycleAudit records one hook lifecycle transition as a structured
// event. A log sink can count these by action and outcome, so the same records
// double as the lifecycle metric.
//
// The record carries identifiers, versions and an outcome only. Hook names,
// descriptions, condition values, requirement messages, action payloads and
// scope target labels are deliberately never emitted — the trail answers "who
// changed which version of which hook, and did it work" without copying
// workspace content into a log sink that is not workspace-scoped. Failures are
// classified from the package's sentinel errors for the same reason: a raw
// error message can carry a hook name back out through a DB constraint.
func hookLifecycleAudit(action, workspaceID, hookID string, actor HookPermissionActor, policy *HookPolicy, err error) {
	record := []any{
		"action", action,
		"workspace_id", workspaceID,
		"actor_type", actor.Type,
		"actor_id", actor.ID,
	}
	if hookID != "" {
		record = append(record, "hook_id", hookID)
	}
	if policy != nil {
		record = append(record,
			"family_id", policy.FamilyID,
			"policy_id", policy.ID,
			"version", policy.Version,
			"revision", policy.Revision,
			"mode", string(policy.Mode),
			"lifecycle_state", string(policy.Lifecycle.State),
			"live_policy_id", policy.Lifecycle.LivePolicyID,
			"live_version", policy.Lifecycle.LiveVersion,
		)
	}
	if err != nil {
		slog.Warn("cerebro hook lifecycle", append(record, "outcome", "failed", "failure", hookFailureKind(err))...)
		return
	}
	slog.Info("cerebro hook lifecycle", append(record, "outcome", "ok")...)
}

// hookFailureKind maps an error to a stable, content-free label.
func hookFailureKind(err error) string {
	switch {
	case errors.Is(err, ErrHookPolicyNotFound):
		return "not_found"
	case errors.Is(err, ErrHookPublishPrerequisite):
		return "publish_prerequisite"
	case errors.Is(err, ErrHookDraftRevisionStale):
		return "draft_revision_stale"
	case errors.Is(err, ErrHookEventNotFound):
		return "event_not_found"
	case errors.Is(err, ErrManagedHookLocked):
		return "managed_locked"
	default:
		return "internal"
	}
}
