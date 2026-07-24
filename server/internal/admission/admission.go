// Package admission holds the pure member-permission predicates shared by the
// HTTP handlers and the automation hook service, so both judge agent invocation
// and autopilot write access with identical semantics. The functions here take
// already-loaded rows and make no DB calls, which lets the hook service evaluate
// them inside its write transaction against a hook's stored principal (MUL-4332
// PR2 review round 3).
package admission

import (
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// MemberHitsInvocationTargets reports whether a member is on a public_to agent's
// allow-list: a workspace target admits any member; a member target admits the
// matching user; team targets are inert in v1.
func MemberHitsInvocationTargets(targets []db.AgentInvocationTarget, userID string) bool {
	for _, t := range targets {
		switch t.TargetType {
		case "workspace":
			return true
		case "member":
			if util.UUIDToString(t.TargetID) == userID {
				return true
			}
		}
	}
	return false
}

// AgentInvocableByMember decides whether a member principal may invoke an agent,
// for the hook service's fail-closed save-time admission. A principal who is not
// a CURRENT member of the workspace can invoke nothing — not even an agent they
// own — because a hook must run under a live, accountable member (review round 4,
// point 1); this is stricter than the interactive Handler.canInvokeAgent, whose
// owner branch is membership-independent. Given a current member: the agent owner
// may invoke; otherwise a public_to agent is invocable only when the member is on
// its allow-list. There is no admin bypass — an admin editing a hook may not
// grant a stored principal reach the principal does not have.
func AgentInvocableByMember(agent db.Agent, targets []db.AgentInvocationTarget, memberUserID string, isCurrentMember bool) bool {
	if !isCurrentMember {
		return false
	}
	if memberUserID != "" && util.UUIDToString(agent.OwnerID) == memberUserID {
		return true
	}
	if agent.PermissionMode != "public_to" {
		return false
	}
	for _, t := range targets {
		switch t.TargetType {
		case "workspace":
			if isCurrentMember {
				return true
			}
		case "member":
			if memberUserID != "" && util.UUIDToString(t.TargetID) == memberUserID {
				return true
			}
		}
	}
	return false
}

// AutopilotWriteByOwnership reports whether a member may write an autopilot by
// role or authorship (workspace owner/admin, or the member who created it).
// Collaborator grants are checked separately by the caller (a DB lookup).
func AutopilotWriteByOwnership(ap db.Autopilot, member db.Member) bool {
	if RoleAllowed(member.Role, "owner", "admin") {
		return true
	}
	return ap.CreatedByType == "member" && util.UUIDToString(ap.CreatedByID) == util.UUIDToString(member.UserID)
}

// RoleAllowed reports whether role is one of the allowed workspace roles.
func RoleAllowed(role string, allowed ...string) bool {
	for _, a := range allowed {
		if role == a {
			return true
		}
	}
	return false
}
