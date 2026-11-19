package agentinvoke

import (
	"context"

	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Allowed is the shared invocation decision for request and queue-write gates.
func Allowed(ctx context.Context, q *db.Queries, agent db.Agent, actorType, actorID, originatorUserID, workspaceID string) bool {
	effectiveUser := actorID
	if actorType != "member" {
		// agent / system: never trust the immediate principal, only the
		// resolved human originator at the top of the chain.
		effectiveUser = originatorUserID
	}

	isWorkspaceMember := false
	if effectiveUser != "" {
		if m, err := member(ctx, q, effectiveUser, workspaceID); err == nil {
			if projectaccess.IsGuestRole(m.Role) {
				return false
			}
			isWorkspaceMember = true
		}
	}

	// The agent owner may always invoke their own agent.
	if effectiveUser != "" && util.UUIDToString(agent.OwnerID) == effectiveUser {
		return true
	}

	if agent.PermissionMode != "public_to" {
		// private (or any unknown mode) is deny-by-default: no admin bypass,
		// no A2A bypass. Only the owner branch above passes.
		return false
	}

	targets, err := q.ListAgentInvocationTargets(ctx, agent.ID)
	if err != nil {
		return false
	}

	// Agents and system triggers are workspace-internal principals: a
	// workspace target admits them even when no human originator resolved.
	// This is a DELIBERATE, product-approved exception (MUL-3963): webhook /
	// system / workspace-wide automation must be able to trigger a
	// `public_to workspace` agent even though there is no human at the top of
	// the chain. It is scoped tightly — it ONLY relaxes the *workspace* target.
	// member/team targets still require a resolved human originator to match,
	// so an unattributed agent/system trigger FAILS CLOSED against a
	// member-/team-scoped private-ish allow-list and can never smuggle itself
	// onto someone's specific-people grant.
	workspaceBroad := actorType == "agent" || actorType == "system"

	for _, t := range targets {
		switch t.TargetType {
		case "workspace":
			if isWorkspaceMember || workspaceBroad {
				return true
			}
		case "member":
			// Requires a resolved human. agent/system triggers with no
			// originator (effectiveUser == "") never match here — fail closed.
			if effectiveUser != "" && util.UUIDToString(t.TargetID) == effectiveUser {
				return true
			}
		case "team":
			// Reserved: team membership does not exist yet in V1, so team
			// targets never admit anyone (also fail-closed for system/agent).
		}
	}
	return false
}

func member(ctx context.Context, q *db.Queries, userID, workspaceID string) (db.Member, error) {
	user, err := util.ParseUUID(userID)
	if err != nil {
		return db.Member{}, err
	}
	workspace, err := util.ParseUUID(workspaceID)
	if err != nil {
		return db.Member{}, err
	}
	return q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: user, WorkspaceID: workspace})
}
