package handler

import (
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Paid feature agent_invocation_members (MUL-9491): letting specific members
// invoke an agent. "Only me" and "whole workspace" are free; a member list is
// the paid configuration. Team targets are not honored by the invoke check
// yet, so they count like members.
//
// Invocation itself (canInvokeAgent) never consults the feature: after a
// downgrade, members on an existing list keep invoking and everyone else is
// still refused.

const (
	agentInvocationOpCreate     = "create"
	agentInvocationOpAddMembers = "add_members"
)

// agentInvocationMembersAdds reports whether moving an agent's invocation
// permission from (beforeMode, before) to after adds to the paid feature: the
// result is a member list (no workspace target) that names someone the
// current permission did not. Switching from "only me" or "whole workspace"
// to a member list is an addition; switching to "only me" or "whole
// workspace", removing people, and re-saving the same list are not. A create
// is the move from "only me" with no targets.
//
// An emptied list never reaches here as a member list: parsePermissionInput
// normalises an empty public_to to the workspace target, which is free.
func agentInvocationMembersAdds(beforeMode string, before []db.AgentInvocationTarget, after resolvedPermission) bool {
	if after.mode != permissionModePublicTo {
		return false
	}
	for _, t := range after.targets {
		if t.targetType == invocationTargetWorkspace {
			return false
		}
	}
	if len(after.targets) == 0 {
		return false
	}
	if beforeMode != permissionModePublicTo {
		return true
	}
	have := make(map[string]struct{}, len(before))
	for _, t := range before {
		if t.TargetType == invocationTargetWorkspace {
			// Everyone could invoke it; a member list is a new configuration.
			return true
		}
		have[t.TargetType+":"+uuidToString(t.TargetID)] = struct{}{}
	}
	for _, t := range after.targets {
		if _, ok := have[t.targetType+":"+uuidToString(t.targetID)]; !ok {
			return true
		}
	}
	return false
}

// agentPermissionNeedsCurrentTargets reports whether deciding the addition
// needs the agent's current targets: only a member-list result does.
func agentPermissionNeedsCurrentTargets(after resolvedPermission) bool {
	return agentInvocationMembersAdds(permissionModePrivate, nil, after)
}
